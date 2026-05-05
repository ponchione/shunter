package protocol

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/websocket"
)

func TestOutgoingBackpressure_BufferFullDisconnects(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}

	// Fill the buffer.
	if err := s.Send(conn.ID, msg); err != nil {
		t.Fatalf("first send: %v", err)
	}

	// Second send overflows → ErrClientBufferFull + disconnect.
	err := s.Send(conn.ID, msg)
	if !errors.Is(err, ErrClientBufferFull) {
		t.Fatalf("expected ErrClientBufferFull, got %v", err)
	}

	// Client should see close 1008.
	readCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, readErr := clientWS.Read(readCtx)
	if code := websocket.CloseStatus(readErr); code != websocket.StatusPolicyViolation {
		t.Errorf("close code = %d, want %d (StatusPolicyViolation/1008)", code, websocket.StatusPolicyViolation)
	}
}

func TestOutgoingBackpressure_OverflowMessageNotDelivered(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn := testConnDirect(&opts)

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	_ = s.Send(conn.ID, msg) // fills buffer

	// Overflow.
	_ = s.Send(conn.ID, msg)

	// OutboundCh should have exactly 1 message (the first), not 2.
	count := len(conn.OutboundCh)
	if count != 1 {
		t.Errorf("OutboundCh length = %d, want 1 (overflow message must not be enqueued)", count)
	}
}

func TestOutgoingBackpressureConcurrentSlowClientShortSoak(t *testing.T) {
	const (
		seed        = uint64(0x5100c11e)
		connections = 5
		workers     = 8
		iterations  = 80
	)
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	opts.DisconnectTimeout = 500 * time.Millisecond

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	conns := make([]*Conn, 0, connections)
	for range connections {
		conn := testConnDirect(&opts)
		conns = append(conns, conn)
		mgr.Add(conn)
	}
	sender := NewClientSender(mgr, inbox)
	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: nil}

	for i, conn := range conns {
		if err := sender.Send(conn.ID, msg); err != nil {
			t.Fatalf("seed=%#x op=prefill-%d runtime_config=connections=%d/workers=%d/iterations=%d observed_error=%v expected=nil",
				seed, i, connections, workers, iterations, err)
		}
	}

	start := make(chan struct{})
	ready := make(chan struct{}, workers)
	failures := make(chan string, workers)
	var sent atomic.Int64
	var bufferFull atomic.Int64
	var notFound atomic.Int64
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			for op := range iterations {
				conn := conns[(int(seed)+worker*13+op*17)%len(conns)]
				err := sender.Send(conn.ID, msg)
				switch {
				case err == nil:
					sent.Add(1)
				case errors.Is(err, ErrClientBufferFull):
					bufferFull.Add(1)
				case errors.Is(err, ErrConnNotFound):
					notFound.Add(1)
				default:
					select {
					case failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=connections=%d/workers=%d/iterations=%d operation=Send(%x) observed_error=%v expected=ErrClientBufferFull-or-ErrConnNotFound",
						seed, worker, op, connections, workers, iterations, conn.ID[:], err):
					default:
					}
					return
				}
				if (int(seed)+worker+op)%5 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}
	waitForSignals(t, ready, workers, "seed=0x5100c11e slow-client backpressure senders started")

	close(start)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
	if got := sent.Load(); got != 0 {
		t.Fatalf("seed=%#x op=worker-send-count runtime_config=connections=%d/workers=%d/iterations=%d observed_successes=%d expected=0 buffer_full=%d not_found=%d",
			seed, connections, workers, iterations, got, bufferFull.Load(), notFound.Load())
	}
	if got := bufferFull.Load() + notFound.Load(); got != workers*iterations {
		t.Fatalf("seed=%#x op=worker-result-count runtime_config=connections=%d/workers=%d/iterations=%d observed=%d expected=%d buffer_full=%d not_found=%d",
			seed, connections, workers, iterations, got, workers*iterations, bufferFull.Load(), notFound.Load())
	}

	for i, conn := range conns {
		select {
		case <-conn.closed:
		case <-time.After(2 * time.Second):
			t.Fatalf("seed=%#x op=closed-%d runtime_config=connections=%d/workers=%d/iterations=%d observed=open expected=closed buffer_full=%d not_found=%d",
				seed, i, connections, workers, iterations, bufferFull.Load(), notFound.Load())
		}
		if got := mgr.Get(conn.ID); got != nil {
			t.Fatalf("seed=%#x op=manager-remove-%d runtime_config=connections=%d/workers=%d/iterations=%d observed=%p expected=nil",
				seed, i, connections, workers, iterations, got)
		}
		if got := len(conn.OutboundCh); got != 1 {
			t.Fatalf("seed=%#x op=overflow-not-enqueued-%d runtime_config=connections=%d/workers=%d/iterations=%d observed_len=%d expected_len=1",
				seed, i, connections, workers, iterations, got)
		}
		err := sender.Send(conn.ID, msg)
		if !errors.Is(err, ErrConnNotFound) {
			t.Fatalf("seed=%#x op=post-close-send-%d runtime_config=connections=%d/workers=%d/iterations=%d observed_error=%v expected=ErrConnNotFound",
				seed, i, connections, workers, iterations, err)
		}
	}
	onDis, onSubs, _ := inbox.disconnectSnapshot()
	if onDis != connections || onSubs != connections {
		t.Fatalf("seed=%#x op=disconnect-count runtime_config=connections=%d/workers=%d/iterations=%d observed=(on_disconnect=%d subscription_disconnect=%d) expected=(%d,%d)",
			seed, connections, workers, iterations, onDis, onSubs, connections, connections)
	}
}

