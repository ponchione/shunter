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

- a maintained external canary/reference application
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
- Keep `Host` preview/advanced for v1 unless a separate compatibility decision
  graduates it.
- Keep generated TypeScript identifier normalization and collision suffixes as
  v1 codegen compatibility commitments.
- Keep lower-level package compatibility limited to the stable subsets in
  `docs/v1-compatibility.md`.
- Keep protocol payload and generated TypeScript golden coverage current as
  stable shapes evolve.
- Keep contract JSON golden coverage current as stable `ModuleContract` fields
  evolve.
- Keep negative tests current for unsupported SQL classes listed in
  `09-sql-read-scope.md`.
- Keep the completed `contractdiff` and `contractworkflow` policy behavior
  aligned with the final v1 compatibility rules as contract fields evolve.

Completed in this phase:

- `contractdiff` and `contractworkflow` policy checks now align with the final
  v1 compatibility rules for stable `ModuleContract` fields, unknown additive
  JSON metadata, known-field type drift, read-surface metadata,
  permissions/read_model/migrations/codegen fields, and policy classification.
- Generated TypeScript identifier normalization and collision suffix behavior
  now has dedicated v1 golden coverage across table, column, reducer,
  lifecycle, declared-read, visibility-filter, permission, and read-model
  identifier categories.
- Protocol v1 wire payload coverage now has dedicated golden fixtures for every
  stable client-to-server and server-to-client message family, stable payload
  variants, tag assignments, reserved tag policy, malformed-body handling, and
  trailing-byte rejection.
- Contract JSON golden coverage now has an explicit key-coverage guard for every
  stable `ModuleContract` field and nested metadata object, including nullable
  column JSON metadata.
- Unsupported SQL negative coverage now spans every explicit v1 non-goal class
  in `09-sql-read-scope.md` at parser admission plus OneOff and SubscribeSingle
  protocol admission.
- Unsupported SQL diagnostics coverage now pins client-visible OneOff,
  SubscribeSingle, and SubscribeMulti errors for every explicit v1 non-goal
  class, including the subscription offending-SQL suffix and no executor
  registration on rejection.
- Parser/planner coverage now pins representative supported read-matrix shapes
  and every explicit v1 SQL non-goal at the `queryplan.Build` boundary;
  generic parse failures preserve `ErrUnsupportedSQL` classification while
  keeping the existing diagnostic text.
- Auth/visibility read-surface proof now covers private/read-policy admission,
  declared-read permission context, `:sender` caller identity, and visibility
  filtering before query results, live initial rows, and live deltas across
  protocol raw reads plus local/protocol declared reads.
- Declared-read contract/codegen shape coverage now pins query projection,
  live-view projection, and live-view aggregate shapes through executable SQL
  and read-model metadata. Generated TypeScript declared-read helpers remain
  byte-level until the TypeScript client runtime adds typed decoding.
- Read-surface performance coverage now benchmarks declared query execution,
  declared live-view initial rows, raw subscription protocol admission,
  one-off SQL join/aggregate reads, and deterministic multi-way live-join
  table-shaped plus aggregate deltas.
- App-facing SQL/read documentation has been audited so broader query wording is
  either labeled as post-v1 expansion or points back to the v1 compatibility
  matrix.

Exit criteria:

- `docs/v1-compatibility.md` has no open compatibility decision that blocks
  `v1.0.0`.
- Every stable payload shape has a fixture or compatibility test.
- App-author docs cite the compatibility matrix for read-surface limits.

Verification:

```bash
rtk go test ./protocol ./codegen ./contractdiff ./contractworkflow ./query/sql ./subscription ./...
rtk go vet ./...
```

## Phase 2: Maintained Reference Application

Goal: use one maintained external canary/reference application to prove the
normal app-author and operator workflow.

Target: the external `opsboard-canary` repository. Do not add a duplicate
in-repo task-board app for v1.

Status: external canary exists and covers app-author, runtime, and offline
operations workflows; SDK wiring and release gating remain.

Tasks:

- Keep `docs/v1-roadmap/02-reference-application.md` current as the Shunter
  roadmap entry, and keep the active app contract in
  `opsboard-canary/OPSBOARD_CANARY_APP_SPEC.md`.
- Keep the canary using only public Shunter APIs for normal operation.
- Keep coverage for 5-8 varied tables, private and public read policies,
  sender-based visibility, reducers with validation and permissions, declared
  queries, declared live views, raw SQL escape-hatch use, subscriptions,
  restart, rollback, offline backup/restore, one app-owned migration path,
  contract export, and generated TypeScript fixtures.
- Keep canary dependency hygiene working against a sibling Shunter checkout.
- Wire the canary client through the v1 TypeScript SDK once the SDK shape is
  available.
- Add canary commands to the release qualification checklist once stable.

Exit criteria:

- The external canary/reference app is the documented v1 proving ground.
- It uses only public APIs for normal operation.
- It fails loudly when contract/codegen/auth/subscription/recovery ergonomics
  regress.
- It proves the offline backup/restore/migration loop through black-box
  workflow coverage.

Verification:

```bash
rtk make canary-quick
rtk make canary-full
```

