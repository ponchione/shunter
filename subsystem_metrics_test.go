package shunter

import (
	"testing"
	"time"

	"github.com/ponchione/shunter/types"
)

func TestSubsystemMetricsUseExactFamiliesAndLabels(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "rt-a",
		Metrics: MetricsConfig{
			Enabled:  true,
			Recorder: metrics,
		},
	})

	obs.RecordProtocolConnections(2)
	obs.RecordProtocolMessage("call_reducer", "ok")
	obs.LogProtocolBackpressure("inbound", "buffer_full")
	obs.RecordExecutorInboxDepth(3)
	obs.RecordExecutorCommand("call_reducer", "rejected")
	obs.RecordExecutorCommandDuration("call_reducer", "ok", 2*time.Millisecond)
	obs.RecordReducerCall("send_message", "committed")
	obs.RecordReducerDuration("send_message", "committed", 3*time.Millisecond)
	obs.RecordDurabilityQueueDepth(4)
	obs.RecordDurabilityDurableTxID(types.TxID(9))
	obs.LogDurabilityFailed(assertErr("disk"), "sync_failed", types.TxID(9))
	obs.RecordSubscriptionActive(5)
	obs.RecordSubscriptionEvalDuration("error", 4*time.Millisecond)
	obs.LogSubscriptionFanoutError("send_failed", nil, assertErr("send"))
	obs.LogSubscriptionClientDropped("buffer_full", nil)

	metrics.requireGauge(t, MetricProtocolConnections, MetricLabels{Module: "chat", Runtime: "rt-a"}, 2)
	metrics.requireCounter(t, MetricProtocolMessagesTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Kind: "call_reducer", Result: "ok"}, 1)
	metrics.requireCounter(t, MetricProtocolBackpressureTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Direction: "inbound"}, 1)
	metrics.requireGauge(t, MetricExecutorInboxDepth, MetricLabels{Module: "chat", Runtime: "rt-a"}, 3)
	metrics.requireCounter(t, MetricExecutorCommandsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Kind: "call_reducer", Result: "rejected"}, 1)
	requireHistogram(t, metrics, MetricExecutorCommandDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Kind: "call_reducer", Result: "ok"})
	metrics.requireCounter(t, MetricReducerCallsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reducer: "send_message", Result: "committed"}, 1)
	requireHistogram(t, metrics, MetricReducerDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Reducer: "send_message", Result: "committed"})
	metrics.requireGauge(t, MetricDurabilityQueueDepth, MetricLabels{Module: "chat", Runtime: "rt-a"}, 4)
	metrics.requireGauge(t, MetricDurabilityDurableTxID, MetricLabels{Module: "chat", Runtime: "rt-a"}, 9)
	metrics.requireCounter(t, MetricDurabilityFailuresTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reason: "sync_failed"}, 1)
	metrics.requireGauge(t, MetricSubscriptionActive, MetricLabels{Module: "chat", Runtime: "rt-a"}, 5)
	requireHistogram(t, metrics, MetricSubscriptionEvalDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Result: "error"})
	metrics.requireCounter(t, MetricSubscriptionFanoutErrorsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reason: "send_failed"}, 1)
	metrics.requireCounter(t, MetricSubscriptionDroppedClientsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reason: "buffer_full"}, 1)
}

func TestReducerMetricsAggregateLabelModeUsesAll(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "rt-a",
		Metrics: MetricsConfig{
			Enabled:          true,
			Recorder:         metrics,
			ReducerLabelMode: ReducerLabelModeAggregate,
		},
	})

	obs.RecordReducerCall("send_message", "committed")
	obs.RecordReducerDuration("send_message", "committed", time.Millisecond)

	metrics.requireCounter(t, MetricReducerCallsTotal, MetricLabels{Module: "chat", Runtime: "rt-a", Reducer: "_all", Result: "committed"}, 1)
	requireHistogram(t, metrics, MetricReducerDurationSeconds, MetricLabels{Module: "chat", Runtime: "rt-a", Reducer: "_all", Result: "committed"})
}

func requireHistogram(t *testing.T, metrics *recordingMetricsRecorder, name MetricName, labels MetricLabels) {
	t.Helper()
	for _, observation := range metrics.snapshot() {
		if observation.kind == "histogram" && observation.name == name && observation.labels == labels && observation.value > 0 {
			return
		}
	}
	t.Fatalf("missing histogram %s labels=%+v in %+v", name, labels, metrics.snapshot())
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
