# Scheduled-load reporting and response-path attribution — 2026-09-08

The supported capacity tool now exposes offered work and scheduling backlog.
The bounded diagnostic study locates much of the remaining elapsed read time
between server socket-write start and client frame receipt, with additional
request-specific delays in client decoding/queueing and before handler entry.
That residual still combines transport, buffering and client scheduling. **No
runtime/application optimization was adopted; PERF-03 and PERF-07 remain open.**

## Delivered reporting and exact execution

The [maintained diff](maintained-harness.patch) adds `scheduled-arrival` to
`scripts/measure-canary-capacity`, with distinct offered/dispatch/completion counts
and rates, after-window completions, read dispatch/scheduled p95/p99, scheduling
lateness, exact queued/in-flight/outstanding counts and peaks, deadline outcomes,
and separate response/handoff/row-decode/exhaustive-validation intervals. It records
all intended operations before a common origin, including undispatched work.
Timed-out writes remain unknown until reconciled against recovered audit rows.

The runner copies current Canary source, including relevant untracked files,
records source/fixture hashes and retains separate scheduled-run outputs. Default
closed-loop workloads keep their metric names and original repetition order.
Only necessary Q1 scheduling/correctness logic was extracted; board, slow-consumer,
profiling and runtime research APIs were not added to maintained code. The three
new Go testdata files compile in the staged Canary workflow package. No new dependency.

Exactly **8 ordinary and 4 diagnostic measured invocations** ran serially. Order:
1000/even, 4000/burst, 4000/even, 1000/burst; each cell ran ordinary A, diagnostic,
ordinary B. Every invocation used the same 32 clients × 100 choices (1637 writes,
1563 reads), 8194 initial tickets, eight subscriptions/project, FullUpdate replies,
one active request/client and two snapshots at offered-window thirds. Even and
burst offsets and all deadlines are in the frozen [contract](contract.md) and
[matrix](frozen-matrix.json). No measured sample was retried, replaced or discarded.

Two excluded smoke invocations preceded freezing: the supported-script 320-operation
smoke passed; a diagnostic command without enabling environment skipped and ran no
workload. One separate 320-operation race workload passed after measurement. The
[notes](measurement-notes.md) preserve setup/analysis errors and scope decisions.
The existing 100/s anchor is reused from the previous investigation; no new anchor,
overload, board or slow-consumer matrix ran.

Validation passed: schedule/backlog/deadline/unit/nearest-rank tests; declared-read,
authorization/visibility, snapshot ordering and unsubscribe workflows; the new
concurrent workload under race; diagnostic buffer overflow/correlation and protocol
client/server race tests; formatting, shell syntax, vet and pinned Staticcheck.
Canary used Shunter's pinned Staticcheck executable. Every `check-*/command.json`,
`raw.txt` and `exit.json` records the exact check. Reconstruction independently
verified **1562 source/fixture files** and passed the arrival tests without a workload.

[Preservation checks](preservation-check.json) prove **3113 pre-existing files
unchanged**, including all 76 Canary files and the complete prior evidence tree.
The four intentionally edited existing Shunter files have original bytes preserved
in the source archive; pre-existing changelog edits also survive verbatim. Three
new maintained testdata files were added. Both HEADs and indexes are unchanged.
Final review restored closed-loop repetition order, corrected a tie-order comment,
and guarded two failure-only reporting cases;
[the final review changes](final-review-vs-measured.patch) are separate from
[the exact measured diff](measured-harness.patch). Frozen measured sources were not
rewritten. Final reporter replay reproduces all semantic measurements from all 12
archived runs; the two live backing-capacity gauges cannot be reconstructed from
JSON-allocated slices and remain recorded from the original runs. No staging, commit, push, PR, qualification, release evidence, version
change, tag or publication occurred.

## Offered load, latency and backlog

Every run offered/dispatched/completed all **3200** operations. Offered windows were
3.2s at 1000/s and 0.8s at 4000/s. Read latencies below include final validation;
application row decoding and exhaustive assertion cost remain separately reported.
Ranges contain the two ordinary per-run values, not pooled percentiles or confidence
intervals. The [full per-run table](metrics.md) includes all 12 invocations;
[metrics.json](metrics.json) also contains dispatch rates, within/after-window counts,
lateness, deadline accounting, decoding, memory and buffer metrics.

| Offered/s | Shape | Dispatch p95 / p99, ms | Scheduled p95 / p99, ms | Completed/s | Peak queued | Peak outstanding |
|---:|---|---:|---:|---:|---:|---:|
| 1000 | even | 0.356–0.365 / 0.715–0.741 | 1.110–1.112 / 1.302–1.566 | 997.8–997.9 | 5–9 | 21–33 |
| 1000 | burst | 0.664–0.810 / 0.973–1.021 | 1.520–1.682 / 1.955–2.904 | 1000 | 32 | 32 |
| 4000 | even | 0.793–0.852 / 1.884–2.873 | 50.012–74.483 / 71.634–101.246 | 3695.6–3788.4 | 128–174 | 157–205 |
| 4000 | burst | 0.824–1.134 / 1.702–2.109 | 45.323–47.681 / 68.342–68.695 | 3835.2–3839.1 | 127–131 | 155–162 |

