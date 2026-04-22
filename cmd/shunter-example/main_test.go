package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/protocol"
)

// TestBuildEngine_BootstrapThenRecover exercises the cold-boot (ErrNoData →
// initial snapshot) and recovery paths on a single tempdir. A second
// buildEngine call must succeed against the same directory.
func TestBuildEngine_BootstrapThenRecover(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	graph, err := buildEngine(ctx, dir)
	if err != nil {
		t.Fatalf("first buildEngine: %v", err)
	}
	graph.shutdown()

	graph2, err := buildEngine(ctx, dir)
	if err != nil {
		t.Fatalf("second buildEngine: %v", err)
	}
	graph2.shutdown()
}

// TestBuildEngine_AdmitsAnonymousConnection stands the server up, upgrades a
// WebSocket on /subscribe, and confirms the anonymous-auth lifecycle admits
// the connection. This pins the full schema → executor → protocol path.
func TestBuildEngine_AdmitsAnonymousConnection(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	graph, err := buildEngine(ctx, dir)
	if err != nil {
		t.Fatalf("buildEngine: %v", err)
	}
	defer graph.shutdown()

	mux := http.NewServeMux()
	mux.HandleFunc("/subscribe", graph.server.HandleSubscribe)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/subscribe"
	dialCtx, dialCancel := context.WithTimeout(ctx, 2*time.Second)
	defer dialCancel()

	conn, resp, err := websocket.Dial(dialCtx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"v1.bsatn.spacetimedb"},
	})
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %d, want 101", resp.StatusCode)
	}

	// Consume InitialConnection frame so the socket advances past handshake
	// before teardown. Nothing to assert beyond "a frame arrived".
	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	if _, _, err := conn.Read(readCtx); err != nil {
		t.Fatalf("read InitialConnection: %v", err)
	}
}

// TestFanOut_SubscriberReceivesReducerInsert pins the full subscription
// fan-out path end-to-end. Client A subscribes to `SELECT * FROM greetings`
// and Client B calls the `say_hello` reducer; the insert must arrive on A
// as a `TransactionUpdateLight` delta. Pre-wiring, the example ran with the
// noop SubscriptionManager and no delta was ever produced, so this pin
// guards against fan-out regression.
func TestFanOut_SubscriberReceivesReducerInsert(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	graph, err := buildEngine(ctx, dir)
	if err != nil {
		t.Fatalf("buildEngine: %v", err)
	}
	defer graph.shutdown()

	mux := http.NewServeMux()
	mux.HandleFunc("/subscribe", graph.server.HandleSubscribe)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/subscribe"
	dialOpts := &websocket.DialOptions{Subprotocols: []string{"v1.bsatn.spacetimedb"}}

	dial := func(name string) *websocket.Conn {
		t.Helper()
		dialCtx, dialCancel := context.WithTimeout(ctx, 2*time.Second)
		defer dialCancel()
		c, _, err := websocket.Dial(dialCtx, wsURL, dialOpts)
		if err != nil {
			t.Fatalf("%s dial: %v", name, err)
		}
		// Consume InitialConnection so subsequent reads land on
		// post-handshake frames.
		readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
		defer readCancel()
		if _, _, err := c.Read(readCtx); err != nil {
			t.Fatalf("%s read InitialConnection: %v", name, err)
		}
		return c
	}
	write := func(name string, c *websocket.Conn, msg any) {
		t.Helper()
		frame, err := protocol.EncodeClientMessage(msg)
		if err != nil {
			t.Fatalf("%s encode: %v", name, err)
		}
		writeCtx, writeCancel := context.WithTimeout(ctx, 2*time.Second)
		defer writeCancel()
		if err := c.Write(writeCtx, websocket.MessageBinary, frame); err != nil {
			t.Fatalf("%s write: %v", name, err)
		}
	}
	readUntilTag := func(name string, c *websocket.Conn, tag uint8) any {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			readCtx, readCancel := context.WithDeadline(ctx, deadline)
			_, frame, err := c.Read(readCtx)
			readCancel()
			if err != nil {
				t.Fatalf("%s read: %v", name, err)
			}
			gotTag, msg, err := protocol.DecodeServerMessage(frame)
			if err != nil {
				t.Fatalf("%s decode: %v", name, err)
			}
			if gotTag == tag {
				return msg
			}
			// Non-target frame (e.g. SubscribeSingleApplied received
			// while waiting for TransactionUpdateLight). Swallow and
			// continue reading.
		}
		t.Fatalf("%s timeout waiting for tag=%d", name, tag)
		return nil
	}

	subscriber := dial("subscriber")
	defer subscriber.Close(websocket.StatusNormalClosure, "")
	caller := dial("caller")
	defer caller.Close(websocket.StatusNormalClosure, "")

	write("subscriber", subscriber, protocol.SubscribeSingleMsg{
		RequestID:   1,
		QueryID:     1,
		QueryString: "SELECT * FROM greetings",
	})
	applied, ok := readUntilTag("subscriber", subscriber, protocol.TagSubscribeSingleApplied).(protocol.SubscribeSingleApplied)
	if !ok {
		t.Fatalf("subscriber: unexpected Applied shape")
	}
	if applied.TableName != "greetings" {
		t.Fatalf("subscriber Applied TableName = %q, want greetings", applied.TableName)
	}

	write("caller", caller, protocol.CallReducerMsg{
		RequestID:   2,
		ReducerName: "say_hello",
		Args:        []byte("hola"),
		Flags:       protocol.CallReducerFlagsFullUpdate,
	})

	light, ok := readUntilTag("subscriber", subscriber, protocol.TagTransactionUpdateLight).(protocol.TransactionUpdateLight)
	if !ok {
		t.Fatalf("subscriber: unexpected Light shape")
	}
	if len(light.Update) != 1 {
		t.Fatalf("light.Update len = %d, want 1", len(light.Update))
	}
	if light.Update[0].TableName != "greetings" {
		t.Fatalf("light update table = %q, want greetings", light.Update[0].TableName)
	}
	// Non-empty Inserts payload proves the reducer's row round-tripped
	// through the fan-out adapter's BSATN encoder; we intentionally do
	// not decode here — a zero-length body would indicate the fan-out
	// wiring dropped the delta.
	if len(light.Update[0].Inserts) == 0 {
		t.Fatalf("light update inserts empty, want encoded row")
	}
}

// TestRun_ShutsDownCleanlyOnContextCancel spawns run() and cancels its
// context. run() must return nil and release all resources (durability
// Close, executor Shutdown, http Shutdown).
func TestRun_ShutsDownCleanlyOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx, "127.0.0.1:0", dir) }()

	// Give the goroutine a beat to start listening before cancelling.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return within 5s after cancel")
	}
}
