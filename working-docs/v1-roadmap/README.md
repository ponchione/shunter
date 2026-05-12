# Shunter v1 Roadmap

Status: v1 release qualification driver
Scope: release gates, auth coverage, hardening, performance status, and
remaining v1.x maintenance after the `v1.0.0` line is cut.

Shunter v1 should stay focused on self-hosted Go applications with
reducer-owned writes, durable state, typed clients, permission-aware reads, and
reliable live updates. Do not broaden v1 into SpacetimeDB wire compatibility,
cloud hosting, dynamic module upload, broad SQL compatibility, or
multi-language client generation.

## Source Of Truth

Use this file as the entry point for continuing v1 work. It owns the remaining
implementation order plus the release qualification picture that used to be
split across auth, hardening, and benchmark working docs.

Use the focused docs below for settled contracts:

- `docs/v1-compatibility.md` - stable API, protocol, contract JSON, generated
  TypeScript, read-surface, and host support matrix.
- `docs/authentication.md` - strict/dev auth behavior for app authors and
  operators.
- `docs/operations.md` - operator workflow for data dirs, backup/restore,
  migrations, upgrades, and release checklist.
- `typescript/client/README.md` - current `@shunter/client` runtime behavior.
- `working-docs/shunter-design-decisions.md` - implementation-facing decisions
  that code and tests still cite.
- `working-docs/specs/` - numbered subsystem implementation contracts.

Use live code and tests before roadmap prose when they disagree.

## Current Baseline

The repo already has the core runtime, storage, protocol, schema, subscription,
SQL/read, auth, contract, codegen, backup/restore, migration-hook,
observability, fuzz, benchmark, and gauntlet foundations.

Current v1 decisions already settled:

- `Host` is preview/advanced for v1, not required for normal app development.
- Lower-level runtime packages remain implementation details unless the v1
  compatibility matrix names a stable subset.
- v1 protocol uses the Shunter-native `v1.bsatn.shunter` token and BSATN wire
  frames.
- v1 strict auth is HS256 JWT, issuer/audience allowlist capable,
  restart-based for key replacement, and uses the `permissions` claim.
- Backup and restore are offline-only for v1.
- Shunter uses an in-process app-trust model for reducers, lifecycle hooks,
  scheduled reducers, and migration hooks.
- SQL writes, grouped aggregates, outer joins, subqueries, broad scalar
  functions, and transaction-control SQL are out of scope for v1.
- The maintained v1 canary/reference app is the external `opsboard-canary`
  repository; do not add a duplicate in-repo reference app.

## Auth Status

Current strict-auth contract:

- Strict protocol auth requires `AuthModeStrict` and a configured
  `AuthSigningKey`.
- Strict tokens are HS256 JWTs with required `iss` and `sub` claims.
- `AuthIssuers` and `AuthAudiences` are allowlists when configured.
- Expired, future-issued, not-yet-valid, malformed, wrong-algorithm,
  bad-signature, audience-mismatched, issuer-mismatched, and missing-claim
  tokens fail before WebSocket upgrade.
- Permission admission uses runtime caller permissions. Protocol strict-mode
  callers receive permissions from the token `permissions` claim; local callers
  must supply permission options explicitly.
- Visibility filters use the caller identity through `:sender`.

Covered surfaces:

- JWT validation and strict config: `auth/*_test.go`, `network_test.go`, and
  `root_validation_test.go`.
- Protocol upgrade and principal propagation: `protocol/upgrade_test.go`,
  `protocol/handle_callreducer_test.go`, and `protocol/lifecycle_test.go`.
- Local reducer admission: `local_test.go`.
- Local declared queries, declared views, table-read permissions, and
  visibility filters: `declared_read_test.go`.
- Protocol declared-read admission: `declared_read_protocol_test.go`.
- Raw one-off SQL read authorization and visibility expansion:
  `protocol/handle_oneoff_test.go`, `protocol/visibility_expansion_test.go`,
  and `read_auth_gauntlet_test.go`.
- Raw subscription visibility and deltas:
  `protocol/visibility_expansion_test.go` and `read_auth_gauntlet_test.go`.
- Strict-auth public-runtime workload: `rc_app_workload_test.go`.
- Strict-auth external canary workflow through realistic JWTs with issuer and
  audience allowlists, permission claims, sender-based visibility, protocol
  clients, and the public TypeScript SDK: external `opsboard-canary`.

## Hardening Status

The hardening campaign is the abuse suite Shunter must survive after major
implementation work lands. It should prove behavior through public surfaces,
compare against simple independent models, inject durability faults, and leave
behind reusable seeds and corpora.

Core invariants:

- Reducer success mutates committed state exactly once.
- Reducer failure does not mutate committed state.
- One-off reads return the same rows as the model for the supported query
  surface.
