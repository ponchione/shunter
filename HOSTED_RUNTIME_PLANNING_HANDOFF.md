# Hosted runtime planning handoff

Use this file when the task is hosted-runtime planning or implementation. Use `NEXT_SESSION_HANDOFF.md` instead for TECH-DEBT / parity work.

## Current hosted-runtime state

V1-H is audited as landed.

Live proof points:
- root package imports as `github.com/ponchione/shunter`
- `Module`, `Config`, `Runtime`, and `Build(...)` exist
- `Runtime.Start`, `Close`, `HTTPHandler`, `ListenAndServe`, local calls, describe, and schema export exist
- `cmd/shunter-example` is now the normal hosted-runtime hello-world path
- the example defines `greetings` and `say_hello` through `shunter.Module`
- the example builds/serves through `shunter.Build` and `Runtime.ListenAndServe(ctx)`
- the example test proves recovery, WebSocket dev admission, subscribe, reducer call, non-caller live update delivery, shutdown, and no manual kernel-assembly regression

Docs updated with this state:
- `README.md`
- `docs/hosted-runtime-bootstrap.md`
- `docs/hosted-runtime-implementation-roadmap.md`
- `docs/decomposition/hosted-runtime-v1-contract.md`
- `docs/decomposition/hosted-runtime-version-phases.md`
- `docs/decomposition/APP-RUNTIME-LAYER-AND-USAGE-SURFACE.md`
- `docs/decomposition/EXECUTION-ORDER.md`
- `TECH-DEBT.md` (`OI-014` closed)

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
- `rtk go fmt . ./cmd/shunter-example`
- `rtk go test ./cmd/shunter-example -count=1`
- `rtk go test . -count=1`
- `rtk go vet ./cmd/shunter-example .`
- `rtk go test ./... -count=1`
- `rtk go build ./...`

Before taking further hosted-runtime implementation work, rerun the focused tests for the surfaces you touch.

## Next hosted-runtime direction

If continuing hosted-runtime work, do planning first for V1.5-A query/view declarations.

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
- `docs/hosted-runtime-bootstrap.md` for the current quickstart surface
- `docs/decomposition/hosted-runtime-version-phases.md` for phase boundaries
- `docs/decomposition/hosted-runtime-v1-contract.md` or `docs/decomposition/hosted-runtime-v1.5-follow-ons.md` for contract questions
- the relevant `docs/hosted-runtime-planning/` plan for an active slice

Use `rtk` for every shell command. Do not push unless explicitly asked.
