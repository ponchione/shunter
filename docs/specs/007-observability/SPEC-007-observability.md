# SPEC-007 -- Logging and Observability

**Status:** Draft implementation contract  
**Depends on:** SPEC-002 (commitlog recovery/durability diagnostics), SPEC-003
(executor lifecycle, reducer execution, scheduler), SPEC-004 (subscription
evaluation and fan-out), SPEC-005 (protocol connections and wire durations),
SPEC-006 (module/schema identity)  
**Depended on by:** hosted-runtime V3 observability work

---

## 1. Purpose and Scope

SPEC-007 defines Shunter's operator-facing logging and observability contract.
Observability MUST be additive: enabling, disabling, or failing an
observability sink MUST NOT change reducer semantics, durability semantics,
subscription behavior, protocol wire compatibility, schema validation, or
transaction ordering.

This spec covers:

- Structured runtime logging
- Runtime and host health snapshots
- HTTP health, diagnostics, and metrics endpoints
- Internal metrics recording and Prometheus export
- Optional tracing hooks
- Cardinality, redaction, and performance constraints
- Verification requirements for the observability surface

This spec does not cover:

- Hosted/cloud control-plane observability
- External alerting rules or dashboards
- Application-defined business metrics
- Client SDK telemetry
- Changes to protocol message formats beyond already-existing duration fields
- Changes to SPEC-006 module/schema introspection fields, including authored
  declaration SQL already exposed by `Runtime.Describe()`

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are normative.

## 2. Design Principles

Production code MUST NOT write to process-global logging directly. Runtime,
host, protocol, executor, commitlog, store, and subscription diagnostics MUST
flow through a runtime-scoped observability object or a package-scoped no-op
used only when no runtime exists yet.

Command-line user output is not runtime diagnostics. CLI commands MAY write
human-facing status, help, and error text to injected stdout/stderr writers,
but MUST NOT use the standard `log` package, process-global loggers, or global
stdout/stderr outside the CLI entry point.

Observability has a build-time phase and a runtime phase. `Build` MUST create
the normalized observability object early enough that build, validation,
bootstrap, and recovery failures can be logged and counted even when no
`Runtime` is returned. Build-time observations MUST use the configured runtime
label after normalization, the module name once a non-empty module name has
been validated, and module label `"unknown"` only when a module name is not
available because validation failed before identity was established.
If runtime label validation itself fails, build-failure observations MUST use
runtime label `"default"`.

Fresh bootstrap without existing durable files is still an observed recovery
attempt. It MUST be reported as a successful recovery run with recovered
transaction ID `0`, no selected durable snapshot, no damaged tail segments, and
no skipped snapshots.

Observability sinks MUST be isolated from runtime correctness:

- A logging, metrics, tracing, or HTTP diagnostics failure MUST NOT panic past
  the observability boundary.
- A sink call MUST NOT be made while holding locks that protect mutable runtime
  subsystem state, unless the call is to an in-memory no-op.
- A sink implementation supplied by the application MUST be treated as
  potentially slow or panic-prone. Shunter MUST recover sink panics and continue
  service.

Metrics MUST be low-cardinality by construction. Connection IDs, identities,
request IDs, query IDs, raw SQL text, reducer args, row payloads, and raw error
strings MUST NOT be metric labels.

Health snapshots MUST be cheap, detached, and synchronous. They MAY read atomic
counters and small protected state. They MUST NOT scan user tables, block on
durability, wait for goroutines, perform network I/O, or allocate output whose
size is unbounded by the number of hosted modules.

## 3. Resolved v1 Decisions

The following decisions close the former open questions for v1:

| Topic | Decision |
|---|---|
| Logger type | Public config uses `*slog.Logger` from the Go standard library. Shunter does not introduce a broad logging framework dependency. |
| Reducer metric labels | Reducer-name labels are enabled by default because reducer names are declaration-bounded. Operators MAY aggregate them to `_all` with `ReducerLabelModeAggregate`. |
| HTTP diagnostics mounting | Runtime diagnostics MAY be auto-mounted by `Runtime.HTTPHandler()` when configured. Host diagnostics are exposed by an explicit helper handler to avoid route-prefix ambiguity. |
| `/metrics` | Prometheus is delivered as an adapter that returns a metrics recorder and an `http.Handler`; Shunter root code MUST NOT import Prometheus packages. |
| Tracing | v1 defines a Shunter-owned tracing hook. OpenTelemetry is an adapter concern and MUST NOT be a root-package dependency. |
| `Degraded` | Degraded is a deterministic health boolean defined in section 9. It is not a replacement for readiness. |

## 4. Public Go API Additions

The root package `github.com/ponchione/shunter` gains these API shapes. The
implementation MAY place helpers in additional files, but exported names and
zero-value behavior MUST match this section.

```go
type Config struct {
    DataDir                 string
    ExecutorQueueCapacity   int
    DurabilityQueueCapacity int
    EnableProtocol          bool
    ListenAddr              string
    AuthMode                AuthMode

    AuthSigningKey []byte
    AuthAudiences  []string

    AnonymousTokenIssuer   string
    AnonymousTokenAudience string
    AnonymousTokenTTL      time.Duration

    Protocol      ProtocolConfig
    Observability ObservabilityConfig
}

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

type RedactionConfig struct {
    // ErrorMessageMaxBytes bounds redacted error text in logs and HTTP
    // diagnostics. Values <= 0 use 1024.
    ErrorMessageMaxBytes int

    // AllowRawSQLInDebugLogs permits raw SQL text only in debug-level logs.
    // It never permits raw SQL in metrics, traces, info/warn/error logs, or
    // HTTP health payloads.
    AllowRawSQLInDebugLogs bool
}

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

type ReducerLabelMode string

const (
    ReducerLabelModeName      ReducerLabelMode = "name"
    ReducerLabelModeAggregate ReducerLabelMode = "aggregate"
)

type DiagnosticsConfig struct {
    // MountHTTP controls whether Runtime.HTTPHandler() mounts the runtime
    // diagnostics endpoints from section 10 in addition to /subscribe.
    MountHTTP bool

    // MetricsHandler is mounted at /metrics only when MountHTTP is true and
    // MetricsHandler is non-nil. The Prometheus adapter supplies this handler.
    MetricsHandler http.Handler
}

type TracingConfig struct {
    // Enabled gates tracing hooks. False means no-op even when Tracer is
    // non-nil.
    Enabled bool

    // Tracer starts spans. Nil means no-op.
    Tracer Tracer
}
```

Zero-value `ObservabilityConfig` MUST produce no logs, metrics, or traces and
MUST NOT mount HTTP diagnostics. `Runtime.Health()`, `Runtime.Describe()`,
`Host.Health()`, and `Host.Describe()` remain available in-process regardless
of observability configuration.

`Config()` MUST continue to return a detached copy. The returned config MUST
not allow mutation of runtime-owned slices, loggers, handlers, metrics
recorders, or tracers beyond the pointer/interface values explicitly supplied by
the caller.

