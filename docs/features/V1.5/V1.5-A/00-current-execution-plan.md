# Hosted Runtime V1.5-A Current Execution Plan

Goal: add the smallest code-first query/view declaration surface to the module
model.

Status: complete.

Task sequence:
1. Reconfirm the live v1 root package and export/introspection foundation.
2. Add failing tests for module-owned query/view declaration metadata.
3. Implement the declaration types and module registration methods.
4. Expose declaration metadata through narrow descriptions without adding the
   full canonical contract.
5. Format and validate V1.5-A gates.

Task progress:
- Task 01 complete: live root package, describe, and schema export
  prerequisites reconfirmed.
- Task 02 complete: `module_declarations_test.go` now pins the module-owned
  query/view declaration metadata surface. The focused command fails at the
  missing declaration API/description symbols, as expected before implementation:
  `rtk go test . -run 'Test(Module.*Declaration|Runtime.*Declaration|.*Describe.*Declaration)' -count=1`.
- Task 03 complete: `QueryDeclaration`, `ViewDeclaration`, `Module.Query`, and
  `Module.View` are implemented in the root package.
- Task 04 complete: `Module.Describe` and `Runtime.Describe` expose detached
  query/view declaration summaries.
- Task 05 complete: V1.5-A validation passed:
  `rtk go fmt .`;
  `rtk go test . -run 'Test(Module.*Declaration|Runtime.*Declaration|.*Describe.*Declaration)' -count=1`;
  `rtk go test . -count=1`;
  `rtk go vet .`.

V1.5-A must stay narrow:
- add named read query declarations
- add named live view/subscription declarations
- keep declarations code-first in Go
- make declarations inspectable/exportable enough for V1.5-B
- do not implement codegen, permissions metadata, migration metadata, or full
  contract JSON
- do not widen the SQL/query engine as part of this slice

Historical sequencing note: later hosted-runtime slices have since landed. Do
not treat this completed V1.5-A plan as a live handoff; use
`docs/internal/HOSTED_RUNTIME_PLANNING_HANDOFF.md` for current hosted-runtime status.
