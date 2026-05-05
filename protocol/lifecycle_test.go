package protocol

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/types"
)

// fakeInbox is a test double for ExecutorInbox. It records each
// lifecycle call and returns a configurable outcome.
type fakeInbox struct {
	mu sync.Mutex

	// OnConnect knobs + observations.
	onConnectErr error
	calls        int
	gotConnID    types.ConnectionID
	gotIdentity  types.Identity

	// Disconnect knobs + observations (Story 3.6).
	onDisconnectErr     error
	disconnectSubsErr   error
	disconnectCalls     int
	disconnectSubsCalls int
	// events records the order in which disconnect methods fired,
	// so tests can assert "subs removed BEFORE OnDisconnect".
	events []string
}

func (f *fakeInbox) OnConnect(_ context.Context, connID types.ConnectionID, identity types.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotConnID = connID
	f.gotIdentity = identity
	return f.onConnectErr
}

func (f *fakeInbox) OnDisconnect(_ context.Context, connID types.ConnectionID, _ types.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnectCalls++
	f.events = append(f.events, "OnDisconnect")
	_ = connID
	return f.onDisconnectErr
}

func (f *fakeInbox) DisconnectClientSubscriptions(_ context.Context, connID types.ConnectionID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnectSubsCalls++
	f.events = append(f.events, "DisconnectClientSubscriptions")
	_ = connID
	return f.disconnectSubsErr
}

func (f *fakeInbox) RegisterSubscriptionSet(_ context.Context, _ RegisterSubscriptionSetRequest) error {
	return nil
}

func (f *fakeInbox) UnregisterSubscriptionSet(_ context.Context, _ UnregisterSubscriptionSetRequest) error {
	return nil
}

func (f *fakeInbox) CallReducer(_ context.Context, _ CallReducerRequest) error {
	return nil
}

func (f *fakeInbox) snapshot() (int, types.ConnectionID, types.Identity) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.gotConnID, f.gotIdentity
}

func (f *fakeInbox) disconnectSnapshot() (onDis, onSubs int, events []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.disconnectCalls, f.disconnectSubsCalls, append([]string{}, f.events...)
}

// lifecycleServer returns a Server configured for strict auth + the
// default-Upgraded lifecycle path (executor + conn manager wired).
func lifecycleServer(t *testing.T, inbox *fakeInbox) (*Server, *ConnManager) {
	t.Helper()
	mgr := NewConnManager()
	return &Server{
		JWT: &auth.JWTConfig{
			SigningKey: testSigningKey,
			AuthMode:   auth.AuthModeStrict,
		},
		Options:  DefaultProtocolOptions(),
		Executor: inbox,
		Conns:    mgr,
	}, mgr
}

// dialSubscribe opens a v1.bsatn.shunter WebSocket against srv's
// /subscribe endpoint with a valid bearer token.
func dialSubscribe(t *testing.T, srv *httptest.Server) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	u := strings.Replace(srv.URL, "http://", "ws://", 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{SubprotocolV1},
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + mintValidToken(t)}},
	})
}

// readOneBinary reads a single WebSocket message and asserts it is
// binary. Errors surface close frames via websocket.CloseError.
func readOneBinary(t *testing.T, c *websocket.Conn, timeout time.Duration) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	mt, data, err := c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if mt != websocket.MessageBinary {
		return nil, fmt.Errorf("got message type %v, want MessageBinary", mt)
	}
	return data, nil
}

func TestTruncateCloseReasonPreservesUTF8(t *testing.T) {
	reason := strings.Repeat("\u00e9", 61)
	got := truncateCloseReason(reason)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated close reason is not valid UTF-8: %q", got)
	}
	if got != strings.Repeat("\u00e9", 60) {
		t.Fatalf("truncated close reason = %q, want 60 complete two-byte runes", got)
	}
	if len(got) > 120 {
		t.Fatalf("truncated close reason length = %d, want <= 120", len(got))
	}
}