### 4.1 Observability Normalization and Validation

`Build` MUST normalize `ObservabilityConfig` deterministically:

- `RuntimeLabel` is `strings.TrimSpace(RuntimeLabel)`, then `"default"` when
  empty.
- Non-empty `RuntimeLabel` MUST be valid UTF-8, MUST NOT contain ASCII control
  characters, and MUST be at most 128 bytes after trimming.
- `Redaction.ErrorMessageMaxBytes <= 0` normalizes to `1024`.
- `Metrics.ReducerLabelMode == ""` normalizes to `ReducerLabelModeName`.

`Build` MUST reject invalid observability configuration before constructing a
runtime. Invalid configuration includes:

- `Metrics.ReducerLabelMode` other than `""`, `"name"`, or `"aggregate"`.
- Invalid `RuntimeLabel` as defined above.

`Metrics.Enabled == false` MUST make metrics a no-op even when
`Metrics.Recorder` is non-nil and even when `Diagnostics.MetricsHandler` is
configured. `Tracing.Enabled == false` MUST make tracing a no-op even when
`Tracing.Tracer` is non-nil.

Configuration validation failures are build failures for observability
purposes: when a logger or metrics recorder is configured and usable, Shunter
MUST emit `runtime.build_failed` and increment
`runtime_errors_total{reason="build_failed"}` before returning the error.

## 5. Metrics Recorder API

Shunter owns a small internal metrics model. Prometheus is an adapter, not the
internal contract.

```go
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

type MetricsRecorder interface {
    AddCounter(name MetricName, labels MetricLabels, delta uint64)
    SetGauge(name MetricName, labels MetricLabels, value float64)
    ObserveHistogram(name MetricName, labels MetricLabels, value float64)
}
```

`MetricsRecorder` implementations MUST be safe for concurrent use. Shunter MUST
call `AddCounter` only for counter metrics, `SetGauge` only for gauges, and
`ObserveHistogram` only for histograms. A recorder MUST ignore unknown metric
names rather than panic.

`MetricLabels` is intentionally a fixed struct. Shunter code MUST NOT add a
free-form label map.

The runtime MUST populate `Module` with `Runtime.ModuleName()` and `Runtime`
with `ObservabilityConfig.RuntimeLabel`, using `"default"` when the configured
value is empty. Build-time observations before a runtime is returned MUST use
the validated module name when available and `"unknown"` otherwise. Reducer
labels MUST be:

- the declared reducer name when `ReducerLabelMode` is empty or `"name"`
- `"_all"` when `ReducerLabelMode` is `"aggregate"`
- `"unknown"` only when Shunter observes a reducer path before resolving a
  declared reducer name

## 6. Metric Families

Prometheus family names are `<namespace>_<metric>`. The default namespace is
`shunter`, so the default public names below are exact.

Duration histograms MUST use these bucket boundaries in seconds:

```
0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05,
0.1, 0.25, 0.5, 1, 2.5, 5, 10
```

| Default name | Type | Labels | Buckets | Description |
|---|---|---|---|---|
| `shunter_runtime_ready` | Gauge | `module`, `runtime` | n/a | `1` iff `RuntimeHealth.Ready` is true, else `0`. |
| `shunter_runtime_state` | Gauge | `module`, `runtime`, `state` | n/a | One-hot lifecycle state gauge. Exactly one state label is `1` per runtime snapshot. |
| `shunter_runtime_degraded` | Gauge | `module`, `runtime` | n/a | `1` iff `RuntimeHealth.Degraded` is true, else `0`. |
| `shunter_runtime_errors_total` | Counter | `module`, `runtime`, `component`, `reason` | n/a | Runtime or subsystem errors after reason mapping. |
| `shunter_protocol_connections` | Gauge | `module`, `runtime` | n/a | Current active protocol connections. |
| `shunter_protocol_connections_total` | Counter | `module`, `runtime`, `result` | n/a | Accepted or rejected connection attempts. |
| `shunter_protocol_messages_total` | Counter | `module`, `runtime`, `kind`, `result` | n/a | Client messages decoded and handled by the protocol layer. |
| `shunter_protocol_backpressure_total` | Counter | `module`, `runtime`, `direction` | n/a | Inbound/outbound backpressure signals. |
| `shunter_executor_commands_total` | Counter | `module`, `runtime`, `kind`, `result` | n/a | Executor command terminal outcomes, including submit-time rejections. |
| `shunter_executor_command_duration_seconds` | Histogram | `module`, `runtime`, `kind`, `result` | default duration buckets | Time from executor dequeue to terminal command response. |
| `shunter_executor_inbox_depth` | Gauge | `module`, `runtime` | n/a | Current executor queue depth. |
| `shunter_executor_fatal` | Gauge | `module`, `runtime` | n/a | `1` iff executor fatal state is latched. |
| `shunter_reducer_calls_total` | Counter | `module`, `runtime`, `reducer`, `result` | n/a | Reducer call outcomes. |
| `shunter_reducer_duration_seconds` | Histogram | `module`, `runtime`, `reducer`, `result` | default duration buckets | Reducer handler wall-clock duration. |
| `shunter_durability_durable_tx_id` | Gauge | `module`, `runtime` | n/a | Latest durable transaction ID. |
| `shunter_durability_queue_depth` | Gauge | `module`, `runtime` | n/a | Current durability queue depth. |
| `shunter_durability_failures_total` | Counter | `module`, `runtime`, `reason` | n/a | Fatal durability failures. |
| `shunter_subscription_active` | Gauge | `module`, `runtime` | n/a | Active subscription count. |
| `shunter_subscription_eval_duration_seconds` | Histogram | `module`, `runtime`, `result` | default duration buckets | Subscription evaluation duration after a committed transaction. |
| `shunter_subscription_fanout_errors_total` | Counter | `module`, `runtime`, `reason` | n/a | Fan-out delivery failures. |
| `shunter_subscription_dropped_clients_total` | Counter | `module`, `runtime`, `reason` | n/a | Client drops initiated by subscription/fan-out backpressure. |
| `shunter_recovery_runs_total` | Counter | `module`, `runtime`, `result` | n/a | Recovery attempts at runtime build/open. |
| `shunter_recovery_recovered_tx_id` | Gauge | `module`, `runtime` | n/a | Recovered transaction horizon from the latest recovery. |
| `shunter_recovery_damaged_tail_segments` | Gauge | `module`, `runtime` | n/a | Damaged tail segment count from the latest recovery. |
| `shunter_recovery_skipped_snapshots_total` | Counter | `module`, `runtime`, `reason` | n/a | Skipped recovery snapshots. |

Allowed label values:

