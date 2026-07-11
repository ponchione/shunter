# v1.1.1-dev release-owner review

- Status: approved for release preparation
- Review period: 2026-07-10T14:29:21Z through 2026-07-10T14:30:04Z
- Reviewer: gernsback
- Environment: Linux gernsback 6.17.0-35-generic, linux/amd64; AMD Ryzen 9
  9900X 12-Core Processor; Go go1.26.3; Node v24.4.1; npm 11.17.0
- Shunter candidate: `bfef461409e9158b53ad4dc96dc956ca1598fe6a`
- opsboard-canary candidate: `fedcbb6de9eabb539e561e814e9687f27ddb4fe6`
- Qualification period: 2026-07-10T14:08:23Z through 2026-07-10T14:17:53Z
- Qualification final Git verification: 2026-07-10T14:20:25Z
- Scope: evidence audit and release-owner candidate decision only

## Evidence reviewed

- Repository instructions, `VERSION`, `CHANGELOG.md`, and
  `working-docs/release-qualification.md`.
- Clean-ref `qualification.md`, `preflight.md`, `final-status.md`,
  `typescript-reproducibility.md`, `hosted-generation-reproducibility.md`, and
  `performance-summary.md`.
- Every retained clean-ref command log: dependency preflight; Shunter test,
  vet, and Staticcheck; TypeScript test, build, pack dry-run, and packed-install
  smoke; static hosted-binary gate; canary module resolution, quick, and full;
  and all four ordered-join raw/sample/comparison records named by the review
  task.
- Blocker-remediation `remediation-summary.md`, `memory-safety-check.md`, and
  `performance-summary.md`.
- A fresh review preflight in both repositories, including the required Git
  state, version, artifact hashes, hosted-binding footer, test-binary, and Go
  module-resolution checks.

## Review dispositions

| Requirement | Disposition |
| --- | --- |
| 1. Candidate identity and initial state | Pass. The retained preflight proves both worktrees were clean at the exact candidate SHAs before qualification began. The review preflight reconfirmed both unchanged HEADs and only the authorized Shunter ledger/evidence paths. |
| 2. Command completeness | Pass. Dependency install, Shunter test/vet/Staticcheck, all four TypeScript gates, the static hosted-binary gate, canary module resolution, canary quick/full, and the authorized narrow benchmark are present with exit code zero. |
| 3. TypeScript reproducibility | Pass. Package version is `1.1.1-dev`, `private` is exactly `true`, manifest and lockfile select TypeScript 5.9.3 exactly, and the package-local compiler reported 5.9.3. All four `dist` hashes remained unchanged; no source-map drift occurred; test, build, dry-run packaging, and packed-install smoke passed. |
| 4. Hosted generation reproducibility | Pass. Contract, generated binding, and frontend lockfile hashes remained unchanged; the binding retained exactly two trailing newline bytes; the static hosted-binary gate passed. No normalization or restoration manufactured the result. |
| 5. External canary coverage | Pass. Quick passed Go tests and SDK smoke/typecheck. Full reached and passed Go tests, SDK smoke/typecheck, seeded workflow, race testing, contract and codegen reproducibility, and in-process and served protocol smoke. The canary remained clean and resolved Shunter to `/home/gernsback/source/shunter`. |
| 6. Performance evidence | Pass as advisory evidence. The clean candidate measured 4.910 ms/op, 269.7 KiB/op, and 3.162k allocations/op. Versus v1.1.0: latency +21.39%, bytes/op -67.63%, allocations/op -33.14%. Versus dirty remediation: latency +2.58%, bytes/op not significantly different, allocations/op equal. Comparisons used `golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d` with `-ignore cpu`. No release threshold is inferred. |
| 7. Memory and machine-failure evidence | Pass as carried-forward bounded evidence. No Shunter memory-growth or leak signal was found. No production soak was performed, and the prior abrupt machine-failure cause remains unknown. The investigation was not rerun. |
| 8. Changelog and decision consistency | Pass. The unreleased changelog claims reduced allocation traffic and one-time source resolution, not a general latency improvement. The qualification record consistently reports passed status, both exact revisions, clean initial state, every required gate passed, no tag or release, and both residual risks. |

## Residual-risk decisions

- Ordered-join latency risk: accepted for this release-owner candidate decision.
  The 4.910 ms/op representative row is 21.39% slower than v1.1.0 and 2.58%
  slower than the dirty remediation run. It is narrow local advisory evidence,
  and the release claim remains limited to allocation traffic and source
  resolution.
- Unknown machine-failure risk: accepted for this release-owner candidate
  decision. The bounded investigation found no Shunter memory-growth or leak
  signal, but it was not a production soak and did not establish the cause of
  the earlier abrupt failure.

## Decision

The two residual risks are explicitly accepted for the release-owner candidate
decision. Both qualified candidates are approved to advance to a separately
authorized release-preparation slice.

This approval is not authorization to tag, publish, create a release, or open a
pull request. No staging, commit, amendment, rebase, reset, restore, checkout,
push, tag, publication, release, or pull request was performed in this review
slice.
