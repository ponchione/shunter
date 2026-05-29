# Shunter Performance Envelopes

This page records measured behavior for the benchmark coverage that already
exists. The rows are advisory unless a release process defines hard thresholds
for a specific workload. The snapshot below uses the preferred repo toolchain
from `go.mod`.

## Snapshot

- Date: 2026-05-13
- Shunter commit: `947e3dd2eb4dda1738abd670009845f127a745e0`
- Measurement worktree: checkout based on the commit above;
  local changes during the run were release metadata, documentation, and
  release-evidence logs only
- Host: `Linux gernsback 6.17.0-23-generic`, linux/amd64
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs

Commands:

```bash
go test -run '^$' -bench . -benchmem -count=10 . ./executor ./protocol ./commitlog ./subscription > working-docs/release-evidence/v1.1.0/shunter-bench-raw.log 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/v1.1.0/shunter-bench-raw.log > working-docs/release-evidence/v1.1.0/shunter-benchstat.log 2>&1
```

The tables below use `benchstat` summaries from that local 10-run sample.
Every row is advisory.

## Protocol

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Compression | `WrapCompressedGzip-24` | 2 KiB repetitive body | 8.684us +/- 5% | 251.0 B +/- 4% | 3 | advisory |
| Compression | `UnwrapCompressedGzip-24` | 2 KiB repetitive body | 916.1ns +/- 1% | 4.616Ki +/- 0% | 7 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/filter_limit-24` | 1,024 task rows | 1.874us +/- 1% | 1.961Ki +/- 0% | 15 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/projection_order_limit-24` | 1,024 task rows | 309.7us +/- 4% | 478.1Ki +/- 0% | 1.082k | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/count_filter-24` | 1,024 task rows | 13.23us +/- 3% | 456 B +/- 0% | 12 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/sum_filter-24` | 1,024 task rows | 20.11us +/- 1% | 616 B +/- 0% | 14 | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/two_table_join_projection_order_limit-24` | 256 users, 32 teams, 1,024 orders | 4.044ms +/- 1% | 832.9Ki +/- 0% | 4.729k | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/multi_way_join_count-24` | 256 users, 32 teams, 1,024 orders | 9.207ms +/- 2% | 558.7Ki +/- 0% | 15.12k | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/multi_way_join_sum-24` | 256 users, 32 teams, 1,024 orders | 8.765ms +/- 0% | 558.9Ki +/- 0% | 15.12k | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/single_table_filter-24` | parse and register single-table query | 1.649us +/- 5% | 3.250Ki +/- 0% | 26 | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/two_table_join-24` | parse and register two-table join | 3.086us +/- 3% | 5.523Ki +/- 0% | 44 | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/multi_way_join-24` | parse and register multi-way join | 6.414us +/- 9% | 14.70Ki +/- 0% | 92 | advisory |
| Subscribe WebSocket | `SubscribeSingleWebSocketRoundTrip-24` | persistent WebSocket; client `SubscribeSingle` write through server dispatch, executor reply, and client `SubscribeSingleApplied` read | 16.02us +/- 4% | 6.485Ki +/- 0% | 82 | advisory |
| Fanout WebSocket | `WebSocketFanout16ClientsLightUpdate-24` | 16 persistent WebSocket clients; protocol light update fanout through `ConnManager`, sender enqueue, outbound writers, and client reads | 59.54us +/- 3% | 41.41Ki +/- 0% | 624 | advisory |
| Fanout WebSocket | `WebSocketFanout64ClientsLightUpdate-24` | 64 persistent WebSocket clients; protocol light update fanout through `ConnManager`, sender enqueue, outbound writers, and client reads | 237.6us +/- 4% | 165.5Ki +/- 0% | 2.496k | advisory |
| Fanout WebSocket | `WebSocketFanout128ClientsLightUpdate-24` | 128 persistent WebSocket clients; protocol light update fanout through `ConnManager`, sender enqueue, outbound writers, and client reads | 468.2us +/- 4% | 331.1Ki +/- 0% | 4.992k | advisory |
| Slow-reader WebSocket | `WebSocketSlowReaderBackpressureUnrelatedFanout-24` | one WebSocket client held in an unread 8 MiB write with a one-message outbound queue and configured `WriteTimeout`; unrelated healthy client receives one light-update fanout over its WebSocket | 6.962us +/- 2% | 2.586Ki +/- 0% | 39 | advisory |
| Backpressure sender | `ClientSenderBackpressureFullBuffer-24` | one registered connection with a one-slot outbound queue already full; `SendTransactionUpdateLight` encodes a light update and rejects the non-blocking enqueue with `ErrClientBufferFull`; no WebSocket writer or async disconnect teardown in the timed loop | 382.8ns +/- 1% | 376 B +/- 0% | 10 | advisory |

## Compression Corpus

`protocol` includes a focused compression corpus benchmark for server-message
frames. The corpus is built from `EncodeServerMessage` output and covers tiny
messages below `DefaultGzipMinBytes`, the older 2 KiB repetitive compression
fixture shape, large initial subscription rows, multi-table subscription
updates, light and heavy transaction updates, one-off query results,
string-heavy rows, mixed rows, and mostly-random bytes-heavy rows.

Refresh command:

```bash
go test -run '^$' -bench 'BenchmarkCompressionCorpus' -benchmem -count=10 ./protocol
```

The corpus reports `wire_B/op`, `wire_pct`, and `saved_pct` for plain frames,
`CompressionNone` envelopes, production gzip, and benchmark-local brotli q1/q4
candidates.

