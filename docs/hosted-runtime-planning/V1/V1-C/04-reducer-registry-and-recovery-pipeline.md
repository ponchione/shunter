# V1-C Task 04: Implement recovery/bootstrap and reducer-registry assembly

Parent plan: `docs/hosted-runtime-planning/V1-C/2026-04-23_205158-hosted-runtime-v1c-runtime-build-pipeline-implplan.md`

Objective: make `Build` assemble the non-started runtime plan from module/schema registrations and durable state.

Files:
- Modify `runtime_build.go`
- Modify tests in `runtime_build_test.go`

Implementation requirements:
- call `mod.builder.Build(...)` and treat the built schema registry as source of truth
- attempt `commitlog.OpenAndRecoverDetailed`
- on `commitlog.ErrNoData`, register tables in a fresh committed state, write an initial snapshot, then reopen
- create a private `executor.ReducerRegistry` from registered reducers and lifecycle hooks
- store enough private runtime-owned state for V1-D to start/close the graph later
- do not instantiate `commitlog.NewDurabilityWorkerWithResumePlan`, because it starts a goroutine immediately

Run:
- `rtk go test .`
