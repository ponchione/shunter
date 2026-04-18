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

// SubprotocolV1 is the Shunter-native WebSocket subprotocol token,
// retained for backward compatibility while Phase 1 parity work
// introduces the reference token. See docs/parity-phase0-ledger.md
// P0-PROTOCOL-001 for the retention rationale.
const SubprotocolV1 = "v1.bsatn.shunter"

// SubprotocolReference is the SpacetimeDB reference WebSocket
// subprotocol token (SPEC-005 §2.2 parity target). A client that
// offers this token MUST be admitted as a Phase 1 parity requirement.
const SubprotocolReference = "v1.bsatn.spacetimedb"

// acceptedSubprotocols lists every token the server admits, in the
// order preferred when multiple are offered. The reference token is
// preferred so a client offering both negotiates the parity-aligned
// identifier.
var acceptedSubprotocols = []string{SubprotocolReference, SubprotocolV1}

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
	// handler to run OnConnect and emit InitialConnection (Story 3.4).
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
	// Upgraded, when non-nil, overrides the built-in lifecycle and is
	// called immediately after the WebSocket handshake completes. It
	// is the extension point for tests that want to bypass OnConnect
	// and for advanced hosts that want custom admission semantics.
	Upgraded func(ctx context.Context, uc *UpgradeContext)
}

// UpgradeContext is the per-connection package that the upgrade
// handler hands to the lifecycle layer. Stories 3.3/3.4 consume
// the Identity + ConnectionID + Token + Compression mode.
type UpgradeContext struct {
	Conn         *websocket.Conn
	Identity     types.Identity
	ConnectionID types.ConnectionID
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
	// 1. Auth — strict requires a token, anonymous mints on absence.
	token, hasToken := extractToken(r)
	var claims *auth.Claims
	var mintedToken string
	var identity types.Identity
	if hasToken {
		c, err := auth.ValidateJWT(token, s.JWT)
		if err != nil {
			http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
			return
		}
		claims = c
		identity = c.DeriveIdentity()
	} else {
		if s.JWT.AuthMode != auth.AuthModeAnonymous {
			http.Error(w, "no token and strict auth enabled", http.StatusUnauthorized)
			return
		}
		if s.Mint == nil {
			http.Error(w, "server misconfigured: anonymous mode requires Mint config", http.StatusInternalServerError)
			return
		}
		mt, id, err := auth.MintAnonymousToken(s.Mint)
		if err != nil {
			http.Error(w, "mint failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		mintedToken = mt
		identity = id
	}

	// 2. connection_id: parse / generate / reject zero.
	connID, err := resolveConnectionID(r.URL.Query().Get("connection_id"))
	if err != nil {
		if errors.Is(err, ErrZeroConnectionID) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "invalid connection_id: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 3. compression mode: default none; reject unknown values.
	compression, err := parseCompressionParam(r.URL.Query().Get("compression"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 4. subprotocol check — client MUST offer at least one of the
	// accepted tokens. Reference token is preferred.
	selected, ok := negotiateSubprotocol(r, acceptedSubprotocols)
	if !ok {
		http.Error(w,
			"Sec-WebSocket-Protocol must include "+SubprotocolReference+
				" or "+SubprotocolV1,
			http.StatusBadRequest)
		return
	}

	// 5. Upgrade.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{selected},
	})
	if err != nil {
		// websocket.Accept has already written an HTTP response at
		// this point; nothing further to emit.
		return
	}
	if s.Options.MaxMessageSize > 0 {
		conn.SetReadLimit(s.Options.MaxMessageSize)
	}

	// 6. Hand off.
	uc := &UpgradeContext{
		Conn:         conn,
		Identity:     identity,
		ConnectionID: connID,
		Token:        mintedToken,
		Compression:  compression,
		Claims:       claims,
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
			&s.Options,
		)
		// RunLifecycle closes the socket on its own failure paths; on
		// success it leaves the socket open for the background
		// goroutines below. Story 3.6 (Disconnect) closes c.closed,
		// unblocking them all; until 3.6 lands, the goroutines exit
		// naturally on ws error when the peer closes.
		if err := c.RunLifecycle(r.Context(), s.Executor, s.Conns); err != nil {
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
		// Exits when OutboundCh is closed during Disconnect.
		go c.runOutboundWriter(context.Background())
		go c.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", s.Executor, s.Conns, dispatchDone, keepaliveDone)
		return
	}
	// No Upgraded hook and no Executor wiring — close the connection
	// so the client does not hang. Preserves pre-3.4 bring-up behavior
	// when the embedder is still assembling its executor graph.
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

// buildMessageHandlers constructs the MessageHandlers that wire each
// client message type to the appropriate handler function, closing over
// the Server's dependencies (executor, schema, state).
func (s *Server) buildMessageHandlers() *MessageHandlers {
	handlers := &MessageHandlers{}
	if s.Executor != nil && s.Schema != nil {
		handlers.OnSubscribe = func(ctx context.Context, conn *Conn, msg *SubscribeMsg) {
			handleSubscribe(ctx, conn, msg, s.Executor, s.Schema)
		}
	}
	if s.Executor != nil {
		handlers.OnUnsubscribe = func(ctx context.Context, conn *Conn, msg *UnsubscribeMsg) {
			handleUnsubscribe(ctx, conn, msg, s.Executor)
		}
		handlers.OnCallReducer = func(ctx context.Context, conn *Conn, msg *CallReducerMsg) {
			handleCallReducer(ctx, conn, msg, s.Executor)
		}
	}
	if s.Schema != nil && s.State != nil {
		handlers.OnOneOffQuery = func(ctx context.Context, conn *Conn, msg *OneOffQueryMsg) {
			handleOneOffQuery(ctx, conn, msg, s.State, s.Schema)
		}
	}
	return handlers
}

// extractToken pulls a JWT from either the Authorization: Bearer
// header (preferred, SPEC-005 §2.3) or the ?token= query parameter.
// Returns the token and whether one was found.
func extractToken(r *http.Request) (string, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(h, prefix) {
			return strings.TrimSpace(h[len(prefix):]), true
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
// first token from preferred that the client also offered. Falls back
// to false when no overlap exists.
func negotiateSubprotocol(r *http.Request, preferred []string) (string, bool) {
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
		if _, ok := offered[want]; ok {
			return want, true
		}
	}
	return "", false
}
