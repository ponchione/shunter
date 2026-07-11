# Clean-ref ordered-join performance evidence

- Result: advisory measurement completed
- Candidate: `bfef461409e9158b53ad4dc96dc956ca1598fe6a`
- Environment: Linux/amd64, Go 1.26.3, AMD Ryzen 9 9900X 12-Core Processor
- Samples: 10
- Scope: only the representative ordered two-table join row

Command:

```text
go test -run '^$' -bench '^BenchmarkExecuteCompiledSQLQueryJoinReadShapes$/^two_table_join_projection_order_limit$' -benchmem -count=10 ./protocol
```

Pinned comparison tool:
`golang.org/x/perf/cmd/benchstat@v0.0.0-20260709024250-82a0b07e230d`,
with `-ignore cpu` as in the preceding evidence.

| State | sec/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| v1.1.0 baseline | 4.044 ms | 832.9 KiB | 4.729k |
| dirty remediation result | 4.786 ms (+18.34% vs v1.1.0) | 269.6 KiB (-67.63%) | 3.162k (-33.14%) |
| clean candidate | 4.910 ms (+21.39% vs v1.1.0) | 269.7 KiB (-67.63%) | 3.162k (-33.14%) |

Directly against the dirty remediation result, the clean candidate measured
2.58% slower latency (`p=0.000`, 10 samples); bytes/op were not significantly
different (`p=0.053`) and all allocation samples were equal.

The metadata-bearing raw evidence file caused benchstat to separate the clean
sample as a distinct configuration. The canonical comparison therefore uses
`perf-ordered-join-clean-ref-samples.txt`, which contains the same 10 raw
benchmark lines extracted without rerunning the benchmark. The original raw
record and initial comparison are preserved.

No hard pass/fail threshold was introduced. The result remains local advisory
evidence, and the ordered-join latency regression versus v1.1.0 remains an
explicit residual risk.

