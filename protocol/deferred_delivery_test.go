package protocol

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDeferredDeliveryPreservesOrderAcrossConcurrentProcedures(t *testing.T) {
	conn := testConnDirect(nil)
	mgr := NewConnManager()
	if err := mgr.Add(conn); err != nil {
		t.Fatal(err)
	}
	sender := NewClientSender(mgr, nil)
	first, second := conn.DeferDelivery(), conn.DeferDelivery()
	for _, msg := range []any{
		TransactionUpdateLight{RequestID: 1},
		TransactionUpdate{ReducerCall: ReducerCallInfo{RequestID: 2}, Status: StatusCommitted{}},
		SubscriptionError{Error: "evaluation failed"},
		SubscribeSingleApplied{RequestID: 3},
	} {
		if err := sender.Send(conn.ID, msg); err != nil {
			t.Fatal(err)
		}
	}
	conn.outboundMu.Lock()
	conn.deferredSince = time.Now().Add(-3 * time.Second)
	conn.outboundMu.Unlock()
	if clients, messages, age := mgr.DeferredDeliveryStats(); clients != 1 || messages != 4 || age < 3*time.Second {
		t.Fatalf("deferred stats = %d clients, %d messages, %v age", clients, messages, age)
	}
	if err := sender.Send(conn.ID, ProcedureResponse{MessageID: []byte("first")}); err != nil {
		t.Fatal(err)
	}
	first()
	first() // Releasing a hold twice must not release the other procedure's hold.
	if got := len(conn.OutboundCh); got != 1 {
		t.Fatalf("outbound before final release = %d, want only procedure response", got)
	}
	if err := sender.Send(conn.ID, ProcedureResponse{MessageID: []byte("second")}); err != nil {
		t.Fatal(err)
	}
	second()
	for i, want := range []byte{TagProcedureResponse, TagProcedureResponse, TagTransactionUpdateLight, TagTransactionUpdate, TagSubscriptionError, TagSubscribeSingleApplied} {
		select {
		case frame := <-conn.OutboundCh:
			conn.releaseOutboundBytes(len(frame))
			tag, _, err := DecodeServerMessage(frame)
			if err != nil || tag != want {
				t.Fatalf("frame %d = tag %d, %v; want %d", i, tag, err, want)
			}
		default:
			t.Fatalf("missing frame %d", i)
		}
	}
	if clients, messages, age := mgr.DeferredDeliveryStats(); clients != 0 || messages != 0 || age != 0 {
		t.Fatalf("stats after release = %d, %d, %v", clients, messages, age)
	}
	if got := conn.outboundQueuedByteCount(); got != 0 {
		t.Fatalf("retained bytes after release and drain = %d", got)
	}
}

func TestDeferredDeliverySharesOutboundLimitsAndDiscardsOnStop(t *testing.T) {
	for _, action := range []string{"message_overflow", "byte_overflow", "response_overflow", "disconnect", "writer_cancel"} {
		t.Run(action, func(t *testing.T) {
			opts := DefaultProtocolOptions()
			opts.OutgoingBufferMessages = 2
			if action == "byte_overflow" {
				opts.OutgoingBufferMessages = 8
			}
			conn := testConnDirect(&opts)
			mgr := NewConnManager()
			if err := mgr.Add(conn); err != nil {
				t.Fatal(err)
			}
			sender := NewClientSender(mgr, nil)
			// One ordinary frame is already queued before the procedure starts.
			if err := sender.Send(conn.ID, TransactionUpdateLight{RequestID: 1}); err != nil {
				t.Fatal(err)
			}
			release := conn.DeferDelivery()
			if err := sender.Send(conn.ID, TransactionUpdateLight{RequestID: 2}); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "message_overflow", "byte_overflow", "response_overflow":
				if action == "byte_overflow" {
					conn.opts.MaxOutboundQueuedBytes = conn.outboundQueuedByteCount()
				}
				var msg any = TransactionUpdateLight{RequestID: 3}
				if action == "response_overflow" {
					msg = ProcedureResponse{MessageID: []byte("response")}
				}
				if err := sender.Send(conn.ID, msg); !errors.Is(err, ErrClientBufferFull) {
					t.Fatalf("overflow = %v, want ErrClientBufferFull", err)
				}
				select {
				case <-conn.disconnectRequested:
				default:
					t.Fatal("overflow did not request disconnect")
				}
			case "disconnect":
				conn.Disconnect(context.Background(), CloseNormal, "test", nil, mgr)
			case "writer_cancel":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				conn.runOutboundWriter(ctx)
			}
			release()
			conn.stopAndAbandonOutboundQueue()
			if got := conn.outboundQueuedByteCount(); got != 0 {
				t.Fatalf("retained bytes after stop = %d", got)
			}
			if len(conn.deferredFrames) != 0 || !conn.deferredSince.IsZero() {
				t.Fatal("stopped connection retained deferred state")
			}
			if err := sender.Send(conn.ID, ProcedureResponse{}); !errors.Is(err, ErrConnNotFound) {
				t.Fatalf("send after stop = %v, want ErrConnNotFound", err)
			}
		})
	}
}
