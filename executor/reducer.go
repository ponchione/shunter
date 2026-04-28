package executor

import "github.com/ponchione/shunter/types"

// RegisteredReducer pairs a reducer name with its handler and lifecycle role.
// ID is assigned monotonically by the registry at Register time so the
// outcome-model heavy TransactionUpdate envelope can carry the reference
// ReducerCallInfo.ReducerId field (u32).
type RegisteredReducer struct {
	Name                string
	Handler             types.ReducerHandler
	Lifecycle           LifecycleKind
	RequiredPermissions []string
	ID                  uint32
}

// ReducerRequest is the input to a reducer call.
type ReducerRequest struct {
	ReducerName string
	Args        []byte
	Caller      types.CallerContext
	RequestID   uint32
	Source      CallSource
	// Flags mirrors the wire `CallReducerFlags` byte. Propagated into
	// `subscription.CallerOutcome.Flags` so the fan-out worker can honor
	// caller opt-outs such as `NoSuccessNotify`.
	Flags byte
	// ScheduleID and IntendedFireAt are populated when Source ==
	// CallSourceScheduled. IntendedFireAt is the sys_scheduled row's
	// next_run_at_ns at enqueue time; firing uses it so repeat
	// semantics are fixed-rate (SPEC-003 §9.5) rather than
	// completion-time-based. Both are zero for external / lifecycle
	// calls.
	ScheduleID     ScheduleID
	IntendedFireAt int64
}

// ReducerResponse is the result of a reducer execution.
type ReducerResponse struct {
	Status      ReducerStatus
	Error       error
	ReturnBSATN []byte
	TxID        types.TxID
}
