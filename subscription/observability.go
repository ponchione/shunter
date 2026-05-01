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
