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

## Records

### v1.1.0 - 2026-05-13

- Status: passed
- Operator: gernsback
- Date/time: 2026-05-13T18:00:09Z
- Environment: Linux gernsback 6.17.0-23-generic, linux/amd64,
  Go go1.26.3, AMD Ryzen 9 9900X 12-Core Processor
- Shunter ref: final `v1.1.0` release tag on the release metadata commit;
  qualification began from `947e3dd2eb4dda1738abd670009845f127a745e0`
- Shunter worktree state: dirty during qualification with release metadata,
  docs, evidence logs, and build-info version fix; clean expected before tag
- `opsboard-canary` ref: `a45ec6f4493148543968339a0060890051430e4a`
- `opsboard-canary` worktree state: dirty; existing local canary changes plus
  generated TypeScript refresh, protocol v2 metadata assertion update, npm lock
  refresh, and reproducibility-script adjustment used by this qualification run

Commands:

| Scope | Working directory | Command | Result | Evidence |
| --- | --- | --- | --- | --- |
| Shunter | `/home/gernsback/source/shunter` | `rtk go test ./...` | pass | `working-docs/release-evidence/v1.1.0/shunter-go-test.log` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go vet ./...` | pass | `working-docs/release-evidence/v1.1.0/shunter-go-vet.log` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go tool staticcheck ./...` | pass | `working-docs/release-evidence/v1.1.0/shunter-staticcheck.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client test` | pass | `working-docs/release-evidence/v1.1.0/typescript-client-test.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run build` | pass | `working-docs/release-evidence/v1.1.0/typescript-client-build.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run pack:dry-run` | pass | `working-docs/release-evidence/v1.1.0/typescript-client-pack-dry-run.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run smoke:package` | pass | `working-docs/release-evidence/v1.1.0/typescript-client-smoke-package.log` |
| Performance | `/home/gernsback/source/shunter` | `go test -run '^$' -bench . -benchmem -count=10 . ./executor ./protocol ./commitlog ./subscription` | pass | `working-docs/release-evidence/v1.1.0/shunter-bench-raw.log` |
| Performance | `/home/gernsback/source/shunter` | `rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/v1.1.0/shunter-bench-raw.log` | pass | `working-docs/release-evidence/v1.1.0/shunter-benchstat.log` |
| Canary | `/home/gernsback/source/opsboard-canary` | `rtk make codegen` | pass | `working-docs/release-evidence/v1.1.0/canary-codegen.log` |
| Canary | `/home/gernsback/source/opsboard-canary` | `rtk make canary-quick` | pass | `working-docs/release-evidence/v1.1.0/canary-quick.log` |
| Canary | `/home/gernsback/source/opsboard-canary` | `rtk make canary-full` | pass | `working-docs/release-evidence/v1.1.0/canary-full.log` |
| Release binary | `/home/gernsback/source/shunter` | `rtk go build -ldflags "-X github.com/ponchione/shunter.Version=v1.1.0 -X github.com/ponchione/shunter.Commit=<release-commit> -X github.com/ponchione/shunter.Date=<utc-rfc3339>" -o /tmp/shunter-v1.1.0 ./cmd/shunter` | pass | `/tmp/shunter-v1.1.0-release-build.log` |
| Release binary | `/home/gernsback/source/shunter` | `rtk /tmp/shunter-v1.1.0 version` | pass | `/tmp/shunter-v1.1.0-release-version.log` |

Superseded attempts:

- Initial `rtk make canary-quick` failed because committed canary TypeScript
  bindings were stale; see
  `working-docs/release-evidence/v1.1.0/canary-quick-initial-fail.log`.
- A second `rtk make canary-quick` failed because the canary metadata assertion
  still expected protocol v1 defaults; see
  `working-docs/release-evidence/v1.1.0/canary-quick-after-codegen-fail.log`.
- Initial `rtk make canary-full` failed because the canary reproducibility
  script compared regenerated files against the Git index while the canary
  checkout was intentionally dirty; see
  `working-docs/release-evidence/v1.1.0/canary-full-index-diff-fail.log`.

Residual risks:

- `opsboard-canary` qualification used a dirty sibling checkout with local
  canary maintenance changes that are not part of this Shunter tag.
- Performance rows remain advisory; external canary-scale fanout, workload
  distributions, canary-scale backup/restore timing, and production-sized
  memory profiles remain future measurement gaps.

Decision:

- Accepted for `v1.1.0`.
