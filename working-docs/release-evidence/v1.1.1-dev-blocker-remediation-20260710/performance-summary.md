# Ordered-join remediation performance

- Date/time: benchmark started 2026-07-10T12:28:16Z
- Shunter base commit: `9fddcf0b842f72d5e24e399d558042394b337fbd`
- Worktree: dirty remediation tree; not an immutable qualified ref
- Environment: Linux/amd64, Go 1.26.3, AMD Ryzen 9 9900X 12-Core Processor
- Samples: 10 per row
- Scope: representative ordered two-table join only

Command:

```text
go test -run '^$' -bench '^BenchmarkExecuteCompiledSQLQueryJoinReadShapes$/^two_table_join_projection_order_limit$' -benchmem -count=10 ./protocol
```

Benchstat used `-ignore cpu` because the preserved v1.1.0 baseline file does
not contain the current raw sample's CPU metadata field.

| State | sec/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| v1.1.0 baseline | 4.044 ms | 832.9 KiB | 4.729k |
| v1.1.1-dev before remediation | 6.704 ms (+65.76%) | 269.6 KiB (-67.63%) | 3.161k (-33.16%) |
| remediated tree | 4.786 ms (+18.34%) | 269.6 KiB (-67.63%) | 3.162k (-33.14%) |

Relative to the pre-remediation development tree, the retained comparator
change improves latency by 28.61%. The extra precompiled term slice accounts
for the measured one-allocation difference while eliminating repeated
left/right source resolution inside sort comparisons.

## Classification

Materially improved with a residual latency regression. The original 65.76%
latency regression is reduced to 18.34%, and the substantial byte/allocation
improvements versus v1.1.0 remain. The changelog makes an allocation and
source-resolution claim, not a general latency-improvement claim.

These are local advisory measurements, not production-scale performance
claims.

Evidence:

- `perf-ordered-join-v1.1.0-baseline.txt`
- `perf-ordered-join-current-raw.txt`
- `perf-ordered-join-remediated-raw.txt`
- `perf-ordered-join-remediated-benchstat.txt`
