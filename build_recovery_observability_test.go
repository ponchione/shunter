package shunter

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ponchione/shunter/commitlog"
)

func TestBuildFailureObservabilityLabels(t *testing.T) {
	tests := []struct {
		name        string
		mod         *Module
		cfg         Config
		wantModule  string
		wantRuntime string
	}{
		{
			name:        "pre_module_validation",
			mod:         nil,
			wantModule:  "unknown",
			wantRuntime: "runtime-a",
		},
		{
			name: "invalid_runtime_label_uses_default_runtime",
			mod:  NewModule("chat"),
			cfg: Config{
				Observability: ObservabilityConfig{RuntimeLabel: "bad\nlabel"},
			},
			wantModule:  "chat",
			wantRuntime: "default",
		},
		{
			name: "post_module_validation_uses_validated_module",
			mod:  NewModule("chat"),
			cfg: Config{
				ExecutorQueueCapacity: -1,
			},
			wantModule:  "chat",
			wantRuntime: "runtime-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs, metrics, obs := newRecordingObservability(t)
			if tt.cfg.Observability.RuntimeLabel == "" {
				tt.cfg.Observability.RuntimeLabel = "runtime-a"
			}
			tt.cfg.Observability.Logger = logs.logger()
			tt.cfg.Observability.Metrics = MetricsConfig{Enabled: true, Recorder: metrics}

			_, err := Build(tt.mod, tt.cfg)
			if err == nil {
				t.Fatal("Build succeeded, want validation failure")
			}

			record := obs.requireLog(t, "runtime.build_failed")
			if record.level != slog.LevelError {
				t.Fatalf("runtime.build_failed level = %v, want error", record.level)
			}
			assertLogAttr(t, record, "component", "runtime")
			assertLogAttr(t, record, "module", tt.wantModule)
			assertLogAttr(t, record, "runtime", tt.wantRuntime)
			if got, ok := record.attrs["error"].(string); !ok || got == "" {
				t.Fatalf("runtime.build_failed error attr = %#v, want non-empty string", record.attrs["error"])
			}
			metrics.requireCounter(t, MetricRuntimeErrorsTotal, MetricLabels{
				Module:    tt.wantModule,
				Runtime:   tt.wantRuntime,
				Component: "runtime",
				Reason:    "build_failed",
			}, 1)
		})
	}
}

func TestBuildFreshBootstrapRecordsRecoveryObservability(t *testing.T) {
	logs, metrics, obs := newRecordingObservability(t)
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "fresh-a",
			Logger:       logs.logger(),
			Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if !rt.recovery.ran || !rt.recovery.succeeded {
		t.Fatalf("recovery facts = %+v, want ran successful", rt.recovery)
	}
	report := rt.recovery.report
	if report.RecoveredTxID != 0 || report.HasSelectedSnapshot || report.HasDurableLog ||
		len(report.DamagedTailSegments) != 0 || len(report.SkippedSnapshots) != 0 {
		t.Fatalf("fresh bootstrap report = %+v, want tx 0 without selected snapshot/damage/skips", report)
	}
	if rt.recoveredTxID != 0 {
		t.Fatalf("runtime recoveredTxID = %d, want 0", rt.recoveredTxID)
	}

	record := obs.requireLog(t, "recovery.completed")
	if record.level != slog.LevelInfo {
		t.Fatalf("recovery.completed level = %v, want info", record.level)
	}
	assertLogAttr(t, record, "component", "commitlog")
	assertLogAttr(t, record, "module", "chat")
	assertLogAttr(t, record, "runtime", "fresh-a")
	assertLogAttr(t, record, "tx_id", uint64(0))
	assertLogAttr(t, record, "damaged_tail_segments", int64(0))
	assertLogAttr(t, record, "skipped_snapshots", int64(0))
	metrics.requireCounter(t, MetricRecoveryRunsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "fresh-a",
		Component: "commitlog",
		Result:    "success",
	}, 1)
	metrics.requireGauge(t, MetricRecoveryRecoveredTxID, MetricLabels{
		Module:    "chat",
		Runtime:   "fresh-a",
		Component: "commitlog",
	}, 0)
	metrics.requireGauge(t, MetricRecoveryDamagedTailSegments, MetricLabels{
		Module:    "chat",
		Runtime:   "fresh-a",
		Component: "commitlog",
	}, 0)
	metrics.requireNoMetric(t, MetricRecoverySkippedSnapshotsTotal)
}

