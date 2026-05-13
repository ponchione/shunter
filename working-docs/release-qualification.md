# Release Qualification Ledger

Status: current release evidence ledger
Scope: durable Shunter release qualification inputs and outcomes.

Use this file for every release candidate or final release decision. Add the
record before tagging whenever release notes claim qualification passed. Keep
failed or superseded candidate records instead of replacing them; a final
record can reference earlier failed records.

## Required Evidence

Each record must include:

- release candidate, release tag, or source version being qualified
- Shunter commit or release tag under test, plus worktree state
- `opsboard-canary` commit under test, plus worktree state
- UTC date/time, operator, and execution environment notes
- exact commands run, working directory, result, and evidence path
- final release decision, including accepted residual risks or `None`

## Current Minimum Command Set

Run from the Shunter repository:

```bash
rtk go test ./...
rtk go vet ./...
rtk go tool staticcheck ./...
rtk npm --prefix typescript/client test
rtk npm --prefix typescript/client run build
rtk npm --prefix typescript/client run pack:dry-run
rtk npm --prefix typescript/client run smoke:package
```

Run from the sibling `opsboard-canary` checkout:

```bash
rtk make canary-quick
rtk make canary-full
```

If a release adds, removes, or narrows a command, record the exact command-set
delta and the reason in that release record.

## Record Template

```markdown
### vX.Y.Z-rc.N - YYYY-MM-DD

- Status: pending | passed | failed | superseded
- Operator:
- Date/time: YYYY-MM-DDTHH:MM:SSZ
- Environment:
- Shunter ref:
- Shunter worktree state:
- `opsboard-canary` ref:
- `opsboard-canary` worktree state:

Commands:

| Scope | Working directory | Command | Result | Evidence |
| --- | --- | --- | --- | --- |
| Shunter | `/path/to/shunter` | `rtk go test ./...` | pass/fail/skipped | log or artifact path |
| Canary | `/path/to/opsboard-canary` | `rtk make canary-quick` | pass/fail/skipped | log or artifact path |

Residual risks:

- None.

Decision:

- Pending, accepted, rejected, or superseded. Include the release tag if
  accepted.
```

## Historical Notes

The `v1.0.0` and `v1.0.1` changelog entries say their release qualification
passed before this ledger existed. Their exact Shunter and `opsboard-canary`
inputs are not reconstructed here. Future release qualification records must
capture those inputs directly in this file.

## Records

No post-ledger release qualification records have been added yet.
