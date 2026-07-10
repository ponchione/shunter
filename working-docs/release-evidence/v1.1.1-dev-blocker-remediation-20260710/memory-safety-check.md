# Pre-benchmark memory-safety check

- Date: 2026-07-10
- Host: Linux/amd64, AMD Ryzen 9 9900X 12-Core Processor
- Purpose: bounded check before resuming the benchmark interrupted by the prior hard system failure
- Result: pass; no evidence of a Shunter memory or goroutine leak in the retained change

## Host and prior-boot evidence

Before testing, the host reported 125 GiB RAM, 114 GiB available, no swap use,
and zero memory-pressure averages. The prior boot ran from 2026-07-10 03:38 EDT
until an abrupt final journal entry around 07:55 EDT. Searches of the previous
boot journal found no kernel OOM kill, userspace OOM action, kernel panic,
watchdog reset, machine-check failure, or Shunter/process coredump. Kernel audit
suppression messages continued until shortly before the log stopped.

The prior failure is therefore consistent with a hard reset or other abrupt
system failure, but the available journal does not establish its cause.

## Retained-code review

The ordered-join change adds one ORDER BY term slice sized to the compiled
query's ORDER BY list. It stores source, column index, direction, and name once
per query execution. It adds no global state, cache, goroutine, or
request-lifetime retention. The existing pair slice remains bounded by the
number of visited join pairs and is released with the query result path.

The discarded flat-key experiment is absent from the current diff.

## Focused checks

Commands:

```text
rtk go test ./protocol -run '^(TestCompileOrderedOneOffPairTermsPreservesSelfJoinAliases|TestOI002JoinProjectionOrderByProjectionAlias|TestOI002JoinProjectionMultiColumnOrderByProjectionAliases|TestOI002JoinProjectionMultiColumnOrderByNonProjectedTableRejected|TestHandleOneOffQuery_MultiWayJoinReturnsRows)$' -count=50
rtk go test -race ./protocol -run '^(TestCompileOrderedOneOffPairTermsPreservesSelfJoinAliases|TestOI002JoinProjectionOrderByProjectionAlias|TestOI002JoinProjectionMultiColumnOrderByProjectionAliases|TestOI002JoinProjectionMultiColumnOrderByNonProjectedTableRejected|TestHandleOneOffQuery_MultiWayJoinReturnsRows)$' -count=1
```

Results:

- 250 repeated focused test executions passed under the package's
  `goleak.VerifyTestMain` harness.
- All five focused tests passed under the race detector.
- End-of-test in-use heap was 105.96 KiB after one pass and 148.71 KiB after
  50 passes with allocation sampling set to one. The retained nodes were
  dominated by Go runtime, regexp, and sync-pool state; no query-term or
  ordered-pair retention appeared.
- The modest 42.75 KiB difference after 50 repetitions is not linear growth
  and does not indicate a leak.

## Benchmark observation

The 16.8-second resumed benchmark kept host free memory approximately flat at
86.5-86.7 GiB in `vmstat`, used no swap, and left 114 GiB available
immediately afterward.

This check is intentionally narrow. It is not a production soak or a proof that
the prior machine crash was hardware-related.
