# Shunter Performance Envelopes

Status: latest recorded advisory v1 release-qualification snapshot
Scope: existing Go benchmarks for protocol, declared reads, executor,
commitlog, subscription, and offline operations hot paths.

This page records measured behavior for the benchmark coverage that already
exists. The rows are advisory for v1 release qualification unless a future
release gate adds hard thresholds. The preferred repo toolchain is currently
the `go.mod` `toolchain` value; the benchmark snapshot below has not been
refreshed solely for a security-only toolchain bump.

## Snapshot

- Date: 2026-05-12
- Shunter commit: `23d6bc1566f35c6e85e2f46afae7c4c7590875cc`
- Measurement worktree: release-candidate checkout based on the commit above;
  local changes during the run were release metadata and documentation only
- Host: `Linux gernsback 6.17.0-23-generic`, linux/amd64
- Go: `go1.26.2` for this recorded benchmark run; current checkout toolchain is
  pinned in `go.mod`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs

Commands:

```bash
go test -run '^$' -bench . -benchmem -count=10 . ./executor ./protocol ./commitlog ./subscription > /tmp/shunter-v1.0.0-bench.txt
rtk go run golang.org/x/perf/cmd/benchstat@latest /tmp/shunter-v1.0.0-bench.txt
```

The tables below use `benchstat` summaries from that local 10-run sample.
Every row is advisory.

## Protocol

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Compression | `WrapCompressedGzip-24` | 2 KiB repetitive body | 8.612us +/- 8% | 246.5 B +/- 4% | 3 | advisory |
| Compression | `UnwrapCompressedGzip-24` | 2 KiB repetitive body | 1.133us +/- 11% | 4.616Ki +/- 0% | 7 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/filter_limit-24` | 1,024 task rows | 2.028us +/- 2% | 1.961Ki +/- 0% | 15 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/projection_order_limit-24` | 1,024 task rows | 368.7us +/- 4% | 478.1Ki +/- 0% | 1.082k | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/count_filter-24` | 1,024 task rows | 13.61us +/- 1% | 456 B +/- 0% | 12 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/sum_filter-24` | 1,024 task rows | 20.75us +/- 1% | 616 B +/- 0% | 14 | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/two_table_join_projection_order_limit-24` | 256 users, 32 teams, 1,024 orders | 4.670ms +/- 2% | 832.9Ki +/- 0% | 4.729k | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/multi_way_join_count-24` | 256 users, 32 teams, 1,024 orders | 9.359ms +/- 2% | 558.7Ki +/- 0% | 15.12k | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/multi_way_join_sum-24` | 256 users, 32 teams, 1,024 orders | 8.821ms +/- 2% | 558.9Ki +/- 0% | 15.12k | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/single_table_filter-24` | parse and register single-table query | 1.746us +/- 14% | 3.219Ki +/- 0% | 26 | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/two_table_join-24` | parse and register two-table join | 3.485us +/- 13% | 5.492Ki +/- 0% | 44 | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/multi_way_join-24` | parse and register multi-way join | 6.495us +/- 8% | 14.67Ki +/- 0% | 92 | advisory |
| Subscribe WebSocket | `SubscribeSingleWebSocketRoundTrip-24` | persistent WebSocket; client `SubscribeSingle` write through server dispatch, executor reply, and client `SubscribeSingleApplied` read | 18.08us +/- 7% | 6.454Ki +/- 0% | 82 | advisory |
| Fanout WebSocket | `WebSocketFanout16ClientsLightUpdate-24` | 16 persistent WebSocket clients; protocol light update fanout through `ConnManager`, sender enqueue, outbound writers, and client reads | 68.39us +/- 8% | 41.41Ki +/- 0% | 624 | advisory |
| Fanout WebSocket | `WebSocketFanout64ClientsLightUpdate-24` | 64 persistent WebSocket clients; protocol light update fanout through `ConnManager`, sender enqueue, outbound writers, and client reads | 292.8us +/- 9% | 165.5Ki +/- 0% | 2.496k | advisory |
| Fanout WebSocket | `WebSocketFanout128ClientsLightUpdate-24` | 128 persistent WebSocket clients; protocol light update fanout through `ConnManager`, sender enqueue, outbound writers, and client reads | 563.9us +/- 7% | 331.1Ki +/- 0% | 4.992k | advisory |
| Slow-reader WebSocket | `WebSocketSlowReaderBackpressureUnrelatedFanout-24` | one WebSocket client held in an unread 8 MiB write with a one-message outbound queue and configured `WriteTimeout`; unrelated healthy client receives one light-update fanout over its WebSocket | 6.888us +/- 1% | 2.586Ki +/- 0% | 39 | advisory |
| Backpressure sender | `ClientSenderBackpressureFullBuffer-24` | one registered connection with a one-slot outbound queue already full; `SendTransactionUpdateLight` encodes a light update and rejects the non-blocking enqueue with `ErrClientBufferFull`; no WebSocket writer or async disconnect teardown in the timed loop | 427.4ns +/- 2% | 376 B +/- 0% | 10 | advisory |

