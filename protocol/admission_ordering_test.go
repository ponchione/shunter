package protocol

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestAdmissionOrdering_AppliedPrecedesFanoutOnOutboundCh pins SPEC-005
// §9.4: on a single connection, a SubscribeApplied envelope enqueued
// during registration MUST reach the wire before any TransactionUpdate
// that references the same subscription.
//
// The ADR (docs/adr/2026-04-19-subscription-admission-model.md §9.4)
// guarantees this by synchronously enqueuing the Applied on the
// connection's OutboundCh inside the executor main-loop goroutine, then
// letting fan-out from the next commit enqueue on the same per-conn
// FIFO. OutboundCh is a per-connection single-writer FIFO, so wire
// order matches enqueue order.
//
// This test asserts the transport-level invariant that is the end-state
// contract: when an Applied frame is enqueued before a fan-out frame on
// a single connection's outbound channel, the wire observes them in
// that order. It works on today's code (any path that enqueues Applied
// first will deliver it first) and continues to work after Tasks 3-4
// wire the synchronous Reply seam, because the invariant is the
// property those tasks will rely on.
func TestAdmissionOrdering_AppliedPrecedesFanoutOnOutboundCh(t *testing.T) {
	t.Parallel()

	conn, connID := testConn(false)
	mgr := NewConnManager()
	mgr.Add(conn)

	// Step 1: register-success side. Simulate the ADR's synchronous
	// Reply enqueue by pushing SubscribeMultiApplied through the same
	// connOnlySender that the executor main loop will use after Task 3.
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

// TestDisconnectBetweenRegisterAndReplyDoesNotSend pins SPEC-005 §9.1
// rule 3 and the ADR §9.1 disconnect-discard guarantee: when the
// connection closes between the executor-side register call and the
// Reply-closure invocation, no Applied frame reaches the connection's
// OutboundCh and no stale admission state resurrects.
//
// Mechanism: the ADR routes every Reply through a connOnlySender, whose
// Send observes conn.closed as its first guard (see async_responses.go).
// Closing conn.closed before the Reply path runs must cause the Reply
// to return ErrConnNotFound with zero frames enqueued. This test uses
// SendSubscribeSingleApplied as the Reply-side delivery primitive; the
// same connOnlySender guard applies to SendSubscribeMultiApplied and
// SendSubscriptionError.
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
