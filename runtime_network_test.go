package shunter

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/protocol"
)

func TestBuildProtocolOptionsUsesDefaultsForZeroConfig(t *testing.T) {
	opts, err := buildProtocolOptions(ProtocolConfig{})
	if err != nil {
		t.Fatalf("buildProtocolOptions returned error: %v", err)
	}
	want := protocol.DefaultProtocolOptions()
	if opts != want {
		t.Fatalf("options = %+v, want %+v", opts, want)
	}
}

func TestBuildProtocolOptionsAppliesOverrides(t *testing.T) {
	opts, err := buildProtocolOptions(ProtocolConfig{
		PingInterval:           time.Second,
		IdleTimeout:            2 * time.Second,
		CloseHandshakeTimeout:  3 * time.Second,
		DisconnectTimeout:      4 * time.Second,
		OutgoingBufferMessages: 17,
		IncomingQueueMessages:  18,
		MaxMessageSize:         19,
	})
	if err != nil {
		t.Fatalf("buildProtocolOptions returned error: %v", err)
	}
	if opts.PingInterval != time.Second ||
		opts.IdleTimeout != 2*time.Second ||
		opts.CloseHandshakeTimeout != 3*time.Second ||
		opts.DisconnectTimeout != 4*time.Second ||
		opts.OutgoingBufferMessages != 17 ||
		opts.IncomingQueueMessages != 18 ||
		opts.MaxMessageSize != 19 {
		t.Fatalf("override mapping failed: %+v", opts)
	}
}

func TestBuildProtocolOptionsRejectsNegativeValues(t *testing.T) {
	cases := []ProtocolConfig{
		{PingInterval: -time.Second},
		{IdleTimeout: -time.Second},
		{CloseHandshakeTimeout: -time.Second},
		{DisconnectTimeout: -time.Second},
		{OutgoingBufferMessages: -1},
		{IncomingQueueMessages: -1},
		{MaxMessageSize: -1},
	}
	for _, cfg := range cases {
		if _, err := buildProtocolOptions(cfg); err == nil {
			t.Fatalf("buildProtocolOptions(%+v) succeeded; want error", cfg)
		}
	}
}

func TestBuildAuthConfigDevGeneratesAnonymousMintConfig(t *testing.T) {
	jwtCfg, mintCfg, err := buildAuthConfig(Config{AuthMode: AuthModeDev})
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if jwtCfg.AuthMode != auth.AuthModeAnonymous {
		t.Fatalf("auth mode = %v, want anonymous", jwtCfg.AuthMode)
	}
	if len(jwtCfg.SigningKey) == 0 || mintCfg == nil || len(mintCfg.SigningKey) == 0 {
		t.Fatal("dev auth did not configure signing/minting")
	}
	if string(jwtCfg.SigningKey) != string(mintCfg.SigningKey) {
		t.Fatal("dev auth JWT and mint signing keys differ")
	}
}

func TestBuildAuthConfigStrictRequiresSigningKey(t *testing.T) {
	_, _, err := buildAuthConfig(Config{AuthMode: AuthModeStrict})
	if !errors.Is(err, ErrAuthSigningKeyRequired) {
		t.Fatalf("error = %v, want ErrAuthSigningKeyRequired", err)
	}
}

func TestBuildAuthConfigStrictMapsAudiencesAndCopiesKey(t *testing.T) {
	key := []byte("test-secret")
	audiences := []string{"app"}
	cfg := Config{AuthMode: AuthModeStrict, AuthSigningKey: key, AuthAudiences: audiences}
	jwtCfg, mintCfg, err := buildAuthConfig(cfg)
	if err != nil {
		t.Fatalf("buildAuthConfig returned error: %v", err)
	}
	if jwtCfg.AuthMode != auth.AuthModeStrict || mintCfg != nil {
		t.Fatalf("unexpected strict config: jwt=%+v mint=%+v", jwtCfg, mintCfg)
	}
	key[0] = 'X'
	audiences[0] = "mutated"
	if string(jwtCfg.SigningKey) == string(key) {
		t.Fatal("signing key was not defensively copied")
	}
	if jwtCfg.Audiences[0] == audiences[0] {
		t.Fatal("audiences were not defensively copied")
	}
}

