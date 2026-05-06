# Process Isolation And App Trust Model

Status: open
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

## v1 Decisions To Make

1. Decide whether v1 explicitly uses an app-trust model.
2. Decide which reducer rules are contractual:
   - writes only through runtime transaction APIs
   - no external side effects
   - deterministic behavior where replay/recovery depends on it
   - no long-running/blocking reducer work
3. Decide whether lifecycle hooks and migration hooks have different side-effect
   rules than reducers.
4. Decide how panics are handled in reducers, hooks, and scheduler callbacks.
5. Decide whether any timeout, cancellation, or worker-pool limits are required
   for v1.
6. Decide whether `internal/processboundary` remains experimental or becomes a
   planned v2 direction.

## Implementation Work

- Audit reducer, lifecycle, scheduler, and migration execution paths.
- Document the v1 app-trust model in app-author docs.
- Add tests for panic behavior, cancellation, shutdown, and failed hooks.
- Ensure errors clearly distinguish app failure from Shunter runtime failure.
- Add optional configuration for execution limits only if the current runtime
  has an enforceable boundary.
- Keep out-of-process runner work separate from v1 unless this decision blocks
  adoption.

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

