package protocol

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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
	// Set-based seam (single/multi variant variant split). Single and Multi
	// subscribe/unsubscribe paths both route through these — Single
	// forwards a one-entry Predicates slice, Multi forwards N.
	RegisterSubscriptionSet(ctx context.Context, req RegisterSubscriptionSetRequest) error
	UnregisterSubscriptionSet(ctx context.Context, req UnregisterSubscriptionSetRequest) error
	CallReducer(ctx context.Context, req CallReducerRequest) error
}

// SubscriptionSetVariant records which wire family produced a set-based
// subscription request. The executor-side register/unregister commands are
// shared across Single and Multi, but the protocol reply envelope must echo
// the originating variant so the adapter can populate the correct Applied arm.
type SubscriptionSetVariant uint8

const (
	SubscriptionSetVariantUnknown SubscriptionSetVariant = iota
	SubscriptionSetVariantSingle
	SubscriptionSetVariantMulti
)

// RegisterSubscriptionSetRequest carries the fields the executor needs to
// register a set of predicates under one QueryID. Predicates is []any (not
// []subscription.Predicate) because the host-owned executor adapter — the
// concrete ExecutorInbox implementation — may live in a package that should
// not depend on the subscription package. The adapter casts each element to
// subscription.Predicate on the way through. A Single-path submission
// forwards len==1; a Multi-path submission forwards len==N.
//
// Reply is a protocol-side closure invoked synchronously on the
// executor goroutine once the register outcome is known. It carries
// exactly one populated arm of SubscriptionSetCommandResponse
// (SingleApplied / MultiApplied / Error) and typically enqueues the
// corresponding wire frame onto the caller's OutboundCh. Synchronous
// invocation on the executor goroutine is what enforces ADR §9.4
// per-connection FIFO between Applied and subsequent fan-out.
type RegisterSubscriptionSetRequest struct {
	ConnID                  types.ConnectionID
	QueryID                 uint32
	RequestID               uint32
	Variant                 SubscriptionSetVariant
	Predicates              []any // []subscription.Predicate
	PredicateHashIdentities []*types.Identity
	Reply                   func(SubscriptionSetCommandResponse)
	// Receipt is the wall-clock instant the protocol handler received the
	// client request, captured before compile/dispatch. The executor reads
	// it to compute `TotalHostExecutionDurationMicros` as
	// `time.Since(Receipt)` at reply time so the wire field reflects the
	// full admission-path duration rather than only the subs-manager call.
	// Zero is allowed and means "fall back to the executor's local start".
	Receipt time.Time
	// SQLText is the original subscribe-query SQL string for SingleSubscribe
	// admission. The adapter reads it to wrap initial-snapshot evaluation
	// errors with the reference `DBError::WithSql` suffix (`", executing:
	// \`<sql>\`"`) — reference `module_subscription_actor.rs:672` via
	// `return_on_err_with_sql_bool!`. Empty on Multi (reference emits a
	// canned message for multi-initial-eval; no SQL-suffix path) and on
	// paths that do not originate from a single SQL string.
	SQLText string
}

// UnregisterSubscriptionSetRequest drops every internal subscription
// registered under (ConnID, QueryID) atomically. Used by both Single
// and Multi unsubscribe paths.
//
// Reply mirrors RegisterSubscriptionSetRequest.Reply for the
// unsubscribe outcome envelope.
type UnregisterSubscriptionSetRequest struct {
	ConnID    types.ConnectionID
	QueryID   uint32
	RequestID uint32
	Variant   SubscriptionSetVariant
	Reply     func(UnsubscribeSetCommandResponse)
	// Receipt mirrors RegisterSubscriptionSetRequest.Receipt — captured at
	// handler entry so the executor can populate
	// `TotalHostExecutionDurationMicros` from the full unsubscribe admission
	// path. Zero falls back to the executor's local start.
	Receipt time.Time
}

// SubscriptionSetCommandResponse is the result envelope the executor
// hands to the protocol-side Reply closure for a set-based subscribe.
// Exactly one of MultiApplied, SingleApplied, or Error is set — the
// Reply closure inspects the populated arm and enqueues the
// corresponding wire message on the connection's OutboundCh
// synchronously, preserving ADR §9.4 FIFO ordering.
type SubscriptionSetCommandResponse struct {
	MultiApplied  *SubscribeMultiApplied
	SingleApplied *SubscribeSingleApplied
	Error         *SubscriptionError
}

