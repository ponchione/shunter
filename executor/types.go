package executor

import "github.com/ponchione/shunter/types"

// ScheduleID identifies a scheduled reducer entry.
type ScheduleID uint64

// SubscriptionID aliases the canonical SubscriptionID in types.
// Canonical home: types.SubscriptionID. Alias kept so existing executor
// callers and tests continue to compile.
type SubscriptionID = types.SubscriptionID

// CallSource identifies how a reducer invocation was triggered.
type CallSource int

const (
	CallSourceExternal  CallSource = iota // client RPC
	CallSourceScheduled                   // scheduled reducer firing
	CallSourceLifecycle                   // OnConnect / OnDisconnect
)

// ReducerStatus describes the outcome of a reducer execution.
type ReducerStatus int

const (
	StatusCommitted      ReducerStatus = iota // committed successfully
	StatusFailedUser                          // reducer returned an error
	StatusFailedPanic                         // reducer panicked
	StatusFailedInternal                      // executor-level failure
)

// LifecycleKind identifies the lifecycle hook type for a reducer.
type LifecycleKind int

const (
	LifecycleNone         LifecycleKind = iota // normal reducer
	LifecycleOnConnect                         // invoked on client connect
	LifecycleOnDisconnect                      // invoked on client disconnect
)
