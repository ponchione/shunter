# V2-G Task 01: Reconfirm Process Boundary Prerequisites

Parent plan: `docs/hosted-runtime-planning/V2/V2-G/00-current-execution-plan.md`

Objective: identify every live seam that an out-of-process module would cross.

Checks:
- `rtk go doc . Module`
- `rtk go doc . Runtime`
- `rtk go doc ./executor Executor`
- `rtk go doc ./executor ReducerRegistry`
- `rtk go doc ./types ReducerContext`
- `rtk go doc ./store Transaction`
- `rtk go doc ./store CommittedState`
- `rtk go doc ./subscription Manager`
- `rtk go doc ./protocol Server`
- `rtk go doc . ModuleContract`

Read only if needed:
- `executor/`
- `store/`
- `subscription/`
- `protocol/`
- `runtime_lifecycle.go`
- `runtime_local.go`

Prerequisite conclusions to record in Task 01:
- reducer invocation currently runs in-process with direct `ReducerContext`
- transaction semantics and rollback are local Go object semantics
- subscriptions depend on committed changesets and live state
- protocol admission is runtime-owned
- contract export does not describe a process invocation protocol

Stop if:
- V2-A boundary cleanup and V2-F host decisions are incomplete
- process isolation would require replacing core transaction semantics without a
  dedicated design
