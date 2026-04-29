# V2-B Task 03: Implement Contract Workflow Helpers

Parent plan: `docs/features/V2/V2-B/00-current-execution-plan.md`

Objective: provide reusable workflow code for scripts, CI, and any CLI wrapper.

Implementation direction:
- done: read canonical `ModuleContract` JSON from files
- done: call `contractdiff.CompareJSON` for diffs
- done: call `contractdiff.CheckPolicy` for warnings/strict status
- done: call `codegen.GenerateFromJSON` for supported generators
- done: write deterministic text/JSON outputs
- done: keep helper APIs usable from app-owned binaries

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

Implemented package:
- `contractworkflow`

Implemented API surface:
- `CompareFiles(previousPath, currentPath string)`
- `CheckPolicyFiles(previousPath, currentPath string, opts contractdiff.PolicyOptions)`
- `GenerateFromFile(contractPath string, opts codegen.Options)`
- `GenerateFile(contractPath, outputPath string, opts codegen.Options)`
- `FormatDiff(report contractdiff.Report, format string)`
- `FormatPolicy(result contractdiff.PolicyResult, format string)`
- `FormatText`, `FormatJSON`, and `ErrUnsupportedFormat`

Notes:
- unsupported generator language errors wrap the existing
  `codegen.ErrUnsupportedLanguage` sentinel.
- malformed diff/policy input keeps using
  `contractdiff.ErrInvalidContractJSON`.
