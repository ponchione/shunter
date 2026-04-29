# V2-G Task 04: Record The Process Isolation Decision

Parent plan: `docs/hosted-runtime-planning/V2/V2-G/00-current-execution-plan.md`

Objective: leave a clear decision trail regardless of implementation outcome.

Record:
- whether out-of-process execution is kept, deferred, or rejected for now
- which code/test evidence drove the decision
- which runtime/module seams would need future work
- whether any prototype package is experimental/internal
- what remains guaranteed for statically linked in-process modules

Required conclusion:
- normal v2 app authors must still have a clear supported path
- any process-isolation path must not silently replace the existing runtime
  model
- future agents should know whether to continue or avoid this direction

## Record

Decision: defer out-of-process module execution.

Evidence:
- `internal/processboundary` proves a small boundary description is possible
  for invocation request/response metadata and failure classification.
- `ValidateInvocationResponse` requires explicit transaction semantics and
  rejects commit/rollback decisions under the unsupported transaction mode.
- `DefaultContract` records transaction mutation as unsupported and pins
  subscription updates to committed host state.
- `runtime_contract_test.go` proves canonical `ModuleContract` JSON is not
  extended with process-boundary metadata.

Future work needed before continuing this direction:
- design a serializable transaction mutation protocol, or explicitly restrict
  out-of-process reducers to non-mutating work.
- define how reducer DB and scheduler handles behave across a process boundary.
- define lifecycle failure and cleanup behavior without violating current
  OnConnect rollback and OnDisconnect cleanup guarantees.
- preserve committed-state-driven subscription fan-out and durability ordering.

Current guarantee:
- the supported runtime model remains statically linked, in-process Go modules
  built with `shunter.Build(...)`.
- no process-isolation path silently replaces `Runtime`, `Host`, executor,
  protocol admission, or canonical contract export.
