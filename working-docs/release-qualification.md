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
rtk bash scripts/static-hosted-binary-gate.sh
```

The TypeScript package commands qualify local package shape, dry-run pack
contents, and packed-install smoke behavior for the current private/local
package workflow. The package smoke gate enforces that the package version
mirrors the Shunter source version without the leading `v`, and checked-in
`dist/` artifacts must match `src/` for the release candidate under test.

These commands do not authorize public npm publishing. Public publishing
requires a separate promotion record that settles `@shunter` package ownership,
release authority, npm access and 2FA policy, publish command policy, package
metadata including licensing, version synchronization, and the final `dist/`
artifact rule.

The static hosted-binary gate wraps the maintained hosted-chat gate and focused
binary-level Go gauntlets. It is the release evidence for the current static
hosted-app shape, strict auth on the built hosted-chat binary, live
subscriptions, clean and restored restarts, and the offline maintenance path:
hosted-chat preflight, no-hook migration, backup/restore, and restored startup.

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

### v1.1.1-dev qualification attempt - 2026-07-10

- Status: failed
- Operator: gernsback
- Date/time: 2026-07-10T10:54:15Z through 2026-07-10T11:07:00Z
- Environment: Linux gernsback 6.17.0-35-generic, linux/amd64,
  Go go1.26.3, AMD Ryzen 9 9900X 12-Core Processor
- Source version: `v1.1.1-dev`
- Shunter ref: `9fddcf0b842f72d5e24e399d558042394b337fbd`
- Shunter worktree state: dirty before qualification with the user-owned
  documentation slice in `working-docs/README.md`,
  `working-docs/deferred-functionality-backlog.md`,
  `working-docs/hosted-backend-roadmap.md`,
  `working-docs/nesl-operational-use-thought-experiment.md`, and
  `working-docs/recommendations/`; qualification-created generated drift was
  restored, leaving those paths plus this ledger/evidence
- `opsboard-canary` ref:
  `e69bce73cb49fbd2334dd8b99eb664b07fc6e132`
- `opsboard-canary` worktree state: clean before and after, local branch
  `master`

Commands:

| Scope | Working directory | Command | Result | Evidence |
| --- | --- | --- | --- | --- |
| Preflight | `/home/gernsback/source/shunter` | required Git, version-file, package-version, CLI-version, operator, UTC, Go, and platform checks | pass; dirty source state recorded | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/preflight.md` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go test ./...` | pass | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/shunter-go-test.log` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go vet ./...` | pass | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/shunter-go-vet.log` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go tool staticcheck ./...` | pass | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/shunter-staticcheck.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client test` | pass | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/typescript-client-test.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run build` | pass; source-map drift restored | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/typescript-client-build.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run pack:dry-run` | pass | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/typescript-client-pack-dry-run.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run smoke:package` | pass | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/typescript-client-smoke-package.log` |
| Hosted binary | `/home/gernsback/source/shunter` | `rtk bash scripts/static-hosted-binary-gate.sh` | pass; generated trailing blank line restored | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/static-hosted-binary-gate.log` |
| Canary | `/home/gernsback/source/opsboard-canary` | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-quick` | fail: stale contract/codegen and obsolete auth-handshake expectation | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/canary-quick.log` |
| Canary | `/home/gernsback/source/opsboard-canary` | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-full` | fail at initial tests; later full stages not run | `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/canary-full.log` |

Failures and supporting evidence:

- The canary mismatch and unchanged canary worktree are documented in
  `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/external-canary-diagnosis.md`.
- TypeScript source-map and hosted-chat trailing-newline drift are documented
  in
  `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/generated-artifact-drift.md`.
- Representative performance evidence supports the unreleased allocation
  claims but records a 66.04% slower two-table ordered-join row; see
  `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/performance-summary.md`.
- No attempt was superseded and no product, gate, canary, changelog, or
  version fix was made.

Residual risks:

- The intended source revision is not immutable because pre-existing
  release-facing documentation remains uncommitted.
- External quick/full canary gates fail, and the full gate does not reach SDK
  smoke, seeded, race, reproducibility, or protocol-smoke stages.
- The TypeScript build resolves an unpinned compiler and does not reproduce
  checked-in source maps cleanly.
- Focused performance rows are advisory local evidence, not production-scale
  claims; the ordered-join latency regression remains unresolved.

Decision:

- Qualification deferred. This is a failed qualification attempt, not a
  passed clean-ref release record. Full details are in
  `working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/qualification.md`.

### v1.1.1-dev canary qualification - 2026-05-25

- Status: passed
- Operator: gernsback
- Date/time: 2026-05-25T13:06:55Z
- Environment: Linux gernsback 6.17.0-23-generic, linux/amd64,
  Go go1.26.3
