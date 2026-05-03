package shunter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ponchione/shunter/commitlog"
	"github.com/ponchione/shunter/types"
)

const (
	defaultObservabilityRuntimeLabel         = "default"
	defaultObservabilityErrorMessageMaxBytes = 1024
)

const (
	traceSpanRuntimeStart           = "shunter.runtime.start"
	traceSpanRecoveryOpen           = "shunter.recovery.open"
	traceSpanProtocolMessage        = "shunter.protocol.message"
	traceSpanReducerCall            = "shunter.reducer.call"
	traceSpanStoreCommit            = "shunter.store.commit"
	traceSpanDurabilityBatch        = "shunter.durability.batch"
	traceSpanSubscriptionEval       = "shunter.subscription.eval"
	traceSpanSubscriptionFanout     = "shunter.subscription.fanout"
	traceSpanQueryOneOff            = "shunter.query.one_off"
	traceSpanSubscriptionRegister   = "shunter.subscription.register"
	traceSpanSubscriptionUnregister = "shunter.subscription.unregister"
)

// ObservabilityConfig configures runtime-scoped logs, metrics, diagnostics,
// and tracing. The zero value is a no-op for all external observations.
type ObservabilityConfig struct {
	// Logger is the runtime-scoped structured logger. Nil means no-op.
	Logger *slog.Logger

	// RuntimeLabel is the low-cardinality instance label used by logs,
	// metrics, diagnostics, and traces. Empty means "default".
	RuntimeLabel string

	Redaction   RedactionConfig
	Metrics     MetricsConfig
	Diagnostics DiagnosticsConfig
	Tracing     TracingConfig
}

// RedactionConfig controls redaction and bounding for operator-facing text.
type RedactionConfig struct {
	// ErrorMessageMaxBytes bounds redacted error text in logs and HTTP
	// diagnostics. Values <= 0 use 1024.
	ErrorMessageMaxBytes int

	// AllowRawSQLInDebugLogs permits raw SQL text only in debug-level logs.
	// It never permits raw SQL in metrics, traces, info/warn/error logs, or
	// HTTP health payloads.
	AllowRawSQLInDebugLogs bool
}

// MetricsConfig configures Shunter-owned metric observations.
type MetricsConfig struct {
	// Enabled gates all metrics calls. False means no-op even when Recorder is
	// non-nil.
	Enabled bool

	// Recorder receives metric observations. Nil means no-op.
	Recorder MetricsRecorder

	// ReducerLabelMode controls the reducer label value for reducer metrics.
	// Empty means ReducerLabelModeName.
	ReducerLabelMode ReducerLabelMode
}

// ReducerLabelMode controls reducer metric label cardinality.
type ReducerLabelMode string

const (
	ReducerLabelModeName      ReducerLabelMode = "name"
	ReducerLabelModeAggregate ReducerLabelMode = "aggregate"
)

// DiagnosticsConfig configures optional HTTP diagnostics mounting.
type DiagnosticsConfig struct {
	// MountHTTP controls whether Runtime.HTTPHandler() mounts runtime
	// diagnostics endpoints in addition to /subscribe.
	MountHTTP bool

	// MetricsHandler is mounted at /metrics only when MountHTTP is true and
	// MetricsHandler is non-nil. The Prometheus adapter supplies this handler.
	MetricsHandler http.Handler
}

// TracingConfig configures Shunter-owned tracing hooks.
type TracingConfig struct {
	// Enabled gates tracing hooks. False means no-op even when Tracer is
	// non-nil.
	Enabled bool

	// Tracer starts spans. Nil means no-op.
	Tracer Tracer
}

// MetricName names a Shunter metric family before adapter-specific namespacing.
type MetricName string

const (
	MetricRuntimeReady                    MetricName = "runtime_ready"
	MetricRuntimeState                    MetricName = "runtime_state"
	MetricRuntimeDegraded                 MetricName = "runtime_degraded"
	MetricRuntimeErrorsTotal              MetricName = "runtime_errors_total"
	MetricProtocolConnections             MetricName = "protocol_connections"
	MetricProtocolConnectionsTotal        MetricName = "protocol_connections_total"
	MetricProtocolMessagesTotal           MetricName = "protocol_messages_total"
	MetricProtocolBackpressureTotal       MetricName = "protocol_backpressure_total"
	MetricExecutorCommandsTotal           MetricName = "executor_commands_total"
	MetricExecutorCommandDurationSeconds  MetricName = "executor_command_duration_seconds"
	MetricExecutorInboxDepth              MetricName = "executor_inbox_depth"
	MetricExecutorFatal                   MetricName = "executor_fatal"
	MetricReducerCallsTotal               MetricName = "reducer_calls_total"
	MetricReducerDurationSeconds          MetricName = "reducer_duration_seconds"
	MetricDurabilityDurableTxID           MetricName = "durability_durable_tx_id"
	MetricDurabilityQueueDepth            MetricName = "durability_queue_depth"
	MetricDurabilityFailuresTotal         MetricName = "durability_failures_total"
	MetricSubscriptionActive              MetricName = "subscription_active"
	MetricSubscriptionEvalDurationSeconds MetricName = "subscription_eval_duration_seconds"
	MetricSubscriptionFanoutErrorsTotal   MetricName = "subscription_fanout_errors_total"
	MetricSubscriptionDroppedClientsTotal MetricName = "subscription_dropped_clients_total"
	MetricRecoveryRunsTotal               MetricName = "recovery_runs_total"
	MetricRecoveryRecoveredTxID           MetricName = "recovery_recovered_tx_id"
	MetricRecoveryDamagedTailSegments     MetricName = "recovery_damaged_tail_segments"
	MetricRecoverySkippedSnapshotsTotal   MetricName = "recovery_skipped_snapshots_total"
)

