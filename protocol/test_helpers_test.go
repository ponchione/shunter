package protocol

import (
	"context"
	"strings"
	"testing"
	"time"
)

// testConnDirect builds a *Conn without a real WebSocket, suitable for
// handler unit tests that only inspect OutboundCh.
func testConnDirect(opts *ProtocolOptions) *Conn {
	if opts == nil {
		o := DefaultProtocolOptions()
		opts = &o
	}
	readCtx, cancelRead := context.WithCancel(context.Background())
	return &Conn{
		ID:          GenerateConnectionID(),
		Identity:    [32]byte{1},
		OutboundCh:  make(chan []byte, opts.OutgoingBufferMessages),
		inflightSem: make(chan struct{}, opts.IncomingQueueMessages),
		opts:        opts,
		readCtx:     readCtx,
		cancelRead:  cancelRead,
		closed:      make(chan struct{}),
	}
}

// drainServerMsg reads one frame from conn.OutboundCh and decodes it,
// returning the tag and decoded server message. Fatals if nothing is
// queued or decode fails.
func drainServerMsg(t *testing.T, conn *Conn) (uint8, any) {
	t.Helper()
	select {
	case frame := <-conn.OutboundCh:
		tag, msg, err := DecodeServerMessage(frame)
		if err != nil {
			t.Fatalf("DecodeServerMessage: %v", err)
		}
		return tag, msg
	default:
		t.Fatal("expected a message on OutboundCh but channel was empty")
		return 0, nil
	}
}

func drainServerMsgEventually(t *testing.T, conn *Conn) (uint8, any) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case frame := <-conn.OutboundCh:
			tag, msg, err := DecodeServerMessage(frame)
			if err != nil {
				t.Fatalf("DecodeServerMessage: %v", err)
			}
			return tag, msg
		case <-deadline:
			t.Fatal("expected a message on OutboundCh but channel stayed empty")
			return 0, nil
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func overlongSQLQuery() string {
	const maxSQLLength = 50_000
	const base = "SELECT * FROM users WHERE id = 1"
	const suffix = " OR id = 1"
	if len(base) > maxSQLLength {
		return base
	}
	var b strings.Builder
	b.Grow(maxSQLLength + len(suffix))
	b.WriteString(base)
	for b.Len() <= maxSQLLength {
		b.WriteString(suffix)
	}
	return b.String()
}