## Executor

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Reducer commit | `ExecutorReducerCommitRoundTrip-24` | one executor goroutine; submit one external reducer call, insert one row, commit, run durability and subscription fakes, wait for response | 5.400us +/- 4% | 5.864Ki +/- 1% | 48 | advisory |
| Reducer commit | `ExecutorReducerCommitBurst64-24` | one executor goroutine; queue reducer commits in 64-command bursts, insert one row per reducer, commit each, then drain responses | 4.604us +/- 4% | 5.722Ki +/- 0% | 46 | advisory |
| Scheduler scans | `SchedulerScanEnqueue-24` | scan scheduler state and enqueue due work | 576.4ns +/- 8% | 1.320Ki +/- 0% | 9 | advisory |

## Commitlog

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Segmented replay | `ReplayLogSegmentedLog-24` | 4 segments, 256 records each | 299.2ms +/- 7% | 399.3Mi +/- 0% | 1.663M | advisory |
| Segmented recovery | `OpenAndRecoverSegmentedLog-24` | 4 segments, 256 records each | 276.5ms +/- 8% | 400.0Mi +/- 0% | 1.675M | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/small-24` | 128 snapshot rows | 279.9us +/- 15% | 747.7Ki +/- 0% | 2.076k | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/medium-24` | 1,024 snapshot rows | 1.473ms +/- 16% | 5.532Mi +/- 0% | 14.73k | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/large-24` | 4,096 snapshot rows | 6.357ms +/- 10% | 22.12Mi +/- 0% | 58.12k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/small-24` | 128 snapshot rows, 16 tail records | 1.293ms +/- 9% | 2.510Mi +/- 0% | 9.709k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/medium-24` | 1,024 snapshot rows, 128 tail records | 81.81ms +/- 9% | 113.3Mi +/- 0% | 450.4k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/large-24` | 4,096 snapshot rows, 512 tail records | 1.450s +/- 15% | 1.700Gi +/- 0% | 6.936M | advisory |
| Snapshot creation | `CreateSnapshotLarge-24` | 4,096 rows | 24.42ms +/- 23% | 2.869Mi +/- 1% | 25.25k | advisory |

## Operations

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Offline backup/restore | `BackupRestoreDataDirWorkflow-24` | 512.5 KiB DataDir: 4 log segments, 2 snapshots, metadata; backup then restore | 79.08ms +/- 12% | 31.35Ki +/- 4% | 364 | advisory |
| Offline backup/restore | `BackupRestoreDataDirWorkflowLarge-24` | 6.001 MiB DataDir: 16 log segments, 4 snapshots, metadata; backup then restore | 232.0ms +/- 13% | 81.38Ki +/- 2% | 839 | advisory |

## Declared Reads

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Declared query | `DeclaredReadRuntimeSurfaces/call_query_projection_order_limit-24` | local declared query with projection, ordering, and limit | 39.58us +/- 8% | 128.7Ki +/- 0% | 370 | advisory |
| Declared live view | `DeclaredReadRuntimeSurfaces/subscribe_view_projection_order_limit_initial-24` | local declared live-view initial rows with projection, ordering, and limit | 45.09us +/- 8% | 138.2Ki +/- 0% | 442 | advisory |
| Declared live view aggregate | `DeclaredReadRuntimeSurfaces/subscribe_view_count_initial-24` | local declared live-view count initial row | 16.87us +/- 9% | 48.74Ki +/- 0% | 195 | advisory |