- Subscription initial snapshots match equivalent one-off reads where the
  syntax and row shape overlap.
- Subscription deltas equal `after - before` for the subscribed predicate.
- Unsubscribe stops future updates without corrupting other subscriptions.
- Rejected queries do not execute and do not register subscriptions.
- Disconnect, reconnect, and backpressure do not corrupt committed state or
  unrelated client fanout.
- Snapshot plus replay reaches the same state as uninterrupted execution.
- Full-log replay reaches the same state as the live runtime reached before
  shutdown.
- Corrupt or unsafe recovery input fails loudly instead of silently accepting
  damaged history.
- Scheduler/timer effects are replayed or resumed according to the documented
  Shunter contract.

Test families to keep growing:

- Public-surface model tests through hosted-runtime APIs and protocol clients.
- Recovery and crash matrix tests around append, sync, publication, snapshot,
  segment rollover, compaction, and scheduler activity.
- Fault injection for short writes, fsync failure, rename failure, truncated
  records, damaged segments, damaged snapshots, missing files, and zero tails.
- Fuzzing for SQL, BSATN, protocol messages, RowList, commitlog records,
  snapshots, and subscription canonicalization.
- Metamorphic tests comparing uninterrupted vs restarted execution,
  full-log vs snapshot-plus-replay, one-off reads vs subscribe initial rows,
  indexed paths vs equivalent scans, and repeated subscribe cycles vs
  long-lived subscriptions.
- Concurrency and soak tests for many clients, slow outbound clients,
  disconnect during send, close during reducer execution, reads around commit
  publication, scheduler firing, and restart loops.

Current gaps:

- Fixed seed sets and regression corpus entries.
- Broader crash/fault coverage across snapshot, compaction, migration, and
  shutdown boundaries.
- More subscription correctness scenarios for joins and concurrent writes.
  Delete/update predicate deltas and caller-specific visibility transitions now
  have root gauntlet coverage.
- Soak/load tests that run outside the normal short local loop.

Release-candidate hardening commands:

```bash
rtk go test ./... -count=1
rtk go vet ./...
rtk go tool staticcheck ./...
rtk go test ./internal/gauntlettests -run 'RuntimeGauntlet|ReleaseCandidateExampleApp' -count=1
rtk go test ./... -run 'RuntimeGauntlet|ReleaseCandidateExampleApp|ShortSoak' -count=1
rtk go test -race . ./executor ./protocol ./subscription ./store ./commitlog -count=1
```

Run the race set whenever a slice changes runtime concurrency, reducer
execution, protocol connection lifecycle, subscription fanout/pruning, store
index mutation, commitlog recovery, snapshot, or compaction behavior.

Fuzz corpus replay is part of normal `rtk go test ./...`; active fuzzing should
run package-at-a-time so failures are attributable:

```bash
rtk go test ./auth -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./bsatn -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./protocol -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./commitlog -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./schema -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./codegen -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./contractdiff -run '^$' -fuzz Fuzz -fuzztime=30s
rtk go test ./subscription -run '^$' -fuzz Fuzz -fuzztime=30s
```

## Performance Status

Run Go benchmarks directly, not through RTK:

```bash
go test -run '^$' -bench . -benchmem . ./executor ./protocol ./commitlog ./subscription
go test -run '^$' -bench . -benchmem -count=10 . ./executor ./protocol ./commitlog ./subscription > /tmp/shunter-v1.0.0-bench.txt
```

RTK may summarize benchmark commands and suppress the raw
`Benchmark... ns/op B/op allocs/op` rows. Normal tests, vet, staticcheck, git,
and shell commands still use RTK.

Current benchmark coverage:

