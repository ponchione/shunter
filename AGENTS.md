# AGENTS.md

Default startup reading:
1. `RTK.md`
2. Package docs, code, and narrow spec sections named by the user task or by
   the surface you are touching

Do not read broad specs by default. Open them only when the active task says
they are required, when a dependency question cannot be answered from code, or
when you are editing that document.

## Repo Reality

- This repo is no longer docs-only. It contains substantial Go implementation across the core subsystem packages.
- The numbered specs under `docs/specs/*/SPEC-*.md` are baseline contracts, but
  they must be checked against live code.
- Do not act like there is no codebase; do not assume the docs are perfectly current either.

## Agent Rules

- Stay inside the assigned slice.
- Use numbered specs for cross-subsystem contracts only when the active task
  needs them.
- Keep docs concise and implementation-facing.
- Keep `reference/SpacetimeDB/` read-only and research-only; never copy source from it.
- Do not add speculative scaffolding or repo structure early.

## Versioning Standard

- Shunter's repo/tool/runtime version lives in `VERSION` and uses v-prefixed
  SemVer, such as `v0.1.0-dev` during development and `v0.1.0` for a release.
- Use `vX.Y.Z` git tags for released versions. Keep normal development on a
  `-dev` version unless the task is explicitly cutting a release.
- Stamp release binaries with linker variables rather than editing generated
  build metadata:
  - `github.com/ponchione/shunter.Version`
  - `github.com/ponchione/shunter.Commit`
  - `github.com/ponchione/shunter.Date`
- The root `shunter.CurrentBuildInfo()` API and `shunter version` CLI output
  are the supported places to expose Shunter build metadata.
- Do not confuse Shunter's release version with application module versions:
  `Module.Version(...)` is app-owned metadata exported into `ModuleContract`
  artifacts, not the Shunter runtime/tool version.
- Update `CHANGELOG.md` when changing release-facing behavior or preparing a
  release.

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
  - run pinned Staticcheck with `rtk go tool staticcheck ./...` when static
    analysis is relevant; pinned Staticcheck is expected to pass
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
- `rtk go tool staticcheck ./...`
- `rtk grep "pattern" docs`

## If docs disagree

Resolve in this order:
1. task-specific user instruction
2. live code and tests
3. relevant spec files for scope and contracts
4. `README.md` for product intent
