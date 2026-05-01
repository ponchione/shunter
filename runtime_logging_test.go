package shunter

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/schema"
)

func TestRuntimeStructuredLoggingReadyAndClosed(t *testing.T) {
	logs := &recordingLogState{}
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "logging-a",
			Logger:       logs.logger(),
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	ready := requireRecordedLog(t, logs, "runtime.ready")
	if ready.level != slog.LevelInfo {
		t.Fatalf("runtime.ready level = %v, want info", ready.level)
	}
	assertLogAttr(t, ready, "component", "runtime")
	assertLogAttr(t, ready, "event", "runtime.ready")
	assertLogAttr(t, ready, "module", "chat")
	assertLogAttr(t, ready, "runtime", "logging-a")
	assertLogAttr(t, ready, "state", string(RuntimeStateReady))
	assertLogAttr(t, ready, "ready", true)
	assertLogAttr(t, ready, "degraded", false)
	assertLogDurationMS(t, ready)

	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	closed := requireRecordedLog(t, logs, "runtime.closed")
	if closed.level != slog.LevelInfo {
		t.Fatalf("runtime.closed level = %v, want info", closed.level)
	}
	assertLogAttr(t, closed, "component", "runtime")
	assertLogAttr(t, closed, "event", "runtime.closed")
	assertLogAttr(t, closed, "module", "chat")
	assertLogAttr(t, closed, "runtime", "logging-a")
	assertLogAttr(t, closed, "state", string(RuntimeStateClosed))
	assertLogDurationMS(t, closed)
}

func TestRuntimeStructuredLoggingStartFailedRedactsAndBoundsError(t *testing.T) {
	logs := &recordingLogState{}
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "logging-fail-a",
			Logger:       logs.logger(),
			Redaction:    RedactionConfig{ErrorMessageMaxBytes: 64},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	prevHook := runtimeStartAfterDurabilityHook
	runtimeStartAfterDurabilityHook = func(*Runtime) error {
		return errors.New("authorization=Bearer secret row=hidden " + strings.Repeat("x", 200))
	}
	t.Cleanup(func() { runtimeStartAfterDurabilityHook = prevHook })

	if err := rt.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded, want injected failure")
	}

	record := requireRecordedLog(t, logs, "runtime.start_failed")
	if record.level != slog.LevelError {
		t.Fatalf("runtime.start_failed level = %v, want error", record.level)
	}
	assertLogAttr(t, record, "component", "runtime")
	assertLogAttr(t, record, "event", "runtime.start_failed")
	assertLogAttr(t, record, "module", "chat")
	assertLogAttr(t, record, "runtime", "logging-fail-a")
	assertLogRedactedAndBounded(t, record, 64)
	assertLogDurationMS(t, record)

	degraded := requireRecordedLog(t, logs, "runtime.health_degraded")
	if degraded.level != slog.LevelWarn {
		t.Fatalf("runtime.health_degraded level = %v, want warn", degraded.level)
	}
	assertLogAttr(t, degraded, "reason", string(runtimeDegradedReasonRuntimeFailed))
}

func TestRuntimeStructuredLoggingCloseFailed(t *testing.T) {
	logs := &recordingLogState{}
	rt := &Runtime{
		observability: newRuntimeObservability("chat", ObservabilityConfig{
			RuntimeLabel: "logging-close-a",
			Logger:       logs.logger(),
			Redaction:    RedactionConfig{ErrorMessageMaxBytes: 48},
		}),
		stateName: RuntimeStateClosed,
	}

	rt.recordCloseFailure(errors.New("sql=select * from users where token='secret'; detail "+strings.Repeat("x", 200)), 2*time.Second)

	record := requireRecordedLog(t, logs, "runtime.close_failed")
	if record.level != slog.LevelError {
		t.Fatalf("runtime.close_failed level = %v, want error", record.level)
	}
	assertLogAttr(t, record, "component", "runtime")
	assertLogAttr(t, record, "event", "runtime.close_failed")
	assertLogAttr(t, record, "module", "chat")
	assertLogAttr(t, record, "runtime", "logging-close-a")
	assertLogRedactedAndBounded(t, record, 48)
	assertLogAttr(t, record, "duration_ms", int64(2000))
}

