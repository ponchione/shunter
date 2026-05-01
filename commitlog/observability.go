package commitlog

import "github.com/ponchione/shunter/types"

// Observer receives runtime-scoped commitlog observations. Nil means no-op;
// package-level commitlog use before a runtime exists remains silent.
type Observer interface {
	LogDurabilityFailed(err error, reason string, txID types.TxID)
}

func recordDurabilityFailed(observer Observer, err error, reason string, txID uint64) {
	if observer == nil || err == nil {
		return
	}
	observer.LogDurabilityFailed(err, reason, types.TxID(txID))
}
