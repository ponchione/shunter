package protocol

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/types"
)

// Server is the HTTP-level entry point for WebSocket upgrades. One
// Server serves many concurrent connections; HandleSubscribe is
// routed from `/subscribe` by the host application.
type Server struct {
	// JWT configures token validation. Required. AuthMode determines
	// whether missing tokens are rejected with 401 (Strict) or
	// converted to a fresh anonymous identity (Anonymous).
	JWT *auth.JWTConfig
	// Mint is required only when JWT.AuthMode == AuthModeAnonymous.
	// Its fields control the issuer/audience/expiry of tokens the
	// server generates for anonymous connections.
	Mint *auth.MintConfig
	// Options tunes transport-layer behavior. DefaultProtocolOptions()
	// supplies SPEC-005 §12 defaults.
	Options ProtocolOptions
	// Executor is the lifecycle seam used by the default Upgraded
	// handler to run OnConnect and emit IdentityToken (Story 3.4).
	// When non-nil AND Conns is non-nil AND Upgraded is nil, the
	// handler drives Conn.RunLifecycle for every admitted upgrade.
	Executor ExecutorInbox
	// Conns tracks currently admitted connections. Required whenever
	// Executor is set so RunLifecycle can register the admitted
	// connection before the first server message is sent.
	Conns *ConnManager
	// Schema provides table name→ID resolution for Subscribe and
	// OneOffQuery handlers. Required for dispatch to work.
	Schema SchemaLookup
	// State provides read-only snapshot access for OneOffQuery.
	// Required for OneOffQuery to work.
	State CommittedStateAccess
	// DeclaredReads handles named QueryDeclaration and ViewDeclaration
	// protocol messages. Raw SQL handlers remain wired through Schema/State
	// and Executor; declared reads use this explicit name-based surface.
	DeclaredReads DeclaredReadHandler
	// VisibilityFilters are row-level filters expanded into raw external SQL
	// predicates after table-read admission and before execution/registration.
	VisibilityFilters []VisibilityFilter
	// Upgraded, when non-nil, overrides the built-in lifecycle and is
	// called immediately after the WebSocket handshake completes. It
	// is the extension point for tests that want to bypass OnConnect
	// and for advanced hosts that want custom admission semantics.
	Upgraded func(ctx context.Context, uc *UpgradeContext)
	// Observer receives runtime-scoped protocol observations.
	Observer Observer
}

// DeclaredReadHandler is the protocol/server seam for module-owned declared
// reads. Implementations receive the declaration name from the client message.
type DeclaredReadHandler interface {
	HandleDeclaredQuery(ctx context.Context, conn *Conn, msg *DeclaredQueryMsg)
	HandleSubscribeDeclaredView(ctx context.Context, conn *Conn, msg *SubscribeDeclaredViewMsg)
}

// UpgradeContext is the per-connection package that the upgrade
// handler hands to the lifecycle layer. Stories 3.3/3.4 consume
// the Identity + ConnectionID + Token + Compression mode.
type UpgradeContext struct {
	Conn                *websocket.Conn
	ProtocolVersion     ProtocolVersion
	Identity            types.Identity
	ConnectionID        types.ConnectionID
	Permissions         []string
	AllowAllPermissions bool
	// Token is the minted anonymous JWT when the server minted one
	// during upgrade. Empty for strict-mode connections that
	// presented a token.
	Token       string
	Compression uint8
	// Claims is the validated claim set when the client presented a
	// token. nil when the server minted anonymously.
	Claims *auth.Claims
}