func TestBuildExistingRecoveryRecordsReportAndMetrics(t *testing.T) {
	dir := t.TempDir()
	if _, err := Build(validChatModule(), Config{DataDir: dir}); err != nil {
		t.Fatalf("initial Build returned error: %v", err)
	}

	logs, metrics, obs := newRecordingObservability(t)
	rt, err := Build(validChatModule(), Config{
		DataDir: dir,
		Observability: ObservabilityConfig{
			RuntimeLabel: "recover-a",
			Logger:       logs.logger(),
			Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	if err != nil {
		t.Fatalf("recovery Build returned error: %v", err)
	}
	if !rt.recovery.ran || !rt.recovery.succeeded {
		t.Fatalf("recovery facts = %+v, want ran successful", rt.recovery)
	}
	if !rt.recovery.report.HasSelectedSnapshot || rt.recovery.report.SelectedSnapshotTxID != 0 {
		t.Fatalf("recovery report selected snapshot = (%v, %d), want bootstrap snapshot",
			rt.recovery.report.HasSelectedSnapshot, rt.recovery.report.SelectedSnapshotTxID)
	}

	record := obs.requireLog(t, "recovery.completed")
	if record.level != slog.LevelInfo {
		t.Fatalf("recovery.completed level = %v, want info", record.level)
	}
	metrics.requireCounter(t, MetricRecoveryRunsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "recover-a",
		Component: "commitlog",
		Result:    "success",
	}, 1)
	metrics.requireGauge(t, MetricRecoveryRecoveredTxID, MetricLabels{
		Module:    "chat",
		Runtime:   "recover-a",
		Component: "commitlog",
	}, 0)
}

func TestBuildRecoveryFailureRecordsRecoveryAndBuildObservability(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "not-a-segment.log"), []byte("bad"), 0o644); err != nil {
		t.Fatalf("write malformed segment: %v", err)
	}
	logs, metrics, obs := newRecordingObservability(t)

	_, err := Build(validChatModule(), Config{
		DataDir: dir,
		Observability: ObservabilityConfig{
			RuntimeLabel: "recover-fail-a",
			Logger:       logs.logger(),
			Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	if err == nil {
		t.Fatal("Build succeeded with malformed segment")
	}

	failed := obs.requireLog(t, "recovery.failed")
	if failed.level != slog.LevelError {
		t.Fatalf("recovery.failed level = %v, want error", failed.level)
	}
	assertLogAttr(t, failed, "component", "commitlog")
	assertLogAttr(t, failed, "module", "chat")
	assertLogAttr(t, failed, "runtime", "recover-fail-a")
	buildFailed := obs.requireLog(t, "runtime.build_failed")
	if buildFailed.level != slog.LevelError {
		t.Fatalf("runtime.build_failed level = %v, want error", buildFailed.level)
	}
	metrics.requireCounter(t, MetricRecoveryRunsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "recover-fail-a",
		Component: "commitlog",
		Result:    "failed",
	}, 1)
	metrics.requireCounter(t, MetricRuntimeErrorsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "recover-fail-a",
		Component: "runtime",
		Reason:    "build_failed",
	}, 1)
}

func TestBuildRecoverySkippedSnapshotMarksDegradedFactsAndWarns(t *testing.T) {
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

	logs, metrics, obs := newRecordingObservability(t)
	rt, err := Build(validChatModule(), Config{
		DataDir: dir,
		Observability: ObservabilityConfig{
			RuntimeLabel: "recover-warn-a",
			Logger:       logs.logger(),
			Metrics:      MetricsConfig{Enabled: true, Recorder: metrics},
		},
	})
	if err != nil {
		t.Fatalf("recovery Build returned error: %v", err)
	}
	if !rt.recovery.degraded() {
		t.Fatalf("recovery facts = %+v, want degraded marker", rt.recovery)
	}
	if len(rt.recovery.report.SkippedSnapshots) != 1 {
		t.Fatalf("skipped snapshots = %+v, want one", rt.recovery.report.SkippedSnapshots)
	}

	record := obs.requireLog(t, "recovery.completed")
	if record.level != slog.LevelWarn {
		t.Fatalf("recovery.completed level = %v, want warn", record.level)
	}
	assertLogAttr(t, record, "skipped_snapshots", int64(1))
	metrics.requireCounter(t, MetricRecoverySkippedSnapshotsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "recover-warn-a",
		Component: "commitlog",
		Reason:    "read_failed",
	}, 1)
}

