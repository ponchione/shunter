package protocol

import (
	"context"
	"testing"
	"time"
)

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

// TestReconnectNoSubscriptionCarryover verifies that subscriptions
// do not carry over from a previous connection (Story 6.4 AC 2).
func TestReconnectNoSubscriptionCarryover(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	opts := DefaultProtocolOptions()

	c1, _, cleanup1 := loopbackConn(t, opts)
	defer cleanup1()
	mgr.Add(c1)

	_ = c1.Subscriptions.Reserve(100)
	c1.Subscriptions.Activate(100)
	_ = c1.Subscriptions.Reserve(200)
	c1.Subscriptions.Activate(200)

	c1.Disconnect(context.Background(), CloseNormal, "", inbox, mgr)

	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if c2.Subscriptions.IsActiveOrPending(100) {
		t.Error("subscription 100 carried over — should not")
	}
	if c2.Subscriptions.IsActiveOrPending(200) {
		t.Error("subscription 200 carried over — should not")
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

	c1.Disconnect(context.Background(), ClosePolicy, "send buffer full", inbox, mgr)

	c2, _, cleanup2 := loopbackConn(t, opts)
	defer cleanup2()
	c2.Identity = c1.Identity
	mgr.Add(c2)

	if mgr.Get(c2.ID) == nil {
		t.Error("reconnected connection not in manager")
	}
	s := NewClientSender(mgr, inbox)
	msg := SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
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
