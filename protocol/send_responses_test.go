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

// Phase 2 Slice 2 admission-model slice (TD-140): the SendSubscribe /
// SendUnsubscribe / SendSubscriptionError helpers are straight transport
// pushes now. The tests below verify the transport-level surface
// (frame enqueue + error propagation). Semantic tests that used to
// assert tracker state transitions (pending → active, release-on-error,
// etc.) are retired — admission is owned by subscription.Manager.querySets
// and the disconnect-race path is covered end-to-end by
// TestDisconnectBetweenRegisterAndReply in admission_ordering_test.go.

func TestSendSubscribeSingleAppliedEnqueuesFrame(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	msg := &SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	if err := SendSubscribeSingleApplied(s, c, msg); err != nil {
		t.Fatal(err)
	}

	select {
	case <-c.OutboundCh:
	default:
		t.Fatal("no frame enqueued")
	}
}

func TestSendSubscribeSingleAppliedPropagatesSendError(t *testing.T) {
	c, _ := testConn(false)

	sender := assertingSender{
		sendFn: func(msg any) error {
			if _, ok := msg.(SubscribeSingleApplied); !ok {
				t.Fatalf("expected SubscribeSingleApplied, got %T", msg)
			}
			return ErrConnNotFound
		},
	}

	msg := &SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	err := SendSubscribeSingleApplied(sender, c, msg)
	if !errors.Is(err, ErrConnNotFound) {
		t.Fatalf("err = %v, want ErrConnNotFound", err)
	}
}

func TestSendUnsubscribeSingleAppliedEnqueuesFrame(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	msg := &UnsubscribeSingleApplied{RequestID: 1, QueryID: 10}
	if err := SendUnsubscribeSingleApplied(s, c, msg); err != nil {
		t.Fatal(err)
	}

	select {
	case <-c.OutboundCh:
	default:
		t.Fatal("no UnsubscribeSingleApplied frame enqueued")
	}
}

func TestSendSubscriptionErrorEnqueuesFrame(t *testing.T) {
	c, _ := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	msg := &SubscriptionError{RequestID: 1, QueryID: 10, Error: "bad predicate"}
	if err := SendSubscriptionError(s, c, msg); err != nil {
		t.Fatal(err)
	}

	select {
	case <-c.OutboundCh:
	default:
		t.Fatal("no SubscriptionError frame enqueued")
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
