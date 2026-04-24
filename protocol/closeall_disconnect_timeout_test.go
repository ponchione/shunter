package protocol

import (
	"context"
	"testing"
	"time"
)

// TestCloseAllBoundsDisconnectOnInboxHang is the primary pin for the
// OI-004 CloseAll disconnect-context sub-hazard. CloseAll
// forwards the caller's ctx into each per-conn Conn.Disconnect, which
// threads it into inbox.DisconnectClientSubscriptions and
// inbox.OnDisconnect. Without a per-conn bounded ctx, a
// Background-rooted caller can stall shutdown indefinitely when the
// inbox hangs (executor crash mid-commit, dispatch deadlock,
// scheduler-held lock). Fails if CloseAll reverts to forwarding the
// caller's ctx directly, which would let a single hung *Conn pin the
// shutdown wait group for the process lifetime.
func TestCloseAllBoundsDisconnectOnInboxHang(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.DisconnectTimeout = 150 * time.Millisecond

	inbox := newBlockingInbox()
	mgr := NewConnManager()

	conn := testConnDirect(&opts)
	mgr.Add(conn)

	done := make(chan struct{})
	start := time.Now()
	go func() {
		mgr.CloseAll(context.Background(), inbox)
		close(done)
	}()

	select {
	case <-inbox.started:
	case <-time.After(1 * time.Second):
		t.Fatal("CloseAll goroutine never reached DisconnectClientSubscriptions")
	}

	// CloseAll must return within DisconnectTimeout + slack even though
	// the inbox blocks on ctx cancellation. conn.closed firing proves
	// Disconnect reached step 4 of the SPEC-005 §5.3 teardown.
	deadline := time.After(opts.DisconnectTimeout + 1*time.Second)
	select {
	case <-conn.closed:
	case <-deadline:
		t.Fatal("conn.closed never fired — CloseAll stuck past DisconnectTimeout")
	}

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("CloseAll did not return after bounded Disconnect")
	}

	elapsed := time.Since(start)
	if elapsed < opts.DisconnectTimeout {
		t.Fatalf("CloseAll completed in %v, before DisconnectTimeout %v (ctx should have bounded, not tripped early)", elapsed, opts.DisconnectTimeout)
	}
	if elapsed > opts.DisconnectTimeout+1*time.Second {
		t.Fatalf("CloseAll took %v, more than DisconnectTimeout+1s slack", elapsed)
	}

	if mgr.Get(conn.ID) != nil {
		t.Fatal("conn not removed from manager after bounded CloseAll")
	}
	if got := inbox.disconnectSubsCalls.Load(); got != 1 {
		t.Fatalf("DisconnectClientSubscriptions calls = %d, want 1", got)
	}
	if got := inbox.onDisconnectCalls.Load(); got != 1 {
		t.Fatalf("OnDisconnect calls = %d, want 1 (teardown must proceed after bounded ctx)", got)
	}
}

// TestCloseAllDeliversOnInboxOK pins the happy-path contract: when the
// inbox returns promptly, CloseAll completes well under
// DisconnectTimeout. Fails if a future refactor serialises on the
// bounded ctx instead of returning on inbox completion.
func TestCloseAllDeliversOnInboxOK(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.DisconnectTimeout = 2 * time.Second

	inbox := &fakeInbox{}
	mgr := NewConnManager()

	conn := testConnDirect(&opts)
	mgr.Add(conn)

	done := make(chan struct{})
	start := time.Now()
	go func() {
		mgr.CloseAll(context.Background(), inbox)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("CloseAll did not return on happy-path")
	}

	if elapsed := time.Since(start); elapsed >= opts.DisconnectTimeout {
		t.Fatalf("happy-path CloseAll took %v, should be well under DisconnectTimeout %v", elapsed, opts.DisconnectTimeout)
	}
	if mgr.Get(conn.ID) != nil {
		t.Fatal("conn not removed from manager after happy-path CloseAll")
	}
}
