# Final worktree status

- Captured (UTC): 2026-07-10T14:20:25Z
- Final verification: pass
- Shunter HEAD unchanged: `bfef461409e9158b53ad4dc96dc956ca1598fe6a`
- opsboard-canary HEAD unchanged: `fedcbb6de9eabb539e561e814e9687f27ddb4fe6`
- Shunter changes limited to the new ledger record and evidence directory: true
- opsboard-canary completely clean: true
- Indexes/staging areas unchanged in both repositories: true

## /home/gernsback/source/shunter

### `rtk git status --short --branch`

Exit code: 0

```text
* main...origin/main [ahead 3]
 M working-docs/release-qualification.md
?? working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/
```

### `rtk git rev-parse HEAD`

Exit code: 0

```text
bfef461409e9158b53ad4dc96dc956ca1598fe6a
```

### `rtk git log --oneline --decorate -8`

Exit code: 0

```text
bfef461 (HEAD -> main) Remediate v1.1.1-dev qualification blockers
2515db0 Record failed v1.1.1-dev qualification
d7e3856 Document continued development recommendations
9fddcf0 (origin/main) Reduce one-off query helper duplication
b03d4b3 Simplify multi-join split-or fixtures
ad81c2c code reduction
8527a22 doc updates
d5c3bb3 test: factor repeated runtime test setup
```

### `rtk git diff --stat`

Exit code: 0

```text
working-docs/release-qualification.md | 52 +++++++++++++++++++++++++++++++++++
 1 file changed, 52 insertions(+)
```

### `rtk git diff --name-status`

Exit code: 0

```text
M	working-docs/release-qualification.md

Changes:

```

### `rtk git diff`

Exit code: 0

```text
working-docs/release-qualification.md | 52 +++++++++++++++++++++++++++++++++++
 1 file changed, 52 insertions(+)

Changes:

working-docs/release-qualification.md
  @@ -95,6 +95,58 @@ Decision:
  +### v1.1.1-dev clean-ref candidate qualification - 2026-07-10
  +
  +- Status: passed
  +- Operator: gernsback
  +- Date/time: 2026-07-10T14:08:23Z through 2026-07-10T14:17:53Z
  +- Environment: Linux gernsback 6.17.0-35-generic, linux/amd64,
  +  Go go1.26.3, Node v24.4.1, npm 11.17.0,
  +  AMD Ryzen 9 9900X 12-Core Processor
  +- Source version: `v1.1.1-dev`
  +- Shunter ref: `bfef461409e9158b53ad4dc96dc956ca1598fe6a`
  +- Shunter worktree state: clean before qualification; afterward limited to
  +  this ledger entry and
  +  `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/`
  +- `opsboard-canary` ref:
  +  `fedcbb6de9eabb539e561e814e9687f27ddb4fe6`
  +- `opsboard-canary` worktree state: clean before and after; local branch
  +  `master`
  +
  +Commands:
  +
  +| Scope | Working directory | Command | Result | Evidence |
  +| --- | --- | --- | --- | --- |
  +| Preflight | `/home/gernsback/source/shunter` and `/home/gernsback/source/opsboard-canary` | required clean-ref Git, version, package, lockfile, artifact, environment, and footer checks | pass | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/preflight.md` |
  +| Dependency | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client ci` | pass; package-local TypeScript 5.9.3 | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/dependency-preflight.log` |
  +| Shunter | `/home/gernsback/source/shunter` | `rtk go test ./...` | pass | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/shunter-go-test.log` |
  +| Shunter | `/home/gernsback/source/shunter` | `rtk go vet ./...` | pass | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/shunter-go-vet.log` |
  +| Shunter | `/home/gernsback/source/shunter` | `rtk go tool staticcheck ./...` | pass | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/shunter-staticcheck.log` |
  +| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client test` | pass | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/typescript-client-test.log` |
  +| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run build` | pass; byte-identical `dist` | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/typescript-client-build.log` |
  +| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run pack:dry-run` | pass | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/typescript-client-pack-dry-run.log` |
  +| TypeScript | `/home/gernsback/source/shunter` | `rtk npm --prefix typescript/client run smoke:package` | pass | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/typescript-client-smoke-package.log` |
  +| Hosted binary | `/home/gernsback/source/shunter` | `rtk bash scripts/static-hosted-binary-gate.sh` | pass; generated hashes and canonical footer reproduced | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/static-hosted-binary-gate.log` |
  +| Canary resolution | `/home/gernsback/source/opsboard-canary` | `rtk go list -m -json github.com/ponchione/shunter` | pass; resolved to the candidate checkout | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/canary-module-resolution.log` |
  +| Canary | `/home/gernsback/source/opsboard-canary` | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-quick` | pass; Go tests and SDK smoke/typecheck | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/canary-quick.log` |
  +| Canary | `/home/gernsback/source/opsboard-canary` | `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-full` | pass; all required stages | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/canary-full.log` |
  +| Performance | `/home/gernsback/source/shunter` | `go test -run '^$' -bench '^BenchmarkExecuteCompiledSQLQueryJoinReadShapes$/^two_table_join_projection_order_limit$' -benchmem -count=10 ./protocol` | pass; advisory, 10 samples | `working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/performance-summary.md` |
  +
  +Residual risks:
  +
  +- The clean candidate's representative ordered-join row measured 4.910 ms/op,
  +  21.39% slower than the v1.1.0 baseline and 2.58% slower than the dirty
  +  remediation result. This local row is advisory and not production-scale.
  +- The prior bounded investigation found no Shunter memory-growth or leak
  +  signal, but the earlier hard machine-failure cause remains unknown.
  +
  +Decision:
  +
  +- Passed as v1.1.1-dev candidate qualification for both exact clean revisions.
  +  No tag or release was created. Release-owner review and an explicit candidate
  +  release decision are the next action.
  +
  +
   ### v1.1.1-dev qualification attempt - 2026-07-10
   
   - Status: failed
  +52 -0
```