// HandleSubscribe is the net/http handler for the `/subscribe`
// endpoint (SPEC-005 §2.3). It authenticates, validates request
// parameters, upgrades the connection, and hands control to s.Upgraded.
func (s *Server) HandleSubscribe(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.JWT == nil {
		err := errors.New("server misconfigured: JWT config is required")
		s.writeAuthRejected(w, err.Error(), http.StatusInternalServerError, "jwt_misconfigured", err)
		return
	}
	options, err := normalizeProtocolOptions(s.Options)
	if err != nil {
		s.writeRejected(w, "invalid protocol options: "+err.Error(), http.StatusInternalServerError, "rejected_internal", err)
		return
	}

	// 1. Auth — strict requires a token, anonymous mints on absence.
	token, hasToken := extractToken(r)
	var claims *auth.Claims
	var mintedToken string
	var identity types.Identity
	var permissions []string
	if hasToken {
		c, err := auth.ValidateJWT(token, s.JWT)
		if err != nil {
			s.writeAuthRejected(w, "invalid token: "+err.Error(), http.StatusUnauthorized, "invalid_token", err)
			return
		}
		claims = c
		identity = c.DeriveIdentity()
		permissions = append([]string(nil), c.Permissions...)
	} else {
		if s.JWT.AuthMode != auth.AuthModeAnonymous {
			err := errors.New("no token and strict auth enabled")
			s.writeAuthRejected(w, err.Error(), http.StatusUnauthorized, "missing_token", err)
			return
		}
		if s.Mint == nil {
			err := errors.New("server misconfigured: anonymous mode requires Mint config")
			s.writeAuthRejected(w, err.Error(), http.StatusInternalServerError, "mint_misconfigured", err)
			return
		}
		mt, id, err := auth.MintAnonymousToken(s.Mint)
		if err != nil {
			s.writeAuthRejected(w, "mint failed: "+err.Error(), http.StatusInternalServerError, "mint_failed", err)
			return
		}
		mintedToken = mt
		identity = id
	}
	allowAllPermissions := s.JWT.AuthMode == auth.AuthModeAnonymous

	// 2. connection_id: parse / generate / reject zero.
	connID, err := resolveConnectionID(r.URL.Query().Get("connection_id"))
	if err != nil {
		if errors.Is(err, ErrZeroConnectionID) {
			s.writeRejected(w, err.Error(), http.StatusBadRequest, "rejected_upgrade", err)
			return
		}
		s.writeRejected(w, "invalid connection_id: "+err.Error(), http.StatusBadRequest, "rejected_upgrade", err)
		return
	}

	// 3. compression mode: default none; reject unknown values.
	compression, err := parseCompressionParam(r.URL.Query().Get("compression"))
	if err != nil {
		s.writeRejected(w, err.Error(), http.StatusBadRequest, "rejected_upgrade", err)
		return
	}

	// 4. subprotocol check — client MUST offer a supported Shunter token.
	selected, version, ok := negotiateSubprotocol(r, SupportedSubprotocols())
	if !ok {
		s.writeRejected(w,
			"Sec-WebSocket-Protocol must include "+SubprotocolV1,
			http.StatusBadRequest,
			"rejected_upgrade",
			errors.New("missing required subprotocol"))
		return
	}

	// 5. Upgrade.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{selected},
	})
	if err != nil {
		// websocket.Accept has already written an HTTP response at
		// this point; nothing further to emit.
		s.recordRejected("rejected_upgrade", err)
		return
	}
	if options.MaxMessageSize > 0 {
		conn.SetReadLimit(options.MaxMessageSize)
	}

	// 6. Hand off.
	uc := &UpgradeContext{
		Conn:                conn,
		ProtocolVersion:     version,
		Identity:            identity,
		ConnectionID:        connID,
		Permissions:         append([]string(nil), permissions...),
		AllowAllPermissions: allowAllPermissions,
		Token:               mintedToken,
		Compression:         compression,
		Claims:              claims,
	}
	if s.Upgraded != nil {
		s.Upgraded(r.Context(), uc)
		return
	}
	if s.Executor != nil && s.Conns != nil {
		c := NewConn(
			uc.ConnectionID,
			uc.Identity,
			uc.Token,
			uc.Compression == CompressionGzip,
			uc.Conn,
			&options,
		)
		c.ProtocolVersion = uc.ProtocolVersion
		c.Permissions = append([]string(nil), uc.Permissions...)
		c.AllowAllPermissions = uc.AllowAllPermissions
		c.Observer = s.Observer
		// RunLifecycle closes the socket on its own failure paths; on
		// success it leaves the socket open for the background
		// goroutines below. Story 3.6 (Disconnect) closes c.closed,
		// unblocking them all; until 3.6 lands, the goroutines exit
		// naturally on ws error when the peer closes.
		if err := c.RunLifecycle(r.Context(), s.Executor, s.Conns); err != nil {
			result := "rejected_internal"
			if errors.Is(err, ErrExecutorAdmissionRejected) {
				result = "rejected_executor"
			} else if errors.Is(err, ErrConnectionIDInUse) {
				result = "rejected_connection_id"
			}
			s.recordRejected(result, err)
			return
		}
		// Spawn per-connection lifecycle goroutines. They outlive
		// this HTTP handler; the supervisor invokes Disconnect when
		// the first of them exits (peer close, idle timeout, ws
		// error), which drives the SPEC-005 §5.3 teardown once.
		dispatchDone := make(chan struct{})
		keepaliveDone := make(chan struct{})
		handlers := s.buildMessageHandlers()
		go func() {
			c.runDispatchLoop(context.Background(), handlers)
			close(dispatchDone)
		}()
		go func() {
			c.runKeepalive(context.Background())
			close(keepaliveDone)
		}()
		// Outbound writer goroutine drains OutboundCh → WebSocket.
		// It exits when Disconnect closes c.closed.
		outboundDone := make(chan struct{})
		go func() {
			c.runOutboundWriter(context.Background())
			close(outboundDone)
		}()
		go c.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", s.Executor, s.Conns, dispatchDone, keepaliveDone, outboundDone)
		return
	}
	// No Upgraded hook and no Executor wiring — close the connection
	// so the client does not hang. Preserves pre-3.4 bring-up behavior
	// when the embedder is still assembling its executor graph.
	s.recordRejected("rejected_internal", errors.New("protocol server missing executor wiring"))
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

