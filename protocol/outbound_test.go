package protocol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestOutboundWriterDeliversFrames(t *testing.T) {
	var serverConn *websocket.Conn
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		serverConn = c
		close(ready)
		<-r.Context().Done()
	}))
	defer srv.Close()

	clientConn, _, err := websocket.Dial(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.CloseNow()
	<-ready

	opts := DefaultProtocolOptions()
	c := &Conn{
		OutboundCh: make(chan []byte, 8),
		ws:         serverConn,
		opts:       &opts,
		closed:     make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		c.runOutboundWriter(ctx)
		wg.Done()
	}()

	c.OutboundCh <- []byte{0x01, 0x02}
	c.OutboundCh <- []byte{0x03, 0x04}

	for _, want := range [][]byte{{0x01, 0x02}, {0x03, 0x04}} {
		_, got, err := clientConn.Read(context.Background())
		if err != nil {
			t.Fatalf("client read: %v", err)
		}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	cancel()
	wg.Wait()
}

func TestOutboundWriterExitsOnClose(t *testing.T) {
	opts := DefaultProtocolOptions()
	c := &Conn{
		OutboundCh: make(chan []byte, 8),
		opts:       &opts,
		closed:     make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		c.runOutboundWriter(context.Background())
		close(done)
	}()

	close(c.OutboundCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writer goroutine did not exit after OutboundCh closed")
	}
}