// MetricLabels is intentionally fixed so Shunter code cannot create free-form
// metric labels.
type MetricLabels struct {
	Module    string
	Runtime   string
	Component string
	Kind      string
	State     string
	Result    string
	Reason    string
	Direction string
	Reducer   string
}

// MetricsRecorder receives Shunter metric observations.
type MetricsRecorder interface {
	AddCounter(name MetricName, labels MetricLabels, delta uint64)
	SetGauge(name MetricName, labels MetricLabels, value float64)
	ObserveHistogram(name MetricName, labels MetricLabels, value float64)
}

// TraceAttr is a Shunter-owned tracing attribute.
type TraceAttr struct {
	Key   string
	Value any
}

// Tracer starts Shunter-owned spans.
type Tracer interface {
	StartSpan(ctx context.Context, name string, attrs ...TraceAttr) (context.Context, Span)
}

// Span is a Shunter-owned tracing span.
type Span interface {
	AddEvent(name string, attrs ...TraceAttr)
	End(err error)
}

func normalizeObservabilityConfig(cfg ObservabilityConfig) (ObservabilityConfig, error) {
	out := cfg
	label, err := normalizeObservabilityRuntimeLabel(cfg.RuntimeLabel)
	if err != nil {
		return ObservabilityConfig{}, err
	}
	out.RuntimeLabel = label
	if out.Redaction.ErrorMessageMaxBytes <= 0 {
		out.Redaction.ErrorMessageMaxBytes = defaultObservabilityErrorMessageMaxBytes
	}
	switch out.Metrics.ReducerLabelMode {
	case "":
		out.Metrics.ReducerLabelMode = ReducerLabelModeName
	case ReducerLabelModeName, ReducerLabelModeAggregate:
	default:
		return ObservabilityConfig{}, fmt.Errorf("observability reducer label mode is invalid")
	}
	return out, nil
}

func normalizeObservabilityRuntimeLabel(raw string) (string, error) {
	label := strings.TrimSpace(raw)
	if label == "" {
		label = defaultObservabilityRuntimeLabel
	}
	if !utf8.ValidString(label) {
		return "", fmt.Errorf("observability runtime label must be valid UTF-8")
	}
	if len(label) > 128 {
		return "", fmt.Errorf("observability runtime label must be at most 128 bytes")
	}
	for i := 0; i < len(label); i++ {
		if label[i] < 0x20 || label[i] == 0x7f {
			return "", fmt.Errorf("observability runtime label must not contain ASCII control characters")
		}
	}
	return label, nil
}

func buildFailureObservabilityRuntimeLabel(raw string) string {
	label, err := normalizeObservabilityRuntimeLabel(raw)
	if err != nil {
		return defaultObservabilityRuntimeLabel
	}
	return label
}

type runtimeObservability struct {
	config       ObservabilityConfig
	moduleName   string
	runtimeLabel string
	redaction    RedactionConfig
	logger       *slog.Logger
	metrics      MetricsRecorder
	tracer       Tracer
	sinkFailure  atomic.Bool
}

func newRuntimeObservability(moduleName string, cfg ObservabilityConfig) *runtimeObservability {
	normalized, err := normalizeObservabilityConfig(cfg)
	if err != nil {
		normalized = ObservabilityConfig{
			RuntimeLabel: defaultObservabilityRuntimeLabel,
			Redaction: RedactionConfig{
				ErrorMessageMaxBytes: defaultObservabilityErrorMessageMaxBytes,
			},
		}
	}
	return newRuntimeObservabilityFromNormalized(moduleName, normalized)
}

func newBuildObservability(moduleName string, cfg ObservabilityConfig) (*runtimeObservability, error) {
	normalized, err := normalizeObservabilityConfig(cfg)
	if err != nil {
		normalized = cfg
		normalized.RuntimeLabel = buildFailureObservabilityRuntimeLabel(cfg.RuntimeLabel)
		if normalized.Redaction.ErrorMessageMaxBytes <= 0 {
			normalized.Redaction.ErrorMessageMaxBytes = defaultObservabilityErrorMessageMaxBytes
		}
		if normalized.Metrics.ReducerLabelMode == "" {
			normalized.Metrics.ReducerLabelMode = ReducerLabelModeName
		}
		return newRuntimeObservabilityFromNormalized(moduleName, normalized), err
	}
	return newRuntimeObservabilityFromNormalized(moduleName, normalized), nil
}

func newRuntimeObservabilityFromNormalized(moduleName string, normalized ObservabilityConfig) *runtimeObservability {
	o := &runtimeObservability{
		config:       normalized,
		moduleName:   moduleName,
		runtimeLabel: normalized.RuntimeLabel,
		redaction:    normalized.Redaction,
		logger:       normalized.Logger,
	}
	if normalized.Metrics.Enabled {
		o.metrics = normalized.Metrics.Recorder
	}
	if normalized.Tracing.Enabled {
		o.tracer = normalized.Tracing.Tracer
	}
	return o
}

func (o *runtimeObservability) setModuleName(moduleName string) {
	if o == nil {
		return
	}
	if strings.TrimSpace(moduleName) == "" {
		moduleName = "unknown"
	}
	o.moduleName = moduleName
}