## Subscription

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Equality subscription eval | `EvalEqualitySubs1K-24` | 1,000 equality subscriptions, 1 changed row | 1.124us +/- 5% | 927 B +/- 0% | 10 | advisory |
| Equality subscription eval | `EvalEqualitySubs10K-24` | 10,000 equality subscriptions, 1 changed row | 1.018us +/- 5% | 924 B +/- 0% | 10 | advisory |
| Subscription lifecycle | `RegisterUnregister-24` | register and unregister one equality query | 1.611us +/- 3% | 3.913Ki +/- 0% | 29 | advisory |
| Initial snapshot | `RegisterSetInitialQueryAllRows-24` | 1,024 committed rows | 58.53us +/- 3% | 71.27Ki +/- 0% | 77 | advisory |
| Initial snapshot diff | `ProjectedRowsBeforeLargeBags-24` | 4,096 current rows, 2,048 inserted rows, 64 distinct keys | 776.6us +/- 1% | 871.7Ki +/- 0% | 12.32k | advisory |
| Fanout | `FanOut1KClientsSameQuery-24` | 1,000 clients on one equality query | 169.3us +/- 9% | 321.3Ki +/- 0% | 2.029k | advisory |
| Fanout | `FanOut1KClientsVariedQueries-24` | 1,000 clients across equality, range, AND, and OR predicates; 256 changed rows | 1.760ms +/- 1% | 448.9Ki +/- 0% | 3.405k | advisory |
| Fanout | `FanOut1KClientsSkewedHotKey-24` | 1,000 clients with 800 on one hot equality predicate and 200 spread across cold equality, range, AND, and OR predicates; 64 changed rows | 292.8us +/- 1% | 355.1Ki +/- 0% | 2.381k | advisory |
| Fanout | `FanOut1KClientsMultiTableVariedQueries-24` | 1,000 clients split across two tables with equality, range, AND, and OR predicates; 256 changed rows per table | 3.316ms +/- 1% | 570.9Ki +/- 0% | 4.786k | advisory |
| Join delta eval | `JoinFragmentEval-24` | two-table join, 100 committed rows per side, 10 inserts per side | 148.4us +/- 2% | 81.36Ki +/- 0% | 285 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/table_shape-24` | 32 rows per joined table | 27.90us +/- 4% | 17.97Ki +/- 0% | 167 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/count-24` | 32 rows per joined table, `COUNT(*)` | 112.9us +/- 4% | 18.24Ki +/- 0% | 170 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/table_shape-24` | 128 rows per joined table | 296.7us +/- 2% | 68.84Ki +/- 0% | 371 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/count-24` | 128 rows per joined table, `COUNT(*)` | 1.638ms +/- 1% | 69.04Ki +/- 0% | 374 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/table_shape-24` | 512 rows per joined table | 4.140ms +/- 1% | 283.0Ki +/- 0% | 1.153k | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/count-24` | 512 rows per joined table, `COUNT(*)` | 24.89ms +/- 0% | 282.8Ki +/- 0% | 1.155k | advisory |
| Delta indexes | `DeltaIndexConstruction-24` | 100 changed rows, 5 indexed columns | 33.56us +/- 1% | 3.968Ki +/- 0% | 501 | advisory |
| Candidate collection | `CandidateCollection-24` | 1,000 equality subscriptions, 10 changed rows | 1.005us +/- 1% | 528 B +/- 0% | 3 | advisory |

## Current Read

- Existing equality subscription evaluation and candidate collection remain the
  healthiest hot paths.
- Large bag diffing, large snapshot-plus-tail recovery, segmented log replay,
  and multi-way joins at 512 rows per table are the clearest allocation and
  latency targets in the current coverage.
- Subscription fanout coverage now includes same-query, varied single-table,
  skewed hot-key, and varied two-table fixtures. Workload-derived and canary
  distributions remain outside the local benchmark envelope.
