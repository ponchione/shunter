package shunter

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/executor"
	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

const (
	defaultListenAddr            = "127.0.0.1:3000"
	defaultHTTPReadHeaderTimeout = 5 * time.Second
	defaultHTTPIdleTimeout       = 60 * time.Second
)

var (
	// ErrAuthSigningKeyRequired reports that strict protocol auth lacks signing material.
	ErrAuthSigningKeyRequired = errors.New("shunter: auth signing key required")
	// ErrRuntimeNotReady reports that protocol traffic reached a non-ready runtime.
	ErrRuntimeNotReady = errors.New("shunter: runtime is not ready")
	// ErrRuntimeServing reports that a serving loop is already active.
	ErrRuntimeServing = errors.New("shunter: runtime is already serving")
)

func buildProtocolOptions(cfg ProtocolConfig) (protocol.ProtocolOptions, error) {
	if cfg.PingInterval < 0 {
		return protocol.ProtocolOptions{}, fmt.Errorf("protocol ping interval must not be negative")
	}
	if cfg.IdleTimeout < 0 {
		return protocol.ProtocolOptions{}, fmt.Errorf("protocol idle timeout must not be negative")
	}
	if cfg.CloseHandshakeTimeout < 0 {
		return protocol.ProtocolOptions{}, fmt.Errorf("protocol close handshake timeout must not be negative")
	}
	if cfg.WriteTimeout < 0 {
		return protocol.ProtocolOptions{}, fmt.Errorf("protocol write timeout must not be negative")
	}
	if cfg.DisconnectTimeout < 0 {
		return protocol.ProtocolOptions{}, fmt.Errorf("protocol disconnect timeout must not be negative")
	}
	if cfg.OutgoingBufferMessages < 0 {
		return protocol.ProtocolOptions{}, fmt.Errorf("protocol outgoing buffer messages must not be negative")
	}
	if cfg.IncomingQueueMessages < 0 {
		return protocol.ProtocolOptions{}, fmt.Errorf("protocol incoming queue messages must not be negative")
	}
	if cfg.MaxMessageSize < 0 {
		return protocol.ProtocolOptions{}, fmt.Errorf("protocol max message size must not be negative")
	}

	opts := protocol.DefaultProtocolOptions()
	if cfg.PingInterval != 0 {
		opts.PingInterval = cfg.PingInterval
	}
	if cfg.IdleTimeout != 0 {
		opts.IdleTimeout = cfg.IdleTimeout
	}
	if cfg.CloseHandshakeTimeout != 0 {
		opts.CloseHandshakeTimeout = cfg.CloseHandshakeTimeout
	}
	if cfg.WriteTimeout != 0 {
		opts.WriteTimeout = cfg.WriteTimeout
	}
	if cfg.DisconnectTimeout != 0 {
		opts.DisconnectTimeout = cfg.DisconnectTimeout
	}
	if cfg.OutgoingBufferMessages != 0 {
		opts.OutgoingBufferMessages = cfg.OutgoingBufferMessages
	}
	if cfg.IncomingQueueMessages != 0 {
		opts.IncomingQueueMessages = cfg.IncomingQueueMessages
	}
	if cfg.MaxMessageSize != 0 {
		opts.MaxMessageSize = cfg.MaxMessageSize
	}
	return opts, nil
}

