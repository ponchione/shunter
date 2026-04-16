package executor

import "errors"

var (
	ErrExecutorBusy     = errors.New("executor: inbox full")
	ErrExecutorShutdown = errors.New("executor: shut down")
	ErrExecutorFatal    = errors.New("executor: fatal state")
	ErrReducerNotFound  = errors.New("executor: reducer not found")
	ErrLifecycleReducer = errors.New("executor: lifecycle reducer cannot be called externally")
	ErrReducerPanic     = errors.New("executor: reducer panic")
	ErrCommitFailed     = errors.New("executor: commit failed")
)
