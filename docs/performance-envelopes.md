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

## Focused Multi-Way Live Join Stage H Cartesian

This focused snapshot extends bounded Cartesian multi-way relation-shape
evidence from `cross3_rows_24` to `cross3_rows_32`. The new fixture keeps the
same 3-relation Cartesian shape and one changed endpoint row; the changed row
emits a 32x32 Cartesian fragment. It records both table-shaped projection and
`COUNT(*)` aggregate rows, remains advisory, and does not change default
multi-way join guardrails.

- Date: 2026-05-29
- Shunter commit: `268ec7de52ea35cabfd56099760486fbeb2826ad`
- Measurement worktree: commit above plus Stage H benchmark and documentation
  changes
- Host: `Linux 6.17.0-29-generic x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 12 sub-benchmarks, `-count=10`, 120 benchmark rows, total
  package benchmark time 183.066s
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-h/multiway-bench-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-h/multiway-benchstat.log`

Command:

```bash
go test -run '^$' -bench 'BenchmarkMultiWayLiveJoin(RelationShapes|AggregateRelationShapes)' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-h/multiway-bench-raw.log 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-h/multiway-bench-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-h/multiway-benchstat.log 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_24-24` | 3-relation Cartesian multi-join, 24 rows per relation, one endpoint insert emits a 24x24 fragment | 50.05us +/- 3% | 86.71Ki +/- 0% | 85 | advisory |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_32-24` | 3-relation Cartesian multi-join, 32 rows per relation, one endpoint insert emits a 32x32 fragment | 86.73us +/- 2% | 155.9Ki +/- 0% | 88 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_24/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 24 rows per relation, one endpoint insert | 307.6us +/- 1% | 9.964Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_32/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 32 rows per relation, one endpoint insert | 707.3us +/- 1% | 12.63Ki +/- 0% | 73 | advisory |
| Multi-way Stage H geomean | all focused Stage H multi-way live join benchmarks | 12 sub-benchmark geomean | 1.116ms | 42.66Ki | 82.69 | advisory |

Current read:

- The bounded `cross3_rows_32` rows remain local-review-sized under
  `-count=10` and extend Cartesian evidence without turning the benchmark into
  a soak/load lane.
- The table-shaped row's allocation growth tracks the larger materialized
  Cartesian fragment. The `COUNT(*)` row avoids that output materialization,
  but still pays the latency cost of counting the 32x32 combinations.
- Larger Cartesian fixtures beyond the bounded 32-row shape, larger
  skew/fanout distributions, relation counts beyond the bounded 5-relation
  chain, broader aggregate-function shapes, and app-derived workload
  distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage I Aggregate Function Shape

This focused snapshot extends aggregate-function evidence from the existing
128-row `chain3` fixture to the existing bounded 128-row `chain4` fixture.
The new rows cover the aggregate functions already accepted by the subscription
layer and remain advisory; runtime semantics and default multi-way join
guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `45acb8f76e3a6cec42dc48f1022d10ee1f6a93cc`
- Measurement worktree: commit above plus Stage I benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 14 sub-benchmarks, `-count=10`, 140 benchmark rows, total
  package benchmark time 216.219s
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-i/multiway-aggregate-functions-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-i/multiway-aggregate-functions-benchstat.log`

Command:

```bash
go test -run '^$' -bench 'BenchmarkMultiWayLiveJoin(AggregateFunctions|AggregateRelationShapes)' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-i/multiway-aggregate-functions-raw.log 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-i/multiway-aggregate-functions-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-i/multiway-aggregate-functions-benchstat.log 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain3/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.352ms +/- 0% | 37.66Ki +/- 0% | 73 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain3/count_column-24` | `COUNT(t3.id)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.356ms +/- 0% | 37.66Ki +/- 0% | 73 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain3/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.389ms +/- 0% | 117.8Ki +/- 0% | 347 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain3/sum-24` | `SUM(t3.id)` over 3-relation chain, 128 rows per relation, one endpoint insert | 2.375ms +/- 1% | 37.78Ki +/- 0% | 75 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain4/count_star-24` | `COUNT(*)` over 4-relation chain, 128 rows per relation, one endpoint insert | 4.709ms +/- 1% | 50.66Ki +/- 0% | 93 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain4/count_column-24` | `COUNT(t3.id)` over 4-relation chain, 128 rows per relation, one endpoint insert | 4.693ms +/- 1% | 50.66Ki +/- 0% | 93 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain4/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 4-relation chain, 128 rows per relation, one endpoint insert | 4.765ms +/- 1% | 130.8Ki +/- 0% | 367 | advisory |
| Multi-way aggregate function | `MultiWayLiveJoinAggregateFunctions/chain4/sum-24` | `SUM(t3.id)` over 4-relation chain, 128 rows per relation, one endpoint insert | 4.699ms +/- 1% | 50.78Ki +/- 0% | 95 | advisory |
| Multi-way Stage I aggregate geomean | all focused Stage I aggregate relation/function benchmarks | 14 sub-benchmark geomean | 3.406ms | 42.55Ki | 101.8 | advisory |

Current read:

- The bounded `chain4` aggregate-function rows remain local-review-sized under
  `-count=10` while extending function-shape evidence beyond `chain3`.
- On `chain4`, `COUNT(column)` and `SUM(column)` continue to track `COUNT(*)`
  latency and allocation closely in this deterministic fixture.
- `COUNT(DISTINCT column)` adds allocation and allocation count on both
  `chain3` and `chain4`, but does not create a new latency standout here.
- The repeated-table `self_alias3/count` relation-shape row remains the
  standout aggregate latency case in this focused command.
- Larger aggregate-function fixtures beyond the bounded 128-row `chain4`
  chain, larger aggregate-function Cartesian fixtures beyond bounded
  `cross3_rows_32`, aggregate-function skew/self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage J Aggregate Cartesian Function Shape

This focused snapshot extends aggregate-function evidence to the existing
bounded `cross3_rows_32` Cartesian fixture. The new rows cover the aggregate
functions already accepted by the subscription layer over the same 3-relation
Cartesian shape used by Stage H; one changed endpoint row emits a 32x32
Cartesian fragment. Runtime semantics and default multi-way join guardrails are
unchanged.

- Date: 2026-05-29
- Shunter commit: `c4ae9f815cdeca3ead5758ab71f1af259fa8c660`
- Measurement worktree: commit above plus Stage J benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 18 sub-benchmarks, `-count=10`, 180 benchmark rows, total
  package benchmark time 272.615s
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-j/multiway-aggregate-functions-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-j/multiway-aggregate-functions-benchstat.log`

Command:

```bash
go test -run '^$' -bench 'BenchmarkMultiWayLiveJoin(AggregateFunctions|AggregateRelationShapes)' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-j/multiway-aggregate-functions-raw.log 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-j/multiway-aggregate-functions-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-j/multiway-aggregate-functions-benchstat.log 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_32/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 32 rows per relation, one endpoint insert | 709.4us +/- 0% | 12.63Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_32/count_star-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 32 rows per relation, one endpoint insert | 710.2us +/- 1% | 12.63Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_32/count_column-24` | `COUNT(t3.id)` over 3-relation Cartesian multi-join, 32 rows per relation, one endpoint insert | 1.866ms +/- 1% | 12.63Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_32/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation Cartesian multi-join, 32 rows per relation, one endpoint insert | 2.989ms +/- 1% | 31.42Ki +/- 0% | 147 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_32/sum-24` | `SUM(t3.id)` over 3-relation Cartesian multi-join, 32 rows per relation, one endpoint insert | 2.254ms +/- 0% | 12.77Ki +/- 0% | 75 | advisory |
| Multi-way Stage J aggregate geomean | all focused Stage J aggregate relation/function benchmarks | 18 sub-benchmark geomean | 2.947ms | 34.19Ki | 98.44 | advisory |

Current read:

- The bounded `cross3_rows_32` aggregate-function rows remain local-review-sized
  under `-count=10` and close one Cartesian aggregate-function evidence gap.
- `COUNT(*)` tracks the Stage H aggregate relation-shape row. `COUNT(column)`
  and `SUM(column)` measure higher latency than `COUNT(*)` in this Cartesian
  fixture while retaining similar allocation.
- `COUNT(DISTINCT column)` is the slowest new Cartesian aggregate-function row
  and adds allocation, but remains far below the existing `self_alias3/count`
  aggregate latency standout in the same focused command.
- Larger Cartesian fixtures beyond the bounded 32-row shape, larger
  skew/fanout distributions, relation counts beyond the bounded 5-relation
  chain, aggregate-function skew/self-alias distributions, and app-derived
  workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage K Aggregate Skew/Fanout Function Shape

This focused snapshot extends aggregate-function evidence to the existing
bounded `hot_key_16x16` skew/fanout fixture. The new rows cover the aggregate
functions already accepted by the subscription layer over the same 3-relation
chain used by Stage G; one changed endpoint row matches a 16x16 fanout
fragment. Runtime semantics and default multi-way join guardrails are
unchanged.