func TestOutgoingBackpressure_FurtherSendsAfterDisconnect(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	_ = s.Send(conn.ID, msg)

	// Overflow triggers disconnect.
	_ = s.Send(conn.ID, msg)

	// Wait for disconnect to complete (conn removed from manager).
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	deadline := time.After(2 * time.Second)
	for mgr.Get(conn.ID) != nil {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("conn not removed from manager after overflow disconnect")
		}
	}

	// Drain the close frame so the background ws.Close goroutine
	// completes before cleanup tears down the httptest server.
	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, _ = clientWS.Read(drainCtx)

	// Further sends return ErrConnNotFound.
	err := s.Send(conn.ID, msg)
	if !errors.Is(err, ErrConnNotFound) {
		t.Fatalf("expected ErrConnNotFound after disconnect, got %v", err)
	}
}

func TestOutgoingBackpressure_SendConcurrentWithDisconnectDoesNotPanic(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 32
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	started := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started <- struct{}{}
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				_ = s.Send(conn.ID, msg)
			}
		}()
	}

	waitForSignals(t, started, 8, "concurrent senders started")
	conn.Disconnect(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr)
	wg.Wait()

	readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readCancel()
	_, _, _ = clientWS.Read(readCtx)
}

func TestClientSenderConcurrentCloseAllShortSoak(t *testing.T) {
	const (
		seed        = uint64(0xc105e411)
		connections = 4
		workers     = 6
		iterations  = 96
	)
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = connections*workers*iterations + 16
	opts.DisconnectTimeout = 500 * time.Millisecond

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	conns := make([]*Conn, 0, connections)
	for range connections {
		conn := testConnDirect(&opts)
		conns = append(conns, conn)
		mgr.Add(conn)
	}
	sender := NewClientSender(mgr, inbox)
	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: nil}

	start := make(chan struct{})
	ready := make(chan struct{}, workers)
	failures := make(chan string, workers*iterations)
	var sent atomic.Int64
	var notFound atomic.Int64
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			ready <- struct{}{}
			<-start
			for op := range iterations {
				conn := conns[(int(seed)+worker*17+op*31)%len(conns)]
				err := sender.Send(conn.ID, msg)
				switch {
				case err == nil:
					sent.Add(1)
				case errors.Is(err, ErrConnNotFound):
					notFound.Add(1)
				default:
					failures <- fmt.Sprintf("seed=%#x worker=%d op=%d runtime_config=connections=%d/workers=%d/iterations=%d operation=Send(%x) observed_error=%v expected=nil-or-ErrConnNotFound",
						seed, worker, op, connections, workers, iterations, conn.ID[:], err)
				}
				if (int(seed)+worker+op)%5 == 0 {
					runtime.Gosched()
				}
			}
		}(worker)
	}
	waitForSignals(t, ready, workers, "seed=0xc105e411 concurrent CloseAll senders started")

	close(start)
	closeAllDone := make(chan struct{})
	go func() {
		mgr.CloseAll(context.Background(), inbox)
		close(closeAllDone)
	}()

	wg.Wait()
	select {
	case <-closeAllDone:
	case <-time.After(2 * time.Second):
		t.Fatal("seed=0xc105e411 op=close-all runtime_config=connections=4/workers=6/iterations=96 observed=timeout expected=CloseAll-complete")
	}
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}

	total := sent.Load() + notFound.Load()
	if total != workers*iterations {
		t.Fatalf("seed=%#x op=final-count runtime_config=connections=%d/workers=%d/iterations=%d observed=%d expected=%d sent=%d not_found=%d",
			seed, connections, workers, iterations, total, workers*iterations, sent.Load(), notFound.Load())
	}
	for op, conn := range conns {
		select {
		case <-conn.closed:
		default:
			t.Fatalf("seed=%#x op=closed-%d runtime_config=connections=%d/workers=%d/iterations=%d observed=open expected=closed", seed, op, connections, workers, iterations)
		}
		if got := mgr.Get(conn.ID); got != nil {
			t.Fatalf("seed=%#x op=manager-remove-%d runtime_config=connections=%d/workers=%d/iterations=%d observed=%p expected=nil", seed, op, connections, workers, iterations, got)
		}
		err := sender.Send(conn.ID, msg)
		if !errors.Is(err, ErrConnNotFound) {
			t.Fatalf("seed=%#x op=post-close-send-%d runtime_config=connections=%d/workers=%d/iterations=%d observed_error=%v expected=ErrConnNotFound", seed, op, connections, workers, iterations, err)
		}
	}
}