- Executor reducer commit coverage now includes one-at-a-time round trips and
  a queued 64-command burst fixture. These are internal executor fixtures, not
  public app or canary throughput measurements.
- Declared read coverage now includes local declared-query execution and local
  declared live-view initial rows for projection/order/limit and count shapes.
- Offline backup/restore is covered for small and larger complete local
  DataDir fixtures and is expected to be I/O dominated; these rows do not
  replace canary-scale backup/restore timing.
- WebSocket coverage now includes a single SubscribeSingle round trip and
  16-, 64-, and 128-client light-update fanout fixtures. Slow-reader
  backpressure now has a network-level advisory row for unrelated healthy
  client delivery while a second client is held in a blocked unread write.
  Deterministic sender-level full-buffer rejection is covered separately.
- The current rows are not release-blocking thresholds. Treat regressions here
  as investigation triggers until the release process defines hard limits.

## Memory Profile Notes

Subscription large-fixture memory profiles were spot-checked on 2026-05-09 at
Shunter commit `59f838f960a762e95b623408b1749dfe7678d6c1`, using the same
host and Go toolchain as the snapshot above. Profiles were written under
`/tmp` and are not repo artifacts. The skewed hot-key fanout profile was
spot-checked at Shunter commit
`0975b147e31703c385056bf664bb1a6907a6000a`.

Commands:

```bash
go test -run '^$' -bench 'BenchmarkRegisterSetInitialQueryAllRows|BenchmarkProjectedRowsBeforeLargeBags|BenchmarkMultiWayLiveJoinEvalSizes/rows_512|BenchmarkFanOut1KClientsVariedQueries' -benchmem -memprofile /tmp/shunter-subscription-mem.out ./subscription
rtk go tool pprof -top -alloc_space /tmp/shunter-subscription-mem.out
go test -run '^$' -bench 'BenchmarkProjectedRowsBeforeLargeBags$' -benchmem -memprofile /tmp/shunter-projected-rows-mem.out ./subscription
go test -run '^$' -bench 'BenchmarkMultiWayLiveJoinEvalSizes/rows_512' -benchmem -memprofile /tmp/shunter-multiway-512-mem.out ./subscription
go test -run '^$' -bench 'BenchmarkRegisterSetInitialQueryAllRows$' -benchmem -memprofile /tmp/shunter-initial-rows-mem.out ./subscription
go test -run '^$' -bench 'BenchmarkFanOut1KClientsSkewedHotKey' -benchmem -memprofile /tmp/shunter-fanout-skewed-mem.out ./subscription
rtk go tool pprof -top -alloc_space /tmp/shunter-projected-rows-mem.out
rtk go tool pprof -top -alloc_space /tmp/shunter-multiway-512-mem.out
rtk go tool pprof -top -alloc_space /tmp/shunter-initial-rows-mem.out
rtk go tool pprof -top -alloc_space /tmp/shunter-fanout-skewed-mem.out
```

Findings:

- `BenchmarkRegisterSetInitialQueryAllRows-24`: 56.125us/op,
  72,921 B/op, 77 allocs/op. Allocation space is dominated by initial
  snapshot row accumulation in `(*initialRowScanWindow).add` at about 80% of
  sampled allocation space, with `sortedRowIDs` around 12%.
- `BenchmarkProjectedRowsBeforeLargeBags-24`: 784.982us/op,
  892,502 B/op, 12,321 allocs/op. Allocation space is concentrated in
  `projectedRowsBefore` row collection, `subtractProjectedRowsByKey`, row-key
  encoding, and pooled canonical encoder release.
- `BenchmarkMultiWayLiveJoinEvalSizes/rows_512`: `table_shape` measured
  4.125ms/op, 289,611 B/op, 1,153 allocs/op; `count` measured
  24.880ms/op, 289,655 B/op, 1,155 allocs/op. Allocation space is mainly
  from before/after row materialization and projected-row diffing
  (`projectedRowsBefore`, `tableRowsAfter`, `subtractProjectedRowsByKey`,
  `encodeRowKey`) inside `evalMultiJoinDelta`.
