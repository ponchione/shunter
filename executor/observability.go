package executor

import (
	"errors"
	"runtime/debug"
	"time"

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
	RecordExecutorCommand(kind, result string)
	RecordExecutorCommandDuration(kind, result string, duration time.Duration)
	RecordExecutorInboxDepth(depth int)
	RecordReducerCall(reducer, result string)
	RecordReducerDuration(reducer, result string, duration time.Duration)
}

type reducerTraceObserver interface {
	TraceReducerCall(reducer, result string, err error)
}

type storeCommitTraceObserver interface {
	TraceStoreCommit(txID types.TxID, result string, err error)
}

type subscriptionRegisterTraceObserver interface {
	TraceSubscriptionRegister(result string, err error)
}

type subscriptionUnregisterTraceObserver interface {
	TraceSubscriptionUnregister(result string, err error)
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

func (e *Executor) recordExecutorCommand(cmd ExecutorCommand, result string) {
	if e != nil && e.observer != nil {
		e.observer.RecordExecutorCommand(executorCommandKind(cmd), executorCommandResult(result))
	}
}

func (e *Executor) recordExecutorCommandDuration(cmd ExecutorCommand, result string, duration time.Duration) {
	if e != nil && e.observer != nil {
		e.observer.RecordExecutorCommandDuration(executorCommandKind(cmd), executorCommandResult(result), duration)
	}
}

func (e *Executor) recordExecutorInboxDepth() {
	if e != nil && e.observer != nil {
		e.observer.RecordExecutorInboxDepth(e.InboxDepth())
	}
}

func (e *Executor) recordReducerMetric(reducer, result string, duration time.Duration, observedDuration bool) {
	if e == nil || e.observer == nil {
		return
	}
	reducer = reducerMetricName(reducer)
	result = reducerMetricResult(result)
	e.observer.RecordReducerCall(reducer, result)
	if observedDuration {
		e.observer.RecordReducerDuration(reducer, result, duration)
	}
}

func (e *Executor) traceReducerCall(reducer, result string, err error) {
	if e == nil || e.observer == nil {
		return
	}
	if observer, ok := e.observer.(reducerTraceObserver); ok {
		observer.TraceReducerCall(reducerMetricName(reducer), reducerMetricResult(result), err)
	}
}

func (e *Executor) traceStoreCommit(txID types.TxID, result string, err error) {
	if e == nil || e.observer == nil {
		return
	}
	if observer, ok := e.observer.(storeCommitTraceObserver); ok {
		observer.TraceStoreCommit(txID, result, err)
	}
}

func (e *Executor) traceSubscriptionRegister(result string, err error) {
	if e == nil || e.observer == nil {
		return
	}
	if observer, ok := e.observer.(subscriptionRegisterTraceObserver); ok {
		observer.TraceSubscriptionRegister(executorCommandResult(result), err)
	}
}

func (e *Executor) traceSubscriptionUnregister(result string, err error) {
	if e == nil || e.observer == nil {
		return
	}
	if observer, ok := e.observer.(subscriptionUnregisterTraceObserver); ok {
		observer.TraceSubscriptionUnregister(executorCommandResult(result), err)
	}
}

func executorCommandKind(cmd ExecutorCommand) string {
	switch c := cmd.(type) {
	case CallReducerCmd:
		if c.Request.Source == CallSourceScheduled {
			return "scheduler_fire"
		}
		return "call_reducer"
	case RegisterSubscriptionSetCmd:
		return "register_subscription_set"
	case UnregisterSubscriptionSetCmd:
		return "unregister_subscription_set"
	case DisconnectClientSubscriptionsCmd:
		return "disconnect_client_subscriptions"
	case OnConnectCmd:
		return "on_connect"
	case OnDisconnectCmd:
		return "on_disconnect"
	default:
		return "unknown"
	}
}

func executorCommandResult(result string) string {
	switch result {
	case "ok", "user_error", "panic", "internal_error", "permission_denied", "rejected", "canceled":
		return result
	default:
		return "internal_error"
	}
}

func executorCommandResultFromStatus(status ReducerStatus) string {
	switch status {
	case StatusCommitted:
		return "ok"
	case StatusFailedUser:
		return "user_error"
	case StatusFailedPanic:
		return "panic"
	case StatusFailedPermission:
		return "permission_denied"
	default:
		return "internal_error"
	}
}

func reducerMetricResultFromStatus(status ReducerStatus) string {
	switch status {
	case StatusCommitted:
		return "committed"
	case StatusFailedUser:
		return "failed_user"
	case StatusFailedPanic:
		return "failed_panic"
	case StatusFailedPermission:
		return "failed_permission"
	default:
		return "failed_internal"
	}
}

func reducerMetricResult(result string) string {
	switch result {
	case "committed", "failed_user", "failed_panic", "failed_internal", "failed_permission":
		return result
	default:
		return "failed_internal"
	}
}

func reducerMetricName(reducer string) string {
	if reducer == "" {
		return "unknown"
	}
	return reducer
}

func reducerTraceErrorFromStatus(status ReducerStatus) error {
	if status == StatusCommitted {
		return nil
	}
	return errors.New(reducerMetricResultFromStatus(status))
}
