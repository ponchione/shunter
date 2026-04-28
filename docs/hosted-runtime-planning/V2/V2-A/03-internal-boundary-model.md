# V2-A Task 03: Introduce The Internal Boundary Model

Parent plan: `docs/hosted-runtime-planning/V2/V2-A/00-current-execution-plan.md`

Objective: reduce implicit coupling inside `Runtime` while preserving the
existing public API.

Implementation direction:
- group app-authored module identity and declarations behind an internal
  snapshot type
- keep schema engine, registry, recovered state, reducer registry,
  subscriptions, protocol graph, and lifecycle-owned resources runtime-owned
- keep defensive copies at module registration, build, description, and
  contract export boundaries
- prefer private helpers/types unless an exported type is required by a
  concrete later slice

Public API boundaries:
- keep `shunter.Module`, `Config`, `Runtime`, and `Build(...)`
- keep `Runtime.Describe`, `ExportSchema`, `ExportContract`, and
  `ExportContractJSON`
- keep `Start`, `Close`, `ListenAndServe`, `HTTPHandler`, `CallReducer`, and
  `Read`

Do not add:
- `Host`
- multi-module routing
- dynamic module loading
- process RPC
- CLI/control-plane commands
