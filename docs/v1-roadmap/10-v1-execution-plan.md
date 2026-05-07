# v1 Execution Plan

Status: active plan
Owner: unassigned
Scope: ordered work required to turn the current v1 roadmap into a release
candidate.

## Baseline

Shunter is not starting from a blank slate. Current code already has the core
runtime, storage, protocol, schema, subscription, SQL/read, auth, contract,
codegen, backup/restore, migration-hook, observability, fuzz, benchmark, and
gauntlet foundations.

The remaining v1 work is mostly about freezing contracts, proving behavior
across realistic workflows, and shipping the missing user-facing pieces:

- a maintained reference application
- a production-credible TypeScript client runtime
- a strict production-auth contract
- an operator runbook and release checklist
- documented performance envelopes
- a clear in-process trust model
- release qualification commands that are repeatable

## Execution Rules

- Keep each slice reviewable and independently testable.
- Prefer root `shunter` APIs, generated contracts, and protocol surfaces for
  user-facing work.
- Update the owning roadmap file when a decision lands or a slice changes the
  current state.
- Add tests before treating a behavior as v1-stable.
- Keep broad SpacetimeDB parity, cloud hosting, dynamic module loading, and
  multi-language clients out of v1 unless a roadmap decision explicitly changes
  scope.

## Phase 0: Roadmap Reality Cleanup

Goal: make the roadmap describe the implementation that exists now.

Status: done by this documentation slice.

Deliverables:

- Update `docs/v1-roadmap/` statuses and current-state sections.
- Reconcile `09-sql-read-scope.md` with `docs/v1-compatibility.md`.
- Add this execution plan and link it from the roadmap index.

Verification:

```bash
rtk git diff --check
```

## Phase 1: Contract And Read-Surface Closure

Goal: make `docs/v1-compatibility.md` a fully backed v1 contract.

Tasks:

- Re-audit root `shunter` exports and lower-level package docs after recent
  ops, migration, declared-read, and health additions.
- Decide whether `Host` remains preview for v1 or graduates to stable.
- Decide whether generated TypeScript identifier normalization and collision
  suffixes are compatibility commitments.
- Confirm whether any lower-level package beyond the existing stable subsets
  receives normal Go compatibility promises.
- Add or confirm golden coverage for every stable protocol payload,
  `ModuleContract` JSON field, and generated TypeScript category.
- Add or confirm negative tests for unsupported SQL classes listed in
  `09-sql-read-scope.md`.
- Make `contractdiff` and `contractworkflow` policy behavior line up with the
  final v1 compatibility rules.

Exit criteria:

- `docs/v1-compatibility.md` has no open decision that blocks `v1.0.0`.
- Every stable payload shape has a fixture or compatibility test.
- App-author docs cite the compatibility matrix for read-surface limits.

Verification:

```bash
rtk go test ./protocol ./codegen ./contractdiff ./contractworkflow ./query/sql ./subscription ./...
rtk go vet ./...
```

## Phase 2: Maintained Reference Application

Goal: ship one in-repo application that proves the normal app-author and
operator workflow.

Recommended domain: collaborative task board. It matches existing
release-candidate tests while leaving room for a richer maintained example.

Status: task-board contract written; implementation, client, and tests remain.

Tasks:

- Keep `docs/v1-roadmap/reference-app-taskboard-contract.md` current as the
  implementation contract.
- Add the app under an examples or integration-test location agreed with the
  repo layout.
- Include 5-8 tables with varied schema shapes, at least one private table, one
  public table, and one sender-based visibility filter.
- Implement several reducers with validation and permission checks.
- Include a scheduled reducer or lifecycle hook only if it remains part of the
  v1 contract.
- Add declared queries and declared live views. Keep raw SQL as a documented
  escape hatch.
- Export a committed `shunter.contract.json` fixture and generated TypeScript
  artifacts.
- Add a small browser or Node client once the SDK shape is available.
- Add black-box tests for empty-data bootstrap, reducer calls, subscriptions,
  clean restart, backup/restore, and one migration path.

Exit criteria:

- The reference app is the documented v1 starting point.
- It uses only public APIs for normal operation.
- It fails loudly when contract/codegen/auth/subscription/recovery ergonomics
  regress.

Verification:

```bash
rtk go test ./...
```

Add the TypeScript typecheck/test command after the client package exists.

## Phase 3: TypeScript Client Runtime

Goal: a normal TypeScript app should not write protocol handlers by hand.

Status: proposed SDK contract written; runtime package and tests remain.

Tasks:

- Decide package location. Preferred default unless contradicted by repo
  constraints: keep a small runtime package in-repo beside generated fixtures so
  Go codegen and TypeScript tests evolve together.
- Keep `docs/v1-roadmap/typescript-sdk-contract.md` current as the runtime API
  target before generating more helpers.
- Decide reducer argument encoding conventions. The Go runtime still accepts raw
  bytes, so generated helpers need a stable app-facing encoding story.
- Implement subscription handles with idempotent unsubscribe.
- Implement protocol version/subprotocol mismatch errors.
- Add tests for connection transitions, auth failure, reducer/query/view
  success and failure, initial snapshots, deltas, unsubscribe, reconnect, and
  mismatch handling.
- Wire the reference app client through the SDK only.

Exit criteria:

- Typed reducer calls, declared queries, and declared views work through the SDK.
- Reconnect and unsubscribe semantics are documented and tested.
- The reference app can be used without handwritten wire-code plumbing.