func (o *runtimeObservability) metricLabels(labels MetricLabels) MetricLabels {
	if o == nil {
		return labels
	}
	if labels.Module == "" {
		labels.Module = o.moduleName
	}
	if labels.Runtime == "" {
		labels.Runtime = o.runtimeLabel
	}
	return labels
}

func (o *runtimeObservability) log(ctx context.Context, level slog.Level, event, component string, attrs ...slog.Attr) {
	if o == nil || o.logger == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	recordAttrs := []slog.Attr{
		slog.String("component", component),
		slog.String("event", event),
		slog.String("module", o.moduleName),
		slog.String("runtime", o.runtimeLabel),
	}
	recordAttrs = append(recordAttrs, attrs...)
	defer func() {
		if r := recover(); r != nil {
			o.recordSinkFailure("logger", r)
		}
	}()
	o.logger.LogAttrs(ctx, level, event, recordAttrs...)
}

func (o *runtimeObservability) recordBuildFailed(err error) {
	if err == nil {
		return
	}
	o.log(context.Background(), slog.LevelError, "runtime.build_failed", "runtime",
		slog.String("error", o.redactError(err)),
	)
	o.recordRuntimeError("build_failed")
}

func (o *runtimeObservability) recordRecoveryCompleted(report commitlog.RecoveryReport, duration time.Duration) {
	o.traceSpan(traceSpanRecoveryOpen, "commitlog", nil,
		TraceAttr{Key: "result", Value: "success"},
		TraceAttr{Key: "tx_id", Value: uint64(report.RecoveredTxID)},
	)
	level := slog.LevelInfo
	if len(report.DamagedTailSegments) > 0 || len(report.SkippedSnapshots) > 0 {
		level = slog.LevelWarn
	}
	o.log(context.Background(), level, "recovery.completed", "commitlog",
		slog.Uint64("tx_id", uint64(report.RecoveredTxID)),
		slog.Int64("duration_ms", duration.Milliseconds()),
		slog.Int("damaged_tail_segments", len(report.DamagedTailSegments)),
		slog.Int("skipped_snapshots", len(report.SkippedSnapshots)),
	)
	o.addCounter(MetricRecoveryRunsTotal, MetricLabels{
		Component: "commitlog",
		Result:    "success",
	}, 1)
	o.setGauge(MetricRecoveryRecoveredTxID, MetricLabels{
		Component: "commitlog",
	}, float64(report.RecoveredTxID))
	o.setGauge(MetricRecoveryDamagedTailSegments, MetricLabels{
		Component: "commitlog",
	}, float64(len(report.DamagedTailSegments)))
	o.setGauge(MetricDurabilityDurableTxID, MetricLabels{}, float64(report.RecoveredTxID))
	for _, skipped := range report.SkippedSnapshots {
		o.addCounter(MetricRecoverySkippedSnapshotsTotal, MetricLabels{
			Component: "commitlog",
			Reason:    recoverySkippedSnapshotMetricReason(skipped.Reason),
		}, 1)
	}
}

func (o *runtimeObservability) recordRecoveryFailed(err error, duration time.Duration) {
	if err == nil {
		return
	}
	o.traceSpan(traceSpanRecoveryOpen, "commitlog", err,
		TraceAttr{Key: "result", Value: "failed"},
	)
	o.log(context.Background(), slog.LevelError, "recovery.failed", "commitlog",
		slog.String("error", o.redactError(err)),
		slog.Int64("duration_ms", duration.Milliseconds()),
	)
	o.addCounter(MetricRecoveryRunsTotal, MetricLabels{
		Component: "commitlog",
		Result:    "failed",
	}, 1)
}

func (o *runtimeObservability) recordRuntimeStartFailed(ctx context.Context, err error, duration time.Duration) {
	if err == nil {
		return
	}
	o.traceSpan(traceSpanRuntimeStart, "runtime", err,
		TraceAttr{Key: "state", Value: string(RuntimeStateFailed)},
	)
	o.log(ctx, slog.LevelError, "runtime.start_failed", "runtime",
		slog.String("error", o.redactError(err)),
		slog.Int64("duration_ms", duration.Milliseconds()),
	)
}

func (o *runtimeObservability) recordRuntimeReady(ctx context.Context, health RuntimeHealth, duration time.Duration) {
	o.traceSpan(traceSpanRuntimeStart, "runtime", nil,
		TraceAttr{Key: "state", Value: string(health.State)},
	)
	o.log(ctx, slog.LevelInfo, "runtime.ready", "runtime",
		slog.String("state", string(health.State)),
		slog.Bool("ready", health.Ready),
		slog.Bool("degraded", health.Degraded),
		slog.Int64("duration_ms", duration.Milliseconds()),
	)
}

func (o *runtimeObservability) recordRuntimeCloseFailed(err error, duration time.Duration) {
	if err == nil {
		return
	}
	o.log(context.Background(), slog.LevelError, "runtime.close_failed", "runtime",
		slog.String("error", o.redactError(err)),
		slog.Int64("duration_ms", duration.Milliseconds()),
	)
}

func (o *runtimeObservability) recordRuntimeClosed(state RuntimeState, duration time.Duration) {
	o.log(context.Background(), slog.LevelInfo, "runtime.closed", "runtime",
		slog.String("state", string(state)),
		slog.Int64("duration_ms", duration.Milliseconds()),
	)
}