| Workload area | Benchmarks | Status |
| --- | --- | --- |
| Protocol compression | `BenchmarkWrapCompressedGzip`, `BenchmarkUnwrapCompressedGzip` | covered |
| Commitlog snapshot and log recovery | `BenchmarkCreateSnapshotLarge`, `BenchmarkOpenAndRecoverSnapshotOnly`, `BenchmarkOpenAndRecoverSnapshotWithTailReplay`, `BenchmarkReplayLogSegmentedLog`, `BenchmarkOpenAndRecoverSegmentedLog` | covered for snapshot creation, snapshot-only recovery, snapshot-plus-tail recovery, and segmented log replay/recovery without snapshots |
| Offline operations | `BenchmarkBackupRestoreDataDirWorkflow`, `BenchmarkBackupRestoreDataDirWorkflowLarge` | covered for complete DataDir backup followed by restore on small and larger local fixtures; canary-scale timing remains outside the local envelope |
| Reducer write path | `BenchmarkExecutorReducerCommitRoundTrip`, `BenchmarkExecutorReducerCommitBurst64` | covered for internal executor one-at-a-time commit round trips and queued 64-command burst throughput; app-level and canary throughput remain outside the local envelope |
| Scheduler scans | `BenchmarkSchedulerScanEnqueue` | covered for enqueue scan hot path |
| One-off SQL | `BenchmarkExecuteCompiledSQLQueryCommonPaths`, `BenchmarkExecuteCompiledSQLQueryJoinReadShapes` | covered for common single-table, join, multi-way aggregate, projection, ordering, and limit paths |
| Declared reads | `BenchmarkDeclaredReadRuntimeSurfaces` | covered for local declared query execution and declared live-view initial rows, including projection/order/limit and aggregate shapes |
| Raw subscription protocol admission and network delivery | `BenchmarkHandleSubscribeSingleAdmissionReadShapes`, `BenchmarkSubscribeSingleWebSocketRoundTrip`, `BenchmarkWebSocketFanout16ClientsLightUpdate`, `BenchmarkWebSocketFanout64ClientsLightUpdate`, `BenchmarkWebSocketFanout128ClientsLightUpdate`, `BenchmarkClientSenderBackpressureFullBuffer` | covered for single-table, two-table join, and multi-way join SubscribeSingle admission, one persistent-WebSocket SubscribeSingle round trip, 16-, 64-, and 128-client WebSocket light-update fanout, and deterministic sender-level full-buffer rejection |
| Subscription equality and lifecycle | `BenchmarkEvalEqualitySubs1K`, `BenchmarkEvalEqualitySubs10K`, `BenchmarkRegisterUnregister` | covered for core hot paths |
| Subscription initial snapshots and fanout | `BenchmarkRegisterSetInitialQueryAllRows`, `BenchmarkProjectedRowsBeforeLargeBags`, `BenchmarkFanOut1KClientsSameQuery`, `BenchmarkFanOut1KClientsVariedQueries`, `BenchmarkFanOut1KClientsSkewedHotKey`, `BenchmarkFanOut1KClientsMultiTableVariedQueries` | partial; covers deterministic same-query, varied single-table, skewed hot-key, and varied two-table fanout plus memory-profile evidence for the current large initial snapshot, projected-row diff, and skewed fanout fixtures; still needs network/canary-scale and workload-derived distributions |
| Subscription joins and candidate pruning | `BenchmarkJoinFragmentEval`, `BenchmarkMultiWayLiveJoinEvalSizes`, `BenchmarkDeltaIndexConstruction`, `BenchmarkCandidateCollection` | covered for two-table joins plus deterministic small/medium/large multi-way live joins with table-shaped and aggregate deltas; `rows_512` has memory-profile evidence |

Known benchmark gaps for v1 envelopes:

- WebSocket network-level subscription workloads beyond the current
  single-connection subscribe and 16/64/128-client light-update fanout
  fixtures, including slow-reader writer/write-timeout backpressure paths and
  external canary-scale fanout. Deterministic sender-level full-buffer
  rejection now has benchmark coverage.
- workload-derived or canary fanout distributions beyond deterministic
  in-process same-query, varied single-table, skewed hot-key, and varied
  two-table predicate fixtures
- external canary workload, including canary-scale backup/restore timing
- memory profiles outside the current subscription large fixtures

Latest baseline snapshot:

- Date: 2026-05-12
- Shunter commit: `23d6bc1566f35c6e85e2f46afae7c4c7590875cc`
- Command: `go test -run '^$' -bench . -benchmem -count=10 . ./executor ./protocol ./commitlog ./subscription > /tmp/shunter-v1.0.0-bench.txt`
- Environment: linux/amd64, `go1.26.2`,
  `AMD Ryzen 9 9900X 12-Core Processor`
- Detailed advisory row table: `docs/performance-envelopes.md`

Current performance read:

- Equality subscription evaluation and candidate collection are the healthiest
  hot paths.
- Large bag diffing, large snapshot-plus-tail recovery, segmented log replay,
  and multi-way joins at larger row counts are the clearest allocation and
  latency targets in the current coverage.
- Reducer write-path coverage now includes the existing internal executor
  round trip plus a queued 64-command burst fixture; app-level and canary
  throughput remain outside the local benchmark envelope.
- Subscription fanout coverage now includes deterministic same-query, varied
  single-table, skewed hot-key, and varied two-table fixtures; workload-derived
  and canary distributions remain open.
- WebSocket fanout coverage now includes deterministic 16-, 64-, and
  128-client light-update network fixtures; slow-reader paths and external
  canary-scale fanout remain open.
- Local offline backup/restore timing now covers complete small and larger
  DataDir copy workflows; canary-scale backup/restore timing remains open.
