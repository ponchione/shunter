package executor

import (
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// ExecutorCommand is the interface for all executor inbox commands.
type ExecutorCommand interface {
	isExecutorCommand()
}

// CallReducerCmd requests a reducer invocation.
type CallReducerCmd struct {
	Request    ReducerRequest
	ResponseCh chan<- ReducerResponse
}

func (CallReducerCmd) isExecutorCommand() {}

// RegisterSubscriptionSetCmd requests atomic set-scoped subscription
// registration. Part of the Phase 2 Slice 2 variant split.
//
// Reply is invoked synchronously on the executor goroutine (before the
// dispatch loop pulls the next command) with the register outcome. Err
// is non-nil only on register failure; on success Result carries the
// populated result. Callers supply a closure that enqueues the
// appropriate wire frame onto the target connection's OutboundCh — per
// ADR §9.4 this enqueue strictly precedes any subsequent fan-out for
// the same connection on the executor goroutine.
type RegisterSubscriptionSetCmd struct {
	Request subscription.SubscriptionSetRegisterRequest
	Reply   func(subscription.SubscriptionSetRegisterResult, error)
}

func (RegisterSubscriptionSetCmd) isExecutorCommand() {}

// UnregisterSubscriptionSetCmd removes every subscription registered
// under one (ConnID, QueryID) key.
// Part of the Phase 2 Slice 2 variant split.
//
// Reply is invoked synchronously on the executor goroutine with the
// unregister outcome. Err is non-nil on failure; on success Result
// carries the populated result. See RegisterSubscriptionSetCmd for the
// ordering contract.
type UnregisterSubscriptionSetCmd struct {
	ConnID  types.ConnectionID
	QueryID uint32
	Reply   func(subscription.SubscriptionSetUnregisterResult, error)
}

func (UnregisterSubscriptionSetCmd) isExecutorCommand() {}

// DisconnectClientSubscriptionsCmd removes all subscriptions for one client.
type DisconnectClientSubscriptionsCmd struct {
	ConnID     types.ConnectionID
	ResponseCh chan<- error
}

func (DisconnectClientSubscriptionsCmd) isExecutorCommand() {}

// OnConnectCmd is an internal command delivered by the protocol layer when a
// client connection is being admitted (SPEC-003 §10.3). It inserts the
// `sys_clients` row and runs the optional OnConnect lifecycle reducer inside
// a single transaction. A failed reducer rolls back the entire transaction.
type OnConnectCmd struct {
	ConnID     types.ConnectionID
	Identity   types.Identity
	ResponseCh chan<- ReducerResponse
}

func (OnConnectCmd) isExecutorCommand() {}

// OnDisconnectCmd is an internal command delivered by the protocol layer after
// the client is considered gone (SPEC-003 §10.4). Disconnect cannot be
// vetoed: if the OnDisconnect reducer fails, a cleanup transaction still
// deletes the `sys_clients` row.
type OnDisconnectCmd struct {
	ConnID     types.ConnectionID
	Identity   types.Identity
	ResponseCh chan<- ReducerResponse
}

func (OnDisconnectCmd) isExecutorCommand() {}
