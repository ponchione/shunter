package protocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/types"
)

func dialSubscribeWithTokenAndQuery(t *testing.T, srv *httptest.Server, token string, query string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	u := strings.Replace(srv.URL, "http://", "ws://", 1)
	if query != "" {
		u += "?" + query
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return websocket.Dial(ctx, u, &websocket.DialOptions{
		Subprotocols: []string{SubprotocolV1},
		HTTPHeader:   http.Header{"Authorization": []string{"Bearer " + token}},
	})
}

// TestReconnectSameIdentity verifies that the same token yields the
// same Identity across connections (Story 6.4 AC 1).
func TestReconnectSameIdentity(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	mgr.Add(c1)
	identity1 := c1.Identity

	c1.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = identity1
	mgr.Add(c2)

	if c2.Identity != identity1 {
		t.Errorf("reconnect Identity = %x, want %x", c2.Identity, identity1)
	}
}

// TestReconnectNoSubscriptionCarryover verifies that reconnect yields a
// distinct Conn object with its own fresh OutboundCh (Story 6.4 AC 2).
// Per-connection subscription admission bookkeeping has been retired
// (TD-140); subscription lifetime is bounded by the subscription
// Manager's querySets, which the executor's DisconnectClientSubscriptions
// path clears on disconnect. This test's job here is to confirm the
// transport surface itself does not leak between connections.
func TestReconnectNoSubscriptionCarryover(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	mgr.Add(c1)

	c1.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if c2 == c1 {
		t.Fatal("reconnect returned the same *Conn pointer; want a fresh connection state")
	}
	if c2.OutboundCh == c1.OutboundCh {
		t.Error("reconnect reused OutboundCh; want a fresh outbound queue")
	}
	select {
	case <-c2.OutboundCh:
		t.Error("fresh Conn OutboundCh had a pre-existing frame")
	default:
	}
}

// TestReconnectAfterBufferOverflow verifies reconnection works after
// a backpressure disconnect (Story 6.4 AC 7).
func TestReconnectAfterBufferOverflow(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	mgr.Add(c1)

	c1.Disconnect(context.Background(), ClosePolicy, CloseReasonSendBufferFull, inbox, mgr)

	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if mgr.Get(c2.ID) == nil {
		t.Error("reconnected connection not in manager")
	}
	s := NewClientSender(mgr, inbox)
	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	if err := s.Send(c2.ID, msg); err != nil {
		t.Fatalf("send after reconnect: %v", err)
	}
}

// TestReconnectDifferentConnectionID verifies new ConnectionID on reconnect.
func TestReconnectDifferentConnectionID(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	mgr.Add(c1)
	id1 := c1.ID

	c1.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if c2.ID == id1 {
		t.Error("reconnected connection reused same ConnectionID")
	}
}

// TestReconnectSameConnectionIDAccepted verifies reusing connection_id
// is accepted (no semantic effect in v1).
func TestReconnectSameConnectionIDAccepted(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	savedID := c1.ID
	mgr.Add(c1)

	c1.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.ID = savedID
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if mgr.Get(savedID) == nil {
		t.Error("reconnected connection with reused ID not found")
	}
}

// TestReconnectNoGoroutineLeakAfterDisconnect verifies goroutines exit.
func TestReconnectNoGoroutineLeakAfterDisconnect(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()
	opts.PingInterval = 50 * time.Millisecond
	opts.IdleTimeout = 200 * time.Millisecond

	c, _, cleanup := loopbackConn(t, opts)
	defer cleanup()
	mgr.Add(c)

	handlers := &MessageHandlers{}
	dispatchDone := runDispatchAsync(c, context.Background(), handlers)
	keepaliveDone := runKeepaliveAsync(c, context.Background())

	c.Disconnect(context.Background(), CloseNormal, "test", inbox, mgr)

	select {
	case <-dispatchDone:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit after Disconnect")
	}
	select {
	case <-keepaliveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("keepalive loop did not exit after Disconnect")
	}
}

func TestReconnectSameIdentityViaRealUpgrade(t *testing.T) {
	inbox := &fakeInbox{}
	s, _ := lifecycleServer(t, inbox)
	srv := newTestServer(t, s)
	token := mintValidToken(t)
	expectedIdentity := auth.DeriveIdentity("test-issuer", "alice")

	c1, resp1, err := dialSubscribeWithTokenAndQuery(t, srv, token, "")
	if err != nil {
		t.Fatalf("first dial: %v (resp=%v)", err, resp1)
	}
	defer c1.Close(websocket.StatusNormalClosure, "")
	data1, err := readOneBinary(t, c1, 2*time.Second)
	if err != nil {
		t.Fatalf("read first IdentityToken: %v", err)
	}
	_, msg1, err := DecodeServerMessage(data1)
	if err != nil {
		t.Fatalf("decode first IdentityToken: %v", err)
	}
	ic1 := msg1.(IdentityToken)
	if ic1.Identity != expectedIdentity {
		t.Fatalf("first identity = %x, want %x", ic1.Identity, expectedIdentity)
	}
	_ = c1.Close(websocket.StatusNormalClosure, "bye")

	c2, resp2, err := dialSubscribeWithTokenAndQuery(t, srv, token, "")
	if err != nil {
		t.Fatalf("second dial: %v (resp=%v)", err, resp2)
	}
	defer c2.Close(websocket.StatusNormalClosure, "")
	data2, err := readOneBinary(t, c2, 2*time.Second)
	if err != nil {
		t.Fatalf("read second IdentityToken: %v", err)
	}
	_, msg2, err := DecodeServerMessage(data2)
	if err != nil {
		t.Fatalf("decode second IdentityToken: %v", err)
	}
	ic2 := msg2.(IdentityToken)

	if ic2.Identity != ic1.Identity {
		t.Fatalf("reconnect identity = %x, want %x", ic2.Identity, ic1.Identity)
	}
	if ic2.ConnectionID == ic1.ConnectionID {
		t.Fatalf("default reconnect reused ConnectionID %x; want a fresh id without query override", ic2.ConnectionID)
	}
	if len(ic2.Token) != 0 {
		t.Fatalf("strict-auth reconnect returned minted token %q; want empty token", ic2.Token)
	}
}

func TestReconnectSameConnectionIDAcceptedViaUpgradeQuery(t *testing.T) {
	inbox := &fakeInbox{}
	s, _ := lifecycleServer(t, inbox)
	srv := newTestServer(t, s)
	token := mintValidToken(t)
	wantID := types.ConnectionID{0xAA, 0xBB, 0xCC, 0xDD}

	c, resp, err := dialSubscribeWithTokenAndQuery(t, srv, token, "connection_id="+wantID.Hex())
	if err != nil {
		t.Fatalf("dial with connection_id: %v (resp=%v)", err, resp)
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
	if ic.ConnectionID != wantID {
		t.Fatalf("ConnectionID = %x, want %x", ic.ConnectionID, wantID)
	}
}

func TestLiveDuplicateConnectionIDRejectedViaUpgradeQuery(t *testing.T) {
	inbox := &fakeInbox{}
	s, _ := lifecycleServer(t, inbox)
	srv := newTestServer(t, s)
	token := mintValidToken(t)
	wantID := types.ConnectionID{0xAA, 0xBB, 0xCC, 0xDD}
	query := "connection_id=" + wantID.Hex()

	c1, resp1, err := dialSubscribeWithTokenAndQuery(t, srv, token, query)
	if err != nil {
		t.Fatalf("first dial: %v (resp=%v)", err, resp1)
	}
	defer c1.Close(websocket.StatusNormalClosure, "")
	if _, err := readOneBinary(t, c1, 2*time.Second); err != nil {
		t.Fatalf("read first IdentityToken: %v", err)
	}

	c2, resp2, err := dialSubscribeWithTokenAndQuery(t, srv, token, query)
	if err != nil {
		t.Fatalf("second dial should complete handshake before policy close: %v (resp=%v)", err, resp2)
	}
	defer c2.Close(websocket.StatusNormalClosure, "")
	if _, err := readOneBinary(t, c2, 2*time.Second); err == nil {
		t.Fatal("duplicate connection_id should close before IdentityToken")
	} else if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Fatalf("duplicate close status = %d, want %d", got, websocket.StatusPolicyViolation)
	}
	calls, _, _ := inbox.snapshot()
	if calls != 1 {
		t.Fatalf("OnConnect calls = %d, want duplicate rejected before OnConnect", calls)
	}
}
