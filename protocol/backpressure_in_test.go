package protocol

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/websocket"
)

func waitForSignals(t *testing.T, ch <-chan struct{}, want int, label string) {
	t.Helper()
	for i := 0; i < want; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: observed %d/%d signals", label, i, want)
		}
	}
}

func TestIncomingBackpressure_WithinLimitAllProcessed(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.IncomingQueueMessages = 4
	conn, clientWS := testConnPair(t, &opts)

	var mu sync.Mutex
	var count int
	processed := make(chan struct{}, 4)
	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			mu.Lock()
			count++
			mu.Unlock()
			processed <- struct{}{}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	for i := uint32(0); i < 4; i++ {
		frame, _ := EncodeClientMessage(SubscribeSingleMsg{
			RequestID: i, QueryID: i + 100,
			QueryString: "SELECT * FROM t",
		})
		wCtx, wCancel := context.WithTimeout(ctx, time.Second)
		if err := clientWS.Write(wCtx, websocket.MessageBinary, frame); err != nil {
			wCancel()
			t.Fatalf("write %d: %v", i, err)
		}
		wCancel()
	}

	waitForSignals(t, processed, 4, "processed within limit")

	mu.Lock()
	got := count
	mu.Unlock()
	if got != 4 {
		t.Errorf("processed = %d, want 4", got)
	}

	cancel()
	<-done
}

func TestIncomingBackpressure_ExceedLimitCloses1008(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.IncomingQueueMessages = 2
	conn, clientWS := testConnPair(t, &opts)

	// Handlers block forever so inflight never decreases.
	block := make(chan struct{})
	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			<-block
		},
	}
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Send 3 messages: first 2 acquire semaphore, third exceeds.
	for i := uint32(0); i < 3; i++ {
		frame, _ := EncodeClientMessage(SubscribeSingleMsg{
			RequestID: i, QueryID: i + 100,
			QueryString: "SELECT * FROM t",
		})
		wCtx, wCancel := context.WithTimeout(ctx, time.Second)
		_ = clientWS.Write(wCtx, websocket.MessageBinary, frame)
		wCancel()
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch loop did not exit on incoming overflow")
	}

	readCtx, rCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rCancel()
	_, _, err := clientWS.Read(readCtx)
	if code := websocket.CloseStatus(err); code != websocket.StatusPolicyViolation {
		t.Errorf("close code = %d, want %d (1008)", code, websocket.StatusPolicyViolation)
	}
}

func TestIncomingBackpressure_RapidBurstWithinLimit(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.IncomingQueueMessages = 8
	conn, clientWS := testConnPair(t, &opts)

	var mu sync.Mutex
	var count int
	processed := make(chan struct{}, 8)
	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			mu.Lock()
			count++
			mu.Unlock()
			processed <- struct{}{}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	for i := uint32(0); i < 8; i++ {
		frame, _ := EncodeClientMessage(SubscribeSingleMsg{
			RequestID: i, QueryID: i + 200,
			QueryString: "SELECT * FROM t",
		})
		wCtx, wCancel := context.WithTimeout(ctx, time.Second)
		_ = clientWS.Write(wCtx, websocket.MessageBinary, frame)
		wCancel()
	}

	waitForSignals(t, processed, 8, "processed rapid burst")

	mu.Lock()
	got := count
	mu.Unlock()
	if got != 8 {
		t.Errorf("processed = %d, want 8", got)
	}

	cancel()
	<-done
}

func TestIncomingBackpressure_OverflowMessageNotProcessed(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.IncomingQueueMessages = 1
	conn, clientWS := testConnPair(t, &opts)

	var mu sync.Mutex
	var ids []uint32
	block := make(chan struct{})
	started := make(chan struct{}, 1)
	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			mu.Lock()
			ids = append(ids, msg.QueryID)
			mu.Unlock()
			started <- struct{}{}
			<-block
		},
	}
	defer close(block)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	frame1, _ := EncodeClientMessage(SubscribeSingleMsg{
		RequestID: 1, QueryID: 100,
		QueryString: "SELECT * FROM t",
	})
	wCtx, wCancel := context.WithTimeout(ctx, time.Second)
	_ = clientWS.Write(wCtx, websocket.MessageBinary, frame1)
	wCancel()

	waitForSignals(t, started, 1, "first handler start")

	frame2, _ := EncodeClientMessage(SubscribeSingleMsg{
		RequestID: 2, QueryID: 200,
		QueryString: "SELECT * FROM t",
	})
	wCtx2, wCancel2 := context.WithTimeout(ctx, time.Second)
	_ = clientWS.Write(wCtx2, websocket.MessageBinary, frame2)
	wCancel2()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on incoming overflow")
	}

	mu.Lock()
	got := ids
	mu.Unlock()

	if len(got) != 1 || got[0] != 100 {
		t.Errorf("processed ids = %v, want [100]", got)
	}
}

func TestIncomingBackpressure_NilHandlerDoesNotLeakSemaphoreToken(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.IncomingQueueMessages = 1
	conn, clientWS := testConnPair(t, &opts)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, &MessageHandlers{})

	frame, _ := EncodeClientMessage(SubscribeSingleMsg{
		RequestID:   1,
		QueryID:     100,
		QueryString: "SELECT * FROM t",
	})
	wCtx, wCancel := context.WithTimeout(ctx, time.Second)
	if err := clientWS.Write(wCtx, websocket.MessageBinary, frame); err != nil {
		wCancel()
		t.Fatalf("write subscribe: %v", err)
	}
	wCancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on nil handler")
	}

	if got := len(conn.inflightSem); got != 0 {
		t.Fatalf("inflight semaphore len = %d, want 0 after nil-handler close", got)
	}
}
