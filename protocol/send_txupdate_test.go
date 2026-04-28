package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

// Outcome-model split: this file exercises the
// `DeliverTransactionUpdateLight` helper used for non-caller
// subscribers. Caller-heavy delivery is exercised through the fanout
// adapter tests in `fanout_adapter_test.go` and the fanout worker tests
// in `subscription/`.

func TestDeliverTransactionUpdateLightSingleConn(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {
			{QueryID: 1, TableName: "t", Inserts: []byte{0x01}, Deletes: []byte{}},
		},
	}

	errs := DeliverTransactionUpdateLight(s, mgr, 42, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	tu := msg.(TransactionUpdateLight)
	if tu.RequestID != 42 {
		t.Fatalf("RequestID = %d, want 42", tu.RequestID)
	}
	if len(tu.Update) != 1 {
		t.Fatalf("Update len = %d, want 1", len(tu.Update))
	}
}

func TestDeliverTransactionUpdateLightMultiConn(t *testing.T) {
	c1, id1 := testConn(false)
	opts := DefaultProtocolOptions()
	c2 := &Conn{
		ID:         types.ConnectionID{2},
		OutboundCh: make(chan []byte, 256),
		opts:       &opts,
		closed:     make(chan struct{}),
	}
	id2 := c2.ID
	mgr := NewConnManager()
	mgr.Add(c1)
	mgr.Add(c2)
	s := NewClientSender(mgr, &fakeInbox{})

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id1: {{QueryID: 1, TableName: "t", Inserts: []byte{0x01}}},
		id2: {{QueryID: 2, TableName: "t", Inserts: []byte{0x02}}},
	}

	errs := DeliverTransactionUpdateLight(s, mgr, 99, fanout)
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

func TestDeliverTransactionUpdateLightSkipsDisconnected(t *testing.T) {
	mgr := NewConnManager()
	s := NewClientSender(mgr, &fakeInbox{})

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		{99}: {{QueryID: 1, TableName: "t", Inserts: []byte{0x01}}},
	}

	errs := DeliverTransactionUpdateLight(s, mgr, 1, fanout)
	if len(errs) != 0 {
		t.Fatalf("disconnected conn should be skipped, not error: %v", errs)
	}
}

func TestDeliverTransactionUpdateLightSkipsEmptyUpdates(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {},
	}

	errs := DeliverTransactionUpdateLight(s, mgr, 1, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	select {
	case <-c.OutboundCh:
		t.Fatal("empty update should not send a frame")
	default:
	}
}

func TestDeliverTransactionUpdateLightBufferFull(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	id := types.ConnectionID{1}
	c := &Conn{
		ID:         id,
		OutboundCh: make(chan []byte, 1),
		opts:       &opts,
		closed:     make(chan struct{}),
	}
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr, &fakeInbox{})

	// Fill buffer.
	c.OutboundCh <- []byte{0xFF}

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {{QueryID: 1, TableName: "t", Inserts: []byte{0x01}}},
	}

	errs := DeliverTransactionUpdateLight(s, mgr, 1, fanout)
	if len(errs) != 1 {
		t.Fatalf("expected 1 buffer-full error, got %d", len(errs))
	}
	if !errors.Is(errs[0].Err, ErrClientBufferFull) {
		t.Fatalf("expected ErrClientBufferFull, got %v", errs[0].Err)
	}
}
