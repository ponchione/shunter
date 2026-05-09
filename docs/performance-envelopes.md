# Shunter Performance Envelopes

Status: current advisory v1 release-qualification snapshot
Scope: existing Go benchmarks for protocol, commitlog, and subscription hot
paths.

This page records measured behavior for the benchmark coverage that already
exists. The rows are advisory for v1 release qualification unless a future
release gate adds hard thresholds.

## Snapshot

- Date: 2026-05-09
- Shunter commit: `8d3306b2ff85b26f47ffa8bfbc4899355545b6e5`
- Measurement worktree: clean detached checkout at the commit above
- Host: `Linux gernsback 6.17.0-23-generic`, linux/amd64
- Go: `go1.26.2`
- CPU: `AMD Ryzen 9 9900X 12-Core Processor`, 12 cores, 24 logical CPUs

Commands:

```bash
go test -run '^$' -bench . -benchmem ./protocol ./commitlog ./subscription
go test -run '^$' -bench . -benchmem -count=10 ./protocol ./commitlog ./subscription > /tmp/shunter-bench-new.txt
rtk go run golang.org/x/perf/cmd/benchstat@latest /tmp/shunter-bench-new.txt
```

The tables below use `benchstat` summaries from local 10-run samples. `+/-`
values are from those local samples. The varied-query fanout row was measured
with:

```bash
go test -run '^$' -bench 'BenchmarkFanOut1KClients' -benchmem -count=10 ./subscription > /tmp/shunter-fanout-bench.txt
rtk go run golang.org/x/perf/cmd/benchstat@latest /tmp/shunter-fanout-bench.txt
```

Every row is advisory.

## Protocol

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Compression | `WrapCompressedGzip-24` | 2 KiB repetitive body | 8.796us +/- 7% | 256 B +/- 4% | 3 | advisory |
| Compression | `UnwrapCompressedGzip-24` | 2 KiB repetitive body | 1.022us +/- 14% | 4.616Ki +/- 0% | 7 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/filter_limit-24` | 1,024 task rows | 2.005us +/- 5% | 1.961Ki +/- 0% | 15 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/projection_order_limit-24` | 1,024 task rows | 336.5us +/- 2% | 478.1Ki +/- 0% | 1.082k | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/count_filter-24` | 1,024 task rows | 13.23us +/- 1% | 456 B +/- 0% | 12 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/sum_filter-24` | 1,024 task rows | 19.85us +/- 1% | 616 B +/- 0% | 14 | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/two_table_join_projection_order_limit-24` | 256 users, 32 teams, 1,024 orders | 4.706ms +/- 1% | 832.9Ki +/- 0% | 4.729k | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/multi_way_join_count-24` | 256 users, 32 teams, 1,024 orders | 9.333ms +/- 2% | 558.7Ki +/- 0% | 15.12k | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/multi_way_join_sum-24` | 256 users, 32 teams, 1,024 orders | 8.738ms +/- 1% | 558.9Ki +/- 0% | 15.12k | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/single_table_filter-24` | parse and register single-table query | 1.634us +/- 7% | 3.219Ki +/- 0% | 26 | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/two_table_join-24` | parse and register two-table join | 2.777us +/- 3% | 5.492Ki +/- 0% | 44 | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/multi_way_join-24` | parse and register multi-way join | 5.659us +/- 10% | 14.67Ki +/- 0% | 92 | advisory |

## Commitlog

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Segmented replay | `ReplayLogSegmentedLog-24` | 4 segments, 256 records each | 288.0ms +/- 8% | 399.3Mi +/- 0% | 1.663M | advisory |
| Segmented recovery | `OpenAndRecoverSegmentedLog-24` | 4 segments, 256 records each | 311.8ms +/- 26% | 399.9Mi +/- 0% | 1.675M | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/small-24` | 128 snapshot rows | 238.4us +/- 9% | 747.6Ki +/- 0% | 2.075k | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/medium-24` | 1,024 snapshot rows | 1.442ms +/- 19% | 5.532Mi +/- 0% | 14.73k | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/large-24` | 4,096 snapshot rows | 6.048ms +/- 15% | 22.12Mi +/- 0% | 58.12k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/small-24` | 128 snapshot rows, 16 tail records | 1.373ms +/- 6% | 2.510Mi +/- 0% | 9.708k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/medium-24` | 1,024 snapshot rows, 128 tail records | 88.81ms +/- 11% | 113.3Mi +/- 0% | 450.4k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/large-24` | 4,096 snapshot rows, 512 tail records | 1.456s +/- 10% | 1.700Gi +/- 0% | 6.936M | advisory |
| Snapshot creation | `CreateSnapshotLarge-24` | 4,096 rows | 24.04ms +/- 8% | 2.867Mi +/- 0% | 25.23k | advisory |