func TestTruncateCloseReasonDropsInvalidUTF8(t *testing.T) {
	reason := "prefix" + string([]byte{0xff}) + strings.Repeat("a", 140)
	got := truncateCloseReason(reason)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated close reason is not valid UTF-8: %q", got)
	}
	if strings.Contains(got, "\ufffd") {
		t.Fatalf("truncated close reason should drop invalid bytes, got %q", got)
	}
	if len(got) > 120 {
		t.Fatalf("truncated close reason length = %d, want <= 120", len(got))
	}
}

func TestRunLifecycleSuccessSendsIdentityToken(t *testing.T) {
	inbox := &fakeInbox{}
	s, mgr := lifecycleServer(t, inbox)
	srv := newTestServer(t, s)

	c, resp, err := dialSubscribe(t, srv)
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %d, want 101", resp.StatusCode)
	}

	data, err := readOneBinary(t, c, 2*time.Second)
	if err != nil {
		t.Fatalf("read IdentityToken: %v", err)
	}
	tag, msg, err := DecodeServerMessage(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tag != TagIdentityToken {
		t.Fatalf("tag = %d, want %d", tag, TagIdentityToken)
	}
	ic, ok := msg.(IdentityToken)
	if !ok {
		t.Fatalf("decoded type = %T, want IdentityToken", msg)
	}

	expectedIdentity := auth.DeriveIdentity("test-issuer", "alice")
	if ic.Identity != expectedIdentity {
		t.Errorf("Identity = %x, want %x", ic.Identity, expectedIdentity)
	}
	if (ic.ConnectionID == types.ConnectionID{}) {
		t.Error("ConnectionID is zero")
	}

	// OnConnect was called exactly once with matching fields.
	calls, gotConnID, gotIdentity := inbox.snapshot()
	if calls != 1 {
		t.Errorf("OnConnect calls = %d, want 1", calls)
	}
	if gotConnID != ic.ConnectionID {
		t.Errorf("OnConnect connID = %x, want %x", gotConnID, ic.ConnectionID)
	}
	if gotIdentity != expectedIdentity {
		t.Errorf("OnConnect identity = %x, want %x", gotIdentity, expectedIdentity)
	}

	// Connection is registered in ConnManager.
	if mgr.Get(ic.ConnectionID) == nil {
		t.Error("ConnManager has no entry for admitted connection")
	}
}

func TestRunLifecycleRejectsLiveDuplicateConnectionIDBeforeOnConnect(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	if err := c1.RunLifecycle(context.Background(), inbox, mgr); err != nil {
		t.Fatalf("first RunLifecycle: %v", err)
	}

	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.ID = c1.ID
	err := c2.RunLifecycle(context.Background(), inbox, mgr)
	if !errors.Is(err, ErrConnectionIDInUse) {
		t.Fatalf("duplicate RunLifecycle error = %v, want ErrConnectionIDInUse", err)
	}
	if got := mgr.Get(c1.ID); got != c1 {
		t.Fatalf("manager entry = %p, want first conn %p", got, c1)
	}
	calls, _, _ := inbox.snapshot()
	if calls != 1 {
		t.Fatalf("OnConnect calls = %d, want only the first connection admitted", calls)
	}
}

func TestRunLifecycleIdentityTokenWriteFailureRunsDisconnect(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()
	conn, _, cleanup := loopbackConn(t, opts)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := conn.RunLifecycle(ctx, inbox, mgr)
	if err == nil || !strings.Contains(err.Error(), "write IdentityToken") {
		t.Fatalf("RunLifecycle error = %v, want write IdentityToken failure", err)
	}
	calls, gotConnID, _ := inbox.snapshot()
	if calls != 1 {
		t.Fatalf("OnConnect calls = %d, want 1", calls)
	}
	if gotConnID != conn.ID {
		t.Fatalf("OnConnect connID = %x, want %x", gotConnID, conn.ID)
	}
	onDisconnect, disconnectSubs, events := inbox.disconnectSnapshot()
	if disconnectSubs != 1 || onDisconnect != 1 {
		t.Fatalf("disconnect calls = (subs=%d,onDisconnect=%d), want (1,1)", disconnectSubs, onDisconnect)
	}
	wantEvents := []string{"DisconnectClientSubscriptions", "OnDisconnect"}
	if fmt.Sprint(events) != fmt.Sprint(wantEvents) {
		t.Fatalf("disconnect event order = %v, want %v", events, wantEvents)
	}
	if got := mgr.Get(conn.ID); got != nil {
		t.Fatalf("manager entry after write failure = %p, want nil", got)
	}
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("conn.closed was not signaled after write failure cleanup")
	}
}

