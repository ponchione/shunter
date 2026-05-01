package shunter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

const (
	defaultObservabilityRuntimeLabel         = "default"
	defaultObservabilityErrorMessageMaxBytes = 1024
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
	label := strings.TrimSpace(cfg.RuntimeLabel)
	if label == "" {
		label = defaultObservabilityRuntimeLabel
	}
	if !utf8.ValidString(label) {
		return ObservabilityConfig{}, fmt.Errorf("observability runtime label must be valid UTF-8")
	}
	if len(label) > 128 {
		return ObservabilityConfig{}, fmt.Errorf("observability runtime label must be at most 128 bytes")
	}
	for i := 0; i < len(label); i++ {
		if label[i] < 0x20 || label[i] == 0x7f {
			return ObservabilityConfig{}, fmt.Errorf("observability runtime label must not contain ASCII control characters")
		}
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
		err = errors.New(s.owner.redactErrorString(err.Error()))
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
	if !strings.HasPrefix(s[i:], prefix) {
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
