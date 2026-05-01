package commitlog

import "github.com/ponchione/shunter/types"

// Observer receives runtime-scoped commitlog observations. Nil means no-op;
// package-level commitlog use before a runtime exists remains silent.
type Observer interface {
	LogDurabilityFailed(err error, reason string, txID types.TxID)
	RecordDurabilityQueueDepth(depth int)
	RecordDurabilityDurableTxID(txID types.TxID)
}

func recordDurabilityFailed(observer Observer, err error, reason string, txID uint64) {
	if observer == nil || err == nil {
		return
	}
	observer.LogDurabilityFailed(err, reason, types.TxID(txID))
}

func recordDurabilityQueueDepth(observer Observer, depth int) {
	if observer != nil {
		observer.RecordDurabilityQueueDepth(depth)
	}
}

func recordDurabilityDurableTxID(observer Observer, txID uint64) {
	if observer != nil {
		observer.RecordDurabilityDurableTxID(types.TxID(txID))
	}
}
