package types

import "time"

// ReducerHandler is the raw runtime signature for all reducers.
type ReducerHandler func(ctx *ReducerContext, argBSATN []byte) ([]byte, error)

// ReducerContext is the execution context passed to a reducer.
// Valid only during synchronous reducer invocation on the executor goroutine.
// Reducers must not retain it after return, use it from another goroutine, or
// perform blocking network/disk/RPC I/O while holding the executor.
type ReducerContext struct {
	ReducerName string
	Caller      CallerContext
	DB          any // *store.Transaction at runtime
	Scheduler   any // executor.SchedulerHandle at runtime
}

// CallerContext captures the identity and timing of a reducer invocation.
type CallerContext struct {
	Identity     Identity
	ConnectionID ConnectionID
	Timestamp    time.Time
}