## Subscription

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Equality subscription eval | `EvalEqualitySubs1K-24` | 1,000 equality subscriptions, 1 changed row | 1.122us +/- 6% | 927 B +/- 0% | 10 | advisory |
| Equality subscription eval | `EvalEqualitySubs10K-24` | 10,000 equality subscriptions, 1 changed row | 987.5ns +/- 2% | 924 B +/- 0% | 10 | advisory |
| Subscription lifecycle | `RegisterUnregister-24` | register and unregister one equality query | 1.845us +/- 7% | 3.937Ki +/- 0% | 30 | advisory |
| Initial snapshot | `RegisterSetInitialQueryAllRows-24` | 1,024 committed rows | 56.58us +/- 1% | 71.25Ki +/- 0% | 77 | advisory |
| Initial snapshot diff | `ProjectedRowsBeforeLargeBags-24` | 4,096 current rows, 2,048 inserted rows, 64 distinct keys | 778.6us +/- 1% | 871.8Ki +/- 0% | 12.32k | advisory |
| Fanout | `FanOut1KClientsSameQuery-24` | 1,000 clients on one equality query | 167.3us +/- 9% | 321.3Ki +/- 0% | 2.029k | advisory |
| Fanout | `FanOut1KClientsVariedQueries-24` | 1,000 clients across equality, range, AND, and OR predicates; 256 changed rows | 1.761ms +/- 2% | 448.7Ki +/- 0% | 3.405k | advisory |
| Join delta eval | `JoinFragmentEval-24` | two-table join, 100 committed rows per side, 10 inserts per side | 146.0us +/- 1% | 81.34Ki +/- 0% | 285 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/table_shape-24` | 32 rows per joined table | 27.97us +/- 6% | 17.97Ki +/- 0% | 167 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/count-24` | 32 rows per joined table, `COUNT(*)` | 114.7us +/- 3% | 18.25Ki +/- 0% | 170 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/table_shape-24` | 128 rows per joined table | 298.1us +/- 0% | 68.84Ki +/- 0% | 371 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/count-24` | 128 rows per joined table, `COUNT(*)` | 1.628ms +/- 3% | 69.05Ki +/- 0% | 374 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/table_shape-24` | 512 rows per joined table | 4.167ms +/- 2% | 283.0Ki +/- 0% | 1.153k | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/count-24` | 512 rows per joined table, `COUNT(*)` | 25.05ms +/- 0% | 282.7Ki +/- 0% | 1.155k | advisory |
| Delta indexes | `DeltaIndexConstruction-24` | 100 changed rows, 5 indexed columns | 34.28us +/- 2% | 3.958Ki +/- 0% | 501 | advisory |
| Candidate collection | `CandidateCollection-24` | 1,000 equality subscriptions, 10 changed rows | 1.003us +/- 1% | 528 B +/- 0% | 3 | advisory |

## Current Read

- Existing equality subscription evaluation and candidate collection remain the
  healthiest hot paths.
- Large bag diffing, large snapshot-plus-tail recovery, segmented log replay,
  and multi-way joins at 512 rows per table are the clearest allocation and
  latency targets in the current coverage.
- The current rows are not release-blocking thresholds. Treat regressions here
  as investigation triggers until the release process defines hard limits.

## Known Gaps

These remain outside the current benchmark envelope:

- WebSocket network-level subscription workloads beyond handler admission
- broader fanout distributions beyond deterministic same-query and varied
  single-table predicate fixtures
- external canary workload and backup/restore workflow
- memory profiles for large joins and initial snapshots
