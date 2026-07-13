package protocol

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
)

func TestOutboundQueueByteLimitPrecedesMessageLimit(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 8
	opts.MaxOutboundQueuedBytes = 10
	conn := testConnDirect(&opts)

	for _, frame := range [][]byte{make([]byte, 4), make([]byte, 6)} {
		if got := conn.trySendOutbound(frame); got != outboundSendSent {
			t.Fatalf("trySendOutbound(%d bytes) = %v, want sent", len(frame), got)
		}
	}
	if got := conn.trySendOutbound([]byte{1}); got != outboundSendBytesFull {
		t.Fatalf("byte-overflow send = %v, want outboundSendBytesFull", got)
	}
	if got := len(conn.OutboundCh); got != 2 {
		t.Fatalf("queued messages = %d, want 2 below message cap", got)
	}
	if got := conn.outboundQueuedByteCount(); got != 10 {
		t.Fatalf("queued bytes = %d, want 10", got)
	}
}

func TestOutboundQueueMessageLimitPrecedesByteLimit(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 2
	opts.MaxOutboundQueuedBytes = 100
	conn := testConnDirect(&opts)

	if got := conn.trySendOutbound([]byte{1}); got != outboundSendSent {
		t.Fatalf("first send = %v, want sent", got)
	}
	if got := conn.trySendOutbound([]byte{2}); got != outboundSendSent {
		t.Fatalf("second send = %v, want sent", got)
	}
	if got := conn.trySendOutbound([]byte{3}); got != outboundSendFull {
		t.Fatalf("message-overflow send = %v, want outboundSendFull", got)
	}
	if got := conn.outboundQueuedByteCount(); got != 2 {
		t.Fatalf("queued bytes = %d, want 2", got)
	}
}

func TestOutboundQueueConcurrentAccountingReturnsToZero(t *testing.T) {
	opts := DefaultProtocolOptions()
	opts.OutgoingBufferMessages = 32
	opts.MaxOutboundQueuedBytes = 1 << 20
	conn := testConnDirect(&opts)

	const producers = 4
	const sendsPerProducer = 500
	errCh := make(chan error, producers)
	var producersDone sync.WaitGroup
	for producer := range producers {
		producersDone.Add(1)
		go func(size int) {
			defer producersDone.Done()
			frame := make([]byte, size+1)
			for range sendsPerProducer {
				for {
					switch got := conn.trySendOutbound(frame); got {
					case outboundSendSent:
						goto sent
					case outboundSendFull:
						runtime.Gosched()
					default:
						errCh <- errors.New("unexpected outbound send result")
						return
					}
				}
			sent:
				continue
			}
		}(producer)
	}

	for range producers * sendsPerProducer {
		frame := <-conn.OutboundCh
		conn.releaseOutboundBytes(len(frame))
	}
	producersDone.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if got := conn.outboundQueuedByteCount(); got != 0 {
		t.Fatalf("queued bytes after concurrent drain = %d, want 0", got)
	}
}

func TestOutboundQueueFailuresDoNotReserveBytes(t *testing.T) {
	conn := testConnDirect(nil)
	mgr := NewConnManager()
	if err := mgr.Add(conn); err != nil {
		t.Fatal(err)
	}
	sender := NewClientSender(mgr, &fakeInbox{})
	if err := sender.Send(conn.ID, struct{}{}); err == nil {
		t.Fatal("unsupported message encoding succeeded")
	}
	if got := conn.outboundQueuedByteCount(); got != 0 {
		t.Fatalf("queued bytes after encode failure = %d, want 0", got)
	}
	if _, err := WrapCompressed(1, []byte("body"), CompressionBrotli); !errors.Is(err, ErrBrotliUnsupported) {
		t.Fatalf("compression error = %v, want ErrBrotliUnsupported", err)
	}
	if got := conn.outboundQueuedByteCount(); got != 0 {
		t.Fatalf("queued bytes after compression failure = %d, want 0", got)
	}
}

func TestOutboundQueueAbandonReleasesReservationsOnce(t *testing.T) {
	conn := testConnDirect(nil)
	for _, frame := range [][]byte{make([]byte, 3), make([]byte, 5), make([]byte, 7)} {
		if got := conn.trySendOutbound(frame); got != outboundSendSent {
			t.Fatalf("send = %v, want sent", got)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn.runOutboundWriter(ctx)
	if got := conn.outboundQueuedByteCount(); got != 0 {
		t.Fatalf("queued bytes after writer discard = %d, want 0", got)
	}
	if got := conn.trySendOutbound([]byte{1}); got != outboundSendClosed {
		t.Fatalf("send after writer exit = %v, want closed", got)
	}
	conn.abandonOutboundQueue()
	if got := conn.outboundQueuedByteCount(); got != 0 {
		t.Fatalf("queued bytes after repeated abandon = %d, want 0", got)
	}
}

func TestDisconnectRequestStopsFurtherOutboundEnqueue(t *testing.T) {
	conn := testConnDirect(nil)
	if !conn.requestDisconnect(CloseProtocol, "fatal") {
		t.Fatal("first disconnect request was not accepted")
	}
	if got := conn.trySendOutbound([]byte{1}); got != outboundSendClosed {
		t.Fatalf("send after disconnect request = %v, want closed", got)
	}
	if got := conn.outboundQueuedByteCount(); got != 0 {
		t.Fatalf("queued bytes after rejected send = %d, want 0", got)
	}
}
