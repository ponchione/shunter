package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestAdmissionOrdering_AppliedPrecedesFanoutOnOutboundCh pins per-connection
// FIFO ordering between Applied and later fan-out frames.
func TestAdmissionOrdering_AppliedPrecedesFanoutOnOutboundCh(t *testing.T) {
	t.Parallel()

	conn, connID := testConn(false)
	mgr := NewConnManager()
	mgr.Add(conn)

	// Step 1: register-success side. Simulate the synchronous Reply enqueue
	// by pushing SubscribeMultiApplied through the same connOnlySender that
	// the executor main loop uses.
	sender := connOnlySender{conn: conn}
	applied := &SubscribeMultiApplied{
		RequestID: 1,
		QueryID:   42,
	}
	if err := SendSubscribeMultiApplied(sender, conn, applied); err != nil {
		t.Fatalf("SendSubscribeMultiApplied: %v", err)
	}

	// Step 2: first fan-out for the same (conn, query_id). Use the
	// production fan-out entry point so the test exercises the real
	// transport path.
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		connID: {
			{
				QueryID:   42,
				TableName: "users",
				Inserts:   []byte{0x01},
			},
		},
	}
	if errs := DeliverTransactionUpdateLight(sender, mgr, 7, fanout); len(errs) != 0 {
		t.Fatalf("DeliverTransactionUpdateLight returned errors: %+v", errs)
	}

	// Step 3: read back both frames from OutboundCh and assert order.
	tag1, msg1 := drainServerMsg(t, conn)
	if tag1 != TagSubscribeMultiApplied {
		t.Fatalf("first frame tag = %d, want TagSubscribeMultiApplied (%d): %T", tag1, TagSubscribeMultiApplied, msg1)
	}
	gotApplied, ok := msg1.(SubscribeMultiApplied)
	if !ok {
		t.Fatalf("first frame type = %T, want SubscribeMultiApplied", msg1)
	}
	if gotApplied.QueryID != 42 {
		t.Fatalf("Applied.QueryID = %d, want 42", gotApplied.QueryID)
	}

	tag2, msg2 := drainServerMsg(t, conn)
	if tag2 != TagTransactionUpdateLight {
		t.Fatalf("second frame tag = %d, want TagTransactionUpdateLight (%d): %T", tag2, TagTransactionUpdateLight, msg2)
	}
	if _, ok := msg2.(TransactionUpdateLight); !ok {
		t.Fatalf("second frame type = %T, want TransactionUpdateLight", msg2)
	}

	// Sanity: no trailing frame.
	select {
	case extra := <-conn.OutboundCh:
		t.Fatalf("unexpected third frame on OutboundCh: %x", extra)
	default:
	}
}

// TestDisconnectBetweenRegisterAndReplyDoesNotSend pins disconnect discard
// for Reply-side delivery.
func TestDisconnectBetweenRegisterAndReplyDoesNotSend(t *testing.T) {
	t.Parallel()

	conn, _ := testConn(false)

	// Disconnect happens between register success and Reply delivery.
	close(conn.closed)

	sender := connOnlySender{conn: conn}
	applied := &SubscribeSingleApplied{
		RequestID: 11,
		QueryID:   23,
		TableName: "users",
		Rows:      []byte{},
	}

	err := SendSubscribeSingleApplied(sender, conn, applied)
	if err == nil {
		t.Fatal("SendSubscribeSingleApplied after close returned nil; want ErrConnNotFound")
	}
	if !errors.Is(err, ErrConnNotFound) {
		t.Fatalf("err = %v, want ErrConnNotFound", err)
	}

	// No frame may be observable on OutboundCh — the guard must fire
	// before the enqueue.
	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected frame on OutboundCh after disconnect-before-reply: %x", frame)
	default:
	}

	// Sanity: a subsequent SubscribeMultiApplied delivery on the same
	// closed conn also discards, confirming the guard is path-agnostic.
	multi := &SubscribeMultiApplied{RequestID: 12, QueryID: 24}
	if err := SendSubscribeMultiApplied(sender, conn, multi); !errors.Is(err, ErrConnNotFound) {
		t.Fatalf("SendSubscribeMultiApplied after close returned %v, want ErrConnNotFound", err)
	}
	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected frame on OutboundCh after second disconnected send: %x", frame)
	default:
	}
}
