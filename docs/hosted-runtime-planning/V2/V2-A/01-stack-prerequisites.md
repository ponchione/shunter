# V2-A Task 01: Reconfirm Runtime/Module Boundary Prerequisites

Parent plan: `docs/hosted-runtime-planning/V2/V2-A/00-current-execution-plan.md`

Objective: verify V2-A is stacked on the live v1/v1.5 runtime owner and does
not reopen completed V1 or V1.5 slices.

Checks:
- `rtk go doc . Module`
- `rtk go doc . Runtime`
- `rtk go doc . Build`
- `rtk go doc . Module.Describe`
- `rtk go doc . Runtime.Describe`
- `rtk go doc . Runtime.ExportContract`
- `rtk go doc ./schema Engine`
- `rtk go doc ./executor Executor`
- `rtk go doc ./subscription Manager`
- `rtk go doc ./protocol Server`

Read only if needed:
- `runtime.go`
- `runtime_build.go`
- `runtime_lifecycle.go`
- `runtime_network.go`
- `runtime_contract.go`
- `module.go`
- `module_declarations.go`

Prerequisite conclusions to record in Task 01:
- `Build` validates and snapshots a `Module` into a `Runtime`
- runtime-owned subsystem handles are unexported
- `Describe`, `ExportSchema`, and `ExportContract` return detached data
- `Start` and `Close` own lifecycle goroutines/resources
- V2-A should harden the boundary before adding multi-module or process
  isolation work

Stop if:
- the root hosted-runtime APIs are missing or failing focused tests
- ongoing V1/V1.5 changes make boundary behavior ambiguous