func (s *Server) writeAuthRejected(w http.ResponseWriter, message string, status int, reason string, err error) {
	if s != nil && s.Observer != nil {
		s.Observer.LogProtocolAuthFailed(reason, err)
	}
	s.writeRejected(w, message, status, "rejected_auth", err)
}

func (s *Server) writeRejected(w http.ResponseWriter, message string, status int, result string, err error) {
	s.recordRejected(result, err)
	http.Error(w, message, status)
}

func (s *Server) recordRejected(result string, err error) {
	if s != nil && s.Conns != nil {
		s.Conns.RecordRejected()
	}
	if s != nil {
		logProtocolConnectionRejected(s.Observer, result, err)
	}
}

// buildMessageHandlers constructs the MessageHandlers that wire each
// client message type to the appropriate handler function, closing over
// the Server's dependencies (executor, schema, state).
func (s *Server) buildMessageHandlers() *MessageHandlers {
	handlers := &MessageHandlers{}
	if s.Executor != nil && s.Schema != nil {
		handlers.OnSubscribeSingle = func(ctx context.Context, conn *Conn, msg *SubscribeSingleMsg) {
			handleSubscribeSingleWithVisibility(ctx, conn, msg, s.Executor, s.Schema, s.VisibilityFilters)
		}
		handlers.OnSubscribeMulti = func(ctx context.Context, conn *Conn, msg *SubscribeMultiMsg) {
			handleSubscribeMultiWithVisibility(ctx, conn, msg, s.Executor, s.Schema, s.VisibilityFilters)
		}
	}
	if s.Executor != nil {
		handlers.OnUnsubscribeSingle = func(ctx context.Context, conn *Conn, msg *UnsubscribeSingleMsg) {
			handleUnsubscribeSingle(ctx, conn, msg, s.Executor)
		}
		handlers.OnUnsubscribeMulti = func(ctx context.Context, conn *Conn, msg *UnsubscribeMultiMsg) {
			handleUnsubscribeMulti(ctx, conn, msg, s.Executor)
		}
		handlers.OnCallReducer = func(ctx context.Context, conn *Conn, msg *CallReducerMsg) {
			handleCallReducer(ctx, conn, msg, s.Executor)
		}
	}
	if s.Schema != nil && s.State != nil {
		handlers.OnOneOffQuery = func(ctx context.Context, conn *Conn, msg *OneOffQueryMsg) {
			handleOneOffQueryWithVisibility(ctx, conn, msg, s.State, s.Schema, s.VisibilityFilters)
		}
	}
	if s.DeclaredReads != nil {
		handlers.OnDeclaredQuery = s.DeclaredReads.HandleDeclaredQuery
		handlers.OnSubscribeDeclaredView = s.DeclaredReads.HandleSubscribeDeclaredView
	}
	return handlers
}

// extractToken pulls a JWT from either the Authorization: Bearer
// header (preferred, SPEC-005 §2.3) or the ?token= query parameter.
// Returns the token and whether one was found.
func extractToken(r *http.Request) (string, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, ok := strings.Cut(strings.TrimSpace(h), " ")
		if ok && strings.EqualFold(scheme, "Bearer") {
			return strings.TrimSpace(token), true
		}
	}
	if q := r.URL.Query().Get("token"); q != "" {
		return q, true
	}
	return "", false
}

// resolveConnectionID returns the client-supplied ConnectionID if
// present, or a freshly generated one if not. All-zero client values
// produce ErrZeroConnectionID (SPEC-005 §4.3).
func resolveConnectionID(raw string) (types.ConnectionID, error) {
	if raw == "" {
		return GenerateConnectionID(), nil
	}
	c, err := types.ParseConnectionIDHex(raw)
	if err != nil {
		return types.ConnectionID{}, err
	}
	if c.IsZero() {
		return types.ConnectionID{}, ErrZeroConnectionID
	}
	return c, nil
}

// parseCompressionParam maps the ?compression= query param into a
// CompressionNone / CompressionGzip value. Missing or empty means
// CompressionNone. Anything else is a 400.
func parseCompressionParam(raw string) (uint8, error) {
	switch raw {
	case "", "none":
		return CompressionNone, nil
	case "gzip":
		return CompressionGzip, nil
	default:
		return 0, errors.New("unknown compression mode: " + raw)
	}
}

// negotiateSubprotocol inspects Sec-WebSocket-Protocol and returns the
// first supported token from preferred that the client also offered. Falls
// back to false when no overlap exists.
func negotiateSubprotocol(r *http.Request, preferred []string) (string, ProtocolVersion, bool) {
	header := r.Header.Values("Sec-WebSocket-Protocol")
	offered := make(map[string]struct{}, len(header))
	for _, line := range header {
		for _, raw := range strings.Split(line, ",") {
			tok := strings.TrimSpace(raw)
			if tok != "" {
				offered[tok] = struct{}{}
			}
		}
	}
	for _, want := range preferred {
		if _, ok := offered[want]; !ok {
			continue
		}
		version, ok := ProtocolVersionForSubprotocol(want)
		if ok {
			return want, version, true
		}
	}
	return "", ProtocolVersionUnknown, false
}