| Label | Allowed values |
|---|---|
| `component` | `runtime`, `protocol`, `executor`, `commitlog`, `store`, `subscription`, `observability` |
| `state` | `built`, `starting`, `ready`, `closing`, `closed`, `failed` |
| `direction` | `inbound`, `outbound` |
| protocol `kind` | `subscribe_single`, `subscribe_multi`, `subscribe_declared_view`, `unsubscribe_single`, `unsubscribe_multi`, `call_reducer`, `one_off_query`, `declared_query`, `unknown` |
| executor `kind` | `call_reducer`, `register_subscription_set`, `unregister_subscription_set`, `disconnect_client_subscriptions`, `on_connect`, `on_disconnect`, `scheduler_fire`, `unknown` |
| connection `result` | `accepted`, `rejected_not_ready`, `rejected_auth`, `rejected_upgrade`, `rejected_executor`, `rejected_internal` |
| protocol message `result` | `ok`, `malformed`, `permission_denied`, `validation_error`, `executor_rejected`, `internal_error`, `connection_closed` |
| executor command `result` | `ok`, `user_error`, `panic`, `internal_error`, `permission_denied`, `rejected`, `canceled` |
| reducer `result` | `committed`, `failed_user`, `failed_panic`, `failed_internal`, `failed_permission` |
| recovery `result` | `success`, `failed` |
| subscription eval `result` | `ok`, `error` |
| `reason` | one of the reason values in section 6.1 |

### 6.1 Metric Reason Mapping

Reason labels MUST come from sentinel/classification mapping, never from raw
error strings.

| Metric family | Allowed `reason` values |
|---|---|
| `runtime_errors_total` | `build_failed`, `start_failed`, `close_failed`, `panic`, `observability_sink_failed`, `unknown` |
| `durability_failures_total` | `open_failed`, `write_failed`, `sync_failed`, `segment_rotate_failed`, `close_failed`, `replay_failed`, `corrupt_segment`, `context_canceled`, `unknown` |
| `subscription_fanout_errors_total` | `buffer_full`, `connection_closed`, `encode_failed`, `send_failed`, `context_canceled`, `unknown` |
| `subscription_dropped_clients_total` | `buffer_full`, `connection_closed`, `fanout_failed`, `unknown` |
| `recovery_skipped_snapshots_total` | `schema_mismatch`, `read_failed`, `newer_than_log`, `damaged`, `unknown` |

Unknown or newly introduced conditions MUST map to `unknown` until this spec is
extended.

### 6.2 Metric Emission Timing

Metrics are best-effort observations. Shunter MUST recover panics from
`MetricsRecorder` calls. A recorder panic MUST NOT change the runtime operation
that was being observed. Shunter MUST use a recursion guard for
observability-failure observations so a panicking recorder cannot trigger an
unbounded loop while recording `observability_sink_failed`.

Gauge updates MUST reflect the latest known value. Counters MUST be incremented
exactly once per event described below. Histograms MUST be observed only for
completed operations with a defined duration; submit-time rejections that never
enter the executor inbox MUST NOT record command duration histograms.

Lifecycle and recovery metrics:

- After successful `Build`, Shunter MUST set `runtime_state` one-hot with
  `state="built"`, `runtime_ready=0`, and `runtime_degraded` from the recovery
  facts.
- On every runtime state transition, Shunter MUST set all `runtime_state`
  labels for that runtime so exactly one state value is `1` and the others are
  `0`.
- `runtime_ready` and `runtime_degraded` MUST be updated synchronously with
  lifecycle transitions and with any subsystem fatal/degraded condition that
  changes health.
- `runtime_errors_total{reason="build_failed"}` increments once for each
  failed `Build` call when observability config can be normalized far enough to
  reach a recorder.
- `runtime_errors_total{reason="start_failed"}` increments once for each
  failed `Start` call that transitions the runtime to `failed`.
- `runtime_errors_total{reason="close_failed"}` increments once for each
  `Close` call that returns a non-nil close error.
- `recovery_runs_total` increments once per recovery/bootstrap attempt during
  `Build`, with `result="success"` when a committed state is opened or freshly
  bootstrapped and `result="failed"` when recovery prevents `Build` from
  returning a runtime.
- On successful recovery/bootstrap, `recovery_recovered_tx_id` and
  `recovery_damaged_tail_segments` MUST be set from the final recovery report.
- `recovery_skipped_snapshots_total` increments once per skipped snapshot
  report, using the mapped skip reason. Reports with zero skipped snapshots
  MUST NOT increment the counter.

Protocol metrics:

- `protocol_connections_total{result="accepted"}` increments after a
  connection has passed auth/upgrade/admission, has been registered in the
  connection manager, and the identity token has been written successfully.
- Rejected connection attempts increment exactly one
  `protocol_connections_total` result: `rejected_not_ready` before protocol
  admission is available, `rejected_auth` for authentication/authorization
  failures, `rejected_upgrade` for invalid upgrade parameters or WebSocket
  handshake failure, `rejected_executor` for executor admission rejection, and
  `rejected_internal` for all other internal failures.
- `protocol_connections` MUST be incremented or set after connection manager
  add/remove so it equals the active manager count.
- `protocol_messages_total` increments once per decoded client message after
  its handler reaches a terminal outcome. Malformed frames that cannot be
  decoded increment with `kind="unknown"` unless the message kind was known
  before the error.
- `protocol_backpressure_total` increments when inbound dispatch pressure or
  outbound queue pressure causes a close, drop, or rejected enqueue.

Executor and reducer metrics:

- `executor_commands_total` increments once for every executor command terminal
  outcome, including submit-time rejection. Submit-time rejection uses
  `result="rejected"` unless the rejection maps more specifically to
  `permission_denied` or `canceled`.
- `executor_command_duration_seconds` observes time from executor dequeue to
  terminal command response for commands that were dequeued. It MUST NOT include
  time spent waiting in the inbox.
- `executor_inbox_depth` MUST be updated after successful enqueue and after
  dequeue. `executor_fatal` MUST be set to `1` when fatal state latches and
  MUST remain `1` until the runtime is discarded.
- `reducer_calls_total` increments once per `CallReducer` terminal outcome that
  reaches reducer command handling.
- `reducer_duration_seconds` observes only calls where a declared reducer
  handler was resolved and invoked. Its duration is the wall-clock time spent
  inside the reducer handler, excluding executor queue time, commit,
  durability enqueue, subscription evaluation, and fan-out.

Durability and subscription metrics:

- `durability_queue_depth` MUST be updated after enqueue and after durability
  worker dequeue. `durability_durable_tx_id` MUST be set whenever the durable
  transaction horizon advances.
- `durability_failures_total` increments once when a durability failure is
  classified as fatal or prevents startup/recovery from continuing.
- `subscription_active` MUST be set after subscription register, unregister,
  disconnect cleanup, and runtime close.
- `subscription_eval_duration_seconds` observes once per post-commit
  subscription evaluation. Evaluation errors use `result="error"`.
- `subscription_fanout_errors_total` increments once per fan-out delivery
  failure.
- `subscription_dropped_clients_total` increments once per client drop signal
  that Shunter enqueues or performs. If a non-blocking drop-signal channel is
  already full and the signal is discarded, Shunter MUST increment
  `subscription_dropped_clients_total{reason="buffer_full"}` before discarding
  when metrics are enabled.

## 7. Prometheus Adapter