## Phase 4: Production Auth Contract

Goal: strict auth is safe to configure and clear to operate.

Status: current dev/strict auth behavior documented, issuer allowlists
implemented, and strict future-token handling tested; broader strict-auth
decisions and reference-app example remain.

Tasks:

- Decide whether v1 is HS256-only or whether asymmetric/JWKS/OIDC support is
  required before `v1.0.0`.
- Keep `AuthIssuers` issuer allowlist behavior documented and tested.
- Decide key replacement/rotation behavior and document operational procedure.
- Decide whether permissions come only from the `permissions` claim or through
  an app-provided mapper.
- Decide strict-mode anonymous-token behavior.
- Add startup/config validation for any newly required strict fields.
- Keep `docs/authentication.md` current as structured dev/strict auth guidance.
- Keep tests current for missing signing config, invalid issuer, invalid
  audience, expired/future/malformed/wrong-algorithm tokens, missing
  permissions, and visibility-filtered reads across local and protocol paths.
- Update the reference app to demonstrate the recommended production pattern.

Exit criteria:

- Strict auth fails closed by default.
- Principal derivation, permission mapping, issuer/audience validation, and key
  replacement behavior are documented and tested.

## Phase 5: Operations And Upgrade Workflow

Goal: operators have one documented path for data safety and upgrades.

Status: runbook, release checklist, clean-shutdown fresh-restore test,
incompatible-contract restore test, and migration hook success/failure tests
added; crash/fault, version-metadata, and reference-app workflow remain.

Tasks:

- Keep `docs/operations.md` current as the focused operator runbook.
- Confirm offline-only backup for v1 or design an online backup path.
- Define snapshot retention and compaction ownership.
- Define contract policy failure behavior in startup/release workflows.
- Define durable metadata that distinguishes Shunter runtime version from app
  module version.
- Test crash during reducer commit, crash during snapshot or compaction, and
  version metadata compatibility.
- Add reference-app backup, restore, migration, and upgrade examples.

Exit criteria:

- Backup, restore, migration, and upgrade are documented as a single operator
  workflow.
- Integration tests cover the recommended workflow and failure behavior.

## Phase 6: Hardening Qualification

Goal: v1 release confidence comes from repeatable tests, not ad hoc confidence.

Status: release-candidate command set and targeted race guidance documented;
seed/corpus and coverage expansion remain.

Tasks:

- Keep the release hardening command set documented in
  `docs/RUNTIME-HARDENING-GAUNTLET.md`.
- Add fixed seed sets and durable regression traces for gauntlet workloads.
- Extend crash/fault tests across commit, snapshot, compaction, migration, and
  shutdown boundaries.
- Expand subscription correctness scenarios beyond single-table happy paths:
  joins, deletes, updates, visibility changes, and concurrent writes.
- Keep race-enabled package guidance current as ownership changes.
- Add soak/load tests outside the default short loop.

Exit criteria:

- Release qualification includes normal tests, vet, staticcheck, targeted race
  tests, fuzz corpus replay, fixed-seed gauntlets, crash/fault tests, and a
  reference-app workload.

## Phase 7: Performance Envelopes

Goal: v1 users know the supported workload range.

Status: benchmark baseline and coverage audit documented; fixture expansion,
envelope table, and indexing guidance remain.

Tasks:

- Keep the benchmark coverage audit in `docs/PERFORMANCE-BENCHMARKS.md` current.
- Add deterministic small/medium/large fixtures.
- Add missing benchmarks for declared queries, raw subscriptions, declared live
  views, multi-way live joins, initial snapshots, fanout, replay, restore, and
  reference-app workloads.
- Decide which thresholds fail CI and which are advisory release notes.
- Publish a performance envelope table under `docs/`.
- Add indexing guidance for scans, predicates, subscriptions, and joins.

Exit criteria:

- Expensive query shapes and indexing requirements are explicit.
- Published benchmark data includes command, machine notes, data size, and
  commit hash.

## Phase 8: In-Process Trust Model

Goal: Shunter's execution boundary is clear and honest.

Status: app-author trust-model docs added and confirmation tests audited;
residual error wording and config decisions remain.

Tasks:

- Keep the v1 app-trust model documented in app-author docs.
- State reducer, lifecycle-hook, scheduler, and migration-hook side-effect
  expectations.
- Document panic and error behavior.
- Keep tests current for reducer panics, lifecycle failures, scheduler callback
  failures, migration-hook failures, cancellation, and shutdown.
- Keep `internal/processboundary` as internal/post-v1 unless a separate decision
  changes scope.

Exit criteria:

- Docs do not imply sandboxing, deterministic enforcement, timeout enforcement,
  or side-effect isolation that the runtime does not provide.

## Phase 9: Release Candidate

Goal: cut a real `v1.0.0` only after the supported surface is backed by docs,
tests, and examples.

Tasks:

- Run the release qualification command set.
- Resolve or document residual risks with explicit workload limits or non-goals.
- Update `VERSION` from `-dev` to the release version only for the release
  commit.
- Update `CHANGELOG.md`.
- Build release binaries with linker-stamped `Version`, `Commit`, and `Date`.
- Tag with `vX.Y.Z`.

Exit criteria:

- `README.md`, `docs/README.md`, `docs/v1-compatibility.md`, roadmap files, and
  reference app agree on the supported v1 story.
