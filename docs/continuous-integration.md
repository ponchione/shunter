# Continuous Integration

The checked-in GitHub Actions workflow is `.github/workflows/ci.yml`. It runs on
pull requests, pushes to `main`, weekly schedules, and manual dispatch. Workflow
permissions are read-only; it does not publish packages, push commits, create
tags or releases, or deploy artifacts.

## Required Jobs And Local Equivalents

| Workflow job | Enforced checks | Local commands |
| --- | --- | --- |
| Go quality | tracked Go formatting, module tidiness, vet, pinned Staticcheck, newly introduced whitespace | `rtk go fmt <touched files>`, `rtk go mod tidy -diff`, `rtk go vet ./...`, `rtk go tool staticcheck ./...`, `rtk bash scripts/check-whitespace.sh HEAD` for the working tree, or `rtk bash scripts/check-whitespace.sh <base> HEAD` for a committed range |
| Go tests | all Go tests and an uncached race run over concurrency-heavy packages | `rtk go test ./...`; `rtk go test -race -count=1 . ./auth ./executor ./protocol ./protocolclient ./subscription` |
| TypeScript client | locked install, typecheck/runtime tests, build, checked-in `dist`, package dry-run/smoke, high-or-critical npm audit | `rtk npm --prefix typescript/client ci`; then `run test`, `run build`, `run pack:dry-run`, `run smoke:package`, and `audit --audit-level=high` with the same prefix |
| Browser integration | locked client/browser installs, pinned Chromium, strict-auth browser test, high-or-critical npm audit | `rtk npm --prefix typescript/browser-integration ci`, `rtk npm --prefix typescript/browser-integration run install:browsers`, `rtk npm --prefix typescript/browser-integration test`, and the prefixed audit command |
| Vulnerability checks | pinned `govulncheck` against all packages | `rtk govulncheck ./...` using govulncheck v1.1.4 |
| Hosted/static | static hosted-binary gauntlet, hosted-chat gate, generated contract/client sync, hosted frontend audit | `rtk bash scripts/static-hosted-binary-gate.sh`, followed by `rtk git diff` on the generated files and the prefixed frontend audit |

The workflow uses SHA-pinned official checkout/setup actions, the Go toolchain
directive in `go.mod`, locked npm dependencies, and separate jobs/caches so one
surface cannot silently mask another. npm audits fail on high or critical
findings. `govulncheck` fails on reachable Go vulnerabilities.

Whitespace enforcement is range-based. Pull requests compare their complete
change against the pull-request base, pushes compare against the pre-push
revision, and scheduled or manual runs fall back to `HEAD^`. This rejects new
whitespace errors without reclassifying retained evidence, fuzz inputs,
generated fixtures, or intentional Markdown formatting as defects in every
future change. CI and local checks both call `scripts/check-whitespace.sh`; its
regression test is `scripts/check-whitespace-test.sh`.

The full uncached repository race suite is intentionally reserved for the
weekly schedule and manual dispatch because it is materially more expensive
than the focused PR race job:

```bash
rtk go test -race -count=1 ./...
```

Gate scripts retain their local RTK commands. CI installs the checked-in
`scripts/ci-rtk-shim.sh`, which only forwards commands because Actions logs do
not require RTK output compaction.
