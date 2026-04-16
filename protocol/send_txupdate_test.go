package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestDeliverTransactionUpdateSingleConn(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	c.Subscriptions.Reserve(1)
	c.Subscriptions.Activate(1)

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {
			{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}, Deletes: []byte{}},
		},
	}

	errs := DeliverTransactionUpdate(s, mgr, 42, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	tu := msg.(TransactionUpdate)
	if tu.TxID != 42 {
		t.Fatalf("TxID = %d, want 42", tu.TxID)
	}
	if len(tu.Updates) != 1 {
		t.Fatalf("Updates len = %d, want 1", len(tu.Updates))
	}
}

func TestDeliverTransactionUpdateMultiConn(t *testing.T) {
	c1, id1 := testConn(false)
	opts := DefaultProtocolOptions()
	c2 := &Conn{
		ID:            types.ConnectionID{2},
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 256),
		opts:          &opts,
		closed:        make(chan struct{}),
	}
	id2 := c2.ID
	mgr := NewConnManager()
	mgr.Add(c1)
	mgr.Add(c2)
	s := NewClientSender(mgr, &fakeInbox{})

	c1.Subscriptions.Reserve(1)
	c1.Subscriptions.Activate(1)
	c2.Subscriptions.Reserve(2)
	c2.Subscriptions.Activate(2)

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id1: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
		id2: {{SubscriptionID: 2, TableName: "t", Inserts: []byte{0x02}}},
	}

	errs := DeliverTransactionUpdate(s, mgr, 99, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	select {
	case <-c1.OutboundCh:
	default:
		t.Fatal("c1 missing frame")
	}
	select {
	case <-c2.OutboundCh:
	default:
		t.Fatal("c2 missing frame")
	}
}

func TestDeliverTransactionUpdateSkipsDisconnected(t *testing.T) {
	mgr := NewConnManager()
	s := NewClientSender(mgr, &fakeInbox{})

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		{99}: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
	}

	errs := DeliverTransactionUpdate(s, mgr, 1, fanout)
	if len(errs) != 0 {
		t.Fatalf("disconnected conn should be skipped, not error: %v", errs)
	}
}

func TestDeliverTransactionUpdateSkipsEmptyUpdates(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {},
	}

	errs := DeliverTransactionUpdate(s, mgr, 1, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	select {
	case <-c.OutboundCh:
		t.Fatal("empty update should not send a frame")
	default:
	}
}

func TestDeliverTransactionUpdateRejectsPendingSubscription(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	c.Subscriptions.Reserve(1)

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
	}

	errs := DeliverTransactionUpdate(s, mgr, 7, fanout)
	if len(errs) != 1 {
		t.Fatalf("expected 1 invariant error, got %d", len(errs))
	}
	if !errors.Is(errs[0].Err, ErrSubscriptionNotActive) {
		t.Fatalf("expected ErrSubscriptionNotActive, got %v", errs[0].Err)
	}
	select {
	case <-c.OutboundCh:
		t.Fatal("pending subscription update should not be delivered")
	default:
	}
}

func TestDeliverTransactionUpdateBufferFull(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	id := types.ConnectionID{1}
	c := &Conn{
		ID:            id,
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 1),
		opts:          &opts,
		closed:        make(chan struct{}),
	}
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	c.Subscriptions.Reserve(1)
	c.Subscriptions.Activate(1)

	// Fill buffer.
	c.OutboundCh <- []byte{0xFF}

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
	}

	errs := DeliverTransactionUpdate(s, mgr, 1, fanout)
	if len(errs) != 1 {
		t.Fatalf("expected 1 buffer-full error, got %d", len(errs))
	}
	if !errors.Is(errs[0].Err, ErrClientBufferFull) {
		t.Fatalf("expected ErrClientBufferFull, got %v", errs[0].Err)
	}
}
