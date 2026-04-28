package protocol

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestOutgoingBackpressure_BufferFullDisconnects(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}

	// Fill the buffer.
	if err := s.Send(conn.ID, msg); err != nil {
		t.Fatalf("first send: %v", err)
	}

	// Second send overflows → ErrClientBufferFull + disconnect.
	err := s.Send(conn.ID, msg)
	if !errors.Is(err, ErrClientBufferFull) {
		t.Fatalf("expected ErrClientBufferFull, got %v", err)
	}

	// Client should see close 1008.
	readCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, readErr := clientWS.Read(readCtx)
	if code := websocket.CloseStatus(readErr); code != websocket.StatusPolicyViolation {
		t.Errorf("close code = %d, want %d (StatusPolicyViolation/1008)", code, websocket.StatusPolicyViolation)
	}
}

func TestOutgoingBackpressure_OverflowMessageNotDelivered(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn := testConnDirect(&opts)

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	_ = s.Send(conn.ID, msg) // fills buffer

	// Overflow.
	_ = s.Send(conn.ID, msg)

	// OutboundCh should have exactly 1 message (the first), not 2.
	count := len(conn.OutboundCh)
	if count != 1 {
		t.Errorf("OutboundCh length = %d, want 1 (overflow message must not be enqueued)", count)
	}
}

func TestOutgoingBackpressure_FurtherSendsAfterDisconnect(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 1
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}
	_ = s.Send(conn.ID, msg)

	// Overflow triggers disconnect.
	_ = s.Send(conn.ID, msg)

	// Wait for disconnect to complete (conn removed from manager).
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	deadline := time.After(2 * time.Second)
	for mgr.Get(conn.ID) != nil {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("conn not removed from manager after overflow disconnect")
		}
	}

	// Drain the close frame so the background ws.Close goroutine
	// completes before cleanup tears down the httptest server.
	drainCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, _ = clientWS.Read(drainCtx)

	// Further sends return ErrConnNotFound.
	err := s.Send(conn.ID, msg)
	if !errors.Is(err, ErrConnNotFound) {
		t.Fatalf("expected ErrConnNotFound after disconnect, got %v", err)
	}
}

func TestOutgoingBackpressure_SendConcurrentWithDisconnectDoesNotPanic(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 32
	conn, clientWS, cleanup := loopbackConn(t, opts)
	defer cleanup()

	inbox := &fakeInbox{}
	mgr := NewConnManager()
	mgr.Add(conn)
	s := NewClientSender(mgr, inbox)

	msg := SubscribeSingleApplied{RequestID: 1, QueryID: 10, TableName: "t", Rows: []byte{}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	started := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started <- struct{}{}
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				_ = s.Send(conn.ID, msg)
			}
		}()
	}

	waitForSignals(t, started, 8, "concurrent senders started")
	conn.Disconnect(context.Background(), websocket.StatusNormalClosure, "", inbox, mgr)
	wg.Wait()

	readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readCancel()
	_, _, _ = clientWS.Read(readCtx)
}
