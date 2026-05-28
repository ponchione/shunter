# Deferred Functionality Backlog

This document holds intentionally deferred functionality gaps. These are not
active work unless a current roadmap, issue, release gate, or user task
promotes them back into scope.

Current product decision:
- Move Shunter toward a standalone self-hosted backend/database system that
  apps run and clients talk to over the Shunter protocol.
- Use static Go app server binaries as the first implementation path for that
  standalone system. App modules are still authored in Go and linked into the
  server binary while the protocol, SDK, auth, operations, and deployment
  surfaces mature.
- Make the TypeScript client runtime publishable as an npm package consumed by
  frontend apps and generated bindings.
- Harden the existing runtime, durability, protocol, schema, contract, and docs
  surfaces first.
- Do not start generic daemon control-plane, managed service, dynamic module
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

1. [ ] Generic daemon and CLI product boundary.

Owner: root `shunter`, `cmd/shunter`, `protocol`, `auth`,
`observability/prometheus`, `internal/gauntlettests`

Deferred decision:
- Shunter's current standalone path is a static self-hosted app server binary.
- `cmd/shunter` may keep protocol-backed running-app admin commands, but it
  should not grow a generic `start` daemon, publish/update, dynamic module
  loading, or control-plane commands until dynamic serving is approved.

Review later:
- Whether Shunter needs `shunter start`.
- Data-dir and listen-address flags.
- Auth mode/key flags.
- Graceful shutdown and restart behavior.
- Built-binary server lifecycle smoke tests.
- Logs and dev-server workflow.
- Whether modules remain linked Go modules or need packaging, publish/update,
  and registration.

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
- Keep `BackupDataDir` and `RestoreDataDir` as offline DataDir copy helpers.
- Keep `Runtime.CreateSnapshot` and `Runtime.CompactCommitLog` as synchronous,
  caller-coordinated maintenance helpers. Callers still own write quiescence
  when they need a graceful maintenance point.
- Defer coordinated online backup/checkpoint orchestration.

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
- Keep current hosted-app additive compatibility reports and app-owned
  migration hooks.
- Contractdiff must reflect durable schema identity, but it is not an
  executable migration planner.
- Defer a general online/executable schema migration engine.

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

10. [ ] Maintained top-N/windowed live views beyond the single-table v1 subset.

Owner: `subscription`, `protocol`, declared-read/view surfaces

Deferred decision:
- Single-table, non-aggregate declared live views maintain `ORDER BY`, `LIMIT`,
  and `OFFSET` window membership after commits.
- The single-table v1 implementation recomputes candidate windows after
  commits rather than using incremental/index-backed top-N maintenance.
- Broader maintained windows remain deferred.

Review later:
- Maintained ordered/windowed live-result semantics for joins and aggregates.
- Delta representation.
- Admission limits.
- Index requirements.
- Incremental/index-backed maintenance.
- Interaction with joins and aggregates.

11. [ ] Incremental plans and default limit policy for high-cardinality
   multi-way live views.

Owner: `subscription`, `protocol`

Deferred decision:
- Keep current multi-way joins correctness-first while incremental join
  planning remains deferred.

Review later:
- Refreshed benchmark numbers for relation count, cardinality, self-joins,
  cross joins, and aggregate shapes.
- Incremental join plan design.
- Hard default limits, if production canaries justify them.

## Deferred Auth Expansion

12. [ ] OIDC discovery documents and background remote auth refresh.

Owner: `auth`, `protocol`, root runtime config

Deferred decision:
- Local strict-mode JWT verification now supports configured HS256, RS256, and
  ES256 verification keys with optional `kid` matching.
- Strict-mode JWKS verification now supports configured issuer/JWKS URL pairs
  with on-demand fetch, cache reuse, HTTPS-by-default URL validation, and keyed
  unknown-`kid` refresh.
- Defer OIDC discovery-document lookup and background remote refresh.

Review later:
- OIDC discovery-document lookup.
- Background cache refresh.
- Provider-specific cache lifetime policy.
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
- Keep current raw reducer-name plus BSATN args scheduler for v1.
- `sys_scheduled` is already represented as a private system table in schema,
  contracts, and generated TypeScript table helpers. Defer a typed scheduling
  model beyond that raw table representation.

Review later:
- Typed scheduled table declarations.
- Scheduled procedures.
- Higher-level interval/date semantics.
- Public or filtered contract/codegen representation for schedules.
- Protocol/export metadata beyond the raw system table.

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
- Current workflow code can generate from a contract file or linked runtime
  contract. Deployed schema fetch and source-module extraction remain deferred.

