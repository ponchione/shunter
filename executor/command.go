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
//
// ResponseCh and ProtocolResponseCh are executor-owned reply paths: when they
// are non-nil, the executor sends exactly one response before it dequeues the
// next command. Public callers should submit buffered channels (the protocol
// adapter uses cap 1): Submit/SubmitWithContext reject unbuffered response
// channels up front, while commands admitted to the inbox still use blocking
// reply sends to preserve delivery and ordering.
type CallReducerCmd struct {
	Request            ReducerRequest
	ResponseCh         chan<- ReducerResponse
	ProtocolResponseCh chan<- ProtocolCallReducerResponse
}

func (CallReducerCmd) isExecutorCommand() {}

// CommittedCallerPayload carries the adapter-specific committed reducer-call
// data the protocol edge needs to build an honest heavy TransactionUpdate.
// Generic executor callers keep using ReducerResponse.
type CommittedCallerPayload struct {
	Outcome subscription.CallerOutcome
	Updates []subscription.SubscriptionUpdate
}

// ProtocolCallReducerResponse complements ReducerResponse for the protocol
// adapter path. Committed is populated only for committed external reducer
// calls after synchronous post-commit evaluation has produced the real
// caller-visible update slice.
type ProtocolCallReducerResponse struct {
	Reducer   ReducerResponse
	Committed *CommittedCallerPayload
}

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
//
// ResponseCh follows the same executor-owned reply contract as CallReducerCmd:
// public Submit/SubmitWithContext calls reject unbuffered channels, while
// commands admitted to the inbox still use blocking reply sends.
type DisconnectClientSubscriptionsCmd struct {
	ConnID     types.ConnectionID
	ResponseCh chan<- error
}

func (DisconnectClientSubscriptionsCmd) isExecutorCommand() {}

// OnConnectCmd is an internal command delivered by the protocol layer when a
// client connection is being admitted (SPEC-003 §10.3). It inserts the
// `sys_clients` row and runs the optional OnConnect lifecycle reducer inside
// a single transaction. A failed reducer rolls back the entire transaction.
// ResponseCh follows the same Submit-time rejection / in-inbox blocking contract
// as CallReducerCmd.
type OnConnectCmd struct {
	ConnID     types.ConnectionID
	Identity   types.Identity
	ResponseCh chan<- ReducerResponse
}

func (OnConnectCmd) isExecutorCommand() {}

// OnDisconnectCmd is an internal command delivered by the protocol layer after
// the client is considered gone (SPEC-003 §10.4). Disconnect cannot be
// vetoed: if the OnDisconnect reducer fails, a cleanup transaction still
// deletes the `sys_clients` row. ResponseCh follows the same Submit-time
// rejection / in-inbox blocking contract as CallReducerCmd.
type OnDisconnectCmd struct {
	ConnID     types.ConnectionID
	Identity   types.Identity
	ResponseCh chan<- ReducerResponse
}

func (OnDisconnectCmd) isExecutorCommand() {}
