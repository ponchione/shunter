package shunter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ponchione/shunter/commitlog"
)

func TestRuntimeMetricsStateGaugesTrackLifecycleTransitions(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "lifecycle-a",
			Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	requireRuntimeStateOneHotGauge(t, metrics, "chat", "lifecycle-a", RuntimeStateBuilt)
	requireLatestGauge(t, metrics, MetricRuntimeReady, MetricLabels{Module: "chat", Runtime: "lifecycle-a"}, 0)
	requireLatestGauge(t, metrics, MetricRuntimeDegraded, MetricLabels{Module: "chat", Runtime: "lifecycle-a"}, 0)

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	requireRuntimeStateOneHotGauge(t, metrics, "chat", "lifecycle-a", RuntimeStateStarting)
	requireRuntimeStateOneHotGauge(t, metrics, "chat", "lifecycle-a", RuntimeStateReady)
	requireLatestGauge(t, metrics, MetricRuntimeReady, MetricLabels{Module: "chat", Runtime: "lifecycle-a"}, 1)
	requireLatestGauge(t, metrics, MetricRuntimeDegraded, MetricLabels{Module: "chat", Runtime: "lifecycle-a"}, 0)

	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	requireRuntimeStateOneHotGauge(t, metrics, "chat", "lifecycle-a", RuntimeStateClosing)
	requireRuntimeStateOneHotGauge(t, metrics, "chat", "lifecycle-a", RuntimeStateClosed)
	requireLatestGauge(t, metrics, MetricRuntimeReady, MetricLabels{Module: "chat", Runtime: "lifecycle-a"}, 0)
	requireLatestGauge(t, metrics, MetricRuntimeDegraded, MetricLabels{Module: "chat", Runtime: "lifecycle-a"}, 0)
}

func TestRuntimeMetricsStateGaugesTrackFailureAndDegradedTransitions(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "failed-a",
			Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	injected := errors.New("injected lifecycle failure")
	oldHook := runtimeStartAfterDurabilityHook
	runtimeStartAfterDurabilityHook = func(*Runtime) error { return injected }
	t.Cleanup(func() { runtimeStartAfterDurabilityHook = oldHook })

	if err := rt.Start(context.Background()); err == nil || !errors.Is(err, injected) {
		t.Fatalf("Start error = %v, want injected failure", err)
	}
	requireRuntimeStateOneHotGauge(t, metrics, "chat", "failed-a", RuntimeStateFailed)
	requireLatestGauge(t, metrics, MetricRuntimeReady, MetricLabels{Module: "chat", Runtime: "failed-a"}, 0)
	requireLatestGauge(t, metrics, MetricRuntimeDegraded, MetricLabels{Module: "chat", Runtime: "failed-a"}, 1)
	requireOnlyRuntimeErrorCounter(t, metrics, "chat", "failed-a", "start_failed")
}