Review later:
- More language targets.
- Multi-output generation.
- Remote/deployed schema fetch.
- Source-module schema extraction without building a runtime.

22. [ ] Public codegen visibility profile.

Owner: `codegen`, contracts, root runtime

Deferred decision:
- Current TypeScript generation emits table row types, decoders, metadata, and
  table helpers for every table in the exported contract, including private
  system tables and private app tables.
- Defer a filtered public-SDK/profile model. Do not treat the current generated
  surface as a public visibility boundary.

Review later:
- Profile selection for public, internal, and private generated surfaces.
- Filtering private system tables and private app tables from public facades.
- Contract metadata needed to distinguish metadata-only exports from callable
  SDK APIs.

23. [ ] Contract workflow provenance and release automation hardening.

Owner: `contractworkflow`, `contractdiff`, `cmd/shunter`

Deferred decision:
- Defer artifact provenance until release automation needs it.

Review later:
- Binding workflow output to contract provenance.
- Including read-only consistency warnings in policy/plan paths.
- Context-aware APIs for long-running file operations.
- Machine-readable output expansion.

## Deferred Aggregates, Metrics, And Helpers

24. [ ] Aggregate semantic expansion.

Owner: `internal/valueagg`, `types`, `subscription`, `protocol`

Deferred decision:
- Keep current aggregate behavior unless app-facing requirements demand
  broader semantics.

Review later:
- `SUM` over the full numeric value domain.
- Empty-set semantics for nullable and non-null source columns.
- `DistinctSet` copy isolation.
- Memory accounting and admission limits for `COUNT(DISTINCT)`.

25. [ ] Observability surface expansion.

Owner: `observability/prometheus`, root runtime, future server boundary

Deferred decision:
- Keep current caller-assembled Prometheus wiring and fixed runtime metrics
  surface.
- `Runtime.HTTPHandler` can mount a caller-supplied metrics handler at
  `/metrics` when diagnostics mounting is configured. Automatic Prometheus
  adapter/route ownership remains deferred.

Review later:
- Metric series lifecycle/delete APIs.
- Shared metric-family registry.
- Additional runtime metrics.
- Per-metric histogram bucket configuration.
- Automatically assembled Prometheus route if standalone serving is approved.

26. [ ] `internal/atomicfile` hardening beyond current callers.

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

27. [ ] Richer value/schema type system.

Owner: `types`, `bsatn`, `schema`, contracts, codegen, protocol

Deferred decision:
- Keep the flat value/schema/codegen model for current v1.
- Current flat kinds already include wide integers, timestamp, duration, UUID,
  JSON, and a narrow string-array kind. Float constructors currently reject
  NaN values.

Review later:
- Nested products.
- Sums.
- Options/results.
- Homogeneous arrays beyond the current string-only array kind.
- Identity/connection ID column kinds.
- Recursive value system.
- Checked, saturating, or explicitly documented `Value.AsDuration` overflow
  behavior.
- JSON numeric normalization.

## Deferred Test Harness Expansion

28. [ ] Property/state-machine gauntlet harness.

Owner: `internal/gauntlettests`, root runtime

Deferred decision:
- Keep named deterministic gauntlet workloads and the fixed seed corpus for
  current hardening. Package-level fuzz and Rapid tests exist outside a
  gauntlet-level property/state-machine harness.

Review later:
- Persisting failing seeds.
- Trace shrinking.
- Generated corpus management.
- Relationship to existing fixed named workloads and package-level fuzz/Rapid
  coverage.

29. [ ] Broader storage fault-injection matrix.

Owner: `internal/gauntlettests`, `commitlog`, root runtime

Deferred decision:
- Keep current package tests and focused runtime crash/storage-fault gauntlets
  unless storage changes justify a broader matrix.

Review later:
- Write, sync, rename, durable waiter, scheduler, snapshot, commitlog sidecar,
  and fanout faults.
- Partial publication cases.
- Recovery-ordering coverage.

30. [ ] Deterministic test time and synchronization hooks.

Owner: runtime packages with scheduler, idle timeout, fanout, and close-state
tests

Deferred decision:
- Scheduler tests already have package-local clock and enqueue-observation
  hooks. Do not add broad deterministic-time or synchronization APIs across
  runtime, protocol, subscription, and close-state tests unless flake or
  implementation work justifies them.

Review later:
- Test clock hooks outside the scheduler.
- Explicit barriers for fanout absence and close behavior.
- CI flake reduction.

31. [ ] End-to-end type/index matrix.

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