Prometheus support MUST live outside the root package, in:

```
github.com/ponchione/shunter/observability/prometheus
```

The adapter package MAY import `github.com/prometheus/client_golang/prometheus`
and `github.com/prometheus/client_golang/prometheus/promhttp`. The root Shunter
package MUST NOT import those packages.

Normative adapter API:

```go
package prometheus

type Config struct {
    // Namespace prefixes metric family names. Empty means "shunter".
    Namespace string

    // Registerer receives collectors. Nil creates a private registry.
    // The adapter MUST NOT use prometheus.DefaultRegisterer unless the caller
    // passes it explicitly.
    Registerer prometheus.Registerer

    // Gatherer backs Handler. Nil uses the private registry created when
    // Registerer is nil, or the supplied Registerer when it also satisfies
    // prometheus.Gatherer. If Registerer is non-nil, Gatherer is nil, and the
    // Registerer does not satisfy prometheus.Gatherer, New MUST return an
    // error.
    Gatherer prometheus.Gatherer

    // ConstLabels are optional low-cardinality labels applied to every family.
    // They MUST NOT duplicate Shunter's reserved labels from section 6.
    ConstLabels prometheus.Labels
}

type Adapter struct {
    // unexported fields
}

func New(cfg Config) (*Adapter, error)
func (a *Adapter) Recorder() shunter.MetricsRecorder
func (a *Adapter) Handler() http.Handler
```

`Config.Namespace` MUST normalize empty to `"shunter"`. A non-empty namespace
MUST be a valid Prometheus metric-name prefix matching
`[a-zA-Z_:][a-zA-Z0-9_:]*`; otherwise `New` MUST return an error.

`Config.ConstLabels` MUST be copied by `New`. Const label names MUST be valid
Prometheus label names and MUST NOT duplicate Shunter's reserved labels:
`module`, `runtime`, `component`, `kind`, `state`, `result`, `reason`,
`direction`, or `reducer`. Any duplicate or invalid const label name MUST make
`New` return an error.

`New` MUST register all collectors exactly once with the chosen registerer. If
a collector registration fails, `New` MUST return an error and the adapter MUST
not be used. The adapter MUST NOT register collectors globally by default.

The handler returned by `Handler()` MUST expose the gatherer in Prometheus text
format. Callers mount it through `Config.Observability.Diagnostics`:

```go
adapter, err := prometheus.New(prometheus.Config{})
if err != nil {
    return err
}
cfg.Observability.Metrics.Enabled = true
cfg.Observability.Metrics.Recorder = adapter.Recorder()
cfg.Observability.Diagnostics.MountHTTP = true
cfg.Observability.Diagnostics.MetricsHandler = adapter.Handler()
```

## 8. Structured Logging

Shunter logging MUST use `log/slog` through `ObservabilityConfig.Logger`.
`nil` logger means no-op. The log record message MUST equal the stable event
name. Human-readable details belong in structured attributes, not in a varying
message string.

Production packages MUST NOT import the standard `log` package. Production
`log/slog` imports are reserved for the runtime observability owner; subsystem
packages emit observations through narrow observer interfaces so required
fields, redaction, level selection, and sink isolation remain centralized.

Every Shunter log record MUST include:

| Field | Type | Meaning |
|---|---|---|
| `component` | string | One of the component values from section 6. |
| `event` | string | Stable event name from section 8.1. |
| `module` | string | Runtime module name, or empty only before module identity exists. |
| `runtime` | string | Runtime label, defaulting to `"default"`. |

Optional common fields:

| Field | Type | Redaction/cardinality rule |
|---|---|---|
| `state` | string | Runtime state value. |
| `ready` | bool | Health/readiness value. |
| `degraded` | bool | Health/degraded value. |
| `tx_id` | uint64 | Log-safe. |
| `reducer` | string | Declared reducer name only. |
| `request_id` | uint32 | Log-safe; never a metric label. |
| `query_id` | uint32 | Log-safe; never a metric label. |
| `connection_id` | string | Lowercase hex string; debug/info only. |
| `kind` | string | Controlled enum matching metric `kind` values. |
| `result` | string | Controlled enum matching metric `result` values. |
| `reason` | string | Controlled enum matching metric reason values or health degraded reasons from section 9.3. |
| `duration_ms` | int64 | `time.Duration.Milliseconds()`. |
| `error` | string | Redacted and bounded by section 11. |
| `stack` | string | Panic events only; debug-gated by logger level. |
| `sink` | string | One of `logger`, `metrics`, `tracer`, or `diagnostics`; observability component only. |

Fields not listed here MAY be added only when they are low-cardinality,
non-sensitive, and covered by section 11. Free-form maps MUST NOT be logged.

### 8.1 Required Log Events

| Event | Level | Component | Required additional fields |
|---|---|---|---|
| `runtime.build_failed` | Error | `runtime` | `error` |
| `runtime.start_failed` | Error | `runtime` | `error`, `duration_ms` |
| `runtime.ready` | Info | `runtime` | `state`, `ready`, `degraded`, `duration_ms` |
| `runtime.close_failed` | Error | `runtime` | `error`, `duration_ms` |
| `runtime.closed` | Info | `runtime` | `state`, `duration_ms` |
| `runtime.health_degraded` | Warn | `runtime` | `state`, `reason` |
| `recovery.completed` | Info or Warn | `commitlog` | `tx_id`, `duration_ms`, `damaged_tail_segments`, `skipped_snapshots` |
| `recovery.failed` | Error | `commitlog` | `error`, `duration_ms` |
| `durability.failed` | Error | `commitlog` | `error`, `reason`, `tx_id` when known |
| `executor.fatal` | Error | `executor` | `error`, `reason` |
| `executor.reducer_panic` | Error | `executor` | `reducer`, `error`, `tx_id` when known, `stack` when enabled |
| `executor.lifecycle_reducer_failed` | Warn | `executor` | `reducer`, `result`, `error` |
| `protocol.connection_rejected` | Warn | `protocol` | `result`, `error` when known |
| `protocol.connection_opened` | Debug | `protocol` | `connection_id` |
| `protocol.connection_closed` | Debug | `protocol` | `connection_id`, `reason` |
| `protocol.protocol_error` | Warn | `protocol` | `kind`, `reason`, `error` |
| `protocol.auth_failed` | Warn | `protocol` | `reason`, `error` |
| `protocol.backpressure` | Warn | `protocol` | `direction`, `reason` |
| `subscription.eval_error` | Warn | `subscription` | `tx_id`, `error` |
| `subscription.fanout_error` | Warn | `subscription` | `reason`, `connection_id` when known, `error` |
| `subscription.client_dropped` | Warn | `subscription` | `reason`, `connection_id` when known |
| `store.snapshot_leaked` | Error | `store` | `reason` |
| `observability.sink_failed` | Warn | `observability` | `sink`, `error` |

`recovery.completed` MUST be logged at Warn when either
`damaged_tail_segments > 0` or `skipped_snapshots > 0`; otherwise it MUST be
logged at Info.

