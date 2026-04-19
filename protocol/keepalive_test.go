package protocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/types"
)

// loopbackConn pairs a server-side *Conn with a client-side
// *websocket.Conn via an httptest.Server. The returned cleanup closes
// both ends.
func loopbackConn(t *testing.T, opts ProtocolOptions) (*Conn, *websocket.Conn, func()) {
	t.Helper()
	serverReady := make(chan *websocket.Conn, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		serverReady <- ws
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	u := strings.Replace(srv.URL, "http://", "ws://", 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	clientWS, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	var serverWS *websocket.Conn
	select {
	case serverWS = <-serverReady:
	case <-time.After(2 * time.Second):
		t.Fatal("server-side ws not ready")
	}

	id := GenerateConnectionID()
	conn := NewConn(id, types.Identity{}, "", false, serverWS, &opts)
	cleanup := func() {
		_ = clientWS.Close(websocket.StatusNormalClosure, "")
		_ = serverWS.Close(websocket.StatusNormalClosure, "")
	}
	return conn, clientWS, cleanup
}

// runKeepaliveAsync starts runKeepalive on a background goroutine and
// returns a done-signal channel the caller can wait on.
func runKeepaliveAsync(c *Conn, ctx context.Context) chan struct{} {
	done := make(chan struct{})
	go func() {
		c.runKeepalive(ctx)
		close(done)
	}()
	return done
}

func TestKeepaliveReturnsOnCtxCancel(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.PingInterval = 2 * time.Second // so no tick fires during the test
	opts.IdleTimeout = 4 * time.Second
	c, _, cleanup := loopbackConn(t, opts)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	done := runKeepaliveAsync(c, ctx)

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runKeepalive did not return on ctx cancel")
	}
}

func TestKeepaliveReturnsOnClosedChan(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.PingInterval = 2 * time.Second
	opts.IdleTimeout = 4 * time.Second
	c, _, cleanup := loopbackConn(t, opts)
	defer cleanup()

	done := runKeepaliveAsync(c, context.Background())
	close(c.closed)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("runKeepalive did not return on closed channel")
	}
}

func TestKeepaliveMarksActivityOnPongFromResponsiveClient(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.PingInterval = 50 * time.Millisecond
	opts.IdleTimeout = 1 * time.Second // generous; don't trip idle in this test
	c, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	// Drive the client's read side so coder/websocket internal
	// bookkeeping processes Ping frames and answers with Pong. The
	// read goroutine stops once we cancel the ctx at test end.
	clientReadCtx, clientReadCancel := context.WithCancel(context.Background())
	var readerWG sync.WaitGroup
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for {
			if _, _, err := clientWS.Read(clientReadCtx); err != nil {
				return
			}
		}
	}()

	start := time.Now().UnixNano()

	ctx, cancel := context.WithCancel(context.Background())
	// Story 3.5 needs BOTH goroutines: runDispatchLoop consumes control
	// frames so Ping(ctx) can complete; runKeepalive fires the Ping.
	handlers := &MessageHandlers{}
	pumpDone := runDispatchAsync(c, ctx, handlers)
	kaDone := runKeepaliveAsync(c, ctx)

	time.Sleep(250 * time.Millisecond) // ~4 PingInterval cycles

	last := c.lastActivity.Load()
	if last <= start {
		t.Errorf("lastActivity = %d, want > %d (expected Pong-driven activity)", last, start)
	}

	cancel()
	clientReadCancel()
	<-kaDone
	<-pumpDone
	readerWG.Wait()
}

func TestKeepaliveClosesIdleConnection(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.PingInterval = 30 * time.Millisecond
	opts.IdleTimeout = 120 * time.Millisecond
	c, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	// Client does NOT read. With coder/websocket, Pong replies flow
	// through the peer's read loop — if the client never reads, Pings
	// pile up unanswered and the server's Ping(ctx) times out. After
	// IdleTimeout elapses with no activity, runKeepalive must close.

	ctx := context.Background()
	done := runKeepaliveAsync(c, ctx)

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("runKeepalive did not close idle connection within the expected window")
	}

	// Client observes the policy-violation close.
	readCtx, readCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer readCancel()
	_, _, err := clientWS.Read(readCtx)
	if err == nil {
		t.Fatal("client expected a close frame on idle timeout")
	}
	if code := websocket.CloseStatus(err); code != websocket.StatusPolicyViolation {
		t.Errorf("close code = %d, want %d (StatusPolicyViolation)", code, websocket.StatusPolicyViolation)
	}
}

func TestDispatchLoopMarksActivityOnInboundFrame(t *testing.T) {
	opts := DefaultProtocolOptions()
	c, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handled := make(chan struct{})
	handlers := &MessageHandlers{
		OnSubscribe: func(_ context.Context, _ *Conn, _ *SubscribeSingleMsg) {
			close(handled)
		},
	}
	pumpDone := runDispatchAsync(c, ctx, handlers)

	// Give the dispatch loop a moment to start and sample a baseline
	// AFTER NewConn's construction MarkActivity so we can detect movement.
	time.Sleep(10 * time.Millisecond)
	before := c.lastActivity.Load()

	// Client sends a valid Subscribe frame. The dispatch loop routes it
	// and must still MarkActivity before handler dispatch.
	frame, err := EncodeClientMessage(SubscribeSingleMsg{
		RequestID: 1,
		QueryID:   1,
		Query:     Query{TableName: "events"},
	})
	if err != nil {
		t.Fatalf("encode subscribe: %v", err)
	}
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer writeCancel()
	if err := clientWS.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("client write: %v", err)
	}

	select {
	case <-handled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("dispatch loop did not invoke subscribe handler")
	}
	if c.lastActivity.Load() <= before {
		t.Errorf("MarkActivity was not called on inbound frame (before=%d, after=%d)",
			before, c.lastActivity.Load())
	}

	cancel()
	<-pumpDone
}

func TestDispatchLoopExitsOnReadError(t *testing.T) {
	opts := DefaultProtocolOptions()
	c, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	handlers := &MessageHandlers{}
	pumpDone := runDispatchAsync(c, context.Background(), handlers)

	// Client closes. Server Read returns an error; dispatch loop must exit.
	_ = clientWS.Close(websocket.StatusNormalClosure, "")

	select {
	case <-pumpDone:
	case <-time.After(1 * time.Second):
		t.Fatal("runDispatchLoop did not exit on peer close")
	}
}

func TestMarkActivityUpdatesTimestamp(t *testing.T) {
	opts := DefaultProtocolOptions()
	c, _, cleanup := loopbackConn(t, opts)
	defer cleanup()

	c.lastActivity.Store(1)
	c.MarkActivity()
	if got := c.lastActivity.Load(); got < time.Now().Add(-time.Second).UnixNano() {
		t.Errorf("MarkActivity did not update lastActivity (got %d)", got)
	}
}