type recordingObservationBundle struct {
	logs *recordingLogState
}

func newRecordingObservability(t *testing.T) (*recordingLogState, *recordingMetricsRecorder, *recordingObservationBundle) {
	t.Helper()
	logs := &recordingLogState{}
	metrics := &recordingMetricsRecorder{}
	return logs, metrics, &recordingObservationBundle{logs: logs}
}

func (b *recordingObservationBundle) requireLog(t *testing.T, event string) recordedLog {
	t.Helper()
	records := b.logs.records()
	for _, record := range records {
		if record.message == event {
			return record
		}
	}
	t.Fatalf("missing log event %q in %+v", event, records)
	return recordedLog{}
}

func assertLogAttr(t *testing.T, record recordedLog, key string, want any) {
	t.Helper()
	got, ok := record.attrs[key]
	if !ok {
		t.Fatalf("log %s missing attr %q in %+v", record.message, key, record.attrs)
	}
	if got != want {
		t.Fatalf("log %s attr %q = %#v, want %#v", record.message, key, got, want)
	}
}

type recordedLog struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

type recordingLogState struct {
	mu      sync.Mutex
	entries []recordedLog
}

func (s *recordingLogState) logger() *slog.Logger {
	return slog.New(recordingSlogHandler{state: s})
}

func (s *recordingLogState) records() []recordedLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]recordedLog, len(s.entries))
	copy(out, s.entries)
	return out
}

type recordingSlogHandler struct {
	state *recordingLogState
	attrs []slog.Attr
}

func (h recordingSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h recordingSlogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any, record.NumAttrs()+len(h.attrs))
	for _, attr := range h.attrs {
		attrs[attr.Key] = attr.Value.Resolve().Any()
	}
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Resolve().Any()
		return true
	})
	h.state.mu.Lock()
	h.state.entries = append(h.state.entries, recordedLog{
		level:   record.Level,
		message: record.Message,
		attrs:   attrs,
	})
	h.state.mu.Unlock()
	return nil
}

func (h recordingSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := recordingSlogHandler{state: h.state, attrs: append([]slog.Attr(nil), h.attrs...)}
	out.attrs = append(out.attrs, attrs...)
	return out
}

func (h recordingSlogHandler) WithGroup(string) slog.Handler { return h }

type metricObservation struct {
	kind   string
	name   MetricName
	labels MetricLabels
	delta  uint64
	value  float64
}

type recordingMetricsRecorder struct {
	mu           sync.Mutex
	observations []metricObservation
}

func (r *recordingMetricsRecorder) AddCounter(name MetricName, labels MetricLabels, delta uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations = append(r.observations, metricObservation{kind: "counter", name: name, labels: labels, delta: delta})
}

func (r *recordingMetricsRecorder) SetGauge(name MetricName, labels MetricLabels, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations = append(r.observations, metricObservation{kind: "gauge", name: name, labels: labels, value: value})
}

func (r *recordingMetricsRecorder) ObserveHistogram(name MetricName, labels MetricLabels, value float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations = append(r.observations, metricObservation{kind: "histogram", name: name, labels: labels, value: value})
}

func (r *recordingMetricsRecorder) snapshot() []metricObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]metricObservation, len(r.observations))
	copy(out, r.observations)
	return out
}

func (r *recordingMetricsRecorder) requireCounter(t *testing.T, name MetricName, labels MetricLabels, delta uint64) {
	t.Helper()
	for _, observation := range r.snapshot() {
		if observation.kind == "counter" && observation.name == name && observation.labels == labels && observation.delta == delta {
			return
		}
	}
	t.Fatalf("missing counter %s labels=%+v delta=%d in %+v", name, labels, delta, r.snapshot())
}

func (r *recordingMetricsRecorder) requireGauge(t *testing.T, name MetricName, labels MetricLabels, value float64) {
	t.Helper()
	for _, observation := range r.snapshot() {
		if observation.kind == "gauge" && observation.name == name && observation.labels == labels && observation.value == value {
			return
		}
	}
	t.Fatalf("missing gauge %s labels=%+v value=%f in %+v", name, labels, value, r.snapshot())
}

func (r *recordingMetricsRecorder) requireNoMetric(t *testing.T, name MetricName) {
	t.Helper()
	for _, observation := range r.snapshot() {
		if observation.name == name {
			t.Fatalf("unexpected metric %s in %+v", name, r.snapshot())
		}
	}
}
