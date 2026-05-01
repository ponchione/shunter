# V3 Task 05: Structured Runtime Logging

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Task 02 observability foundation
- Task 03 build and recovery observations
- Task 04 health and description expansion

Objective: route production diagnostics through runtime-scoped `slog` events
with SPEC-007 fields, levels, redaction, and sink isolation.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` sections 2, 8,
  8.1, 9.3, 11, and 14
- `docs/features/V3/02-observability-foundation.md`
- `docs/features/V3/03-build-recovery-observations.md`
- `docs/features/V3/04-health-and-descriptions.md`

Inspect:

```sh
rtk go doc . ObservabilityConfig
rtk grep -n 'log\\.Printf|log\\.' *.go commitlog executor protocol subscription store
rtk go doc ./executor Executor
rtk go doc ./protocol Server
rtk go doc ./subscription Manager
rtk go doc ./commitlog DurabilityWorker
```

## Target Behavior

Replace production process-global logging with structured runtime logging:

- every record message equals the stable event name
- every record includes `component`, `event`, `module`, and `runtime`
- optional fields follow SPEC-007 section 8 names and cardinality rules
- free-form maps are not logged
- required events from section 8.1 are emitted at the required levels
- `recovery.completed` is Warn when damaged tails or skipped snapshots exist,
  otherwise Info
- stack traces appear only on panic events and may be debug-gated
- raw SQL is logged only in a dedicated debug field when
  `AllowRawSQLInDebugLogs` is true
- errors are redacted and bounded before logging
- `observability.sink_failed` is emitted only through a sink that did not fail
  for the current event, and recursive sink retry loops are avoided

Subsystem packages that can be used before a runtime exists may keep an
internal package-level no-op path, but production runtime-owned operations must
receive or derive the runtime-scoped observability object.

## Tests To Add First

Add focused failing tests for:

- non-nil logger receives `runtime.ready` with required base fields and message
  equal to the event
- startup failure logs `runtime.start_failed` with redacted error and duration
- close failure logs `runtime.close_failed` and successful close logs
  `runtime.closed`
- recovery warning level changes when damaged tails or skipped snapshots exist
- degraded health logs `runtime.health_degraded` with the primary reason
- protocol rejection/auth/protocol-error logs use controlled `result` or
  `reason` values and redacted errors
- protocol open, close, and backpressure logs use the required debug/warn
  levels and controlled fields
- durability fatal failures, executor fatal state, and store snapshot leaks
  emit their required SPEC-007 section 8.1 events
- reducer panic logs `executor.reducer_panic` with reducer name and gated stack
- lifecycle reducer failures log `executor.lifecycle_reducer_failed`
- subscription eval and fan-out errors use required events and redacted fields
- raw SQL appears only in a dedicated debug field when explicitly enabled and
  that field is UTF-8 normalized and bounded
- production `log.Printf` call sites are gone or unreachable from
  runtime-owned production paths
- logger panic is recovered and does not change reducer or lifecycle results

Existing tests that capture process-global `log` output should be rewritten to
capture a configured `slog.Logger`.

## Validation

Run at least:

```sh
rtk go fmt . ./commitlog ./executor ./protocol ./subscription ./store
rtk go test . -run 'Test.*(Logging|Runtime|Recovery|Start|Close|Degraded)' -count=1
rtk go test ./executor ./protocol ./subscription ./commitlog ./store -run 'Test.*(Log|Panic|Error|Recovery|Subscription|Protocol)' -count=1
rtk go vet . ./commitlog ./executor ./protocol ./subscription ./store
rtk grep -n 'log\\.Printf|log\\.' *.go commitlog executor protocol subscription store
```

The final grep should show only test code, comments, or explicitly documented
pre-runtime no-op fallback code.

## Completion Notes

When complete, update this file with:

- production logging call sites replaced
- any package-level no-op fallback left in place and why
- event coverage added
- validation commands run

### Recorded Completion 2026-05-01

Production logging call sites replaced:

- Runtime lifecycle logging now emits `runtime.start_failed`,
  `runtime.ready`, `runtime.close_failed`, `runtime.closed`, and
  `runtime.health_degraded` from the runtime-scoped `runtimeObservability`.
- `commitlog`, `executor`, `protocol`, `subscription`, and `store` now expose
  narrow observer interfaces that are wired from `Runtime.Start` or `Build`
  with the runtime-scoped observability object.
- Declared-read protocol send failures now use `protocol.protocol_error`
  instead of package-global logging.
- Production `log.Printf` imports/call sites were removed from runtime-owned
  code paths. Existing tests that captured process-global logs were rewritten
  to use package observers.

No-op fallback / isolation:

- Nil observers in subsystem packages are the package-level no-op fallback for
  standalone package tests and pre-runtime use.
- CLI command output is explicit user-facing stdout/stderr text through
  injected writers, not runtime diagnostics; it remains outside process-global
  logging and must not use the standard `log` package.
- Commitlog offset-index advisory failures remain non-fatal and now disable
  indexing silently rather than writing process-global logs; durability-fatal
  failures emit `durability.failed` when a runtime observer is present.
- Logger, metrics, and tracer sink panics remain isolated by
  `runtimeObservability`; logger panic coverage now includes runtime lifecycle
  operations.

Event coverage added:

- Runtime lifecycle/degraded logs: `runtime.ready`, `runtime.start_failed`,
  `runtime.close_failed`, `runtime.closed`, and `runtime.health_degraded`.
- Subsystem structured events wired for `durability.failed`,
  `executor.fatal`, `executor.reducer_panic`,
  `executor.lifecycle_reducer_failed`, protocol rejection/open/close/error/auth
  and backpressure events, subscription eval/fanout/drop events, and
  `store.snapshot_leaked`.
- New tests cover lifecycle base fields, redacted/bounded errors, primary
  degraded reason selection, reducer panic stack gating, and logger panic
  isolation.

Validation:

```sh
rtk go fmt . ./commitlog ./executor ./protocol ./subscription ./store
rtk go test . -run 'Test.*(Logging|Runtime|Recovery|Start|Close|Degraded)' -count=1
rtk go test ./executor ./protocol ./subscription ./commitlog ./store -run 'Test.*(Log|Panic|Error|Recovery|Subscription|Protocol)' -count=1
rtk go vet . ./commitlog ./executor ./protocol ./subscription ./store
rtk grep -n 'log\\.Printf|log\\.' *.go commitlog executor protocol subscription store
rtk go test ./... -count=1
```

Results: all Go format/test/vet commands passed. The required grep command
still reports false-positive `commitlog.` and `slog.` substrings; a
word-bounded process-global check and import check showed no production
`log.Printf` paths and no production `"log"` imports:

```sh
rtk grep -n '\\blog\\.Printf|\\blog\\.' *.go commitlog executor protocol subscription store
rtk grep -n '"log"' *.go commitlog/*.go executor/*.go protocol/*.go subscription/*.go store/*.go
```

Post-completion hardening added a root regression test that parses production
Go files and fails on standard `log` imports, process-global `log.*` calls, or
direct production `log/slog` imports outside the runtime observability owner.
