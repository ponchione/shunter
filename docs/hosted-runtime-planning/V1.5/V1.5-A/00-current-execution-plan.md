# Hosted Runtime V1.5-A Current Execution Plan

Goal: add the smallest code-first query/view declaration surface to the module
model.

Task sequence:
1. Reconfirm the live v1 root package and export/introspection foundation.
2. Add failing tests for module-owned query/view declaration metadata.
3. Implement the declaration types and module registration methods.
4. Expose declaration metadata through narrow descriptions without adding the
   full canonical contract.
5. Format and validate V1.5-A gates.

Task progress:
- Task 01 pending.
- Task 02 pending.
- Task 03 pending.
- Task 04 pending.
- Task 05 pending.

V1.5-A must stay narrow:
- add named read query declarations
- add named live view/subscription declarations
- keep declarations code-first in Go
- make declarations inspectable/exportable enough for V1.5-B
- do not implement codegen, permissions metadata, migration metadata, or full
  contract JSON
- do not widen the SQL/query engine as part of this slice

Immediate next V1.5 slice after V1.5-A: V1.5-B canonical contract export.

