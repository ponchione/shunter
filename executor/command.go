package executor

import (
	"context"
	"time"

	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

// ExecutorCommand is the interface for all executor inbox commands.
type ExecutorCommand interface {
	isExecutorCommand()
}

// CallReducerCmd requests a reducer invocation.
// Non-nil reply channels receive exactly one response before the next command.
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

// RegisterSubscriptionSetCmd atomically registers a subscription set.
// Reply runs synchronously on the executor goroutine before the next command.
type RegisterSubscriptionSetCmd struct {
	Request subscription.SubscriptionSetRegisterRequest
	Reply   func(subscription.SubscriptionSetRegisterResult, error)
	// Receipt is the protocol-handler receipt timestamp. When non-zero the
	// executor computes `TotalHostExecutionDurationMicros` as
	// `time.Since(Receipt)` at reply time so the wire duration reflects the
	// full admission path rather than only the subs-manager call.
	Receipt time.Time
}

func (RegisterSubscriptionSetCmd) isExecutorCommand() {}

// UnregisterSubscriptionSetCmd removes every subscription under one
// (ConnID, QueryID) key.
type UnregisterSubscriptionSetCmd struct {
	ConnID  types.ConnectionID
	QueryID uint32
	Reply   func(subscription.SubscriptionSetUnregisterResult, error)
	Context context.Context
	// Receipt mirrors RegisterSubscriptionSetCmd.Receipt for the unsubscribe
	// admission path.
	Receipt time.Time
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