At 4000/s, all ordinary runs reached 32 in-flight requests. The extra outstanding
work waited in the preserved client schedule. At window end, 42–88 operations
remained outstanding and completed afterward. At 1000/even, 5–6 completed afterward;
1000/burst drained before window end. Every deadline ended with zero undispatched
work and zero active waits. Counts/rates retain the complete offered window even
when its last request finishes early. Maximum ordinary read latency reached
**10.001 ms**; the adverse run remains included.

Ordinary server TotalAlloc was 255.05–256.02 MiB/job; retained Go heap after untimed
GC was 61.93–62.10 MiB; sampled RSS peaks were 172.38–200.09 MiB. Client B/op was
281.89–282.49 million bytes per whole invocation, not server B/request. Each run
retained 512000 bytes of operation backing storage and 393216 bytes of delivery
backing storage, excluding strings/maps. The server sampler retains counters only,
so elapsed duration does not retain more sampler history. Diagnostic buffers reserve
an additional **4 MiB of static storage per process**, not Go heap allocation; their
footprint must be considered in RSS comparisons. Dump allocations occur after
measured/retained capture. Peaks are 10ms samples, not exact maxima.

The host was the same unpinned Ryzen 9 9900X, 24 logical CPUs, Go 1.27.1, local
NVMe/ext4, warm filesystem/build caches. Recorded one-minute load rose from 0.76
to 1.51. Shared-host activity and roughly 16 observations beyond each run's read
p99 limit inference. These are experimental limits, not a product acceptance budget.

## Same-request attribution and instrumentation overhead

The traced path is: raw benchmark Send → protocol client request encoding/write →
server dispatch goroutine → declared-query handler → CallQuery authorization,
visibility, committed-state query/result preparation and detached copy → row encoding
→ response envelope encoding → outbound queue → WebSocket writer → client frame
receipt/outer decoding → protocol pending queue → benchmark reader/reply channel →
row decoding → exhaustive comparison. Ticket 100's declared SQL is `WHERE id = 100`;
this query path does not use the reducer adapter targeted by PERF-03.

All **6252 diagnostic reads** matched connection ID plus request ID, with all 16
client/server probe stages present exactly once. There were zero dropped events,
identity-parser failures, missing stages, duplicate stages or negative disjoint
intervals. Raw timestamps contain no tokens, headers or application payloads.
Each process uses a fixed 65536-slot buffer and emits after its interval.

The frozen primary cohort contains the **16 reads at/above each diagnostic run's
own dispatch p99**. Full component distributions for all reads, the >=p95 cohort
and the primary cohort are in [correlation.json](correlation.json). Human-readable
[component distributions](components.md) and [12 complete representative timelines](timelines.md)
retain the first, median and maximum request in each primary cohort. No independent
percentiles were subtracted or summed.

| Diagnostic cell | Socket write start → client frame receipt: median / max within slow cohort, ms | Share of those same reads' total time | Reads where that interval is largest |
|---|---:|---:|---:|
| 1000/even | 0.228 / 1.546 | 43.5% | 9/16 |
| 4000/burst | 0.503 / 6.389 | 37.1% | 4/16 |
| 4000/even | 0.473 / 1.707 | 46.7% | 9/16 |
| 1000/burst | 2.785 / 2.974 | 70.0% | 11/16 |

**Established within the diagnostic runs:** socket-write start through full client
frame receipt is the largest interval in 33/64 slow reads. Server socket-write
calls themselves took only 0.019–0.043 ms at their respective slow-cohort maxima.
The remaining elapsed gap often persists after the server's Write call returns.
This bounds much of the delay outside the observed handler and ordinary outbound
queue, but does not distinguish transport, kernel/WebSocket buffering or client
reader scheduling.

Other slow reads have different paths. In 4000/burst, client outer decoding reached
3.562 ms, row decoding 2.358 ms and pending-queue waiting 1.383 ms within the same
slow cohort. These intervals include possible goroutine suspension; they are not
CPU-time measurements or proof of inefficient decoding. Server outbound queueing
was the largest interval in only 3/64 slow reads, although its 1000/burst maximum
was 1.125 ms. Query preparation and pre-handler delay also dominate individual reads.
The worst diagnostic read, **9.270 ms**, remains in the report even though that
run's p99 improved; 6.389 ms lay between write start and frame receipt and another
1.371 ms in the client pending queue.

Clock handling uses same-process monotonic differences and the shared host wall
clock only at cross-process boundaries. Twenty clock brackets before and after
each run were compatible with zero host-clock offset; their tightest compatible
bounds across cells stayed within approximately -9.53 to +10.90 microseconds.
Per-event wall/monotonic pairing skew reached 23.750 microseconds in the client and
21.010 in the server (44.760 combined observed mapping spread). Component closure
residuals reached 23.750 microseconds and are retained. These are observed
uncertainties, not precision guarantees. **65 reads had client receipt before the
server write-completion timestamp**, down to -0.183 ms; those signed overlaps remain
in the data. A socket write is never treated as a receipt acknowledgment.

