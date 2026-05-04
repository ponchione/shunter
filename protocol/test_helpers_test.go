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
		ID:                  GenerateConnectionID(),
		Identity:            [32]byte{1},
		AllowAllPermissions: true,
		OutboundCh:          make(chan []byte, opts.OutgoingBufferMessages),
		inflightSem:         make(chan struct{}, opts.IncomingQueueMessages),
		opts:                opts,
		readCtx:             readCtx,
		cancelRead:          cancelRead,
		closed:              make(chan struct{}),
	}
}

// drainServerMsg reads one frame from conn.OutboundCh and decodes it,
// returning the tag and decoded server message. Fatals if nothing is
// queued or decode fails.
func drainServerMsg(t *testing.T, conn *Conn) (uint8, any) {
	t.Helper()
	select {
	case frame := <-conn.OutboundCh:
		return decodeOutboundServerFrame(t, conn, frame)
	default:
		t.Fatal("expected a message on OutboundCh but channel was empty")
		return 0, nil
	}
}

func drainServerMsgEventually(t *testing.T, conn *Conn) (uint8, any) {
	t.Helper()
	select {
	case frame := <-conn.OutboundCh:
		return decodeOutboundServerFrame(t, conn, frame)
	case <-time.After(2 * time.Second):
		t.Fatal("expected a message on OutboundCh but channel stayed empty")
		return 0, nil
	}
}

func decodeOutboundServerFrame(t *testing.T, conn *Conn, frame []byte) (uint8, any) {
	t.Helper()
	if conn != nil && conn.Compression {
		tag, body, err := UnwrapCompressed(frame)
		if err != nil {
			t.Fatalf("UnwrapCompressed: %v", err)
		}
		wire := make([]byte, 1+len(body))
		wire[0] = tag
		copy(wire[1:], body)
		frame = wire
	}
	tag, msg, err := DecodeServerMessage(frame)
	if err != nil {
		t.Fatalf("DecodeServerMessage: %v", err)
	}
	return tag, msg
}

func requireSubscriptionError(t *testing.T, conn *Conn, requestID, queryID uint32, want string) SubscriptionError {
	t.Helper()
	tag, decoded := drainServerMsgEventually(t, conn)
	if tag != TagSubscriptionError {
		t.Fatalf("tag = %d, want %d (TagSubscriptionError)", tag, TagSubscriptionError)
	}
	se := decoded.(SubscriptionError)
	if se.RequestID == nil || *se.RequestID != requestID {
		t.Fatalf("RequestID = %v, want %d", se.RequestID, requestID)
	}
	if se.QueryID == nil || *se.QueryID != queryID {
		t.Fatalf("QueryID = %v, want %d", se.QueryID, queryID)
	}
	if se.Error != want {
		t.Fatalf("Error = %q, want %q", se.Error, want)
	}
	return se
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