- Date: 2026-05-29
- Shunter commit: `609d3e6f7ed697d22524ad9104612680b7f5db05`
- Measurement worktree: commit above plus Stage K benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 19 sub-benchmarks, `-count=10`, 190 benchmark rows, total
  package benchmark time 288.311s
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-k/multiway-aggregate-skew-functions-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-k/multiway-aggregate-skew-functions-benchstat.log`

Command:

```bash
go test -run '^$' -bench 'BenchmarkMultiWayLiveJoin(AggregateFunctions|Selectivity)' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-k/multiway-aggregate-skew-functions-raw.log 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-k/multiway-aggregate-skew-functions-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-k/multiway-aggregate-skew-functions-benchstat.log 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_16x16-24` | 128 rows per relation, one changed hot-key row matching 16 left rows by 16 middle rows | 390.4us +/- 0% | 74.61Ki +/- 0% | 83 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_16x16/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 16x16 fragment | 5.244ms +/- 0% | 37.69Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_16x16/count_column-24` | `COUNT(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 16x16 fragment | 5.267ms +/- 0% | 37.69Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_16x16/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 16x16 fragment | 5.286ms +/- 0% | 42.61Ki +/- 0% | 99 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_16x16/sum-24` | `SUM(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 16x16 fragment | 5.256ms +/- 0% | 37.82Ki +/- 0% | 75 | advisory |
| Multi-way Stage K aggregate geomean | all focused Stage K aggregate/selectivity benchmarks | 19 sub-benchmark geomean | 2.269ms | 39.35Ki | 95.70 | advisory |

Current read:

- The bounded `hot_key_16x16` aggregate-function rows remain
  local-review-sized under `-count=10` and close one aggregate skew/fanout
  evidence gap without turning the benchmark into a soak/load lane.
- The changed endpoint row matches key `1`, the hot key shared by 16 rows on
  each upstream relation, so each aggregate row evaluates a 16x16 fanout
  fragment over the existing 128-row fixture.
- `COUNT(column)` and `SUM(column)` track `COUNT(*)` latency and allocation
  closely in this skew fixture. `COUNT(DISTINCT column)` adds allocation, but
  not the broader latency spread seen in the Cartesian function rows.
- Larger skew/fanout distributions beyond 16x16, larger Cartesian fixtures
  beyond the bounded 32-row shape, relation counts beyond the bounded
  5-relation chain, aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage L Aggregate Self-Alias Function Shape

This focused snapshot extends aggregate-function evidence to the existing
bounded `self_alias3` repeated-table fixture. The new rows cover the aggregate
functions already accepted by the subscription layer over the same 3-alias,
2-physical-table shape used by the Stage B `COUNT(*)` aggregate relation-shape
row; one changed endpoint row is inserted into table 2 alias 2. Runtime
semantics and default multi-way join guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `75457879a256c4bbadd95fd750db3d221141a733`
- Measurement worktree: commit above plus Stage L benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 26 sub-benchmarks, `-count=10`, 260 benchmark rows, total
  package benchmark time 405.559s
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-l/multiway-aggregate-self-alias-functions-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-l/multiway-aggregate-self-alias-functions-benchstat.log`

Command:

```bash
go test -run '^$' -bench 'BenchmarkMultiWayLiveJoin(AggregateFunctions|AggregateRelationShapes)' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-l/multiway-aggregate-self-alias-functions-raw.log 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-l/multiway-aggregate-self-alias-functions-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-l/multiway-aggregate-self-alias-functions-benchstat.log 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way aggregate self-alias shape | `MultiWayLiveJoinAggregateRelationShapes/self_alias3/count-24` | `COUNT(*)` over 3 aliases across 2 physical tables, 128-row fixture, one repeated-table insert | 101.2ms +/- 0% | 39.45Ki +/- 0% | 77.00 +/- 1% | advisory |
| Multi-way aggregate self-alias function | `MultiWayLiveJoinAggregateFunctions/self_alias3/count_star-24` | `COUNT(*)` over 3 aliases across 2 physical tables, 128-row fixture, one repeated-table insert | 101.7ms +/- 1% | 39.45Ki +/- 0% | 77.00 +/- 1% | advisory |
| Multi-way aggregate self-alias function | `MultiWayLiveJoinAggregateFunctions/self_alias3/count_column-24` | `COUNT(t2.id)` over table 2 alias 2 in the 3-alias repeated-table fixture | 102.4ms +/- 2% | 39.45Ki +/- 0% | 77.00 +/- 1% | advisory |
| Multi-way aggregate self-alias function | `MultiWayLiveJoinAggregateFunctions/self_alias3/count_distinct-24` | `COUNT(DISTINCT t1.id)` over table 1 alias 0 in the 3-alias repeated-table fixture | 102.4ms +/- 1% | 119.3Ki +/- 0% | 351.0 +/- 0% | advisory |
| Multi-way aggregate self-alias function | `MultiWayLiveJoinAggregateFunctions/self_alias3/sum-24` | `SUM(t2.id)` over table 2 alias 2 in the 3-alias repeated-table fixture | 101.8ms +/- 0% | 39.58Ki +/- 0% | 79.00 +/- 0% | advisory |
| Multi-way Stage L aggregate geomean | all focused Stage L aggregate relation/function benchmarks | 26 sub-benchmark geomean | 5.542ms | 37.21Ki | 97.29 | advisory |

Current read:

- The bounded `self_alias3` aggregate-function rows are local-review-sized, but
  they dominate the focused command wall time: the `-count=10` run took
  405.559s.
- `COUNT(column)` and `SUM(column)` over table 2 alias 2 track `COUNT(*)`
  latency and allocation closely in this repeated-table fixture.
- `COUNT(DISTINCT column)` adds allocation and allocation count, as in the
  chain fixtures, but does not create additional latency spread in this shape.
- Larger self-alias distributions, larger Cartesian fixtures beyond the
  bounded 32-row shape, larger skew/fanout distributions beyond 16x16,
  relation counts beyond the bounded 5-relation chain, and app-derived
  workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage M Larger Cartesian Function Shape

This focused snapshot extends the bounded 3-relation Cartesian fixture from
`cross3_rows_32` to `cross3_rows_40`. The new rows keep the same Cartesian
shape and one changed endpoint row; the changed row emits a 40x40 Cartesian
fragment. The snapshot records table-shaped projection, `COUNT(*)`, and the
aggregate functions already accepted by the subscription layer. Runtime
semantics and default multi-way join guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `bb733b7fc492d5c7572da454554ff8812342d769`
- Measurement worktree: commit above plus Stage M benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 12 sub-benchmarks, `-count=10`, 120 benchmark rows, total
  package benchmark time 179.126s across three focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-m/multiway-cartesian-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-m/multiway-cartesian-benchstat.log`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-05-29-subscription-stage-m
rtk bash -lc 'go test -run "^$" -bench "^BenchmarkMultiWayLiveJoinRelationShapes$/^cross3_rows_(32|40)$" -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-m/multiway-cartesian-raw.log 2>&1'
rtk bash -lc 'go test -run "^$" -bench "^BenchmarkMultiWayLiveJoinAggregateRelationShapes$/^cross3_rows_(32|40)$/^count$" -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-m/multiway-cartesian-raw.log 2>&1'
rtk bash -lc 'go test -run "^$" -bench "^BenchmarkMultiWayLiveJoinAggregateFunctions$/^cross3_rows_(32|40)$/^(count_star|count_column|count_distinct|sum)$" -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-m/multiway-cartesian-raw.log 2>&1'
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-m/multiway-cartesian-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-m/multiway-cartesian-benchstat.log 2>&1'
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_32-24` | 3-relation Cartesian multi-join, 32 rows per relation, one endpoint insert emits a 32x32 fragment | 89.30us +/- 3% | 155.9Ki +/- 0% | 88 | advisory |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_40-24` | 3-relation Cartesian multi-join, 40 rows per relation, one endpoint insert emits a 40x40 fragment | 137.5us +/- 2% | 251.5Ki +/- 0% | 90 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_32/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 32 rows per relation, one endpoint insert | 719.2us +/- 1% | 12.63Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_40/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 40 rows per relation, one endpoint insert | 1.377ms +/- 1% | 14.14Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_40/count_star-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 40 rows per relation, one endpoint insert | 1.370ms +/- 1% | 14.14Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_40/count_column-24` | `COUNT(t3.id)` over 3-relation Cartesian multi-join, 40 rows per relation, one endpoint insert | 3.634ms +/- 0% | 14.15Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_40/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation Cartesian multi-join, 40 rows per relation, one endpoint insert | 5.905ms +/- 0% | 35.70Ki +/- 0% | 163 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_40/sum-24` | `SUM(t3.id)` over 3-relation Cartesian multi-join, 40 rows per relation, one endpoint insert | 4.408ms +/- 0% | 14.28Ki +/- 0% | 75 | advisory |
| Multi-way Stage M Cartesian geomean | all focused Stage M Cartesian benchmarks | 12 sub-benchmark geomean | 1.249ms | 24.45Ki | 85.91 | advisory |

Current read:

- The bounded `cross3_rows_40` rows remain local-review-sized under
  `-count=10` while extending Cartesian size evidence beyond the 32-row shape.
- The table-shaped row's allocation growth tracks the larger materialized
  40x40 Cartesian fragment. The `COUNT(*)` row avoids that output
  materialization, but latency still scales with counting the combinations.