Stack traces MUST appear only on panic events. Stack capture MAY be skipped
when `Logger.Enabled(ctx, slog.LevelDebug)` is false.

`observability.sink_failed` MUST be emitted only through a sink that did not
itself fail for the current event. If every configured sink for the event has
failed, Shunter MUST drop the observability-failure record rather than retry
recursively.

## 9. Health and Diagnostics API

`Runtime.Health()` expands into the following detached snapshot. The exact field
names and JSON tags are normative.

```go
type RuntimeHealth struct {
    State     RuntimeState `json:"state"`
    Ready     bool         `json:"ready"`
    Degraded  bool         `json:"degraded"`
    LastError string       `json:"last_error,omitempty"`

    Executor      ExecutorHealth      `json:"executor"`
    Durability    DurabilityHealth    `json:"durability"`
    Protocol      ProtocolHealth      `json:"protocol"`
    Subscriptions SubscriptionHealth  `json:"subscriptions"`
    Recovery      RecoveryHealth      `json:"recovery"`
}

type ExecutorHealth struct {
    Started        bool   `json:"started"`
    AdmissionReady bool   `json:"admission_ready"`
    InboxDepth     int    `json:"inbox_depth"`
    InboxCapacity  int    `json:"inbox_capacity"`
    Fatal          bool   `json:"fatal"`
    FatalError     string `json:"fatal_error,omitempty"`
}

type DurabilityHealth struct {
    Started       bool       `json:"started"`
    DurableTxID   types.TxID `json:"durable_tx_id"`
    QueueDepth    int        `json:"queue_depth"`
    QueueCapacity int        `json:"queue_capacity"`
    Fatal         bool       `json:"fatal"`
    FatalError    string     `json:"fatal_error,omitempty"`
}

type ProtocolHealth struct {
    Enabled             bool   `json:"enabled"`
    Ready               bool   `json:"ready"`
    ActiveConnections   int    `json:"active_connections"`
    AcceptedConnections uint64 `json:"accepted_connections"`
    RejectedConnections uint64 `json:"rejected_connections"`
    LastError           string `json:"last_error,omitempty"`
}

type SubscriptionHealth struct {
    Started             bool   `json:"started"`
    ActiveSubscriptions int    `json:"active_subscriptions"`
    DroppedClients      uint64 `json:"dropped_clients"`
    FanoutQueueDepth    int    `json:"fanout_queue_depth"`
    FanoutQueueCapacity int    `json:"fanout_queue_capacity"`
    FanoutFatal         bool   `json:"fanout_fatal"`
    FanoutFatalError    string `json:"fanout_fatal_error,omitempty"`
}

type RecoveryHealth struct {
    Ran                  bool       `json:"ran"`
    Succeeded            bool       `json:"succeeded"`
    HasSelectedSnapshot  bool       `json:"has_selected_snapshot"`
    SelectedSnapshotTxID types.TxID `json:"selected_snapshot_tx_id"`
    HasDurableLog        bool       `json:"has_durable_log"`
    DurableLogHorizon    types.TxID `json:"durable_log_horizon"`
    ReplayedFromTxID     types.TxID `json:"replayed_from_tx_id"`
    ReplayedToTxID       types.TxID `json:"replayed_to_tx_id"`
    RecoveredTxID        types.TxID `json:"recovered_tx_id"`
    DamagedTailSegments  int        `json:"damaged_tail_segments"`
    SkippedSnapshots     int        `json:"skipped_snapshots"`
    LastError            string     `json:"last_error,omitempty"`
}
```

`Host.Health()` expands into an aggregate plus detached per-module health:

```go
type HostHealth struct {
    Ready    bool               `json:"ready"`
    Degraded bool               `json:"degraded"`
    Modules  []HostModuleHealth `json:"modules"`
}

type HostModuleHealth struct {
    Name        string        `json:"name"`
    RoutePrefix string        `json:"route_prefix"`
    Health      RuntimeHealth `json:"health"`
}
```

`Runtime.Describe()` MUST include the expanded `RuntimeHealth` without aliasing
runtime-owned mutable state. `Host.Describe()` MUST include the expanded
`HostHealth` indirectly through each `RuntimeDescription`.

### 9.1 Health Field Semantics

`RuntimeHealth.Ready` MUST be true only when `RuntimeStateReady` is active,
runtime-owned workers are running, and external reducer/protocol admission can
make forward progress.

`ExecutorHealth.AdmissionReady` MUST be false when the runtime is not ready,
the executor is nil, executor fatal state is latched, or the executor can no
longer accept externally submitted commands.

`ProtocolHealth.Enabled` MUST reflect `Config.EnableProtocol`.
`ProtocolHealth.Ready` MUST be true only when protocol is enabled, the runtime
is ready, and the protocol graph is initialized. If protocol is disabled,
`ProtocolHealth.Ready` MUST be false and this alone MUST NOT degrade the
runtime.

`RecoveryHealth.Ran` MUST be true after the runtime has attempted to open or
bootstrap durable state during `Build`. Fresh bootstrap without durable files is
a successful recovery run with zero recovered transaction ID.

All `LastError` and `FatalError` strings MUST be redacted and bounded by
section 11.

Capacity fields MUST report the normalized configured capacity even when the
underlying channel or worker has not been allocated yet or has already been
torn down. Depth fields MUST report `0` when the underlying queue is absent.

Subsystem `Started` fields MUST be false before `Start` completes and after
`Close` tears the subsystem down. They MUST also be false when startup failed
before the subsystem was created. Fatal booleans and fatal error strings are
latched facts: once a subsystem fatal condition is observed, health snapshots
MUST continue to report it until the runtime is discarded, even if `Close`
later tears down subsystem pointers.

`DurabilityHealth.DurableTxID` MUST be the latest known durable horizon. Before
the durability worker starts, it MUST equal `RecoveryHealth.RecoveredTxID`.
After a successful `Close`, it MUST equal the final durable transaction ID
reported by the durability worker close path when known, otherwise the last
known durable horizon.

`ProtocolHealth.ActiveConnections` MUST be `0` when the connection manager is
absent. Accepted and rejected connection counters MUST be retained across
connection close and runtime close. If protocol is disabled,
`ProtocolHealth.Enabled=false`, `ProtocolHealth.Ready=false`, and
`ProtocolHealth.ActiveConnections=0`.

`SubscriptionHealth.ActiveSubscriptions` MUST be the number of active
client-visible subscription sets, not the number of deduplicated internal query
states. It MUST be `0` before the subscription manager starts and after runtime
close. `DroppedClients` MUST be a retained cumulative count.

`Host.Health()` on a nil host or a host with zero modules MUST return
`Ready=false`, `Degraded=true`, and a non-nil empty `Modules` slice.

### 9.2 Degraded Rules

`RuntimeHealth.Degraded` MUST be true when any of these conditions hold:

