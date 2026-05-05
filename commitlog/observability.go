package commitlog

import (
	"time"

	"github.com/ponchione/shunter/types"
)

// Observer receives runtime-scoped commitlog observations. Nil means no-op;
// package-level commitlog use before a runtime exists remains silent.
type Observer interface {
	LogDurabilityFailed(err error, reason string, txID types.TxID)
	RecordDurabilityQueueDepth(depth int)
	RecordDurabilityDurableTxID(txID types.TxID)
}

// SnapshotObserver receives snapshot-writer observations. Nil means no-op.
type SnapshotObserver interface {
	RecordSnapshotDuration(result string, duration time.Duration)
}

type durabilityBatchTraceObserver interface {
	TraceDurabilityBatch(txID types.TxID, result string, err error)
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

func traceDurabilityBatch(observer Observer, txID uint64, result string, err error) {
	if observer == nil {
		return
	}
	if tracer, ok := observer.(durabilityBatchTraceObserver); ok {
		tracer.TraceDurabilityBatch(types.TxID(txID), result, err)
	}
}

func recordSnapshotDuration(observer SnapshotObserver, result string, duration time.Duration) {
	if observer != nil {
		observer.RecordSnapshotDuration(result, duration)
	}
}

func resultFromErr(err error) string {
	if err == nil {
		return "ok"
	}
	return "error"
}
