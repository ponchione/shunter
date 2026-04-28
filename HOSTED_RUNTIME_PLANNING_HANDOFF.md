# Hosted runtime planning handoff

Use this file when the task is hosted-runtime planning or implementation. Use `NEXT_SESSION_HANDOFF.md` instead for TECH-DEBT / parity work.

## Current hosted-runtime state

V1-H is audited as landed.

Live proof points:
- root package imports as `github.com/ponchione/shunter`
- `Module`, `Config`, `Runtime`, and `Build(...)` exist
- `Runtime.Start`, `Close`, `HTTPHandler`, `ListenAndServe`, local calls, describe, and schema export exist
- the prior bundled hello-world command has been removed because it no longer served a maintained product or integration purpose
- root/runtime package tests are now the live proof for hosted-runtime ownership, serving, local calls, describe, export, and lifecycle behavior

Docs updated with this state:
- `README.md`
- `docs/hosted-runtime-implementation-roadmap.md`
- `docs/decomposition/hosted-runtime-v1-contract.md`
- `docs/decomposition/hosted-runtime-version-phases.md`
- `docs/decomposition/APP-RUNTIME-LAYER-AND-USAGE-SURFACE.md`
- `docs/decomposition/EXECUTION-ORDER.md`
- `TECH-DEBT.md`

## Validation from the V1-H audit

Commands run:
- `rtk go list .`
- `rtk go doc . Module`
- `rtk go doc . NewModule`
- `rtk go doc . Runtime`
- `rtk go doc . Runtime.Start`
- `rtk go doc . Runtime.ListenAndServe`
- `rtk go doc . Runtime.HTTPHandler`
- `rtk go doc . Runtime.ExportSchema`
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -count=1`
- `rtk go build ./...`

Pinned Staticcheck is available as `rtk go tool staticcheck ./...`. Use it for
static-analysis visibility, but do not treat a broad green run as required
until OI-008 cleanup clears the known findings and any dirty compile blockers.

Before taking further hosted-runtime implementation work, rerun the focused tests for the surfaces you touch.

## Next hosted-runtime direction

If continuing hosted-runtime work, start from the V1.5-A query/view declaration
plan.
The implementation-planning decomposition now lives under
`docs/hosted-runtime-planning/V1.5/`.

The next plan should stay narrow:
- decide the smallest declared read surface that attaches to `shunter.Module`
- decide how declarations appear in existing `Describe` / export foundations without implementing full canonical contract JSON yet
- add tests first for module-owned declaration metadata
- keep codegen, client bindings, permissions metadata, migration metadata, and v2 runtime boundary work out of the first V1.5-A slice
- preserve WebSocket-first v1 runtime behavior

Do not reopen V1-A through V1-H unless a new failing regression proves drift.

## Startup reading for hosted-runtime work

Required:
1. `RTK.md`
2. this file

Then inspect the live root package and the specific hosted-runtime files you will touch with Go tools.

Open these only when needed:
- `docs/decomposition/hosted-runtime-version-phases.md` for phase boundaries
- `docs/decomposition/hosted-runtime-v1-contract.md` or `docs/decomposition/hosted-runtime-v1.5-follow-ons.md` for contract questions
- the relevant `docs/hosted-runtime-planning/` plan for an active slice

Use `rtk` for every shell command. Do not push unless explicitly asked.
