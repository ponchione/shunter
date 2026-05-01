package subscription

import "github.com/ponchione/shunter/types"

// Observer receives runtime-scoped subscription observations. Nil means no-op
// for package-level tests or pre-runtime use.
type Observer interface {
	LogSubscriptionEvalError(txID types.TxID, err error)
	LogSubscriptionFanoutError(reason string, connID *types.ConnectionID, err error)
	LogSubscriptionClientDropped(reason string, connID *types.ConnectionID)
	LogProtocolBackpressure(direction, reason string)
}
