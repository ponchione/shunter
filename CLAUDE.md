# Shunter

Read `RTK.md` first for command rules.

## What to Use

- `README.md` = product and repo orientation
- `docs/decomposition/EXECUTION-ORDER.md` = implementation sequencing and dependency gates
- `docs/decomposition/` = executable epic/story breakdown
- `reference/SpacetimeDB/` = read-only research input only

## Rules

- Keep the clean-room boundary: do not copy or port Rust code from `reference/SpacetimeDB/`.
- Treat `docs/decomposition/EXECUTION-ORDER.md` as the sequencing authority when choosing what lands next.
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
  - pinned Staticcheck with `rtk go tool staticcheck ./...` when static
    analysis is relevant; after OI-008 cleanup it is expected to pass
  - `rtk go fmt` on touched code
- Do not report a Go change as finished until those checks pass.