func (o *runtimeObservability) recordRuntimeHealthDegraded(health RuntimeHealth, reason runtimeDegradedReason) {
	if reason == runtimeDegradedReasonNone {
		return
	}
	o.log(context.Background(), slog.LevelWarn, "runtime.health_degraded", "runtime",
		slog.String("state", string(health.State)),
		slog.String("reason", string(reason)),
	)
}

func (o *runtimeObservability) recordRuntimeHealthMetrics(health RuntimeHealth) {
	if o == nil {
		return
	}
	o.setGauge(MetricRuntimeReady, MetricLabels{}, boolMetricValue(health.Ready))
	for _, state := range runtimeMetricStates {
		value := 0.0
		if health.State == state {
			value = 1
		}
		o.setGauge(MetricRuntimeState, MetricLabels{State: string(state)}, value)
	}
	o.setGauge(MetricRuntimeDegraded, MetricLabels{}, boolMetricValue(health.Degraded))
	o.setGauge(MetricDurabilityDurableTxID, MetricLabels{}, float64(health.Durability.DurableTxID))
}

func (o *runtimeObservability) recordRuntimeError(reason string) {
	o.addCounter(MetricRuntimeErrorsTotal, MetricLabels{
		Component: "runtime",
		Reason:    runtimeErrorMetricReason(reason),
	}, 1)
}

func (o *runtimeObservability) LogDurabilityFailed(err error, reason string, txID types.TxID) {
	if err == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("error", o.redactError(err)),
		slog.String("reason", reason),
	}
	if txID != 0 {
		attrs = append(attrs, slog.Uint64("tx_id", uint64(txID)))
	}
	o.log(context.Background(), slog.LevelError, "durability.failed", "commitlog", attrs...)
	o.addCounter(MetricDurabilityFailuresTotal, MetricLabels{
		Reason: durabilityFailureMetricReason(reason),
	}, 1)
}

func (o *runtimeObservability) PanicStackEnabled() (enabled bool) {
	if o == nil || o.logger == nil {
		return false
	}
	defer func() {
		if r := recover(); r != nil {
			enabled = false
			o.recordSinkFailure("logger", r)
		}
	}()
	return o.logger.Enabled(context.Background(), slog.LevelDebug)
}

func (o *runtimeObservability) LogExecutorFatal(err error, reason string, txID types.TxID) {
	if err == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("error", o.redactError(err)),
		slog.String("reason", reason),
	}
	if txID != 0 {
		attrs = append(attrs, slog.Uint64("tx_id", uint64(txID)))
	}
	o.log(context.Background(), slog.LevelError, "executor.fatal", "executor", attrs...)
	o.setGauge(MetricExecutorFatal, MetricLabels{}, 1)
}

func (o *runtimeObservability) LogExecutorReducerPanic(reducer string, err error, txID types.TxID, stack string) {
	if err == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("reducer", reducer),
		slog.String("error", o.redactError(err)),
	}
	if txID != 0 {
		attrs = append(attrs, slog.Uint64("tx_id", uint64(txID)))
	}
	if stack != "" {
		attrs = append(attrs, slog.String("stack", o.redactErrorString(stack)))
	}
	o.log(context.Background(), slog.LevelError, "executor.reducer_panic", "executor", attrs...)
}

func (o *runtimeObservability) LogExecutorLifecycleReducerFailed(reducer, result string, err error) {
	if err == nil {
		return
	}
	o.log(context.Background(), slog.LevelWarn, "executor.lifecycle_reducer_failed", "executor",
		slog.String("reducer", reducer),
		slog.String("result", result),
		slog.String("error", o.redactError(err)),
	)
}

func (o *runtimeObservability) LogProtocolConnectionRejected(result string, err error) {
	attrs := []slog.Attr{slog.String("result", result)}
	if err != nil {
		attrs = append(attrs, slog.String("error", o.redactError(err)))
	}
	o.log(context.Background(), slog.LevelWarn, "protocol.connection_rejected", "protocol", attrs...)
	o.addCounter(MetricProtocolConnectionsTotal, MetricLabels{
		Result: protocolConnectionMetricResult(result),
	}, 1)
}

func (o *runtimeObservability) LogProtocolConnectionOpened(connID types.ConnectionID) {
	o.log(context.Background(), slog.LevelDebug, "protocol.connection_opened", "protocol",
		slog.String("connection_id", connID.Hex()),
	)
	o.addCounter(MetricProtocolConnectionsTotal, MetricLabels{
		Result: "accepted",
	}, 1)
}

func (o *runtimeObservability) LogProtocolConnectionClosed(connID types.ConnectionID, reason string) {
	o.log(context.Background(), slog.LevelDebug, "protocol.connection_closed", "protocol",
		slog.String("connection_id", connID.Hex()),
		slog.String("reason", reason),
	)
}

func (o *runtimeObservability) LogProtocolProtocolError(kind, reason string, err error) {
	if err == nil {
		return
	}
	o.log(context.Background(), slog.LevelWarn, "protocol.protocol_error", "protocol",
		slog.String("kind", kind),
		slog.String("reason", reason),
		slog.String("error", o.redactError(err)),
	)
}

func (o *runtimeObservability) LogProtocolAuthFailed(reason string, err error) {
	if err == nil {
		return
	}
	o.log(context.Background(), slog.LevelWarn, "protocol.auth_failed", "protocol",
		slog.String("reason", reason),
		slog.String("error", o.redactError(err)),
	)
}

