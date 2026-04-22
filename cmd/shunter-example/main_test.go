package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
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
