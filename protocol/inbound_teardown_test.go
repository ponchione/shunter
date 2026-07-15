package protocol

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
	"github.com/ponchione/websocket"
)

type orderedDisconnectInbox struct {
	fakeInbox
	handlerFinished <-chan struct{}
	cleanupStarted  chan struct{}
	cleanupEarly    atomic.Bool
}

func (i *orderedDisconnectInbox) DisconnectClientSubscriptions(ctx context.Context, connID types.ConnectionID) error {
	select {
	case <-i.handlerFinished:
	default:
		i.cleanupEarly.Store(true)
	}
	close(i.cleanupStarted)
	return i.fakeInbox.DisconnectClientSubscriptions(ctx, connID)
}

func TestDisconnectCancelsAndDrainsHandlersBeforeExecutorCleanup(t *testing.T) {
	conn, client := testConnPair(t, nil)
	mgr := NewConnManager()
	if err := mgr.Add(conn); err != nil {
		t.Fatalf("Add: %v", err)
	}

	handlerStarted := make(chan struct{})
	handlerCanceled := make(chan struct{})
	releaseHandler := make(chan struct{})
	handlerFinished := make(chan struct{})
	dispatchDone := runDispatchAsync(conn, context.Background(), &MessageHandlers{
		OnCallReducer: func(ctx context.Context, _ *Conn, _ *CallReducerMsg) {
			close(handlerStarted)
			<-ctx.Done()
			close(handlerCanceled)
			<-releaseHandler
			close(handlerFinished)
		},
	})

	frame, err := EncodeClientMessage(CallReducerMsg{RequestID: 1, ReducerName: "mutate"})
	if err != nil {
		t.Fatalf("EncodeClientMessage: %v", err)
	}
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), time.Second)
	defer cancelWrite()
	if err := client.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("Write: %v", err)
	}
	select {
	case <-handlerStarted:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}

	inbox := &orderedDisconnectInbox{
		handlerFinished: handlerFinished,
		cleanupStarted:  make(chan struct{}),
	}
	disconnectDone := make(chan struct{})
	go func() {
		conn.Disconnect(context.Background(), CloseNormal, "test", inbox, mgr)
		close(disconnectDone)
	}()

	select {
	case <-handlerCanceled:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not cancel the active handler")
	}
	select {
	case <-inbox.cleanupStarted:
		t.Fatal("subscription cleanup started before the active handler drained")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseHandler)
	select {
	case <-disconnectDone:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not finish after handler drain")
	}
	if inbox.cleanupEarly.Load() {
		t.Fatal("executor cleanup observed an unfinished inbound handler")
	}
	select {
	case <-dispatchDone:
	case <-time.After(time.Second):
		t.Fatal("dispatch loop did not exit at the inbound barrier")
	}
}

type blockedDisconnectCleanupInbox struct {
	fakeInbox
	started chan struct{}
	release chan struct{}
}

func (i *blockedDisconnectCleanupInbox) DisconnectClientSubscriptions(ctx context.Context, connID types.ConnectionID) error {
	close(i.started)
	select {
	case <-i.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return i.fakeInbox.DisconnectClientSubscriptions(ctx, connID)
}

func TestCloseAllBarrierRejectsReducerAndSubscriptionFramesDuringCleanup(t *testing.T) {
	conn, client := testConnPair(t, nil)
	mgr := NewConnManager()
	if err := mgr.Add(conn); err != nil {
		t.Fatalf("Add: %v", err)
	}

	reducerCalled := make(chan struct{}, 1)
	subscribeCalled := make(chan struct{}, 1)
	dispatchDone := runDispatchAsync(conn, context.Background(), &MessageHandlers{
		OnCallReducer: func(context.Context, *Conn, *CallReducerMsg) {
			reducerCalled <- struct{}{}
		},
		OnSubscribeSingle: func(context.Context, *Conn, *SubscribeSingleMsg) {
			subscribeCalled <- struct{}{}
		},
	})
	inbox := &blockedDisconnectCleanupInbox{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	closeAllDone := make(chan struct{})
	go func() {
		mgr.CloseAll(context.Background(), inbox)
		close(closeAllDone)
	}()

	select {
	case <-conn.inboundDone():
	case <-time.After(time.Second):
		t.Fatal("CloseAll did not establish the inbound barrier")
	}
	select {
	case <-inbox.started:
	case <-time.After(time.Second):
		t.Fatal("CloseAll did not reach blocked subscription cleanup")
	}

	frames := []any{
		CallReducerMsg{RequestID: 10, ReducerName: "late_reducer"},
		SubscribeSingleMsg{RequestID: 11, QueryID: 12, QueryString: "SELECT * FROM users"},
	}
	for _, msg := range frames {
		frame, err := EncodeClientMessage(msg)
		if err != nil {
			t.Fatalf("EncodeClientMessage(%T): %v", msg, err)
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_ = client.Write(writeCtx, websocket.MessageBinary, frame)
		cancel()
	}
	select {
	case <-reducerCalled:
		t.Fatal("reducer frame reached a handler after the CloseAll barrier")
	case <-subscribeCalled:
		t.Fatal("subscription frame reached a handler after the CloseAll barrier")
	case <-time.After(50 * time.Millisecond):
	}

	close(inbox.release)
	select {
	case <-closeAllDone:
	case <-time.After(time.Second):
		t.Fatal("CloseAll did not finish after cleanup release")
	}
	select {
	case <-dispatchDone:
	case <-time.After(time.Second):
		t.Fatal("dispatch loop did not exit after CloseAll")
	}
	if mgr.ActiveCount() != 0 {
		t.Fatalf("active connections = %d, want 0", mgr.ActiveCount())
	}
}

func TestDisconnectRequestSourcesEstablishInboundBarrier(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger func(*Conn) error
	}{
		{
			name: "protocol error",
			trigger: func(conn *Conn) error {
				closeProtocolError(conn, CloseReasonMalformedMessage)
				return nil
			},
		},
		{
			name: "idle timeout",
			trigger: func(conn *Conn) error {
				conn.requestDisconnect(ClosePolicy, CloseReasonIdleTimeout)
				return nil
			},
		},
		{
			name: "outbound backpressure",
			trigger: func(conn *Conn) error {
				for len(conn.OutboundCh) < cap(conn.OutboundCh) {
					conn.OutboundCh <- []byte{0}
				}
				return SendToConn(conn, SubscriptionError{Error: "overflow"})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := testConnDirect(nil)
			err := tc.trigger(conn)
			if tc.name == "outbound backpressure" && !errors.Is(err, ErrClientBufferFull) {
				t.Fatalf("trigger error = %v, want ErrClientBufferFull", err)
			}
			select {
			case <-conn.inboundDone():
			default:
				t.Fatal("disconnect request left inbound admission open")
			}
			if conn.beginInboundHandler() {
				conn.finishInboundHandler()
				t.Fatal("disconnect request still admitted an inbound handler")
			}
		})
	}
}
