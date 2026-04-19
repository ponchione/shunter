package subscription

import "github.com/ponchione/shunter/types"

// FanOutMessage is the handoff payload between the executor's evaluation
// loop and the fan-out worker (SPEC-004 §8.1 / Story 6.1).
//
// Phase 1.5 outcome-model decision (`docs/parity-phase1.5-outcome-model.md`):
// when the commit originated from a caller-addressable reducer call,
// `CallerConnID` identifies the caller so the fan-out worker can keep that
// connection out of non-caller light delivery. `CallerOutcome` is populated
// only when the fan-out worker itself owns the caller's heavy
// `TransactionUpdate` envelope; protocol-originated reducer replies may carry
// `CallerConnID` with a nil `CallerOutcome` because the protocol inbox adapter
// owns the heavy reply directly while still reusing evaluator-derived caller
// updates.
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

// SubscriptionError is the protocol-facing evaluation-failure payload queued
// for clients affected by a broken subscription. SPEC-005 owns wire encoding;
// this package only carries the semantic content across the fan-out seam.
type SubscriptionError struct {
	RequestID      uint32
	SubscriptionID types.SubscriptionID
	QueryHash      QueryHash
	Predicate      string
	Message        string
}

// CallerOutcomeKind is the discriminant for `CallerOutcome`. It maps
// directly onto the reference `UpdateStatus` tagged union. See
// `docs/parity-phase1.5-outcome-model.md`.
type CallerOutcomeKind uint8

const (
	CallerOutcomeCommitted CallerOutcomeKind = iota
	CallerOutcomeFailed
	CallerOutcomeOutOfEnergy
)

// CallerOutcome carries the caller-visible reducer outcome plus the
// metadata required by the protocol layer to assemble the heavy
// `TransactionUpdate` envelope. The `Kind` field selects the
// `UpdateStatus` arm on the wire. `Error` is only read when Kind is
// `CallerOutcomeFailed`. Row deltas are not carried here — they are
// produced by the evaluator and delivered alongside the outcome by the
// fan-out worker / adapter.
type CallerOutcome struct {
	Kind                       CallerOutcomeKind
	Error                      string
	CallerIdentity             [32]byte
	ReducerName                string
	ReducerID                  uint32
	Args                       []byte
	RequestID                  uint32
	Timestamp                  int64
	EnergyQuantaUsed           uint64
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
