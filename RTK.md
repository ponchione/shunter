# RTK - Rust Token Killer (Codex CLI)

**Usage**: Token-optimized CLI proxy for shell commands.

## Rule

Always prefix shell commands with `rtk`.

Prefer RTK-native subcommands when available instead of wrapping the underlying tool directly.

Exception: run Go benchmarks directly with `go test -bench ...` instead of
`rtk go test -bench ...`. RTK summarizes benchmark commands and may suppress
the raw `Benchmark... ns/op B/op allocs/op` lines needed for performance work.

## Preferred command mapping

- Directory listing/tree/search: `rtk ls`, `rtk tree`, `rtk find`
- File reading/search: `rtk read`, `rtk grep`
- Git/GitHub: `rtk git`, `rtk gh`
- Tests/builds/checks: `rtk test`, `rtk go`, `rtk cargo`, `rtk pytest`, `rtk vitest`, `rtk tsc`, `rtk lint`, `rtk next`
- Pinned Go static analysis: `rtk go tool staticcheck ./...`
- Logs/errors/diffs/summaries: `rtk log`, `rtk err`, `rtk diff`, `rtk summary`

Examples:

```bash
rtk git status
rtk grep "pattern" docs
rtk find docs -name "*.md"
rtk cargo test
rtk npm run build
rtk pytest -q
rtk go build
rtk go test
rtk go tool staticcheck ./...
```

## Verification

```bash
rtk --version
rtk gain
which rtk
```