func TestRuntimeMetricsReadyAndDegradedGaugesTrackRecoveryHealth(t *testing.T) {
	dir := t.TempDir()
	initial, err := Build(validChatModule(), Config{DataDir: dir})
	if err != nil {
		t.Fatalf("initial Build returned error: %v", err)
	}
	initial.state.SetCommittedTxID(1)
	if err := commitlog.NewSnapshotWriter(dir, initial.registry).CreateSnapshot(initial.state, 1); err != nil {
		t.Fatalf("create snapshot to corrupt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "1", "snapshot"), []byte("corrupt"), 0o644); err != nil {
		t.Fatalf("corrupt snapshot: %v", err)
	}

	metrics := &recordingMetricsRecorder{}
	rt, err := Build(validChatModule(), Config{
		DataDir: dir,
		Observability: ObservabilityConfig{
			RuntimeLabel: "degraded-a",
			Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	if err != nil {
		t.Fatalf("recovery Build returned error: %v", err)
	}
	requireLatestGauge(t, metrics, MetricRuntimeReady, MetricLabels{Module: "chat", Runtime: "degraded-a"}, 0)
	requireLatestGauge(t, metrics, MetricRuntimeDegraded, MetricLabels{Module: "chat", Runtime: "degraded-a"}, 1)

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	requireLatestGauge(t, metrics, MetricRuntimeReady, MetricLabels{Module: "chat", Runtime: "degraded-a"}, 1)
	requireLatestGauge(t, metrics, MetricRuntimeDegraded, MetricLabels{Module: "chat", Runtime: "degraded-a"}, 1)
}

func TestRuntimeMetricsFailureCountersUseMappedReasonsExactlyOnce(t *testing.T) {
	t.Run("build", func(t *testing.T) {
		metrics := &recordingMetricsRecorder{}
		_, err := Build(validChatModule(), Config{
			ExecutorQueueCapacity: -1,
			Observability: ObservabilityConfig{
				RuntimeLabel: "build-fail-a",
				Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
			},
		})
		if err == nil {
			t.Fatal("Build succeeded, want validation failure")
		}
		requireOnlyRuntimeErrorCounter(t, metrics, "chat", "build-fail-a", "build_failed")
	})

	t.Run("start", func(t *testing.T) {
		metrics := &recordingMetricsRecorder{}
		rt, err := Build(validChatModule(), Config{
			DataDir: t.TempDir(),
			Observability: ObservabilityConfig{
				RuntimeLabel: "start-fail-a",
				Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
			},
		})
		if err != nil {
			t.Fatalf("Build returned error: %v", err)
		}
		injected := errors.New("injected lifecycle failure")
		oldHook := runtimeStartAfterDurabilityHook
		runtimeStartAfterDurabilityHook = func(*Runtime) error { return injected }
		t.Cleanup(func() { runtimeStartAfterDurabilityHook = oldHook })

		if err := rt.Start(context.Background()); err == nil || !errors.Is(err, injected) {
			t.Fatalf("Start error = %v, want injected failure", err)
		}
		requireOnlyRuntimeErrorCounter(t, metrics, "chat", "start-fail-a", "start_failed")
	})

	t.Run("close", func(t *testing.T) {
		metrics := &recordingMetricsRecorder{}
		rt := &Runtime{
			observability: newRuntimeObservability("chat", ObservabilityConfig{
				RuntimeLabel: "close-fail-a",
				Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
			}),
			stateName: RuntimeStateClosed,
		}

		rt.recordCloseFailure(errors.New("close failed"), time.Millisecond)

		requireOnlyRuntimeErrorCounter(t, metrics, "chat", "close-fail-a", "close_failed")
	})
}

func TestRecoveryMetricsCoreFamiliesAndLabels(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	_, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "  normalized-runtime  ",
			Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	metrics.requireCounter(t, MetricRecoveryRunsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "normalized-runtime",
		Component: "commitlog",
		Result:    "success",
	}, 1)
	metrics.requireGauge(t, MetricRecoveryRecoveredTxID, MetricLabels{
		Module:    "chat",
		Runtime:   "normalized-runtime",
		Component: "commitlog",
	}, 0)
	metrics.requireGauge(t, MetricDurabilityDurableTxID, MetricLabels{
		Module:  "chat",
		Runtime: "normalized-runtime",
	}, 0)
	requireRuntimeStateOneHotGauge(t, metrics, "chat", "normalized-runtime", RuntimeStateBuilt)
}

func TestRecoverySkippedSnapshotMetricsMapReasonsOncePerReport(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "skip-a",
		Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
	})

	obs.recordRecoveryCompleted(commitlog.RecoveryReport{
		RecoveredTxID: 7,
		ReplayedTxRange: commitlog.RecoveryTxIDRange{
			Start: 5,
			End:   7,
		},
		ReplayDuration: 2 * time.Millisecond,
		SkippedSnapshots: []commitlog.SkippedSnapshotReport{
			{Reason: commitlog.SnapshotSkipPastDurableHorizon},
			{Reason: commitlog.SnapshotSkipReadFailed},
			{Reason: commitlog.SnapshotSkipReason("future_reason")},
		},
	}, time.Millisecond)

	metrics.requireCounter(t, MetricRecoverySkippedSnapshotsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "skip-a",
		Component: "commitlog",
		Reason:    "newer_than_log",
	}, 1)
	metrics.requireCounter(t, MetricRecoverySkippedSnapshotsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "skip-a",
		Component: "commitlog",
		Reason:    "read_failed",
	}, 1)
	metrics.requireCounter(t, MetricRecoverySkippedSnapshotsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "skip-a",
		Component: "commitlog",
		Reason:    "unknown",
	}, 1)
	if got := countMetricObservations(metrics, "counter", MetricRecoverySkippedSnapshotsTotal); got != 3 {
		t.Fatalf("skipped snapshot counter observations = %d, want 3", got)
	}
	requireHistogram(t, metrics, MetricRecoveryReplayDurationSeconds, MetricLabels{
		Module:  "chat",
		Runtime: "skip-a",
		Result:  "ok",
	})
}

