# v1.1.1-dev clean-ref candidate qualification

- Status: passed
- Qualification period: 2026-07-10T14:08:23Z through 2026-07-10T14:17:53Z
- Operator: gernsback
- Shunter candidate: `bfef461409e9158b53ad4dc96dc956ca1598fe6a`
- opsboard-canary candidate: `fedcbb6de9eabb539e561e814e9687f27ddb4fe6`
- Environment: Linux gernsback 6.17.0-35-generic, linux/amd64; AMD Ryzen 9
  9900X 12-Core Processor; Go go1.26.3; Node v24.4.1; npm 11.17.0
- Nature: v1.1.1-dev candidate qualification; no release or tag created

Both repositories matched the exact requested candidate revisions and were
completely clean before any build, test, npm, Make, or generation command.
Shunter reported `v1.1.1-dev`; the private TypeScript package reported
`1.1.1-dev`, `private: true`, and an exact TypeScript 5.9.3 manifest and
lockfile resolution. No Go test binary was present. The canary module resolved
Shunter to `/home/gernsback/source/shunter`.

## Command results

| Scope | Working directory | Command | Result | Evidence |
| --- | --- | --- | --- | --- |
| Dependency | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client ci` | pass | `dependency-preflight.log` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go test ./...` | pass | `shunter-go-test.log` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go vet ./...` | pass | `shunter-go-vet.log` |
| Shunter | `/home/gernsback/source/shunter` | `rtk go tool staticcheck ./...` | pass | `shunter-staticcheck.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client test` | pass | `typescript-client-test.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run build` | pass; byte-identical, TypeScript 5.9.3 | `typescript-client-build.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run pack:dry-run` | pass | `typescript-client-pack-dry-run.log` |
| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run smoke:package` | pass | `typescript-client-smoke-package.log` |
| Hosted binary | `/home/gernsback/source/shunter` | `rtk bash scripts/static-hosted-binary-gate.sh` | pass; reproducible | `static-hosted-binary-gate.log` |
| Canary resolution | `/home/gernsback/source/opsboard-canary` | `rtk go list -m -json github.com/ponchione/shunter` | pass; local checkout confirmed | `canary-module-resolution.log` |
| Canary | `/home/gernsback/source/opsboard-canary` | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-quick` | pass | `canary-quick.log` |
| Canary | `/home/gernsback/source/opsboard-canary` | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-full` | pass | `canary-full.log` |
| Performance | `/home/gernsback/source/shunter` | `go test -run '^$' -bench '^BenchmarkExecuteCompiledSQLQueryJoinReadShapes$/^two_table_join_projection_order_limit$' -benchmem -count=10 ./protocol` | pass; advisory | `perf-ordered-join-clean-ref-raw.txt` |

Post-command repository and generated-artifact checks found no tracked, staged,
source, generated, lockfile, dependency, source-map, or test-binary drift.
Untracked Shunter paths were limited to this evidence directory.

## Reproducibility

- TypeScript: pass. The package-local compiler was TypeScript 5.9.3 and all
  four `dist` hashes were identical before and after build.
- Hosted generation: pass. Contract, generated binding, and frontend lockfile
  hashes were identical before and after the static gate; the canonical
  two-newline footer remained unchanged.
- Canary quick: pass through Go tests and SDK smoke/typecheck.
- Canary full: pass through Go tests, SDK smoke/typecheck, seeded workflow,
  race testing, contract reproducibility, codegen reproducibility, in-process
  protocol smoke, and served protocol smoke.

## Performance and carried-forward safety evidence

The clean candidate measured 4.910 ms/op, 269.7 KiB/op, and 3.162k
allocations/op. This is 21.39% slower than the v1.1.0 latency baseline and
2.58% slower than the dirty remediation run while retaining the substantial
bytes/allocation improvements versus v1.1.0.

The earlier bounded memory investigation is not rerun. Its conclusion is
carried forward: no memory-growth or leak signal was found, while the cause of
the prior abrupt machine failure remains unknown.

## Residual risks

- The representative ordered-join row retains a 21.39% local latency regression
  versus v1.1.0. It is advisory evidence, not a production-scale workload.
- The prior hard machine failure has no established root cause, despite no
  Shunter memory-leak signal in the preceding investigation.

## Decision

Passed as the v1.1.1-dev qualification of the two exact clean candidate
revisions. No staging, commit, amendment, rebase, reset, restore, push, tag,
publication, release, or pull request occurred. Release-owner review and a
candidate release decision are required before any tag or release action.