// UnsubscribeSetCommandResponse mirrors SubscriptionSetCommandResponse
// for the unsubscribe path. Exactly one field is populated.
type UnsubscribeSetCommandResponse struct {
	MultiApplied  *UnsubscribeMultiApplied
	SingleApplied *UnsubscribeSingleApplied
	Error         *SubscriptionError
}

// CallReducerRequest carries the fields for a reducer invocation
// (SPEC-003 §10.3).
//
// Outcome-model decision (`docs/shunter-design-decisions.md#outcome-model`):
// the caller-visible reducer outcome is delivered as a heavy
// `TransactionUpdate` envelope through the subscription fan-out seam.
// ResponseCh carries that final heavy envelope back to the protocol
// handler, but the envelope is assembled by the executor's protocol
// adapter: committed responses use the shared heavy-envelope builder fed
// by the evaluator's authoritative caller-visible update slice; failed /
// pre-commit responses synthesize only the failure shell. When
// `CallReducerFlagsNoSuccessNotify` suppresses a committed success echo,
// the adapter closes ResponseCh without sending so the protocol watcher
// can exit cleanly. Pre-acceptance rejections (lifecycle-reducer-name
// collision, executor-unavailable) are surfaced via the `error` return of
// `ExecutorInbox.CallReducer` — the protocol layer synthesizes a heavy
// envelope with `StatusFailed` in that case.
type CallReducerRequest struct {
	ConnID              types.ConnectionID
	Identity            types.Identity
	RequestID           uint32
	ReducerName         string
	Args                []byte
	Permissions         []string
	AllowAllPermissions bool
	// Flags mirrors the wire `CallReducerFlags` byte. The executor / fan-out
	// seam reads this to decide whether to suppress the caller's
	// successful-commit heavy echo (`CallReducerFlagsNoSuccessNotify`).
	Flags      byte
	ResponseCh chan TransactionUpdate
	// Done signals the owning connection is being torn down (Conn.closed
	// closed at step 4 of the SPEC-005 §5.3 teardown). The executor-side
	// response-forwarding goroutine selects on this in addition to its
	// internal response channel, so it exits promptly when the conn goes
	// away rather than leaking if the executor never feeds the internal
	// channel (crash mid-commit, hung reducer, engine shutdown). A nil
	// Done blocks forever on its select arm, matching pre-wire behavior
	// for callers that do not wire a lifecycle signal. Analog to the
	// `watchReducerResponse` hardening on the protocol-side watcher.
	Done <-chan struct{}
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
//     encode + send IdentityToken as the first binary frame. On a
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

	// Register before first send: the fan-out worker (fan-out integration) resolves
	// ConnectionID → Conn via this manager, and admitting a connection
	// that is not yet visible would drop its first delta. Order is
	// safe because RunLifecycle is synchronous and IdentityToken is
	// the very next thing emitted on this socket.
	mgr.Add(c)

	frame, err := encodeIdentityTokenFrame(IdentityToken{
		Identity:     c.Identity,
		ConnectionID: c.ID,
		Token:        c.Token,
	}, c.Compression)
	if err != nil {
		mgr.Remove(c.ID)
		_ = c.ws.Close(websocket.StatusInternalError, "encode IdentityToken")
		return fmt.Errorf("encode IdentityToken: %w", err)
	}
	if err := c.ws.Write(ctx, websocket.MessageBinary, frame); err != nil {
		mgr.Remove(c.ID)
		_ = c.ws.Close(websocket.StatusInternalError, "write IdentityToken")
		return fmt.Errorf("write IdentityToken: %w", err)
	}
	return nil
}

// encodeIdentityTokenFrame serializes the IdentityToken server
// message per SPEC-005 §8.1 and wraps it in the correct transport
// envelope. When compression was negotiated at upgrade time, the
// handshake frame still carries its compression byte (CompressionNone)
// per §3.3 so the client's decoder branches consistently; the
// handshake body itself is never gzipped.
func encodeIdentityTokenFrame(msg IdentityToken, compressionNegotiated bool) ([]byte, error) {
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
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	if len(s) <= maxLen {
		return s
	}
	end := 0
	for end < len(s) {
		_, size := utf8.DecodeRuneInString(s[end:])
		if end+size > maxLen {
			break
		}
		end += size
	}
	return s[:end]
}