func TestMetricsDisabledWithRecorderRecordsNothingThroughLifecycle(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "disabled-a",
			Metrics: MetricsConfig{
				Enabled:  false,
				Recorder: metrics,
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if got := metrics.snapshot(); len(got) != 0 {
		t.Fatalf("disabled metrics recorded observations: %+v", got)
	}
}

func TestMetricRecorderPanicRecoveredWithoutRecursiveSinkFailureMetric(t *testing.T) {
	logs := &recordingLogState{}
	metrics := &panicRecordingMetricsRecorder{}
	rt, err := Build(validChatModule().Reducer("send_message", noopReducer), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "panic-a",
			Logger:       logs.logger(),
			Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error with panicking metrics recorder: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error with panicking metrics recorder: %v", err)
	}
	if _, err := rt.CallReducer(context.Background(), "send_message", nil); err != nil {
		t.Fatalf("CallReducer returned error with panicking metrics recorder: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error with panicking metrics recorder: %v", err)
	}

	for _, observation := range metrics.snapshot() {
		if observation.name == MetricRuntimeErrorsTotal && observation.labels.Reason == "observability_sink_failed" {
			t.Fatalf("metrics panic recursively attempted observability sink failure counter: %+v", observation)
		}
	}
	requireRecordedLog(t, logs, "observability.sink_failed")
}

func requireRuntimeStateOneHotGauge(t *testing.T, metrics *recordingMetricsRecorder, module, runtime string, want RuntimeState) {
	t.Helper()
	filtered := make([]metricObservation, 0)
	for _, observation := range metrics.snapshot() {
		if observation.kind == "gauge" &&
			observation.name == MetricRuntimeState &&
			observation.labels.Module == module &&
			observation.labels.Runtime == runtime {
			filtered = append(filtered, observation)
		}
	}
	for start := 0; start+len(allRuntimeMetricStates) <= len(filtered); start++ {
		window := filtered[start : start+len(allRuntimeMetricStates)]
		values := make(map[RuntimeState]float64, len(allRuntimeMetricStates))
		valid := true
		for _, observation := range window {
			state := RuntimeState(observation.labels.State)
			if !knownRuntimeMetricState(state) {
				valid = false
				break
			}
			if _, ok := values[state]; ok {
				valid = false
				break
			}
			values[state] = observation.value
		}
		if !valid || len(values) != len(allRuntimeMetricStates) {
			continue
		}
		for _, state := range allRuntimeMetricStates {
			wantValue := 0.0
			if state == want {
				wantValue = 1
			}
			if values[state] != wantValue {
				valid = false
				break
			}
		}
		if valid {
			return
		}
	}
	t.Fatalf("missing one-hot runtime_state snapshot for state %q module=%q runtime=%q in %+v",
		want, module, runtime, filtered)
}

func requireLatestGauge(t *testing.T, metrics *recordingMetricsRecorder, name MetricName, labels MetricLabels, value float64) {
	t.Helper()
	var (
		got metricObservation
		ok  bool
	)
	for _, observation := range metrics.snapshot() {
		if observation.kind == "gauge" && observation.name == name && observation.labels == labels {
			got = observation
			ok = true
		}
	}
	if !ok {
		t.Fatalf("missing gauge %s labels=%+v in %+v", name, labels, metrics.snapshot())
	}
	if got.value != value {
		t.Fatalf("latest gauge %s labels=%+v value=%f, want %f", name, labels, got.value, value)
	}
}

func requireOnlyRuntimeErrorCounter(t *testing.T, metrics *recordingMetricsRecorder, module, runtime, reason string) {
	t.Helper()
	var matched bool
	count := 0
	for _, observation := range metrics.snapshot() {
		if observation.kind != "counter" || observation.name != MetricRuntimeErrorsTotal {
			continue
		}
		count++
		if observation.labels == (MetricLabels{
			Module:    module,
			Runtime:   runtime,
			Component: "runtime",
			Reason:    reason,
		}) && observation.delta == 1 {
			matched = true
		}
	}
	if count != 1 || !matched {
		t.Fatalf("runtime error counters count=%d matched=%v want one %s for module=%q runtime=%q in %+v",
			count, matched, reason, module, runtime, metrics.snapshot())
	}
}

func countMetricObservations(metrics *recordingMetricsRecorder, kind string, name MetricName) int {
	count := 0
	for _, observation := range metrics.snapshot() {
		if observation.kind == kind && observation.name == name {
			count++
		}
	}
	return count
}

var allRuntimeMetricStates = []RuntimeState{
	RuntimeStateBuilt,
	RuntimeStateStarting,
	RuntimeStateReady,
	RuntimeStateClosing,
	RuntimeStateClosed,
	RuntimeStateFailed,
}

func knownRuntimeMetricState(state RuntimeState) bool {
	for _, known := range allRuntimeMetricStates {
		if state == known {
			return true
		}
	}
	return false
}

type panicRecordingMetricsRecorder struct {
	mu           sync.Mutex
	observations []metricObservation
}

func (r *panicRecordingMetricsRecorder) AddCounter(name MetricName, labels MetricLabels, delta uint64) {
	r.mu.Lock()
	r.observations = append(r.observations, metricObservation{kind: "counter", name: name, labels: labels, delta: delta})
	r.mu.Unlock()
	panic("metrics failed")
}

func (r *panicRecordingMetricsRecorder) SetGauge(name MetricName, labels MetricLabels, value float64) {
	r.mu.Lock()
	r.observations = append(r.observations, metricObservation{kind: "gauge", name: name, labels: labels, value: value})
	r.mu.Unlock()
	panic("metrics failed")
}

func (r *panicRecordingMetricsRecorder) ObserveHistogram(name MetricName, labels MetricLabels, value float64) {
	r.mu.Lock()
	r.observations = append(r.observations, metricObservation{kind: "histogram", name: name, labels: labels, value: value})
	r.mu.Unlock()
	panic("metrics failed")
}

func (r *panicRecordingMetricsRecorder) snapshot() []metricObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.observations)
}
