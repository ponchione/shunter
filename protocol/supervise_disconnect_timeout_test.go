package protocol

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestSuperviseLifecycleBoundsDisconnectOnInboxHang is the primary pin
// for the supervise-lifecycle disconnect-context. The
// supervisor is the only Conn.Disconnect call site that gets
// context.Background() hardcoded by its caller (upgrade.go
// HandleSubscribe). Fails if superviseLifecycle reverts to forwarding
// that unbounded ctx into c.Disconnect, which would leak the *Conn
// for the process lifetime when inbox.DisconnectClientSubscriptions
// or inbox.OnDisconnect hangs on executor dispatch.
func TestSuperviseLifecycleBoundsDisconnectOnInboxHang(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.DisconnectTimeout = 150 * time.Millisecond
	conn := testConnDirect(&opts)

	inbox := newBlockingInbox()
	mgr := NewConnManager()
	mgr.Add(conn)

	dispatchDone := make(chan struct{})
	keepaliveDone := make(chan struct{})
	outboundDone := make(chan struct{})

	supervised := make(chan struct{})
	start := time.Now()
	go func() {
		conn.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr, dispatchDone, keepaliveDone, outboundDone)
		close(supervised)
	}()

	// Simulate dispatch exiting (peer close, ws read error). The
	// supervisor must then drive Disconnect with a bounded ctx.
	close(dispatchDone)
	// The supervisor waits on every done channel after Disconnect
	// returns; keepalive/outbound normally exit on c.closed, so close them
	// pre-emptively to let the supervisor unwind once Disconnect
	// returns.
	go func() {
		<-conn.closed
		close(keepaliveDone)
		close(outboundDone)
	}()

	select {
	case <-inbox.started:
	case <-time.After(1 * time.Second):
		t.Fatal("supervisor never reached DisconnectClientSubscriptions")
	}

	// Supervisor must complete within DisconnectTimeout + slack even
	// though the inbox blocks on ctx cancellation. conn.closed firing
	// proves Disconnect reached step 4 of the SPEC-005 §5.3 teardown.
	deadline := time.After(opts.DisconnectTimeout + 1*time.Second)
	select {
	case <-conn.closed:
	case <-deadline:
		t.Fatal("conn.closed never fired — supervisor stuck past DisconnectTimeout")
	}

	select {
	case <-supervised:
	case <-time.After(1 * time.Second):
		t.Fatal("supervisor did not return after Disconnect + both done channels")
	}

	elapsed := time.Since(start)
	if elapsed < opts.DisconnectTimeout {
		t.Fatalf("supervisor completed in %v, before DisconnectTimeout %v (ctx should have bounded, not tripped early)", elapsed, opts.DisconnectTimeout)
	}
	if elapsed > opts.DisconnectTimeout+1*time.Second {
		t.Fatalf("supervisor took %v, more than DisconnectTimeout+1s slack", elapsed)
	}

	// Teardown steps 2–4 must have run: both inbox calls then manager
	// drop + channel close.
	if mgr.Get(conn.ID) != nil {
		t.Fatal("conn not removed from manager after bounded supervisor Disconnect")
	}
	if got := inbox.disconnectSubsCalls.Load(); got != 1 {
		t.Fatalf("DisconnectClientSubscriptions calls = %d, want 1", got)
	}
	if got := inbox.onDisconnectCalls.Load(); got != 1 {
		t.Fatalf("OnDisconnect calls = %d, want 1 (teardown must proceed after bounded ctx)", got)
	}
}

// TestSuperviseLifecycleDeliversOnInboxOK pins the happy-path
// contract: when the inbox returns promptly, the supervisor completes
// well under DisconnectTimeout. Fails if a future change serialises
// on the bounded ctx instead of returning on first inbox completion.
func TestSuperviseLifecycleDeliversOnInboxOK(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.DisconnectTimeout = 2 * time.Second
	conn := testConnDirect(&opts)

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)

	dispatchDone := make(chan struct{})
	keepaliveDone := make(chan struct{})
	outboundDone := make(chan struct{})

	supervised := make(chan struct{})
	start := time.Now()
	go func() {
		conn.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr, dispatchDone, keepaliveDone, outboundDone)
		close(supervised)
	}()

	close(dispatchDone)
	go func() {
		<-conn.closed
		close(keepaliveDone)
		close(outboundDone)
	}()

	select {
	case <-supervised:
	case <-time.After(1 * time.Second):
		t.Fatal("supervisor did not return on happy-path Disconnect")
	}

	if elapsed := time.Since(start); elapsed >= opts.DisconnectTimeout {
		t.Fatalf("happy-path supervisor took %v, should be well under DisconnectTimeout %v", elapsed, opts.DisconnectTimeout)
	}
	if mgr.Get(conn.ID) != nil {
		t.Fatal("conn not removed from manager after happy-path supervisor Disconnect")
	}
}