func TestRunLifecycleZeroServerOptionsUseDefaults(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	s := &Server{
		JWT: &auth.JWTConfig{
			SigningKey: testSigningKey,
			AuthMode:   auth.AuthModeStrict,
		},
		Executor: inbox,
		Conns:    mgr,
	}
	srv := newTestServer(t, s)

	c, resp, err := dialSubscribe(t, srv)
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	data, err := readOneBinary(t, c, 2*time.Second)
	if err != nil {
		t.Fatalf("read IdentityToken: %v", err)
	}
	_, msg, err := DecodeServerMessage(data)
	if err != nil {
		t.Fatalf("decode IdentityToken: %v", err)
	}
	ic := msg.(IdentityToken)
	serverConn := mgr.Get(ic.ConnectionID)
	if serverConn == nil {
		t.Fatal("admitted connection missing from manager")
	}
	defaults := DefaultProtocolOptions()
	if serverConn.opts.PingInterval != defaults.PingInterval {
		t.Fatalf("PingInterval = %v, want default %v", serverConn.opts.PingInterval, defaults.PingInterval)
	}
	if cap(serverConn.OutboundCh) != defaults.OutgoingBufferMessages {
		t.Fatalf("OutboundCh cap = %d, want default %d", cap(serverConn.OutboundCh), defaults.OutgoingBufferMessages)
	}
	if cap(serverConn.inflightSem) != defaults.IncomingQueueMessages {
		t.Fatalf("inflightSem cap = %d, want default %d", cap(serverConn.inflightSem), defaults.IncomingQueueMessages)
	}
}

func TestRunLifecycleOnConnectRejectClosesWith1008(t *testing.T) {
	rejectErr := errors.New("admission denied by policy")
	inbox := &fakeInbox{onConnectErr: rejectErr}
	s, mgr := lifecycleServer(t, inbox)
	srv := newTestServer(t, s)

	c, resp, err := dialSubscribe(t, srv)
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %d, want 101 (upgrade precedes executor hook)", resp.StatusCode)
	}

	// The first and only frame the client observes must be the close.
	_, err = readOneBinary(t, c, 2*time.Second)
	if err == nil {
		t.Fatal("expected close frame; got a data frame instead — IdentityToken must not be sent on OnConnect failure")
	}
	code := websocket.CloseStatus(err)
	if code != websocket.StatusPolicyViolation {
		t.Errorf("close code = %d, want %d (StatusPolicyViolation)", code, websocket.StatusPolicyViolation)
	}

	// Rejected connection must not appear in ConnManager.
	calls, gotConnID, _ := inbox.snapshot()
	if calls != 1 {
		t.Errorf("OnConnect calls = %d, want 1", calls)
	}
	if mgr.Get(gotConnID) != nil {
		t.Error("ConnManager must not hold rejected connection")
	}
}

func TestRunLifecycleNoExecutorNoConnsClosesCleanly(t *testing.T) {
	// Executor=nil path still closes the upgraded socket so the client
	// does not hang. Preserves pre-3.4 Server.HandleSubscribe behavior
	// when the embedder is still bringing up the executor.
	s := &Server{
		JWT: &auth.JWTConfig{
			SigningKey: testSigningKey,
			AuthMode:   auth.AuthModeStrict,
		},
		Options: DefaultProtocolOptions(),
	}
	srv := newTestServer(t, s)
	c, _, err := dialSubscribe(t, srv)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	_, err = readOneBinary(t, c, 1*time.Second)
	if err == nil {
		t.Fatal("expected close frame when no executor is wired")
	}
	if got := websocket.CloseStatus(err); got != websocket.StatusNormalClosure {
		t.Errorf("close code = %d, want %d (StatusNormalClosure)", got, websocket.StatusNormalClosure)
	}
}
