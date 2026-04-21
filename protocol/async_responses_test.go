package protocol

import (
	"testing"
	"time"
)

// TestWatchReducerResponseExitsOnConnClose pins the OI-004 Tier-B
// hardening fix for the `watchReducerResponse` goroutine leak.
//
// Sharp edge: before 2026-04-20, the watcher goroutine blocked
// unconditionally on `<-respCh`. If the executor accepted the
// CallReducer but never sent on or closed the response channel (hung
// reducer, executor crash mid-commit, engine shutdown mid-flight), the
// watcher goroutine would leak for the lifetime of the process and
// hold its `*Conn` alive past disconnect.
//
// Contract: after `conn.closed` is closed by `Conn.Disconnect` (step 4
// of the SPEC-005 §5.3 teardown), the watcher must exit promptly even
// if `respCh` never fires. Asserted by running the watcher body
// synchronously through `runReducerResponseWatcher` in a test-owned
// goroutine, closing `conn.closed`, and waiting for the body to
// return within a bounded deadline.
func TestWatchReducerResponseExitsOnConnClose(t *testing.T) {
	conn := testConnDirect(nil)
	respCh := make(chan TransactionUpdate) // never sends, never closes

	done := make(chan struct{})
	go func() {
		runReducerResponseWatcher(conn, respCh)
		close(done)
	}()

	// Simulate Conn.Disconnect signalling teardown.
	close(conn.closed)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit after conn.closed; goroutine leak")
	}
}

// TestWatchReducerResponseDeliversOnRespCh pins that the watcher still
// delivers the heavy TransactionUpdate envelope onto OutboundCh when
// `respCh` fires before `conn.closed`. Guards the happy path against
// a future refactor that accidentally inverts the select arms.
func TestWatchReducerResponseDeliversOnRespCh(t *testing.T) {
	conn := testConnDirect(nil)
	respCh := make(chan TransactionUpdate, 1)

	done := make(chan struct{})
	go func() {
		runReducerResponseWatcher(conn, respCh)
		close(done)
	}()

	respCh <- TransactionUpdate{
		Status: StatusCommitted{},
		ReducerCall: ReducerCallInfo{
			ReducerName: "AddUser",
			RequestID:   77,
		},
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit after respCh send")
	}

	tag, decoded := drainServerMsg(t, conn)
	if tag != TagTransactionUpdate {
		t.Fatalf("tag = %d, want %d (TagTransactionUpdate)", tag, TagTransactionUpdate)
	}
	tu := decoded.(TransactionUpdate)
	if tu.ReducerCall.RequestID != 77 {
		t.Fatalf("ReducerCall.RequestID = %d, want 77", tu.ReducerCall.RequestID)
	}
}

// TestWatchReducerResponseExitsOnRespChClose pins that a closed (not
// sent-on) respCh still exits the watcher cleanly and does not deliver
// a zero-value TransactionUpdate. This case corresponds to an executor
// path that tears down the response channel without ever producing an
// outcome.
func TestWatchReducerResponseExitsOnRespChClose(t *testing.T) {
	conn := testConnDirect(nil)
	respCh := make(chan TransactionUpdate)

	done := make(chan struct{})
	go func() {
		runReducerResponseWatcher(conn, respCh)
		close(done)
	}()

	close(respCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not exit after respCh close")
	}

	select {
	case frame := <-conn.OutboundCh:
		t.Fatalf("unexpected outbound frame after respCh close: %x", frame)
	default:
	}
}
