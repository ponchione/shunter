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
	// DisconnectTimeout bounds the detached teardown goroutine the
	// outbound-overflow path spawns in
	// connManagerSender.enqueueOnConn. It is the ceiling for how long
	// inbox.DisconnectClientSubscriptions + inbox.OnDisconnect may
	// run before the teardown proceeds to close(c.closed) anyway.
	// OI-004 sub-hazard pin: with a
	// Background ctx the detached goroutine was unbounded if either
	// inbox call hung; the bounded ctx makes the leak observable
	// and collectible.
	DisconnectTimeout time.Duration
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

// DefaultOutgoingBufferMessages matches the reference SpacetimeDB
// per-client outbound channel capacity (`CLIENT_CHANNEL_CAPACITY = 16 *
// KB`) at
// `reference/SpacetimeDB/crates/core/src/client/client_connection.rs:657`.
// Phase 2 Slice 3 (`docs/parity-phase2-slice3-lag-policy.md`) aligned
// the default so realistic bursty workloads tolerate the same lag before
// the connection is torn down.
const DefaultOutgoingBufferMessages = 16 * 1024

// DefaultProtocolOptions returns SPEC-005 §12 default values.
func DefaultProtocolOptions() ProtocolOptions {
	return ProtocolOptions{
		PingInterval:           15 * time.Second,
		IdleTimeout:            30 * time.Second,
		CloseHandshakeTimeout:  250 * time.Millisecond,
		DisconnectTimeout:      5 * time.Second,
		OutgoingBufferMessages: DefaultOutgoingBufferMessages,
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
