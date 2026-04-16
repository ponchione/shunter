package protocol

import (
	"context"
	"fmt"

	"github.com/coder/websocket"

	"github.com/ponchione/shunter/types"
)

// ExecutorInbox is the narrow seam the protocol layer uses to hand
// lifecycle events off to the transaction executor (SPEC-003 §10.3 /
// §10.4). The protocol package deliberately does not import the
// executor package; a host-owned adapter translates these calls into
// executor.OnConnectCmd + executor.OnDisconnectCmd and blocks on the
// ReducerResponse channel until a status is available.
//
// Lifecycle contract:
//   - OnConnect blocks until the executor admits or rejects the
//     connection. A nil error means StatusCommitted (admit); a non-nil
//     error means any non-committed outcome (reject). Reason text is
//     relayed only to the close frame — it is not machine-parseable.
//   - DisconnectClientSubscriptions asks the executor to drop every
//     subscription registered for this connection. It MUST complete
//     before OnDisconnect is called (SPEC-005 §5.3). A non-nil error
//     is logged and does not veto the rest of the disconnect
//     sequence.
//   - OnDisconnect runs the OnDisconnect lifecycle reducer and the
//     sys_clients cleanup. A non-nil error is logged; disconnect
//     cannot be vetoed (SPEC-003 §10.4).
//
// Additional methods (subscription dispatch, call-reducer submit)
// arrive in later epics. The interface intentionally stays minimal
// per-story so each slice lands with the smallest useful shape.
type ExecutorInbox interface {
	OnConnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error
	OnDisconnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error
	DisconnectClientSubscriptions(ctx context.Context, connID types.ConnectionID) error
	RegisterSubscription(ctx context.Context, req RegisterSubscriptionRequest) error
	UnregisterSubscription(ctx context.Context, req UnregisterSubscriptionRequest) error
	CallReducer(ctx context.Context, req CallReducerRequest) error
}

// SubscriptionCommandResponse is the protocol-side async result envelope
// for a submitted subscribe command. E4 only allocates and hands this
// channel to the executor seam; E5 watches it and turns accepted-command
// outcomes into wire responses.
type SubscriptionCommandResponse struct {
	Applied *SubscribeApplied
	Error   *SubscriptionError
}

// UnsubscribeCommandResponse is the protocol-side async result envelope
// for a submitted unsubscribe command. E5 watches it to deliver the
// eventual UnsubscribeApplied / SubscriptionError message.
type UnsubscribeCommandResponse struct {
	Applied *UnsubscribeApplied
	Error   *SubscriptionError
}

// RegisterSubscriptionRequest carries the fields the executor needs to
// register and evaluate a new subscription (SPEC-004 §2.1).
type RegisterSubscriptionRequest struct {
	ConnID         types.ConnectionID
	SubscriptionID uint32
	RequestID      uint32
	Predicate      any // subscription.Predicate — typed as any to avoid import cycle
	ResponseCh     chan<- SubscriptionCommandResponse
}

// UnregisterSubscriptionRequest carries the unsubscribe fields the
// executor / delivery path needs to produce UnsubscribeApplied later.
type UnregisterSubscriptionRequest struct {
	ConnID         types.ConnectionID
	SubscriptionID uint32
	RequestID      uint32
	SendDropped    bool
	ResponseCh     chan<- UnsubscribeCommandResponse
}

// CallReducerRequest carries the fields for a reducer invocation
// (SPEC-003 §10.3).
type CallReducerRequest struct {
	ConnID      types.ConnectionID
	Identity    types.Identity
	RequestID   uint32
	ReducerName string
	Args        []byte
	ResponseCh  chan<- ReducerCallResult
}

// RunLifecycle drives SPEC-005 §5.1–§5.2 admission for one connection:
//
//  1. Submit OnConnect via the executor inbox and block for the
//     response. The executor runs the optional OnConnect reducer plus
//     sys_clients bookkeeping inside a single transaction (executor
//     Story 7.2).
//  2. On reject: close the WebSocket with StatusPolicyViolation (1008,
//     per SPEC-005 §11.1), return the underlying error, and leave the
//     ConnManager untouched so downstream fan-out / disconnect code
//     never sees the rejected connection.
//  3. On admit: register the Conn in the manager first so any
//     concurrent fan-out delivery can resolve the ConnectionID, then
//     encode + send InitialConnection as the first binary frame. On a
//     write failure the connection is de-registered and closed with
//     StatusInternalError (1011).
//
// Read / write loops and keep-alive goroutines are NOT started here —
// Story 3.5 (keep-alive) and Epic 4 (read+write loops) extend this
// sequence. The caller (default Upgraded handler) either returns to
// let the hijacked WebSocket persist or spawns those goroutines.
func (c *Conn) RunLifecycle(ctx context.Context, inbox ExecutorInbox, mgr *ConnManager) error {
	if err := inbox.OnConnect(ctx, c.ID, c.Identity); err != nil {
		reason := truncateCloseReason("onconnect rejected: " + err.Error())
		_ = c.ws.Close(websocket.StatusPolicyViolation, reason)
		return fmt.Errorf("onconnect rejected: %w", err)
	}

	// Register before first send: the fan-out worker (Phase 8) resolves
	// ConnectionID → Conn via this manager, and admitting a connection
	// that is not yet visible would drop its first delta. Order is
	// safe because RunLifecycle is synchronous and InitialConnection is
	// the very next thing emitted on this socket.
	mgr.Add(c)

	frame, err := encodeInitialConnectionFrame(InitialConnection{
		Identity:     c.Identity,
		ConnectionID: c.ID,
		Token:        c.Token,
	}, c.Compression)
	if err != nil {
		mgr.Remove(c.ID)
		_ = c.ws.Close(websocket.StatusInternalError, "encode InitialConnection")
		return fmt.Errorf("encode InitialConnection: %w", err)
	}
	if err := c.ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
		mgr.Remove(c.ID)
		_ = c.ws.Close(websocket.StatusInternalError, "write InitialConnection")
		return fmt.Errorf("write InitialConnection: %w", err)
	}
	return nil
}

// encodeInitialConnectionFrame serializes the InitialConnection server
// message per SPEC-005 §8.1 and wraps it in the correct transport
// envelope. When compression was negotiated at upgrade time, the
// handshake frame still carries its compression byte (CompressionNone)
// per §3.3 so the client's decoder branches consistently; the
// handshake body itself is never gzipped.
func encodeInitialConnectionFrame(msg InitialConnection, compressionNegotiated bool) ([]byte, error) {
	wire, err := EncodeServerMessage(msg)
	if err != nil {
		return nil, err
	}
	if !compressionNegotiated {
		return wire, nil
	}
	out := make([]byte, 1+len(wire))
	out[0] = CompressionNone
	copy(out[1:], wire)
	return out, nil
}

// truncateCloseReason keeps the reason string inside the 123-byte
// WebSocket Close limit (RFC 6455 §5.5.1). A conservative 120-byte cap
// leaves headroom for UTF-8 multi-byte sequences at the boundary.
func truncateCloseReason(s string) string {
	const maxLen = 120
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
