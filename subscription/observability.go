package subscription

import (
	"time"

	"github.com/ponchione/shunter/types"
)

// Observer receives runtime-scoped subscription observations. Nil means no-op
// for package-level tests or pre-runtime use.
type Observer interface {
	LogSubscriptionEvalError(txID types.TxID, err error)
	LogSubscriptionFanoutError(reason string, connID *types.ConnectionID, err error)
	LogSubscriptionClientDropped(reason string, connID *types.ConnectionID)
	LogProtocolBackpressure(direction, reason string)
	RecordSubscriptionActive(active int)
	RecordSubscriptionEvalDuration(result string, duration time.Duration)
}

type subscriptionEvalTraceObserver interface {
	TraceSubscriptionEval(txID types.TxID, result string, err error)
}

type subscriptionFanoutTraceObserver interface {
	TraceSubscriptionFanout(result, reason string, err error)
}

type subscriptionFanoutBlockedObserver interface {
	RecordSubscriptionFanoutBlockedDuration(duration time.Duration)
}

func recordSubscriptionActive(observer Observer, active int) {
	if observer != nil {
		observer.RecordSubscriptionActive(active)
	}
}

func recordSubscriptionEvalDuration(observer Observer, result string, duration time.Duration) {
	if observer != nil {
		observer.RecordSubscriptionEvalDuration(result, duration)
	}
}

func traceSubscriptionEval(observer Observer, txID types.TxID, result string, err error) {
	if observer == nil {
		return
	}
	if tracer, ok := observer.(subscriptionEvalTraceObserver); ok {
		tracer.TraceSubscriptionEval(txID, result, err)
	}
}

func traceSubscriptionFanout(observer Observer, result, reason string, err error) {
	if observer == nil {
		return
	}
	if tracer, ok := observer.(subscriptionFanoutTraceObserver); ok {
		tracer.TraceSubscriptionFanout(result, reason, err)
	}
}

func recordSubscriptionFanoutBlockedDuration(observer Observer, duration time.Duration) {
	if observer == nil || duration <= 0 {
		return
	}
	if recorder, ok := observer.(subscriptionFanoutBlockedObserver); ok {
		recorder.RecordSubscriptionFanoutBlockedDuration(duration)
	}
}