func TestRuntimeConfigDefensivelyCopiesAuthSlices(t *testing.T) {
	key := []byte("strict-runtime-secret")
	audiences := []string{"app"}
	cfg := Config{
		DataDir:        t.TempDir(),
		AuthMode:       AuthModeStrict,
		AuthSigningKey: key,
		AuthAudiences:  audiences,
	}

	rt, err := Build(validChatModule(), cfg)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	key[0] = 'X'
	audiences[0] = "mutated"

	got := rt.Config()
	if string(got.AuthSigningKey) != "strict-runtime-secret" {
		t.Fatalf("Config AuthSigningKey = %q, want original key", got.AuthSigningKey)
	}
	if len(got.AuthAudiences) != 1 || got.AuthAudiences[0] != "app" {
		t.Fatalf("Config AuthAudiences = %#v, want original audience", got.AuthAudiences)
	}

	got.AuthSigningKey[0] = 'Y'
	got.AuthAudiences[0] = "changed"

	again := rt.Config()
	if string(again.AuthSigningKey) != "strict-runtime-secret" {
		t.Fatalf("second Config AuthSigningKey = %q, want detached original key", again.AuthSigningKey)
	}
	if len(again.AuthAudiences) != 1 || again.AuthAudiences[0] != "app" {
		t.Fatalf("second Config AuthAudiences = %#v, want detached original audience", again.AuthAudiences)
	}
}

func TestRuntimeStartStrictAuthWithoutSigningKeyFails(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), AuthMode: AuthModeStrict})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	err = rt.Start(context.Background())
	if !errors.Is(err, ErrAuthSigningKeyRequired) {
		t.Fatalf("Start error = %v, want ErrAuthSigningKeyRequired", err)
	}
	if rt.Ready() {
		t.Fatal("runtime ready after strict-auth startup failure")
	}
}

func TestHTTPHandlerReturnsServiceUnavailableBeforeStart(t *testing.T) {
	rt := buildValidTestRuntime(t)
	req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
	rec := httptest.NewRecorder()

	rt.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if rt.Ready() {
		t.Fatal("HTTPHandler started lifecycle; want composable handler only")
	}
}

func TestHTTPHandlerRoutesSubscribeAfterStart(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code == http.StatusServiceUnavailable {
		t.Fatal("handler still gated after Start")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want protocol HTTP rejection 400", rec.Code)
	}
	if rt.protocolServer == nil || rt.protocolConns == nil || rt.protocolInbox == nil {
		t.Fatal("protocol graph was not initialized")
	}
}

func TestHTTPHandlerReturnsServiceUnavailableAfterClose(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
	rec := httptest.NewRecorder()
	rt.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestListenAndServeStartsRuntimeAndStopsOnContextCancel(t *testing.T) {
	rt := buildValidTestRuntime(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- rt.serve(ctx, ln) }()

	eventually(t, func() bool { return rt.Ready() })
	cancel()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit after context cancellation")
	}
	if rt.Ready() {
		t.Fatal("runtime still ready after serve cancellation")
	}
}

func TestListenAndServeDuplicateCallReturnsRuntimeServing(t *testing.T) {
	addr := reserveRuntimeListenAddr(t)
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir(), ListenAddr: addr})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- rt.ListenAndServe(ctx) }()

	eventually(t, func() bool {
		rt.mu.Lock()
		serving := rt.serving
		rt.mu.Unlock()
		return serving
	})

	err = rt.ListenAndServe(context.Background())
	if !errors.Is(err, ErrRuntimeServing) {
		t.Fatalf("duplicate ListenAndServe error = %v, want ErrRuntimeServing", err)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("first ListenAndServe returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first ListenAndServe did not exit after cancellation")
	}
}

func TestListenAndServeAfterClosePreservesClosedError(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	err = rt.serve(context.Background(), ln)
	if !errors.Is(err, ErrRuntimeClosed) {
		t.Fatalf("serve after Close error = %v, want ErrRuntimeClosed", err)
	}
}

func TestRuntimeNetworkReplacesNoopFanOutSender(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	sender, ok := rt.fanOutSender.(*swappableFanOutSender)
	if !ok {
		t.Fatalf("fanOutSender = %T, want *swappableFanOutSender", rt.fanOutSender)
	}
	if _, ok := sender.Target().(noopFanOutSender); ok {
		t.Fatal("fan-out sender still points at noop after Start/protocol wiring")
	}
	if _, ok := sender.Target().(*protocol.FanOutSenderAdapter); !ok {
		t.Fatalf("fan-out sender target = %T, want protocol-backed adapter", sender.Target())
	}
}

func TestRuntimeCloseClearsProtocolGraphBeforeExecutorResources(t *testing.T) {
	rt := buildValidTestRuntime(t)
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if rt.protocolConns == nil || rt.protocolInbox == nil || rt.executor == nil {
		t.Fatal("protocol/executor resources missing before Close")
	}

	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if rt.protocolConns != nil || rt.protocolInbox != nil || rt.protocolServer != nil || rt.protocolSender != nil || rt.fanOutSender != nil {
		t.Fatalf("protocol resources not cleared after Close")
	}
	if rt.executor != nil || rt.durability != nil || rt.scheduler != nil {
		t.Fatalf("lifecycle resources not cleared after Close")
	}
}

func reserveRuntimeListenAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-timeout.C:
			t.Fatal("condition was not met before deadline")
		case <-ticker.C:
		}
	}
}
