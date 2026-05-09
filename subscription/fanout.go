package subscription

import (
	"context"

	"github.com/ponchione/shunter/types"
)

// FanOutMessage is the handoff payload from evaluation to the fan-out worker.
// CallerConnID suppresses the caller's light delivery; CallerOutcome is set
// only when fan-out owns the heavy caller response.
type FanOutMessage struct {
	// TxID identifies the transaction this payload came from. Zero for
	// synthetic caller-outcome deliveries with no underlying commit.
	TxID types.TxID

	// TxDurable becomes ready when the transaction is durable. The fan-out
	// worker consumes readiness when the recipient requires confirmed reads.
	// Nil is allowed and means "treat as already durable" (fast reads only).
	TxDurable <-chan types.TxID

	// Fanout holds the per-connection subscription updates produced for
	// this commit.
	Fanout CommitFanout

	// Errors holds per-connection subscription evaluation failures that should
	// be delivered as SubscriptionError messages before cleanup. Delivery wiring
	// is owned by the deferred Epic 6 / SPEC-005 integration path.
	Errors map[types.ConnectionID][]SubscriptionError

	// CallerConnID, when non-nil, identifies the caller connection. The
	// fan-out worker routes this connection's delivery into the heavy
	// envelope regardless of whether the evaluator produced any row-touches.
	CallerConnID *types.ConnectionID

	// CallerOutcome carries the caller-visible reducer outcome and the
	// metadata required to assemble the heavy `TransactionUpdate`
	// envelope when the fan-out worker owns caller-heavy delivery. Nil is
	// allowed when some other seam (for example the protocol inbox
	// adapter) owns the caller's heavy reply and FanOutMessage only needs
	// CallerConnID to suppress the caller's light echo.
	CallerOutcome *CallerOutcome
}

// PostCommitMeta carries executor-owned delivery metadata into the
// subscription fan-out seam. Zero value means ordinary non-caller,
// fast-read delivery.
type PostCommitMeta struct {
	// Context bounds post-commit subscription evaluation. Nil means Background.
	Context context.Context
	// FanoutContext bounds enqueueing the evaluated fan-out message. Nil means
	// Background so evaluation cancellation can still deliver eval errors.
	FanoutContext context.Context
	TxDurable     <-chan types.TxID
	CallerConnID  *types.ConnectionID
	CallerOutcome *CallerOutcome
	// CaptureCallerUpdates, when non-nil, receives the authoritative
	// caller-visible update slice extracted from the same per-connection
	// fanout map entry that would be delivered to the caller connection.
	// EvalAndBroadcast invokes it synchronously on the executor goroutine
	// before enqueueing the FanOutMessage.
	CaptureCallerUpdates func([]SubscriptionUpdate)
}

// SubscriptionError is the evaluation-failure payload queued for clients.
// TotalHostExecutionDurationMicros is shared by all errors from one eval pass.
type SubscriptionError struct {
	RequestID                        uint32
	SubscriptionID                   types.SubscriptionID
	QueryHash                        QueryHash
	Predicate                        string
	Message                          string
	TotalHostExecutionDurationMicros uint64
}

// CallerOutcomeKind is the discriminant for `CallerOutcome`. It maps
// directly onto the protocol `UpdateStatus` tagged union.
type CallerOutcomeKind uint8

const (
	CallerOutcomeCommitted CallerOutcomeKind = iota
	CallerOutcomeFailed
)

// CallerOutcome carries reducer outcome metadata for the heavy caller envelope.
// Row deltas are produced separately by the evaluator.
type CallerOutcome struct {
	Kind                       CallerOutcomeKind
	Error                      string
	CallerIdentity             [32]byte
	ReducerName                string
	ReducerID                  uint32
	Args                       []byte
	RequestID                  uint32
	Timestamp                  int64
	TotalHostExecutionDuration int64
	// Flags mirrors the `CallReducerFlags` byte received on the wire.
	// The fan-out worker reads this to suppress the caller's successful
	// heavy echo when `CallerOutcomeFlagNoSuccessNotify` is set.
	Flags byte
}

// CallerOutcome flag values mirror the wire `CallReducerFlags` byte. Keeping
// the constants in the subscription package lets the fan-out worker
// switch on outcome.Flags without importing the protocol layer.
const (
	CallerOutcomeFlagFullUpdate      byte = 0
	CallerOutcomeFlagNoSuccessNotify byte = 1
)