- `BenchmarkFanOut1KClientsSkewedHotKey-24`: 290.886us/op,
  363,442 B/op, 2,381 allocs/op. Allocation space is dominated by
  `(*Manager).evaluate`, with candidate collection over distinct changed
  values next; smaller samples come from per-query evaluation and
  single-table delta evaluation.

Operations and network memory profiles were spot-checked on 2026-05-09 from a
clean detached worktree at Shunter commit
`446d7c124a3128fa954d7c3a31aeda6c8a9b9309`. The 16-client WebSocket
fanout profile was spot-checked at Shunter commit
`5d768686238922af86044a9b607ca99707b6d093`. The 64-client WebSocket
fanout profile was spot-checked at Shunter commit
`f0de4eb465f9e586802179b7eeaba2fb575af1e0`. The sender-level
backpressure profile was spot-checked from a clean detached worktree at
Shunter commit `b23f871e4f248e05f6430520f1d84d85e4d9072c`. The 128-client
WebSocket fanout profile was spot-checked at Shunter commit
`64dd7129310efa534febd779f1586b045f138efb`.
Executor reducer commit profiles were spot-checked at Shunter commit
`10c7b4c64b387441d9e2cd67caadcc62e36ff16c`.
The larger backup/restore profile was spot-checked at Shunter commit
`21fdde75ffeb82ff054ad3622297d332b4549694`.

Commands:

```bash
go test -run '^$' -bench 'BenchmarkBackupRestoreDataDirWorkflow' -benchmem -memprofile /tmp/shunter-backup-restore-mem.out .
rtk go tool pprof -top -alloc_space /tmp/shunter-backup-restore-mem.out
go test -run '^$' -bench 'BenchmarkBackupRestoreDataDirWorkflowLarge' -benchmem -memprofile /tmp/shunter-backup-restore-large-mem.out .
rtk go tool pprof -top -alloc_space /tmp/shunter-backup-restore-large-mem.out
go test -run '^$' -bench 'BenchmarkSubscribeSingleWebSocketRoundTrip' -benchmem -memprofile /tmp/shunter-websocket-subscribe-mem.out ./protocol
rtk go tool pprof -top -alloc_space /tmp/shunter-websocket-subscribe-mem.out
go test -run '^$' -bench 'BenchmarkWebSocketFanout16ClientsLightUpdate' -benchmem -memprofile /tmp/shunter-websocket-fanout-mem.out ./protocol
rtk go tool pprof -top -alloc_space /tmp/shunter-websocket-fanout-mem.out
go test -run '^$' -bench 'BenchmarkWebSocketFanout64ClientsLightUpdate' -benchmem -memprofile /tmp/shunter-websocket-fanout-64-mem.out ./protocol
rtk go tool pprof -top -alloc_space /tmp/shunter-websocket-fanout-64-mem.out
go test -run '^$' -bench 'BenchmarkWebSocketFanout128ClientsLightUpdate' -benchmem -memprofile /tmp/shunter-websocket-fanout-128-mem.out ./protocol
rtk go tool pprof -top -alloc_space /tmp/shunter-websocket-fanout-128-mem.out
go test -run '^$' -bench 'Benchmark.*Backpressure.*' -benchmem -memprofile /tmp/shunter-backpressure-mem.out ./protocol
rtk go tool pprof -top -alloc_space /tmp/shunter-backpressure-mem.out
go test -run '^$' -bench 'BenchmarkExecutorReducerCommit' -benchmem -memprofile /tmp/shunter-executor-reducer-mem.out ./executor
rtk go tool pprof -top -alloc_space /tmp/shunter-executor-reducer-mem.out
```

Findings:

- `BenchmarkBackupRestoreDataDirWorkflow-24`: 72.135ms/op,
  30,522 B/op, 363 allocs/op. The allocation-space profile is small and mixed
  with benchmark fixture setup; the timed copy path shows allocation through
  `copyDirectoryContents`, `filepath.WalkDir`, `filepath.Join`, and `os.Lstat`.
- `BenchmarkBackupRestoreDataDirWorkflowLarge-24`: 226.297ms/op,
  82,036 B/op, 839 allocs/op. The allocation-space profile is also mixed with
  benchmark fixture setup; fixture file creation dominates the sample, while
  the timed copy path remains mostly visible through directory walking, stat,
  file open, and cleanup allocations. The workload is still a local 6.001 MiB
  fixture, not canary-scale backup/restore evidence.
