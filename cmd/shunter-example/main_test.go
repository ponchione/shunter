package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/protocol"
)

func TestHostedHello_BootstrapThenRecover(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	rt, err := buildHelloRuntime(dir, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("first buildHelloRuntime: %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	rt2, err := buildHelloRuntime(dir, "127.0.0.1:0")
	if err != nil {
		t.Fatalf("second buildHelloRuntime: %v", err)
	}
	if err := rt2.Start(ctx); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if err := rt2.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestHostedHello_AdmitsDevConnection(t *testing.T) {
	rt := mustBuildAndStartHelloRuntime(t)
	defer rt.Close()

	srv := httptest.NewServer(rt.HTTPHandler())
	defer srv.Close()

	conn := dialHostedHello(t, srv.URL)
	defer conn.Close(websocket.StatusNormalClosure, "")
}

func TestHostedHello_SubscriberReceivesReducerInsert(t *testing.T) {
	rt := mustBuildAndStartHelloRuntime(t)
	defer rt.Close()

	srv := httptest.NewServer(rt.HTTPHandler())
	defer srv.Close()

	subscriber := dialHostedHello(t, srv.URL)
	defer subscriber.Close(websocket.StatusNormalClosure, "")
	caller := dialHostedHello(t, srv.URL)
	defer caller.Close(websocket.StatusNormalClosure, "")

	writeClientMessage(t, subscriber, protocol.SubscribeSingleMsg{
		RequestID:   1,
		QueryID:     1,
		QueryString: "SELECT * FROM greetings",
	})
	applied, ok := readUntilTag(t, subscriber, protocol.TagSubscribeSingleApplied).(protocol.SubscribeSingleApplied)
	if !ok {
		t.Fatalf("subscriber: unexpected SubscribeSingleApplied shape")
	}
	if applied.TableName != "greetings" {
		t.Fatalf("subscriber applied TableName = %q, want greetings", applied.TableName)
	}

	writeClientMessage(t, caller, protocol.CallReducerMsg{
		RequestID:   2,
		ReducerName: "say_hello",
		Args:        []byte("hola"),
		Flags:       protocol.CallReducerFlagsFullUpdate,
	})

	light, ok := readUntilTag(t, subscriber, protocol.TagTransactionUpdateLight).(protocol.TransactionUpdateLight)
	if !ok {
		t.Fatalf("subscriber: unexpected TransactionUpdateLight shape")
	}
	if len(light.Update) != 1 {
		t.Fatalf("light.Update len = %d, want 1", len(light.Update))
	}
	if light.Update[0].TableName != "greetings" {
		t.Fatalf("light update table = %q, want greetings", light.Update[0].TableName)
	}
	if len(light.Update[0].Inserts) == 0 {
		t.Fatal("light update inserts empty, want encoded row")
	}
}

func TestHostedHello_RunShutsDownCleanlyOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() { errCh <- run(ctx, "127.0.0.1:0", dir) }()

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

func TestHostedHello_DoesNotManuallyAssembleKernelGraph(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{
		"commitlog.NewDurabilityWorker",
		"executor.NewExecutor",
		"executor.NewReducerRegistry",
		"subscription.NewManager",
		"subscription.NewFanOutWorker",
		"protocol.NewConnManager",
		"protocol.NewClientSender",
		"protocol.Server{",
		"schema.NewBuilder",
	}
	for _, needle := range forbidden {
		if strings.Contains(string(src), needle) {
			t.Fatalf("hosted example must not manually assemble kernel graph; found %q", needle)
		}
	}
}

func mustBuildAndStartHelloRuntime(t *testing.T) interface {
	HTTPHandler() http.Handler
	Close() error
} {
	t.Helper()
	rt, err := buildHelloRuntime(t.TempDir(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("buildHelloRuntime: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return rt
}

func dialHostedHello(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	wsURL := strings.Replace(serverURL, "http://", "ws://", 1) + "/subscribe"
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		Subprotocols: []string{"v1.bsatn.spacetimedb"},
	})
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status = %d, want 101", resp.StatusCode)
	}
	readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readCancel()
	if _, _, err := conn.Read(readCtx); err != nil {
		t.Fatalf("read IdentityToken: %v", err)
	}
	return conn
}

func writeClientMessage(t *testing.T, conn *websocket.Conn, msg any) {
	t.Helper()
	frame, err := protocol.EncodeClientMessage(msg)
	if err != nil {
		t.Fatalf("EncodeClientMessage: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write client message: %v", err)
	}
}

func readUntilTag(t *testing.T, conn *websocket.Conn, tag uint8) any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		_, frame, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read server message: %v", err)
		}
		gotTag, msg, err := protocol.DecodeServerMessage(frame)
		if err != nil {
			t.Fatalf("DecodeServerMessage: %v", err)
		}
		if gotTag == tag {
			return msg
		}
	}
	t.Fatalf("timeout waiting for tag=%d", tag)
	return nil
}