- `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while measuring higher latency in this Cartesian fixture.
- `COUNT(DISTINCT column)` is the slowest Stage M Cartesian aggregate-function
  row and adds allocation, but remains far below the existing `self_alias3`
  aggregate-function latency standout.
- Larger Cartesian fixtures beyond the bounded 40-row shape, larger
  skew/fanout distributions beyond 16x16, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage N Larger Skew/Fanout Function Shape

This focused snapshot extends the bounded 3-relation skew/fanout fixture from
`hot_key_16x16` to `hot_key_24x24`. The new rows keep the same 128 rows per
relation and one changed endpoint row; the changed row matches key `1`, the hot
key shared by 24 rows on each upstream relation, so it emits a 24x24 fanout
fragment. The snapshot records table-shaped projection and the aggregate
functions already accepted by the subscription layer. Runtime semantics and
default multi-way join guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `2d1c0ee160cc4ec9c3786fceb9050122327d9898`
- Measurement worktree: commit above plus Stage N benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 10 sub-benchmarks, `-count=10`, 100 benchmark rows, total
  package benchmark time 166.757s across two focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-n/selectivity-hot-key-16x16-24x24.txt`
  and
  `working-docs/release-evidence/2026-05-29-subscription-stage-n/aggregate-functions-hot-key-16x16-24x24.txt`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-n/selectivity-hot-key-16x16-24x24-benchstat.txt`
  and
  `working-docs/release-evidence/2026-05-29-subscription-stage-n/aggregate-functions-hot-key-16x16-24x24-benchstat.txt`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-05-29-subscription-stage-n
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinSelectivity$/^rows_128$/^hot_key_(16x16|24x24)$' -benchmem -count=10 ./subscription | tee working-docs/release-evidence/2026-05-29-subscription-stage-n/selectivity-hot-key-16x16-24x24.txt
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^hot_key_(16x16|24x24)$' -benchmem -count=10 ./subscription | tee working-docs/release-evidence/2026-05-29-subscription-stage-n/aggregate-functions-hot-key-16x16-24x24.txt
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-n/selectivity-hot-key-16x16-24x24.txt > working-docs/release-evidence/2026-05-29-subscription-stage-n/selectivity-hot-key-16x16-24x24-benchstat.txt 2>&1'
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-n/aggregate-functions-hot-key-16x16-24x24.txt > working-docs/release-evidence/2026-05-29-subscription-stage-n/aggregate-functions-hot-key-16x16-24x24-benchstat.txt 2>&1'
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_16x16-24` | 128 rows per relation, one changed hot-key row matching 16 left rows by 16 middle rows | 388.1us +/- 0% | 74.61Ki +/- 0% | 83 | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_24x24-24` | 128 rows per relation, one changed hot-key row matching 24 left rows by 24 middle rows | 427.4us +/- 1% | 114.5Ki +/- 0% | 85 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_16x16/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 16x16 fragment | 4.073ms +/- 1% | 37.67Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_24x24/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 24x24 fragment | 7.068ms +/- 1% | 37.70Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_24x24/count_column-24` | `COUNT(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 24x24 fragment | 7.024ms +/- 1% | 37.70Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_24x24/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 24x24 fragment | 6.989ms +/- 1% | 43.98Ki +/- 0% | 107 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_24x24/sum-24` | `SUM(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 24x24 fragment | 7.022ms +/- 3% | 37.82Ki +/- 0% | 75 | advisory |
| Multi-way Stage N selectivity geomean | focused Stage N selectivity hot-key benchmarks | 2 sub-benchmark geomean | 407.3us | 92.42Ki | 83.99 | advisory |
| Multi-way Stage N aggregate geomean | focused Stage N aggregate hot-key benchmarks | 8 sub-benchmark geomean | 5.399ms | 39.05Ki | 80.09 | advisory |

Current read:

- The bounded `hot_key_24x24` rows remain local-review-sized under
  `-count=10` while extending skew/fanout evidence beyond the 16x16 shape.
- The table-shaped row's allocation growth tracks the larger materialized
  24x24 fanout fragment; latency grows modestly relative to `hot_key_16x16`.
- The `hot_key_24x24` aggregate-function rows are stable around 7.0ms/op in
  this focused run. `COUNT(column)` and `SUM(column)` remain allocation-stable
  relative to `COUNT(*)`, while `COUNT(DISTINCT column)` adds allocation and
  allocation count without becoming a latency standout.
- The `hot_key_16x16/count_column` comparison row was noisy in this local run;
  it was retained as context rather than treated as a release gate.
- Larger skew/fanout distributions beyond 24x24, larger Cartesian fixtures
  beyond the bounded 40-row shape, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage O Larger Cartesian Function Shape

This focused snapshot extends the bounded 3-relation Cartesian fixture from
`cross3_rows_40` to `cross3_rows_48`. The new rows keep the same Cartesian
shape and one changed endpoint row; the changed row emits a 48x48 Cartesian
fragment. The snapshot records table-shaped projection, `COUNT(*)`, and the
aggregate functions already accepted by the subscription layer. Runtime
semantics and default multi-way join guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `2cd88672e062a2287d52afa45f1e5a4c712f84e3`
- Measurement worktree: commit above plus Stage O benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 12 sub-benchmarks, `-count=10`, 120 benchmark rows, total
  package benchmark time 178.226s across three focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-o/multiway-cartesian-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-o/multiway-cartesian-benchstat.log`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-05-29-subscription-stage-o
rtk bash -lc 'go test -run "^$" -bench "^BenchmarkMultiWayLiveJoinRelationShapes$/^cross3_rows_(40|48)$" -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-o/multiway-cartesian-raw.log 2>&1'
rtk bash -lc 'go test -run "^$" -bench "^BenchmarkMultiWayLiveJoinAggregateRelationShapes$/^cross3_rows_(40|48)$/^count$" -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-o/multiway-cartesian-raw.log 2>&1'
rtk bash -lc 'go test -run "^$" -bench "^BenchmarkMultiWayLiveJoinAggregateFunctions$/^cross3_rows_(40|48)$" -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-o/multiway-cartesian-raw.log 2>&1'
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-o/multiway-cartesian-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-o/multiway-cartesian-benchstat.log 2>&1'
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_40-24` | 3-relation Cartesian multi-join, 40 rows per relation, one endpoint insert emits a 40x40 fragment | 136.5us +/- 2% | 251.5Ki +/- 0% | 90 | advisory |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_48-24` | 3-relation Cartesian multi-join, 48 rows per relation, one endpoint insert emits a 48x48 fragment | 195.9us +/- 2% | 383.0Ki +/- 0% | 93 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_40/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 40 rows per relation, one endpoint insert | 1.381ms +/- 1% | 14.14Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_48/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 48 rows per relation, one endpoint insert | 2.337ms +/- 1% | 16.79Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_48/count_star-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 48 rows per relation, one endpoint insert | 2.354ms +/- 1% | 16.79Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_48/count_column-24` | `COUNT(t3.id)` over 3-relation Cartesian multi-join, 48 rows per relation, one endpoint insert | 6.253ms +/- 1% | 16.81Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_48/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation Cartesian multi-join, 48 rows per relation, one endpoint insert | 10.25ms +/- 0% | 41.13Ki +/- 0% | 179 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_48/sum-24` | `SUM(t3.id)` over 3-relation Cartesian multi-join, 48 rows per relation, one endpoint insert | 7.567ms +/- 0% | 16.90Ki +/- 0% | 75 | advisory |
| Multi-way Stage O Cartesian geomean | all focused Stage O Cartesian benchmarks | 12 sub-benchmark geomean | 2.199ms | 29.63Ki | 87.73 | advisory |

Current read:

- The bounded `cross3_rows_48` rows remain local-review-sized under
  `-count=10` while extending Cartesian size evidence beyond the 40-row shape.
- The table-shaped row's allocation growth tracks the larger materialized
  48x48 Cartesian fragment. The `COUNT(*)` row avoids that output
  materialization, but latency still scales with counting the combinations.
