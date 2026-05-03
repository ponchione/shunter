package protocol

import (
	"context"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestDispatchLoop_HandlerCtxCancelsOnConnClose pins handler context
// cancellation when the owning Conn closes.
func TestDispatchLoop_HandlerCtxCancelsOnConnClose(t *testing.T) {
	conn, client := testConnPair(t, nil)

	handlerCtxCh := make(chan context.Context, 1)
	handlerDone := make(chan struct{})
	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			handlerCtxCh <- ctx
			<-ctx.Done()
			close(handlerDone)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dispatchDone := runDispatchAsync(conn, ctx, handlers)

	sub := SubscribeSingleMsg{
		RequestID:   1,
		QueryID:     42,
		QueryString: "SELECT * FROM users",
	}
	frame, err := EncodeClientMessage(sub)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	defer writeCancel()
	if err := client.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("client write: %v", err)
	}

	var handlerCtx context.Context
	select {
	case handlerCtx = <-handlerCtxCh:
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked")
	}

	// Sanity window: handler ctx must stay live while both arms (outer
	// ctx + c.closed) remain open. Catches a future refactor that
	// collapses handlerCtx onto the outer ctx without a teardown wire,
	// or one that cancels handlerCtx spuriously.
	select {
	case <-handlerCtx.Done():
		t.Fatal("handler ctx cancelled before conn teardown signal fired")
	case <-handlerDone:
		t.Fatal("handler exited before conn teardown signal fired")
	case <-time.After(25 * time.Millisecond):
	}

	// Simulate SPEC-005 §5.3 step 4: Conn.Disconnect.closeOnce.Do fires
	// close(c.closed). This test drives the signal directly instead of
	// invoking Disconnect to isolate the handlerCtx wire from the wider
	// disconnect teardown surface.
	close(conn.closed)

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not exit within 1 s of closing conn.closed")
	}

	if handlerCtx.Err() == nil {
		t.Fatalf("handler ctx.Err() = nil, want non-nil after conn teardown")
	}

	// Cleanup: cancel the outer ctx so the dispatch goroutine unwinds.
	// runDispatchLoop blocks inside ws.Read(readCtx) after the handler
	// is spawned; readCtx is tied to the outer ctx and c.readCtx, not
	// c.closed, so close(conn.closed) alone does not unblock Read. In
	// production, Conn.Disconnect invokes c.cancelRead() immediately
	// before close(c.closed), which fires c.readCtx.Done() and cancels
	// readCtx. The test takes the equivalent via cancel() here since
	// this test drives close(c.closed) directly rather than through
	// Disconnect.
	cancel()
	select {
	case <-dispatchDone:
	case <-time.After(time.Second):
		t.Fatal("dispatch loop did not exit after outer ctx cancel")
	}
}

// TestDispatchLoop_HandlerCtxCancelsOnOuterCtx pins the second leg of the
// handlerCtx contract: outer ctx cancellation still propagates. Prevents a
// future refactor from accidentally severing handlerCtx from the outer
// ctx while wiring c.closed.
func TestDispatchLoop_HandlerCtxCancelsOnOuterCtx(t *testing.T) {
	conn, client := testConnPair(t, nil)

	handlerCtxCh := make(chan context.Context, 1)
	handlerDone := make(chan struct{})
	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			handlerCtxCh <- ctx
			<-ctx.Done()
			close(handlerDone)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	dispatchDone := runDispatchAsync(conn, ctx, handlers)

	sub := SubscribeSingleMsg{
		RequestID:   1,
		QueryID:     42,
		QueryString: "SELECT * FROM users",
	}
	frame, err := EncodeClientMessage(sub)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	defer writeCancel()
	if err := client.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("client write: %v", err)
	}

	select {
	case <-handlerCtxCh:
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not invoked")
	}

	cancel()

	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not exit within 1 s of outer ctx cancel")
	}
	select {
	case <-dispatchDone:
	case <-time.After(time.Second):
		t.Fatal("dispatch loop did not exit after outer ctx cancel")
	}
}
