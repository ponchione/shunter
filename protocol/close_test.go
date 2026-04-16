package protocol

import (
	"context"
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

	supervised := make(chan struct{})
	go func() {
		conn.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr, dispatchDone, keepaliveDone)
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

func TestCloseWithHandshake_UnresponsivePeerTimesOut(t *testing.T) {
	conn, _, cleanup := loopbackConn(t, DefaultProtocolOptions())
	defer cleanup()

	// Client does NOT read — close handshake will time out.
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
		t.Fatal("closeWithHandshake did not time out")
	}

	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("closeWithHandshake took %v, expected ~%v", elapsed, timeout)
	}
}
