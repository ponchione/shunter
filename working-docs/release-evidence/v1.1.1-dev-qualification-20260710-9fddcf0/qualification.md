# v1.1.1-dev qualification attempt

- Status: failed; qualification deferred
- Started: 2026-07-10T10:54:15Z
- Finished: 2026-07-10T11:07:00Z
- Operator: gernsback
- Environment: Linux gernsback 6.17.0-35-generic, linux/amd64, Go go1.26.3, AMD Ryzen 9 9900X 12-Core Processor
- Source version: `v1.1.1-dev`
- Shunter commit under test: `9fddcf0b842f72d5e24e399d558042394b337fbd`
- Initial Shunter worktree: dirty with user-owned changes that predate qualification in `working-docs/README.md`, `working-docs/deferred-functionality-backlog.md`, `working-docs/hosted-backend-roadmap.md`, `working-docs/nesl-operational-use-thought-experiment.md`, and `working-docs/recommendations/`
- External canary commit: `e69bce73cb49fbd2334dd8b99eb664b07fc6e132`
- Initial/final external canary worktree: clean, local branch `master`

Version metadata is internally consistent: `VERSION` is `v1.1.1-dev`, `@shunter/client` is `1.1.1-dev`, and `rtk go run ./cmd/shunter version` reports `shunter v1.1.1-dev`.

## Required commands

| Scope | Working directory | Command | Result | Evidence |
| --- | --- | --- | --- | --- |
| Preflight | Shunter | `rtk git status --short --branch` | pass; dirty state recorded | [preflight.md](preflight.md) |
| Preflight | Shunter | `rtk git rev-parse HEAD` | pass | [preflight.md](preflight.md) |
| Preflight | Shunter | `rtk git log -10 --date=iso-strict --pretty=fuller` | pass | [preflight.md](preflight.md) |
| Preflight | Shunter | `rtk git tag --sort=-version:refname` | pass | [preflight.md](preflight.md) |
| Preflight | Shunter | inspect `VERSION` and `typescript/client/package.json` | pass; versions mirror | [preflight.md](preflight.md) |
| Preflight | Shunter | `rtk go run ./cmd/shunter version` | pass | [preflight.md](preflight.md) |
| Go | Shunter | `rtk go test ./...` | pass | [shunter-go-test.log](shunter-go-test.log) |
| Go | Shunter | `rtk go vet ./...` | pass | [shunter-go-vet.log](shunter-go-vet.log) |
| Go | Shunter | `rtk go tool staticcheck ./...` | pass | [shunter-staticcheck.log](shunter-staticcheck.log) |
| TypeScript | Shunter | `rtk npm --prefix typescript/client test` | pass | [typescript-client-test.log](typescript-client-test.log) |
| TypeScript | Shunter | `rtk npm --prefix typescript/client run build` | pass; source-map drift | [typescript-client-build.log](typescript-client-build.log) |
| TypeScript | Shunter | `rtk npm --prefix typescript/client run pack:dry-run` | pass | [typescript-client-pack-dry-run.log](typescript-client-pack-dry-run.log) |
| TypeScript | Shunter | `rtk npm --prefix typescript/client run smoke:package` | pass | [typescript-client-smoke-package.log](typescript-client-smoke-package.log) |
| Hosted binary | Shunter | `rtk bash scripts/static-hosted-binary-gate.sh` | pass; trailing-blank-line drift | [static-hosted-binary-gate.log](static-hosted-binary-gate.log) |
| Canary | opsboard-canary | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-quick` | fail | [canary-quick.log](canary-quick.log) |
| Canary | opsboard-canary | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-full` | fail at initial tests; later full stages not run | [canary-full.log](canary-full.log) |

## Failures and generated drift

- The external canary is stale against current contract/codegen metadata and current strict-auth close semantics. See [external-canary-diagnosis.md](external-canary-diagnosis.md).
- Qualification-created changes to two client source maps and one hosted-chat generated file were diagnosed and restored. See [generated-artifact-drift.md](generated-artifact-drift.md).
- No runtime, test, gate, canary, changelog, or version fix was made.
- No attempt was superseded.

## Performance review

Four representative `-count=10` rows were refreshed because the unreleased changelog makes concrete ordered-read allocation claims. Those allocation claims are supported, but the representative ordered join row is 66.04% slower than the `v1.1.0` snapshot. See [performance-summary.md](performance-summary.md). No production-scale claim is made.

## Residual risks

- The intended release source state is ambiguous because release-facing documentation changes predate qualification and are not committed or otherwise identified by an immutable ref. This attempt cannot become a formal clean-ref qualification record.
- External canary quick and full gates do not pass. The full gate exits before SDK smoke, seeded, race, reproducibility, and protocol-smoke coverage.
- The TypeScript build resolves an unpinned compiler and rewrites checked-in source maps, so generated `dist` reproducibility is not clean at current HEAD.
- The representative ordered join benchmark reduces allocations but has materially worse local latency than the `v1.1.0` snapshot.
- Local gates and advisory benchmarks do not establish production scale.

## Decision

Qualification deferred. The in-repository gates and version checks pass, but a release-preparation recommendation is blocked by the ambiguous dirty source state, failed external canary, and generated-artifact reproducibility drift.

