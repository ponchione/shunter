# Representative performance evidence

- Current Shunter commit: `9fddcf0b842f72d5e24e399d558042394b337fbd`
- Baselines: broad `v1.1.0` snapshot at `947e3dd2eb4dda1738abd670009845f127a745e0`; ordered-window published snapshot at `9c88c7ddc4153d6f33dea3f9bb2fb032f40deab3`
- Date: 2026-07-10
- Environment: Linux/amd64, Go `go1.26.3`, AMD Ryzen 9 9900X 12-Core Processor
- Count: 10 samples per refreshed row
- Scope: four representative rows tied to the unreleased changelog's ordered-read allocation claims; the broader benchmark corpus was not run.

## One-off ordered reads

Commands:

`go test -run '^$' -bench '^BenchmarkExecuteCompiledSQLQueryCommonPaths$/^projection_order_limit$' -benchmem -count=10 ./protocol`

`go test -run '^$' -bench '^BenchmarkExecuteCompiledSQLQueryJoinReadShapes$/^two_table_join_projection_order_limit$' -benchmem -count=10 ./protocol`

| Benchmark | sec/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Single-table baseline | 309.7us | 478.10Ki | 1082 |
| Single-table current | 220.0us (-28.98%) | 78.16Ki (-83.65%) | 58 (-94.64%) |
| Two-table join baseline | 4.044ms | 832.9Ki | 4.729k |
| Two-table join current | 6.716ms (+66.04%) | 269.6Ki (-67.63%) | 3.161k (-33.16%) |

The allocation claim is supported for both representative rows. The join row's latency regression is also material and must remain explicit; this evidence does not support a general performance-improvement claim.

Evidence:

- [perf-oneoff-focused-benchstat.txt](perf-oneoff-focused-benchstat.txt)
- [perf-oneoff-projection-order-limit-raw.txt](perf-oneoff-projection-order-limit-raw.txt)
- [perf-oneoff-join-projection-order-limit-raw.txt](perf-oneoff-join-projection-order-limit-raw.txt)

## Ordered live-view initial materialization

Commands:

`go test -run '^$' -bench '^BenchmarkInitialRowsForTableOrderedWindow$/^table_scan$/^rows_1024$/^limit_100$/^offset_0$/^shuffled$/^1col$' -benchmem -count=10 ./subscription`

`go test -run '^$' -bench '^BenchmarkOrderedInitialRowsComparatorShapes$/^bounded$/^rows_4096$/^shuffled$/^1col$/^desc$/^ties$' -benchmem -count=10 ./subscription`

| Benchmark | Published baseline | Current |
| --- | --- | --- |
| Initial rows, 1,024/top 100 | 252.4us, 255.6Ki, 3.085k allocs | 119.7us, 20.75Ki, 13 allocs |
| Bounded comparator, 4,096/ties | 1.597ms, 1.129Mi, 12.29k allocs | 884.6us, 224.1Ki, 5 allocs |

The allocation claim is supported. The historical ordered-window raw sample is no longer present, so the comparison uses its published `-count=10` summary rather than a new statistical before/after benchstat comparison.

Evidence:

- [perf-ordered-focused-benchstat.txt](perf-ordered-focused-benchstat.txt)
- [perf-ordered-initial-window-raw.txt](perf-ordered-initial-window-raw.txt)
- [perf-ordered-comparator-ties-raw.txt](perf-ordered-comparator-ties-raw.txt)

All rows remain advisory local evidence, not production-scale throughput, fanout, or latency claims.

