# Final worktree status

Captured: 2026-07-10T12:37:13Z

This is dirty-worktree remediation state, not a clean-ref qualification.

## Shunter

HEAD: `9fddcf0b842f72d5e24e399d558042394b337fbd`

`rtk git status --short --branch`:

```text
* main...origin/main
 M CHANGELOG.md
 M examples/hosted-chat/frontend/package-lock.json
 M examples/hosted-chat/frontend/src/generated/hosted_chat.ts
 M protocol/handle_oneoff.go
 M protocol/handle_oneoff_test.go
 M typescript/client/package.json
 M working-docs/README.md
 M working-docs/deferred-functionality-backlog.md
 M working-docs/hosted-backend-roadmap.md
 M working-docs/release-qualification.md
?? typescript/client/package-lock.json
?? working-docs/nesl-operational-use-thought-experiment.md
?? working-docs/recommendations/
?? working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/
?? working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/

```

`rtk git diff --stat`:

```text
CHANGELOG.md                                       |  3 +-
 examples/hosted-chat/frontend/package-lock.json    |  2 +-
 .../frontend/src/generated/hosted_chat.ts          |  1 +
 protocol/handle_oneoff.go                          | 89 +++++++++++++++++-----
 protocol/handle_oneoff_test.go                     | 34 +++++++++
 typescript/client/package.json                     |  8 +-
 working-docs/README.md                             |  5 ++
 working-docs/deferred-functionality-backlog.md     | 45 +++++------
 working-docs/hosted-backend-roadmap.md             | 34 +++++++++
 working-docs/release-qualification.md              | 67 ++++++++++++++++
 10 files changed, 234 insertions(+), 54 deletions(-)

```

`rtk git ls-files --others --exclude-standard`:

```text
typescript/client/package-lock.json
working-docs/nesl-operational-use-thought-experiment.md
working-docs/recommendations/01-current-release-qualification.md
working-docs/recommendations/02-product-operating-envelope.md
working-docs/recommendations/03-durability-maintenance-policy.md
working-docs/recommendations/04-recovery-efficiency-refactor.md
working-docs/recommendations/05-enterprise-integration-reliability.md
working-docs/recommendations/06-operational-authorization-model.md
working-docs/recommendations/07-client-connectivity-resilience.md
working-docs/recommendations/08-live-query-admission-policy.md
working-docs/recommendations/09-operational-audit-trail.md
working-docs/recommendations/10-typescript-client-distribution.md
working-docs/recommendations/11-type-system-depth.md
working-docs/recommendations/README.md
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/canary-full.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/canary-quick.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/final-status.md
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/hosted-generation-reproducibility.md
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/memory-safety-check.md
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/opsboard-canary-final-tracked.diff
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/perf-ordered-join-current-benchstat.txt
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/perf-ordered-join-current-raw.txt
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/perf-ordered-join-remediated-benchstat.txt
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/perf-ordered-join-remediated-raw.txt
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/perf-ordered-join-v1.1.0-baseline.txt
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/performance-summary.md
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/preflight.md
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/remediation-summary.md
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/shunter-final-tracked.diff
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/shunter-go-test.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/shunter-go-vet.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/shunter-staticcheck.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/static-hosted-binary-gate.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/typescript-client-build.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/typescript-client-pack-dry-run.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/typescript-client-smoke-package.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/typescript-client-test.log
working-docs/release-evidence/v1.1.1-dev-blocker-remediation-20260710/typescript-reproducibility.md
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/canary-full.log
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/canary-quick.log
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/external-canary-diagnosis.md
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/generated-artifact-drift.md
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-oneoff-focused-benchstat.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-oneoff-join-projection-order-limit-raw.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-oneoff-projection-order-limit-benchstat.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-oneoff-projection-order-limit-raw.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-ordered-comparator-ties-raw.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-ordered-focused-benchstat.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-ordered-initial-window-raw.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-v1.1.0-oneoff-baseline-focused.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-v1.1.1-dev-oneoff-current-focused.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/perf-v1.1.1-dev-ordered-current-focused.txt
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/performance-summary.md
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/preflight.md
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/qualification.md
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/shunter-go-test.log
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/shunter-go-vet.log
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/shunter-staticcheck.log
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/static-hosted-binary-gate.log
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/typescript-client-build.log
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/typescript-client-pack-dry-run.log
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/typescript-client-smoke-package.log
working-docs/release-evidence/v1.1.1-dev-qualification-20260710-9fddcf0/typescript-client-test.log

```

The index is unchanged: `rtk git diff --cached --stat` produced no output.
The older documentation, recommendations, and failed-qualification paths are
pre-existing user-owned changes. The TypeScript lockfile, implementation,
generated/lockfile updates, and blocker-remediation evidence are
remediation-owned.

## opsboard-canary

HEAD: `e69bce73cb49fbd2334dd8b99eb664b07fc6e132`

`rtk git status --short --branch`:

```text
* master
 M contracts/shunter.contract.json
 M generated/typescript/index.ts
 M internal/workflows/auth_handshake_test.go
 M package-lock.json

```

`rtk git diff --stat`:

```text
contracts/shunter.contract.json           | 30 +++++++++++++++
 generated/typescript/index.ts             |  2 +
 internal/workflows/auth_handshake_test.go | 61 +++++++++++++++++++++++++++----
 package-lock.json                         |  2 +-
 4 files changed, 86 insertions(+), 9 deletions(-)

```

`rtk git ls-files --others --exclude-standard` produced no output. The index
is unchanged: `rtk git diff --cached --stat` produced no output.

All four tracked canary modifications are remediation-owned and explained by
the contract/codegen/auth refresh.