- `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while measuring higher latency in this Cartesian fixture.
- `COUNT(DISTINCT column)` is the slowest Stage O Cartesian aggregate-function
  row and adds allocation, but remains below the existing `self_alias3`
  aggregate-function latency standout.
- Larger Cartesian fixtures beyond the bounded 48-row shape, larger
  skew/fanout distributions beyond 24x24, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage P Larger Skew/Fanout Function Shape

This focused snapshot extends the bounded 3-relation skew/fanout fixture from
`hot_key_24x24` to `hot_key_32x32`. The new rows keep the same 128 rows per
relation and one changed endpoint row; the changed row matches key `1`, the hot
key shared by 32 rows on each upstream relation, so it emits a 32x32 fanout
fragment. The snapshot records table-shaped projection and the aggregate
functions already accepted by the subscription layer. Runtime semantics and
default multi-way join guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `0746eb8a14bbd6cb249bda0906412878f1b2f121`
- Measurement worktree: commit above plus Stage P benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 10 sub-benchmarks, `-count=10`, 100 benchmark rows, total
  package benchmark time 190.411s across two focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-p/selectivity-hot-key-24x24-32x32.txt`
  and
  `working-docs/release-evidence/2026-05-29-subscription-stage-p/aggregate-functions-hot-key-24x24-32x32.txt`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-p/selectivity-hot-key-24x24-32x32-benchstat.txt`
  and
  `working-docs/release-evidence/2026-05-29-subscription-stage-p/aggregate-functions-hot-key-24x24-32x32-benchstat.txt`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-05-29-subscription-stage-p
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinSelectivity$/^rows_128$/^hot_key_(24x24|32x32)$' -benchmem -count=10 ./subscription | tee working-docs/release-evidence/2026-05-29-subscription-stage-p/selectivity-hot-key-24x24-32x32.txt
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^hot_key_(24x24|32x32)$' -benchmem -count=10 ./subscription | tee working-docs/release-evidence/2026-05-29-subscription-stage-p/aggregate-functions-hot-key-24x24-32x32.txt
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-p/selectivity-hot-key-24x24-32x32.txt > working-docs/release-evidence/2026-05-29-subscription-stage-p/selectivity-hot-key-24x24-32x32-benchstat.txt 2>&1'
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-p/aggregate-functions-hot-key-24x24-32x32.txt > working-docs/release-evidence/2026-05-29-subscription-stage-p/aggregate-functions-hot-key-24x24-32x32-benchstat.txt 2>&1'
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_24x24-24` | 128 rows per relation, one changed hot-key row matching 24 left rows by 24 middle rows | 459.5us +/- 26% | 114.5Ki +/- 0% | 85 | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_32x32-24` | 128 rows per relation, one changed hot-key row matching 32 left rows by 32 middle rows | 501.7us +/- 2% | 181.1Ki +/- 0% | 88 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_24x24/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 24x24 fragment | 7.148ms +/- 3% | 37.70Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_32x32/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 32x32 fragment | 11.17ms +/- 1% | 37.71Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_32x32/count_column-24` | `COUNT(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 32x32 fragment | 11.17ms +/- 3% | 37.71Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_32x32/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 32x32 fragment | 11.20ms +/- 0% | 47.66Ki +/- 0% | 117 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_32x32/sum-24` | `SUM(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 32x32 fragment | 11.24ms +/- 1% | 37.84Ki +/- 0% | 75 | advisory |
| Multi-way Stage P selectivity geomean | focused Stage P selectivity hot-key benchmarks | 2 sub-benchmark geomean | 480.2us | 144.0Ki | 86.49 | advisory |
| Multi-way Stage P aggregate geomean | focused Stage P aggregate hot-key benchmarks | 8 sub-benchmark geomean | 8.952ms | 39.61Ki | 81.78 | advisory |

Current read:

- The bounded `hot_key_32x32` rows remain local-review-sized under
  `-count=10` while extending skew/fanout evidence beyond the 24x24 shape.
- The table-shaped row's allocation growth tracks the larger materialized
  32x32 fanout fragment; the `hot_key_24x24` comparison row was noisy in this
  local run and is retained as context rather than treated as a release gate.
- The `hot_key_32x32` aggregate-function rows are stable around
  11.17-11.24ms/op in this focused run. `COUNT(column)` and `SUM(column)`
  remain allocation-stable relative to `COUNT(*)`, while
  `COUNT(DISTINCT column)` adds allocation and allocation count without
  becoming a latency standout.
- Larger skew/fanout distributions beyond 32x32, larger Cartesian fixtures
  beyond the bounded 48-row shape, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage Q Larger Cartesian Function Shape

This focused snapshot extends the bounded 3-relation Cartesian fixture from
`cross3_rows_48` to `cross3_rows_56`. The new rows keep the same Cartesian
shape and one changed endpoint row; the changed row emits a 56x56 Cartesian
fragment. The snapshot records table-shaped projection, `COUNT(*)`, and the
aggregate functions already accepted by the subscription layer. Runtime
semantics and default multi-way join guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `6f06a56317c60ff508f6bca576f94d1de271c3a4`
- Measurement worktree: commit above plus Stage Q benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 12 sub-benchmarks, `-count=10`, 120 benchmark rows, total
  package benchmark time 179.936s across three focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-q/multiway-cartesian-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-q/multiway-cartesian-benchstat.log`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-05-29-subscription-stage-q
rtk bash -lc 'go test -run "^$" -bench "^BenchmarkMultiWayLiveJoinRelationShapes$/^cross3_rows_(48|56)$" -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-q/multiway-cartesian-raw.log 2>&1'
rtk bash -lc 'go test -run "^$" -bench "^BenchmarkMultiWayLiveJoinAggregateRelationShapes$/^cross3_rows_(48|56)$/^count$" -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-q/multiway-cartesian-raw.log 2>&1'
rtk bash -lc 'go test -run "^$" -bench "^BenchmarkMultiWayLiveJoinAggregateFunctions$/^cross3_rows_(48|56)$" -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-q/multiway-cartesian-raw.log 2>&1'
rtk bash -lc 'rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-q/multiway-cartesian-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-q/multiway-cartesian-benchstat.log 2>&1'
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_48-24` | 3-relation Cartesian multi-join, 48 rows per relation, one endpoint insert emits a 48x48 fragment | 200.5us +/- 8% | 383.0Ki +/- 0% | 93 | advisory |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_56-24` | 3-relation Cartesian multi-join, 56 rows per relation, one endpoint insert emits a 56x56 fragment | 285.2us +/- 4% | 569.7Ki +/- 0% | 96 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_48/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 48 rows per relation, one endpoint insert | 2.366ms +/- 0% | 16.79Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_56/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 56 rows per relation, one endpoint insert | 3.727ms +/- 1% | 18.30Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_56/count_star-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 56 rows per relation, one endpoint insert | 3.699ms +/- 1% | 18.30Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_56/count_column-24` | `COUNT(t3.id)` over 3-relation Cartesian multi-join, 56 rows per relation, one endpoint insert | 9.867ms +/- 1% | 18.30Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_56/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation Cartesian multi-join, 56 rows per relation, one endpoint insert | 16.60ms +/- 0% | 45.45Ki +/- 0% | 195 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_56/sum-24` | `SUM(t3.id)` over 3-relation Cartesian multi-join, 56 rows per relation, one endpoint insert | 11.94ms +/- 1% | 18.44Ki +/- 0% | 75 | advisory |
| Multi-way Stage Q Cartesian geomean | all focused Stage Q Cartesian benchmarks | 12 sub-benchmark geomean | 3.569ms | 35.26Ki | 89.53 | advisory |

Current read:

- The bounded `cross3_rows_56` rows remain local-review-sized under
  `-count=10` while extending Cartesian size evidence beyond the 48-row shape.
- The table-shaped row's allocation growth tracks the larger materialized
  56x56 Cartesian fragment. The `COUNT(*)` row avoids that output
  materialization, but latency still scales with counting the combinations.