Focused snapshot:

- Date: 2026-05-28
- Shunter commit: `4385e358895cd917d4d45ffaeb7a63cb17f0237e`
- Host: `Linux gernsback 6.17.0-29-generic`, linux/amd64
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs
- Raw sample: 100 sub-benchmarks, `-count=10`, total package benchmark time
  1387.460s
- Raw output: `/tmp/shunter-compression-corpus-bench-raw-20260528.txt`
- Benchstat output: `/tmp/shunter-compression-corpus-benchstat-20260528.txt`

Command:

```bash
go test -run '^$' -bench 'BenchmarkCompressionCorpus' -benchmem -count=10 ./protocol > /tmp/shunter-compression-corpus-bench-raw-20260528.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest /tmp/shunter-compression-corpus-bench-raw-20260528.txt > /tmp/shunter-compression-corpus-benchstat-20260528.txt 2>&1
```

Representative production gzip standings:

| Fixture | Encode sec/op | Decode sec/op | Wire % | Saved % | Encode B/op | Decode B/op | Gate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| tiny unsubscribe | 13.53ns +/- 1% | 32.34ns +/- 12% | 105.6% | -5.56% | 24 B +/- 0% | 48 B +/- 0% | advisory |
| 2 KiB repetitive | 10.07us +/- 10% | 3.04us +/- 6% | 5.66% | 94.3% | 253 B +/- 5% | 9.60Ki +/- 0% | advisory |
| single large initial | 628.20us +/- 2% | 78.21us +/- 7% | 22.3% | 77.7% | 32.32Ki +/- 2% | 171.71Ki +/- 0% | advisory |
| multi-table initial | 455.97us +/- 2% | 69.16us +/- 2% | 16.5% | 83.5% | 16.34Ki +/- 3% | 193.37Ki +/- 0% | advisory |
| light many changes | 730.45us +/- 1% | 109.08us +/- 6% | 16.8% | 83.2% | 32.39Ki +/- 1% | 270.18Ki +/- 0% | advisory |
| heavy reducer args | 276.64us +/- 2% | 52.57us +/- 2% | 15.7% | 84.3% | 16.07Ki +/- 2% | 165.82Ki +/- 0% | advisory |
| one-off several tables | 466.45us +/- 2% | 75.46us +/- 7% | 24.5% | 75.5% | 32.38Ki +/- 2% | 188.43Ki +/- 0% | advisory |
| string-heavy rows | 260.28us +/- 5% | 54.98us +/- 15% | 7.23% | 92.8% | 7.87Ki +/- 2% | 243.18Ki +/- 0% | advisory |
| mixed rows | 400.48us +/- 1% | 54.78us +/- 1% | 24.2% | 75.8% | 16.01Ki +/- 3% | 116.43Ki +/- 0% | advisory |
| random bytes rows | 267.35us +/- 5% | 82.16us +/- 4% | 91.2% | 8.84% | 64.21Ki +/- 0% | 95.53Ki +/- 0% | advisory |

Best benchmark-local brotli wire result compared with production gzip:

| Fixture | Best brotli | Gzip wire % | Brotli wire % | Brotli wire delta vs gzip | Brotli encode | Brotli decode | Gate |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| 2 KiB repetitive | `brotli_q4` | 5.66% | 3.62% | 36.1% smaller | 80.94us +/- 18% | 12.47us +/- 11% | advisory |
| single large initial | `brotli_q4` | 22.3% | 22.5% | 0.9% larger | 469.23us +/- 2% | 114.21us +/- 1% | advisory |
| multi-table initial | `brotli_q4` | 16.5% | 16.2% | 2.1% smaller | 479.70us +/- 16% | 101.82us +/- 1% | advisory |
| light many changes | `brotli_q4` | 16.8% | 16.3% | 2.6% smaller | 683.31us +/- 3% | 143.84us +/- 3% | advisory |
| heavy reducer args | `brotli_q4` | 15.7% | 14.8% | 6.1% smaller | 409.70us +/- 3% | 83.79us +/- 4% | advisory |
| one-off several tables | `brotli_q4` | 24.5% | 23.6% | 3.9% smaller | 566.85us +/- 6% | 115.53us +/- 2% | advisory |
| string-heavy rows | `brotli_q4` | 7.23% | 8.51% | 17.7% larger | 401.81us +/- 56% | 91.22us +/- 17% | advisory |
| mixed rows | `brotli_q4` | 24.2% | 23.1% | 4.5% smaller | 353.00us +/- 14% | 81.13us +/- 7% | advisory |
| random bytes rows | `brotli_q1` | 91.2% | 90.3% | 0.9% smaller | 177.88us +/- 4% | 100.30us +/- 3% | advisory |

Current read:

- Production gzip remains the right supported compression mode for the current
  corpus.
- Brotli only beats gzip by a material wire-size margin on the older synthetic
  2 KiB repetitive fixture, where `brotli_q4` also costs about 8x gzip encode
  time and about 4x gzip decode time.
- On product-shaped large fixtures, best brotli wire savings range from worse
  than gzip to 6.1% smaller, below the material-margin rubric.
- Brotli decode is slower than gzip on every representative row above, and
  brotli encode allocations are much larger in the full benchstat output.
- Keep `github.com/andybalholm/brotli` as benchmark-only evidence tooling;
  do not add runtime brotli support without client or product pressure.

Decision rubric:

- Consider brotli only when real workload evidence shows large, compressible
  frames are common or bandwidth is constrained.
- A brotli candidate should beat gzip by a material wire-size margin on
  realistic large compressible fixtures, roughly 15% or more smaller than gzip.
- Encode cost must be acceptable for realtime fanout, especially at q1/q4.
- Random or bytes-heavy fixtures are negative controls and should not drive the
  decision.
- Benchmark evidence alone is not enough without a client or product need.

## Executor

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Reducer commit | `ExecutorReducerCommitRoundTrip-24` | one executor goroutine; submit one external reducer call, insert one row, commit, run durability and subscription fakes, wait for response | 5.062us +/- 3% | 5.901Ki +/- 1% | 48 | advisory |
| Reducer commit | `ExecutorReducerCommitBurst64-24` | one executor goroutine; queue reducer commits in 64-command bursts, insert one row per reducer, commit each, then drain responses | 4.120us +/- 1% | 5.712Ki +/- 0% | 46 | advisory |
| Scheduler scans | `SchedulerScanEnqueue-24` | scan scheduler state and enqueue due work | 527.4ns +/- 1% | 1.320Ki +/- 0% | 9 | advisory |

## Commitlog

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Segmented replay | `ReplayLogSegmentedLog-24` | 4 segments, 256 records each | 272.4ms +/- 1% | 399.3Mi +/- 0% | 1.663M | advisory |
| Segmented recovery | `OpenAndRecoverSegmentedLog-24` | 4 segments, 256 records each | 263.9ms +/- 3% | 399.9Mi +/- 0% | 1.675M | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/small-24` | 128 snapshot rows | 195.7us +/- 2% | 747.4Ki +/- 0% | 2.075k | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/medium-24` | 1,024 snapshot rows | 1.230ms +/- 3% | 5.532Mi +/- 0% | 14.73k | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/large-24` | 4,096 snapshot rows | 5.133ms +/- 1% | 22.12Mi +/- 0% | 58.12k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/small-24` | 128 snapshot rows, 16 tail records | 1.174ms +/- 2% | 2.510Mi +/- 0% | 9.709k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/medium-24` | 1,024 snapshot rows, 128 tail records | 74.39ms +/- 1% | 113.3Mi +/- 0% | 450.4k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/large-24` | 4,096 snapshot rows, 512 tail records | 1.315s +/- 3% | 1.700Gi +/- 0% | 6.936M | advisory |
| Snapshot creation | `CreateSnapshotLarge-24` | 4,096 rows | 24.40ms +/- 6% | 2.867Mi +/- 0% | 25.23k | advisory |

## Operations

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Offline backup/restore | `BackupRestoreDataDirWorkflow-24` | 512.5 KiB DataDir: 4 log segments, 2 snapshots, metadata; backup then restore | 78.19ms +/- 10% | 42.04Ki +/- 3% | 458 | advisory |
| Offline backup/restore | `BackupRestoreDataDirWorkflowLarge-24` | 6.001 MiB DataDir: 16 log segments, 4 snapshots, metadata; backup then restore | 211.7ms +/- 9% | 95.90Ki +/- 1% | 961 | advisory |

## Declared Reads

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Declared query | `DeclaredReadRuntimeSurfaces/call_query_projection_order_limit-24` | local declared query with projection, ordering, and limit | 37.67us +/- 1% | 128.7Ki +/- 0% | 370 | advisory |
| Declared live view | `DeclaredReadRuntimeSurfaces/subscribe_view_projection_order_limit_initial-24` | local declared live-view initial rows with projection, ordering, and limit | 38.05us +/- 1% | 138.7Ki +/- 0% | 446 | advisory |
| Declared live view aggregate | `DeclaredReadRuntimeSurfaces/subscribe_view_count_initial-24` | local declared live-view count initial row | 13.23us +/- 2% | 48.75Ki +/- 0% | 195 | advisory |