- Memory-profile evidence now covers the existing large subscription initial
  snapshot, projected-row diff, skewed fanout, `rows_512` multi-way join,
  small/larger local backup/restore, single-WebSocket subscribe, 16-, 64-, and
  128-client WebSocket fanout, and deterministic sender-level full-buffer
  backpressure fixtures; canary-scale, slow-reader network paths, and
  production-sized backup/restore profiles remain open.
- Current measured rows are advisory. The repo does not yet define hard
  performance thresholds.

## Remaining Drivers

### 1. TypeScript SDK Completion

Status: local SDK runtime/completion hardening pass is done.

The checked-in `typescript/client` runtime already covers connection state,
token handling, IdentityToken decoding, raw reducer/query/view/table request
plumbing, RowList splitting, generated table row decoders, managed table
handles, declared-view row handles when RowList row bytes are available,
acknowledged unsubscribe, opt-in reconnect with resubscription, generated
reducer product codecs, and generated declared-read row decoders/helpers.

Maintenance tasks:

- Keep public runtime behavior tests current as protocol and codegen surfaces
  change.
- Keep the external canary app wired through the public SDK only.

Verification:

```bash
rtk npm --prefix typescript/client run test
rtk go test ./codegen ./...
rtk go vet ./...
```

### 2. External Canary Release Gate

The external `opsboard-canary` repository is the v1 proving ground. Do not add
a duplicate in-repo reference app.

Current release-gate coverage:

- `opsboard-canary` has a public `@shunter/client` SDK smoke path in
  `make sdk-smoke`, and `make canary-quick` runs it.
- `opsboard-canary` strict auth uses accepted issuer and audience allowlists and
  exercises permissioned reducers, declared reads/views, raw SQL, table
  subscriptions, declared-view subscriptions, sender-based visibility,
  backup/restore, and an app-owned offline migration path.

Tasks:

- Keep the canary on public Shunter APIs for normal operation.
- Keep coverage for strict auth, permissions, private/public tables,
  sender-based visibility, reducers, declared queries/views, raw SQL escape
  hatches, subscriptions, restart/rollback, contract export, generated
  TypeScript, offline backup/restore, and one app-owned migration path.
- Record the Shunter commit or release tag under test and the `opsboard-canary`
  commit used for each release qualification run.

Verification from the canary checkout:

```bash
rtk make canary-quick
rtk make canary-full
```

### 3. Hardening Qualification

Tasks:

- Continue adding regression corpus entries to the named gauntlet seed sets in
  `gauntlet_seed_corpus_test.go` as new failures are found.
- Extend crash/fault coverage across snapshot, compaction, migration, and
  shutdown boundaries.
- Expand subscription correctness scenarios for joins, deletes, updates,
  visibility changes, caller-specific subscriptions, and concurrent writes.
- Keep race-enabled package guidance current as ownership changes.
- Add soak/load tests that run outside the normal short local loop.

Use the release-candidate commands in this file as the active hardening gate.

### 4. Performance Envelopes

Tasks:

- Add deterministic small/medium/large fixtures.
- Add or expand benchmarks for reducer throughput, indexed lookup/range scans,
  replay/recovery, restore latency, network-level subscription workloads,
  varied-query fanout, large initial snapshots, memory profiles, and the
  external canary workload.
- Keep the envelope table under `docs/performance-envelopes.md` current with
  command, machine notes, data size, commit hash, and whether each threshold is
  advisory or release-gating.
- Keep app-author indexing guidance aligned with measured scan, predicate,
  subscription, and join limits.

### 5. Release Candidate

Tasks:

- Run the release qualification command set, TypeScript SDK tests, and canary
  gates.
- Resolve or document residual risks with explicit workload limits or
  non-goals.
- Update `CHANGELOG.md` for release-facing behavior.
- Move `VERSION` from `-dev` to the release version only for the release
  commit.
- Build release binaries with linker-stamped `Version`, `Commit`, and `Date`.
- Tag released versions with `vX.Y.Z`.

Exit criteria:

- `README.md`, `docs/README.md`, `docs/v1-compatibility.md`,
  `docs/operations.md`, this roadmap, and the external canary agree on the
  supported v1 story.
- Every stable protocol, contract JSON, generated TypeScript, auth, operation,
  read, and live-update promise has tests, fixtures, or an explicit preview
  boundary.
- Failures from hardening, fuzzing, and canary runs are reproducible from a
  seed, trace, command, or fixture.

## Maintenance Rules

- Keep this document implementation-facing.
- Delete stale handoffs instead of archiving them.
- Update this file only when remaining v1 work, release gates, auth coverage,
  hardening, or performance status changes.
- Use live code and tests before roadmap prose when they disagree.
- Keep `reference/SpacetimeDB/` read-only and research-only.