func (o *runtimeObservability) LogProtocolBackpressure(direction, reason string) {
	o.log(context.Background(), slog.LevelWarn, "protocol.backpressure", "protocol",
		slog.String("direction", direction),
		slog.String("reason", reason),
	)
	o.addCounter(MetricProtocolBackpressureTotal, MetricLabels{
		Direction: protocolBackpressureMetricDirection(direction),
	}, 1)
}

func (o *runtimeObservability) LogSubscriptionEvalError(txID types.TxID, err error) {
	if err == nil {
		return
	}
	o.log(context.Background(), slog.LevelWarn, "subscription.eval_error", "subscription",
		slog.Uint64("tx_id", uint64(txID)),
		slog.String("error", o.redactError(err)),
	)
}

func (o *runtimeObservability) LogSubscriptionFanoutError(reason string, connID *types.ConnectionID, err error) {
	if err == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("reason", reason),
		slog.String("error", o.redactError(err)),
	}
	if connID != nil {
		attrs = append(attrs, slog.String("connection_id", connID.Hex()))
	}
	o.log(context.Background(), slog.LevelWarn, "subscription.fanout_error", "subscription", attrs...)
	o.addCounter(MetricSubscriptionFanoutErrorsTotal, MetricLabels{
		Reason: subscriptionFanoutMetricReason(reason),
	}, 1)
}

func (o *runtimeObservability) LogSubscriptionClientDropped(reason string, connID *types.ConnectionID) {
	attrs := []slog.Attr{slog.String("reason", reason)}
	if connID != nil {
		attrs = append(attrs, slog.String("connection_id", connID.Hex()))
	}
	o.log(context.Background(), slog.LevelWarn, "subscription.client_dropped", "subscription", attrs...)
	o.addCounter(MetricSubscriptionDroppedClientsTotal, MetricLabels{
		Reason: subscriptionDroppedMetricReason(reason),
	}, 1)
}

func (o *runtimeObservability) RecordProtocolConnections(active int) {
	o.setGauge(MetricProtocolConnections, MetricLabels{}, float64(active))
}

func (o *runtimeObservability) RecordProtocolMessage(kind, result string) {
	kind = protocolMessageMetricKind(kind)
	result = protocolMessageMetricResult(result)
	o.traceSpan(traceSpanProtocolMessage, "protocol", safeTraceError(result, nil),
		TraceAttr{Key: "kind", Value: kind},
		TraceAttr{Key: "result", Value: result},
	)
	if kind == "one_off_query" {
		o.traceSpan(traceSpanQueryOneOff, "protocol", safeTraceError(result, nil),
			TraceAttr{Key: "result", Value: result},
		)
	}
	o.addCounter(MetricProtocolMessagesTotal, MetricLabels{
		Kind:   kind,
		Result: result,
	}, 1)
}

func (o *runtimeObservability) RecordExecutorCommand(kind, result string) {
	o.addCounter(MetricExecutorCommandsTotal, MetricLabels{
		Kind:   executorCommandMetricKind(kind),
		Result: executorCommandMetricResult(result),
	}, 1)
}

func (o *runtimeObservability) RecordExecutorCommandDuration(kind, result string, duration time.Duration) {
	o.observeHistogram(MetricExecutorCommandDurationSeconds, MetricLabels{
		Kind:   executorCommandMetricKind(kind),
		Result: executorCommandMetricResult(result),
	}, duration.Seconds())
}

func (o *runtimeObservability) RecordExecutorInboxDepth(depth int) {
	o.setGauge(MetricExecutorInboxDepth, MetricLabels{}, float64(depth))
}

func (o *runtimeObservability) RecordReducerCall(reducer, result string) {
	o.addCounter(MetricReducerCallsTotal, MetricLabels{
		Reducer: o.reducerMetricLabel(reducer),
		Result:  reducerMetricResult(result),
	}, 1)
}

func (o *runtimeObservability) RecordReducerDuration(reducer, result string, duration time.Duration) {
	o.observeHistogram(MetricReducerDurationSeconds, MetricLabels{
		Reducer: o.reducerMetricLabel(reducer),
		Result:  reducerMetricResult(result),
	}, duration.Seconds())
}

func (o *runtimeObservability) TraceReducerCall(reducer, result string, err error) {
	result = reducerMetricResult(result)
	o.traceSpan(traceSpanReducerCall, "executor", safeTraceError(result, err),
		TraceAttr{Key: "reducer", Value: reducerTraceName(reducer)},
		TraceAttr{Key: "result", Value: result},
	)
}

func (o *runtimeObservability) TraceStoreCommit(txID types.TxID, result string, err error) {
	result = okErrorTraceResult(result)
	o.traceSpan(traceSpanStoreCommit, "store", safeTraceError(result, err),
		TraceAttr{Key: "tx_id", Value: uint64(txID)},
		TraceAttr{Key: "result", Value: result},
	)
}

func (o *runtimeObservability) TraceDurabilityBatch(txID types.TxID, result string, err error) {
	result = okErrorTraceResult(result)
	o.traceSpan(traceSpanDurabilityBatch, "commitlog", safeTraceError(result, err),
		TraceAttr{Key: "tx_id", Value: uint64(txID)},
		TraceAttr{Key: "result", Value: result},
	)
}

func (o *runtimeObservability) TraceSubscriptionEval(txID types.TxID, result string, err error) {
	result = subscriptionEvalMetricResult(result)
	o.traceSpan(traceSpanSubscriptionEval, "subscription", safeTraceError(result, err),
		TraceAttr{Key: "tx_id", Value: uint64(txID)},
		TraceAttr{Key: "result", Value: result},
	)
}

