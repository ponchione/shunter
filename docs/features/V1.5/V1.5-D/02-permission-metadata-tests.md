# V1.5-D Task 02: Add Failing Permission Metadata Tests

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-D/00-current-execution-plan.md`

Objective: pin narrow permission/read-model metadata as exported declaration
metadata.

Likely files:
- add root package tests for module declaration metadata
- add contract export tests
- add codegen fixture tests only if generated output includes metadata

Tests to add:
- reducer permission metadata can be declared
- query permission/read-model metadata can be declared
- view permission/read-model metadata can be declared
- metadata appears in `Runtime.ExportContract`
- metadata is detached from module internals
- generated bindings can see metadata if V1.5-C has a metadata representation
- absent metadata serializes deterministically

Test boundaries:
- do not require runtime authorization enforcement
- do not add a full policy language
- do not make permissions required for every declaration
- do not implement migration metadata

