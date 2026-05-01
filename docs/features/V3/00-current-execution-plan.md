# Hosted Runtime V3 Current Execution Plan

Status: planned

Goal: implement SPEC-007 operator-facing observability without changing hosted
runtime correctness or public protocol compatibility.

Primary authority:
- `docs/specs/007-observability/SPEC-007-observability.md`

## Target

V3 must make these statements true:

- `Config` exposes `ObservabilityConfig`, metrics, diagnostics, redaction, and
  tracing settings with SPEC-007 normalization and validation
- zero-value observability remains a no-op while in-process `Health` and
  `Describe` remain available
- build failures, fresh bootstrap, recovery success/failure, damaged tails, and
  skipped snapshots are observable before and after runtime construction
- `Runtime.Health`, `Host.Health`, `Runtime.Describe`, and `Host.Describe`
  expose the expanded detached health shapes from SPEC-007 section 9
- production diagnostics use runtime-scoped `slog` events and no longer write
  directly to process-global logging
- lifecycle, recovery, protocol, executor, reducer, durability, subscription,
  and fan-out metrics use the fixed `MetricsRecorder` and `MetricLabels`
  surface
- `observability/prometheus` adapts Shunter metrics to Prometheus without root
  package Prometheus imports or default global registration
- `/healthz`, `/readyz`, `/debug/shunter/runtime`, `/debug/shunter/host`, and
  optional `/metrics` obey the SPEC-007 HTTP method, status, content, payload,
  and exact-path rules
- tracing hooks are optional, panic-isolated, redacted, and do not require
  OpenTelemetry in the root package
- redaction, cardinality, sink isolation, and endpoint behavior are pinned by
  focused tests and a final V3 verification pass

## Task Sequence

1. Reconfirm the live runtime, health, recovery, logging, protocol, executor,
   subscription, and host stack.
2. Add the observability public API, normalization, redaction helpers, metrics
   and tracing interfaces, and internal sink isolation.
3. Persist recovery facts and emit build/recovery observations.
4. Expand runtime and host health/description snapshots.
5. Replace process-global production logging with structured runtime logs.
6. Add the metrics core and lifecycle/recovery metrics.
7. Instrument protocol, executor, reducer, durability, subscription, and fan-out
   metrics.
8. Add the Prometheus adapter package.
9. Add runtime and host HTTP diagnostics.
10. Add optional tracing hooks.
11. Run the SPEC-007 gauntlet and final validation gates.

## Phase Boundaries

Phase A, tasks 01-04, may land without the Prometheus adapter, HTTP endpoints,
or tracing hooks. It must leave zero-value observability no-op and make health
and recovery facts available to later instrumentation.

Phase B, tasks 05-07, may add signal plumbing incrementally. Each task must
recover sink panics and keep the observed runtime operation's result unchanged.
No subsystem should grow free-form metric labels or unredacted log fields.

Phase C, tasks 08-10, depends on the foundation and health snapshots. The
Prometheus adapter, diagnostics handlers, and tracing hooks must remain
optional and must not become runtime startup requirements.

Task 11 closes the feature. Do not call V3 complete without a SPEC-007 section
14 coverage matrix and final validation commands.

## Validation Posture

Each worker should:
- inspect live Go symbols with `rtk go doc` before editing unfamiliar code
- add failing tests before implementation
- keep changes scoped to the assigned task
- run `rtk go fmt` on touched packages
- run targeted package tests first
- run `rtk go vet` for touched packages when behavior, exported APIs, or
  interfaces changed
- expand to `rtk go test ./... -count=1` when changes cross root runtime,
  commitlog, protocol, executor, subscription, diagnostics, or adapters

Final completion gates:

```sh
rtk go fmt ./...
rtk go test ./commitlog ./executor ./protocol ./subscription ./store ./observability/prometheus -count=1
rtk go test . -run 'Test.*(Observability|Health|Diagnostics|Metrics|Logging|Tracing|Recovery|Redaction|Prometheus|Runtime|Host)' -count=1
rtk go vet ./commitlog ./executor ./protocol ./subscription ./store ./observability/prometheus .
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

