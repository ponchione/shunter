# V3 Task 10: Tracing Hooks

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- Task 02 observability foundation
- Task 03 build and recovery observations
- Task 07 subsystem metrics

Objective: add optional Shunter-owned tracing hooks with required span names,
attributes, redaction, and panic isolation.

## Required Context

Read:
- `docs/specs/007-observability/SPEC-007-observability.md` sections 4, 11, 12,
  and 14
- `docs/features/V3/02-observability-foundation.md`

Inspect:

```sh
rtk go doc . Tracer
rtk go doc . Span
rtk grep -n 'Start\\(|OpenAndRecover|CallReducer|Commit|Durability|EvalAndBroadcast|HandleSubscribe|RegisterSet|UnregisterSet|OneOff|DeclaredRead' *.go commitlog executor protocol subscription store
```

## Target Behavior

Add tracing spans only when `Tracing.Enabled=true` and `Tracing.Tracer` is
non-nil.

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

Every span includes default attributes:

- `component`
- `module`
- `runtime`

Additional attributes must match SPEC-007 section 12. Attribute values must
follow the same redaction and cardinality rules as logs. Raw SQL, reducer args,
row payloads, tokens, authorization headers, signing keys, request IDs,
query IDs, and connection IDs must not be default trace attributes.

Tracing sink isolation:

- recover `StartSpan` panics and treat tracing as disabled for that operation
- skip `AddEvent` and `End` when `StartSpan` returns a nil span
- recover `Span.AddEvent` and `Span.End` panics
- pass nil to `Span.End` on success
- pass only redacted bounded errors or known-safe errors to `Span.End` on
  failure

Do not add OpenTelemetry as a root package dependency. Any OpenTelemetry bridge
belongs outside the root package as a future adapter.

## Tests To Add First

Add focused failing tests for:

- tracing disabled with a non-nil tracer records no spans
- tracing enabled records each required span name in a representative runtime
  path
- spans include required default attributes
- recovery, protocol, reducer, store commit, durability, subscription eval,
  fan-out, one-off query, register, and unregister spans include required
  additional attributes
- trace attributes redact/bound errors and do not contain raw SQL, reducer
  args, row payloads, tokens, or signing keys
- `StartSpan`, `AddEvent`, and `End` panics are recovered
- nil spans are skipped
- tracer panic does not change reducer, protocol, or subscription operation
  results

## Validation

Run at least:

```sh
rtk go fmt . ./commitlog ./executor ./protocol ./subscription ./store
rtk go test . -run 'Test.*(Tracing|Span|Reducer|Query|Subscription|Runtime|Recovery)' -count=1
rtk go test ./executor ./protocol ./subscription ./commitlog ./store -run 'Test.*(Tracing|Span|Reducer|Durability|Protocol|Subscription)' -count=1
rtk go vet . ./commitlog ./executor ./protocol ./subscription ./store
```

Expand to `rtk go test ./... -count=1` if tracing changes subsystem interfaces.

## Completion Notes

When complete, update this file with:

- span insertion points
- attribute coverage
- panic isolation behavior
- validation commands run

### Recorded Completion 2026-05-01

Span insertion points:

- `shunter.runtime.start`: recorded from runtime start ready/failure paths.
- `shunter.recovery.open`: recorded from build-time recovery/bootstrap
  success and failure observations.
- `shunter.protocol.message`: recorded with protocol message metric
  classification at terminal message outcomes.
- `shunter.query.one_off`: recorded from terminal `one_off_query` protocol
  outcomes.
- `shunter.reducer.call`: recorded from executor reducer terminal outcomes,
  including permission, user error, panic, commit failure, and post-commit
  fatal outcomes.
- `shunter.store.commit`: recorded around executor-owned `store.Commit`
  success/failure.
- `shunter.durability.batch`: recorded from durability worker batch
  success/failure.
- `shunter.subscription.eval`: recorded after post-commit subscription
  evaluation.
- `shunter.subscription.fanout`: recorded after fan-out delivery attempts,
  with failed reason classification when delivery fails.
- `shunter.subscription.register` and `shunter.subscription.unregister`:
  recorded from executor-owned set register/unregister terminal outcomes.

Attribute coverage:

- Every span starts through runtime observability and includes `component`,
  `module`, and `runtime`.
- Required additional attributes are covered with low-cardinality values:
  `state`, `result`, `kind`, `reducer`, `tx_id`, and fan-out `reason` on
  failure.
- Trace attributes intentionally exclude raw SQL, reducer args, row payloads,
  tokens, authorization headers, signing keys, request IDs, query IDs, and
  connection IDs.
- Failure span endings pass either known-safe classified errors or
  runtime-redacted bounded errors.

Panic isolation:

- Existing runtime tracing wrappers recover `StartSpan`, `AddEvent`, and
  `End` panics.
- `StartSpan` panic is treated as a disabled span for that operation.
- Nil spans skip `AddEvent` and `End`.
- Tracer and span panics do not alter reducer, protocol, or subscription
  operation results.

Validation:

```sh
rtk go fmt . ./commitlog ./executor ./protocol ./subscription ./store
rtk go test . -run 'Test.*(Tracing|Span|Reducer|Query|Subscription|Runtime|Recovery)' -count=1
rtk go test ./executor ./protocol ./subscription ./commitlog ./store -run 'Test.*(Tracing|Span|Reducer|Durability|Protocol|Subscription)' -count=1
rtk go vet . ./commitlog ./executor ./protocol ./subscription ./store
rtk go test ./... -count=1
```

Results: all commands passed.
