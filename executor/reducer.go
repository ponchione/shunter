package executor

import "github.com/ponchione/shunter/types"

// RegisteredReducer pairs a reducer name with its handler and lifecycle role.
type RegisteredReducer struct {
	Name      string
	Handler   types.ReducerHandler
	Lifecycle LifecycleKind
}

// ReducerRequest is the input to a reducer call.
type ReducerRequest struct {
	ReducerName string
	Args        []byte
	Caller      types.CallerContext
	Source      CallSource
}

// ReducerResponse is the result of a reducer execution.
type ReducerResponse struct {
	Status      ReducerStatus
	Error       error
	ReturnBSATN []byte
	TxID        types.TxID
}