Run these from the `opsboard-canary` checkout. Add the TypeScript
typecheck/test command after the SDK-backed client package exists.

## Phase 3: TypeScript Client Runtime

Goal: a normal TypeScript app should not write protocol handlers by hand.

Status: package location decided; runtime type foundation and generated import
goldens added; behavioral runtime remains.

Tasks:

- Keep the v1 package location as `typescript/client` (`@shunter/client`) unless
  a release-packaging decision explicitly moves it.
- Keep `docs/v1-roadmap/typescript-sdk-contract.md` current as the runtime API
  target before generating more helpers.
- Decide reducer argument encoding conventions. The Go runtime still accepts raw
  bytes, and generated helpers remain byte-level until this is resolved.
- Implement the actual WebSocket connection runtime for browser and Node
  clients.
- Implement subscription handles with idempotent unsubscribe.
- Implement protocol version/subprotocol mismatch errors.
- Add tests for connection transitions, auth failure, reducer/query/view
  success and failure, initial snapshots, deltas, unsubscribe, reconnect, and
  mismatch handling.
- Implement row decoding and local cache primitives for declared query/view and
  table subscription results.
- Wire the external canary app client through the SDK only.

Exit criteria:

- Typed reducer calls, declared queries, and declared views work through the SDK.
- Reconnect and unsubscribe semantics are documented and tested.
- The external canary app can be used without handwritten wire-code plumbing.

## Phase 4: Production Auth Contract

Goal: strict auth is safe to configure and clear to operate.

Status: current dev/strict auth behavior documented, issuer allowlists
implemented, strict future-token handling tested, and v1 strict-auth policy and
coverage audit documented; reference-app example remains.

Tasks:

- Keep the v1 HS256-only strict-auth policy documented unless a new roadmap
  decision expands the supported algorithms.
- Keep `AuthIssuers` issuer allowlist behavior documented and tested.
- Keep restart-based key replacement documented as the v1 operational
  procedure.
- Keep `permissions` claim mapping and no strict-mode anonymous-token behavior
  documented as v1 policy.
- Add startup/config validation for any newly required strict fields.
- Keep `docs/authentication.md` current as structured dev/strict auth guidance.
- Keep `docs/AUTH-COVERAGE.md` current as strict-auth consistency coverage
  changes.
- Keep tests current for missing signing config, invalid issuer, invalid
  audience, expired/future/malformed/wrong-algorithm tokens, missing
  permissions, and visibility-filtered reads across local and protocol paths.
- Update the external canary app to demonstrate the recommended production
  pattern.

Exit criteria:

- Strict auth fails closed by default.
- Principal derivation, permission mapping, issuer/audience validation, and key
  replacement behavior are documented and tested.

## Phase 5: Operations And Upgrade Workflow

Goal: operators have one documented path for data safety and upgrades.

Status: runbook, release checklist, clean-shutdown fresh-restore test,
incompatible-contract restore test, migration hook success/failure tests, and
build-vs-module version guardrail added; durable data-dir metadata and
compatibility checks added; reducer commit durability-failure recovery covered;
runtime snapshot/compaction fault coverage added; restore helper/CLI source
errors clarified; reference-app workflow remains.

Tasks:

- Keep `docs/operations.md` current as the focused operator runbook.
- Confirm offline-only backup for v1 or design an online backup path.
- Define snapshot retention and compaction ownership.
- Define contract policy failure behavior in startup/release workflows.
- Keep durable metadata current as app module and Shunter version semantics
  evolve.
- Keep external canary backup, restore, migration, and upgrade examples current.

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

Status: benchmark baseline, coverage audit, and schema/indexing guidance
documented; fixture expansion and envelope table remain.

Tasks:

- Keep the benchmark coverage audit in `docs/PERFORMANCE-BENCHMARKS.md` current.
- Add deterministic small/medium/large fixtures.
- Add missing benchmarks for declared queries, raw subscriptions, declared live
  views, multi-way live joins, initial snapshots, fanout, replay, restore, and
  reference-app workloads.
- Decide which thresholds fail CI and which are advisory release notes.
- Publish a performance envelope table under `docs/`.
- Keep indexing guidance current for scans, predicates, subscriptions, and
  joins as measured envelopes land.

Exit criteria:

- Expensive query shapes and indexing requirements are explicit.
- Published benchmark data includes command, machine notes, data size, and
  commit hash.

## Phase 8: In-Process Trust Model

Goal: Shunter's execution boundary is clear and honest.

Status: v1 app-trust model documented and confirmation tests audited; reducer
failure-source wording aligned.

Tasks:

- Keep the v1 app-trust model documented in app-author docs.
- State reducer, lifecycle-hook, scheduler, and migration-hook side-effect
  expectations.
- Document panic and error behavior.
- Keep tests current for reducer panics, lifecycle failures, scheduler callback
  failures, migration-hook failures, cancellation, and shutdown.
- Keep protocol reducer failure strings labeled by source: app reducer error,
  app reducer panic, permission denial, or Shunter runtime error.
- Keep `internal/processboundary` as internal/post-v1 unless a separate decision
  changes scope.
- Do not add execution-limit configuration for v1 unless the runtime gains a
  boundary it can enforce.

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
  external canary app agree on the supported v1 story.