- `State == RuntimeStateFailed`
- `Executor.Fatal == true`
- `Durability.Fatal == true`
- `Subscriptions.FanoutFatal == true`
- `Recovery.Succeeded == true` and `Recovery.DamagedTailSegments > 0`
- `Recovery.Succeeded == true` and `Recovery.SkippedSnapshots > 0`
- `Ready == true`, `Protocol.Enabled == true`, and `Protocol.Ready == false`

`RuntimeHealth.Degraded` MUST be false for `RuntimeStateBuilt`,
`RuntimeStateStarting`, `RuntimeStateClosing`, and `RuntimeStateClosed` unless
one of the subsystem conditions above is true. Not-ready is not automatically
degraded.

`HostHealth.Ready` MUST be true only when the host has at least one module and
every module health has `Ready == true`.

`HostHealth.Degraded` MUST be true when any module health has
`Degraded == true`, or when the host has zero modules.

### 9.3 Degraded Reason Selection

When `RuntimeHealth.Degraded` is true, Shunter MUST classify the primary
degraded reason using this priority order:

1. `runtime_failed` when `State == RuntimeStateFailed`
2. `executor_fatal` when `Executor.Fatal == true`
3. `durability_fatal` when `Durability.Fatal == true`
4. `fanout_fatal` when `Subscriptions.FanoutFatal == true`
5. `recovery_damaged_tail` when recovery succeeded with damaged tail segments
6. `recovery_skipped_snapshot` when recovery succeeded with skipped snapshots
7. `protocol_not_ready` when ready protocol-enabled runtime health has
   `Protocol.Ready == false`

`runtime.health_degraded` MUST use this primary reason. If multiple degraded
conditions hold, lower-priority reasons MAY be recorded as additional
low-cardinality fields only after this spec defines their field names; v1 logs
and metrics MUST NOT invent free-form reason lists.

## 10. HTTP Diagnostics

`Runtime.HTTPHandler()` MUST continue to serve `/subscribe` exactly as defined
by SPEC-005. When `Config.Observability.Diagnostics.MountHTTP` is false, the
runtime handler MUST NOT mount SPEC-007 endpoints.

When `MountHTTP` is true, `Runtime.HTTPHandler()` MUST additionally mount:

- `/healthz`
- `/readyz`
- `/debug/shunter/runtime`
- `/metrics` only when `Diagnostics.MetricsHandler != nil`

Host-level diagnostics are explicit to avoid ambiguity with module route
prefixes:

```go
func RuntimeDiagnosticsHandler(r *Runtime) http.Handler
func HostDiagnosticsHandler(h *Host, metrics http.Handler) http.Handler
```

`RuntimeDiagnosticsHandler` MUST serve the same runtime endpoints listed above,
using the runtime's configured metrics handler. It MUST serve those diagnostics
endpoints regardless of the runtime's `Diagnostics.MountHTTP` setting.
`HostDiagnosticsHandler` MUST serve `/healthz`, `/readyz`,
`/debug/shunter/host`, and `/metrics` when the `metrics` argument is non-nil.
It MUST NOT serve `/subscribe`.

### 10.1 Method and Content Rules

All JSON diagnostics endpoints MUST accept `GET` and `HEAD` only. Other methods
MUST return `405 Method Not Allowed` and an `Allow: GET, HEAD` header.

JSON diagnostics endpoints MUST return `Content-Type: application/json` for
`GET`. They SHOULD return `Cache-Control: no-store`. `HEAD` responses MUST use
the same status code and headers as `GET` and MUST NOT write a body.

`/metrics` method and content behavior is delegated to the configured metrics
handler.

### 10.2 Status Codes

Runtime status classification:

| Classification | Condition |
|---|---|
| `failed` | state is `failed`, `closing`, or `closed`, or any fatal subsystem flag is true |
| `degraded` | classification is not `failed` and `RuntimeHealth.Degraded` is true |
| `ok` | classification is not `failed`/`degraded` and `RuntimeHealth.Ready` is true |
| `not_ready` | none of the above |

Host classification uses the same strings:

- `failed` when any module would classify as `failed`, or the host has zero
  modules
- `degraded` when no module failed and `HostHealth.Degraded` is true
- `ok` when `HostHealth.Ready` is true and `HostHealth.Degraded` is false
- `not_ready` otherwise

Status codes:

| Endpoint | `ok` | `degraded` | `not_ready` | `failed` |
|---|---:|---:|---:|---:|
| `/healthz` | 200 | 200 | 200 | 503 |
| `/readyz` | 200 | 503 | 503 | 503 |
| `/debug/shunter/runtime` | 200 | 200 | 200 | 200 |
| `/debug/shunter/host` | 200 | 200 | 200 | 200 |

Debug endpoints MUST return 500 only if JSON encoding fails after the snapshot
has been built.

### 10.3 Payloads

Runtime health endpoints MUST return:

```json
{
  "status": "ok",
  "runtime": {
    "state": "ready",
    "ready": true,
    "degraded": false,
    "executor": {},
    "durability": {},
    "protocol": {},
    "subscriptions": {},
    "recovery": {}
  }
}
```

The `runtime` object MUST be a full `RuntimeHealth` JSON object. Empty
subsystem objects in the example are placeholders only.

Host health endpoints MUST return:

```json
{
  "status": "ok",
  "host": {
    "ready": true,
    "degraded": false,
    "modules": []
  }
}
```

`/debug/shunter/runtime` MUST return the JSON encoding of
`Runtime.Describe()`. `/debug/shunter/host` MUST return the JSON encoding of
`Host.Describe()`.

Health and readiness payloads MUST NOT include client raw SQL, reducer args,
row payloads, tokens, authorization headers, signing keys, or raw unbounded
error strings. Debug payloads MAY include authored declaration SQL that
`Runtime.Describe()` and `Host.Describe()` already expose under SPEC-006; they
MUST NOT include client raw SQL/query strings, reducer args, row payloads,
tokens, authorization headers, signing keys, or raw unbounded error strings.

### 10.4 Handler Edge Rules

Diagnostics handlers MUST match only the exact paths named in this section.
Query strings are ignored for routing. Subpaths and trailing-slash variants
such as `/healthz/`, `/readyz/`, `/debug/shunter/runtime/`, and
`/debug/shunter/host/` MUST return `404 Not Found` unless a caller wraps the
handler with its own router that rewrites paths before they reach Shunter.

When `Runtime.HTTPHandler()` is used with `Diagnostics.MountHTTP=false`, the
SPEC-007 endpoints MUST be absent and MUST return `404 Not Found`; `/subscribe`
behavior remains governed by SPEC-005.

`RuntimeDiagnosticsHandler(nil)` MUST return runtime health/readiness payloads
with classification `failed`, status `503` for `/healthz` and `/readyz`, and a
failed runtime health object whose `LastError` is redacted and bounded. Its
debug endpoint MUST return a zero runtime description plus the failed health
object with status `200`, unless JSON encoding fails.

`HostDiagnosticsHandler(nil, metrics)` MUST behave like an empty host:
`/healthz` and `/readyz` classify as `failed`, host health has `Ready=false`,
`Degraded=true`, and `Modules=[]`, and `/debug/shunter/host` returns that empty
host description with status `200`.

