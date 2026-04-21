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

## Go Workflow

- When working in Go code, prefer Go-native tooling over generic text search.
- Before editing unfamiliar Go code, inspect it with Go tools first:
  - `rtk go doc <pkg>`
  - `rtk go doc <pkg>.<Symbol>`
  - `rtk go doc <pkg>.<Type>.<Method>`
  - `rtk go list -json <pkg>`
  - `rtk go list -json ./...`
- If LSP/editor tooling is available, prefer gopls-backed navigation for Go code before broad grep:
  - go to definition
  - find references
  - find implementations
  - call hierarchy
  - rename
  - code actions / quick fixes
  - diagnostics
- Prefer the Go standard library and existing project patterns before adding new dependencies or helper layers.
- Validate Go changes with the Go toolchain:
  - run targeted tests for touched packages first
  - expand to broader test runs only when needed
  - run `rtk go vet` for touched packages when behavior, exported APIs, or interfaces changed
  - run `rtk go fmt` on touched files/packages before finishing
- Do not claim a Go change is complete until the relevant Go commands pass.

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