func TestRuntimeStructuredLoggingHealthDegradedUsesPrimaryReason(t *testing.T) {
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

	logs := &recordingLogState{}
	rt, err := Build(validChatModule(), Config{
		DataDir: dir,
		Observability: ObservabilityConfig{
			RuntimeLabel: "logging-degraded-a",
			Logger:       logs.logger(),
		},
	})
	if err != nil {
		t.Fatalf("recovery Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	ready := requireRecordedLog(t, logs, "runtime.ready")
	assertLogAttr(t, ready, "degraded", true)
	degraded := requireRecordedLog(t, logs, "runtime.health_degraded")
	if degraded.level != slog.LevelWarn {
		t.Fatalf("runtime.health_degraded level = %v, want warn", degraded.level)
	}
	assertLogAttr(t, degraded, "component", "runtime")
	assertLogAttr(t, degraded, "event", "runtime.health_degraded")
	assertLogAttr(t, degraded, "state", string(RuntimeStateReady))
	assertLogAttr(t, degraded, "reason", string(runtimeDegradedReasonRecoverySkipped))
}

func TestRuntimeStructuredLoggingLoggerPanicIsolation(t *testing.T) {
	rt, err := Build(validChatModule().Reducer("send_message", noopReducer), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Logger: slog.New(panicSlogHandler{}),
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error with panicking logger: %v", err)
	}
	if _, err := rt.CallReducer(context.Background(), "send_message", nil); err != nil {
		t.Fatalf("CallReducer returned error after logger panic: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error with panicking logger: %v", err)
	}
}

func TestRuntimeStructuredLoggingReducerPanic(t *testing.T) {
	logs := &recordingLogState{}
	rt, err := Build(validChatModule().Reducer("explode", func(*schema.ReducerContext, []byte) ([]byte, error) {
		panic("token=secret")
	}), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "logging-reducer-a",
			Logger:       logs.logger(),
			Redaction:    RedactionConfig{ErrorMessageMaxBytes: 64},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	res, err := rt.CallReducer(context.Background(), "explode", nil)
	if err != nil {
		t.Fatalf("CallReducer returned error: %v", err)
	}
	if res.Status != StatusFailedPanic {
		t.Fatalf("reducer status = %v, want panic", res.Status)
	}

	record := requireRecordedLog(t, logs, "executor.reducer_panic")
	if record.level != slog.LevelError {
		t.Fatalf("executor.reducer_panic level = %v, want error", record.level)
	}
	assertLogAttr(t, record, "component", "executor")
	assertLogAttr(t, record, "event", "executor.reducer_panic")
	assertLogAttr(t, record, "module", "chat")
	assertLogAttr(t, record, "runtime", "logging-reducer-a")
	assertLogAttr(t, record, "reducer", "explode")
	assertLogRedactedAndBounded(t, record, 64)
	if stack, ok := record.attrs["stack"].(string); !ok || stack == "" {
		t.Fatalf("executor.reducer_panic stack attr = %#v, want non-empty debug stack", record.attrs["stack"])
	}
}

func TestRuntimeStructuredLoggingProtocolAndSubscriptionEventsRedact(t *testing.T) {
	logs := &recordingLogState{}
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "logging-subsystem-a",
		Logger:       logs.logger(),
		Redaction:    RedactionConfig{ErrorMessageMaxBytes: 72},
	})

	obs.LogProtocolConnectionRejected("rejected_executor", errors.New("authorization=Bearer secret row=hidden "+strings.Repeat("x", 200)))
	obs.LogProtocolAuthFailed("missing_token", errors.New("token=secret signing_key=hidden"))
	obs.LogSubscriptionEvalError(7, errors.New("sql=select * from users where token='secret'; detail "+strings.Repeat("x", 200)))
	obs.LogProtocolBackpressure("outbound", "buffer_full")
	obs.LogSubscriptionClientDropped("buffer_full", nil)

	rejected := requireRecordedLog(t, logs, "protocol.connection_rejected")
	if rejected.level != slog.LevelWarn {
		t.Fatalf("protocol.connection_rejected level = %v, want warn", rejected.level)
	}
	assertLogAttr(t, rejected, "component", "protocol")
	assertLogAttr(t, rejected, "event", "protocol.connection_rejected")
	assertLogAttr(t, rejected, "module", "chat")
	assertLogAttr(t, rejected, "runtime", "logging-subsystem-a")
	assertLogAttr(t, rejected, "result", "rejected_executor")
	assertLogRedactedAndBounded(t, rejected, 72)

	authFailed := requireRecordedLog(t, logs, "protocol.auth_failed")
	if authFailed.level != slog.LevelWarn {
		t.Fatalf("protocol.auth_failed level = %v, want warn", authFailed.level)
	}
	assertLogAttr(t, authFailed, "component", "protocol")
	assertLogAttr(t, authFailed, "reason", "missing_token")
	assertLogRedactedAndBounded(t, authFailed, 72)

	evalError := requireRecordedLog(t, logs, "subscription.eval_error")
	if evalError.level != slog.LevelWarn {
		t.Fatalf("subscription.eval_error level = %v, want warn", evalError.level)
	}
	assertLogAttr(t, evalError, "component", "subscription")
	assertLogAttr(t, evalError, "tx_id", uint64(7))
	assertLogRedactedAndBounded(t, evalError, 72)

	backpressure := requireRecordedLog(t, logs, "protocol.backpressure")
	if backpressure.level != slog.LevelWarn {
		t.Fatalf("protocol.backpressure level = %v, want warn", backpressure.level)
	}
	assertLogAttr(t, backpressure, "component", "protocol")
	assertLogAttr(t, backpressure, "direction", "outbound")
	assertLogAttr(t, backpressure, "reason", "buffer_full")

	dropped := requireRecordedLog(t, logs, "subscription.client_dropped")
	if dropped.level != slog.LevelWarn {
		t.Fatalf("subscription.client_dropped level = %v, want warn", dropped.level)
	}
	assertLogAttr(t, dropped, "component", "subscription")
	assertLogAttr(t, dropped, "reason", "buffer_full")
}

func assertLogDurationMS(t *testing.T, record recordedLog) {
	t.Helper()
	got, ok := record.attrs["duration_ms"].(int64)
	if !ok {
		t.Fatalf("log %s duration_ms = %#v, want int64", record.message, record.attrs["duration_ms"])
	}
	if got < 0 {
		t.Fatalf("log %s duration_ms = %d, want >= 0", record.message, got)
	}
}

func assertLogRedactedAndBounded(t *testing.T, record recordedLog, maxBytes int) {
	t.Helper()
	got, ok := record.attrs["error"].(string)
	if !ok || got == "" {
		t.Fatalf("log %s error = %#v, want non-empty string", record.message, record.attrs["error"])
	}
	if len(got) > maxBytes {
		t.Fatalf("log %s error length = %d, want <= %d: %q", record.message, len(got), maxBytes, got)
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "hidden") {
		t.Fatalf("log %s error leaked sensitive text: %q", record.message, got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("log %s error = %q, want redaction marker", record.message, got)
	}
}