func (o *runtimeObservability) TraceSubscriptionFanout(result, reason string, err error) {
	result = okErrorTraceResult(result)
	attrs := []TraceAttr{{Key: "result", Value: result}}
	if result != "ok" {
		attrs = append(attrs, TraceAttr{Key: "reason", Value: subscriptionFanoutMetricReason(reason)})
	}
	o.traceSpan(traceSpanSubscriptionFanout, "subscription", safeTraceError(result, err), attrs...)
}

func (o *runtimeObservability) TraceSubscriptionRegister(result string, err error) {
	result = executorCommandMetricResult(result)
	o.traceSpan(traceSpanSubscriptionRegister, "subscription", safeTraceError(result, err),
		TraceAttr{Key: "result", Value: result},
	)
}

func (o *runtimeObservability) TraceSubscriptionUnregister(result string, err error) {
	result = executorCommandMetricResult(result)
	o.traceSpan(traceSpanSubscriptionUnregister, "subscription", safeTraceError(result, err),
		TraceAttr{Key: "result", Value: result},
	)
}

func (o *runtimeObservability) RecordDurabilityQueueDepth(depth int) {
	o.setGauge(MetricDurabilityQueueDepth, MetricLabels{}, float64(depth))
}

func (o *runtimeObservability) RecordDurabilityDurableTxID(txID types.TxID) {
	o.setGauge(MetricDurabilityDurableTxID, MetricLabels{}, float64(txID))
}

func (o *runtimeObservability) RecordSubscriptionActive(active int) {
	o.setGauge(MetricSubscriptionActive, MetricLabels{}, float64(active))
}

func (o *runtimeObservability) RecordSubscriptionEvalDuration(result string, duration time.Duration) {
	o.observeHistogram(MetricSubscriptionEvalDurationSeconds, MetricLabels{
		Result: subscriptionEvalMetricResult(result),
	}, duration.Seconds())
}

func (o *runtimeObservability) LogStoreSnapshotLeaked(reason string) {
	o.log(context.Background(), slog.LevelError, "store.snapshot_leaked", "store",
		slog.String("reason", reason),
	)
}

func recoverySkippedSnapshotMetricReason(reason commitlog.SnapshotSkipReason) string {
	switch reason {
	case commitlog.SnapshotSkipPastDurableHorizon:
		return "newer_than_log"
	case commitlog.SnapshotSkipReadFailed:
		return "read_failed"
	default:
		return "unknown"
	}
}

func runtimeErrorMetricReason(reason string) string {
	switch reason {
	case "build_failed", "start_failed", "close_failed", "panic", "observability_sink_failed":
		return reason
	default:
		return "unknown"
	}
}

func durabilityFailureMetricReason(reason string) string {
	switch reason {
	case "open_failed", "write_failed", "sync_failed", "segment_rotate_failed", "close_failed", "replay_failed", "corrupt_segment", "context_canceled":
		return reason
	default:
		return "unknown"
	}
}

func protocolConnectionMetricResult(result string) string {
	switch result {
	case "accepted", "rejected_not_ready", "rejected_auth", "rejected_upgrade", "rejected_executor", "rejected_internal":
		return result
	default:
		return "rejected_internal"
	}
}

func protocolMessageMetricKind(kind string) string {
	switch kind {
	case "subscribe_single", "subscribe_multi", "subscribe_declared_view", "unsubscribe_single", "unsubscribe_multi", "call_reducer", "one_off_query", "declared_query", "unknown":
		return kind
	default:
		return "unknown"
	}
}

func protocolMessageMetricResult(result string) string {
	switch result {
	case "ok", "malformed", "permission_denied", "validation_error", "executor_rejected", "internal_error", "connection_closed":
		return result
	default:
		return "internal_error"
	}
}

func protocolBackpressureMetricDirection(direction string) string {
	switch direction {
	case "inbound", "outbound":
		return direction
	default:
		return "inbound"
	}
}

func executorCommandMetricKind(kind string) string {
	switch kind {
	case "call_reducer", "register_subscription_set", "unregister_subscription_set", "disconnect_client_subscriptions", "on_connect", "on_disconnect", "scheduler_fire", "unknown":
		return kind
	default:
		return "unknown"
	}
}

func executorCommandMetricResult(result string) string {
	switch result {
	case "ok", "user_error", "panic", "internal_error", "permission_denied", "rejected", "canceled":
		return result
	default:
		return "internal_error"
	}
}

func reducerMetricResult(result string) string {
	switch result {
	case "committed", "failed_user", "failed_panic", "failed_internal", "failed_permission":
		return result
	default:
		return "failed_internal"
	}
}

func subscriptionEvalMetricResult(result string) string {
	switch result {
	case "ok", "error":
		return result
	default:
		return "error"
	}
}

func subscriptionFanoutMetricReason(reason string) string {
	switch reason {
	case "buffer_full", "connection_closed", "encode_failed", "send_failed", "context_canceled", "unknown":
		return reason
	default:
		return "unknown"
	}
}

func subscriptionDroppedMetricReason(reason string) string {
	switch reason {
	case "buffer_full", "connection_closed", "fanout_failed", "unknown":
		return reason
	default:
		return "unknown"
	}
}