| Cell | Ordinary dispatch p99, ms | Diagnostic p99, ms | Diagnostic difference vs ordinary runs |
|---|---:|---:|---:|
| 1000/even | 0.715–0.741 | 0.454 | -38.7% to -36.5% |
| 4000/burst | 1.702–2.109 | 1.251 | -40.7% to -26.5% |
| 4000/even | 1.884–2.873 | 1.175 | -59.1% to -37.7% |
| 1000/burst | 0.973–1.021 | 1.542 | +51.0% to +58.5% |

[Overhead comparisons](overhead.json) include scheduled tails, achieved rate,
backlog, allocation, retained heap and RSS; [raw benchmark metrics](raw-benchmark-metrics.json)
retain client B/op/allocs and maxima. Instrumentation materially perturbed the tails;
negative differences are not performance gains to adopt. The sandwich controls
expose variability but do not isolate probe cost from host/GC/scheduling variation.
Attribution is established for the instrumented requests; transferring precise
shares to ordinary requests is **suggestive**, not established.

**Still unknown:** the transport/buffering/scheduler split in the large residual;
the causal role of GC, locks, fanout, durability or snapshots; and the earlier
original-versus-combined deterioration. Current-only observations cannot prove that
historical cause. No aggregate profiling was used to explain individual requests.

## Correctness and stop decisions

All 12 runs passed exact recovered-state and accepted-outcome checks:
**38400 operations, 19644 accepted writes, 18756 reads, and 157152 complete ordered
deliveries**. There were zero request errors, timeouts, rejections, unresolved write
outcomes or runtime/application correctness failures. Every project client received
the same complete ordered stream. All original tables, changed ticket values,
new audit values/IDs and allocator/physical-ID bounds were checked after shutdown.
[Independent analysis checks](analysis-check.json) also reconstruct schedules,
counts, identities, percentiles and delivery membership from raw observations.
No failing runtime reproduction or outcome reconciliation was needed. Final harness
review found that `Before.TotalAlloc=100` with a missing final memory sample could
wrap an unsigned delta; the delivered reporter emits -1, covered by the accounting
test. Resource cancellation before 90s now remains an error without becoming a
timeout. Neither case occurred in the matrix. Failure capture/reconciliation remains
in the maintained harness.

Smallest next actions, ranked by evidence:

1. Use scheduled-arrival reporting for offered-load decisions and specify an actual
   product arrival pattern/latency budget. This study already demonstrates why
   dispatch-only p99 hides backlog; no runtime change is needed for that conclusion.
2. Only if the remaining delay matters against that budget, separately authorize a
   narrow client WebSocket/socket read-readiness/return measurement correlated with
   these frame-receipt timestamps. That is the smallest next boundary needed to
   distinguish buffered/waiting bytes from client reader scheduling. The present
   8+4 invocation budget is exhausted; no additional profiling or cell was added.
3. Do not select an optimization from these tails. Keep the CallQuery copy,
   PERF-03 iteration changes and PERF-07 startup work deferred. Preserve segment
   defaults, append validation, compaction policy, native audit flags, buffering,
   snapshots, durability and resource limits. Do not repeat the settled historical
   comparison or completed slow-consumer study.

## Reproduction and artifacts

Supported maintained command (ordinary load, fresh output directory):

```bash
rtk proxy env CANARY_CAPACITY_MODE=scheduled-arrival CANARY_CAPACITY_RATE=4000 CANARY_CAPACITY_SHAPE=burst CANARY_CAPACITY_COUNT=2 scripts/measure-canary-capacity /tmp/shunter-arrivals-new
```

To reconstruct **exact measured sources/fixtures** without touching either original
working tree, run from this evidence directory:

```bash
rtk proxy python3 reproduce.py /tmp/shunter-response-new /home/gernsback/source/shunter /home/gernsback/source/opsboard-canary
# Optional new experiment, serially; reconstruction itself runs no workload:
rtk proxy env RESPONSE_WORK_ROOT=/tmp/shunter-response-new python3 /tmp/shunter-response-new/results/run.py
rtk proxy python3 /tmp/shunter-response-new/results/analyze.py
```

`reproduce.py` verifies every frozen byte before the two documented Go module-path
relocations. It uses the preserved fixture archive, never an old temporary path.
Final maintained files are also archived in `final-maintained-files.tar.gz`; the
closed-loop and failure-reporting review changes are retained separately from
measured sources. Recompute existing evidence without workloads:

```bash
rtk proxy python3 analyze.py
rtk proxy python3 check.py
```

Source archives, fixture archive/manifests, frozen contract/matrix, maintained and
isolated instrumentation patches, exact command environments, host observations,
all raw results/errors, overhead comparisons, checks and derived timelines are
preserved here. These are development investigation artifacts, not release evidence.