- Shunter ref: `e93e51def0183ae94ae942b759dd803018f7a9cb`
- Shunter worktree state: clean before evidence capture; dirty afterward with
  this qualification record and evidence logs
- `opsboard-canary` ref:
  `e69bce73cb49fbd2334dd8b99eb664b07fc6e132`
- `opsboard-canary` worktree state: clean; local-only canary maintenance commit
  refreshed contract/codegen artifacts for the current Shunter protocol and SDK
  surface before qualification

Commands:

| Scope | Working directory | Command | Result | Evidence |
| --- | --- | --- | --- | --- |
| Shunter | `/home/gernsback/source/shunter` | `rtk go test ./...` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/shunter-go-test.log` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go vet ./...` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/shunter-go-vet.log` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go tool staticcheck ./...` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/shunter-staticcheck.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client test` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/typescript-client-test.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run build` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/typescript-client-build.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run pack:dry-run` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/typescript-client-pack-dry-run.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run smoke:package` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/typescript-client-smoke-package.log` |
| Hosted example | `/home/gernsback/source/shunter` | `rtk bash scripts/hosted-chat-gate.sh` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/hosted-chat-gate.log` |
| Canary | `/home/gernsback/source/opsboard-canary` | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-quick` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/canary-quick.log` |
| Canary | `/home/gernsback/source/opsboard-canary` | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-full` | pass | `working-docs/release-evidence/v1.1.1-canary-20260525/canary-full.log` |

Residual risks:

- `opsboard-canary` is a local-only checkout, so this evidence depends on the
  local canary commit above rather than a remote-tracked ref.
- This is a local `v1.1.1-dev` qualification record, not a tagged release
  qualification.

Decision:

- Accepted as local canary-backed qualification for the current `v1.1.1-dev`
  source line.

### v1.1.1-dev local qualification - 2026-05-24

- Status: passed
- Operator: ponchione
- Date/time: 2026-05-24T23:40:04Z
- Environment: Linux gerns-win11 6.6.114.1-microsoft-standard-WSL2,
  linux/amd64, Go go1.26.3
- Shunter ref: `e32f933e39baf57ce3dcd537c75012c288f90ecf`
- Shunter worktree state: clean before evidence capture; dirty afterward with
  this qualification record and evidence logs
- `opsboard-canary` ref: skipped by instruction; no local checkout available
- `opsboard-canary` worktree state: skipped by instruction; no local checkout
  available

Commands:

| Scope | Working directory | Command | Result | Evidence |
| --- | --- | --- | --- | --- |
| Shunter | `/home/ponchione/source/shunter` | `rtk go test ./...` | pass | `working-docs/release-evidence/v1.1.1-local/shunter-go-test.log` |
| Shunter | `/home/ponchione/source/shunter` | `rtk go vet ./...` | pass | `working-docs/release-evidence/v1.1.1-local/shunter-go-vet.log` |
| Shunter | `/home/ponchione/source/shunter` | `rtk go tool staticcheck ./...` | pass | `working-docs/release-evidence/v1.1.1-local/shunter-staticcheck.log` |
| TypeScript | `/home/ponchione/source/shunter` | `rtk npm --prefix typescript/client test` | pass | `working-docs/release-evidence/v1.1.1-local/typescript-client-test.log` |
| TypeScript | `/home/ponchione/source/shunter` | `rtk npm --prefix typescript/client run build` | pass | `working-docs/release-evidence/v1.1.1-local/typescript-client-build.log` |
| TypeScript | `/home/ponchione/source/shunter` | `rtk npm --prefix typescript/client run pack:dry-run` | pass | `working-docs/release-evidence/v1.1.1-local/typescript-client-pack-dry-run.log` |
| TypeScript | `/home/ponchione/source/shunter` | `rtk npm --prefix typescript/client run smoke:package` | pass | `working-docs/release-evidence/v1.1.1-local/typescript-client-smoke-package.log` |
| Hosted example | `/home/ponchione/source/shunter` | `rtk bash scripts/hosted-chat-gate.sh` | pass | `working-docs/release-evidence/v1.1.1-local/hosted-chat-gate.log` |
| Canary | `/home/ponchione/source/opsboard-canary` | `rtk make canary-quick` | skipped | no checkout available; skipped by instruction |
| Canary | `/home/ponchione/source/opsboard-canary` | `rtk make canary-full` | skipped | no checkout available; skipped by instruction |

Residual risks:

- External `opsboard-canary` quick/full gates were not run because no local
  checkout is available and this qualification explicitly ignored canary.
- This is a local `v1.1.1-dev` qualification record, not a tagged release
  qualification.

Decision:

- Accepted as local qualification for the current `v1.1.1-dev` source line.

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
