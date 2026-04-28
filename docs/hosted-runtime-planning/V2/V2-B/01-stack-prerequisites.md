# V2-B Task 01: Reconfirm Contract Tooling Prerequisites

Parent plan: `docs/hosted-runtime-planning/V2/V2-B/00-current-execution-plan.md`

Objective: verify the existing reusable contract tooling before adding
admin/CLI workflows.

Checks:
- `rtk go doc . ModuleContract`
- `rtk go doc . Runtime.ExportContractJSON`
- `rtk go doc . ModuleContract.MarshalCanonicalJSON`
- `rtk go doc ./codegen GenerateFromJSON`
- `rtk go doc ./codegen Options`
- `rtk go doc ./contractdiff CompareJSON`
- `rtk go doc ./contractdiff CheckPolicy`
- `rtk go doc ./contractdiff PolicyOptions`

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
