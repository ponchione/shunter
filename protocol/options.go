package protocol

import (
	"crypto/rand"
	"errors"
	"fmt"
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
	// WriteTimeout bounds each server-to-client WebSocket data write.
	WriteTimeout time.Duration
	// DisconnectTimeout bounds detached teardown work before local close proceeds.
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

// DefaultOutgoingBufferMessages is the per-client outbound queue capacity.
const DefaultOutgoingBufferMessages = 16 * 1024

// DefaultProtocolOptions returns SPEC-005 §12 default values.
func DefaultProtocolOptions() ProtocolOptions {
	return ProtocolOptions{
			PingInterval:           15 * time.Second,
			IdleTimeout:            30 * time.Second,
			CloseHandshakeTimeout:  250 * time.Millisecond,
			WriteTimeout:           5 * time.Second,
			DisconnectTimeout:      5 * time.Second,
			OutgoingBufferMessages: DefaultOutgoingBufferMessages,
		IncomingQueueMessages:  64,
		MaxMessageSize:         4 * 1024 * 1024,
	}
}

func normalizeProtocolOptions(opts ProtocolOptions) (ProtocolOptions, error) {
	if opts.PingInterval < 0 {
		return ProtocolOptions{}, fmt.Errorf("ping interval must not be negative")
	}
	if opts.IdleTimeout < 0 {
		return ProtocolOptions{}, fmt.Errorf("idle timeout must not be negative")
	}
	if opts.CloseHandshakeTimeout < 0 {
		return ProtocolOptions{}, fmt.Errorf("close handshake timeout must not be negative")
	}
	if opts.WriteTimeout < 0 {
		return ProtocolOptions{}, fmt.Errorf("write timeout must not be negative")
	}
	if opts.DisconnectTimeout < 0 {
		return ProtocolOptions{}, fmt.Errorf("disconnect timeout must not be negative")
	}
	if opts.OutgoingBufferMessages < 0 {
		return ProtocolOptions{}, fmt.Errorf("outgoing buffer messages must not be negative")
	}
	if opts.IncomingQueueMessages < 0 {
		return ProtocolOptions{}, fmt.Errorf("incoming queue messages must not be negative")
	}
	if opts.MaxMessageSize < 0 {
		return ProtocolOptions{}, fmt.Errorf("max message size must not be negative")
	}

	defaults := DefaultProtocolOptions()
	if opts.PingInterval == 0 {
		opts.PingInterval = defaults.PingInterval
	}
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = defaults.IdleTimeout
	}
	if opts.CloseHandshakeTimeout == 0 {
		opts.CloseHandshakeTimeout = defaults.CloseHandshakeTimeout
	}
	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = defaults.WriteTimeout
	}
	if opts.DisconnectTimeout == 0 {
		opts.DisconnectTimeout = defaults.DisconnectTimeout
	}
	if opts.OutgoingBufferMessages == 0 {
		opts.OutgoingBufferMessages = defaults.OutgoingBufferMessages
	}
	if opts.IncomingQueueMessages == 0 {
		opts.IncomingQueueMessages = defaults.IncomingQueueMessages
	}
	if opts.MaxMessageSize == 0 {
		opts.MaxMessageSize = defaults.MaxMessageSize
	}
	return opts, nil
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
