# V2-A Task 02: Add Failing Boundary Invariant Tests

Parent plan: `docs/hosted-runtime-planning/V2/V2-A/00-current-execution-plan.md`

Objective: pin the current in-process boundary before internal cleanup.

Likely files:
- `runtime_build_test.go`
- `runtime_describe_test.go`
- `runtime_contract_test.go`
- new `runtime_boundary_test.go` if the cases do not fit existing files

Tests to add:
- mutating `Module.Metadata(...)` after `Build` does not change
  `Runtime.Describe` or `Runtime.ExportContract`
- mutating query/view declaration input slices after registration does not
  change module descriptions or runtime contracts
- mutating migration/permission/read-model input slices after registration does
  not change module descriptions or runtime contracts
- mutating values returned from `Module.Describe`, `Runtime.Describe`,
  `ExportSchema`, or `ExportContract` does not change later exports
- `Build` still rejects nil modules, blank module names, invalid config, and
  invalid declaration names before exposing a runtime
- `Start` and `Close` behavior is unchanged after the internal boundary cleanup

Test boundaries:
- do not introduce new exported V2 API just to make tests pass
- do not assert multi-module behavior
- do not assert process isolation
- do not assert executable migration or policy behavior