## Subscription

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Equality subscription eval | `EvalEqualitySubs1K-24` | 1,000 equality subscriptions, 1 changed row | 1.048us +/- 1% | 927 B +/- 0% | 10 | advisory |
| Equality subscription eval | `EvalEqualitySubs10K-24` | 10,000 equality subscriptions, 1 changed row | 969.7ns +/- 1% | 924 B +/- 0% | 10 | advisory |
| Subscription lifecycle | `RegisterUnregister-24` | register and unregister one equality query | 1.561us +/- 2% | 3.913Ki +/- 0% | 29 | advisory |
| Initial snapshot | `RegisterSetInitialQueryAllRows-24` | 1,024 committed rows | 56.40us +/- 2% | 71.27Ki +/- 0% | 77 | advisory |
| Initial snapshot diff | `ProjectedRowsBeforeLargeBags-24` | 4,096 current rows, 2,048 inserted rows, 64 distinct keys | 756.6us +/- 1% | 871.8Ki +/- 0% | 12.32k | advisory |
| Fanout | `FanOut1KClientsSameQuery-24` | 1,000 clients on one equality query | 155.7us +/- 2% | 321.3Ki +/- 0% | 2.029k | advisory |
| Fanout | `FanOut1KClientsVariedQueries-24` | 1,000 clients across equality, range, AND, and OR predicates; 256 changed rows | 1.746ms +/- 1% | 449.0Ki +/- 0% | 3.405k | advisory |
| Fanout | `FanOut1KClientsSkewedHotKey-24` | 1,000 clients with 800 on one hot equality predicate and 200 spread across cold equality, range, AND, and OR predicates; 64 changed rows | 298.8us +/- 3% | 355.1Ki +/- 0% | 2.381k | advisory |
| Fanout | `FanOut1KClientsMultiTableVariedQueries-24` | 1,000 clients split across two tables with equality, range, AND, and OR predicates; 256 changed rows per table | 3.252ms +/- 0% | 570.9Ki +/- 0% | 4.786k | advisory |
| Join delta eval | `JoinFragmentEval-24` | two-table join, 100 committed rows per side, 10 inserts per side | 145.9us +/- 2% | 81.35Ki +/- 0% | 285 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/table_shape-24` | 32 rows per joined table | 29.35us +/- 1% | 17.97Ki +/- 0% | 167 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/count-24` | 32 rows per joined table, `COUNT(*)` | 107.9us +/- 2% | 18.25Ki +/- 0% | 170 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/table_shape-24` | 128 rows per joined table | 319.4us +/- 1% | 68.84Ki +/- 0% | 371 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/count-24` | 128 rows per joined table, `COUNT(*)` | 1.532ms +/- 0% | 69.05Ki +/- 0% | 374 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/table_shape-24` | 512 rows per joined table | 4.485ms +/- 1% | 282.9Ki +/- 0% | 1.153k | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/count-24` | 512 rows per joined table, `COUNT(*)` | 23.65ms +/- 0% | 282.9Ki +/- 0% | 1.155k | advisory |
| Delta indexes | `DeltaIndexConstruction-24` | 100 changed rows, 5 indexed columns | 33.96us +/- 0% | 3.965Ki +/- 0% | 501 | advisory |
| Candidate collection | `CandidateCollection-24` | 1,000 equality subscriptions, 10 changed rows | 1.003us +/- 1% | 528 B +/- 0% | 3 | advisory |

## Focused Multi-Way Live Join Stage A Baseline

This focused snapshot refreshes the existing multi-way live join size rows and
publishes the relation-shape rows that were already present in
`subscription/bench_test.go`. It is advisory and should be refreshed with
before/after `benchstat` output when changing multi-way live join evaluation,
candidate pruning, aggregate join evaluation, or guardrail policy.

- Date: 2026-05-28
- Shunter commit: `69df08549a22d8c2bb135131b3cc18900f370771`
- Host: `Linux gernsback 6.17.0-29-generic`, linux/amd64
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs
- Raw sample: 9 sub-benchmarks, `-count=10`, 90 benchmark rows, total
  package benchmark time 129.422s
- Raw output: `/tmp/shunter-subscription-stage-a-bench-raw-20260528.txt`
- Benchstat output: `/tmp/shunter-subscription-stage-a-benchstat-20260528.txt`

Command:

```bash
rtk bash -lc 'go test -run "^$" -bench "BenchmarkMultiWayLiveJoin(EvalSizes|RelationShapes)" -benchmem -count=10 ./subscription > /tmp/shunter-subscription-stage-a-bench-raw-20260528.txt 2>&1'
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest /tmp/shunter-subscription-stage-a-bench-raw-20260528.txt > /tmp/shunter-subscription-stage-a-benchstat-20260528.txt 2>&1'
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/table_shape-24` | 32 rows per joined table, table-shaped projection | 31.88us +/- 1% | 12.31Ki +/- 0% | 68 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/count-24` | 32 rows per joined table, `COUNT(*)` | 126.8us +/- 1% | 12.63Ki +/- 0% | 73 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/table_shape-24` | 128 rows per joined table, table-shaped projection | 384.3us +/- 0% | 37.37Ki +/- 0% | 68 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/count-24` | 128 rows per joined table, `COUNT(*)` | 1.857ms +/- 0% | 37.66Ki +/- 0% | 73 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/table_shape-24` | 512 rows per joined table, table-shaped projection | 5.648ms +/- 0% | 148.5Ki +/- 0% | 68 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/count-24` | 512 rows per joined table, `COUNT(*)` | 28.92ms +/- 0% | 148.7Ki +/- 0% | 73 | advisory |
| Multi-way relation shape | `MultiWayLiveJoinRelationShapes/chain3-24` | 3-relation chain, 128 rows per relation, one endpoint insert | 382.0us +/- 0% | 37.37Ki +/- 0% | 68 | advisory |
| Multi-way relation shape | `MultiWayLiveJoinRelationShapes/self_alias3-24` | 3 aliases over 2 physical tables, 128-row fixture, one repeated-table insert | 490.9us +/- 0% | 38.55Ki +/- 0% | 71 | advisory |
| Multi-way relation shape | `MultiWayLiveJoinRelationShapes/chain4-24` | 4-relation chain, 128 rows per relation, one endpoint insert | 1.067ms +/- 1% | 50.42Ki +/- 0% | 88 | advisory |
| Multi-way focused geomean | all focused multi-way live join benchmarks | 9 sub-benchmark geomean | 769.2us | 41.31Ki | 72 | advisory |

## Focused Multi-Way Live Join Stage B Dimensions

This focused snapshot adds bounded benchmark dimensions for the Stage B
subscription evidence slice: one Cartesian multi-join relation shape,
selectivity/skew rows, changed-row-count variation, and `COUNT(*)` aggregate
relation-shape variants over accepted multi-join shapes. It is advisory and
does not change default multi-way join guardrails.

- Date: 2026-05-28
- Shunter commit: `1c40533f56dcaf7bd45b395bb906bd79e13bd343`
- Measurement worktree: commit above plus Stage B benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic`, linux/amd64
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs
- Raw sample: 13 sub-benchmarks, `-count=10`, 130 benchmark rows, total
  package benchmark time 177.668s