func (o *runtimeObservability) reducerMetricLabel(reducer string) string {
	if o != nil && o.config.Metrics.ReducerLabelMode == ReducerLabelModeAggregate {
		return "_all"
	}
	if strings.TrimSpace(reducer) == "" {
		return "unknown"
	}
	return reducer
}

func boolMetricValue(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

var runtimeMetricStates = [...]RuntimeState{
	RuntimeStateBuilt,
	RuntimeStateStarting,
	RuntimeStateReady,
	RuntimeStateClosing,
	RuntimeStateClosed,
	RuntimeStateFailed,
}

func (o *runtimeObservability) addCounter(name MetricName, labels MetricLabels, delta uint64) {
	if o == nil || o.metrics == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			o.recordSinkFailure("metrics", r)
		}
	}()
	o.metrics.AddCounter(name, o.metricLabels(labels), delta)
}

func (o *runtimeObservability) setGauge(name MetricName, labels MetricLabels, value float64) {
	if o == nil || o.metrics == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			o.recordSinkFailure("metrics", r)
		}
	}()
	o.metrics.SetGauge(name, o.metricLabels(labels), value)
}

func (o *runtimeObservability) observeHistogram(name MetricName, labels MetricLabels, value float64) {
	if o == nil || o.metrics == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			o.recordSinkFailure("metrics", r)
		}
	}()
	o.metrics.ObserveHistogram(name, o.metricLabels(labels), value)
}

func (o *runtimeObservability) traceSpan(name, component string, err error, attrs ...TraceAttr) {
	if o == nil || o.tracer == nil {
		return
	}
	_, span := o.startSpan(context.Background(), name, component, attrs...)
	if span == nil {
		return
	}
	span.AddEvent("shunter.span.complete", attrs...)
	span.End(err)
}

func (o *runtimeObservability) startSpan(ctx context.Context, name, component string, attrs ...TraceAttr) (context.Context, Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	if o == nil || o.tracer == nil {
		return ctx, nil
	}
	traceAttrs := []TraceAttr{
		{Key: "component", Value: component},
		{Key: "module", Value: o.moduleName},
		{Key: "runtime", Value: o.runtimeLabel},
	}
	traceAttrs = append(traceAttrs, attrs...)
	var span Span
	outCtx := ctx
	func() {
		defer func() {
			if r := recover(); r != nil {
				outCtx = ctx
				span = nil
				o.recordSinkFailure("tracer", r)
			}
		}()
		outCtx, span = o.tracer.StartSpan(ctx, name, traceAttrs...)
	}()
	if span == nil {
		return outCtx, nil
	}
	return outCtx, observedSpan{owner: o, span: span}
}

type observedSpan struct {
	owner *runtimeObservability
	span  Span
}

func (s observedSpan) AddEvent(name string, attrs ...TraceAttr) {
	if s.span == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil && s.owner != nil {
			s.owner.recordSinkFailure("tracer", r)
		}
	}()
	s.span.AddEvent(name, attrs...)
}

func (s observedSpan) End(err error) {
	if s.span == nil {
		return
	}
	if err != nil && s.owner != nil {
		err = errors.New(s.owner.redactError(err))
	}
	defer func() {
		if r := recover(); r != nil && s.owner != nil {
			s.owner.recordSinkFailure("tracer", r)
		}
	}()
	s.span.End(err)
}

func (o *runtimeObservability) recordSinkFailure(failedSink string, recovered any) {
	if o == nil || !o.sinkFailure.CompareAndSwap(false, true) {
		return
	}
	defer o.sinkFailure.Store(false)
	errText := o.redactErrorString(fmt.Sprint(recovered))
	if failedSink != "logger" && o.logger != nil {
		func() {
			defer func() { _ = recover() }()
			o.logger.LogAttrs(context.Background(), slog.LevelWarn, "observability.sink_failed",
				slog.String("component", "observability"),
				slog.String("event", "observability.sink_failed"),
				slog.String("module", o.moduleName),
				slog.String("runtime", o.runtimeLabel),
				slog.String("sink", failedSink),
				slog.String("error", errText),
			)
		}()
	}
	if failedSink != "metrics" && o.metrics != nil {
		func() {
			defer func() { _ = recover() }()
			o.metrics.AddCounter(MetricRuntimeErrorsTotal, o.metricLabels(MetricLabels{
				Component: "observability",
				Reason:    "observability_sink_failed",
			}), 1)
		}()
	}
}

func (o *runtimeObservability) redactError(err error) string {
	if err == nil {
		return ""
	}
	return o.redactErrorString(err.Error())
}

func (o *runtimeObservability) redactErrorString(raw string) string {
	return boundUTF8(redactSensitive(strings.ToValidUTF8(raw, "")), o.errorMessageMaxBytes())
}

func (o *runtimeObservability) debugSQLString(raw string) (string, bool) {
	if o == nil || !o.redaction.AllowRawSQLInDebugLogs {
		return "", false
	}
	return boundUTF8(strings.ToValidUTF8(raw, ""), o.errorMessageMaxBytes()), true
}

func (o *runtimeObservability) errorMessageMaxBytes() int {
	if o == nil || o.redaction.ErrorMessageMaxBytes <= 0 {
		return defaultObservabilityErrorMessageMaxBytes
	}
	return o.redaction.ErrorMessageMaxBytes
}

func boundUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultObservabilityErrorMessageMaxBytes
	}
	if len(s) <= maxBytes {
		return s
	}
	n := maxBytes
	for n > 0 && !utf8.ValidString(s[:n]) {
		n--
	}
	return s[:n]
}

