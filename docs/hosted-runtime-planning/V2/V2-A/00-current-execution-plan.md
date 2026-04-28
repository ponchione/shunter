# Hosted Runtime V2-A Current Execution Plan

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

Immediate next V2 slice after V2-A: V2-B contract artifact admin and CLI
workflows.