- `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while measuring higher latency in this Cartesian fixture.
- `COUNT(DISTINCT column)` is the slowest Stage Q Cartesian aggregate-function
  row and adds allocation, but remains local-review-sized in the focused run.
- Larger Cartesian fixtures beyond the bounded 56-row shape, larger
  skew/fanout distributions beyond 32x32, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage R Larger Skew/Fanout Function Shape

This focused snapshot extends the bounded 3-relation skew/fanout fixture from
`hot_key_32x32` to `hot_key_40x40`. The new rows keep the same 128 rows per
relation and one changed endpoint row; the changed row matches key `1`, the hot
key shared by 40 rows on each upstream relation, so it emits a 40x40 fanout
fragment. The snapshot records table-shaped projection and the aggregate
functions already accepted by the subscription layer. Runtime semantics and
default multi-way join guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `b71f99f732a6679ecf6200f9e9bfdee17b9d9e55`
- Measurement worktree: commit above plus Stage R benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 10 sub-benchmarks, `-count=10`, 100 benchmark rows, total
  package benchmark time 200.434s across two focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-r/selectivity-hot-key-32x32-40x40.txt`
  and
  `working-docs/release-evidence/2026-05-29-subscription-stage-r/aggregate-functions-hot-key-32x32-40x40.txt`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-r/selectivity-hot-key-32x32-40x40-benchstat.txt`
  and
  `working-docs/release-evidence/2026-05-29-subscription-stage-r/aggregate-functions-hot-key-32x32-40x40-benchstat.txt`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-05-29-subscription-stage-r
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinSelectivity$/^rows_128$/^hot_key_(32x32|40x40)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-r/selectivity-hot-key-32x32-40x40.txt 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^hot_key_(32x32|40x40)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-r/aggregate-functions-hot-key-32x32-40x40.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-r/selectivity-hot-key-32x32-40x40.txt > working-docs/release-evidence/2026-05-29-subscription-stage-r/selectivity-hot-key-32x32-40x40-benchstat.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-r/aggregate-functions-hot-key-32x32-40x40.txt > working-docs/release-evidence/2026-05-29-subscription-stage-r/aggregate-functions-hot-key-32x32-40x40-benchstat.txt 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_32x32-24` | 128 rows per relation, one changed hot-key row matching 32 left rows by 32 middle rows | 484.9us +/- 2% | 181.1Ki +/- 0% | 88 | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_40x40-24` | 128 rows per relation, one changed hot-key row matching 40 left rows by 40 middle rows | 565.2us +/- 2% | 275.2Ki +/- 0% | 91 +/- 1% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_32x32/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 32x32 fragment | 11.11ms +/- 1% | 37.71Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_40x40/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 40x40 fragment | 16.39ms +/- 1% | 37.80Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_40x40/count_column-24` | `COUNT(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 40x40 fragment | 16.40ms +/- 1% | 37.80Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_40x40/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 40x40 fragment | 16.47ms +/- 0% | 49.12Ki +/- 0% | 125 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_40x40/sum-24` | `SUM(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 40x40 fragment | 16.41ms +/- 1% | 37.93Ki +/- 0% | 75 | advisory |
| Multi-way Stage R selectivity geomean | focused Stage R selectivity hot-key benchmarks | 2 sub-benchmark geomean | 523.5us | 223.2Ki | 89.49 | advisory |
| Multi-way Stage R aggregate geomean | focused Stage R aggregate hot-key benchmarks | 8 sub-benchmark geomean | 13.52ms | 40.20Ki | 83.38 | advisory |

Current read:

- The bounded `hot_key_40x40` rows remain local-review-sized under
  `-count=10` while extending skew/fanout evidence beyond the 32x32 shape.
- The table-shaped row's allocation growth tracks the larger materialized
  40x40 fanout fragment.
- The `hot_key_40x40` aggregate-function rows are stable around
  16.39-16.47ms/op in this focused run. `COUNT(column)` and `SUM(column)`
  remain allocation-stable relative to `COUNT(*)`, while
  `COUNT(DISTINCT column)` adds allocation and allocation count without
  becoming a latency standout.
- Larger skew/fanout distributions beyond 40x40, larger Cartesian fixtures
  beyond the bounded 56-row shape, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage S Larger Cartesian Function Shape

This focused snapshot extends the bounded 3-relation Cartesian fixture from
`cross3_rows_56` to `cross3_rows_64`. The new rows keep the same Cartesian
shape and one changed endpoint row; the changed row emits a 64x64 Cartesian
fragment. The snapshot records table-shaped projection, `COUNT(*)`, and the
aggregate functions already accepted by the subscription layer. Runtime
semantics and default multi-way join guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `108ecb6841c05c2ff33108804599354007ee4dbe`
- Measurement worktree: commit above plus Stage S benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 12 sub-benchmarks, `-count=10`, 120 benchmark rows, total
  package benchmark time 178.134s across three focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-s/multiway-cartesian-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-s/multiway-cartesian-benchstat.log`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-05-29-subscription-stage-s
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinRelationShapes$/^cross3_rows_(56|64)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-s/multiway-cartesian-raw.log 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateRelationShapes$/^cross3_rows_(56|64)$/^count$' -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-s/multiway-cartesian-raw.log 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^cross3_rows_(56|64)$' -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-s/multiway-cartesian-raw.log 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-s/multiway-cartesian-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-s/multiway-cartesian-benchstat.log 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_56-24` | 3-relation Cartesian multi-join, 56 rows per relation, one endpoint insert emits a 56x56 fragment | 326.7us +/- 14% | 569.8Ki +/- 0% | 96 | advisory |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_64-24` | 3-relation Cartesian multi-join, 64 rows per relation, one endpoint insert emits a 64x64 fragment | 359.8us +/- 4% | 817.2Ki +/- 0% | 100 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_56/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 56 rows per relation, one endpoint insert | 3.685ms +/- 0% | 18.30Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_64/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 64 rows per relation, one endpoint insert | 5.461ms +/- 1% | 22.11Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_64/count_star-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 64 rows per relation, one endpoint insert | 5.478ms +/- 1% | 22.11Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_64/count_column-24` | `COUNT(t3.id)` over 3-relation Cartesian multi-join, 64 rows per relation, one endpoint insert | 14.68ms +/- 0% | 22.13Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_64/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation Cartesian multi-join, 64 rows per relation, one endpoint insert | 24.15ms +/- 0% | 61.71Ki +/- 0% | 215 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_64/sum-24` | `SUM(t3.id)` over 3-relation Cartesian multi-join, 64 rows per relation, one endpoint insert | 17.70ms +/- 0% | 22.30Ki +/- 0% | 75 | advisory |
| Multi-way Stage S Cartesian geomean | all focused Stage S Cartesian benchmarks | 12 sub-benchmark geomean | 5.362ms | 42.59Ki | 91.46 | advisory |

Current read:

- The bounded `cross3_rows_64` rows remain local-review-sized under
  `-count=10` while extending Cartesian size evidence beyond the 56-row shape.
- The table-shaped row's allocation growth tracks the larger materialized
  64x64 Cartesian fragment. The `COUNT(*)` row avoids that output
  materialization, but latency still scales with counting the combinations.
- `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while measuring higher latency in this Cartesian fixture.
- `COUNT(DISTINCT column)` is the slowest Stage S Cartesian aggregate-function
  row and adds allocation, but remains local-review-sized in the focused run.
- Larger Cartesian fixtures beyond the bounded 64-row shape, larger
  skew/fanout distributions beyond 40x40, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage T Larger Skew/Fanout Function Shape

This focused snapshot extends the bounded 3-relation skew/fanout fixture from
`hot_key_40x40` to `hot_key_48x48`. The new rows keep the same 128 rows per
relation and one changed endpoint row; the changed row matches key `1`, the hot
key shared by 48 rows on each upstream relation, so it emits a 48x48 fanout
fragment. The snapshot records table-shaped projection and the aggregate
functions already accepted by the subscription layer. Runtime semantics and
default multi-way join guardrails are unchanged.

- Date: 2026-05-29
- Shunter commit: `92dbe5c4562399b976b0d53ffef7de0c3c1494e8`
- Measurement worktree: commit above plus Stage T benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-29-generic #29~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Mon May 11 10:30:58 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 10 sub-benchmarks, `-count=10`, 100 benchmark rows, total
  package benchmark time 201.520s across two focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-t/selectivity-hot-key-40x40-48x48.txt`
  and
  `working-docs/release-evidence/2026-05-29-subscription-stage-t/aggregate-functions-hot-key-40x40-48x48.txt`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-t/selectivity-hot-key-40x40-48x48-benchstat.txt`
  and
  `working-docs/release-evidence/2026-05-29-subscription-stage-t/aggregate-functions-hot-key-40x40-48x48-benchstat.txt`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-05-29-subscription-stage-t
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinSelectivity$/^rows_128$/^hot_key_(40x40|48x48)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-t/selectivity-hot-key-40x40-48x48.txt 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^hot_key_(40x40|48x48)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-t/aggregate-functions-hot-key-40x40-48x48.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-t/selectivity-hot-key-40x40-48x48.txt > working-docs/release-evidence/2026-05-29-subscription-stage-t/selectivity-hot-key-40x40-48x48-benchstat.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-t/aggregate-functions-hot-key-40x40-48x48.txt > working-docs/release-evidence/2026-05-29-subscription-stage-t/aggregate-functions-hot-key-40x40-48x48-benchstat.txt 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_40x40-24` | 128 rows per relation, one changed hot-key row matching 40 left rows by 40 middle rows | 563.5us +/- 1% | 275.2Ki +/- 0% | 91.00 +/- 1% | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_48x48-24` | 128 rows per relation, one changed hot-key row matching 48 left rows by 48 middle rows | 664.4us +/- 1% | 404.0Ki +/- 0% | 93.00 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_40x40/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 40x40 fragment | 16.39ms +/- 1% | 37.80Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_48x48/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 48x48 fragment | 22.77ms +/- 1% | 37.73Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_48x48/count_column-24` | `COUNT(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 48x48 fragment | 22.73ms +/- 0% | 37.73Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_48x48/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 48x48 fragment | 22.80ms +/- 1% | 50.61Ki +/- 0% | 133 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_48x48/sum-24` | `SUM(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 48x48 fragment | 22.74ms +/- 0% | 37.85Ki +/- 0% | 75 | advisory |
| Multi-way Stage T selectivity geomean | focused Stage T selectivity hot-key benchmarks | 2 sub-benchmark geomean | 611.9us | 333.5Ki | 91.99 | advisory |
| Multi-way Stage T aggregate geomean | focused Stage T aggregate hot-key benchmarks | 8 sub-benchmark geomean | 19.34ms | 40.51Ki | 84.73 | advisory |

Current read:

- The bounded `hot_key_48x48` rows remain local-review-sized under
  `-count=10` while extending skew/fanout evidence beyond the 40x40 shape.
- The table-shaped row's allocation growth tracks the larger materialized
  48x48 fanout fragment.
- The `hot_key_48x48` aggregate-function rows are stable around
  22.73-22.80ms/op in this focused run. `COUNT(column)` and `SUM(column)`
  remain allocation-stable relative to `COUNT(*)`, while
  `COUNT(DISTINCT column)` adds allocation and allocation count without
  becoming a latency standout.
- Larger skew/fanout distributions beyond 48x48, larger Cartesian fixtures
  beyond the bounded 64-row shape, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage U Larger Cartesian Function Shape

This focused snapshot extends the bounded 3-relation Cartesian fixture from
`cross3_rows_64` to `cross3_rows_72`. The new rows keep the same Cartesian
shape and one changed endpoint row; the changed row emits a 72x72 Cartesian
fragment. The snapshot records table-shaped projection, `COUNT(*)`, and the
aggregate functions already accepted by the subscription layer. Runtime
semantics and default multi-way join guardrails are unchanged.

- Date: 2026-06-02
- Shunter commit: `f103beb007a6278c3d637d00bf1bcd55b3474477`
- Measurement worktree: commit above plus Stage U benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-35-generic #35~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Tue May 26 19:30:42 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 12 sub-benchmarks, `-count=10`, 120 benchmark rows, total
  package benchmark time 177.431s across three focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-u/multiway-cartesian-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-05-29-subscription-stage-u/multiway-cartesian-benchstat.log`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-05-29-subscription-stage-u
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinRelationShapes$/^cross3_rows_(64|72)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-05-29-subscription-stage-u/multiway-cartesian-raw.log 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateRelationShapes$/^cross3_rows_(64|72)$/^count$' -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-u/multiway-cartesian-raw.log 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^cross3_rows_(64|72)$' -benchmem -count=10 ./subscription >> working-docs/release-evidence/2026-05-29-subscription-stage-u/multiway-cartesian-raw.log 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-05-29-subscription-stage-u/multiway-cartesian-raw.log > working-docs/release-evidence/2026-05-29-subscription-stage-u/multiway-cartesian-benchstat.log 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_64-24` | 3-relation Cartesian multi-join, 64 rows per relation, one endpoint insert emits a 64x64 fragment | 371.5us +/- 6% | 816.9Ki +/- 0% | 100 | advisory |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_72-24` | 3-relation Cartesian multi-join, 72 rows per relation, one endpoint insert emits a 72x72 fragment | 465.2us +/- 6% | 1.131Mi +/- 0% | 105 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_64/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 64 rows per relation, one endpoint insert | 5.481ms +/- 1% | 22.11Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_72/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 72 rows per relation, one endpoint insert | 7.788ms +/- 1% | 22.53Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_72/count_star-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 72 rows per relation, one endpoint insert | 7.758ms +/- 1% | 22.53Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_72/count_column-24` | `COUNT(t3.id)` over 3-relation Cartesian multi-join, 72 rows per relation, one endpoint insert | 20.88ms +/- 1% | 22.55Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_72/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation Cartesian multi-join, 72 rows per relation, one endpoint insert | 34.93ms +/- 0% | 64.74Ki +/- 0% | 231 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_72/sum-24` | `SUM(t3.id)` over 3-relation Cartesian multi-join, 72 rows per relation, one endpoint insert | 25.24ms +/- 1% | 22.75Ki +/- 0% | 75 | advisory |
| Multi-way Stage U Cartesian geomean | all focused Stage U Cartesian benchmarks | 12 sub-benchmark geomean | 7.594ms | 49.89Ki | 93.46 | advisory |