Diagnostics JSON encoding MUST be deterministic for tests: structs are encoded
with Go's standard `encoding/json` field order, nil slices in public payloads
that represent module lists MUST be emitted as `[]`, and omitted fields MUST
follow the JSON tags in section 9.

## 11. Redaction and Cardinality

The following data MUST NOT appear in logs, metrics, traces, or HTTP diagnostic
payloads unless explicitly allowed below:

- Authorization headers, bearer tokens, JWTs, anonymous tokens, signing keys,
  and auth audience secrets
- Reducer argument bytes, BSATN payloads, and reducer return bytes
- Row payloads, row values, table snapshots, and transaction row bodies
- Client-supplied raw SQL text and raw query strings
- Raw error strings before redaction
- Caller identity bytes; use a future stable hash field only if specified

Authored query/view SQL already exposed by `Runtime.Describe()` is governed by
SPEC-006 and MAY remain in `/debug/shunter/runtime` and `/debug/shunter/host`
payloads. It MUST NOT be used as a metric label or default trace attribute, and
it MUST NOT be logged outside the debug SQL exception below.

Client-supplied raw SQL MAY appear only in debug-level logs when
`RedactionConfig.AllowRawSQLInDebugLogs` is true. Raw SQL MUST NOT appear in
metrics, traces, health payloads, readiness payloads, or non-debug logs.

Reducer args, row payloads, tokens, authorization headers, and signing keys
MUST NEVER be logged, traced, exposed in diagnostics, or used as labels even
when debug logging is enabled.

Error strings MUST be redacted before output. Redaction MUST:

1. Replace case-insensitive key/value fields named `authorization`, `token`,
   `access_token`, `refresh_token`, `signing_key`, `args`, `arg_bsatn`, `row`,
   `rows`, `payload`, `query`, `query_string`, or `sql` with `[redacted]` when
   the key is followed by `=`, `:`, or a JSON string colon.
2. Replace bearer-token values following `Bearer ` with `[redacted]`.
3. Truncate the resulting UTF-8 string to `ErrorMessageMaxBytes`, defaulting to
   1024 bytes when the configured value is `<= 0`.
4. Preserve valid UTF-8 by truncating only at rune boundaries.

### 11.1 Redaction Boundary Rules

Redaction MUST first convert invalid UTF-8 to valid UTF-8 with invalid byte
sequences removed or replaced. It MUST then apply key/value and bearer-token
redaction before truncation.

Key matching MUST be case-insensitive and token-bounded. A redacted key must be
at the beginning of the string or preceded by a byte that is not an ASCII
letter, digit, or underscore. The key may be followed by ASCII whitespace and
then by one of:

- `=`
- `:`
- a JSON string colon, as in `"token": "value"`

For JSON key matches, the replacement MUST preserve JSON shape when possible by
replacing the whole JSON value with the JSON string `"[redacted]"`;
`"token":"abc"` becomes `"token":"[redacted]"` and `"row":{"id":1}` becomes
`"row":"[redacted]"`. For quoted non-JSON text values, the replacement runs
through the matching quote. For unquoted values, the replacement runs until the
first comma, semicolon, newline, carriage return, closing brace/bracket, or end
of string. This intentionally redacts multi-word SQL fragments such as
`sql=select * from t`.

Bearer redaction MUST match `Bearer ` case-sensitively and replace the token
bytes following that prefix through the first ASCII whitespace, comma,
semicolon, quote, apostrophe, or end of string. The prefix is preserved:
`Bearer abc` becomes `Bearer [redacted]`.

When raw SQL is allowed in debug logs, Shunter MUST log it in a dedicated
structured field rather than interpolating it into the message. That field MUST
be valid UTF-8 and bounded by `ErrorMessageMaxBytes` after normalization,
defaulting to 1024 bytes.

Examples that MUST hold:

| Input | Output |
|---|---|
| `authorization=Bearer abc.def` | `authorization=[redacted]` |
| `failed: Bearer abc.def` | `failed: Bearer [redacted]` |
| `{"token":"abc","row":{"id":1}}` | `{"token":"[redacted]","row":"[redacted]"}` |
| `sql=select * from users where token='abc'; detail` | `sql=[redacted]; detail` |
| `signing_key: secret` | `signing_key: [redacted]` |

Metrics MUST use only the labels listed in section 6. Logs and traces MAY carry
high-cardinality IDs such as `request_id`, `query_id`, and `connection_id`, but
those fields MUST NOT be promoted to metrics.

## 12. Tracing

Tracing is a Shunter-owned hook surface. v1 MUST NOT require OpenTelemetry in
the root package.

```go
type TraceAttr struct {
    Key   string
    Value any
}

type Tracer interface {
    StartSpan(ctx context.Context, name string, attrs ...TraceAttr) (context.Context, Span)
}

type Span interface {
    AddEvent(name string, attrs ...TraceAttr)
    End(err error)
}
```

Tracer and Span implementations MUST be safe for concurrent use according to
their own documentation. Shunter MUST recover tracer/span panics and continue
service.

Required span names:

- `shunter.runtime.start`
- `shunter.recovery.open`
- `shunter.protocol.message`
- `shunter.reducer.call`
- `shunter.store.commit`
- `shunter.durability.batch`
- `shunter.subscription.eval`
- `shunter.subscription.fanout`
- `shunter.query.one_off`
- `shunter.subscription.register`
- `shunter.subscription.unregister`

Every span MUST include these default attributes:

| Attribute | Meaning |
|---|---|
| `component` | Component value from section 6. |
| `module` | Runtime module name, or `"unknown"` during build-time failure before identity exists. |
| `runtime` | Normalized runtime label. |

Additional required attributes:

| Span | Required additional attributes |
|---|---|
| `shunter.runtime.start` | `state` when known |
| `shunter.recovery.open` | `result`, `tx_id` when successful |
| `shunter.protocol.message` | `kind`, `result` |
| `shunter.reducer.call` | `reducer`, `result` |
| `shunter.store.commit` | `tx_id`, `result` |
| `shunter.durability.batch` | `tx_id`, `result` |
| `shunter.subscription.eval` | `tx_id`, `result` |
| `shunter.subscription.fanout` | `result`, `reason` when failed |
| `shunter.query.one_off` | `result` |
| `shunter.subscription.register` | `result` |
| `shunter.subscription.unregister` | `result` |

Trace attributes MUST follow the same redaction and cardinality rules as logs.
Raw SQL, reducer args, row payloads, tokens, authorization headers, signing
keys, request IDs, query IDs, and connection IDs MUST NOT be default trace
attributes. Connection/request/query IDs MAY be added only by a future explicit
debug tracing extension.

`StartSpan` panics MUST be recovered and treated as if tracing were disabled for
that operation. If `StartSpan` returns a nil `Span`, Shunter MUST skip
`AddEvent` and `End` for that span. Shunter MUST recover panics from
`Span.AddEvent` and `Span.End`.

