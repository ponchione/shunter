package executor

import (
	"runtime/debug"

	"github.com/ponchione/shunter/types"
)

// Observer receives runtime-scoped executor and executor-owned downstream
// observations. Nil means no-op for standalone package tests.
type Observer interface {
	PanicStackEnabled() bool
	LogExecutorFatal(err error, reason string, txID types.TxID)
	LogExecutorReducerPanic(reducer string, err error, txID types.TxID, stack string)
	LogExecutorLifecycleReducerFailed(reducer, result string, err error)
	LogSubscriptionFanoutError(reason string, connID *types.ConnectionID, err error)
}

func observerPanicStackEnabled(observer Observer) bool {
	return observer != nil && observer.PanicStackEnabled()
}

func capturePanicStack(observer Observer) string {
	if !observerPanicStackEnabled(observer) {
		return ""
	}
	return string(debug.Stack())
}

func (e *Executor) recordExecutorFatal(err error, reason string, txID types.TxID) {
	if e != nil && e.observer != nil {
		e.observer.LogExecutorFatal(err, reason, txID)
	}
}

func (e *Executor) recordReducerPanic(reducer string, err error, txID types.TxID, stack string) {
	if e != nil && e.observer != nil {
		e.observer.LogExecutorReducerPanic(reducer, err, txID, stack)
	}
}

func (e *Executor) recordLifecycleReducerFailed(reducer, result string, err error) {
	if e != nil && e.observer != nil {
		e.observer.LogExecutorLifecycleReducerFailed(reducer, result, err)
	}
}

func (e *Executor) recordSubscriptionFanoutError(reason string, connID types.ConnectionID, err error) {
	if e != nil && e.observer != nil {
		e.observer.LogSubscriptionFanoutError(reason, &connID, err)
	}
}
