# Deferred Functionality Backlog

This document holds intentionally deferred functionality gaps. These are not
active work for the current self-hosted v1 hardening list in
`working-docs/functionality-gap-log.md`.

Current product decision:
- Keep Shunter focused on self-hosted Go applications that embed Shunter as a
  runtime library.
- Harden the existing runtime, durability, protocol, schema, contract, and docs
  surfaces first.
- Do not start standalone server/control-plane, managed service, dynamic module
  loading, publish/update, broad SQL, distributed database, or multi-language
  module-hosting work until this backlog is explicitly reviewed.

Clean-room rules still apply:
- Do not copy source, structure, comments, tests, or identifiers from the
  ignored `reference/` tree.
- Do not treat reference-runtime wire compatibility, byte-for-byte format
  compatibility, client interoperability, or source compatibility as goals.
- Reframe any reference-runtime capability as a Shunter-owned product/runtime
  capability before promoting it back to active work.

## Deferred Platform And Product Scope

1. [ ] Standalone server and CLI product boundary.

Owner: root `shunter`, `cmd/shunter`, `protocol`, `auth`,
`observability/prometheus`, `internal/gauntlettests`

Deferred decision:
- Shunter remains a self-hosted app-integrated runtime for now.
- `cmd/shunter` should not grow `start`, publish/update, live database
  interaction, or control-plane commands until standalone serving is approved.

Review later:
- Whether Shunter needs `shunter start`.
- Data-dir and listen-address flags.
- Auth mode/key flags.
- Graceful shutdown and restart behavior.
- WebSocket protocol operations through the built binary.
- Diagnostics and logs.
- Whether modules remain linked Go modules or need packaging, publish/update,
  and registration.
- Black-box smoke tests that invoke the built binary.

2. [ ] Managed control-plane behavior.

Owner: root `shunter`, `cmd/shunter`, future server packages

Deferred decision:
- Do not add publish/update/reset/delete, DNS/name registry, owner/org models,
  program storage, replica control, or dynamic module lifecycle in current v1
  hardening.

Review later:
- Database identity model.
- Program/module storage.
- Authorization model for control actions.
- Route shape.
- Client disconnect behavior on breaking updates.
- Migration policy enforcement at publish/update boundaries.

3. [ ] Coordinated online backup and snapshot orchestration.

Owner: root `shunter`, `commitlog`, server/CLI surfaces if added

Deferred decision:
- Keep existing backup/restore and snapshot helpers as offline or
  caller-coordinated maintenance primitives.

Review later:
- How a running runtime pauses or drains writes.
- How an online checkpoint is requested and reported.
- Recoverable artifact format.
- Operator progress/status reporting.
- CLI or HTTP exposure if standalone serving is approved.

4. [ ] Black-box installed-binary gauntlets.

Owner: `internal/gauntlettests`, root runtime, `cmd/shunter`

Deferred decision:
- Since `cmd/shunter` is not currently a server boundary, keep current gauntlets
  centered on embedded runtime and protocol paths.

Review later:
- Start/stop through a built binary.
- HTTP/WebSocket auth, reads, reducers, subscriptions, diagnostics, logs, and
  restart workflows.
- Interaction with any future `shunter start` command.

## Deferred Migration And Catalog Work

5. [ ] Automatic or online schema migration engine.

Owner: `schema`, `store`, `commitlog`, `contractdiff`, root `shunter`

Deferred decision:
- v1 remains exact/fail-fast for durable schema compatibility.
- Contractdiff must reflect durable schema identity, but it is not an
  executable migration planner.

Review later:
- Which schema changes can become explicit migration operations.
- Default/backfill semantics.
- Data rewrite steps.
- View resubscription behavior.
- Client disconnect requirements.
- How app-owned migration hooks become inputs to an executable plan.

6. [ ] Transactional catalog and online DDL.

Owner: `schema`, `store`, root runtime

Deferred decision:
- Keep the static in-memory registry for current v1.

Review later:
- Catalog storage.
- Table/index metadata mutation.
- Startup compatibility.
- Migration hooks.
- Subscription and visibility effects.
- Whether system catalog tables are needed.

7. [ ] Truncation as a durable runtime feature.

Owner: `store`, `commitlog`, `subscription`, root runtime

Deferred decision:
- Do not add a store-only truncate/clear helper.

Review later:
- Store changeset shape.
- Commitlog payload representation.
- Recovery replay semantics.
- Subscription delta behavior.
- Tests for durability and live delivery.

## Deferred Query, Visibility, And Subscription Expansion

8. [ ] Broad SQL or SQL mutation/admin surface.

Owner: `query/sql`, `internal/queryplan`, `protocol`, `subscription`, root
runtime

Deferred decision:
- Current SQL remains scoped to documented read surfaces.
- No DML, admin SQL, subqueries, outer joins, full-text, JSON-path, scalar
  functions, or mutation semantics in current v1 hardening.

Review later:
- Shunter-owned transaction semantics for SQL writes.
- Permission and durability model.
- Reducer interaction model.
- Planner ownership outside protocol compile helpers.

