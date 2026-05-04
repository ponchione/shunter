package executor

import "errors"

var (
	ErrExecutorBusy                      = errors.New("executor: inbox full")
	ErrExecutorShutdown                  = errors.New("executor: shut down")
	ErrExecutorFatal                     = errors.New("executor: fatal state")
	ErrExecutorNotStarted                = errors.New("executor: external admission closed until Startup completes")
	ErrExecutorUnbufferedResponseChannel = errors.New("executor: unbuffered response channel")
	ErrReducerNotFound                   = errors.New("executor: reducer not found")
	ErrLifecycleReducer                  = errors.New("executor: lifecycle reducer cannot be called externally")
	ErrReducerPanic                      = errors.New("executor: reducer panic")
	ErrPermissionDenied                  = errors.New("executor: permission denied")
	ErrCommitFailed                      = errors.New("executor: commit failed")
	ErrTxIDExhausted                     = errors.New("executor: tx id exhausted")
	ErrInvalidScheduleInterval           = errors.New("executor: schedule repeat interval must be positive")
	ErrScheduleIDExhausted               = errors.New("executor: schedule id exhausted")
)
