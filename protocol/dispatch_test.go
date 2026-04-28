package protocol

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testConnPair creates a httptest server with websocket.Accept, dials
// from the client side, and wraps the server conn in a *Conn. Returns
// the server *Conn and client *websocket.Conn.
func testConnPair(t *testing.T, opts *ProtocolOptions) (*Conn, *websocket.Conn) {
	t.Helper()
	if opts == nil {
		o := DefaultProtocolOptions()
		opts = &o
	}
	var serverConn *websocket.Conn
	ready := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer c.CloseNow()
		serverConn = c
		close(ready)
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	ctx := context.Background()
	client, _, err := websocket.Dial(ctx, srv.URL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { client.CloseNow() })

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server accept")
	}

	conn := NewConn(GenerateConnectionID(), [32]byte{1}, "test-token", false, serverConn, opts)
	return conn, client
}

// runDispatchAsync starts runDispatchLoop in a background goroutine and
// returns a channel that closes when the loop exits.
func runDispatchAsync(c *Conn, ctx context.Context, handlers *MessageHandlers) chan struct{} {
	done := make(chan struct{})
	go func() {
		c.runDispatchLoop(ctx, handlers)
		close(done)
	}()
	return done
}

func TestDispatchLoop_ValidSubscribe(t *testing.T) {
	conn, client := testConnPair(t, nil)

	var mu sync.Mutex
	var gotQueryID uint32
	called := make(chan struct{})

	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			mu.Lock()
			gotQueryID = msg.QueryID
			mu.Unlock()
			close(called)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Build and send a valid Subscribe frame.
	subMsg := SubscribeSingleMsg{
		RequestID:   1,
		QueryID:     42,
		QueryString: "SELECT * FROM users",
	}
	frame, err := EncodeClientMessage(subMsg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	defer writeCancel()
	if err := client.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("client write: %v", err)
	}

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("OnSubscribe was not called within timeout")
	}

	mu.Lock()
	if gotQueryID != 42 {
		t.Errorf("QueryID = %d, want 42", gotQueryID)
	}
	mu.Unlock()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatch loop did not exit after cancel")
	}
}

func TestDispatchLoop_TextFrameCloses(t *testing.T) {
	conn, client := testConnPair(t, nil)

	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			t.Error("OnSubscribe should not be called for text frame")
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Send a text frame; dispatch should close with 1002.
	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	defer writeCancel()
	if err := client.Write(writeCtx, websocket.MessageText, []byte("hello")); err != nil {
		t.Fatalf("client write: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on text frame")
	}

	// Verify client received 1002 close code.
	readCtx, rCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rCancel()
	_, _, readErr := client.Read(readCtx)
	if code := websocket.CloseStatus(readErr); code != websocket.StatusProtocolError {
		t.Errorf("close code = %d, want %d (StatusProtocolError)", code, websocket.StatusProtocolError)
	}
}

func TestDispatchLoop_UnknownTagCloses(t *testing.T) {
	conn, client := testConnPair(t, nil)

	handlers := &MessageHandlers{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Send a binary frame with unknown tag 0xFF.
	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	defer writeCancel()
	if err := client.Write(writeCtx, websocket.MessageBinary, []byte{0xFF, 0x00, 0x00}); err != nil {
		t.Fatalf("client write: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on unknown tag")
	}
}

func TestDispatchLoop_UnknownTagLogsDetail(t *testing.T) {
	conn, client := testConnPair(t, nil)

	handlers := &MessageHandlers{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	var logs bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	}()

	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	defer writeCancel()
	if err := client.Write(writeCtx, websocket.MessageBinary, []byte{0xFF, 0x00, 0x00}); err != nil {
		t.Fatalf("client write: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on unknown tag")
	}

	got := logs.String()
	if got == "" {
		t.Fatal("expected protocol error log output, got empty log")
	}
	if !bytes.Contains([]byte(got), []byte("unknown message tag")) {
		t.Fatalf("log output %q does not mention unknown message tag", got)
	}
}

func TestDispatchLoop_NilHandlerCloses(t *testing.T) {
	conn, client := testConnPair(t, nil)

	// All handler fields are nil.
	handlers := &MessageHandlers{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Send a valid Subscribe frame — but OnSubscribe is nil.
	subMsg := SubscribeSingleMsg{
		RequestID:   1,
		QueryID:     10,
		QueryString: "SELECT * FROM items",
	}
	frame, err := EncodeClientMessage(subMsg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	defer writeCancel()
	if err := client.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("client write: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on nil handler")
	}
}

func TestDispatchLoop_MalformedBodyCloses(t *testing.T) {
	conn, client := testConnPair(t, nil)

	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			t.Error("OnSubscribe should not be called for malformed body")
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := runDispatchAsync(conn, ctx, handlers)

	// Valid Subscribe tag but truncated body (only 2 bytes after tag).
	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	defer writeCancel()
	if err := client.Write(writeCtx, websocket.MessageBinary, []byte{TagSubscribeSingle, 0x01, 0x02}); err != nil {
		t.Fatalf("client write: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch loop did not exit on malformed body")
	}
}

func TestDispatchLoop_MarksActivity(t *testing.T) {
	conn, client := testConnPair(t, nil)

	handled := make(chan struct{})
	handlers := &MessageHandlers{
		OnSubscribeSingle: func(ctx context.Context, c *Conn, msg *SubscribeSingleMsg) {
			close(handled)
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Let the initial MarkActivity from NewConn settle, then sample.
	time.Sleep(10 * time.Millisecond)
	before := conn.lastActivity.Load()

	done := runDispatchAsync(conn, ctx, handlers)

	subMsg := SubscribeSingleMsg{
		RequestID:   1,
		QueryID:     7,
		QueryString: "SELECT * FROM events",
	}
	frame, err := EncodeClientMessage(subMsg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	writeCtx, writeCancel := context.WithTimeout(context.Background(), time.Second)
	defer writeCancel()
	if err := client.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("client write: %v", err)
	}

	// Wait for handler to fire so we know Read completed.
	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called within timeout")
	}

	after := conn.lastActivity.Load()
	if after <= before {
		t.Errorf("lastActivity not updated: before=%d after=%d", before, after)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatch loop did not exit after cancel")
	}
}
