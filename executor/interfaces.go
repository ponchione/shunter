package executor

import (
	"time"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// SchedulerHandle provides transactional access to the scheduled-reducer system.
// Operations are logically part of the current transaction — rolled back if the
// reducer fails. Implemented in SPEC-003 Epic 6.
type SchedulerHandle interface {
	Schedule(reducerName string, args []byte, at time.Time) (ScheduleID, error)
	ScheduleRepeat(reducerName string, args []byte, interval time.Duration) (ScheduleID, error)
	Cancel(id ScheduleID) bool
}

// DurabilityHandle is the narrow surface the executor uses to hand committed
// changesets to the commit-log durability subsystem (SPEC-003 §7).
// EnqueueCommitted blocks only for bounded-queue backpressure; it MUST NOT
// drop accepted commits silently. The executor invokes it first in the
// post-commit pipeline (SPEC-003 §5.1).
type DurabilityHandle interface {
	EnqueueCommitted(txID types.TxID, changeset *store.Changeset)
}

// SubscriptionManager is the post-commit subscription-evaluation surface used
// by the executor. EvalAndBroadcast runs synchronously against the supplied
// committed read view and must return before the next executor command is
// dequeued (SPEC-003 §5.3).
//
// After response delivery, the executor drains `DroppedClients()` and calls
// `DisconnectClient` on each signaled connection (SPEC-003 §5 step 5,
// Story 5.2). The drain is non-blocking: a nil channel or empty channel
// exits immediately.
type SubscriptionManager interface {
	EvalAndBroadcast(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView)
	DroppedClients() <-chan types.ConnectionID
	DisconnectClient(connID types.ConnectionID) error
}
