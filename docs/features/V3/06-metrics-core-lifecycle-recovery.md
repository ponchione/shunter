# V3 Task 06: Metrics Core, Lifecycle, And Recovery

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Task 02 observability foundation
- Task 03 build and recovery observations
- Task 04 health and description expansion

Objective: implement the internal metrics call path and lifecycle/recovery
metric families without adding a Prometheus dependency to the root package.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` sections 5, 6,
  6.1, 6.2, 9, and 14
- `docs/features/V3/02-observability-foundation.md`
- `docs/features/V3/03-build-recovery-observations.md`

Inspect:

```sh
rtk go doc . MetricsRecorder
rtk go doc . MetricLabels
rtk go doc . RuntimeState
rtk grep -n 'RuntimeState|Start\\(|Close\\(|Health\\(|Recovery|DurableTxID|queue' *.go commitlog
```

## Target Behavior

Add best-effort metrics helpers for the fixed `MetricsRecorder` interface:

- call `AddCounter` only for counters
- call `SetGauge` only for gauges
- call `ObserveHistogram` only for histograms
- recover recorder panics
- avoid recursive `observability_sink_failed` observations
- populate `Module` and `Runtime` labels consistently
- never add free-form labels

Instrument lifecycle and recovery families:

- `runtime_ready`
- `runtime_state`
- `runtime_degraded`
- `runtime_errors_total`
- `recovery_runs_total`
- `recovery_recovered_tx_id`
- `recovery_damaged_tail_segments`
- `recovery_skipped_snapshots_total`
- initial `durability_durable_tx_id` from recovery facts where appropriate

State gauges must be one-hot after build and every state transition. Ready and
degraded gauges must update synchronously with lifecycle transitions and fatal
or degraded condition changes.

This task establishes the shared metric timing and label helpers used by task
07. It does not add the Prometheus adapter.

## Tests To Add First

Add focused failing tests for:

- runtime state gauges are one-hot after build, start, failure, closing, and
  closed transitions
- `runtime_ready` and `runtime_degraded` gauges track health transitions
- build/start/close failures increment exactly one mapped
  `runtime_errors_total` counter
- successful recovery/bootstrap increments `recovery_runs_total{result="success"}`
  and sets recovered tx gauge
- recovery failure increments `recovery_runs_total{result="failed"}`
- skipped snapshots increment `recovery_skipped_snapshots_total` once per
  skipped report with mapped reasons
- metric recorder panic is recovered and does not recursively emit sink-failure
  observations
- metrics disabled with a non-nil recorder records nothing
- metric labels use module name and normalized runtime label

## Validation

Run at least:

```sh
rtk go fmt . ./commitlog
rtk go test . -run 'Test.*(Metrics|Gauge|RuntimeState|Recovery|Build|Start|Close|Degraded)' -count=1
rtk go test ./commitlog -run 'Test.*Recovery' -count=1
rtk go vet . ./commitlog
```

Expand to `rtk go test ./... -count=1` if lifecycle state or build behavior
changes broadly.

## Completion Notes

When complete, update this file with:

- lifecycle and recovery metric helpers added
- exact label values and reason mappings covered
- recorder panic behavior
- validation commands run

### Recorded Completion 2026-05-01

Lifecycle and recovery metric helpers:

- `runtimeObservability` now centralizes recorder-safe `AddCounter`,
  `SetGauge`, and `ObserveHistogram` calls for fixed `MetricLabels`; lifecycle
  helpers emit `runtime_ready`, one-hot `runtime_state`,
  `runtime_degraded`, and `durability_durable_tx_id` from runtime health.
- `Build` emits the initial built-state lifecycle gauges after successful
  runtime construction.
- `Start` emits the starting and ready/failed state gauges; failed starts
  increment `runtime_errors_total`.
- `Close` emits closing and closed state gauges; close failures increment
  `runtime_errors_total`.
- recovery/bootstrap success continues to increment `recovery_runs_total`,
  set `recovery_recovered_tx_id`, set `recovery_damaged_tail_segments`,
  increment skipped-snapshot counters, and now also seeds
  `durability_durable_tx_id` from the recovered transaction horizon.

Label values and mappings covered:

- lifecycle gauges use `module=<ModuleName()>` and the normalized
  `runtime=<Observability.RuntimeLabel>` with no free-form labels.
- `runtime_state` uses bounded `state` values: `built`, `starting`, `ready`,
  `closing`, `closed`, and `failed`.
- `runtime_errors_total` uses `component="runtime"` and mapped reasons
  `build_failed`, `start_failed`, and `close_failed` for this slice; the helper
  maps unknown runtime reasons to `unknown`.
- recovery metrics keep `component="commitlog"` and use bounded
  `result="success"` / `result="failed"`.
- skipped snapshot reasons map `past_durable_horizon` to `newer_than_log`,
  `read_failed` to `read_failed`, and unknown reasons to `unknown`, one counter
  increment per skipped snapshot report.

Recorder panic behavior:

- metric recorder panics are recovered at each metric call site.
- a panicking recorder does not change build/start/reducer/close outcomes.
- metrics sink failures do not recursively attempt
  `runtime_errors_total{reason="observability_sink_failed"}` through the same
  failed recorder; non-failing logger fallback still records
  `observability.sink_failed`.

Validation:

```sh
rtk go fmt . ./commitlog
rtk go test . -run 'Test.*(Metrics|Gauge|RuntimeState|Recovery|Build|Start|Close|Degraded)' -count=1
rtk go test ./commitlog -run 'Test.*Recovery' -count=1
rtk go vet . ./commitlog
rtk go test ./... -count=1
```

Results: all commands passed.
