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
value is empty. Reducer labels MUST be:

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
| `reason` | string | Controlled enum matching metric `reason` values. |
| `duration_ms` | int64 | `time.Duration.Milliseconds()`. |
| `error` | string | Redacted and bounded by section 11. |
| `stack` | string | Panic events only; debug-gated by logger level. |

Fields not listed here MAY be added only when they are low-cardinality,
non-sensitive, and covered by section 11. Free-form maps MUST NOT be logged.

### 8.1 Required Log Events

| Event | Level | Component | Required additional fields |
|---|---|---|---|
| `runtime.build_failed` | Error | `runtime` | `error` |
| `runtime.start_failed` | Error | `runtime` | `error`, `duration_ms` |
| `runtime.ready` | Info | `runtime` | `state`, `ready`, `degraded`, `duration_ms` |
| `runtime.closed` | Info | `runtime` | `state`, `duration_ms` |
| `runtime.health_degraded` | Warn | `runtime` | `state`, `reason` |
| `recovery.completed` | Info or Warn | `commitlog` | `tx_id`, `duration_ms`, `damaged_tail_segments`, `skipped_snapshots` |
| `recovery.failed` | Error | `commitlog` | `error`, `duration_ms` |
| `durability.failed` | Error | `commitlog` | `error`, `reason`, `tx_id` when known |
| `executor.fatal` | Error | `executor` | `error`, `reason` |
| `executor.reducer_panic` | Error | `executor` | `reducer`, `error`, `tx_id` when known, `stack` when enabled |
| `executor.lifecycle_reducer_failed` | Warn | `executor` | `reducer`, `result`, `error` |
| `protocol.connection_opened` | Debug | `protocol` | `connection_id` |
| `protocol.connection_closed` | Debug | `protocol` | `connection_id`, `reason` |
| `protocol.protocol_error` | Warn | `protocol` | `kind`, `reason`, `error` |
| `protocol.auth_failed` | Warn | `protocol` | `reason`, `error` |
| `protocol.backpressure` | Warn | `protocol` | `direction`, `reason` |
| `subscription.eval_error` | Warn | `subscription` | `tx_id`, `error` |
| `subscription.fanout_error` | Warn | `subscription` | `reason`, `connection_id` when known, `error` |
| `subscription.client_dropped` | Warn | `subscription` | `reason`, `connection_id` when known |
| `store.snapshot_leaked` | Error | `store` | `reason` |

`recovery.completed` MUST be logged at Warn when either
`damaged_tail_segments > 0` or `skipped_snapshots > 0`; otherwise it MUST be
logged at Info.

Stack traces MUST appear only on panic events. Stack capture MAY be skipped
when `Logger.Enabled(ctx, slog.LevelDebug)` is false.

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
using the runtime's configured metrics handler. `HostDiagnosticsHandler` MUST
serve `/healthz`, `/readyz`, `/debug/shunter/host`, and `/metrics` when the
`metrics` argument is non-nil. It MUST NOT serve `/subscribe`.

### 10.1 Method and Content Rules

All JSON diagnostics endpoints MUST accept `GET` and `HEAD` only. Other methods
MUST return `405 Method Not Allowed` and an `Allow: GET, HEAD` header.

JSON diagnostics endpoints MUST return `Content-Type: application/json` for
`GET`. `HEAD` responses MUST use the same status code as `GET` and MUST NOT
write a body.

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

Trace attributes MUST follow the same redaction and cardinality rules as logs.
Raw SQL, reducer args, row payloads, tokens, authorization headers, signing
keys, request IDs, query IDs, and connection IDs MUST NOT be default trace
attributes. Connection/request/query IDs MAY be added only by a future explicit
debug tracing extension.

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
| Non-nil logger receives `runtime.ready` with required base fields and message equal to event | Structured logging schema |
| Startup failure logs `runtime.start_failed` and increments `runtime_errors_total{reason="start_failed"}` | Lifecycle error observability |
| Recovery success records `RecoveryHealth`, `recovery.completed`, `recovery_runs_total{result="success"}`, and recovered tx gauge | Recovery visibility |
| Recovery with damaged tail or skipped snapshots sets `Degraded=true` and logs `recovery.completed` at Warn | Degraded recovery rules |
| Runtime health reports executor fatal and durability fatal state without blocking | Health depth and cheap snapshot |
| Runtime health reports protocol disabled as not ready but not degraded | Protocol disabled rule |
| Host health aggregates ready/degraded across modules and returns detached slices | Multi-module diagnostics |
| Protocol connection open/close updates active connection gauge and accepted counter | Protocol gauges/counters |
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
| Prometheus adapter rejects const labels that duplicate reserved Shunter labels | Label safety |
| Metric recorder cannot receive labels outside `MetricLabels` by type | No free-form metric labels |
| Logs redact bearer tokens, reducer args, row payloads, SQL key/value fields, and signing keys | Redaction |
| Redacted error truncation respects UTF-8 boundaries and default 1024-byte limit | Error bound |
| Raw SQL appears only in debug logs when explicitly enabled | SQL redaction exception |
| `/healthz` absent when `MountHTTP=false` | Endpoint opt-in |
| `/healthz` returns 200 for ready, degraded, and not-ready nonfailed runtimes | Liveness status semantics |
| `/readyz` returns 200 only when ready and not degraded | Readiness status semantics |
| Failed, closing, and closed runtimes return 503 from `/healthz` and `/readyz` | Failed status semantics |
| JSON endpoints reject POST with 405 and `Allow: GET, HEAD` | HTTP method contract |
| HEAD diagnostics use GET status and write no body | HTTP HEAD contract |
| `/debug/shunter/runtime` returns `Runtime.Describe()` JSON without raw secrets | Runtime diagnostics payload |
| `HostDiagnosticsHandler` serves host health/debug endpoints and never serves `/subscribe` | Host handler separation |
| `/metrics` is mounted only when a metrics handler is configured | Metrics endpoint opt-in |
| Tracing disabled with a non-nil tracer records no spans | Tracing gate |
| Tracing enabled records required span names and redacts attributes | Tracing contract |
| Logger/metrics/tracer panics are recovered and do not change reducer results | Sink isolation |
