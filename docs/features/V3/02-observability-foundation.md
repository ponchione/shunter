# V3 Task 02: Observability Foundation

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Task 01 stack reconfirmation

Objective: add SPEC-007 public observability API shapes, deterministic
normalization, redaction helpers, metrics/tracing interfaces, and internal sink
isolation without changing runtime behavior.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` sections 2, 4, 5,
  8, 11, and 12
- `docs/features/V3/01-stack-prerequisites.md`

Inspect:

```sh
rtk go doc . Config
rtk go doc . Runtime.Config
rtk go doc . Build
rtk go doc . Runtime
rtk grep -n 'normalize|Config\\(|clone|copy|Validate|RuntimeLabel|Permission' *.go
```

## Target Behavior

Add the root package API shapes from SPEC-007 sections 4, 5, and 12:

- `ObservabilityConfig`
- `RedactionConfig`
- `MetricsConfig`
- `ReducerLabelMode`
- `DiagnosticsConfig`
- `TracingConfig`
- `MetricName`
- `MetricLabels`
- `MetricsRecorder`
- `TraceAttr`
- `Tracer`
- `Span`

Extend `Config` with `Observability ObservabilityConfig`.

Normalize and validate:

- trim `RuntimeLabel`, default it to `"default"`, reject invalid UTF-8, ASCII
  control characters, and labels longer than 128 bytes
- default `Redaction.ErrorMessageMaxBytes` to `1024`
- default `Metrics.ReducerLabelMode` to `ReducerLabelModeName`
- reject invalid reducer label modes before runtime construction
- make disabled metrics/tracing no-op even with non-nil recorder/tracer values

Build a runtime-scoped internal observability object that:

- is created early enough for build validation failures once config can be
  normalized
- has no-op logger, metrics, tracing, and diagnostics behavior when configured
  zero
- wraps all sink calls in panic recovery
- does not call configured sinks while holding locks that protect mutable
  runtime subsystem state, except for in-memory no-op calls
- has a recursion guard for `observability.sink_failed` and
  `runtime_errors_total{reason="observability_sink_failed"}`
- centralizes base labels/fields: module, runtime, component, event
- provides redaction helpers for errors, trace errors, debug SQL, and bounded
  UTF-8 strings

Keep `Runtime.Config()` detached. Returned slices must remain copied, while
caller-supplied pointer/interface values for logger, metrics handler, recorder,
and tracer may remain the same values explicitly supplied by the caller.

## Tests To Add First

Add focused failing tests for:

- zero `ObservabilityConfig` build/start/call/close succeeds and emits no
  observations
- runtime label trimming/defaulting and invalid label rejection
- invalid `ReducerLabelMode` rejection before runtime construction
- redaction examples from SPEC-007 section 11.1
- token-bounded redaction key matching, JSON-shaped values, and quoted and
  unquoted value boundaries from SPEC-007 section 11.1
- invalid UTF-8 normalization before redaction and truncation
- redacted error truncation preserves UTF-8 boundaries and default 1024-byte
  limit
- debug raw SQL field normalization and bounding when
  `AllowRawSQLInDebugLogs=true`
- `Metrics.Enabled=false` ignores a non-nil recorder
- `Tracing.Enabled=false` ignores a non-nil tracer
- recorder, logger, and tracer panics are recovered by foundation helpers
  without changing a simple runtime operation
- `Runtime.Config()` does not allow mutation of runtime-owned slices

## Validation

Run at least:

```sh
rtk go fmt .
rtk go test . -run 'Test.*(Observability|Config|RuntimeLabel|ReducerLabel|Redaction|Noop|Sink|Tracing|Metrics)' -count=1
rtk go vet .
```

Expand to `rtk go test ./... -count=1` if exported config or validation changes
break downstream packages.

## Completion Notes

When complete, update this file with:

- exported API names and files
- normalization and validation behavior
- redaction helper coverage
- sink isolation behavior and any remaining unsupported sink path
- validation commands run

### Recorded Completion 2026-05-01

Exported API names added in `observability.go` and `config.go`:

- `Config.Observability`
- `ObservabilityConfig`
- `RedactionConfig`
- `MetricsConfig`
- `ReducerLabelMode`
- `DiagnosticsConfig`
- `TracingConfig`
- `MetricName`
- `MetricLabels`
- `MetricsRecorder`
- `TraceAttr`
- `Tracer`
- `Span`

Normalization and validation:

- `normalizeConfig` now normalizes `ObservabilityConfig` before schema build.
- `RuntimeLabel` is trimmed, defaults to `"default"`, and rejects invalid
  UTF-8, ASCII control characters, and values over 128 bytes.
- `Redaction.ErrorMessageMaxBytes <= 0` defaults to `1024`.
- `Metrics.ReducerLabelMode == ""` defaults to `ReducerLabelModeName`.
- invalid reducer label modes reject before runtime construction.
- disabled metrics/tracing produce no runtime-scoped sink calls even when a
  recorder or tracer is configured.

Foundation internals:

- `Runtime` now carries a `runtimeObservability` built from normalized config.
- sink wrappers recover panics from logger, recorder, tracer, span event, and
  span end calls.
- sink-failure reporting has a recursion guard and avoids retrying through the
  sink that just failed.
- base module/runtime labels and log/trace fields are centralized on the
  runtime-scoped object.
- diagnostics handler panic isolation is not wired yet because SPEC-007 HTTP
  diagnostics are task 09.

Redaction coverage:

- SPEC-007 section 11.1 examples are pinned.
- token-bounded key matching, JSON-shaped values, quoted values, unquoted
  delimiter boundaries, bearer token values, invalid UTF-8 normalization, and
  UTF-8-safe truncation are covered.
- debug raw SQL normalization and bounding is available only when explicitly
  enabled.

Validation:

```sh
rtk go fmt .
rtk go test . -run 'Test.*(Observability|Config|RuntimeLabel|ReducerLabel|Redaction|Noop|Sink|Tracing|Metrics)' -count=1
rtk go vet .
rtk go test . -count=1
rtk go test ./... -count=1
```

Results: all commands passed.