- Raw output: `/tmp/shunter-subscription-stage-b-bench-raw-20260528.txt`
- Benchstat output: `/tmp/shunter-subscription-stage-b-benchstat-20260528.txt`

Command:

```bash
rtk bash -lc 'go test -run "^$" -bench "BenchmarkMultiWayLiveJoin(RelationShapes|Selectivity|ChangedRows|AggregateRelationShapes)$" -benchmem -count=10 ./subscription > /tmp/shunter-subscription-stage-b-bench-raw-20260528.txt 2>&1'
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest /tmp/shunter-subscription-stage-b-bench-raw-20260528.txt > /tmp/shunter-subscription-stage-b-benchstat-20260528.txt 2>&1'
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way relation shape | `MultiWayLiveJoinRelationShapes/chain3-24` | 3-relation chain, 128 rows per relation, one endpoint insert | 385.6us +/- 1% | 37.37Ki +/- 0% | 68 | advisory |
| Multi-way relation shape | `MultiWayLiveJoinRelationShapes/self_alias3-24` | 3 aliases over 2 physical tables, 128-row fixture, one repeated-table insert | 496.2us +/- 1% | 38.55Ki +/- 0% | 71 | advisory |
| Multi-way relation shape | `MultiWayLiveJoinRelationShapes/chain4-24` | 4-relation chain, 128 rows per relation, one endpoint insert | 1.064ms +/- 1% | 50.42Ki +/- 0% | 88 | advisory |
| Multi-way relation shape | `MultiWayLiveJoinRelationShapes/cross3_rows_24-24` | 3-relation Cartesian multi-join, 24 rows per relation, one endpoint insert | 50.09us +/- 1% | 86.70Ki +/- 0% | 85 | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/one_match-24` | 128 rows per relation, one changed row matching one left/middle tuple | 351.4us +/- 1% | 37.37Ki +/- 0% | 68 | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_8x8-24` | 128 rows per relation, one changed hot-key row matching 8 left rows by 8 middle rows | 361.7us +/- 1% | 45.79Ki +/- 0% | 79 | advisory |
| Multi-way changed rows | `MultiWayLiveJoinChangedRows/rows_128/changed_1-24` | 128 rows per relation, 1 inserted endpoint row | 383.3us +/- 1% | 37.37Ki +/- 0% | 68 | advisory |
| Multi-way changed rows | `MultiWayLiveJoinChangedRows/rows_128/changed_10-24` | 128 rows per relation, 10 inserted endpoint rows | 436.7us +/- 1% | 40.35Ki +/- 0% | 77 | advisory |
| Multi-way changed rows | `MultiWayLiveJoinChangedRows/rows_128/changed_100-24` | 128 rows per relation, 100 inserted endpoint rows | 953.7us +/- 0% | 91.98Ki +/- 0% | 183 | advisory |
| Multi-way aggregate shape | `MultiWayLiveJoinAggregateRelationShapes/chain3/count-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.366ms +/- 1% | 37.66Ki +/- 0% | 73 | advisory |
| Multi-way aggregate shape | `MultiWayLiveJoinAggregateRelationShapes/self_alias3/count-24` | `COUNT(*)` over 3 aliases across 2 physical tables, 128-row fixture, one repeated-table insert | 102.3ms +/- 2% | 39.45Ki +/- 0% | 77 | advisory |
| Multi-way aggregate shape | `MultiWayLiveJoinAggregateRelationShapes/chain4/count-24` | `COUNT(*)` over 4-relation chain, 128 rows per relation, one endpoint insert | 4.708ms +/- 0% | 50.66Ki +/- 0% | 93 | advisory |
| Multi-way aggregate shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_24/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 24 rows per relation, one endpoint insert | 309.5us +/- 1% | 9.964Ki +/- 0% | 73 | advisory |
| Multi-way Stage B geomean | all focused Stage B multi-way live join benchmarks | 13 sub-benchmark geomean | 817.6us | 41.61Ki | 81.58 | advisory |

Current read:

- The bounded 3-relation Cartesian fixture is intentionally much smaller than
  the 128-row chain fixtures. Its delta row emits a 24x24 Cartesian fragment;
  this is useful shape evidence, not a default-limit proposal.
- Changed-row-count evidence is now present for 1, 10, and 100 inserted
  endpoint rows on the 128-row chain fixture.
- The hot-key fixture shows a modest local cost increase for one changed row
  matching 8 left rows by 8 middle rows. Larger skew remains outside this
  bounded slice.
- The accepted `COUNT(*)` aggregate relation-shape rows show the repeated-table
  self-alias aggregate path is the standout latency case in this slice.

## Focused Multi-Way Live Join Stage D Aggregate Functions

This focused snapshot adds bounded aggregate-function evidence for the
multi-way live join aggregate functions already accepted by the subscription
layer. It uses the existing 128-row `chain3` fixture and is advisory for
default-limit policy.

