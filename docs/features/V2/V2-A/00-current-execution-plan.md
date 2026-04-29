# Hosted Runtime V2-A Current Execution Plan

Status: complete as of 2026-04-28.

Goal: make the in-process runtime/module boundary explicit enough for later v2
work without changing the public app-author API.

V2-A target:
- clarify what belongs to the app-authored module definition
- clarify what belongs to the built runtime owner
- preserve defensive copies between `Module` and `Runtime`
- preserve detached `Describe`, `ExportSchema`, and `ExportContract` outputs
- keep one statically linked Go module and one runtime as the supported simple
  mode

Task sequence:
1. Reconfirm live root runtime, module, schema, executor, subscription, and
   protocol contracts.
2. Add failing tests for module snapshot isolation and runtime boundary
   invariants.
3. Introduce the smallest internal boundary model needed to reduce implicit
   runtime field coupling.
4. Preserve public descriptions, contract export, and lifecycle behavior.
5. Format and validate V2-A gates.

Scope boundaries:
- In scope: internal owner graph cleanup, defensive-copy tests, boundary
  invariants, preserving existing public APIs.
- Out of scope: multi-module hosting, process isolation, dynamic module
  loading, control-plane APIs, migration execution, policy enforcement, query
  language expansion.

Historical sequencing note: later hosted-runtime slices have since landed. Do
not treat this completed V2-A plan as a live handoff; use
`HOSTED_RUNTIME_PLANNING_HANDOFF.md` for current hosted-runtime status.

## Completion Proof

- `Runtime` now groups app-authored module identity, reducer declaration
  metadata, query/view declarations, migration metadata, and table migrations
  behind the private `moduleSnapshot`.
- `Build` creates the module snapshot after validation and before returning the
  runtime; later mutation of the original `Module` does not affect runtime
  describe or contract exports.
- Runtime-owned schema engine, registry, committed state, reducer registry,
  subscription manager, executor, scheduler, protocol graph, and lifecycle
  resources remain separate runtime-owned fields.
- `Runtime.Describe` and `Runtime.ExportContract` use snapshot helpers that
  return detached copies; `Runtime.ExportSchema` remains a detached schema
  export from the runtime-owned engine.
- `runtime_boundary_test.go` pins build-time module snapshot isolation,
  internal snapshot detachment, and deep `Runtime.Describe` detachment.

Validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Boundary|Describe|Export|Contract|Lifecycle|Build)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -count=1`