9. [ ] Planner-level cross-table visibility/RLS composition.

Owner: `protocol`, `internal/queryplan`, `subscription`, root `shunter`,
module declaration and contract surfaces

Deferred decision:
- Keep visibility filters single-table and row-local for v1.

Review later:
- View-like composition.
- Cycle detection.
- Alias handling.
- Caller-dependent query identity.
- Subscription hash identity.
- Resubscription behavior.
- Contract/export representation.

10. [ ] Maintained top-N/windowed live views.

Owner: `subscription`, `protocol`, declared-read/view surfaces

Deferred decision:
- Current `ORDER BY`, `LIMIT`, and `OFFSET` behavior is initial-snapshot only.

Review later:
- Maintained ordered/windowed live-result semantics.
- Delta representation.
- Admission limits.
- Index requirements.
- Interaction with joins and aggregates.

11. [ ] Production limits or incremental plans for high-cardinality multi-way
   live views.

Owner: `subscription`, `protocol`

Deferred decision:
- Keep current multi-way joins correctness-first; incremental join planning is
  still deferred.
- Production guardrails now include optional
  `Config.SubscriptionMaxMultiJoinRelations` and
  `Config.SubscriptionMaxMultiJoinRowsPerRelation`. Zero preserves the
  previous unlimited behavior.

Review later:
- Refreshed benchmark numbers for relation count, cardinality, self-joins,
  cross joins, and aggregate shapes.
- Incremental join plan design.
- Hard default limits, if production canaries justify them.

## Deferred Auth Expansion

12. [ ] Remote auth key discovery and automatic rotation caches.

Owner: `auth`, `protocol`, root runtime config

Deferred decision:
- Local strict-mode JWT verification now supports configured HS256, RS256, and
  ES256 verification keys with optional `kid` matching.
- Defer OIDC/JWKS discovery and automatic remote rotation caches.

Review later:
- JWKS/OIDC discovery.
- Cache lifetimes.
- Rotation behavior.
- Protocol 401 mapping.

13. [ ] Richer app-visible auth claim context.

Owner: `auth`, `protocol`, reducer context surfaces

Deferred decision:
- Keep current `AuthPrincipal` normalized fields unless app requirements demand
  more.

Review later:
- A narrow `AuthClaims` or extended `AuthPrincipal` surface.
- Copy isolation.
- Size limits.
- Extra-claim preservation.
- Avoiding raw token internals in reducer contexts.

## Deferred Commitlog And Storage Operations

14. [ ] Commitlog streaming, mirroring, and trusted remote append.

Owner: `commitlog`, root maintenance APIs, future server operation surfaces

Deferred decision:
- Keep commitlog local-file recovery/replay oriented for current v1 hardening.

Review later:
- Raw range streaming.
- Contiguous-offset checks.
- Trusted append semantics.
- Damaged-tail trimming.
- Progress reporting.
- Backup or follower-catchup workflows that justify the API.

15. [ ] Commitlog sealed-segment compression and storage accounting.

Owner: `commitlog`

Deferred decision:
- Keep compaction delete-only for current v1 hardening.

Review later:
- Sealed-segment compression.
- Transparent compressed read path.
- Segment preallocation.
- Size-on-disk reporting.
- Mixed compressed/uncompressed recovery tests.

16. [ ] Blob/page storage for large rows.

Owner: `store`, `types`, `bsatn`, root runtime

Deferred decision:
- Keep current row/value-copy storage until profiling proves it is a problem.

Review later:
- Bytes/JSON-heavy row benchmarks with secondary indexes and snapshots.
- Blob/page ownership.
- Snapshot and recovery representation.
- Indexing semantics.

## Deferred Runtime And Module Expansion

17. [ ] Crash-recovered client disconnect lifecycle replay.

Owner: `executor`, root runtime, auth/principal surfaces

Deferred decision:
- Keep startup cleanup behavior that deletes recovered `sys_clients` rows
  without running `OnDisconnect`.

Review later:
- Durable principal/context needed to replay disconnect lifecycle calls.
- Ordering with startup scheduler replay.
- App-visible semantics after process crash.

18. [ ] Typed scheduled rows/procedures and schedule metadata exports.

Owner: `executor`, `schema`, contracts, codegen, root runtime

Deferred decision:
- Keep current raw reducer-name plus args scheduler for v1.

Review later:
- Typed scheduled table declarations.
- Scheduled procedures.
- Interval/date semantics.
- Contract and codegen representation.
- Protocol/export metadata.

19. [ ] Module `init`/`update` lifecycle reducers.

Owner: `executor`, root runtime, migration surfaces

Deferred decision:
- Defer transaction-shaped module init/update lifecycle.

Review later:
- Startup/update ordering.
- Transaction semantics.
- Migration integration.
- Failure handling.

20. [ ] Out-of-process module boundary integration.

Owner: `internal/processboundary`, `executor`, root runtime

Deferred decision:
- Keep `internal/processboundary` as a package-local contract model unless
  out-of-process modules become product scope.

