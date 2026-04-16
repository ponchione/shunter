package protocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/golang-jwt/jwt/v5"

	"github.com/ponchione/shunter/auth"
)

var testSigningKey = []byte("upgrade-test-key")

func newTestServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(s.HandleSubscribe))
	t.Cleanup(srv.Close)
	return srv
}

func mintValidToken(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "alice",
		"iss": "test-issuer",
		"iat": time.Now().Unix(),
	})
	s, err := tok.SignedString(testSigningKey)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// strictServer returns a Server configured for strict auth. Upgraded
// callback signals when a connection has been handed off so tests can
// assert it happened (or did not).
func strictServer(t *testing.T) (*Server, *upgradeRecorder) {
	t.Helper()
	rec := &upgradeRecorder{}
	return &Server{
		JWT: &auth.JWTConfig{
			SigningKey: testSigningKey,
			AuthMode:   auth.AuthModeStrict,
		},
		Options:  DefaultProtocolOptions(),
		Upgraded: rec.record,
	}, rec
}

// anonymousServer returns a Server configured for anonymous-mode mint.
func anonymousServer(t *testing.T) (*Server, *upgradeRecorder) {
	t.Helper()
	rec := &upgradeRecorder{}
	return &Server{
		JWT: &auth.JWTConfig{
			SigningKey: testSigningKey,
			AuthMode:   auth.AuthModeAnonymous,
		},
		Mint: &auth.MintConfig{
			Issuer:     "https://shunter.local/anonymous",
			Audience:   "shunter-local",
			SigningKey: testSigningKey,
			Expiry:     time.Hour,
		},
		Options:  DefaultProtocolOptions(),
		Upgraded: rec.record,
	}, rec
}

type upgradeRecorder struct {
	mu   sync.Mutex
	seen []UpgradeContext
}

func (r *upgradeRecorder) record(ctx context.Context, uc *UpgradeContext) {
	r.mu.Lock()
	r.seen = append(r.seen, *uc)
	r.mu.Unlock()
	// Tests don't exercise the upgraded-conn read/write path here —
	// close cleanly to let the server-side goroutine exit.
	_ = uc.Conn.Close(websocket.StatusNormalClosure, "")
}

func (r *upgradeRecorder) last() (UpgradeContext, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.seen) == 0 {
		return UpgradeContext{}, false
	}
	return r.seen[len(r.seen)-1], true
}

// dialWS opens a ws:// upgrade against httptestServer's /subscribe
// endpoint with the given header + query modifications applied.
func dialWS(t *testing.T, srv *httptest.Server, opts wsDialOpts) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	u := strings.Replace(srv.URL, "http://", "ws://", 1)
	if opts.query != "" {
		u += "?" + opts.query
	}
	dialOpts := &websocket.DialOptions{
		Subprotocols: opts.subprotocols,
	}
	if opts.authHeader != "" {
		dialOpts.HTTPHeader = http.Header{"Authorization": []string{opts.authHeader}}
	}
	if opts.skipSubprotocol {
		dialOpts.Subprotocols = nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return websocket.Dial(ctx, u, dialOpts)
}

type wsDialOpts struct {
	authHeader      string
	query           string
	subprotocols    []string
	skipSubprotocol bool
}

