package protocol

import (
	"context"
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

func BenchmarkWebSocketSlowReaderBackpressureUnrelatedFanout(b *testing.B) {
	opts := DefaultProtocolOptions()
	opts.WriteTimeout = 30 * time.Second
	opts.CloseHandshakeTimeout = 50 * time.Millisecond
	opts.DisconnectTimeout = 500 * time.Millisecond
	opts.OutgoingBufferMessages = 1

	inbox := &fakeInbox{}
	mgr := NewConnManager()

	slowConn, _, cleanupSlow := loopbackConnWithTCPBuffers(b, opts, 1024)
	if err := mgr.Add(slowConn); err != nil {
		b.Fatalf("register slow connection: %v", err)
	}
	slowWriterCtx, slowWriterCancel := context.WithCancel(context.Background())
	slowOutboundDone := make(chan struct{})
	go func() {
		slowConn.runOutboundWriter(slowWriterCtx)
		close(slowOutboundDone)
	}()
	slowPressureDone := benchmarkStartSlowReaderPressure(b, slowConn, slowOutboundDone)
	b.Cleanup(func() {
		slowWriterCancel()
		slowConn.ws.CloseNow()
		cleanupSlow()
		benchmarkWaitDone(b, slowOutboundDone, "slow outbound writer")
		benchmarkWaitDone(b, slowPressureDone, "slow pressure goroutine")
	})

	goodConn, goodClient, cleanupGood := loopbackConn(b, opts)
	if err := mgr.Add(goodConn); err != nil {
		b.Fatalf("register good connection: %v", err)
	}
	goodWriterCtx, goodWriterCancel := context.WithCancel(context.Background())
	goodOutboundDone := make(chan struct{})
	go func() {
		goodConn.runOutboundWriter(goodWriterCtx)
		close(goodOutboundDone)
	}()
	b.Cleanup(func() {
		goodWriterCancel()
		goodConn.ws.CloseNow()
		cleanupGood()
		benchmarkWaitDone(b, goodOutboundDone, "good outbound writer")
	})

	sender := NewClientSender(mgr, inbox)
	rows := EncodeRowList([][]byte{{0x01, 0x02, 0x03, 0x04}})
	empty := EncodeRowList(nil)
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		goodConn.ID: {{
			QueryID:   7,
			TableName: "orders",
			Inserts:   rows,
			Deletes:   empty,
		}},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if errs := DeliverTransactionUpdateLight(sender, mgr, uint32(i+1), fanout); len(errs) != 0 {
			b.Fatalf("DeliverTransactionUpdateLight errors: %v", errs)
		}
		frame := benchmarkReadFanoutFrame(b, goodClient)
		if len(frame) == 0 || frame[0] != TagTransactionUpdateLight {
			b.Fatalf("server tag = %v, want %d", firstByte(frame), TagTransactionUpdateLight)
		}
	}
}

func benchmarkStartSlowReaderPressure(b *testing.B, slowConn *Conn, slowOutboundDone <-chan struct{}) <-chan struct{} {
	b.Helper()

	slowFrame := make([]byte, 8<<20)
	for i := range slowFrame {
		slowFrame[i] = byte(i)
	}

	firstSend := make(chan struct{}, 1)
	pressureReady := make(chan struct{})
	pressureDone := make(chan struct{})
	go func() {
		defer close(pressureDone)
		sent := 0
		for {
			select {
			case <-slowOutboundDone:
				return
			case slowConn.OutboundCh <- slowFrame:
				sent++
				if sent == 1 {
					firstSend <- struct{}{}
				}
				if sent == 2 {
					close(pressureReady)
				}
			}
		}
	}()

	select {
	case <-firstSend:
	case <-time.After(time.Second):
		b.Fatal("slow client pressure did not enqueue first frame")
	}
	select {
	case <-pressureReady:
	case <-slowOutboundDone:
		b.Fatal("slow outbound writer exited before its queue showed unread-client pressure")
	case <-time.After(time.Second):
		b.Fatal("slow outbound writer did not consume the first unread-client frame")
	}
	return pressureDone
}

func benchmarkWaitDone(b *testing.B, done <-chan struct{}, label string) {
	b.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		b.Fatalf("%s did not stop", label)
	}
}
