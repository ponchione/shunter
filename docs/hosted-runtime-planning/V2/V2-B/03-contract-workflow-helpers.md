# V2-B Task 03: Implement Contract Workflow Helpers

Parent plan: `docs/hosted-runtime-planning/V2/V2-B/00-current-execution-plan.md`

Objective: provide reusable workflow code for scripts, CI, and any CLI wrapper.

Implementation direction:
- read canonical `ModuleContract` JSON from files
- call `contractdiff.CompareJSON` for diffs
- call `contractdiff.CheckPolicy` for warnings/strict status
- call `codegen.GenerateFromJSON` for supported generators
- write deterministic text/JSON outputs
- keep helper APIs usable from app-owned binaries

Design constraints:
- prefer package APIs over shelling out internally
- keep file IO and report formatting separate from core diff/codegen logic
- do not duplicate contractdiff or codegen classification logic
- preserve existing error sentinels where possible

Do not implement:
- runtime startup
- module import/plugin loading
- cloud/project configuration
- backup/restore
