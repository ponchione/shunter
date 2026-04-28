# V2-F Task 01: Reconfirm Multi-Module Prerequisites

Parent plan: `docs/hosted-runtime-planning/V2/V2-F/00-current-execution-plan.md`

Objective: verify that current one-module runtime behavior is stable before
adding any host abstraction.

Checks:
- `rtk go doc . Runtime`
- `rtk go doc . Config`
- `rtk go doc . Runtime.Start`
- `rtk go doc . Runtime.Close`
- `rtk go doc . Runtime.HTTPHandler`
- `rtk go doc . Runtime.ListenAndServe`
- `rtk go doc . Runtime.ExportContract`
- `rtk go doc ./protocol Server`
- `rtk go doc ./commitlog OpenAndRecoverDetailed`

Read only if needed:
- `runtime.go`
- `runtime_lifecycle.go`
- `runtime_network.go`
- `runtime_contract.go`
- `runtime_build.go`

Prerequisite conclusions to record in Task 01:
- current `Runtime` owns exactly one module
- `Config.DataDir` and listen/handler behavior are runtime-scoped
- protocol routes are currently mounted under one runtime handler
- per-module contracts already have module identity
- aggregate contracts do not exist

Stop if:
- one-module lifecycle or serving tests are failing
- V2-A did not land a clear enough boundary to build on