- Date: 2026-05-29
- Shunter commit: `2303f576344eecf62c73d5998aad0c7d01b7f344`
- Measurement worktree: commit above plus Stage D benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic`, linux/amd64
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs
- Raw sample: 4 sub-benchmarks, `-count=10`, 40 benchmark rows, total package
  benchmark time 57.571s
- Raw output:
  `/tmp/shunter-subscription-stage-d-aggregate-functions-raw-20260529.txt`
- Benchstat output:
  `/tmp/shunter-subscription-stage-d-aggregate-functions-benchstat-20260529.txt`

Command:

```bash
rtk bash -lc 'go test -run "^$" -bench "BenchmarkMultiWayLiveJoinAggregateFunctions$" -benchmem -count=10 ./subscription > /tmp/shunter-subscription-stage-d-aggregate-functions-raw-20260529.txt 2>&1'
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest /tmp/shunter-subscription-stage-d-aggregate-functions-raw-20260529.txt > /tmp/shunter-subscription-stage-d-aggregate-functions-benchstat-20260529.txt 2>&1'
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain3/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.365ms +/- 1% | 37.66Ki +/- 0% | 73 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain3/count_column-24` | `COUNT(r.id)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.374ms +/- 1% | 37.66Ki +/- 0% | 73 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain3/count_distinct-24` | `COUNT(DISTINCT t.id)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.403ms +/- 0% | 117.8Ki +/- 0% | 347 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain3/sum-24` | `SUM(r.id)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.365ms +/- 1% | 37.78Ki +/- 0% | 75 | advisory |
| Multi-way Stage D aggregate geomean | all focused Stage D aggregate-function benchmarks | 4 sub-benchmark geomean | 2.377ms | 50.12Ki | 108.5 | advisory |

Current read:

- `COUNT(column)` and `SUM(column)` track the existing `COUNT(*)` chain3
  latency envelope in this bounded fixture.
- `COUNT(DISTINCT column)` increases allocation and allocation count, but does
  not create a new latency standout in the bounded 128-row `chain3` sample.
- This evidence supports keeping default multi-way join guardrails unlimited.
  The bounded rows remain advisory, the worst local rows are not enough to
  select safe defaults, and apps can still opt into guardrails through config.

## Focused Multi-Way Live Join Stage F Relation Count

This focused snapshot extends bounded relation-count evidence from the existing
3- and 4-relation chain fixtures to a 5-relation chain. The new fixture keeps
the same 128 rows per relation and one endpoint insert shape, and records both
table-shaped projection and `COUNT(*)` aggregate rows. It is advisory and does
not change default multi-way join guardrails.

- Date: 2026-05-29
- Shunter commit: `9ac9bba5d037acad1944ab2888d81ebf8b9adb4d`
- Measurement worktree: commit above plus Stage F benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic`, linux/amd64
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs
- Raw sample: 25 sub-benchmarks, `-count=10`, 250 benchmark rows, total
  package benchmark time 353.572s
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-f/multiway-bench-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-f/multiway-benchstat.log`

Command:

```bash
rtk bash -lc 'go test -run "^$" -bench "BenchmarkMultiWayLiveJoin(EvalSizes|RelationShapes|Selectivity|ChangedRows|AggregateRelationShapes|AggregateFunctions)" -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-f/multiway-bench-raw.log 2>&1'
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-f/multiway-bench-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-f/multiway-benchstat.log 2>&1'
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way relation count | `MultiWayLiveJoinRelationShapes/chain3-24` | 3-relation chain, 128 rows per relation, one endpoint insert | 383.3us +/- 1% | 37.37Ki +/- 0% | 68 | advisory |
| Multi-way relation count | `MultiWayLiveJoinRelationShapes/chain4-24` | 4-relation chain, 128 rows per relation, one endpoint insert | 1.055ms +/- 1% | 50.42Ki +/- 0% | 88 | advisory |
| Multi-way relation count | `MultiWayLiveJoinRelationShapes/chain5_rows_128-24` | 5-relation chain, 128 rows per relation, one endpoint insert | 2.057ms +/- 0% | 62.38Ki +/- 0% | 105 | advisory |
| Multi-way aggregate relation count | `MultiWayLiveJoinAggregateRelationShapes/chain3/count-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.346ms +/- 0% | 37.67Ki +/- 0% | 73 | advisory |
| Multi-way aggregate relation count | `MultiWayLiveJoinAggregateRelationShapes/chain4/count-24` | `COUNT(*)` over 4-relation chain, 128 rows per relation, one endpoint insert | 4.648ms +/- 0% | 50.66Ki +/- 0% | 93 | advisory |
| Multi-way aggregate relation count | `MultiWayLiveJoinAggregateRelationShapes/chain5_rows_128/count-24` | `COUNT(*)` over 5-relation chain, 128 rows per relation, one endpoint insert | 7.591ms +/- 0% | 62.55Ki +/- 0% | 110 | advisory |
| Multi-way Stage F geomean | all focused Stage F multi-way live join benchmarks | 25 sub-benchmark geomean | 1.114ms | 44.16Ki | 84.28 | advisory |

Current read:

- The bounded 5-relation chain stays local-review-sized at 128 rows per
  relation while extending relation-count evidence beyond the prior `chain4`
  fixture.
- The chain relation-count rows grow predictably in this deterministic fixture,
  and the accepted `COUNT(*)` chain5 row remains well below the repeated-table
  self-alias aggregate standout from Stage B.
- Larger relation counts, larger Cartesian/skew fixtures, aggregate-function
  rows beyond the bounded `chain3` fixture, and app-derived workload
  distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage G Skew/Fanout

