package protocol

import (
	"crypto/rand"
	"errors"
	"time"

	"github.com/ponchione/shunter/types"
)

// ProtocolOptions tunes the WebSocket transport layer (SPEC-005 §12).
// DefaultProtocolOptions returns spec-sane starting values; deployers
// can override individual fields before handing the struct to the
// upgrade handler.
type ProtocolOptions struct {
	// PingInterval is the cadence of server-sent WebSocket Ping
	// frames that drive keep-alive.
	PingInterval time.Duration
	// IdleTimeout fires when no inbound traffic is observed for the
	// interval. On expiry the server closes the connection and the
	// normal disconnect pipeline runs.
	IdleTimeout time.Duration
	// CloseHandshakeTimeout caps how long Close frames may wait for
	// the peer's acknowledgement before the socket is forcibly torn
	// down.
	CloseHandshakeTimeout time.Duration
	// OutgoingBufferMessages caps the per-connection outbound queue.
	// Overflow triggers a 1008 close per SPEC-005 §10.1.
	OutgoingBufferMessages int
	// IncomingQueueMessages caps inbound per-connection queue depth.
	// Overflow triggers a 1008 close per SPEC-005 §10.2.
	IncomingQueueMessages int
	// MaxMessageSize is the largest frame payload the read layer
	// accepts; larger frames cause a 1008 close.
	MaxMessageSize int64
}

// DefaultProtocolOptions returns SPEC-005 §12 default values.
func DefaultProtocolOptions() ProtocolOptions {
	return ProtocolOptions{
		PingInterval:           15 * time.Second,
		IdleTimeout:            30 * time.Second,
		CloseHandshakeTimeout:  250 * time.Millisecond,
		OutgoingBufferMessages: 256,
		IncomingQueueMessages:  64,
		MaxMessageSize:         4 * 1024 * 1024,
	}
}

// ErrZeroConnectionID is returned by the upgrade handler when a
// client-supplied ConnectionID is the reserved all-zero value
// (SPEC-005 §4.3 — 400 before WebSocket upgrade).
var ErrZeroConnectionID = errors.New("protocol: connection_id must not be zero")

// GenerateConnectionID returns a fresh 16-byte ConnectionID sourced
// from crypto/rand. On the (vanishingly unlikely) event of an all-zero
// draw, a single retry shifts the outcome — callers cannot observe
// IsZero() == true from this function.
func GenerateConnectionID() types.ConnectionID {
	for {
		var c types.ConnectionID
		if _, err := rand.Read(c[:]); err != nil {
			// crypto/rand.Read is documented never to fail on
			// supported platforms; panic surfaces misconfiguration
			// loudly rather than silently yielding a zero ID.
			panic("protocol: crypto/rand.Read: " + err.Error())
		}
		if !c.IsZero() {
			return c
		}
	}
}