Review later:
- Deployable protocol envelope.
- Host-owned transaction mutation semantics.
- Committed-state-only subscription semantics.
- Lifecycle constant ownership.

## Deferred Contract, Codegen, And Workflow Breadth

21. [ ] Codegen language and output expansion.

Owner: `codegen`, contracts, `contractworkflow`, `cmd/shunter`

Deferred decision:
- Keep the current TypeScript single-output-file path until the contract and
  runtime surfaces stabilize.

Review later:
- More language targets.
- Multi-output generation.
- Remote/deployed schema fetch.
- Source-module schema extraction.

22. [ ] Codegen visibility profile.

Owner: `codegen`, contracts, root runtime

Deferred decision:
- Do not generate APIs for private/hidden surfaces without an explicit profile.

Review later:
- Internal/private declaration filtering.
- Public SDK surface.
- Contract metadata needed for generation.

23. [ ] Generated reducer result schemas.

Owner: `codegen`, contracts, root runtime

Deferred decision:
- Do not broaden generated APIs until reducer result schemas are confirmed as
  stable public contract data.

Review later:
- Result product schema consumption.
- Runtime decode behavior.
- TypeScript SDK API shape.

24. [ ] Contract workflow provenance and release automation hardening.

Owner: `contractworkflow`, `contractdiff`, `cmd/shunter`

Deferred decision:
- Defer artifact provenance until release automation needs it.

Review later:
- Binding workflow output to contract provenance.
- Including read-only consistency warnings in policy/plan paths.
- Context-aware APIs for long-running file operations.
- Machine-readable output expansion.

## Deferred Aggregates, Metrics, And Helpers

25. [ ] Aggregate semantic expansion.

Owner: `internal/valueagg`, `types`, `subscription`, `protocol`

Deferred decision:
- Keep current aggregate behavior unless app-facing requirements demand
  broader semantics.

Review later:
- `SUM` over the full numeric value domain.
- Empty-set semantics for nullable and non-null source columns.
- `DistinctSet` copy isolation.
- Memory accounting and admission limits for `COUNT(DISTINCT)`.

26. [ ] Observability surface expansion.

Owner: `observability/prometheus`, root runtime, future server boundary

Deferred decision:
- Keep current caller-assembled Prometheus wiring and current runtime metrics
  surface.

Review later:
- Metric series lifecycle/delete APIs.
- Shared metric-family registry.
- Additional runtime metrics.
- Per-metric histogram bucket configuration.
- Built-in Prometheus route if standalone serving is approved.

27. [ ] `internal/atomicfile` hardening beyond current callers.

Owner: `internal/atomicfile`

Deferred decision:
- Keep current helper semantics unless a caller needs stronger durable
  replacement behavior.

Review later:
- Platform-specific `SyncDir` fallback.
- Parent directory creation.
- Fault-injection seams.
- Preserve-mode behavior beyond permission bits.
- Symlink handling.

28. [ ] Richer value/schema type system.

Owner: `types`, `bsatn`, `schema`, contracts, codegen, protocol

Deferred decision:
- Keep the flat value/schema/codegen model for current v1.

Review later:
- Nested products.
- Sums.
- Options/results.
- Homogeneous arrays.
- Identity/connection ID column kinds.
- Recursive value system.
- Checked or saturating `Value.AsDuration` conversion.
- JSON numeric normalization.
- NaN float policy.

## Deferred Test Harness Expansion

29. [ ] Property/state-machine gauntlet harness.

Owner: `internal/gauntlettests`, root runtime

Deferred decision:
- Keep named deterministic gauntlet workloads for current hardening unless
  failures need shrinking/persistence support.

Review later:
- Persisting failing seeds.
- Trace shrinking.
- Generated corpus management.
- Relationship to existing fixed named workloads.

30. [ ] Broader storage fault-injection matrix.

Owner: `internal/gauntlettests`, `commitlog`, root runtime

Deferred decision:
- Keep current package tests and focused crash/fault coverage unless storage
  changes justify a broader matrix.

Review later:
- Write, sync, rename, durable waiter, scheduler, snapshot, commitlog sidecar,
  and fanout faults.
- Partial publication cases.
- Recovery-ordering coverage.

31. [ ] Deterministic test time and synchronization hooks.

Owner: runtime packages with scheduler, idle timeout, fanout, and close-state
tests

Deferred decision:
- Keep current timing-sensitive tests unless they become flaky or are touched
  by related implementation work.

Review later:
- Test clock hooks.
- Explicit barriers for fanout absence and close behavior.
- CI flake reduction.

32. [ ] End-to-end type/index matrix.

Owner: `internal/gauntlettests`, root runtime, protocol, codegen

Deferred decision:
- Keep package-level type/index coverage unless hosted runtime behavior needs
  broader end-to-end proof.

Review later:
- Primitive widths.
- Identity/connection IDs if added as value kinds.
- Timestamps/durations.
- Vectors or richer arrays if added.
- Unique indexes.
- Table-cache/codegen behavior.
