package protocol

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/auth"
	"github.com/ponchione/shunter/types"
	"github.com/ponchione/websocket"
)

type lifecycleCloseObserver struct {
	mu      sync.Mutex
	reasons []string
}

func (*lifecycleCloseObserver) RecordProtocolConnections(int)                  {}
func (*lifecycleCloseObserver) RecordProtocolMessage(string, string)           {}
func (*lifecycleCloseObserver) LogProtocolConnectionRejected(string, error)    {}
func (*lifecycleCloseObserver) LogProtocolConnectionOpened(types.ConnectionID) {}
func (*lifecycleCloseObserver) LogProtocolProtocolError(string, string, error) {}
func (*lifecycleCloseObserver) LogProtocolAuthFailed(string, error)            {}
func (*lifecycleCloseObserver) LogProtocolBackpressure(string, string)         {}
func (o *lifecycleCloseObserver) LogProtocolConnectionClosed(_ types.ConnectionID, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reasons = append(o.reasons, reason)
}

func (o *lifecycleCloseObserver) waitForSingleReason(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		got := append([]string(nil), o.reasons...)
		o.mu.Unlock()
		if len(got) == 1 && got[0] == want {
			return
		}
		if len(got) > 1 {
			t.Fatalf("close telemetry = %v, want one %q event", got, want)
		}
		time.Sleep(time.Millisecond)
	}
	o.mu.Lock()
	got := append([]string(nil), o.reasons...)
	o.mu.Unlock()
	t.Fatalf("close telemetry = %v, want [%s]", got, want)
}

type blockingCallInbox struct {
	fakeInbox
	started chan struct{}
}

