package protocol

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/types"
)

func BenchmarkWebSocketFanout16ClientsLightUpdate(b *testing.B) {
	benchmarkWebSocketFanoutLightUpdate(b, 16)
}

func BenchmarkWebSocketFanout64ClientsLightUpdate(b *testing.B) {
	benchmarkWebSocketFanoutLightUpdate(b, 64)
}

func benchmarkWebSocketFanoutLightUpdate(b *testing.B, clientCount int) {
	h := newBenchmarkWebSocketFanoutHarness(b, clientCount)
	fanout := make(map[types.ConnectionID][]SubscriptionUpdate, clientCount)
	rows := EncodeRowList([][]byte{{0x01, 0x02, 0x03, 0x04}})
	empty := EncodeRowList(nil)
	for _, id := range h.ids {
		fanout[id] = []SubscriptionUpdate{{
			QueryID:   uint32(id[0]) + 1,
			TableName: "orders",
			Inserts:   rows,
			Deletes:   empty,
		}}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if errs := DeliverTransactionUpdateLight(h.sender, h.mgr, uint32(i+1), fanout); len(errs) != 0 {
			b.Fatalf("DeliverTransactionUpdateLight errors: %v", errs)
		}
		for _, client := range h.clients {
			frame := benchmarkReadFanoutFrame(b, client)
			if len(frame) == 0 || frame[0] != TagTransactionUpdateLight {
				b.Fatalf("server tag = %v, want %d", firstByte(frame), TagTransactionUpdateLight)
			}
		}
	}
}

type benchmarkWebSocketFanoutHarness struct {
	clients []*websocket.Conn
	ids     []types.ConnectionID
	mgr     *ConnManager
	sender  ClientSender
}

func newBenchmarkWebSocketFanoutHarness(b *testing.B, clientCount int) *benchmarkWebSocketFanoutHarness {
	b.Helper()

	accepted := make(chan *websocket.Conn, clientCount)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			b.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.CloseNow()
		accepted <- conn
		<-release
	}))
	b.Cleanup(srv.Close)
	b.Cleanup(func() { close(release) })

	opts := DefaultProtocolOptions()
	mgr := NewConnManager()
	sender := NewClientSender(mgr, &fakeInbox{})
	h := &benchmarkWebSocketFanoutHarness{
		clients: make([]*websocket.Conn, 0, clientCount),
		ids:     make([]types.ConnectionID, 0, clientCount),
		mgr:     mgr,
		sender:  sender,
	}

	baseURL := strings.Replace(srv.URL, "http://", "ws://", 1)
	for i := 0; i < clientCount; i++ {
		dialCtx, cancelDial := context.WithTimeout(context.Background(), 2*time.Second)
		client, _, err := websocket.Dial(dialCtx, baseURL, nil)
		cancelDial()
		if err != nil {
			b.Fatalf("dial websocket client %d: %v", i, err)
		}
		serverConn := benchmarkAcceptServerConn(b, accepted)
		id := benchmarkFanoutConnID(i + 1)
		conn := &Conn{
			ID:         id,
			OutboundCh: make(chan []byte, opts.OutgoingBufferMessages),
			ws:         serverConn,
			opts:       &opts,
			closed:     make(chan struct{}),
		}
		if err := mgr.Add(conn); err != nil {
			b.Fatalf("register connection %d: %v", i, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn.runOutboundWriter(ctx)
		}()
		b.Cleanup(func() {
			cancel()
			client.CloseNow()
			serverConn.CloseNow()
			wg.Wait()
		})
		h.clients = append(h.clients, client)
		h.ids = append(h.ids, id)
	}
	return h
}

func benchmarkAcceptServerConn(b *testing.B, accepted <-chan *websocket.Conn) *websocket.Conn {
	b.Helper()
	select {
	case conn := <-accepted:
		return conn
	case <-time.After(2 * time.Second):
		b.Fatal("timeout waiting for server websocket accept")
		return nil
	}
}

func benchmarkFanoutConnID(n int) types.ConnectionID {
	var id types.ConnectionID
	id[0] = byte(n)
	id[1] = byte(n >> 8)
	return id
}

func benchmarkReadFanoutFrame(b *testing.B, conn *websocket.Conn) []byte {
	b.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	mt, frame, err := conn.Read(ctx)
	if err != nil {
		b.Fatalf("read fanout frame: %v", err)
	}
	if mt != websocket.MessageBinary {
		b.Fatalf("fanout message type = %v, want binary", mt)
	}
	return frame
}

func firstByte(frame []byte) string {
	if len(frame) == 0 {
		return "<empty>"
	}
	return fmt.Sprintf("%d", frame[0])
}
