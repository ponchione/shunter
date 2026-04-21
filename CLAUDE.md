# Shunter

Read `RTK.md` first for command rules, then `docs/project-brief.md`, then `docs/EXECUTION-ORDER.md`.

## Current Repo State

This repo started docs-first, but it now contains substantial implementation code alongside the planning docs. The working source of truth is split across:
- the docs tree for intended contracts, sequencing, and clean-room rationale
- the live Go packages for current operational reality

Do not treat this as spec-only anymore. Read the docs first, then verify against the code.

## What to Use

- `docs/project-brief.md` = product and architectural intent
- `docs/EXECUTION-ORDER.md` = implementation sequencing and dependency gates
- `docs/decomposition/` = executable epic/story breakdown
- `reference/SpacetimeDB/` = read-only research input only

## Rules

- Keep the clean-room boundary: do not copy or port Rust code from `reference/SpacetimeDB/`.
- Treat `docs/EXECUTION-ORDER.md` as the sequencing authority when choosing what lands next.
- Keep the repo lean. Do not invent extra structure before the work actually needs it.
- When editing docs, keep them tight and operational.
- For any shell or git command, follow `RTK.md`: use RTK and prefer RTK-native subcommands when available; otherwise prefix the underlying command with `rtk`.

## Go-specific working style

- For unfamiliar Go code, inspect packages and symbols with Go-native tools first (`rtk go doc`, `rtk go list -json`) before broad text search.
- If gopls/editor navigation is available, prefer definition/references/implementations/call-hierarchy over grep for symbol-level investigation.
- Prefer standard library solutions and existing repo patterns before introducing new dependencies.
- Before concluding a Go task, run the relevant validation:
  - targeted `rtk go test` for touched packages
  - `rtk go vet` when interfaces or behavior changed
  - `rtk go fmt` on touched code
- Do not report a Go change as finished until those checks pass.

## Practical Default

Before starting a task, identify the exact spec/epic/story slice it belongs to and work only that slice unless explicitly asked to widen scope.