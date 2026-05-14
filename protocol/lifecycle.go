package protocol

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ponchione/websocket"

	"github.com/ponchione/shunter/types"
)

// ExecutorInbox is the protocol-to-executor handoff interface.
// Lifecycle disconnect first drops subscriptions, then runs OnDisconnect;
// disconnect errors are logged and do not veto teardown.
type ExecutorInbox interface {
	OnConnect(ctx context.Context, connID types.ConnectionID, identity types.Identity, principal types.AuthPrincipal) error
	OnDisconnect(ctx context.Context, connID types.ConnectionID, identity types.Identity, principal types.AuthPrincipal) error
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

// RegisterSubscriptionSetRequest carries a Single or Multi subscription set.
// Reply is invoked synchronously on the executor goroutine with exactly one
// populated response arm.
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
	// SQLText is the original SingleSubscribe SQL used to wrap initial-eval
	// errors. Empty on Multi and non-SQL paths.
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

// SubscriptionSetCommandResponse is the subscribe result envelope.
// Exactly one field is populated.
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

// CallReducerRequest carries a reducer invocation and the channel for the
// caller-visible TransactionUpdate envelope.
type CallReducerRequest struct {
	ConnID              types.ConnectionID
	Identity            types.Identity
	Principal           types.AuthPrincipal
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
	// Done closes with the owning connection so response forwarding can exit
	// if the executor never produces a result.
	Done <-chan struct{}
}

// RunLifecycle admits one connection and sends its IdentityToken.
// OnConnect rejection closes the socket without registering the connection.
func (c *Conn) RunLifecycle(ctx context.Context, inbox ExecutorInbox, mgr *ConnManager) error {
	c.bindDisconnect(inbox, mgr)
	if err := mgr.reserve(c); err != nil {
		_ = c.ws.Close(websocket.StatusPolicyViolation, "connection_id already in use")
		return err
	}
	reserved := true
	defer func() {
		if reserved {
			mgr.releaseReservation(c)
		}
	}()
	if err := inbox.OnConnect(ctx, c.ID, c.Identity, c.Principal.Copy()); err != nil {
		_ = c.ws.Close(websocket.StatusPolicyViolation, CloseReasonOnConnectRejected)
		return fmt.Errorf("%w: onconnect rejected: %v", ErrExecutorAdmissionRejected, err)
	}

	// Register before first send so immediate fan-out can resolve this Conn.
	if err := mgr.Add(c); err != nil {
		_ = c.ws.Close(websocket.StatusPolicyViolation, "connection_id already in use")
		return err
	}
	reserved = false
	recordProtocolConnections(c.Observer, mgr.ActiveCount())

	frame, err := encodeIdentityTokenFrame(IdentityToken{
		Identity:     c.Identity,
		ConnectionID: c.ID,
		Token:        c.Token,
	}, c.Compression)
	if err != nil {
		c.disconnectAfterAdmittedFailure(inbox, mgr, websocket.StatusInternalError, "encode IdentityToken")
		return fmt.Errorf("encode IdentityToken: %w", err)
	}
	select {
	case <-ctx.Done():
		c.disconnectAfterAdmittedFailure(inbox, mgr, websocket.StatusInternalError, "write IdentityToken")
		return fmt.Errorf("write IdentityToken: %w", ctx.Err())
	default:
	}
	if err := c.writeBinary(ctx, frame); err != nil {
		c.disconnectAfterAdmittedFailure(inbox, mgr, websocket.StatusInternalError, "write IdentityToken")
		return fmt.Errorf("write IdentityToken: %w", err)
	}
	logProtocolConnectionOpened(c.Observer, c.ID)
	return nil
}

func (c *Conn) disconnectAfterAdmittedFailure(inbox ExecutorInbox, mgr *ConnManager, code websocket.StatusCode, reason string) {
	disconnectCtx, cancel := context.WithTimeout(context.Background(), c.disconnectTimeout())
	defer cancel()
	c.Disconnect(disconnectCtx, code, reason, inbox, mgr)
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