- `BenchmarkSubscribeSingleWebSocketRoundTrip-24`: 16.137us/op,
  6,609 B/op, 82 allocs/op. Allocation space is dominated by SQL
  tokenization/parse/query-plan construction and WebSocket read/write timeout
  machinery, with top samples in `query/sql.tokenize`, `io.ReadAll`,
  `context.AfterFunc`, `compileRawSubscribeAdmissionPlan`, and
  `normalizePredicate`.
- `BenchmarkWebSocketFanout16ClientsLightUpdate-24`: 69.272us/op,
  42,405 B/op, 624 allocs/op. Allocation space is dominated by WebSocket
  client reads and write timeout machinery across the 16 connections, with top
  samples in `io.ReadAll`, `context.(*cancelCtx).propagateCancel`,
  `context.AfterFunc`, `context.WithDeadlineCause`, `time.newTimer`,
  `websocket.(*Conn).prepareRead`, and protocol sender enqueue.
- `BenchmarkWebSocketFanout64ClientsLightUpdate-24`: 320.660us/op,
  169,521 B/op, 2,496 allocs/op. Allocation space keeps the same shape at
  64 clients: WebSocket client reads, context/deadline setup, harness
  connection setup, writer timeouts, and protocol sender enqueue dominate the
  sampled allocation space, with top samples in `io.ReadAll`,
  `context.(*cancelCtx).propagateCancel`, `context.AfterFunc`,
  `context.WithDeadlineCause`, `newBenchmarkWebSocketFanoutHarness`,
  `time.newTimer`, `websocket.(*Conn).prepareRead`, and
  `enqueueTransactionEnvelope`.
- `BenchmarkWebSocketFanout128ClientsLightUpdate-24`: 616.287us/op,
  339,010 B/op, 4,992 allocs/op. Allocation space keeps the same broad shape
  as the smaller fanout fixtures: harness connection setup, WebSocket client
  reads, context/deadline setup, writer timeouts, and protocol sender enqueue
  dominate, with top samples in `newBenchmarkWebSocketFanoutHarness`,
  `io.ReadAll`, `context.(*cancelCtx).propagateCancel`, `context.AfterFunc`,
  `context.WithDeadlineCause`, `time.newTimer`,
  `websocket.(*Conn).prepareRead`, and `enqueueTransactionEnvelope`.
- `BenchmarkClientSenderBackpressureFullBuffer-24`: 428.6ns/op,
  376 B/op, 10 allocs/op. Allocation space is dominated by rejection error
  construction, light-update server-message encoding and frame wrapping,
  subscription update validation, and connection ID formatting. This profile
  covers the deterministic full-buffer sender path and does not include
  WebSocket writes or async disconnect teardown.
- `BenchmarkExecutorReducerCommitRoundTrip-24`: 6.077us/op,
  6,791 B/op, 72 allocs/op, and
  `BenchmarkExecutorReducerCommitBurst64-24`: 5.201us/op, 6,683 B/op,
  70 allocs/op. Allocation space is dominated by row/value copying, primary
  key extraction, transaction insert tracking, commit revalidation, table
  insertion, transaction setup, and the benchmark durability acknowledgement
  channel.

## Known Gaps

These remain outside the current benchmark envelope:

- WebSocket network-level subscription workloads beyond the current
  single-connection subscribe, 16/64/128-client light-update fanout, and
  slow-reader backpressure fixtures, including external canary-scale fanout;
  deterministic sender-level full-buffer rejection is covered separately
- workload-derived or canary fanout distributions beyond the deterministic
  in-process same-query, varied single-table, skewed hot-key, and varied
  two-table predicate fixtures
- external canary workload, including canary-scale backup/restore timing
- memory profiles outside the current subscription, single-WebSocket,
  16/64/128-client WebSocket fanout, sender-level backpressure, executor
  reducer commit, and small/larger local backup/restore fixtures, including
  canary-scale, slow-reader network paths, and production-sized
  backup/restore workloads