func buildAuthConfig(cfg Config) (*auth.JWTConfig, *auth.MintConfig, error) {
	signingKey := append([]byte(nil), cfg.AuthSigningKey...)
	issuers := append([]string(nil), cfg.AuthIssuers...)
	audiences := append([]string(nil), cfg.AuthAudiences...)

	switch cfg.AuthMode {
	case AuthModeDev:
		if len(signingKey) == 0 {
			signingKey = make([]byte, 32)
			if _, err := rand.Read(signingKey); err != nil {
				return nil, nil, fmt.Errorf("generate dev auth signing key: %w", err)
			}
		}
		issuer := cfg.AnonymousTokenIssuer
		if issuer == "" {
			issuer = "shunter-dev"
		}
		if len(issuers) > 0 && !slices.Contains(issuers, issuer) {
			issuers = append(issuers, issuer)
		}
		audience := cfg.AnonymousTokenAudience
		if audience == "" {
			if len(audiences) > 0 {
				audience = audiences[0]
			} else {
				audience = "shunter-dev"
			}
		} else if len(audiences) > 0 && !slices.Contains(audiences, audience) {
			audiences = append(audiences, audience)
		}
		jwtCfg := &auth.JWTConfig{SigningKey: append([]byte(nil), signingKey...), Issuers: issuers, Audiences: audiences, AuthMode: auth.AuthModeAnonymous}
		mintCfg := &auth.MintConfig{Issuer: issuer, Audience: audience, SigningKey: append([]byte(nil), signingKey...), Expiry: cfg.AnonymousTokenTTL}
		return jwtCfg, mintCfg, nil
	case AuthModeStrict:
		if len(signingKey) == 0 {
			return nil, nil, ErrAuthSigningKeyRequired
		}
		return &auth.JWTConfig{SigningKey: signingKey, Issuers: issuers, Audiences: audiences, AuthMode: auth.AuthModeStrict}, nil, nil
	default:
		return nil, nil, fmt.Errorf("auth mode is invalid")
	}
}

// HTTPHandler returns a composable HTTP handler for the runtime network surface.
// The handler does not start runtime lifecycle; callers using it directly must
// call Start before serving traffic.
func (r *Runtime) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	if r != nil && r.buildConfig.EnableProtocol {
		mux.HandleFunc("/subscribe", r.handleSubscribe)
	}
	if r != nil && r.buildConfig.Observability.Diagnostics.MountHTTP {
		mux.Handle("/", RuntimeDiagnosticsHandler(r))
	}
	return mux
}

func (r *Runtime) handleSubscribe(w http.ResponseWriter, req *http.Request) {
	if r == nil || !r.buildConfig.EnableProtocol {
		http.NotFound(w, req)
		return
	}
	r.mu.Lock()
	if err := r.readyLocked(); err != nil || r.protocolServer == nil {
		r.mu.Unlock()
		http.Error(w, ErrRuntimeNotReady.Error(), http.StatusServiceUnavailable)
		return
	}
	server := r.protocolServer
	r.mu.Unlock()
	server.HandleSubscribe(w, req)
}

// ListenAndServe starts runtime lifecycle if needed, serves the runtime HTTP
// handler on Config.ListenAddr, and shuts serving plus runtime ownership down
// when ctx is canceled.
func (r *Runtime) ListenAndServe(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !r.tryBeginServing() {
		return ErrRuntimeServing
	}
	addr := r.listenAddr()
	ln, err := listenTCP(ctx, addr)
	if err != nil {
		r.endServing()
		return fmt.Errorf("listen %q: %w", addr, err)
	}
	return r.serveStarted(ctx, ln)
}

func listenTCP(ctx context.Context, addr string) (net.Listener, error) {
	if strings.ContainsRune(addr, '\x00') {
		return nil, fmt.Errorf("listen address must not contain NUL byte")
	}
	var listenConfig net.ListenConfig
	return listenConfig.Listen(ctx, "tcp", addr)
}

func (r *Runtime) listenAddr() string {
	if r.buildConfig.ListenAddr == "" {
		return defaultListenAddr
	}
	return r.buildConfig.ListenAddr
}

func (r *Runtime) serve(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !r.tryBeginServing() {
		_ = ln.Close()
		return ErrRuntimeServing
	}
	return r.serveStarted(ctx, ln)
}

func (r *Runtime) tryBeginServing() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.serving {
		return false
	}
	r.serving = true
	return true
}

func (r *Runtime) endServing() {
	r.mu.Lock()
	r.serving = false
	r.mu.Unlock()
}

func (r *Runtime) serveStarted(ctx context.Context, ln net.Listener) error {
	if ctx == nil {
		ctx = context.Background()
	}
	defer r.endServing()
	return serveHTTPWithLifecycle(ctx, ln, r.HTTPHandler(), r.Start, r.Close)
}

