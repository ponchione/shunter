package protocol

import (
	"errors"
	"testing"
	"time"
)

// TestWatchReducerResponseExitsOnConnClose pins that response watchers exit
// when the owning connection closes.
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
	conn.Compression = true
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

	var frame []byte
	select {
	case frame = <-conn.OutboundCh:
	default:
		t.Fatal("expected TransactionUpdate frame on OutboundCh")
	}
	if frame[0] != CompressionNone {
		t.Fatalf("compression byte = %d, want CompressionNone for small negotiated frame", frame[0])
	}
	tag, decoded := decodeOutboundServerFrame(t, conn, frame)
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

func TestConnOnlySenderBufferFullDisconnectsLifecycleConn(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	opts.DisconnectTimeout = 500 * time.Millisecond
	conn := testConnDirect(&opts)
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	conn.bindDisconnect(inbox, mgr)

	conn.OutboundCh <- []byte{0xff}
	sender := connOnlySender{conn: conn}
	err := sender.Send(conn.ID, SubscriptionError{Error: "overflow"})
	if !errors.Is(err, ErrClientBufferFull) {
		t.Fatalf("Send error = %v, want ErrClientBufferFull", err)
	}

	waitForConnClosed(t, conn, "connOnlySender overflow")
	if got := mgr.Get(conn.ID); got != nil {
		t.Fatalf("connection still registered after overflow disconnect: %p", got)
	}
	onDisconnect, onSubs, _ := inbox.disconnectSnapshot()
	if onDisconnect != 1 || onSubs != 1 {
		t.Fatalf("disconnect calls = (%d, %d), want (1, 1)", onDisconnect, onSubs)
	}
	if got := len(conn.OutboundCh); got != 1 {
		t.Fatalf("overflow frame was enqueued; OutboundCh len = %d, want 1", got)
	}
}

func TestSendErrorBufferFullDisconnectsLifecycleConn(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	opts.DisconnectTimeout = 500 * time.Millisecond
	conn := testConnDirect(&opts)
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	conn.bindDisconnect(inbox, mgr)

	conn.OutboundCh <- []byte{0xff}
	sendError(conn, SubscriptionError{Error: "overflow"})

	waitForConnClosed(t, conn, "sendError overflow")
	if got := mgr.Get(conn.ID); got != nil {
		t.Fatalf("connection still registered after sendError overflow disconnect: %p", got)
	}
	onDisconnect, onSubs, _ := inbox.disconnectSnapshot()
	if onDisconnect != 1 || onSubs != 1 {
		t.Fatalf("disconnect calls = (%d, %d), want (1, 1)", onDisconnect, onSubs)
	}
	if got := len(conn.OutboundCh); got != 1 {
		t.Fatalf("overflow error frame was enqueued; OutboundCh len = %d, want 1", got)
	}
}

func waitForConnClosed(t *testing.T, conn *Conn, label string) {
	t.Helper()
	select {
	case <-conn.closed:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: connection did not close", label)
	}
}
