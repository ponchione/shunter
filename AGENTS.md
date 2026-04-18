# AGENTS.md

Read in this order:
1. `RTK.md`
2. `docs/project-brief.md`
3. `docs/EXECUTION-ORDER.md`
4. The relevant spec and decomposition files for the slice you are touching

## Repo Reality

- This repo is no longer docs-only. It contains substantial Go implementation across the core subsystem packages.
- The implementation plan still lives in `docs/EXECUTION-ORDER.md` and `docs/decomposition/`, but those docs must now be checked against live code.
- Do not act like there is no codebase; do not assume the docs are perfectly current either.

## Agent Rules

- Stay inside the assigned slice.
- Use the execution order document for sequencing and dependency checks.
- Use decomposition stories/epics for concrete scope.
- Keep docs concise and implementation-facing.
- Keep `reference/SpacetimeDB/` read-only and research-only; never copy source from it.
- Do not add speculative scaffolding or repo structure early.

## Commands

`RTK.md` is mandatory for shell usage.

- Use RTK for shell commands.
- Prefer RTK-native subcommands from `RTK.md` when available; otherwise wrap the underlying command with `rtk`.
- Prefix every git command with `rtk`.

Examples:
- `rtk git status`
- `rtk go test ./...`
- `rtk grep "pattern" docs`

## If docs disagree

Resolve in this order:
1. task-specific user instruction
2. `docs/EXECUTION-ORDER.md` for sequencing
3. relevant spec/decomposition files for scope and contracts
4. `docs/project-brief.md` for product intent
