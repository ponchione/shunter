# V2-B Task 01: Reconfirm Contract Tooling Prerequisites

Parent plan: `docs/features/V2/V2-B/00-current-execution-plan.md`

Objective: verify the existing reusable contract tooling before adding
admin/CLI workflows.

Checks:
- passed: `rtk go doc . ModuleContract`
- passed: `rtk go doc . Runtime.ExportContractJSON`
- passed: `rtk go doc . ModuleContract.MarshalCanonicalJSON`
- passed: `rtk go doc ./codegen GenerateFromJSON`
- passed: `rtk go doc ./codegen Options`
- passed: `rtk go doc ./contractdiff CompareJSON`
- passed: `rtk go doc ./contractdiff CheckPolicy`
- passed: `rtk go doc ./contractdiff PolicyOptions`

Read only if needed:
- `runtime_contract.go`
- `codegen/typescript.go`
- `contractdiff/`

Prerequisite conclusions to record in Task 01:
- generic tools can operate on canonical JSON files without importing an app
  module
- generic tools cannot export a live app module contract unless the app binary
  links that module
- V2-B should not pretend dynamic module loading exists
- command output must be deterministic enough for CI and review

Stop if:
- canonical contract JSON or `contractdiff` package behavior is unstable
- ongoing codegen/contractdiff changes alter the expected package API

Recorded conclusions:
- generic tools can operate on canonical JSON files without importing an app
  module.
- generic tools cannot export a live app module contract unless the app binary
  links that module.
- V2-B did not add or imply dynamic module loading.
- command output is deterministic enough for CI and review because reports are
  sorted by `contractdiff` and rendered by stable text/JSON formatters.
