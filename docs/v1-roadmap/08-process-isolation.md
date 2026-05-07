# Process Isolation And App Trust Model

Status: active, v1 app-trust model and failure-source wording documented
Owner: unassigned
Scope: Shunter's execution trust boundary for Go modules, reducer behavior,
side effects, global state, and future out-of-process isolation.

## Goal

Make Shunter's execution model explicit for v1.

Shunter is Go-native and in-process today. That is a legitimate product choice,
but it has different safety and determinism properties from systems that execute
application modules in a sandbox. v1 must document those properties and decide
which ones are enforced, tested, or left to app ownership.

## Current State

Reducers and hooks run in the application's Go process. This gives Shunter a
simple embedding model, direct access to Go libraries, and easy deployment as a
normal app binary. It also means Shunter does not automatically prevent:

- non-deterministic reducer behavior
- external side effects inside reducers
- global mutable state
- goroutine leaks
- process-wide panics
- blocking calls that stall runtime work
- memory exhaustion caused by app code

SpacetimeDB's reference model is useful as contrast: its module execution model
creates stronger isolation expectations. Shunter does not need to copy that
model for v1, but Shunter must not imply guarantees it does not enforce.

Current code reality:

- Shunter is still an in-process Go runtime. There is no WASM/plugin sandbox,
  dynamic module upload, or out-of-process reducer runner in the app-facing v1
  surface.
- Reducer panic, lifecycle, scheduler, shutdown, and migration-hook behavior has
  package and gauntlet coverage.
- The app-author guide now includes an explicit in-process trust-model section.
- `internal/processboundary` remains internal and should not be treated as a v1
  feature without a separate decision.
- Confirmation coverage includes reducer panic rollback in `gauntlet_test.go`,
  lifecycle panic handling in `executor/lifecycle_test.go`, scheduler panic
  behavior in `executor/scheduler_firing_test.go`, migration hook failure in
  `migration_test.go`, and cancellation/shutdown behavior in `local_test.go`
  and `lifecycle_test.go`.
- Local reducer results expose typed user-error, panic, permission, and
  internal statuses. Protocol reducer failure strings now preserve that
  distinction with source prefixes so app failures and Shunter runtime failures
  are visible to clients.

## Settled v1 Decisions

- v1 explicitly uses an app-trust model. App code runs in process and is trusted
  not to exhaust memory, deadlock the process, or start unsafe goroutines.
- Reducers should mutate Shunter state only through reducer transaction APIs,
  keep replay-sensitive work deterministic, avoid external side effects, and
  avoid long-running work on the serialized executor path.
- Lifecycle hooks and scheduled reducers use the same in-process trust
  boundary. Migration hooks are app-owned startup or offline maintenance work
  and should be retryable from a known backup.
- Reducer user errors and reducer panics roll back the reducer transaction and
  are reported as failed reducer results. Scheduler, lifecycle, migration, and
  shutdown behavior is covered by package and gauntlet tests.
- v1 does not add timeout, cancellation, worker-pool, memory, WASM, plugin, or
  process-isolation guarantees beyond what the current runtime can enforce.
- `internal/processboundary` remains internal research/post-v1 planning, not a
  v1 app-facing feature.

## Implementation Work

Completed or partially complete:

- Audit reducer, lifecycle, scheduler, and migration execution paths enough to
  document the current app-trust boundary.
- Document the v1 app-trust model in app-author docs.
- Add package and gauntlet coverage for reducer panic, lifecycle, scheduler,
  shutdown, and failed migration-hook behavior.
- Confirm the existing panic, cancellation, shutdown, and failed-hook tests that
  back the documented app-trust model.
- Settle the v1 process-isolation policy as an explicit in-process app-trust
  model with no new sandbox or timeout configuration.
- Add protocol adapter coverage that labels reducer user errors, reducer
  panics, permission failures, internal failures, and unknown executor statuses
  in caller-visible failed `TransactionUpdate` strings.

Remaining:

- Keep confirmation tests current when panic, cancellation, shutdown, or
  failed-hook behavior changes.
- Keep failure-source wording stable and clearly documented when local or
  protocol reducer result surfaces change.

## Verification

Run targeted executor/runtime tests, then:

```bash
rtk go test ./...
rtk go vet ./...
```

If cancellation or timeout semantics change, add tests that prove shutdown does
not hang indefinitely when app code returns errors or panics.

## Done Criteria

- The v1 docs plainly state the in-process trust model.
- Reducer, hook, and scheduler side-effect expectations are documented.
- Panic and error behavior is tested.
- Any unsupported isolation guarantee is explicitly called out.
- Future process-boundary work is tracked as a separate post-v1 direction unless
  a v1 decision says otherwise.

## Non-Goals

- WASM or plugin runtime for v1.
- Dynamic module upload.
- Sandboxing arbitrary untrusted code.
- Matching SpacetimeDB's execution isolation model.
