package protocol

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

// blockingInbox blocks DisconnectClientSubscriptions until ctx is
// cancelled, simulating a hang in the executor dispatch path.
// OnDisconnect is cheap and returns immediately because the hang
// scenario is about the first inbox call (the one that would race
// closeOnce with the rest of the teardown). OnConnect and the
// subscription / reducer seams are unused in this test.
type blockingInbox struct {
	started             chan struct{}
	disconnectSubsErr   atomic.Value // error
	onDisconnectCalls   atomic.Int32
	disconnectSubsCalls atomic.Int32
}

func newBlockingInbox() *blockingInbox {
	return &blockingInbox{started: make(chan struct{}, 1)}
}

func (b *blockingInbox) OnConnect(context.Context, types.ConnectionID, types.Identity) error {
	return nil
}

func (b *blockingInbox) OnDisconnect(_ context.Context, _ types.ConnectionID, _ types.Identity) error {
	b.onDisconnectCalls.Add(1)
	return nil
}

func (b *blockingInbox) DisconnectClientSubscriptions(ctx context.Context, _ types.ConnectionID) error {
	b.disconnectSubsCalls.Add(1)
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	if v := b.disconnectSubsErr.Load(); v != nil {
		return v.(error)
	}
	return ctx.Err()
}

func (b *blockingInbox) RegisterSubscriptionSet(context.Context, RegisterSubscriptionSetRequest) error {
	return nil
}

func (b *blockingInbox) UnregisterSubscriptionSet(context.Context, UnregisterSubscriptionSetRequest) error {
	return nil
}

func (b *blockingInbox) CallReducer(context.Context, CallReducerRequest) error {
	return nil
}

// TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang is the primary
// sender-disconnect-context pin. Fails if
// connManagerSender.enqueueOnConn reverts to spawning Disconnect with
// a Background context (or any ctx that never cancels under
// DisconnectTimeout), which would leak the detached goroutine for the
// process lifetime when the inbox hangs.
func TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	opts.DisconnectTimeout = 150 * time.Millisecond
	conn := testConnDirect(&opts)

	inbox := newBlockingInbox()
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	if err := s.Send(conn.ID, msg); err != nil {
		t.Fatalf("first send: %v", err)
	}

	start := time.Now()
	err := s.Send(conn.ID, msg)
	if !errors.Is(err, ErrClientBufferFull) {
		t.Fatalf("overflow send: expected ErrClientBufferFull, got %v", err)
	}

	select {
	case <-inbox.started:
	case <-time.After(1 * time.Second):
		t.Fatal("detached Disconnect goroutine never reached DisconnectClientSubscriptions")
	}

	// The detached goroutine must exit within DisconnectTimeout + slack
	// even though the inbox blocks until ctx cancels. conn.closed firing
	// proves Disconnect reached step 4 of the SPEC-005 §5.3 teardown.
	deadline := time.After(opts.DisconnectTimeout + 1*time.Second)
	select {
	case <-conn.closed:
	case <-deadline:
		t.Fatal("conn.closed never fired — Disconnect goroutine stuck past DisconnectTimeout")
	}

	elapsed := time.Since(start)
	if elapsed < opts.DisconnectTimeout {
		t.Fatalf("Disconnect completed in %v, before DisconnectTimeout %v (ctx should have bounded, not tripped early)", elapsed, opts.DisconnectTimeout)
	}
	if elapsed > opts.DisconnectTimeout+1*time.Second {
		t.Fatalf("Disconnect took %v, more than DisconnectTimeout+1s slack", elapsed)
	}

	// Teardown steps 3–4 must have run: manager drop + channel close.
	if mgr.Get(conn.ID) != nil {
		t.Fatal("conn not removed from manager after bounded Disconnect")
	}
	if got := inbox.disconnectSubsCalls.Load(); got != 1 {
		t.Fatalf("DisconnectClientSubscriptions calls = %d, want 1", got)
	}
	if got := inbox.onDisconnectCalls.Load(); got != 1 {
		t.Fatalf("OnDisconnect calls = %d, want 1 (teardown must proceed after bounded ctx)", got)
	}
}

// TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK pins the
// happy-path contract: when the inbox returns promptly the detached
// Disconnect goroutine completes quickly, well under DisconnectTimeout.
// Fails if a future refactor serialises on the bounded ctx instead of
// returning on first completion.
func TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	opts.DisconnectTimeout = 2 * time.Second
	conn := testConnDirect(&opts)

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	if err := s.Send(conn.ID, msg); err != nil {
		t.Fatalf("first send: %v", err)
	}

	start := time.Now()
	if err := s.Send(conn.ID, msg); !errors.Is(err, ErrClientBufferFull) {
		t.Fatalf("overflow send: expected ErrClientBufferFull, got %v", err)
	}

	select {
	case <-conn.closed:
	case <-time.After(1 * time.Second):
		t.Fatal("conn.closed never fired on happy-path Disconnect")
	}

	if elapsed := time.Since(start); elapsed >= opts.DisconnectTimeout {
		t.Fatalf("happy-path Disconnect took %v, should be well under DisconnectTimeout %v", elapsed, opts.DisconnectTimeout)
	}
	if mgr.Get(conn.ID) != nil {
		t.Fatal("conn not removed from manager after happy-path Disconnect")
	}
}
