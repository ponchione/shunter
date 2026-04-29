# Hosted Runtime V1-G Current Execution Plan

Goal: implement V1-G export/introspection foundation after V1-F local runtime calls.

Task sequence:
1. Reconfirm live prerequisites and schema export contracts.
2. Add RED tests for module/runtime/schema description APIs.
3. Implement `Module.Describe`, `Runtime.ExportSchema`, and `Runtime.Describe` without widening into v1.5 contract export.
4. Format and validate V1-G gates.

Task progress:
- Task 01 complete: live root/runtime/schema export contracts were reconfirmed with `rtk go doc`.
- Task 02 complete: RED V1-G tests added in `runtime_describe_test.go` and confirmed failing on missing `Module.Describe`, `Runtime.ExportSchema`, and `Runtime.Describe` APIs before implementation.
- Task 03 complete: `runtime_describe.go` now exposes detached `ModuleDescription`, `RuntimeDescription`, `Module.Describe`, `Runtime.ExportSchema`, and `Runtime.Describe`; `runtime.go` preserves module version/metadata on built runtimes.
- Task 04 complete: V1-G focused tests, root tests, vet, and Go doc checks passed.

Latest Task 04 validation:
- `rtk go fmt .` -> passed.
- `rtk go test . -run 'Test(ModuleDescribe|RuntimeExportSchema|RuntimeDescribe)' -count=1` -> passed, 4 tests.
- `rtk go test . -count=1` -> passed, 63 tests.
- `rtk go test ./... -count=1` -> passed, 1894 tests across 12 packages.
- `rtk go vet .` -> passed.
- `rtk go doc . Module.Describe` -> passed.
- `rtk go doc . Runtime.ExportSchema` -> passed.
- `rtk go doc . Runtime.Describe` -> passed.

Historical sequencing note: later hosted-runtime slices have since landed. Do
not treat this completed V1-G plan as a live handoff; use
`HOSTED_RUNTIME_PLANNING_HANDOFF.md` for current hosted-runtime status.
