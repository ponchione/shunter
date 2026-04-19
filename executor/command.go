package executor

import (
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// SubscriptionRegisterRequest aliases the canonical SPEC-004 registration
// request so executor command contracts can reference the subscription-owned
// type without duplicating its shape.
type SubscriptionRegisterRequest = subscription.SubscriptionRegisterRequest

// SubscriptionRegisterResult aliases the canonical SPEC-004 registration
// result so executor command contracts can reference the subscription-owned
// type without duplicating its shape.
type SubscriptionRegisterResult = subscription.SubscriptionRegisterResult

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

// RegisterSubscriptionCmd requests atomic subscription registration through the
// executor queue.
type RegisterSubscriptionCmd struct {
	Request    SubscriptionRegisterRequest
	ResponseCh chan<- SubscriptionRegisterResult
}

func (RegisterSubscriptionCmd) isExecutorCommand() {}

// UnregisterSubscriptionCmd removes one connection-owned subscription.
type UnregisterSubscriptionCmd struct {
	ConnID         types.ConnectionID
	SubscriptionID types.SubscriptionID
	ResponseCh     chan<- error
}

func (UnregisterSubscriptionCmd) isExecutorCommand() {}

// RegisterSubscriptionSetCmd requests atomic set-scoped subscription
// registration. Reference-aligned replacement for RegisterSubscriptionCmd.
// Part of the Phase 2 Slice 2 variant split.
type RegisterSubscriptionSetCmd struct {
	Request    subscription.SubscriptionSetRegisterRequest
	ResponseCh chan<- subscription.SubscriptionSetRegisterResult
}

func (RegisterSubscriptionSetCmd) isExecutorCommand() {}

// UnregisterSubscriptionSetCmd removes every subscription registered
// under one (ConnID, QueryID) key.
type UnregisterSubscriptionSetCmd struct {
	ConnID     types.ConnectionID
	QueryID    uint32
	ResponseCh chan<- UnregisterSubscriptionSetResponse
}

func (UnregisterSubscriptionSetCmd) isExecutorCommand() {}

// UnregisterSubscriptionSetResponse carries either the final delta
// (on success) or an error.
type UnregisterSubscriptionSetResponse struct {
	Result subscription.SubscriptionSetUnregisterResult
	Err    error
}

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
