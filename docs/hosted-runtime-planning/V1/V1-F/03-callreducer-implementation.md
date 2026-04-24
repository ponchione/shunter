# V1-F Task 03: Implement local reducer invocation through the executor command path

Parent plan: `docs/hosted-runtime-planning/V1-F/2026-04-23_212927-hosted-runtime-v1f-local-runtime-calls-implplan.md`

Objective: expose a synchronous local reducer API without bypassing executor semantics.

Files:
- Modify `runtime.go`
- Create or modify `runtime_local.go`

Implementation requirements:
- add a small root result type aligned with `executor.ReducerResponse`
- add local caller options or request struct with explicit identity fields
- default zero identity to a deterministic private local dev/test identity
- submit `executor.CallReducerCmd` with a buffered response channel
- do not call reducer handlers directly and do not construct fake protocol connections
- use executor admission/runtime errors as call errors and reducer user errors as result failures
