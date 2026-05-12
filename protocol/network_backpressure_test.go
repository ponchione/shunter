package protocol

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/types"
)

func TestWebSocketSlowReaderWriteTimeoutDoesNotBlockUnrelatedFanout(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.WriteTimeout = 150 * time.Millisecond
	opts.CloseHandshakeTimeout = 50 * time.Millisecond
	opts.DisconnectTimeout = 500 * time.Millisecond
	opts.OutgoingBufferMessages = 1

	inbox := &fakeInbox{}
	mgr := NewConnManager()

	slowConn, _, cleanupSlow := loopbackConnWithTCPBuffers(t, opts, 1024)
	defer cleanupSlow()
	if err := mgr.Add(slowConn); err != nil {
		t.Fatalf("register slow connection: %v", err)
	}
	slowWriterCtx, slowWriterCancel := context.WithCancel(context.Background())
	defer slowWriterCancel()
	slowOutboundDone := make(chan struct{})
	go func() {
		slowConn.runOutboundWriter(slowWriterCtx)
		close(slowOutboundDone)
	}()
	slowSupervised := superviseSyntheticConn(t, slowConn, inbox, mgr, slowOutboundDone)

	goodConn, goodClient, cleanupGood := loopbackConn(t, opts)
	defer cleanupGood()
	if err := mgr.Add(goodConn); err != nil {
		t.Fatalf("register good connection: %v", err)
	}
	goodWriterCtx, goodWriterCancel := context.WithCancel(context.Background())
	defer goodWriterCancel()
	goodOutboundDone := make(chan struct{})
	go func() {
		goodConn.runOutboundWriter(goodWriterCtx)
		close(goodOutboundDone)
	}()

	slowFrame := make([]byte, 8<<20)
	for i := range slowFrame {
		slowFrame[i] = byte(i)
	}

	slowPressureReady := make(chan struct{})
	slowFirstSend := make(chan time.Time, 1)
	slowPressureDone := make(chan struct{})
	go func() {
		defer close(slowPressureDone)
		sent := 0
		for {
			select {
			case <-slowOutboundDone:
				return
			case slowConn.OutboundCh <- slowFrame:
				sent++
				if sent == 1 {
					slowFirstSend <- time.Now()
				}
				if sent == 2 {
					close(slowPressureReady)
				}
			}
		}
	}()

	var slowStart time.Time
	select {
	case slowStart = <-slowFirstSend:
	case <-time.After(time.Second):
		t.Fatal("slow client pressure did not enqueue first frame")
	}
	select {
	case <-slowPressureReady:
	case <-slowOutboundDone:
		t.Fatal("slow outbound writer exited before its queue showed unread-client pressure")
	case <-time.After(time.Second):
		t.Fatal("slow outbound writer did not consume the first unread-client frame")
	}

	sender := NewClientSender(mgr, inbox)
	rows := EncodeRowList([][]byte{{0x01, 0x02, 0x03}})
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		goodConn.ID: {{
			QueryID:   7,
			TableName: "orders",
			Inserts:   rows,
			Deletes:   EncodeRowList(nil),
		}},
	}
	if errs := DeliverTransactionUpdateLight(sender, mgr, 33, fanout); len(errs) != 0 {
		t.Fatalf("unrelated fanout errors: %v", errs)
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), time.Second)
	defer readCancel()
	mt, frame, err := goodClient.Read(readCtx)
	if err != nil {
		t.Fatalf("good client read fanout: %v", err)
	}
	if mt != websocket.MessageBinary {
		t.Fatalf("good client message type = %v, want binary", mt)
	}
	tag, decoded, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("decode good client fanout: %v", err)
	}
	if tag != TagTransactionUpdateLight {
		t.Fatalf("good client tag = %d, want %d", tag, TagTransactionUpdateLight)
	}
	update := decoded.(TransactionUpdateLight)
	if update.RequestID != 33 || len(update.Update) != 1 || update.Update[0].QueryID != 7 {
		t.Fatalf("good client update = %+v, want request 33/query 7", update)
	}

	select {
	case <-slowOutboundDone:
	case <-time.After(2 * time.Second):
		t.Fatal("slow outbound writer did not exit after WriteTimeout")
	}
	if elapsed := time.Since(slowStart); elapsed < opts.WriteTimeout {
		t.Fatalf("slow outbound writer exited in %v, before WriteTimeout %v", elapsed, opts.WriteTimeout)
	}
	select {
	case <-slowPressureDone:
	case <-time.After(time.Second):
		t.Fatal("slow pressure goroutine did not stop after outbound writer exit")
	}
	select {
	case <-slowSupervised:
	case <-time.After(time.Second):
		t.Fatal("slow connection supervisor did not complete after writer timeout")
	}
	if got := mgr.Get(slowConn.ID); got != nil {
		t.Fatalf("slow connection still registered after writer timeout: %p", got)
	}
	if got := mgr.Get(goodConn.ID); got != goodConn {
		t.Fatalf("good connection manager entry = %p, want %p", got, goodConn)
	}

	goodWriterCancel()
	select {
	case <-goodOutboundDone:
	case <-time.After(time.Second):
		t.Fatal("good outbound writer did not stop")
	}
}

type tcpBufferListener struct {
	net.Listener
	bufferBytes int
}

func (l tcpBufferListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(l.bufferBytes)
		_ = tcp.SetWriteBuffer(l.bufferBytes)
		_ = tcp.SetNoDelay(true)
	}
	return conn, nil
}

func loopbackConnWithTCPBuffers(t *testing.T, opts ProtocolOptions, bufferBytes int) (*Conn, *websocket.Conn, func()) {
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
	srv := httptest.NewUnstartedServer(handler)
	if err := srv.Listener.Close(); err != nil {
		t.Fatalf("close default test listener: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv.Listener = tcpBufferListener{Listener: ln, bufferBytes: bufferBytes}
	srv.Start()
	t.Cleanup(srv.Close)

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var dialer net.Dialer
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetReadBuffer(bufferBytes)
				_ = tcp.SetWriteBuffer(bufferBytes)
				_ = tcp.SetNoDelay(true)
			}
			return conn, nil
		},
	}
	t.Cleanup(transport.CloseIdleConnections)

	u := strings.Replace(srv.URL, "http://", "ws://", 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	clientWS, _, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	var serverWS *websocket.Conn
	select {
	case serverWS = <-serverReady:
	case <-time.After(2 * time.Second):
		t.Fatal("server-side ws not ready")
	}

	conn := NewConn(GenerateConnectionID(), types.Identity{}, "", false, serverWS, &opts)
	cleanup := func() {
		_ = clientWS.Close(websocket.StatusNormalClosure, "")
		_ = serverWS.Close(websocket.StatusNormalClosure, "")
	}
	return conn, clientWS, cleanup
}

func superviseSyntheticConn(t *testing.T, conn *Conn, inbox ExecutorInbox, mgr *ConnManager, outboundDone <-chan struct{}) <-chan struct{} {
	t.Helper()

	dispatchDone := make(chan struct{})
	keepaliveDone := make(chan struct{})
	go func() {
		<-conn.closed
		close(dispatchDone)
		close(keepaliveDone)
	}()

	supervised := make(chan struct{})
	go func() {
		conn.superviseLifecycle(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr, dispatchDone, keepaliveDone, outboundDone)
		close(supervised)
	}()
	return supervised
}
