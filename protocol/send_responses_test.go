package protocol

import (
	"testing"
)

func TestSendSubscribeAppliedActivatesSubscription(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(10)

	msg := &SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	if err := SendSubscribeApplied(s, c, msg); err != nil {
		t.Fatal(err)
	}

	if !c.Subscriptions.IsActive(10) {
		t.Fatal("subscription 10 should be active")
	}

	select {
	case <-c.OutboundCh:
	default:
		t.Fatal("no frame enqueued")
	}
}

func TestSendSubscribeAppliedDiscardsAfterDisconnect(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(10)
	c.Subscriptions.Remove(10)

	msg := &SubscribeApplied{RequestID: 1, SubscriptionID: 10, TableName: "t", Rows: []byte{}}
	err := SendSubscribeApplied(s, c, msg)
	if err != nil {
		t.Fatal("should silently discard, not error")
	}
	select {
	case <-c.OutboundCh:
		t.Fatal("frame should not be enqueued for removed subscription")
	default:
	}
}

func TestSendUnsubscribeAppliedRemovesSubscription(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(10)
	c.Subscriptions.Activate(10)

	msg := &UnsubscribeApplied{RequestID: 1, SubscriptionID: 10}
	if err := SendUnsubscribeApplied(s, c, msg); err != nil {
		t.Fatal(err)
	}

	if c.Subscriptions.IsActiveOrPending(10) {
		t.Fatal("subscription 10 should be removed")
	}
}

func TestSendSubscriptionErrorReleasesID(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(10)

	msg := &SubscriptionError{RequestID: 1, SubscriptionID: 10, Error: "bad predicate"}
	if err := SendSubscriptionError(s, c, msg); err != nil {
		t.Fatal(err)
	}

	if c.Subscriptions.IsActiveOrPending(10) {
		t.Fatal("subscription 10 should be released")
	}

	if err := c.Subscriptions.Reserve(10); err != nil {
		t.Fatalf("subscription 10 should be reusable: %v", err)
	}
}

func TestSendOneOffQueryResult(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	msg := &OneOffQueryResult{RequestID: 7, Status: 0, Rows: []byte{0x01}}
	if err := SendOneOffQueryResult(s, id, msg); err != nil {
		t.Fatal(err)
	}

	frame := <-c.OutboundCh
	_, decoded, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := decoded.(OneOffQueryResult)
	if !ok {
		t.Fatalf("expected OneOffQueryResult, got %T", decoded)
	}
	if result.RequestID != 7 {
		t.Fatalf("RequestID = %d, want 7", result.RequestID)
	}
}