This focused snapshot extends bounded multi-way selectivity/skew evidence from
the existing `hot_key_8x8` fixture to a `hot_key_16x16` fixture. The new row
keeps the same 128 rows per relation and one changed endpoint row, but that
changed row now matches 16 left rows by 16 middle rows. It is advisory and does
not change default multi-way join guardrails.

- Date: 2026-05-29
- Shunter commit: `419e58055528c049b49a73dd898292911052ec50`
- Measurement worktree: commit above plus Stage G benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic`, linux/amd64
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs
- Raw sample: 16 sub-benchmarks, `-count=10`, 160 benchmark rows, total
  package benchmark time 225.201s
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-g/multiway-bench-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-g/multiway-benchstat.log`

Command:

```bash
go test -run '^$' -bench 'BenchmarkMultiWayLiveJoin(RelationShapes|Selectivity|ChangedRows|AggregateRelationShapes)' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-g/multiway-bench-raw.log 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-g/multiway-bench-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-g/multiway-benchstat.log 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/one_match-24` | 128 rows per relation, one changed row matching one left/middle tuple | 350.1us +/- 3% | 37.37Ki +/- 0% | 68 | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_8x8-24` | 128 rows per relation, one changed hot-key row matching 8 left rows by 8 middle rows | 361.8us +/- 0% | 45.79Ki +/- 0% | 79 | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_16x16-24` | 128 rows per relation, one changed hot-key row matching 16 left rows by 16 middle rows | 388.3us +/- 1% | 74.63Ki +/- 0% | 83 | advisory |
| Multi-way Stage G geomean | all focused Stage G multi-way live join benchmarks | 16 sub-benchmark geomean | 948.9us | 45.41Ki | 84.53 | advisory |

Current read:

- The bounded `hot_key_16x16` row remains local-review-sized under `-count=10`
  and extends skew/fanout coverage without turning the benchmark into a load
  test.
- In this fixture, increasing fanout from 8x8 to 16x16 increases allocation
  more visibly than latency for one changed endpoint row.
- Larger skew/fanout distributions, larger Cartesian fixtures, aggregate
  skew rows, relation counts beyond the bounded 5-relation chain, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Ordered Subscription Window Baseline

This focused snapshot records the ordered subscription window benchmarks added
after the broad v1.1.0 envelope above. It is advisory and should be refreshed
with before/after `benchstat` output when changing ordered initial-row
collection, bounded ordered windows, live ordered/limited deltas, or
ORDER BY comparator behavior.

- Date: 2026-05-26
- Shunter commit: `9c88c7ddc4153d6f33dea3f9bb2fb032f40deab3`
- Host: `Linux gernsback 6.17.0-29-generic`, linux/amd64
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs
- Raw sample: 47 sub-benchmarks, `-count=10`, 470 benchmark rows, total
  package benchmark time 634.469s

Command:

