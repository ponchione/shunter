# V2-G Task 03: Prototype Or Defer Process Isolation

Parent plan: `docs/hosted-runtime-planning/V2/V2-G/00-current-execution-plan.md`

Objective: make an explicit, evidence-based decision.

Prototype only if:
- Task 02 tests show a small, coherent boundary
- reducer transaction semantics can be preserved or safely limited
- lifecycle failure semantics are clear
- local testing remains practical
- in-process modules remain supported

Defer if:
- the process boundary requires broad transaction redesign
- subscription semantics become ambiguous
- app ergonomics degrade without a concrete operational win
- multi-module host needs are still unsolved

If prototyping:
- keep it behind an internal or experimental package boundary
- do not expose it as the default runtime path
- document unsupported behaviors explicitly

## Decision

Deferred production out-of-process module execution.

Implemented only an internal boundary-contract prototype in
`internal/processboundary`. It is not a runner and is not wired into
`Runtime`, `Executor`, `Host`, or `protocol.Server`.

Reasons:
- reducer mutation still depends on direct `store.Transaction` access from a
  synchronous in-process `types.ReducerContext`.
- rollback and commit are host-local Go object semantics, not a serializable
  process protocol.
- subscriptions are evaluated from committed changesets and committed read
  views after host commit, so process messages must not become an alternate
  broadcast source.
- lifecycle ordering can be described, but preserving OnConnect rollback and
  OnDisconnect cleanup behavior across a process boundary would require a
  larger transaction design.

Supported path retained:
- statically linked in-process modules remain the normal v2 app-authoring and
  runtime execution model.
