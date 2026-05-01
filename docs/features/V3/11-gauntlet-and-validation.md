# V3 Task 11: Gauntlet And Validation

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Tasks 01-10 complete

Objective: close V3 by proving the full SPEC-007 verification matrix across
no-op defaults, sink isolation, redaction, health, logging, metrics,
Prometheus, diagnostics, tracing, and runtime correctness.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` section 14
- all completed `docs/features/V3/0*.md` task files

Inspect:

```sh
rtk go test . -list 'Test.*(Observability|Health|Diagnostics|Metrics|Logging|Tracing|Recovery|Redaction|Prometheus|Runtime|Host)'
rtk go test ./commitlog -list 'Test.*(Recovery|Durability|Observability|Metrics)'
rtk go test ./executor -list 'Test.*(Executor|Reducer|Metrics|Logging|Tracing)'
rtk go test ./protocol -list 'Test.*(Protocol|Connection|Metrics|Logging|Tracing|Diagnostics)'
rtk go test ./subscription -list 'Test.*(Subscription|Fanout|Metrics|Logging|Tracing)'
rtk go test ./observability/prometheus -list 'Test.*'
```

## Target Behavior

Before marking V3 complete:

- build a coverage matrix that maps every SPEC-007 section 14 verification row
  to at least one test name
- keep the matrix row-granular: broad category names are not enough when
  section 14 lists separate edge cases
- add missing focused tests before adding or expanding broad gauntlet tests
- add a root runtime gauntlet only for cross-subsystem behavior that cannot be
  pinned well by focused package tests
- verify zero-value observability remains no-op across build/start/call/close
- verify sink panics never alter reducer results, protocol results,
  subscription behavior, or runtime lifecycle return values
- verify redaction covers logs, diagnostics, trace errors/attributes, and
  metric reason mapping
- verify metric labels never include raw SQL, reducer args, row payloads,
  request IDs, query IDs, connection IDs, raw error strings, identities, tokens,
  or signing keys
- verify diagnostics endpoints do not mount unless configured and exact-path
  behavior is pinned
- verify root package does not import Prometheus packages
- verify no production process-global logging remains except documented
  pre-runtime no-op fallback paths

## Tests To Add First

Add any missing coverage from the section 14 matrix. Prefer:

- focused unit tests for redaction, normalization, metrics recorder behavior,
  Prometheus family registration, and HTTP method/path/status rules
- root runtime tests for build/start/close, health, describe, and no-op
  behavior
- subsystem tests for protocol, executor, reducer, durability, subscription,
  fan-out, and tracing insertion points
- one cross-subsystem gauntlet for configured logger, recorder, tracer, and
  diagnostics handler operating together under normal runtime traffic

Do not duplicate every lower-level package edge case in the gauntlet if focused
tests already pin it. Record the mapping instead.

## Validation

Run the final completion gates:

```sh
rtk go fmt ./...
rtk go test ./commitlog ./executor ./protocol ./subscription ./store ./observability/prometheus -count=1
rtk go test . -run 'Test.*(Observability|Health|Diagnostics|Metrics|Logging|Tracing|Recovery|Redaction|Prometheus|Runtime|Host)' -count=1
rtk go vet ./commitlog ./executor ./protocol ./subscription ./store ./observability/prometheus .
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

If a validation command is not run, record the reason and the remaining risk in
this file before closing V3.

## Completion Notes

When complete, update this file with:

- the SPEC-007 section 14 coverage matrix or a link to it
- gauntlet tests added
- final validation command results
- any accepted closure caveats