func safeTraceError(result string, err error) error {
	if err != nil {
		return err
	}
	switch result {
	case "", "ok", "success", "committed":
		return nil
	default:
		return errors.New(result)
	}
}

func okErrorTraceResult(result string) string {
	if result == "ok" {
		return "ok"
	}
	return "error"
}

func reducerTraceName(reducer string) string {
	if strings.TrimSpace(reducer) == "" {
		return "unknown"
	}
	return reducer
}

var sensitiveRedactionKeys = [...]string{
	"authorization",
	"token",
	"access_token",
	"refresh_token",
	"signing_key",
	"args",
	"arg_bsatn",
	"row",
	"rows",
	"payload",
	"query",
	"query_string",
	"sql",
}

func redactSensitive(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); {
		if start, end, ok := matchJSONKeyValue(s, i); ok {
			b.WriteString(s[last:start])
			b.WriteString(`"[redacted]"`)
			i = end
			last = end
			continue
		}
		if start, end, ok := matchPlainKeyValue(s, i); ok {
			b.WriteString(s[last:start])
			b.WriteString("[redacted]")
			i = end
			last = end
			continue
		}
		if start, end, ok := matchBearerToken(s, i); ok {
			b.WriteString(s[last:start])
			b.WriteString("[redacted]")
			i = end
			last = end
			continue
		}
		_, width := utf8.DecodeRuneInString(s[i:])
		if width < 1 {
			width = 1
		}
		i += width
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func matchJSONKeyValue(s string, i int) (int, int, bool) {
	if s[i] != '"' || !keyBoundary(s, i) {
		return 0, 0, false
	}
	keyStart := i + 1
	keyEnd := strings.IndexByte(s[keyStart:], '"')
	if keyEnd < 0 {
		return 0, 0, false
	}
	keyEnd += keyStart
	if !isSensitiveKey(s[keyStart:keyEnd]) {
		return 0, 0, false
	}
	j := skipASCIIWhitespace(s, keyEnd+1)
	if j >= len(s) || s[j] != ':' {
		return 0, 0, false
	}
	valueStart := skipASCIIWhitespace(s, j+1)
	if valueStart > len(s) {
		return 0, 0, false
	}
	return valueStart, consumeJSONValue(s, valueStart), true
}

func matchPlainKeyValue(s string, i int) (int, int, bool) {
	if !keyBoundary(s, i) {
		return 0, 0, false
	}
	for _, key := range sensitiveRedactionKeys {
		if !hasASCIIFoldPrefix(s[i:], key) {
			continue
		}
		j := skipASCIIWhitespace(s, i+len(key))
		if j >= len(s) || (s[j] != '=' && s[j] != ':') {
			continue
		}
		valueStart := skipASCIIWhitespace(s, j+1)
		return valueStart, consumePlainValue(s, valueStart), true
	}
	return 0, 0, false
}

func matchBearerToken(s string, i int) (int, int, bool) {
	const prefix = "Bearer "
	if !hasASCIIFoldPrefix(s[i:], prefix) {
		return 0, 0, false
	}
	start := i + len(prefix)
	j := start
	for j < len(s) {
		switch s[j] {
		case ' ', '\t', '\n', '\r', ',', ';', '"', '\'':
			return start, j, true
		default:
			_, width := utf8.DecodeRuneInString(s[j:])
			if width < 1 {
				width = 1
			}
			j += width
		}
	}
	return start, j, true
}

func consumePlainValue(s string, start int) int {
	if start >= len(s) {
		return start
	}
	if s[start] == '"' || s[start] == '\'' {
		return consumeQuoted(s, start)
	}
	j := start
	for j < len(s) {
		switch s[j] {
		case ',', ';', '\n', '\r', '}', ']':
			return j
		default:
			_, width := utf8.DecodeRuneInString(s[j:])
			if width < 1 {
				width = 1
			}
			j += width
		}
	}
	return j
}

func consumeJSONValue(s string, start int) int {
	if start >= len(s) {
		return start
	}
	switch s[start] {
	case '"':
		return consumeQuoted(s, start)
	case '{':
		return consumeBalancedJSON(s, start, '{', '}')
	case '[':
		return consumeBalancedJSON(s, start, '[', ']')
	default:
		return consumePlainValue(s, start)
	}
}

func consumeQuoted(s string, start int) int {
	quote := s[start]
	escaped := false
	for i := start + 1; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		if s[i] == '\\' {
			escaped = true
			continue
		}
		if s[i] == quote {
			return i + 1
		}
	}
	return len(s)
}

func consumeBalancedJSON(s string, start int, open, close byte) int {
	depth := 0
	for i := start; i < len(s); {
		switch s[i] {
		case '"':
			i = consumeQuoted(s, i)
			continue
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
		_, width := utf8.DecodeRuneInString(s[i:])
		if width < 1 {
			width = 1
		}
		i += width
	}
	return len(s)
}

func skipASCIIWhitespace(s string, i int) int {
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

func keyBoundary(s string, i int) bool {
	return i == 0 || !isASCIINameByte(s[i-1])
}

func isASCIINameByte(b byte) bool {
	return b == '_' || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z') || ('0' <= b && b <= '9')
}

func isSensitiveKey(key string) bool {
	for _, sensitive := range sensitiveRedactionKeys {
		if strings.EqualFold(key, sensitive) {
			return true
		}
	}
	return false
}

func hasASCIIFoldPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}
