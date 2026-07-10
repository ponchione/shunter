# v1.1.1-dev blocker remediation

- Status: complete; ready for candidate-commit review
- Date: 2026-07-10
- Nature of record: mutable-worktree remediation evidence, not clean-ref release qualification
- Shunter starting commit: `9fddcf0b842f72d5e24e399d558042394b337fbd`
- opsboard-canary starting commit: `e69bce73cb49fbd2334dd8b99eb664b07fc6e132`

No commit, staging operation, tag, publication, push, pull request, or release
qualification was created.

## Initial states

The Shunter preflight recorded the pre-existing documentation and failed
qualification slice before remediation. The opsboard-canary checkout was clean
on local branch `master`. See `preflight.md`.

## Remediation-owned changes

Shunter implementation and generated state:

- `CHANGELOG.md`
- `typescript/client/package.json`
- `typescript/client/package-lock.json`
- `examples/hosted-chat/frontend/package-lock.json`
- `examples/hosted-chat/frontend/src/generated/hosted_chat.ts`
- `protocol/handle_oneoff.go`
- `protocol/handle_oneoff_test.go`
- this evidence directory

opsboard-canary:

- `contracts/shunter.contract.json`
- `generated/typescript/index.ts`
- `internal/workflows/auth_handshake_test.go`
- `package-lock.json`

The older Shunter documentation/recommendation slice and failed qualification
record remain pre-existing user-owned changes and were not rewritten by this
remediation.

## Blocker disposition

### TypeScript compiler and generated artifacts

The previous scripts resolved an unconstrained TypeScript 7.0.2. Exact-version
comparison established TypeScript 5.9.3 as a repository-consistent compiler.
The private client now pins and locks 5.9.3 and invokes its local `tsc`.

Two consecutive final builds were byte-identical to the checked-in `dist`
tree. Test, build, dry-run pack, and packed-install smoke gates pass.

Disposition: resolved.

### Hosted generated binding

The generator consistently emits a canonical two-newline footer. The checked-in
hosted binding was aligned to that output. The downstream frontend lockfile
records the linked client's exact TypeScript dependency.

Two consecutive static hosted-binary gates passed without changing contract,
binding, or lockfile hashes.

Disposition: resolved.

### External canary

The canary contract now contains eight public and two system
`sdk.visibility` blocks. Generated TypeScript records the internal profile and
`@shunter/client` runtime import. Strict-auth tests now distinguish malformed
pre-upgrade Authorization headers from supported-subprotocol connections that
upgrade and then close with code 1008 and Shunter's auth-rejection reason.

Both final canary gates pass. The full gate includes Go tests, SDK smoke, seeded
workflow, race testing, contract/codegen reproducibility, in-process protocol
smoke, and served protocol smoke. Go module resolution confirms the canary uses
`/home/gernsback/source/shunter`.

Disposition: resolved.

### Ordered-join latency

The development tree's representative row reproduced at 6.704 ms/op, 65.76%
slower than the 4.044 ms/op v1.1.0 baseline. Resolving ORDER BY row sources once
per query reduces the final row to 4.786 ms/op, a 28.61% improvement over the
pre-remediation tree. It remains 18.34% slower than v1.1.0 while retaining
67.63% lower bytes/op and 33.14% fewer allocations/op.

The discarded flat-key experiment is absent. Focused alias, order-direction,
projection, rejection, and multi-way join tests pass repeatedly, under race
detection, and in the full suite.

Disposition: materially improved with an explicit residual latency regression.
The changelog claim is limited to allocation traffic and source resolution.

### Prior system crash and memory concern

The prior boot ended abruptly without an OOM kill or kernel panic in the
available journal. Current host memory was healthy. Repeated focused tests,
goleak, race, heap sampling, and live benchmark memory observation found no
evidence of retained query state, goroutine leakage, swap pressure, or
benchmark-driven memory growth.

Disposition: no Shunter memory-leak signal found; original machine-failure
cause remains unknown.

## Final commands

| Command | Result |
| --- | --- |
| focused tests, 50 repetitions | pass; 250 executions |
| focused race test | pass |
| representative ordered-join benchmark, count 10 | pass |
| `rtk go test ./...` | pass; 6,416 tests in 30 packages |
| `rtk go vet ./...` | pass |
| `rtk go tool staticcheck ./...` | pass |
| `rtk npm --prefix typescript/client test` | pass |
| two consecutive TypeScript builds | pass; byte-identical |
| `rtk npm --prefix typescript/client run pack:dry-run` | pass |
| `rtk npm --prefix typescript/client run smoke:package` | pass |
| two consecutive static hosted-binary gates | pass; idempotent |
| `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-quick` | pass |
| `SHUNTER_CHECKOUT=/home/gernsback/source/shunter rtk make canary-full` | pass |

## Remaining risks

- Both intended revisions are still mutable dirty worktrees.
- Pre-existing Shunter release-facing documentation remains uncommitted.
- The ordered-join representative row remains 18.34% slower than v1.1.0,
  despite the substantial remediation and retained allocation improvements.
- The previous hard system failure has no established root cause.
- These local performance rows do not establish production scale.
- opsboard-canary is a local branch without an immutable candidate revision.

## Readiness decision

No unexplained changes remain. Both worktrees are ready for release-owner
candidate-commit review. They are not release qualified. After immutable
candidate revisions are created with explicit authorization, qualification must
be rerun from clean refs.
