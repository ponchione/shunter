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
