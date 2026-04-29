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

Recorded conclusions:
- `Runtime` remains a single-module owner with a private module snapshot,
  runtime-scoped config, lifecycle resources, protocol graph, and state.
- `Config.DataDir` normalizes to a runtime-owned data directory during `Build`;
  host-level multi-module sharing therefore needs explicit collision checks
  rather than a shared storage namespace.
- `Runtime.HTTPHandler` still mounts protocol traffic at `/subscribe` for one
  runtime. V2-F host routing mounts that existing handler below explicit
  per-module prefixes instead of changing the runtime handler contract.
- `Runtime.ExportContract` already includes canonical module identity, so V2-F
  keeps contracts per module.
- No aggregate contract existed before V2-F, and none was added because host
  health/description diagnostics were sufficient.

Stop if:
- one-module lifecycle or serving tests are failing
- V2-A did not land a clear enough boundary to build on
