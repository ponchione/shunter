package protocol

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestClientInitiatedClose_DisconnectSequenceRuns(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.PingInterval = 2 * time.Second
	opts.IdleTimeout = 4 * time.Second
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)

	handlers := &MessageHandlers{}
	dispatchDone := runDispatchAsync(conn, context.Background(), handlers)
	keepaliveDone := runKeepaliveAsync(conn, context.Background())
	outboundDone := make(chan struct{})
	go func() {
		conn.runOutboundWriter(context.Background())
		close(outboundDone)
	}()

	supervised := make(chan struct{})
	go func() {
		conn.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr, dispatchDone, keepaliveDone, outboundDone)
		close(supervised)
	}()

	// Client sends Close.
	_ = clientWS.Close(websocket.StatusNormalClosure, "bye")

	select {
	case <-supervised:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not complete after client close")
	}

	onDis, onSubs, _ := inbox.disconnectSnapshot()
	if onDis != 1 {
		t.Errorf("OnDisconnect calls = %d, want 1", onDis)
	}
	if onSubs != 1 {
		t.Errorf("DisconnectClientSubscriptions calls = %d, want 1", onSubs)
	}
}

func TestCloseConstants(t *testing.T) {
	tests := []struct {
		name string
		got  websocket.StatusCode
		want websocket.StatusCode
	}{
		{"Normal", CloseNormal, websocket.StatusNormalClosure},
		{"Protocol", CloseProtocol, websocket.StatusProtocolError},
		{"Policy", ClosePolicy, websocket.StatusPolicyViolation},
		{"Internal", CloseInternal, websocket.StatusInternalError},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, tt.got, tt.want)
		}
	}
}

func TestCloseWithHandshake_ResponsivePeer(t *testing.T) {
	conn, clientWS, cleanup := loopbackConn(t, DefaultProtocolOptions())
	defer cleanup()

	// Client reads so the close handshake completes.
	go func() {
		_, _, _ = clientWS.Read(context.Background())
	}()

	done := make(chan struct{})
	go func() {
		closeWithHandshake(conn.ws, CloseNormal, "test", 2*time.Second)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("closeWithHandshake did not complete")
	}
}

func TestCloseAll_DisconnectsEveryConnection(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()

	conns := make([]*Conn, 3)
	for i := range conns {
		c, _, cleanup := loopbackConn(t, DefaultProtocolOptions())
		defer cleanup()
		conns[i] = c
		mgr.Add(c)
	}

	mgr.CloseAll(context.Background(), inbox)

	for i, c := range conns {
		select {
		case <-c.closed:
		default:
			t.Errorf("conn %d: c.closed not signaled after CloseAll", i)
		}
		if mgr.Get(c.ID) != nil {
			t.Errorf("conn %d: still in ConnManager after CloseAll", i)
		}
	}

	onDis, onSubs, _ := inbox.disconnectSnapshot()
	if onDis != 3 {
		t.Errorf("OnDisconnect calls = %d, want 3", onDis)
	}
	if onSubs != 3 {
		t.Errorf("DisconnectClientSubscriptions calls = %d, want 3", onSubs)
	}
}

func TestCloseAll_EmptyManagerNoOp(t *testing.T) {
	inbox := &fakeInbox{}
	mgr := NewConnManager()

	// Must not panic.
	mgr.CloseAll(context.Background(), inbox)

	onDis, _, _ := inbox.disconnectSnapshot()
	if onDis != 0 {
		t.Errorf("OnDisconnect calls = %d, want 0", onDis)
	}
}

func TestCloseAllConcurrentCallersShortSoak(t *testing.T) {
	const (
		seed        = uint64(0xc105ea11)
		connections = 5
		closers     = 6
	)
	opts := DefaultProtocolOptions()
	opts.DisconnectTimeout = 500 * time.Millisecond
	inbox := &fakeInbox{}
	mgr := NewConnManager()
	conns := make([]*Conn, connections)
	for i := range conns {
		conn := testConnDirect(&opts)
		conns[i] = conn
		mgr.Add(conn)
	}

	start := make(chan struct{})
	failures := make(chan string, closers+connections)
	var wg sync.WaitGroup
	for closer := range closers {
		wg.Add(1)
		go func(closer int) {
			defer wg.Done()
			<-start
			mgr.CloseAll(context.Background(), inbox)
			if (int(seed)+closer)%2 == 0 {
				runtime.Gosched()
			}
		}(closer)
	}
	close(start)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("seed=%#x op=wait runtime_config=connections=%d/closers=%d operation=CloseAll-concurrent observed=timeout expected=all-callers-finished",
			seed, connections, closers)
	}

	for i, conn := range conns {
		select {
		case <-conn.closed:
		default:
			failures <- fmt.Sprintf("seed=%#x conn=%d runtime_config=connections=%d/closers=%d operation=closed-signal observed=open expected=closed",
				seed, i, connections, closers)
		}
		if got := mgr.Get(conn.ID); got != nil {
			failures <- fmt.Sprintf("seed=%#x conn=%d runtime_config=connections=%d/closers=%d operation=manager-lookup observed=%p expected=nil",
				seed, i, connections, closers, got)
		}
	}
	onDis, onSubs, _ := inbox.disconnectSnapshot()
	if onDis != connections || onSubs != connections {
		failures <- fmt.Sprintf("seed=%#x op=disconnect-count runtime_config=connections=%d/closers=%d operation=CloseAll-concurrent observed=(on_disconnect=%d subscription_disconnect=%d) expected=(%d,%d)",
			seed, connections, closers, onDis, onSubs, connections, connections)
	}
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
}

func TestCloseWithHandshake_HardTeardownOnTimeout(t *testing.T) {
	conn, clientWS, cleanup := loopbackConn(t, DefaultProtocolOptions())
	defer cleanup()

	// Client does NOT read — close handshake will time out. Backed by the
	// Shunter fork of coder/websocket (SPEC-WS-FORK-001), this guarantees
	// both a bounded helper return and forced transport teardown.
	timeout := 100 * time.Millisecond
	start := time.Now()

	done := make(chan struct{})
	go func() {
		closeWithHandshake(conn.ws, ClosePolicy, "test", timeout)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("closeWithHandshake did not return after timeout")
	}

	elapsed := time.Since(start)
	if elapsed > timeout+400*time.Millisecond {
		t.Errorf("closeWithHandshake took %v, expected ~%v + unwind budget", elapsed, timeout)
	}

	// Transport must be dead: Write fails.
	wctx, wcancel := context.WithTimeout(context.Background(), time.Second)
	defer wcancel()
	if werr := conn.ws.Write(wctx, websocket.MessageText, []byte("x")); werr == nil {
		t.Fatal("expected transport to be dead after closeWithHandshake timeout, Write succeeded")
	}

	_ = clientWS
}
