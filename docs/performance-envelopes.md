# Shunter Performance Envelopes

Status: current advisory v1 release-qualification snapshot
Scope: existing Go benchmarks for protocol, commitlog, and subscription hot
paths.

This page records measured behavior for the benchmark coverage that already
exists. The rows are advisory for v1 release qualification unless a future
release gate adds hard thresholds.

## Snapshot

- Date: 2026-05-09
- Shunter commit: `452cdebb433af76f3c4bb65f6c95b90c26a0af4f`
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

The tables below use the `benchstat` summary from the 10-run sample. `+/-`
values are from that local sample. Every row is advisory.

## Protocol

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Compression | `WrapCompressedGzip-24` | 2 KiB repetitive body | 8.753us +/- 6% | 246 B +/- 4% | 3 | advisory |
| Compression | `UnwrapCompressedGzip-24` | 2 KiB repetitive body | 1.023us +/- 5% | 4.616Ki +/- 0% | 7 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/filter_limit-24` | 1,024 task rows | 1.878us +/- 4% | 1.961Ki +/- 0% | 15 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/projection_order_limit-24` | 1,024 task rows | 358.5us +/- 9% | 478.1Ki +/- 0% | 1.082k | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/count_filter-24` | 1,024 task rows | 13.11us +/- 0% | 456 B +/- 0% | 12 | advisory |
| One-off SQL | `ExecuteCompiledSQLQueryCommonPaths/sum_filter-24` | 1,024 task rows | 19.84us +/- 2% | 616 B +/- 0% | 14 | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/two_table_join_projection_order_limit-24` | 256 users, 32 teams, 1,024 orders | 4.698ms +/- 3% | 832.9Ki +/- 0% | 4.729k | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/multi_way_join_count-24` | 256 users, 32 teams, 1,024 orders | 9.201ms +/- 3% | 558.7Ki +/- 0% | 15.12k | advisory |
| One-off SQL joins | `ExecuteCompiledSQLQueryJoinReadShapes/multi_way_join_sum-24` | 256 users, 32 teams, 1,024 orders | 8.738ms +/- 3% | 558.9Ki +/- 0% | 15.12k | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/single_table_filter-24` | parse and register single-table query | 1.552us +/- 4% | 3.219Ki +/- 0% | 26 | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/two_table_join-24` | parse and register two-table join | 3.105us +/- 9% | 5.492Ki +/- 0% | 44 | advisory |
| Subscribe admission | `HandleSubscribeSingleAdmissionReadShapes/multi_way_join-24` | parse and register multi-way join | 5.559us +/- 8% | 14.67Ki +/- 0% | 92 | advisory |

## Commitlog

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Segmented replay | `ReplayLogSegmentedLog-24` | 4 segments, 256 records each | 293.0ms +/- 11% | 399.2Mi +/- 0% | 1.663M | advisory |
| Segmented recovery | `OpenAndRecoverSegmentedLog-24` | 4 segments, 256 records each | 275.9ms +/- 3% | 400.0Mi +/- 0% | 1.675M | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/small-24` | 128 snapshot rows | 276.5us +/- 21% | 747.7Ki +/- 0% | 2.076k | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/medium-24` | 1,024 snapshot rows | 1.403ms +/- 10% | 5.532Mi +/- 0% | 14.73k | advisory |
| Snapshot recovery | `OpenAndRecoverSnapshotOnly/large-24` | 4,096 snapshot rows | 5.873ms +/- 17% | 22.12Mi +/- 0% | 58.12k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/small-24` | 128 snapshot rows, 16 tail records | 1.357ms +/- 6% | 2.510Mi +/- 0% | 9.709k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/medium-24` | 1,024 snapshot rows, 128 tail records | 81.08ms +/- 13% | 113.3Mi +/- 0% | 450.4k | advisory |
| Snapshot plus tail replay | `OpenAndRecoverSnapshotWithTailReplay/large-24` | 4,096 snapshot rows, 512 tail records | 1.599s +/- 7% | 1.700Gi +/- 0% | 6.936M | advisory |
| Snapshot creation | `CreateSnapshotLarge-24` | 4,096 rows | 24.58ms +/- 13% | 2.866Mi +/- 1% | 25.23k | advisory |

## Subscription

| Workload area | Benchmark | Fixture | sec/op | B/op | allocs/op | Gate |
| --- | --- | --- | ---: | ---: | ---: | --- |
| Equality subscription eval | `EvalEqualitySubs1K-24` | 1,000 equality subscriptions, 1 changed row | 1.113us +/- 5% | 927 B +/- 0% | 10 | advisory |
| Equality subscription eval | `EvalEqualitySubs10K-24` | 10,000 equality subscriptions, 1 changed row | 1.045us +/- 11% | 924 B +/- 0% | 10 | advisory |
| Subscription lifecycle | `RegisterUnregister-24` | register and unregister one equality query | 1.754us +/- 21% | 3.936Ki +/- 0% | 30 | advisory |
| Initial snapshot | `RegisterSetInitialQueryAllRows-24` | 1,024 committed rows | 57.46us +/- 3% | 71.24Ki +/- 0% | 77 | advisory |
| Initial snapshot diff | `ProjectedRowsBeforeLargeBags-24` | 4,096 current rows, 2,048 inserted rows, 64 distinct keys | 764.4us +/- 3% | 871.7Ki +/- 0% | 12.32k | advisory |
| Fanout | `FanOut1KClientsSameQuery-24` | 1,000 clients on one equality query | 157.3us +/- 8% | 321.3Ki +/- 0% | 2.029k | advisory |
| Join delta eval | `JoinFragmentEval-24` | two-table join, 100 committed rows per side, 10 inserts per side | 152.3us +/- 4% | 81.35Ki +/- 0% | 285 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/table_shape-24` | 32 rows per joined table | 118.0us +/- 3% | 34.63Ki +/- 0% | 320 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_32/count-24` | 32 rows per joined table, `COUNT(*)` | 109.3us +/- 1% | 17.73Ki +/- 0% | 158 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/table_shape-24` | 128 rows per joined table | 1.648ms +/- 3% | 138.4Ki +/- 0% | 916 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_128/count-24` | 128 rows per joined table, `COUNT(*)` | 1.588ms +/- 2% | 68.54Ki +/- 0% | 362 | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/table_shape-24` | 512 rows per joined table | 24.39ms +/- 1% | 581.9Ki +/- 0% | 3.243k | advisory |
| Multi-way join eval | `MultiWayLiveJoinEvalSizes/rows_512/count-24` | 512 rows per joined table, `COUNT(*)` | 24.93ms +/- 2% | 282.5Ki +/- 0% | 1.143k | advisory |
| Delta indexes | `DeltaIndexConstruction-24` | 100 changed rows, 5 indexed columns | 34.17us +/- 2% | 3.958Ki +/- 0% | 501 | advisory |
| Candidate collection | `CandidateCollection-24` | 1,000 equality subscriptions, 10 changed rows | 1.006us +/- 1% | 528 B +/- 0% | 3 | advisory |

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
- varied-query fanout across many clients
- external canary workload and backup/restore workflow
- memory profiles for large joins and initial snapshots