When ending a span with an error, Shunter MUST NOT pass an error whose
`Error()` string contains unredacted or unbounded sensitive data. It MUST pass
nil on success, and on failure it MUST pass either a redacted bounded wrapper
error or an error value whose string is known to already satisfy section 11.

## 13. Implementation Sequencing

1. Add the no-op observability config, redaction helpers, metrics recorder,
   tracing interfaces, and internal sink isolation.
2. Persist `commitlog.RecoveryReport` facts on `Runtime` build/startup for
   health, logs, and metrics.
3. Expand `RuntimeHealth`, `HostHealth`, `RuntimeDescription`, and
   `HostDescription` to the exact shapes in section 9.
4. Replace production `log.Printf` or process-global logging with runtime
   `slog` events from section 8.
5. Instrument runtime lifecycle, recovery, durability, executor, reducer,
   protocol, subscription, and fan-out metrics from section 6.
6. Add the Prometheus adapter in `observability/prometheus`.
7. Add optional runtime HTTP diagnostics mounting and explicit host diagnostics
   helpers from section 10.
8. Add tracing hooks and no-op defaults from section 12.
9. Add the verification tests from section 14.

Each implementation slice MUST keep zero-value observability no-op and MUST run
the relevant Go tests before claiming completion.

## 14. Verification

| Test | What it verifies |
|---|---|
| Zero `ObservabilityConfig` build/start/call/close succeeds with no panics | No-op defaults and sink isolation |
| Build validation failure emits `runtime.build_failed` and `runtime_errors_total{reason="build_failed"}` when sinks are configured | Build-time observability before a runtime exists |
| Fresh bootstrap records successful recovery with recovered tx `0` and no damage/skips | Bootstrap is an observed recovery run |
| Invalid `RuntimeLabel` or `ReducerLabelMode` is rejected before runtime construction | Observability config validation |
| Non-nil logger receives `runtime.ready` with required base fields and message equal to event | Structured logging schema |
| Startup failure logs `runtime.start_failed` and increments `runtime_errors_total{reason="start_failed"}` | Lifecycle error observability |
| Close failure logs `runtime.close_failed` and increments `runtime_errors_total{reason="close_failed"}` | Close-path observability |
| Recovery success records `RecoveryHealth`, `recovery.completed`, `recovery_runs_total{result="success"}`, and recovered tx gauge | Recovery visibility |
| Recovery with damaged tail or skipped snapshots sets `Degraded=true` and logs `recovery.completed` at Warn | Degraded recovery rules |
| Multiple degraded conditions choose the section 9.3 primary reason deterministically | Degraded reason priority |
| Health before start and after close reports configured capacities, zero absent depths, retained counters, and latched fatal facts | Health edge semantics |
| Runtime health reports executor fatal and durability fatal state without blocking | Health depth and cheap snapshot |
| Runtime health reports protocol disabled as not ready but not degraded | Protocol disabled rule |
| Nil or empty host health returns `Ready=false`, `Degraded=true`, and `Modules=[]` | Host edge semantics |
| Host health aggregates ready/degraded across modules and returns detached slices | Multi-module diagnostics |
| Runtime state gauges are one-hot after build, start, failure, closing, and closed transitions | Gauge determinism |
| Metric recorder panic is recovered and does not recursively emit sink-failure observations | Metrics sink isolation |
| Protocol connection open/close updates active connection gauge and accepted counter | Protocol gauges/counters |
| Protocol connection rejection maps exactly one rejection result and logs `protocol.connection_rejected` | Connection rejection classification |
| Protocol auth failure increments rejected connection counter with `rejected_auth` and logs redacted error | Protocol auth diagnostics |
| Protocol malformed message increments `protocol_messages_total{result="malformed"}` with kind `unknown` or decoded kind | Message classification |
| Reducer committed/user error/panic/internal/permission outcomes increment distinct reducer result labels | Reducer result mapping |
| Reducer-name metric labels default to the declared reducer name | Default reducer labels |
| `ReducerLabelModeAggregate` emits reducer label `_all` | Cardinality control |
| Executor submit rejection increments `executor_commands_total{result="rejected"}` | Queue/admission visibility |
| Executor command duration histogram uses exact default buckets | Histogram bucket contract |
| Subscription eval error logs `subscription.eval_error` and increments eval error metric | Eval diagnostics |
| Fan-out buffer-full logs `protocol.backpressure` or `subscription.client_dropped` and increments drop metric with `buffer_full` | Backpressure visibility |
| Prometheus adapter with nil registerer uses a private registry and does not pollute globals | No global Prometheus registration |
| Prometheus adapter with custom registry registers all families with default namespace `shunter` | Metric family contract |
| Prometheus adapter rejects invalid namespace and invalid const-label names | Adapter validation |
| Prometheus adapter rejects const labels that duplicate reserved Shunter labels | Label safety |
| Metric recorder cannot receive labels outside `MetricLabels` by type | No free-form metric labels |
| Logs redact bearer tokens, reducer args, row payloads, SQL key/value fields, and signing keys | Redaction |
| JSON-shaped redaction replaces sensitive object/string values with valid `[redacted]` JSON strings | Redaction boundary rules |
| Redacted error truncation respects UTF-8 boundaries and default 1024-byte limit | Error bound |
| Raw SQL appears only in debug logs when explicitly enabled | SQL redaction exception |
| Debug raw SQL field is UTF-8 normalized and bounded by `ErrorMessageMaxBytes` | Debug SQL bound |
| `/healthz` absent when `MountHTTP=false` | Endpoint opt-in |
| `RuntimeDiagnosticsHandler` serves diagnostics even when `MountHTTP=false` | Explicit handler behavior |
| `/healthz` returns 200 for ready, degraded, and not-ready nonfailed runtimes | Liveness status semantics |
| `/readyz` returns 200 only when ready and not degraded | Readiness status semantics |
| Failed, closing, and closed runtimes return 503 from `/healthz` and `/readyz` | Failed status semantics |
| JSON endpoints reject POST with 405 and `Allow: GET, HEAD` | HTTP method contract |
| HEAD diagnostics use GET status and write no body | HTTP HEAD contract |
| Diagnostics handlers reject trailing-slash and subpath variants with 404 | Exact path contract |
| Nil runtime/host diagnostics return deterministic failed/empty payloads | Handler nil-input behavior |
| `/debug/shunter/runtime` returns `Runtime.Describe()` JSON without raw secrets | Runtime diagnostics payload |
| `HostDiagnosticsHandler` serves host health/debug endpoints and never serves `/subscribe` | Host handler separation |
| `/metrics` is mounted only when a metrics handler is configured | Metrics endpoint opt-in |
| Tracing disabled with a non-nil tracer records no spans | Tracing gate |
| Tracing enabled records required span names, required default attributes, and redacts attributes | Tracing contract |
| Tracing `StartSpan`, `AddEvent`, and `End` panics are recovered and nil spans are skipped | Tracing sink isolation |
| Logger/metrics/tracer panics are recovered and do not change reducer results | Sink isolation |
