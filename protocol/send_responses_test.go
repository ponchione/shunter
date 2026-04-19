package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

type assertingSender struct {
	sendFn func(any) error
}

func (s assertingSender) Send(_ types.ConnectionID, msg any) error {
	if s.sendFn != nil {
		return s.sendFn(msg)
	}
	return nil
}

func (s assertingSender) SendTransactionUpdate(_ types.ConnectionID, _ *TransactionUpdate) error {
	return nil
}

func (s assertingSender) SendTransactionUpdateLight(_ types.ConnectionID, _ *TransactionUpdateLight) error {
	return nil
}

func TestSendSubscribeSingleAppliedActivatesSubscription(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	c.Subscriptions.Reserve(10)

	msg := &SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	if err := SendSubscribeSingleApplied(s, c, msg); err != nil {
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

func TestSendSubscribeSingleAppliedActivatesBeforeSend(t *testing.T) {
	c, _ := testConn(false)
	c.Subscriptions.Reserve(10)

	sender := assertingSender{
		sendFn: func(msg any) error {
			if _, ok := msg.(SubscribeSingleApplied); !ok {
				t.Fatalf("expected SubscribeSingleApplied, got %T", msg)
			}
			if !c.Subscriptions.IsActive(10) {
				t.Fatal("subscription should be active before send")
			}
			return nil
		},
	}

	msg := &SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	if err := SendSubscribeSingleApplied(sender, c, msg); err != nil {
		t.Fatal(err)
	}
}

func TestSendSubscribeSingleAppliedDiscardsAfterDisconnect(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	c.Subscriptions.Reserve(10)
	c.Subscriptions.Remove(10)

	msg := &SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	err := SendSubscribeSingleApplied(s, c, msg)
	if err != nil {
		t.Fatal("should silently discard, not error")
	}
	select {
	case <-c.OutboundCh:
		t.Fatal("frame should not be enqueued for removed subscription")
	default:
	}
}

func TestSendSubscribeSingleAppliedSendFailureDoesNotLeaveSubscriptionActive(t *testing.T) {
	c, _ := testConn(false)
	c.Subscriptions.Reserve(10)

	sender := assertingSender{
		sendFn: func(msg any) error {
			if _, ok := msg.(SubscribeSingleApplied); !ok {
				t.Fatalf("expected SubscribeSingleApplied, got %T", msg)
			}
			if !c.Subscriptions.IsActive(10) {
				t.Fatal("subscription should be active at send attempt")
			}
			return ErrConnNotFound
		},
	}

	msg := &SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	err := SendSubscribeSingleApplied(sender, c, msg)
	if !errors.Is(err, ErrConnNotFound) {
		t.Fatalf("err = %v, want ErrConnNotFound", err)
	}
	if c.Subscriptions.IsActiveOrPending(10) {
		t.Fatal("subscription 10 should be released after failed SubscribeSingleApplied delivery")
	}
}

func TestSendUnsubscribeSingleAppliedRemovesSubscription(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	c.Subscriptions.Reserve(10)
	c.Subscriptions.Activate(10)

	msg := &UnsubscribeSingleApplied{RequestID: 1, QueryID: 10}
	if err := SendUnsubscribeSingleApplied(s, c, msg); err != nil {
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
	s := NewClientSender(mgr, &fakeInbox{})

	c.Subscriptions.Reserve(10)

	msg := &SubscriptionError{RequestID: 1, QueryID: 10, Error: "bad predicate"}
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
	s := NewClientSender(mgr, &fakeInbox{})

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