func (b *blockingCallInbox) CallReducer(ctx context.Context, _ CallReducerRequest) error {
	select {
	case b.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

type panicCallInbox struct{ fakeInbox }

func (*panicCallInbox) CallReducer(context.Context, CallReducerRequest) error {
	panic("handler boom")
}

func liveLifecycleServer(executor ExecutorInbox, opts ProtocolOptions, observer Observer) (*Server, *ConnManager) {
	mgr := NewConnManager()
	return &Server{
		JWT: &auth.JWTConfig{
			SigningKey: testSigningKey,
			AuthMode:   auth.AuthModeStrict,
		},
		Options:  opts,
		Executor: executor,
		Conns:    mgr,
		Observer: observer,
	}, mgr
}

func dialLiveLifecycle(t *testing.T, server *Server) *websocket.Conn {
	t.Helper()
	srv := newTestServer(t, server)
	conn, resp, err := dialSubscribe(t, srv)
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	if _, err := readOneBinary(t, conn, 2*time.Second); err != nil {
		t.Fatalf("read IdentityToken: %v", err)
	}
	return conn
}

func requireLiveLifecycleClose(t *testing.T, conn *websocket.Conn, observer *lifecycleCloseObserver, mgr *ConnManager, code websocket.StatusCode, reason, taxonomy string) {
	t.Helper()
	readCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	messageType, data, err := conn.Read(readCtx)
	if err == nil {
		t.Fatalf("expected close frame, got message type %v data %x", messageType, data)
	}
	if got := websocket.CloseStatus(err); got != code {
		t.Fatalf("close code = %d, want %d (err=%v)", got, code, err)
	}
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("read error = %T, want websocket.CloseError", err)
	}
	if closeErr.Reason != reason {
		t.Fatalf("close reason = %q, want %q", closeErr.Reason, reason)
	}
	observer.waitForSingleReason(t, taxonomy)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.ActiveCount() == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("active connections = %d, want 0 after close", mgr.ActiveCount())
}

func TestHandleSubscribeFatalPathsUseLifecycleDisconnectOwner(t *testing.T) {
	t.Run("text frame", func(t *testing.T) {
		inbox := &fakeInbox{}
		observer := &lifecycleCloseObserver{}
		server, mgr := liveLifecycleServer(inbox, DefaultProtocolOptions(), observer)
		conn := dialLiveLifecycle(t, server)
		writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := conn.Write(writeCtx, websocket.MessageText, []byte("unsupported")); err != nil {
			t.Fatal(err)
		}
		requireLiveLifecycleClose(t, conn, observer, mgr, CloseProtocol, CloseReasonTextFrameUnsupported, "protocol_error")
	})

	t.Run("malformed tag", func(t *testing.T) {
		inbox := &fakeInbox{}
		observer := &lifecycleCloseObserver{}
		server, mgr := liveLifecycleServer(inbox, DefaultProtocolOptions(), observer)
		conn := dialLiveLifecycle(t, server)
		frame := []byte{0xff}
		_, _, decodeErr := DecodeClientMessageForVersion(ProtocolVersionV1, frame)
		if decodeErr == nil {
			t.Fatal("malformed test tag decoded successfully")
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := conn.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
			t.Fatal(err)
		}
		requireLiveLifecycleClose(t, conn, observer, mgr, CloseProtocol, decodeErr.Error(), "protocol_error")
	})

	t.Run("inbound overflow", func(t *testing.T) {
		inbox := &blockingCallInbox{started: make(chan struct{}, 1)}
		observer := &lifecycleCloseObserver{}
		opts := DefaultProtocolOptions()
		opts.IncomingQueueMessages = 1
		server, mgr := liveLifecycleServer(inbox, opts, observer)
		conn := dialLiveLifecycle(t, server)
		first, err := EncodeClientMessage(CallReducerMsg{ReducerName: "block", RequestID: 1})
		if err != nil {
			t.Fatal(err)
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := conn.Write(writeCtx, websocket.MessageBinary, first); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		select {
		case <-inbox.started:
		case <-time.After(time.Second):
			t.Fatal("first handler did not block in executor")
		}
		second, err := EncodeClientMessage(CallReducerMsg{ReducerName: "overflow", RequestID: 2})
		if err != nil {
			t.Fatal(err)
		}
		writeCtx, cancel = context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := conn.Write(writeCtx, websocket.MessageBinary, second); err != nil {
			t.Fatal(err)
		}
		requireLiveLifecycleClose(t, conn, observer, mgr, ClosePolicy, CloseReasonTooManyRequests, "policy_violation")
	})

	t.Run("idle timeout", func(t *testing.T) {
		inbox := &fakeInbox{}
		observer := &lifecycleCloseObserver{}
		opts := DefaultProtocolOptions()
		opts.PingInterval = 40 * time.Millisecond
		opts.IdleTimeout = 10 * time.Millisecond
		server, mgr := liveLifecycleServer(inbox, opts, observer)
		conn := dialLiveLifecycle(t, server)
		time.Sleep(120 * time.Millisecond)
		requireLiveLifecycleClose(t, conn, observer, mgr, ClosePolicy, CloseReasonIdleTimeout, "idle_timeout")
	})

	t.Run("handler panic", func(t *testing.T) {
		inbox := &panicCallInbox{}
		observer := &lifecycleCloseObserver{}
		server, mgr := liveLifecycleServer(inbox, DefaultProtocolOptions(), observer)
		conn := dialLiveLifecycle(t, server)
		frame, err := EncodeClientMessage(CallReducerMsg{ReducerName: "panic", RequestID: 1})
		if err != nil {
			t.Fatal(err)
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := conn.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
			t.Fatal(err)
		}
		requireLiveLifecycleClose(t, conn, observer, mgr, CloseInternal, "internal error", "internal_error")
		onDisconnect, disconnectSubs, _ := inbox.disconnectSnapshot()
		if onDisconnect != 1 || disconnectSubs != 1 {
			t.Fatalf("panic teardown calls = (disconnect=%d, subscriptions=%d), want (1, 1)", onDisconnect, disconnectSubs)
		}
	})
}