Current read:

- The bounded `cross3_rows_72` rows remain local-review-sized under
  `-count=10` while extending Cartesian size evidence beyond the 64-row shape.
- The table-shaped row's allocation growth tracks the larger materialized
  72x72 Cartesian fragment. The `COUNT(*)` row avoids that output
  materialization, but latency still scales with counting the combinations.
- `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while measuring higher latency in this Cartesian fixture.
- `COUNT(DISTINCT column)` is the slowest Stage U Cartesian aggregate-function
  row and adds allocation, but remains local-review-sized in the focused run.
- Larger Cartesian fixtures beyond the bounded 72-row shape, larger
  skew/fanout distributions beyond 48x48, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage V Larger Skew/Fanout Function Shape

This focused snapshot extends the bounded 3-relation skew/fanout fixture from
`hot_key_48x48` to `hot_key_56x56`. The new rows keep the same 128 rows per
relation and one changed endpoint row; the changed row matches key `1`, the hot
key shared by 56 rows on each upstream relation, so it emits a 56x56 fanout
fragment. The snapshot records table-shaped projection and the aggregate
functions already accepted by the subscription layer. Runtime semantics and
default multi-way join guardrails are unchanged.

- Date: 2026-06-02
- Shunter commit: `049a4b4bb8cc89dbac88b163b3d9f40801da9b3e`
- Measurement worktree: commit above plus Stage V benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-35-generic #35~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Tue May 26 19:30:42 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 10 sub-benchmarks, `-count=10`, 100 benchmark rows, total
  package benchmark time 202.034s across two focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-v/selectivity-hot-key-48x48-56x56.txt`
  and
  `working-docs/release-evidence/2026-06-02-subscription-stage-v/aggregate-functions-hot-key-48x48-56x56.txt`
- Benchstat output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-v/selectivity-hot-key-48x48-56x56-benchstat.txt`
  and
  `working-docs/release-evidence/2026-06-02-subscription-stage-v/aggregate-functions-hot-key-48x48-56x56-benchstat.txt`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-06-02-subscription-stage-v
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinSelectivity$/^rows_128$/^hot_key_(48x48|56x56)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-v/selectivity-hot-key-48x48-56x56.txt 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^hot_key_(48x48|56x56)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-v/aggregate-functions-hot-key-48x48-56x56.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-v/selectivity-hot-key-48x48-56x56.txt > working-docs/release-evidence/2026-06-02-subscription-stage-v/selectivity-hot-key-48x48-56x56-benchstat.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-v/aggregate-functions-hot-key-48x48-56x56.txt > working-docs/release-evidence/2026-06-02-subscription-stage-v/aggregate-functions-hot-key-48x48-56x56-benchstat.txt 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_48x48-24` | 128 rows per relation, one changed hot-key row matching 48 left rows by 48 middle rows | 660.7us +/- 1% | 404.1Ki +/- 0% | 93 | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_56x56-24` | 128 rows per relation, one changed hot-key row matching 56 left rows by 56 middle rows | 778.0us +/- 1% | 589.4Ki +/- 0% | 96.00 +/- 1% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_48x48/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 48x48 fragment | 22.61ms +/- 0% | 37.73Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_56x56/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 56x56 fragment | 30.33ms +/- 1% | 37.77Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_56x56/count_column-24` | `COUNT(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 56x56 fragment | 30.18ms +/- 0% | 37.80Ki +/- 0% | 73 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_56x56/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 56x56 fragment | 30.40ms +/- 1% | 51.87Ki +/- 0% | 141 | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_56x56/sum-24` | `SUM(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 56x56 fragment | 30.39ms +/- 1% | 37.93Ki +/- 0% | 75 | advisory |
| Multi-way Stage V selectivity geomean | focused Stage V selectivity hot-key benchmarks | 2 sub-benchmark geomean | 717.0us | 488.0Ki | 94.49 | advisory |
| Multi-way Stage V aggregate geomean | focused Stage V aggregate hot-key benchmarks | 8 sub-benchmark geomean | 26.22ms | 40.78Ki | 86.01 | advisory |

Current read:

- The bounded `hot_key_56x56` rows remain local-review-sized under
  `-count=10` while extending skew/fanout evidence beyond the 48x48 shape.
- The table-shaped row's allocation growth tracks the larger materialized
  56x56 fanout fragment.
- The `hot_key_56x56` aggregate-function rows are stable around
  30.18-30.40ms/op in this focused run. `COUNT(column)` and `SUM(column)`
  remain allocation-stable relative to `COUNT(*)`, while
  `COUNT(DISTINCT column)` adds allocation and allocation count without
  becoming a latency standout.
- Larger skew/fanout distributions beyond 56x56, larger Cartesian fixtures
  beyond the bounded 72-row shape, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage W Larger Cartesian Function Shape

This focused snapshot extends the bounded 3-relation Cartesian fixture from
`cross3_rows_72` to `cross3_rows_80`. The new rows keep the same Cartesian
shape and one changed endpoint row; the changed row emits an 80x80 Cartesian
fragment. The snapshot records table-shaped projection, `COUNT(*)`, and the
aggregate functions already accepted by the subscription layer. Runtime
semantics and default multi-way join guardrails are unchanged.

- Date: 2026-06-02
- Shunter commit: `d9863a446e624197fb0aad7e5cf84b68aade956b`
- Measurement worktree: commit above plus Stage W benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-35-generic #35~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Tue May 26 19:30:42 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 12 sub-benchmarks, `-count=10`, 120 benchmark rows, total
  package benchmark time 165.169s across three focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-w/multiway-cartesian-raw.log`
- Benchstat output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-w/multiway-cartesian-benchstat.log`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-06-02-subscription-stage-w
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinRelationShapes$/^cross3_rows_(72|80)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-w/relation-shapes-cross3-72-80.txt 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateRelationShapes$/^cross3_rows_(72|80)$/^count$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-w/aggregate-relation-shapes-cross3-72-80.txt 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^cross3_rows_(72|80)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-w/aggregate-functions-cross3-72-80.txt 2>&1
rtk awk '1' working-docs/release-evidence/2026-06-02-subscription-stage-w/relation-shapes-cross3-72-80.txt working-docs/release-evidence/2026-06-02-subscription-stage-w/aggregate-relation-shapes-cross3-72-80.txt working-docs/release-evidence/2026-06-02-subscription-stage-w/aggregate-functions-cross3-72-80.txt > working-docs/release-evidence/2026-06-02-subscription-stage-w/multiway-cartesian-raw.log
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-w/multiway-cartesian-raw.log > working-docs/release-evidence/2026-06-02-subscription-stage-w/multiway-cartesian-benchstat.log 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_72-24` | 3-relation Cartesian multi-join, 72 rows per relation, one endpoint insert emits a 72x72 fragment | 465.3us +/- 3% | 1.130Mi +/- 0% | 105.0 +/- 1% | advisory |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_80-24` | 3-relation Cartesian multi-join, 80 rows per relation, one endpoint insert emits an 80x80 fragment | 558.7us +/- 4% | 1.157Mi +/- 0% | 105.0 +/- 1% | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_72/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 72 rows per relation, one endpoint insert | 7.777ms +/- 1% | 22.53Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_80/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 80 rows per relation, one endpoint insert | 10.59ms +/- 1% | 25.13Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_80/count_star-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 80 rows per relation, one endpoint insert | 10.59ms +/- 0% | 25.13Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_80/count_column-24` | `COUNT(t3.id)` over 3-relation Cartesian multi-join, 80 rows per relation, one endpoint insert | 28.59ms +/- 0% | 25.24Ki +/- 0% | 73 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_80/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation Cartesian multi-join, 80 rows per relation, one endpoint insert | 47.08ms +/- 0% | 70.22Ki +/- 0% | 247 | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_80/sum-24` | `SUM(t3.id)` over 3-relation Cartesian multi-join, 80 rows per relation, one endpoint insert | 34.64ms +/- 1% | 25.47Ki +/- 0% | 75 | advisory |
| Multi-way Stage W Cartesian geomean | all focused Stage W Cartesian benchmarks | 12 sub-benchmark geomean | 10.33ms | 54.31Ki | 94.93 | advisory |

Current read:

- The bounded `cross3_rows_80` rows remain local-review-sized under
  `-count=10` while extending Cartesian size evidence beyond the 72-row shape.
- The table-shaped row's allocation growth tracks the larger materialized
  80x80 Cartesian fragment. The `COUNT(*)` row avoids that output
  materialization, but latency still scales with counting the combinations.
- `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while measuring higher latency in this Cartesian fixture.
- `COUNT(DISTINCT column)` remains the slowest Stage W Cartesian
  aggregate-function row and adds allocation, but remains local-review-sized
  in the focused run.
