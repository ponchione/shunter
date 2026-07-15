package executor

import (
	"context"
	"testing"
	"time"

	"github.com/ponchione/shunter/protocol"
	"github.com/ponchione/shunter/types"
	"github.com/ponchione/websocket"
)

type delayedDisconnectSubscriptions struct {
	*fakeSubs
	started chan struct{}
	release chan struct{}
}

func (s *delayedDisconnectSubscriptions) DisconnectClient(connID types.ConnectionID) error {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-s.release
	return s.fakeSubs.DisconnectClient(connID)
}

func TestDisconnectSubscriptionTimeoutCannotSuppressOnDisconnectAdmission(t *testing.T) {
	h := newLifecycleHarness(t, lifecycleOpt{withOnDisconn: true})
	delayed := &delayedDisconnectSubscriptions{
		fakeSubs: h.subs,
		started:  make(chan struct{}, 1),
		release:  make(chan struct{}),
	}
	h.exec.subs = delayed
	if err := h.exec.Startup(context.Background(), nil); err != nil {
		t.Fatalf("Startup: %v", err)
	}
	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	go h.exec.Run(runCtx)

	connID := types.ConnectionID{0xD1}
	identity := types.Identity{0xD2}
	prime(t, h, connID, identity)

	opts := protocol.DefaultProtocolOptions()
	opts.DisconnectTimeout = 120 * time.Millisecond
	conn := protocol.NewConn(connID, identity, "", false, nil, &opts)
	mgr := protocol.NewConnManager()
	mgr.Add(conn)

	disconnectDone := make(chan struct{})
	started := time.Now()
	go func() {
		conn.Disconnect(
			context.Background(),
			websocket.StatusNormalClosure,
			"",
			NewProtocolInboxAdapter(h.exec),
			mgr,
		)
		close(disconnectDone)
	}()

	select {
	case <-delayed.started:
	case <-time.After(time.Second):
		t.Fatal("subscription cleanup did not start")
	}
	select {
	case <-disconnectDone:
	case <-time.After(opts.DisconnectTimeout + time.Second):
		t.Fatal("local teardown exceeded its outer timeout")
	}
	if elapsed := time.Since(started); elapsed > opts.DisconnectTimeout+250*time.Millisecond {
		t.Fatalf("local teardown elapsed = %v, want outer timeout %v plus slack", elapsed, opts.DisconnectTimeout)
	}
	if mgr.Get(connID) != nil {
		t.Fatal("connection manager retained connection after bounded teardown")
	}
	if got := h.onDisconn.count(); got != 0 {
		t.Fatalf("OnDisconnect ran before delayed subscription cleanup completed: %d", got)
	}

	close(delayed.release)
	deadline := time.Now().Add(time.Second)
	for h.onDisconn.count() != 1 || len(h.sysClientsSnapshot()) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf(
				"admitted OnDisconnect did not finish: reducer calls=%d sys_clients rows=%d",
				h.onDisconn.count(),
				len(h.sysClientsSnapshot()),
			)
		}
		time.Sleep(time.Millisecond)
	}
	if got := h.onDisconn.count(); got != 1 {
		t.Fatalf("OnDisconnect reducer calls = %d, want exactly 1", got)
	}
}
