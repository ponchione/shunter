package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestDeliverReducerResultEmbedsDelta(t *testing.T) {
	caller, callerID := testConn(false)
	opts := DefaultProtocolOptions()
	other := &Conn{
		ID:            types.ConnectionID{2},
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 256),
		opts:          &opts,
		closed:        make(chan struct{}),
	}
	otherID := other.ID
	mgr := NewConnManager()
	mgr.Add(caller)
	mgr.Add(other)
	s := NewClientSender(mgr)

	caller.Subscriptions.Reserve(1)
	caller.Subscriptions.Activate(1)
	other.Subscriptions.Reserve(2)
	other.Subscriptions.Activate(2)

	result := &ReducerCallResult{RequestID: 5, Status: 0, TxID: 42}
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		callerID: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
		otherID:  {{SubscriptionID: 2, TableName: "t", Inserts: []byte{0x02}}},
	}

	errs := DeliverReducerCallResult(s, mgr, result, &callerID, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	// Caller should get ReducerCallResult with embedded updates.
	callerFrame := <-caller.OutboundCh
	_, callerMsg, err := DecodeServerMessage(callerFrame)
	if err != nil {
		t.Fatal(err)
	}
	rcr := callerMsg.(ReducerCallResult)
	if rcr.RequestID != 5 {
		t.Fatalf("RequestID = %d, want 5", rcr.RequestID)
	}
	if len(rcr.TransactionUpdate) != 1 {
		t.Fatalf("caller embedded updates = %d, want 1", len(rcr.TransactionUpdate))
	}

	// Other should get standalone TransactionUpdate.
	otherFrame := <-other.OutboundCh
	_, otherMsg, err := DecodeServerMessage(otherFrame)
	if err != nil {
		t.Fatal(err)
	}
	tu := otherMsg.(TransactionUpdate)
	if tu.TxID != 42 {
		t.Fatalf("other TxID = %d, want 42", tu.TxID)
	}

	// Caller should NOT have a second frame (no standalone TxUpdate).
	select {
	case <-caller.OutboundCh:
		t.Fatal("caller should not get standalone TransactionUpdate")
	default:
	}
}

func TestDeliverReducerResultFailedReducerEmptyUpdate(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	result := &ReducerCallResult{RequestID: 1, Status: 1, Error: "user error"}
	errs := DeliverReducerCallResult(s, mgr, result, &id, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	rcr := msg.(ReducerCallResult)
	if rcr.Status != 1 {
		t.Fatalf("Status = %d, want 1", rcr.Status)
	}
	if len(rcr.TransactionUpdate) != 0 {
		t.Fatal("failed reducer should have empty TransactionUpdate")
	}
}

func TestDeliverReducerResultNoCaller(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	c.Subscriptions.Reserve(1)
	c.Subscriptions.Activate(1)

	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		id: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
	}

	// Non-reducer commit: callerConnID is nil, result carries TxID for standalone delivery.
	result := &ReducerCallResult{TxID: 50}
	errs := DeliverReducerCallResult(s, mgr, result, nil, fanout)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := msg.(TransactionUpdate); !ok {
		t.Fatalf("expected TransactionUpdate for non-reducer commit, got %T", msg)
	}
}

func TestDeliverReducerResultCallerDisconnected(t *testing.T) {
	opts := DefaultProtocolOptions()
	other := &Conn{
		ID:            types.ConnectionID{2},
		Subscriptions: NewSubscriptionTracker(),
		OutboundCh:    make(chan []byte, 256),
		opts:          &opts,
		closed:        make(chan struct{}),
	}
	mgr := NewConnManager()
	mgr.Add(other)
	s := NewClientSender(mgr)

	other.Subscriptions.Reserve(2)
	other.Subscriptions.Activate(2)

	callerID := types.ConnectionID{1} // not in manager
	result := &ReducerCallResult{RequestID: 5, Status: 0, TxID: 42}
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		callerID: {{SubscriptionID: 1, TableName: "t", Inserts: []byte{0x01}}},
		other.ID: {{SubscriptionID: 2, TableName: "t", Inserts: []byte{0x02}}},
	}

	errs := DeliverReducerCallResult(s, mgr, result, &callerID, fanout)
	// Caller send fails with ErrConnNotFound — collected as error.
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for disconnected caller, got %d", len(errs))
	}
	if !errors.Is(errs[0].Err, ErrConnNotFound) {
		t.Fatalf("expected ErrConnNotFound, got %v", errs[0].Err)
	}
	// Other should still get TransactionUpdate.
	select {
	case <-other.OutboundCh:
	default:
		t.Fatal("other should have received TransactionUpdate")
	}
}

func TestDeliverReducerResultNotFoundStatus(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	result := &ReducerCallResult{RequestID: 1, Status: 3, TxID: 0, Error: "reducer not found"}
	errs := DeliverReducerCallResult(s, mgr, result, &id, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	rcr := msg.(ReducerCallResult)
	if rcr.Status != 3 {
		t.Fatalf("Status = %d, want 3", rcr.Status)
	}
	if rcr.TxID != 0 {
		t.Fatalf("TxID = %d, want 0 for not-found", rcr.TxID)
	}
}

func TestDeliverReducerResultEnergyAlwaysZero(t *testing.T) {
	c, id := testConn(false)
	mgr := NewConnManager()
	mgr.Add(c)
	s := NewClientSender(mgr)

	// Set Energy to non-zero — delivery must force it to 0.
	result := &ReducerCallResult{RequestID: 1, Status: 0, TxID: 10, Energy: 999}
	errs := DeliverReducerCallResult(s, mgr, result, &id, nil)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}

	frame := <-c.OutboundCh
	_, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatal(err)
	}
	rcr := msg.(ReducerCallResult)
	if rcr.Energy != 0 {
		t.Fatalf("Energy = %d, want 0 (v1 always zero)", rcr.Energy)
	}
}