```bash
go test -run '^$' -bench 'Benchmark(RegisterSetOrderedInitialRows|InitialRowsForTableOrderedWindow|BoundedOrderedInitialRowsAdd|OrderWindowRows|OrderedInitialRowsComparatorShapes|EvalOrderedLimitedWindowDelta)' -benchmem -count=10 ./subscription > /tmp/shunter-ordered-subscription-bench-raw.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest /tmp/shunter-ordered-subscription-bench-raw.txt > /tmp/shunter-ordered-subscription-benchstat.txt 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Ordered register | `RegisterSetOrderedInitialRows/rows_128/limit_10/offset_0/ascending/1col-24` | 128 rows, top 10, ascending one-column key | 24.67us +/- 1% | 38.44Ki +/- 0% | 494 | advisory |
| Ordered register | `RegisterSetOrderedInitialRows/rows_1024/limit_100/offset_0/descending/1col-24` | 1,024 rows, top 100, descending one-column key | 291.8us +/- 2% | 291.9Ki +/- 0% | 3.454k | advisory |
| Ordered register | `RegisterSetOrderedInitialRows/rows_4096/limit_1000/offset_0/shuffled/1col-24` | 4,096 rows, top 1,000, shuffled one-column key | 1.772ms +/- 1% | 1.398Mi +/- 0% | 15.38k | advisory |
| Initial row collection | `InitialRowsForTableOrderedWindow/table_scan/rows_1024/limit_100/offset_0/shuffled/1col-24` | table scan, 1,024 rows, top 100 | 252.4us +/- 5% | 255.6Ki +/- 0% | 3.085k | advisory |
| Initial row collection | `InitialRowsForTableOrderedWindow/index_range/rows_4096/limit_100/offset_0/shuffled/2col-24` | indexed range, 4,096 rows, top 100, two-column key | 1.330ms +/- 6% | 1.623Mi +/- 0% | 12.31k | advisory |
| Bounded ordered window | `BoundedOrderedInitialRowsAdd/rows_1024/keep_100/shuffled/1col-24` | bounded add, 1,024 rows, keep 100 | 193.3us +/- 6% | 244.5Ki +/- 0% | 3.074k | advisory |
| Bounded ordered window | `BoundedOrderedInitialRowsAdd/rows_4096/keep_1000/shuffled/2col-24` | bounded add, 4,096 rows, keep 1,000, two-column key | 1.487ms +/- 10% | 1.726Mi +/- 0% | 12.29k | advisory |
| Full ordering | `OrderWindowRows/rows_4096/shuffled/2col-24` | full sort, 4,096 shuffled rows, two-column key | 1.484ms +/- 6% | 1.912Mi +/- 0% | 12.29k | advisory |
| Comparator shape | `OrderedInitialRowsComparatorShapes/bounded/rows_4096/shuffled/1col/desc/ties-24` | bounded DESC with tie-heavy keys | 1.597ms +/- 2% | 1.129Mi +/- 0% | 12.29k | advisory |
| Comparator shape | `OrderedInitialRowsComparatorShapes/full/rows_4096/descending/2col/mixed/ties-24` | full sort, mixed direction, tie-heavy two-column key | 2.114ms +/- 7% | 2.006Mi +/- 0% | 12.29k | advisory |
| Live ordered window delta | `EvalOrderedLimitedWindowDelta/rows_128/limit_10/ascending/1col/insert_head-24` | live top-10 insert into window head | 55.10us +/- 4% | 107.4Ki +/- 0% | 1.119k | advisory |
| Live ordered window delta | `EvalOrderedLimitedWindowDelta/rows_1024/limit_100/shuffled/2col/insert_outside-24` | live top-100 insert outside window, shuffled two-column key | 959.4us +/- 1% | 1.179Mi +/- 0% | 8.668k | advisory |
| Live ordered window delta | `EvalOrderedLimitedWindowDelta/rows_4096/limit_100/descending/2col/insert_head-24` | live top-100 insert into window head, two-column key | 3.737ms +/- 2% | 4.828Mi +/- 0% | 33.28k | advisory |
| Live ordered window delta | `EvalOrderedLimitedWindowDelta/rows_4096/limit_1000/shuffled/1col/delete_head-24` | live top-1,000 delete from window head | 3.146ms +/- 4% | 3.554Mi +/- 0% | 28.69k | advisory |
| Ordered suite geomean | all focused ordered subscription window benchmarks | 47 sub-benchmark geomean | 310.2us | 431.0Ki | 3.724k | advisory |

## Current Read

- Existing equality subscription evaluation and candidate collection remain the
  healthiest hot paths.
- Large bag diffing, large snapshot-plus-tail recovery, segmented log replay,
  and multi-way joins at 512 rows per table are the clearest allocation and
  latency targets in the current coverage.
- Subscription fanout coverage now includes same-query, varied single-table,
  skewed hot-key, and varied two-table fixtures. Workload-derived application
  distributions remain outside the local benchmark envelope.
- Ordered subscription window coverage now has a focused `-count=10` baseline
  across bounded initial rows, full ordering, initial row collection,
  registration, comparator shape, and live ordered/limited delta fixtures.
- Compression corpus coverage now has a focused `-count=10` baseline across
  production gzip and benchmark-local brotli candidates. The current evidence
  keeps brotli as benchmark-only tooling rather than runtime support.
- Executor reducer commit coverage now includes one-at-a-time round trips and
  a queued 64-command burst fixture. These are internal executor fixtures, not
  end-to-end application throughput measurements.
- Declared read coverage now includes local declared-query execution and local
  declared live-view initial rows for projection/order/limit and count shapes.
- Live multi-way joins now have opt-in production guardrails through
  `Config.SubscriptionMaxMultiJoinRelations` and
  `Config.SubscriptionMaxMultiJoinRowsPerRelation`. The focused Stage A
  snapshot above refreshes the relation-size fixtures and publishes the
  existing chain3, self_alias3, and chain4 relation-shape fixtures. The focused
  Stage B snapshot adds bounded cross-shape, selectivity/skew,
  changed-row-count, and `COUNT(*)` aggregate relation-shape rows. The focused
  Stage F snapshot extends relation-count evidence to a bounded 5-relation
  chain for table-shaped projection and `COUNT(*)`. This evidence does not
  justify changing the unlimited defaults.
- Offline backup/restore is covered for small and larger complete local
  DataDir fixtures and is expected to be I/O dominated; these rows do not
  replace production-scale backup/restore timing.
- WebSocket coverage now includes a single SubscribeSingle round trip and
  16-, 64-, and 128-client light-update fanout fixtures. Slow-reader
  backpressure now has a network-level advisory row for unrelated healthy
  client delivery while a second client is held in a blocked unread write.
  Deterministic sender-level full-buffer rejection is covered separately.
- The current rows are not release-blocking thresholds. Treat regressions here
  as investigation triggers until the release process defines hard limits.

## Memory Profile Notes

Subscription large-fixture memory profiles were spot-checked on 2026-05-09 at
Shunter commit `59f838f960a762e95b623408b1749dfe7678d6c1`, using the then
current host and Go toolchain from that snapshot. Profiles were written under
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
  fixture, not production-scale backup/restore evidence.
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
  slow-reader backpressure fixtures, including application-scale fanout;
  deterministic sender-level full-buffer rejection is covered separately
- workload-derived application fanout distributions beyond the deterministic
  in-process same-query, varied single-table, skewed hot-key, and varied
  two-table predicate fixtures
- application workload timing, including production-scale backup/restore timing
- multi-way join evidence beyond the focused Stage B, Stage D, and Stage F
  snapshots, including larger Cartesian fixtures, larger skew/fanout
  distributions, relation counts beyond the bounded 5-relation chain fixture,
  aggregate-function rows beyond the bounded 128-row `chain3` fixture, and
  app-derived workload distributions
- memory profiles outside the current subscription, single-WebSocket,
  16/64/128-client WebSocket fanout, sender-level backpressure, executor
  reducer commit, and small/larger local backup/restore fixtures, including
  application-scale fanout, slow-reader network paths, and production-sized
  backup/restore workloads