- Larger Cartesian fixtures beyond the bounded 80-row shape, larger
  skew/fanout distributions beyond 56x56, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage X Larger Skew/Fanout Function Shape

This focused snapshot extends the bounded 3-relation skew/fanout fixture from
`hot_key_56x56` to `hot_key_64x64`. The new rows keep the same 128 rows per
relation and one changed endpoint row; the changed row matches key `1`, the hot
key shared by 64 rows on each upstream relation, so it emits a 64x64 fanout
fragment. The snapshot records table-shaped projection and the aggregate
functions already accepted by the subscription layer. Runtime semantics and
default multi-way join guardrails are unchanged.

- Date: 2026-06-02
- Shunter commit: `7b17db3d10499dfd9d926bb1da87c2a713e22a47`
- Measurement worktree: commit above plus Stage X benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-35-generic #35~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Tue May 26 19:30:42 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 10 sub-benchmarks, `-count=10`, 100 benchmark rows, total
  package benchmark time 204.476s across two focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-x/selectivity-hot-key-56x56-64x64.txt`
  and
  `working-docs/release-evidence/2026-06-02-subscription-stage-x/aggregate-functions-hot-key-56x56-64x64.txt`
- Benchstat output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-x/selectivity-hot-key-56x56-64x64-benchstat.txt`
  and
  `working-docs/release-evidence/2026-06-02-subscription-stage-x/aggregate-functions-hot-key-56x56-64x64-benchstat.txt`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-06-02-subscription-stage-x
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinSelectivity$/^rows_128$/^hot_key_(56x56|64x64)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-x/selectivity-hot-key-56x56-64x64.txt 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^hot_key_(56x56|64x64)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-x/aggregate-functions-hot-key-56x56-64x64.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-x/selectivity-hot-key-56x56-64x64.txt > working-docs/release-evidence/2026-06-02-subscription-stage-x/selectivity-hot-key-56x56-64x64-benchstat.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-x/aggregate-functions-hot-key-56x56-64x64.txt > working-docs/release-evidence/2026-06-02-subscription-stage-x/aggregate-functions-hot-key-56x56-64x64-benchstat.txt 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_56x56-24` | 128 rows per relation, one changed hot-key row matching 56 left rows by 56 middle rows | 780.1us +/- 1% | 589.4Ki +/- 0% | 96.00 +/- 0% | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_64x64-24` | 128 rows per relation, one changed hot-key row matching 64 left rows by 64 middle rows | 937.1us +/- 2% | 834.0Ki +/- 0% | 101.0 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_56x56/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 56x56 fragment | 30.50ms +/- 1% | 37.80Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_64x64/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 64x64 fragment | 39.41ms +/- 2% | 37.88Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_64x64/count_column-24` | `COUNT(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 64x64 fragment | 39.26ms +/- 1% | 37.88Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_64x64/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 64x64 fragment | 39.61ms +/- 1% | 58.10Ki +/- 0% | 151.0 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_64x64/sum-24` | `SUM(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 64x64 fragment | 39.54ms +/- 1% | 38.00Ki +/- 0% | 75.00 +/- 0% | advisory |
| Multi-way Stage X selectivity geomean | focused Stage X selectivity hot-key benchmarks | 2 sub-benchmark geomean | 855.0us | 701.1Ki | 98.47 | advisory |
| Multi-way Stage X aggregate geomean | focused Stage X aggregate hot-key benchmarks | 8 sub-benchmark geomean | 34.71ms | 41.56Ki | 87.39 | advisory |

Current read:

- The bounded `hot_key_64x64` rows remain local-review-sized under
  `-count=10` while extending skew/fanout evidence beyond the 56x56 shape.
- The table-shaped row's allocation growth tracks the larger materialized
  64x64 fanout fragment.
- The `hot_key_64x64` aggregate-function rows are stable around
  39.26-39.61ms/op in this focused run. `COUNT(column)` and `SUM(column)`
  remain allocation-stable relative to `COUNT(*)`, while
  `COUNT(DISTINCT column)` adds allocation and allocation count without
  becoming a latency standout.
- Larger skew/fanout distributions beyond 64x64, larger Cartesian fixtures
  beyond the bounded 80-row shape, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage Y Larger Cartesian Function Shape

This focused snapshot extends the bounded 3-relation Cartesian fixture from
`cross3_rows_80` to `cross3_rows_88`. The new rows keep the same Cartesian
shape and one changed endpoint row; the changed row emits an 88x88 Cartesian
fragment. The snapshot records table-shaped projection, `COUNT(*)`, and the
aggregate functions already accepted by the subscription layer. Runtime
semantics and default multi-way join guardrails are unchanged.

- Date: 2026-06-02
- Shunter commit: `eb2df3ab949c4217cd4280afd2cdaa689b0f7ed7`
- Measurement worktree: commit above plus Stage Y benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-35-generic #35~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Tue May 26 19:30:42 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 12 sub-benchmarks, `-count=10`, 120 benchmark rows, total
  package benchmark time 153.786s across three focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-y/relation-shapes-raw.txt`,
  `working-docs/release-evidence/2026-06-02-subscription-stage-y/aggregate-relation-shapes-raw.txt`,
  and
  `working-docs/release-evidence/2026-06-02-subscription-stage-y/aggregate-functions-raw.txt`
- Benchstat output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-y/benchstat.txt`
  and
  `working-docs/release-evidence/2026-06-02-subscription-stage-y/all-benchstat.txt`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-06-02-subscription-stage-y
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinRelationShapes$/^cross3_rows_(80|88)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-y/relation-shapes-raw.txt 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateRelationShapes$/^cross3_rows_(80|88)$/^count$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-y/aggregate-relation-shapes-raw.txt 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^cross3_rows_(80|88)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-y/aggregate-functions-raw.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-y/relation-shapes-raw.txt working-docs/release-evidence/2026-06-02-subscription-stage-y/aggregate-relation-shapes-raw.txt working-docs/release-evidence/2026-06-02-subscription-stage-y/aggregate-functions-raw.txt > working-docs/release-evidence/2026-06-02-subscription-stage-y/benchstat.txt 2>&1
rtk cat working-docs/release-evidence/2026-06-02-subscription-stage-y/relation-shapes-raw.txt working-docs/release-evidence/2026-06-02-subscription-stage-y/aggregate-relation-shapes-raw.txt working-docs/release-evidence/2026-06-02-subscription-stage-y/aggregate-functions-raw.txt > working-docs/release-evidence/2026-06-02-subscription-stage-y/all-raw.txt
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-y/all-raw.txt > working-docs/release-evidence/2026-06-02-subscription-stage-y/all-benchstat.txt 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_80-24` | 3-relation Cartesian multi-join, 80 rows per relation, one endpoint insert emits an 80x80 fragment | 563.5us +/- 5% | 1.157Mi +/- 0% | 105.0 +/- 1% | advisory |
| Multi-way Cartesian shape | `MultiWayLiveJoinRelationShapes/cross3_rows_88-24` | 3-relation Cartesian multi-join, 88 rows per relation, one endpoint insert emits an 88x88 fragment | 715.3us +/- 5% | 1.587Mi +/- 0% | 112.0 +/- 0% | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_80/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 80 rows per relation, one endpoint insert | 10.58ms +/- 0% | 25.13Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate Cartesian shape | `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_88/count-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 88 rows per relation, one endpoint insert | 14.10ms +/- 0% | 27.77Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_88/count_star-24` | `COUNT(*)` over 3-relation Cartesian multi-join, 88 rows per relation, one endpoint insert | 14.50ms +/- 2% | 27.78Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_88/count_column-24` | `COUNT(t3.id)` over 3-relation Cartesian multi-join, 88 rows per relation, one endpoint insert | 38.67ms +/- 1% | 27.94Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_88/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation Cartesian multi-join, 88 rows per relation, one endpoint insert | 64.01ms +/- 1% | 75.75Ki +/- 0% | 263.5 +/- 0% | advisory |
| Multi-way aggregate Cartesian function | `MultiWayLiveJoinAggregateFunctions/cross3_rows_88/sum-24` | `SUM(t3.id)` over 3-relation Cartesian multi-join, 88 rows per relation, one endpoint insert | 46.66ms +/- 0% | 28.19Ki +/- 0% | 75.00 +/- 0% | advisory |
| Multi-way Stage Y Cartesian geomean | all focused Stage Y Cartesian benchmarks | 12 sub-benchmark geomean | 13.92ms | 60.75Ki | 96.49 | advisory |

Current read:

- The bounded `cross3_rows_88` rows remain local-review-sized under
  `-count=10` while extending Cartesian size evidence beyond the 80-row shape.
- The table-shaped row's allocation growth tracks the larger materialized
  88x88 Cartesian fragment. The `COUNT(*)` row avoids that output
  materialization, but latency still scales with counting the combinations.
- `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while measuring higher latency in this Cartesian fixture.
- `COUNT(DISTINCT column)` remains the slowest Stage Y Cartesian
  aggregate-function row and adds allocation, but remains local-review-sized
  in the focused run.
- Larger Cartesian fixtures beyond the bounded 88-row shape, larger
  skew/fanout distributions beyond 64x64, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
  app-derived workload distributions remain outside the current envelope.
- This evidence keeps the default multi-way join guardrails unchanged:
  unlimited by default, with app-owned opt-in limits available through config.

## Focused Multi-Way Live Join Stage Z Larger Skew/Fanout Function Shape

This focused snapshot extends the bounded 3-relation skew/fanout fixture from
`hot_key_64x64` to `hot_key_72x72`. The new rows keep the same 128 rows per
relation and one changed endpoint row; the changed row matches key `1`, the hot
key shared by 72 rows on each upstream relation, so it emits a 72x72 fanout
fragment. The snapshot records table-shaped projection and the aggregate
functions already accepted by the subscription layer. Runtime semantics and
default multi-way join guardrails are unchanged.

- Date: 2026-06-02
- Shunter commit: `054c5c26371aa2d3882ae96219730eb365e0ce66`
- Measurement worktree: commit above plus Stage Z benchmark and documentation
  changes
- Host: `Linux gernsback 6.17.0-35-generic #35~24.04.1-Ubuntu SMP PREEMPT_DYNAMIC Tue May 26 19:30:42 UTC 2 x86_64 x86_64 x86_64 GNU/Linux`
- Go: `go1.26.3`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`
- Raw sample: 10 sub-benchmarks, `-count=10`, 100 benchmark rows, total
  package benchmark time 206.497s across two focused `go test` invocations
- Raw output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-z/selectivity-hot-key-64x64-72x72.txt`
  and
  `working-docs/release-evidence/2026-06-02-subscription-stage-z/aggregate-functions-hot-key-64x64-72x72.txt`
- Benchstat output:
  `working-docs/release-evidence/2026-06-02-subscription-stage-z/selectivity-hot-key-64x64-72x72-benchstat.txt`,
  `working-docs/release-evidence/2026-06-02-subscription-stage-z/aggregate-functions-hot-key-64x64-72x72-benchstat.txt`,
  and
  `working-docs/release-evidence/2026-06-02-subscription-stage-z/all-benchstat.txt`

Command:

```bash
rtk mkdir -p working-docs/release-evidence/2026-06-02-subscription-stage-z
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinSelectivity$/^rows_128$/^hot_key_(64x64|72x72)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-z/selectivity-hot-key-64x64-72x72.txt 2>&1
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^hot_key_(64x64|72x72)$' -benchmem -count=10 ./subscription > working-docs/release-evidence/2026-06-02-subscription-stage-z/aggregate-functions-hot-key-64x64-72x72.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-z/selectivity-hot-key-64x64-72x72.txt > working-docs/release-evidence/2026-06-02-subscription-stage-z/selectivity-hot-key-64x64-72x72-benchstat.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-z/aggregate-functions-hot-key-64x64-72x72.txt > working-docs/release-evidence/2026-06-02-subscription-stage-z/aggregate-functions-hot-key-64x64-72x72-benchstat.txt 2>&1
rtk go run golang.org/x/perf/cmd/benchstat@latest working-docs/release-evidence/2026-06-02-subscription-stage-z/selectivity-hot-key-64x64-72x72.txt working-docs/release-evidence/2026-06-02-subscription-stage-z/aggregate-functions-hot-key-64x64-72x72.txt > working-docs/release-evidence/2026-06-02-subscription-stage-z/all-benchstat.txt 2>&1
```

Representative standings:

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_64x64-24` | 128 rows per relation, one changed hot-key row matching 64 left rows by 64 middle rows | 906.6us +/- 1% | 833.9Ki +/- 0% | 101.0 +/- 0% | advisory |
| Multi-way selectivity | `MultiWayLiveJoinSelectivity/rows_128/hot_key_72x72-24` | 128 rows per relation, one changed hot-key row matching 72 left rows by 72 middle rows | 1.066ms +/- 0% | 1.147Mi +/- 0% | 106.0 +/- 1% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_64x64/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 64x64 fragment | 39.00ms +/- 0% | 37.88Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_72x72/count_star-24` | `COUNT(*)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 72x72 fragment | 48.90ms +/- 1% | 37.97Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_72x72/count_column-24` | `COUNT(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 72x72 fragment | 49.01ms +/- 0% | 37.97Ki +/- 0% | 73.00 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_72x72/count_distinct-24` | `COUNT(DISTINCT t1.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 72x72 fragment | 49.10ms +/- 0% | 59.57Ki +/- 0% | 159.0 +/- 0% | advisory |
| Multi-way aggregate skew function | `MultiWayLiveJoinAggregateFunctions/hot_key_72x72/sum-24` | `SUM(t3.id)` over 3-relation chain, 128 rows per relation, one changed hot-key endpoint row matching a 72x72 fragment | 48.96ms +/- 2% | 38.09Ki +/- 0% | 75.00 +/- 0% | advisory |
| Multi-way Stage Z selectivity geomean | focused Stage Z selectivity hot-key benchmarks | 2 sub-benchmark geomean | 982.9us | 989.5Ki | 103.5 | advisory |
| Multi-way Stage Z aggregate geomean | focused Stage Z aggregate hot-key benchmarks | 8 sub-benchmark geomean | 43.79ms | 42.36Ki | 88.71 | advisory |

Current read:

- The bounded `hot_key_72x72` rows remain local-review-sized under
  `-count=10` while extending skew/fanout evidence beyond the 64x64 shape.
- The table-shaped row's allocation growth tracks the larger materialized
  72x72 fanout fragment.
- The `hot_key_72x72` aggregate-function rows are stable around
  48.90-49.10ms/op in this focused run. `COUNT(column)` and `SUM(column)`
  remain allocation-stable relative to `COUNT(*)`, while
  `COUNT(DISTINCT column)` adds allocation and allocation count without
  becoming a latency standout.
- Larger skew/fanout distributions beyond 72x72, larger Cartesian fixtures
  beyond the bounded 88-row shape, relation counts beyond the bounded
  5-relation chain, larger aggregate-function self-alias distributions, and
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
  chain for table-shaped projection and `COUNT(*)`, Stage G extends bounded
  skew/fanout evidence to `hot_key_16x16`, and Stage H extends bounded
  Cartesian evidence to `cross3_rows_32` for table-shaped projection and
  `COUNT(*)`. Stage I extends aggregate-function evidence from `chain3` to
  bounded `chain4` rows for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`. Stage J extends those
  aggregate-function rows to bounded `cross3_rows_32` Cartesian rows. Stage K
  extends them to the bounded `hot_key_16x16` skew/fanout fixture. Stage L
  extends them to the bounded `self_alias3` repeated-table fixture. Stage M
  extends Cartesian evidence to `cross3_rows_40` for table-shaped projection,
  `COUNT(*)`, and the current aggregate-function rows. Stage N extends
  skew/fanout evidence to `hot_key_24x24` for table-shaped projection and the
  current aggregate-function rows. Stage O extends larger bounded Cartesian
  evidence to `cross3_rows_48` for table-shaped projection, `COUNT(*)`, and
  the current aggregate-function rows. Stage P extends skew/fanout evidence to
  `hot_key_32x32`, Stage Q extends Cartesian evidence to `cross3_rows_56`,
  Stage R extends skew/fanout evidence to `hot_key_40x40`, Stage S extends
  Cartesian evidence to `cross3_rows_64`, Stage T extends skew/fanout evidence
  to `hot_key_48x48`, and Stage U extends Cartesian evidence to
  `cross3_rows_72`. Stage V extends skew/fanout evidence to
  `hot_key_56x56`, Stage W extends Cartesian evidence to `cross3_rows_80`,
  Stage X extends skew/fanout evidence to `hot_key_64x64`, and Stage Y
  extends Cartesian evidence to `cross3_rows_88`. Stage Z extends
  skew/fanout evidence to `hot_key_72x72`. This evidence does not justify
  changing the unlimited defaults.
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
  through Stage Z snapshots, including larger Cartesian fixtures beyond the
  bounded 88-row cross shape, larger skew/fanout distributions beyond the
  bounded 72x72 row, relation counts beyond the bounded 5-relation chain
  fixture, larger
  aggregate-function self-alias distributions beyond the bounded `self_alias3`
  fixture, and app-derived workload distributions
- memory profiles outside the current subscription, single-WebSocket,
  16/64/128-client WebSocket fanout, sender-level backpressure, executor
  reducer commit, and small/larger local backup/restore fixtures, including
  application-scale fanout, slow-reader network paths, and production-sized
  backup/restore workloads
