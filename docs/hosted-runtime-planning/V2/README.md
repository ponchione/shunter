# Hosted Runtime V2 Planning

Status: draft decomposition
Scope: implementation-facing slice plans for hosted-runtime v2 structural work.

Source docs:
- `docs/decomposition/hosted-runtime-v2-directions.md`
- `docs/decomposition/hosted-runtime-v1-contract.md`
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`
- `docs/decomposition/hosted-runtime-version-phases.md`
- `HOSTED_RUNTIME_PLANNING_HANDOFF.md`

V2 starts from the live v1/v1.5 codebase. It should not reopen landed
hosted-runtime work unless a failing regression proves drift.

## Slice Order

1. `V2-A`: runtime/module boundary hardening
2. `V2-B`: contract artifact admin and CLI workflows
3. `V2-C`: migration planning and validation
4. `V2-D`: declared read and SQL protocol convergence
5. `V2-E`: policy/auth enforcement foundation
6. `V2-F`: multi-module hosting exploration
7. `V2-G`: out-of-process module execution gate

This keeps the dependency chain explicit:
- boundary hardening makes later structural work safer
- admin/CLI workflows reuse existing contract/codegen/diff packages
- migration planning builds on canonical contract snapshots and diffs
- declared read execution must reconcile with the live SQL protocol surface
- policy enforcement depends on clear read/write surfaces and auth claims
- multi-module hosting depends on module identity, routing, lifecycle, and
  contract namespacing
- process isolation depends on a proven runtime/module boundary

## Boundary Rules

V2 must preserve:
- one-module hosting as the simple default
- top-level `shunter.Module`, `shunter.Config`, `shunter.Runtime`, and
  `shunter.Build(...)` as the normal app-facing surface
- WebSocket-first external client behavior
- statically linked Go module authoring as the default path
- canonical `ModuleContract` JSON as the source of truth until a real consumer
  split exists
- non-blocking runtime startup for descriptive migration metadata

V2 must not start by adding:
- cloud or fleet control-plane assumptions
- mandatory multi-module hosting
- mandatory out-of-process module execution
- dynamic plugin loading
- cross-language module authoring
- automatic startup migrations
- a broad policy language detached from reducers, queries, and views

## Execution Granularity

Proceed one lettered slice at a time.
A normal implementation run should complete the whole active `V2-*` slice:
1. read the slice execution plan and task docs
2. run prerequisite inspection commands
3. add planned failing tests
4. implement the slice
5. run focused validation gates
6. update the slice plan and handoff state

Do not start a later `V2-*` slice until the previous slice is either complete
or explicitly deferred with a recorded reason.

## Validation Posture

Each implementation slice should:
- inspect live Go symbols with `rtk go doc` before coding
- add failing tests before implementation
- keep changes scoped to the active slice
- run focused package tests first
- run `rtk go fmt` on touched packages
- run `rtk go vet` for touched packages when behavior, exported APIs, or
  interfaces changed
- expand to `rtk go test ./... -count=1` when root/runtime behavior or shared
  contracts change

Pinned Staticcheck is expected to be green after OI-008 cleanup:
- `rtk go tool staticcheck ./...`