func serveHTTPWithLifecycle(ctx context.Context, ln net.Listener, handler http.Handler, start func(context.Context) error, stop func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := start(ctx); err != nil {
		_ = ln.Close()
		return err
	}

	httpServer := newServingHTTPServer(handler)
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr := httpServer.Shutdown(shutdownCtx)
		closeErr := stop()
		serveErr := <-errCh
		if shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
			return shutdownErr
		}
		if closeErr != nil {
			return closeErr
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return ctx.Err()
	case err := <-errCh:
		closeErr := stop()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return closeErr
	}
}

func newServingHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: defaultHTTPReadHeaderTimeout,
		IdleTimeout:       defaultHTTPIdleTimeout,
	}
}

func (r *Runtime) ensureProtocolGraphLocked() error {
	if !r.buildConfig.EnableProtocol {
		return nil
	}
	if r.protocolServer != nil {
		return nil
	}
	if r.executor == nil {
		return ErrRuntimeNotReady
	}
	jwtCfg, mintCfg, err := buildAuthConfig(r.config)
	if err != nil {
		return err
	}
	opts, err := buildProtocolOptions(r.config.Protocol)
	if err != nil {
		return err
	}
	conns := protocol.NewConnManager()
	inbox := executor.NewProtocolInboxAdapter(r.executor)
	clientSender := protocol.NewClientSender(conns, inbox)
	fanOutSender := protocol.NewFanOutSenderAdapter(clientSender)
	if swappable, ok := r.fanOutSender.(*swappableFanOutSender); ok {
		swappable.SetTarget(fanOutSender)
	} else {
		r.fanOutSender = fanOutSender
	}
	r.protocolConns = conns
	r.protocolInbox = inbox
	r.protocolSender = clientSender
	r.protocolServer = &protocol.Server{
		JWT:               jwtCfg,
		Mint:              mintCfg,
		Options:           opts,
		Executor:          inbox,
		Conns:             conns,
		Schema:            r.registry,
		State:             committedStateAccess{state: r.state},
		DeclaredReads:     r,
		VisibilityFilters: runtimeProtocolVisibilityFilters(r.module.visibilityFilters),
		Observer:          r.observability,
	}
	return nil
}

func runtimeProtocolVisibilityFilters(filters []VisibilityFilterDescription) []protocol.VisibilityFilter {
	if len(filters) == 0 {
		return nil
	}
	out := make([]protocol.VisibilityFilter, len(filters))
	for i, filter := range filters {
		out[i] = protocol.VisibilityFilter{
			SQL:                filter.SQL,
			ReturnTableID:      filter.ReturnTableID,
			UsesCallerIdentity: filter.UsesCallerIdentity,
		}
	}
	return out
}

func (r *Runtime) closeProtocolGraph(conns *protocol.ConnManager, inbox *executor.ProtocolInboxAdapter) {
	if conns == nil || inbox == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conns.CloseAll(ctx, inbox)
}

type committedStateAccess struct {
	state *store.CommittedState
}

func (a committedStateAccess) Snapshot() store.CommittedReadView {
	return a.state.Snapshot()
}

type swappableFanOutSender struct {
	mu     sync.RWMutex
	target subscription.FanOutSender
}

func newSwappableFanOutSender(target subscription.FanOutSender) *swappableFanOutSender {
	return &swappableFanOutSender{target: target}
}

func (s *swappableFanOutSender) SetTarget(target subscription.FanOutSender) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.target = target
}

func (s *swappableFanOutSender) Target() subscription.FanOutSender {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.target
}

func (s *swappableFanOutSender) SendTransactionUpdateHeavy(connID types.ConnectionID, outcome subscription.CallerOutcome, updates []subscription.SubscriptionUpdate, memo *subscription.EncodingMemo) error {
	return s.Target().SendTransactionUpdateHeavy(connID, outcome, updates, memo)
}

func (s *swappableFanOutSender) SendTransactionUpdateLight(connID types.ConnectionID, requestID uint32, updates []subscription.SubscriptionUpdate, memo *subscription.EncodingMemo) error {
	return s.Target().SendTransactionUpdateLight(connID, requestID, updates, memo)
}

func (s *swappableFanOutSender) SendSubscriptionError(connID types.ConnectionID, subErr subscription.SubscriptionError) error {
	return s.Target().SendSubscriptionError(connID, subErr)
}
