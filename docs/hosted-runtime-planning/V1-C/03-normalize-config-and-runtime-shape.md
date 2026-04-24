# V1-C Task 03: Implement config normalization and private runtime build state

Parent plan: `docs/hosted-runtime-planning/V1-C/2026-04-23_205158-hosted-runtime-v1c-runtime-build-pipeline-implplan.md`

Objective: extend the root runtime to retain normalized build/recovery state needed for later slices.

Files:
- Modify `config.go`
- Modify `runtime.go`
- Create or modify `runtime_build.go`

Implementation requirements:
- normalize blank `DataDir` to the runtime default private path
- store private built-state fields such as `registry`, `dataDir`, committed state, recovered tx id, and resume plan
- keep the root public API narrow; private fields are acceptable
- do not start durability workers, executor loops, scheduler loops, fan-out workers, or HTTP serving
- do not use `cmd/shunter-example` as implementation source of truth