func TestUpgradeValidTokenHeaderSucceeds(t *testing.T) {
	s, rec := strictServer(t)
	srv := newTestServer(t, s)

	conn, resp, err := dialWS(t, srv, wsDialOpts{
		authHeader:   "Bearer " + mintValidToken(t),
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err != nil {
		t.Fatalf("dial failed: %v (resp=%v)", err, resp)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want 101", resp.StatusCode)
	}
	if got := resp.Header.Get("Sec-WebSocket-Protocol"); got != "v1.bsatn.shunter" {
		t.Errorf("Sec-WebSocket-Protocol echoed = %q, want v1.bsatn.shunter", got)
	}

	// Give the server handler time to invoke Upgraded and close.
	time.Sleep(50 * time.Millisecond)
	uc, ok := rec.last()
	if !ok {
		t.Fatal("Upgraded callback not invoked")
	}
	if uc.Identity != auth.DeriveIdentity("test-issuer", "alice") {
		t.Errorf("Identity mismatch: got %x", uc.Identity)
	}
}

func TestUpgradeValidTokenQueryParamSucceeds(t *testing.T) {
	s, _ := strictServer(t)
	srv := newTestServer(t, s)

	conn, resp, err := dialWS(t, srv, wsDialOpts{
		query:        "token=" + mintValidToken(t),
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want 101", resp.StatusCode)
	}
}

func TestUpgradeStrictNoTokenRejected(t *testing.T) {
	s, _ := strictServer(t)
	srv := newTestServer(t, s)

	_, resp, err := dialWS(t, srv, wsDialOpts{
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err == nil {
		t.Fatal("dial should fail without token in strict mode")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp)
	}
}

func TestUpgradeInvalidTokenRejected(t *testing.T) {
	s, _ := strictServer(t)
	srv := newTestServer(t, s)

	_, resp, err := dialWS(t, srv, wsDialOpts{
		authHeader:   "Bearer not-a-jwt",
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err == nil {
		t.Fatal("dial should fail with invalid token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %v, want 401", resp)
	}
}

func TestUpgradeAnonymousNoTokenMints(t *testing.T) {
	s, rec := anonymousServer(t)
	srv := newTestServer(t, s)

	conn, resp, err := dialWS(t, srv, wsDialOpts{
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err != nil {
		t.Fatalf("anonymous dial: %v (resp=%v)", err, resp)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want 101", resp.StatusCode)
	}

	time.Sleep(50 * time.Millisecond)
	uc, ok := rec.last()
	if !ok {
		t.Fatal("Upgraded callback not invoked for anonymous mint")
	}
	if uc.Token == "" {
		t.Error("anonymous upgrade should produce a minted token in UpgradeContext.Token")
	}
	if uc.Identity.IsZero() {
		t.Error("anonymous identity must be non-zero")
	}
}

func TestUpgradeZeroConnectionIDRejected(t *testing.T) {
	s, _ := strictServer(t)
	srv := newTestServer(t, s)

	_, resp, err := dialWS(t, srv, wsDialOpts{
		authHeader:   "Bearer " + mintValidToken(t),
		query:        "connection_id=00000000000000000000000000000000",
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err == nil {
		t.Fatal("dial should fail with zero connection_id")
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %v, want 400", resp)
	}
}

func TestUpgradeGeneratesConnectionIDWhenAbsent(t *testing.T) {
	s, rec := strictServer(t)
	srv := newTestServer(t, s)

	conn, _, err := dialWS(t, srv, wsDialOpts{
		authHeader:   "Bearer " + mintValidToken(t),
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	time.Sleep(50 * time.Millisecond)
	uc, ok := rec.last()
	if !ok {
		t.Fatal("no upgrade record")
	}
	if uc.ConnectionID.IsZero() {
		t.Error("server should have generated a non-zero connection_id")
	}
}

func TestUpgradeClientSuppliedConnectionIDUsed(t *testing.T) {
	s, rec := strictServer(t)
	srv := newTestServer(t, s)

	clientID := "0102030405060708090a0b0c0d0e0f10"
	conn, _, err := dialWS(t, srv, wsDialOpts{
		authHeader:   "Bearer " + mintValidToken(t),
		query:        "connection_id=" + clientID,
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	time.Sleep(50 * time.Millisecond)
	uc, ok := rec.last()
	if !ok {
		t.Fatal("no upgrade record")
	}
	if uc.ConnectionID.Hex() != clientID {
		t.Errorf("ConnectionID.Hex = %q, want %q", uc.ConnectionID.Hex(), clientID)
	}
}

func TestUpgradeMissingSubprotocolRejected(t *testing.T) {
	s, _ := strictServer(t)
	srv := newTestServer(t, s)

	_, resp, err := dialWS(t, srv, wsDialOpts{
		authHeader:      "Bearer " + mintValidToken(t),
		skipSubprotocol: true,
	})
	if err == nil {
		t.Fatal("dial should fail without Sec-WebSocket-Protocol")
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %v, want 400", resp)
	}
}

func TestUpgradeCompressionGzip(t *testing.T) {
	s, rec := strictServer(t)
	srv := newTestServer(t, s)

	conn, _, err := dialWS(t, srv, wsDialOpts{
		authHeader:   "Bearer " + mintValidToken(t),
		query:        "compression=gzip",
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	time.Sleep(50 * time.Millisecond)
	uc, _ := rec.last()
	if uc.Compression != CompressionGzip {
		t.Errorf("Compression = %d, want CompressionGzip", uc.Compression)
	}
}

func TestUpgradeCompressionNone(t *testing.T) {
	s, rec := strictServer(t)
	srv := newTestServer(t, s)

	conn, _, err := dialWS(t, srv, wsDialOpts{
		authHeader:   "Bearer " + mintValidToken(t),
		query:        "compression=none",
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	time.Sleep(50 * time.Millisecond)
	uc, _ := rec.last()
	if uc.Compression != CompressionNone {
		t.Errorf("Compression = %d, want CompressionNone", uc.Compression)
	}
}

func TestUpgradeCompressionUnknownRejected(t *testing.T) {
	s, _ := strictServer(t)
	srv := newTestServer(t, s)

	_, resp, err := dialWS(t, srv, wsDialOpts{
		authHeader:   "Bearer " + mintValidToken(t),
		query:        "compression=zstd",
		subprotocols: []string{"v1.bsatn.shunter"},
	})
	if err == nil {
		t.Fatal("dial should fail with unknown compression mode")
	}
	if resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %v, want 400", resp)
	}
}

func TestBuildMessageHandlers_NilWhenDependenciesMissing(t *testing.T) {
	s := &Server{}
	h := s.buildMessageHandlers()
	if h.OnSubscribe != nil {
		t.Fatal("OnSubscribe should be nil when schema/executor are missing")
	}
	if h.OnUnsubscribe != nil {
		t.Fatal("OnUnsubscribe should be nil when executor is missing")
	}
	if h.OnCallReducer != nil {
		t.Fatal("OnCallReducer should be nil when executor is missing")
	}
	if h.OnOneOffQuery != nil {
		t.Fatal("OnOneOffQuery should be nil when schema/state are missing")
	}
}

func TestBuildMessageHandlers_WiresOnlySatisfiedDependencies(t *testing.T) {
	s := &Server{Executor: &fakeInbox{}}
	h := s.buildMessageHandlers()
	if h.OnSubscribe != nil {
		t.Fatal("OnSubscribe should stay nil until schema is wired")
	}
	if h.OnUnsubscribe == nil {
		t.Fatal("OnUnsubscribe should be wired when executor is present")
	}
	if h.OnCallReducer == nil {
		t.Fatal("OnCallReducer should be wired when executor is present")
	}
	if h.OnOneOffQuery != nil {
		t.Fatal("OnOneOffQuery should stay nil until schema and state are both wired")
	}
}