### `rtk git diff --cached`

Exit code: 0

```text

```

### `rtk git ls-files --others --exclude-standard`

Exit code: 0

```text
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/canary-full.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/canary-module-resolution.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/canary-quick.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/dependency-preflight.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/final-status.md
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/hosted-generation-reproducibility.md
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/perf-ordered-join-clean-ref-benchstat.txt
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/perf-ordered-join-clean-ref-raw.txt
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/perf-ordered-join-clean-ref-samples.txt
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/perf-ordered-join-comparison.txt
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/perf-ordered-join-vs-remediation.txt
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/performance-summary.md
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/preflight.md
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/qualification.md
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/shunter-go-test.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/shunter-go-vet.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/shunter-staticcheck.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/static-hosted-binary-gate.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/typescript-client-build.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/typescript-client-pack-dry-run.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/typescript-client-smoke-package.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/typescript-client-test.log
working-docs/release-evidence/v1.1.1-dev-clean-ref-qualification-20260710-bfef461-fedcbb6/typescript-reproducibility.md
```

## /home/gernsback/source/opsboard-canary

### `rtk git status --short --branch`

Exit code: 0

```text
## master

```

### `rtk git rev-parse HEAD`

Exit code: 0

```text
fedcbb6de9eabb539e561e814e9687f27ddb4fe6
```

### `rtk git log --oneline --decorate -8`

Exit code: 0

```text
fedcbb6 (HEAD -> master) Refresh canary for current Shunter contracts
e69bce7 Refresh canary for current Shunter protocol
a45ec6f Cover app reducer validation recovery canary
86a4a22 Cover malformed reducer args recovery canary
2f0420d Cover missing reducer recovery canary
fbc85a8 Cover reducer permission recovery canary
471a70e Cover denied declared query recovery canary
bf4f3f1 Cover audit admission multi recovery canary
```

### `rtk git diff --stat`

Exit code: 0

```text

```

### `rtk git diff --name-status`

Exit code: 0

```text

```

### `rtk git diff`

Exit code: 0

```text

```

### `rtk git diff --cached`

Exit code: 0

```text

```

### `rtk git ls-files --others --exclude-standard`

Exit code: 0

```text

```

