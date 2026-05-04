package shunter

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestZeroObservabilityConfigBuildStartCallCloseNoop(t *testing.T) {
	rt, err := Build(validChatModule().Reducer("send_message", noopReducer), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if rt.observability == nil {
		t.Fatal("runtime observability is nil")
	}
	if rt.observability.logger != nil {
		t.Fatal("zero observability configured a logger")
	}
	if rt.observability.metrics != nil {
		t.Fatal("zero observability configured metrics")
	}
	if rt.observability.tracer != nil {
		t.Fatal("zero observability configured tracing")
	}

	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if _, err := rt.CallReducer(context.Background(), "send_message", nil); err != nil {
		t.Fatalf("CallReducer returned error: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestObservabilityRuntimeLabelNormalizationAndValidation(t *testing.T) {
	rt, err := Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			RuntimeLabel: "  runtime-a  ",
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if got := rt.buildConfig.Observability.RuntimeLabel; got != "runtime-a" {
		t.Fatalf("normalized runtime label = %q, want runtime-a", got)
	}
	if got := rt.observability.runtimeLabel; got != "runtime-a" {
		t.Fatalf("observability runtime label = %q, want runtime-a", got)
	}

	rt, err = Build(validChatModule(), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build with default observability returned error: %v", err)
	}
	if got := rt.buildConfig.Observability.RuntimeLabel; got != "default" {
		t.Fatalf("default runtime label = %q, want default", got)
	}

	tests := []struct {
		name  string
		label string
	}{
		{name: "control", label: "bad\nlabel"},
		{name: "del", label: "bad" + string(rune(0x7f)) + "label"},
		{name: "too_long", label: strings.Repeat("a", 129)},
		{name: "invalid_utf8", label: string([]byte{'o', 'k', 0xff})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(NewModule("chat"), Config{
				Observability: ObservabilityConfig{RuntimeLabel: tt.label},
			})
			if err == nil {
				t.Fatal("Build succeeded with invalid runtime label")
			}
			assertErrorMentions(t, err, "runtime label")
			assertNotSchemaValidationError(t, err)
		})
	}
}

func TestObservabilityReducerLabelModeNormalizationAndValidation(t *testing.T) {
	rt, err := Build(validChatModule(), Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if got := rt.buildConfig.Observability.Metrics.ReducerLabelMode; got != ReducerLabelModeName {
		t.Fatalf("default reducer label mode = %q, want %q", got, ReducerLabelModeName)
	}

	rt, err = Build(validChatModule(), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Metrics: MetricsConfig{ReducerLabelMode: ReducerLabelModeAggregate},
		},
	})
	if err != nil {
		t.Fatalf("Build with aggregate reducer label mode returned error: %v", err)
	}
	if got := rt.buildConfig.Observability.Metrics.ReducerLabelMode; got != ReducerLabelModeAggregate {
		t.Fatalf("reducer label mode = %q, want %q", got, ReducerLabelModeAggregate)
	}

	_, err = Build(NewModule("chat"), Config{
		Observability: ObservabilityConfig{
			Metrics: MetricsConfig{ReducerLabelMode: ReducerLabelMode("per_request")},
		},
	})
	if err == nil {
		t.Fatal("Build succeeded with invalid reducer label mode")
	}
	assertErrorMentions(t, err, "reducer label")
	assertNotSchemaValidationError(t, err)
}

func FuzzObservabilityConfigNormalization(f *testing.F) {
	for _, seed := range []struct {
		label    string
		mode     string
		maxBytes int
	}{
		{label: "", mode: "", maxBytes: 0},
		{label: "  runtime-a  ", mode: string(ReducerLabelModeName), maxBytes: -1},
		{label: "runtime-b", mode: string(ReducerLabelModeAggregate), maxBytes: 1},
		{label: "bad\nlabel", mode: "", maxBytes: 1024},
		{label: strings.Repeat("a", 129), mode: "", maxBytes: 1024},
		{label: string([]byte{'o', 'k', 0xff}), mode: "", maxBytes: 1024},
		{label: "runtime-c", mode: "per_request", maxBytes: 1024},
	} {
		f.Add(seed.label, seed.mode, seed.maxBytes)
	}

	f.Fuzz(func(t *testing.T, label, mode string, maxBytes int) {
		if len(label) > 512 || len(mode) > 128 {
			return
		}
		cfg := ObservabilityConfig{
			RuntimeLabel: label,
			Redaction:    RedactionConfig{ErrorMessageMaxBytes: maxBytes},
			Metrics:      MetricsConfig{ReducerLabelMode: ReducerLabelMode(mode)},
		}

		normalized, err := normalizeObservabilityConfig(cfg)
		buildObs, buildErr := newBuildObservability("chat", cfg)
		if buildObs == nil {
			t.Fatal("newBuildObservability returned nil observability")
		}
		if (err == nil) != (buildErr == nil) {
			t.Fatalf("normalize error = %v, build observability error = %v", err, buildErr)
		}
		if err != nil {
			if !strings.Contains(err.Error(), "runtime label") && !strings.Contains(err.Error(), "reducer label") {
				t.Fatalf("normalization error = %v, want categorized observability error", err)
			}
			if !validObservabilityRuntimeLabel(buildObs.runtimeLabel) {
				t.Fatalf("build failure runtime label = %q, want valid fallback label", buildObs.runtimeLabel)
			}
			if maxBytes <= 0 && buildObs.redaction.ErrorMessageMaxBytes != defaultObservabilityErrorMessageMaxBytes {
				t.Fatalf("build failure max error bytes = %d, want default %d",
					buildObs.redaction.ErrorMessageMaxBytes, defaultObservabilityErrorMessageMaxBytes)
			}
			return
		}

		if !validObservabilityRuntimeLabel(normalized.RuntimeLabel) {
			t.Fatalf("accepted invalid runtime label %q", normalized.RuntimeLabel)
		}
		if buildObs.runtimeLabel != normalized.RuntimeLabel {
			t.Fatalf("build runtime label = %q, want normalized %q", buildObs.runtimeLabel, normalized.RuntimeLabel)
		}
		wantMaxBytes := maxBytes
		if wantMaxBytes <= 0 {
			wantMaxBytes = defaultObservabilityErrorMessageMaxBytes
		}
		if normalized.Redaction.ErrorMessageMaxBytes != wantMaxBytes {
			t.Fatalf("normalized max error bytes = %d, want %d", normalized.Redaction.ErrorMessageMaxBytes, wantMaxBytes)
		}
		if buildObs.redaction.ErrorMessageMaxBytes != wantMaxBytes {
			t.Fatalf("build max error bytes = %d, want %d", buildObs.redaction.ErrorMessageMaxBytes, wantMaxBytes)
		}
		switch normalized.Metrics.ReducerLabelMode {
		case ReducerLabelModeName, ReducerLabelModeAggregate:
		default:
			t.Fatalf("accepted invalid reducer label mode %q", normalized.Metrics.ReducerLabelMode)
		}
	})
}

func TestObservabilityRedactionExamples(t *testing.T) {
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "default",
		Redaction:    RedactionConfig{ErrorMessageMaxBytes: 1024},
	})

	tests := []struct {
		in   string
		want string
	}{
		{in: "authorization=Bearer abc.def", want: "authorization=[redacted]"},
		{in: "failed: Bearer abc.def", want: "failed: Bearer [redacted]"},
		{in: "failed: bearer abc.def", want: "failed: bearer [redacted]"},
		{in: "failed: BEARER abc.def", want: "failed: BEARER [redacted]"},
		{in: `{"token":"abc","row":{"id":1}}`, want: `{"token":"[redacted]","row":"[redacted]"}`},
		{in: "sql=select * from users where token='abc'; detail", want: "sql=[redacted]; detail"},
		{in: "signing_key: secret", want: "signing_key: [redacted]"},
	}
	for _, tt := range tests {
		if got := obs.redactErrorString(tt.in); got != tt.want {
			t.Fatalf("redactErrorString(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestObservabilityRedactionBoundaries(t *testing.T) {
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "default",
		Redaction:    RedactionConfig{ErrorMessageMaxBytes: 1024},
	})

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "token bounded", in: "mytoken=abc token=def", want: "mytoken=abc token=[redacted]"},
		{name: "case insensitive key", in: "TOKEN: abc", want: "TOKEN: [redacted]"},
		{name: "quoted text value", in: `args="one two" ok`, want: "args=[redacted] ok"},
		{name: "single quoted text value", in: "payload='one two' ok", want: "payload=[redacted] ok"},
		{name: "unquoted delimiter", in: "query=select one, keep", want: "query=[redacted], keep"},
		{name: "json string whitespace", in: `"access_token" : "abc"`, want: `"access_token" : "[redacted]"`},
		{name: "json array value", in: `"rows":[{"id":1}]`, want: `"rows":"[redacted]"`},
		{name: "bearer delimiter", in: `x Bearer abc.def,"next"`, want: `x Bearer [redacted],"next"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := obs.redactErrorString(tt.in); got != tt.want {
				t.Fatalf("redactErrorString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestObservabilityRedactionInvalidUTF8AndTruncation(t *testing.T) {
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "default",
		Redaction:    RedactionConfig{ErrorMessageMaxBytes: 4},
	})

	raw := string([]byte{'a', 0xff, 'b', 0xe2, 0x82, 0xac, 'c'})
	got := obs.redactErrorString(raw)
	if !utf8.ValidString(got) {
		t.Fatalf("redacted string is invalid UTF-8: %q", got)
	}
	if len(got) > 4 {
		t.Fatalf("redacted length = %d, want <= 4", len(got))
	}
	if got != "ab" {
		t.Fatalf("redacted invalid/truncated string = %q, want ab", got)
	}

	defaultObs := newRuntimeObservability("chat", ObservabilityConfig{RuntimeLabel: "default"})
	long := strings.Repeat("x", 1100)
	if got := defaultObs.redactErrorString(long); len(got) != 1024 {
		t.Fatalf("default redaction length = %d, want 1024", len(got))
	}
}

func FuzzObservabilityRedactErrorString(f *testing.F) {
	for _, seed := range []struct {
		raw      string
		maxBytes int
	}{
		{raw: "authorization=Bearer secret", maxBytes: 1024},
		{raw: `{"token":"secret","row":{"id":1}}`, maxBytes: 1024},
		{raw: "payload='secret' keep", maxBytes: 1024},
		{raw: "query=select * from users where token='secret'; keep", maxBytes: 1024},
		{raw: "failed: Bearer secret, keep", maxBytes: 1024},
		{raw: string([]byte{'a', 0xff, 'b', 0xe2, 0x82, 0xac, 'c'}), maxBytes: 4},
		{raw: strings.Repeat("x", 2048), maxBytes: 128},
	} {
		f.Add(seed.raw, seed.maxBytes)
	}

	f.Fuzz(func(t *testing.T, raw string, maxBytes int) {
		maxBytes = maxBytes % 2048
		if maxBytes < 1 {
			maxBytes = 1 - maxBytes
		}
		obs := newRuntimeObservability("chat", ObservabilityConfig{
			RuntimeLabel: "default",
			Redaction:    RedactionConfig{ErrorMessageMaxBytes: maxBytes},
		})

		got := obs.redactErrorString(raw)
		again := obs.redactErrorString(raw)
		if got != again {
			t.Fatalf("redaction is not deterministic:\nfirst=%q\nsecond=%q", got, again)
		}
		if !utf8.ValidString(got) {
			t.Fatalf("redacted string is invalid UTF-8: %q", got)
		}
		if len(got) > maxBytes {
			t.Fatalf("redacted length = %d, want <= %d", len(got), maxBytes)
		}
		if containsSeededSensitiveSecret(raw) && strings.Contains(got, "secret") {
			t.Fatalf("redacted output leaked seeded secret:\nraw=%q\nredacted=%q", raw, got)
		}
	})
}

func TestObservabilityDebugSQLBoundedWhenAllowed(t *testing.T) {
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "default",
		Redaction: RedactionConfig{
			ErrorMessageMaxBytes:   4,
			AllowRawSQLInDebugLogs: true,
		},
	})

	got, ok := obs.debugSQLString(string([]byte{'a', 'b', 0xff, 0xe2, 0x82, 0xac, 'c'}))
	if !ok {
		t.Fatal("debug SQL was not allowed")
	}
	if got != "ab" {
		t.Fatalf("debug SQL = %q, want ab", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("debug SQL is invalid UTF-8: %q", got)
	}

	disabled := newRuntimeObservability("chat", ObservabilityConfig{RuntimeLabel: "default"})
	if got, ok := disabled.debugSQLString("select 1"); ok || got != "" {
		t.Fatalf("disabled debug SQL = %q, %v; want empty false", got, ok)
	}
}

func TestObservabilityDisabledMetricsAndTracingIgnoreSinks(t *testing.T) {
	rec := &countingMetricsRecorder{}
	tracer := &countingTracer{}
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "default",
		Metrics: MetricsConfig{
			Enabled:  false,
			Recorder: rec,
		},
		Tracing: TracingConfig{
			Enabled: false,
			Tracer:  tracer,
		},
	})

	obs.addCounter(MetricRuntimeErrorsTotal, MetricLabels{Component: "runtime"}, 1)
	obs.setGauge(MetricRuntimeReady, MetricLabels{Component: "runtime"}, 1)
	obs.observeHistogram(MetricReducerDurationSeconds, MetricLabels{Component: "executor"}, 0.1)
	_, span := obs.startSpan(context.Background(), "shunter.runtime.start", "runtime")
	if span != nil {
		t.Fatal("disabled tracing returned a span")
	}
	if got := rec.calls.Load(); got != 0 {
		t.Fatalf("disabled metrics recorder calls = %d, want 0", got)
	}
	if got := tracer.starts.Load(); got != 0 {
		t.Fatalf("disabled tracer starts = %d, want 0", got)
	}
}

func TestRuntimeObservabilityRecordsStoreReadRows(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "store-reads-a",
		Metrics: MetricsConfig{
			Enabled:  true,
			Recorder: metrics,
		},
	})

	obs.RecordStoreReadRows("table_scan", 3)
	obs.RecordStoreReadRows("index_scan", 2)
	obs.RecordStoreReadRows("not-a-kind", 1)
	obs.RecordStoreReadRows("index_seek", 0)

	metrics.requireCounter(t, MetricStoreReadRowsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "store-reads-a",
		Component: "store",
		Kind:      "table_scan",
	}, 3)
	metrics.requireCounter(t, MetricStoreReadRowsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "store-reads-a",
		Component: "store",
		Kind:      "index_scan",
	}, 2)
	metrics.requireCounter(t, MetricStoreReadRowsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "store-reads-a",
		Component: "store",
		Kind:      "unknown",
	}, 1)
	for _, observation := range metrics.snapshot() {
		if observation.name == MetricStoreReadRowsTotal && observation.labels.Kind == "index_seek" {
			t.Fatalf("zero-row store read recorded a counter: %+v", observation)
		}
	}
}

func TestObservabilitySinkPanicsRecoveredBeforeRuntimeOperation(t *testing.T) {
	logger := slog.New(panicSlogHandler{})
	rec := panicMetricsRecorder{}
	tracer := panicTracer{}
	rt, err := Build(validChatModule().Reducer("send_message", noopReducer), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Logger: logger,
			Metrics: MetricsConfig{
				Enabled:  true,
				Recorder: rec,
			},
			Tracing: TracingConfig{
				Enabled: true,
				Tracer:  tracer,
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	defer rt.Close()

	rt.observability.log(context.Background(), slog.LevelInfo, "runtime.ready", "runtime")
	rt.observability.addCounter(MetricRuntimeErrorsTotal, MetricLabels{Component: "runtime"}, 1)
	_, span := rt.observability.startSpan(context.Background(), "shunter.runtime.start", "runtime")
	if span != nil {
		span.AddEvent("event")
		span.End(errors.New("token=secret"))
	}

	if _, err := rt.CallReducer(context.Background(), "send_message", nil); err != nil {
		t.Fatalf("CallReducer after sink panics returned error: %v", err)
	}
}

func TestObservabilitySinkFailureFallbacksAreBoundedAndNonRecursive(t *testing.T) {
	t.Run("metrics panic records warning log", func(t *testing.T) {
		logs := &recordingLogState{}
		obs := newRuntimeObservability("chat", ObservabilityConfig{
			RuntimeLabel: "runtime-a",
			Logger:       logs.logger(),
			Metrics: MetricsConfig{
				Enabled:  true,
				Recorder: secretPanicMetricsRecorder{},
			},
		})

		obs.addCounter(MetricRuntimeErrorsTotal, MetricLabels{Component: "runtime"}, 1)

		record := requireRecordedLog(t, logs, "observability.sink_failed")
		if record.level != slog.LevelWarn {
			t.Fatalf("sink failure level = %v, want warn", record.level)
		}
		assertLogAttr(t, record, "component", "observability")
		assertLogAttr(t, record, "module", "chat")
		assertLogAttr(t, record, "runtime", "runtime-a")
		assertLogAttr(t, record, "sink", "metrics")
		if got, ok := record.attrs["error"].(string); !ok || strings.Contains(got, "secret") || !strings.Contains(got, "[redacted]") {
			t.Fatalf("sink failure error attr = %#v, want redacted secret", record.attrs["error"])
		}
	})

	t.Run("logger panic records metric", func(t *testing.T) {
		metrics := &recordingMetricsRecorder{}
		obs := newRuntimeObservability("chat", ObservabilityConfig{
			RuntimeLabel: "runtime-b",
			Logger:       slog.New(secretPanicSlogHandler{}),
			Metrics: MetricsConfig{
				Enabled:  true,
				Recorder: metrics,
			},
		})

		obs.log(context.Background(), slog.LevelInfo, "runtime.ready", "runtime")

		metrics.requireCounter(t, MetricRuntimeErrorsTotal, MetricLabels{
			Module:    "chat",
			Runtime:   "runtime-b",
			Component: "observability",
			Reason:    "observability_sink_failed",
		}, 1)
	})

	t.Run("all sinks panic without escaping", func(t *testing.T) {
		obs := newRuntimeObservability("chat", ObservabilityConfig{
			RuntimeLabel: "runtime-c",
			Logger:       slog.New(secretPanicSlogHandler{}),
			Metrics: MetricsConfig{
				Enabled:  true,
				Recorder: secretPanicMetricsRecorder{},
			},
		})
		obs.log(context.Background(), slog.LevelInfo, "runtime.ready", "runtime")
		obs.addCounter(MetricRuntimeErrorsTotal, MetricLabels{Component: "runtime"}, 1)
	})
}

func TestRuntimeObservationMethodsRecoverPanickingSinks(t *testing.T) {
	observerErr := errors.New("authorization=Bearer secret args=raw row=hidden sql=select * from users where token='secret'")
	health := RuntimeHealth{
		State:    RuntimeStateReady,
		Ready:    true,
		Degraded: true,
		Durability: DurabilityHealth{
			DurableTxID: types.TxID(42),
		},
	}
	report := commitlog.RecoveryReport{
		RecoveredTxID: types.TxID(42),
		DamagedTailSegments: []commitlog.SegmentInfo{{
			Path:    "segment-a",
			StartTx: types.TxID(40),
			LastTx:  types.TxID(41),
		}},
		SkippedSnapshots: []commitlog.SkippedSnapshotReport{{
			TxID:   types.TxID(39),
			Reason: commitlog.SnapshotSkipPastDurableHorizon,
		}},
	}
	var connID types.ConnectionID
	connID[0] = 1

	methods := []struct {
		name string
		call func(*runtimeObservability)
	}{
		{name: "log", call: func(o *runtimeObservability) {
			o.log(context.Background(), slog.LevelInfo, "runtime.ready", "runtime", slog.String("state", string(RuntimeStateReady)))
		}},
		{name: "record_build_failed", call: func(o *runtimeObservability) {
			o.recordBuildFailed(observerErr)
		}},
		{name: "record_recovery_completed", call: func(o *runtimeObservability) {
			o.recordRecoveryCompleted(report, time.Millisecond)
		}},
		{name: "record_recovery_failed", call: func(o *runtimeObservability) {
			o.recordRecoveryFailed(observerErr, time.Millisecond)
		}},
		{name: "record_runtime_start_failed", call: func(o *runtimeObservability) {
			o.recordRuntimeStartFailed(context.Background(), observerErr, time.Millisecond)
		}},
		{name: "record_runtime_ready", call: func(o *runtimeObservability) {
			o.recordRuntimeReady(context.Background(), health, time.Millisecond)
		}},
		{name: "record_runtime_close_failed", call: func(o *runtimeObservability) {
			o.recordRuntimeCloseFailed(observerErr, time.Millisecond)
		}},
		{name: "record_runtime_closed", call: func(o *runtimeObservability) {
			o.recordRuntimeClosed(RuntimeStateClosed, time.Millisecond)
		}},
		{name: "record_runtime_health_degraded", call: func(o *runtimeObservability) {
			o.recordRuntimeHealthDegraded(health, runtimeDegradedReasonRecoverySkipped)
		}},
		{name: "record_runtime_health_metrics", call: func(o *runtimeObservability) {
			o.recordRuntimeHealthMetrics(health)
		}},
		{name: "record_runtime_error", call: func(o *runtimeObservability) {
			o.recordRuntimeError("panic")
		}},
		{name: "log_durability_failed", call: func(o *runtimeObservability) {
			o.LogDurabilityFailed(observerErr, "sync_failed", types.TxID(42))
		}},
		{name: "panic_stack_enabled", call: func(o *runtimeObservability) {
			_ = o.PanicStackEnabled()
		}},
		{name: "log_executor_fatal", call: func(o *runtimeObservability) {
			o.LogExecutorFatal(observerErr, "post_commit", types.TxID(42))
		}},
		{name: "log_executor_reducer_panic", call: func(o *runtimeObservability) {
			o.LogExecutorReducerPanic("send_message", observerErr, types.TxID(42), "token=secret")
		}},
		{name: "log_executor_lifecycle_reducer_failed", call: func(o *runtimeObservability) {
			o.LogExecutorLifecycleReducerFailed("on_connect", "panic", observerErr)
		}},
		{name: "log_protocol_connection_rejected", call: func(o *runtimeObservability) {
			o.LogProtocolConnectionRejected("rejected_auth", observerErr)
		}},
		{name: "log_protocol_connection_opened", call: func(o *runtimeObservability) {
			o.LogProtocolConnectionOpened(connID)
		}},
		{name: "log_protocol_connection_closed", call: func(o *runtimeObservability) {
			o.LogProtocolConnectionClosed(connID, "server_shutdown")
		}},
		{name: "log_protocol_protocol_error", call: func(o *runtimeObservability) {
			o.LogProtocolProtocolError("one_off_query", "malformed", observerErr)
		}},
		{name: "log_protocol_auth_failed", call: func(o *runtimeObservability) {
			o.LogProtocolAuthFailed("invalid_token", observerErr)
		}},
		{name: "log_protocol_backpressure", call: func(o *runtimeObservability) {
			o.LogProtocolBackpressure("outbound", "buffer_full")
		}},
		{name: "log_subscription_eval_error", call: func(o *runtimeObservability) {
			o.LogSubscriptionEvalError(types.TxID(42), observerErr)
		}},
		{name: "log_subscription_fanout_error", call: func(o *runtimeObservability) {
			o.LogSubscriptionFanoutError("send_failed", &connID, observerErr)
		}},
		{name: "log_subscription_client_dropped", call: func(o *runtimeObservability) {
			o.LogSubscriptionClientDropped("buffer_full", &connID)
		}},
		{name: "record_protocol_connections", call: func(o *runtimeObservability) {
			o.RecordProtocolConnections(3)
		}},
		{name: "record_protocol_message", call: func(o *runtimeObservability) {
			o.RecordProtocolMessage("one_off_query", "validation_error")
		}},
		{name: "record_executor_command", call: func(o *runtimeObservability) {
			o.RecordExecutorCommand("call_reducer", "ok")
		}},
		{name: "record_executor_command_duration", call: func(o *runtimeObservability) {
			o.RecordExecutorCommandDuration("call_reducer", "ok", time.Millisecond)
		}},
		{name: "record_executor_inbox_depth", call: func(o *runtimeObservability) {
			o.RecordExecutorInboxDepth(2)
		}},
		{name: "record_reducer_call", call: func(o *runtimeObservability) {
			o.RecordReducerCall("send_message", "committed")
		}},
		{name: "record_reducer_duration", call: func(o *runtimeObservability) {
			o.RecordReducerDuration("send_message", "committed", time.Millisecond)
		}},
		{name: "record_store_commit_duration", call: func(o *runtimeObservability) {
			o.RecordStoreCommitDuration("ok", time.Millisecond)
		}},
		{name: "trace_reducer_call", call: func(o *runtimeObservability) {
			o.TraceReducerCall("send_message", "failed_user", observerErr)
		}},
		{name: "trace_store_commit", call: func(o *runtimeObservability) {
			o.TraceStoreCommit(types.TxID(42), "error", observerErr)
		}},
		{name: "trace_durability_batch", call: func(o *runtimeObservability) {
			o.TraceDurabilityBatch(types.TxID(42), "error", observerErr)
		}},
		{name: "trace_subscription_eval", call: func(o *runtimeObservability) {
			o.TraceSubscriptionEval(types.TxID(42), "error", observerErr)
		}},
		{name: "trace_subscription_fanout", call: func(o *runtimeObservability) {
			o.TraceSubscriptionFanout("error", "send_failed", observerErr)
		}},
		{name: "trace_subscription_register", call: func(o *runtimeObservability) {
			o.TraceSubscriptionRegister("internal_error", observerErr)
		}},
		{name: "trace_subscription_unregister", call: func(o *runtimeObservability) {
			o.TraceSubscriptionUnregister("internal_error", observerErr)
		}},
		{name: "record_durability_queue_depth", call: func(o *runtimeObservability) {
			o.RecordDurabilityQueueDepth(4)
		}},
		{name: "record_durability_durable_tx_id", call: func(o *runtimeObservability) {
			o.RecordDurabilityDurableTxID(types.TxID(42))
		}},
		{name: "record_snapshot_duration", call: func(o *runtimeObservability) {
			o.RecordSnapshotDuration("error", time.Millisecond)
		}},
		{name: "record_subscription_active", call: func(o *runtimeObservability) {
			o.RecordSubscriptionActive(5)
		}},
		{name: "record_subscription_eval_duration", call: func(o *runtimeObservability) {
			o.RecordSubscriptionEvalDuration("error", time.Millisecond)
		}},
		{name: "log_store_snapshot_leaked", call: func(o *runtimeObservability) {
			o.LogStoreSnapshotLeaked("test")
		}},
		{name: "record_store_read_rows", call: func(o *runtimeObservability) {
			o.RecordStoreReadRows("table_scan", 1)
		}},
		{name: "metric_helpers", call: func(o *runtimeObservability) {
			o.addCounter(MetricRuntimeErrorsTotal, MetricLabels{Component: "runtime"}, 1)
			o.setGauge(MetricRuntimeReady, MetricLabels{}, 1)
			o.observeHistogram(MetricReducerDurationSeconds, MetricLabels{Reducer: "send_message"}, 0.1)
		}},
		{name: "start_span", call: func(o *runtimeObservability) {
			_, span := o.startSpan(context.Background(), "shunter.runtime.start", "runtime")
			if span != nil {
				span.AddEvent("event", TraceAttr{Key: "token", Value: "secret"})
				span.End(observerErr)
			}
		}},
	}

	tracers := []struct {
		name   string
		tracer Tracer
	}{
		{name: "start_span_panic", tracer: panicStartTracer{}},
		{name: "span_add_event_panic", tracer: addEventPanicTracer{}},
		{name: "span_end_panic", tracer: endPanicTracer{}},
	}

	for _, tracerCase := range tracers {
		t.Run(tracerCase.name, func(t *testing.T) {
			for _, method := range methods {
				t.Run(method.name, func(t *testing.T) {
					obs := newRuntimeObservability("chat", ObservabilityConfig{
						RuntimeLabel: "panic-sinks-a",
						Logger:       slog.New(panicSlogHandler{}),
						Redaction:    RedactionConfig{ErrorMessageMaxBytes: 96},
						Metrics: MetricsConfig{
							Enabled:  true,
							Recorder: panicMetricsRecorder{},
						},
						Tracing: TracingConfig{
							Enabled: true,
							Tracer:  tracerCase.tracer,
						},
					})
					requireNoPanic(t, func() { method.call(obs) })
				})
			}
		})
	}
}

func TestRuntimePanicStackEnabledRecoversLoggerEnabledPanic(t *testing.T) {
	metrics := &recordingMetricsRecorder{}
	obs := newRuntimeObservability("chat", ObservabilityConfig{
		RuntimeLabel: "enabled-panic-a",
		Logger:       slog.New(enabledPanicSlogHandler{}),
		Metrics: MetricsConfig{
			Enabled:  true,
			Recorder: metrics,
		},
	})

	if obs.PanicStackEnabled() {
		t.Fatal("PanicStackEnabled returned true after logger Enabled panic")
	}
	metrics.requireCounter(t, MetricRuntimeErrorsTotal, MetricLabels{
		Module:    "chat",
		Runtime:   "enabled-panic-a",
		Component: "observability",
		Reason:    "observability_sink_failed",
	}, 1)
}

func TestRuntimeReducerPanicResultSurvivesLoggerEnabledPanic(t *testing.T) {
	rt, err := Build(validChatModule().Reducer("explode", func(*schema.ReducerContext, []byte) ([]byte, error) {
		panic("token=secret")
	}), Config{
		DataDir: t.TempDir(),
		Observability: ObservabilityConfig{
			Logger: slog.New(enabledPanicSlogHandler{}),
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if err := rt.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error with panicking logger Enabled: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := rt.CallReducer(ctx, "explode", nil)
	if err != nil {
		t.Fatalf("CallReducer returned error with panicking logger Enabled: %v", err)
	}
	if res.Status != StatusFailedPanic {
		t.Fatalf("CallReducer status = %v, want panic; response=%+v", res.Status, res)
	}
}

func TestRuntimeConfigObservabilityCopyKeepsOwnedSlicesDetached(t *testing.T) {
	logger := slog.Default()
	handler := noopHTTPHandler{}
	rec := &countingMetricsRecorder{}
	tracer := &countingTracer{}
	rt, err := Build(validChatModule(), Config{
		DataDir:        t.TempDir(),
		AuthSigningKey: []byte("01234567890123456789012345678901"),
		AuthAudiences:  []string{"aud-1"},
		Observability: ObservabilityConfig{
			Logger:       logger,
			RuntimeLabel: "  public-label  ",
			Diagnostics:  DiagnosticsConfig{MetricsHandler: handler},
			Metrics: MetricsConfig{
				Enabled:  true,
				Recorder: rec,
			},
			Tracing: TracingConfig{
				Enabled: true,
				Tracer:  tracer,
			},
		},
	})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	cfg := rt.Config()
	cfg.AuthSigningKey[0] = 'x'
	cfg.AuthAudiences[0] = "mutated"

	again := rt.Config()
	if string(again.AuthSigningKey) != "01234567890123456789012345678901" {
		t.Fatalf("AuthSigningKey mutated through Config(): %q", string(again.AuthSigningKey))
	}
	if got := again.AuthAudiences[0]; got != "aud-1" {
		t.Fatalf("AuthAudiences mutated through Config(): %q", got)
	}
	if again.Observability.Logger != logger {
		t.Fatal("Config() did not preserve caller-supplied logger pointer")
	}
	if again.Observability.Diagnostics.MetricsHandler != handler {
		t.Fatal("Config() did not preserve caller-supplied metrics handler")
	}
	if again.Observability.Metrics.Recorder != rec {
		t.Fatal("Config() did not preserve caller-supplied metrics recorder")
	}
	if again.Observability.Tracing.Tracer != tracer {
		t.Fatal("Config() did not preserve caller-supplied tracer")
	}
}

type countingMetricsRecorder struct {
	calls atomic.Uint64
}

func (r *countingMetricsRecorder) AddCounter(MetricName, MetricLabels, uint64) {
	r.calls.Add(1)
}

func (r *countingMetricsRecorder) SetGauge(MetricName, MetricLabels, float64) {
	r.calls.Add(1)
}

func (r *countingMetricsRecorder) ObserveHistogram(MetricName, MetricLabels, float64) {
	r.calls.Add(1)
}

type panicMetricsRecorder struct{}

func (panicMetricsRecorder) AddCounter(MetricName, MetricLabels, uint64) {
	panic("metrics failed")
}

func (panicMetricsRecorder) SetGauge(MetricName, MetricLabels, float64) {
	panic("metrics failed")
}

func (panicMetricsRecorder) ObserveHistogram(MetricName, MetricLabels, float64) {
	panic("metrics failed")
}

type secretPanicMetricsRecorder struct{}

func (secretPanicMetricsRecorder) AddCounter(MetricName, MetricLabels, uint64) {
	panic("token=secret")
}

func (secretPanicMetricsRecorder) SetGauge(MetricName, MetricLabels, float64) {
	panic("token=secret")
}

func (secretPanicMetricsRecorder) ObserveHistogram(MetricName, MetricLabels, float64) {
	panic("token=secret")
}

type countingTracer struct {
	starts atomic.Uint64
}

func (t *countingTracer) StartSpan(ctx context.Context, _ string, _ ...TraceAttr) (context.Context, Span) {
	t.starts.Add(1)
	return ctx, nil
}

type panicTracer struct{}

func (panicTracer) StartSpan(context.Context, string, ...TraceAttr) (context.Context, Span) {
	panic("tracer failed")
}

type panicSlogHandler struct{}

func (panicSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (panicSlogHandler) Handle(context.Context, slog.Record) error {
	panic("logger failed")
}

func (h panicSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h panicSlogHandler) WithGroup(string) slog.Handler { return h }

type enabledPanicSlogHandler struct{}

func (enabledPanicSlogHandler) Enabled(context.Context, slog.Level) bool {
	panic("logger enabled failed token=secret")
}

func (enabledPanicSlogHandler) Handle(context.Context, slog.Record) error {
	panic("logger handle should not be called")
}

func (h enabledPanicSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h enabledPanicSlogHandler) WithGroup(string) slog.Handler { return h }

func requireNoPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}

type secretPanicSlogHandler struct{}

func (secretPanicSlogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (secretPanicSlogHandler) Handle(context.Context, slog.Record) error {
	panic("authorization=Bearer secret")
}

func (h secretPanicSlogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h secretPanicSlogHandler) WithGroup(string) slog.Handler { return h }

type noopHTTPHandler struct{}

func (noopHTTPHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func containsSeededSensitiveSecret(raw string) bool {
	return strings.Contains(raw, "authorization=Bearer secret") ||
		strings.Contains(raw, `"token":"secret"`) ||
		strings.Contains(raw, "payload='secret'") ||
		strings.Contains(raw, "query=select * from users where token='secret'") ||
		strings.Contains(raw, "Bearer secret")
}

func validObservabilityRuntimeLabel(label string) bool {
	if label == "" || !utf8.ValidString(label) || len(label) > 128 {
		return false
	}
	for i := 0; i < len(label); i++ {
		if label[i] < 0x20 || label[i] == 0x7f {
			return false
		}
	}
	return true
}

func requireRecordedLog(t *testing.T, logs *recordingLogState, event string) recordedLog {
	t.Helper()
	for _, record := range logs.records() {
		if record.message == event {
			return record
		}
	}
	t.Fatalf("missing log event %q in %+v", event, logs.records())
	return recordedLog{}
}
