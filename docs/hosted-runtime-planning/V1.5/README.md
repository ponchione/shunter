# Hosted Runtime V1.5 Planning

Status: draft decomposition
Scope: bite-sized implementation plans for the v1.5 hosted-runtime follow-ons.

Source docs:
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`
- `docs/decomposition/hosted-runtime-version-phases.md`
- `docs/hosted-runtime-implementation-roadmap.md`
- `HOSTED_RUNTIME_PLANNING_HANDOFF.md`

V1.5 starts after the v1 hosted-runtime owner is live. It should improve
developer ergonomics and platform usability without reopening the v1 runtime
shape.

## Slice Order

1. `V1.5-A`: query/view declarations
2. `V1.5-B`: canonical contract export and JSON snapshot output
3. `V1.5-C`: client bindings and codegen
4. `V1.5-D`: permissions/read-model metadata
5. `V1.5-E`: migration metadata, contract diffs, and warning policy checks

This keeps the dependency chain explicit:
- declarations define the exported read surface
- contract export makes schema, reducers, queries, and views inspectable
- codegen consumes the contract
- permissions annotate declared read/write surfaces
- migration metadata and diff tooling use the same canonical artifact

## Boundary Rules

V1.5 must not change these v1 decisions:
- hosted-first runtime/server identity
- one-runtime / one-module primary model
- top-level `shunter` API as the normal surface
- WebSocket-first external client model
- narrow runtime config boundary
- statically linked Go module model in v1

V1.5 must not implement:
- full SQL/view system
- executable migration runners
- runtime-blocking migration metadata enforcement
- broad standalone policy/auth framework
- server/module implementation generation
- all-language SDK generation
- multi-module hosting
- out-of-process module execution
- cloud/control-plane expansion

## Validation Posture

Each implementation slice should:
- inspect the live root package with `rtk go doc` before coding
- add failing tests before implementation
- keep changes scoped to the active slice
- run focused package tests first
- run `rtk go fmt` and `rtk go vet` for touched packages
- expand to `rtk go test ./... -count=1` when root/runtime contracts change

