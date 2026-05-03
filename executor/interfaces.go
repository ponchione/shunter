package executor

import (
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// SchedulerHandle provides transactional access to the scheduled-reducer system.
// Operations are logically part of the current transaction — rolled back if the
// reducer fails. Implemented in SPEC-003 Epic 6.
type SchedulerHandle = types.ReducerScheduler

// DurabilityHandle receives committed changesets from the executor.
// EnqueueCommitted must not silently drop accepted commits.
type DurabilityHandle interface {
	EnqueueCommitted(txID types.TxID, changeset *store.Changeset)
	WaitUntilDurable(txID types.TxID) <-chan types.TxID
	FatalError() error
}

// SubscriptionManager evaluates subscriptions and reports dropped clients after
// each commit.
type SubscriptionManager interface {
	RegisterSet(req subscription.SubscriptionSetRegisterRequest, view store.CommittedReadView) (subscription.SubscriptionSetRegisterResult, error)
	UnregisterSet(connID types.ConnectionID, queryID uint32, view store.CommittedReadView) (subscription.SubscriptionSetUnregisterResult, error)
	EvalAndBroadcast(txID types.TxID, changeset *store.Changeset, view store.CommittedReadView, meta subscription.PostCommitMeta)
	DroppedClients() <-chan types.ConnectionID
	DisconnectClient(connID types.ConnectionID) error
}
