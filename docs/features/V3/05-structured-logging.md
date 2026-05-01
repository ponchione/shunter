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
