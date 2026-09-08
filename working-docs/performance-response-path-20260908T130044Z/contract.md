# Response-path development investigation contract

Frozen before measured workloads; source hashes and exact ordered invocations are
in `frozen-inputs.json` and `frozen-matrix.json`. Original working-tree source and
fixture identity are in `source-manifest.json` and `fixture-manifest.json`.
No runtime/application optimization, qualification or release activity is authorized.

## Questions and falsifiable predictions

1. If the residual read time accumulates in outbound delivery, the same slow reads
   will show long enqueue-to-dequeue or dequeue/write intervals.
2. If it accumulates in client processing, those reads will show long frame-receipt
   to outer-decode, pending queue, benchmark handoff or row-decode intervals.
3. If it precedes the response, request-write to handler-entry or query preparation
   will dominate the slow cohort; aggregate CPU/lock time alone is not attribution.
4. If neither observed interval accounts for it, the cross-process transport and
   scheduling residual remains unobserved. Socket write completion is not receipt.

The prior 100/s anchor is reused from the previous README/metrics. No 100/s workload,
overload/slow-consumer matrix, board-copy experiment, original/combined comparison,
startup, broad PERF-03/07 work or profiling is added.

## Frozen workload and order

Exactly eight ordinary and four instrumented invocations maximum. Cells are
1000/even, 4000/burst, 4000/even, 1000/burst, in that order; within each cell run
ordinary A, diagnostic, ordinary B. This sandwich balances diagnostic-before/after
comparisons within each cell; opposite rate order in the second pair limits a
simple host-time trend. There are no retries or replacement measured samples.

32 clients, 100 operations/client, 8192 added tickets (8194 total), identical
archived fixture bytes. PCG seed 20260907; stream client+1, `<50` selects writes:
1637 writes and 1563 reads/run. Even due=(ordinal*32+client)/rate seconds; burst
due=ordinal*32/rate. Offered windows are 3.2 and 0.8 seconds, including the final
gap. Per-client order, one request/client, close/reopen choices, FullUpdate replies,
eight subscriptions/project and snapshots at window thirds are unchanged.

All runs use the actual current source, including pre-existing uncommitted and
untracked PERF work, in detached worktrees. Ordinary/diagnostic implementations
are byte-matched except the archived instrumentation overlays and module-path
relocations. Fresh restored state and a fresh separate server process per run.
Warm filesystem/build caches, unpinned shared host, same Go toolchain/default
runtime settings. No concurrent task workload, build, test, profiling or heavy
analysis. Runner captures UTC, load, process counts/CPU/RSS, memory and filesystem.

Smoke 1: supported script, 1000/even, 10 operations/client, passed. Smoke 2:
diagnostic command omitted enabling environment and skipped (no workload); retained.
No more excluded smoke invocations. Race/validation commands are separate and do
not consume measured cells. No race workload is substituted for a measured run.

## Boundaries, correctness and stops

Intended operations are recorded before a common phase origin. Record dispatch,
request-write completion, benchmark reader receipt, reply handoff, row decode
start/end and final validation separately. All operation-relative times use
in-process monotonic differences and are milliseconds. `read-response` ends after
outer protocol decode and internal routing, not at raw frame receipt. Diagnostic
probes split those earlier boundaries. Host wire microseconds divide by 1000.

Counts/rates cover the complete offered window, dispatch and completed responses,
including after-window work. Completion means response through decode/validation;
timeout ends a client wait but is not a received completion or a rejection.
Outstanding means scheduled-but-not-dispatched plus active client waits. Unknown
write outcomes remain separately visible after waits end. Exact event sweeps
reconstruct queue/flight/outstanding peaks and counts at window end and deadline.
Nearest-rank p95/p99 are per-run, never pooled or subtracted across distributions.

Requests: 30s. Completion/drain: 90s from origin. Go test: 170s. Outer process:
180s; kill the process group on timeout. Server 10ms RSS sampler latches >1GiB;
client control polls every 100ms and cancels on the resource stop. Preserve failed
invocations and stop dependent cells on any correctness/resource/deadline failure.
A final control sample also checks the latched resource limit. Failed jobs preserve
raw outcomes and recovered data; writes are reconciled against audit rows after
shutdown. No timeout is counted as rejected without a server response.

Success requires all 3200 choices executed once in order, 1637 accepted writes,
complete original-table values, exact changed-ticket values, accepted audit values
and unique contiguous audit IDs, allocator high water and physical-ID bounds.
Every one of 13096 expected deliveries must drain, with complete identical ordered
streams for each project's eight clients and per-writer timestamp order. Exhaustive
read equality is separate from row decoding; complete recovery and delivery checks
run outside latency intervals where possible. Exactly two snapshots must succeed.

Client B/op is per invocation, excluding setup allocations. Operation/delivery
backing buffers are reported separately (string/map storage excluded). Server
TotalAlloc is a process delta through drain; retained heap follows untimed GC;
10ms RSS/heap peaks are samples. The ordinary server sampler retains counters only.
The diagnostic fixed buffer reserves 65536 slots per process, reported by sizeof;
no duration-growing trace history, payloads, tokens, or auth headers are logged.
Dump allocation happens after retained memory capture and after the workload.

## Diagnostic correlation, clocks and interpretation

Key = connection ID plus numeric request ID, unique per client operation. Capture
client request write start/end; server handler entry, query ready, row encoding,
envelope encode start/end, outbound enqueue/dequeue, socket write start/end; client
frame receipt, outer decode, routing entry, pending enqueue/dequeue. Maintained
operation fields provide intended arrival/dispatch/reply receipt/handoff/row decode/
validation. One phase-origin probe joins the client's monotonic timestamps exactly.
The probe reads only uncompressed response tag/identity bytes and does not decode
application payloads. Compression remains unnegotiated as in ordinary clients.

Each timestamp records wall UnixNS and duration from its own process's Go monotonic
origin. Same-process components subtract monotonic stamps. Cross-process boundaries
use the colocated host CLOCK_REALTIME mapping, never independent monotonic origins.
Twenty loopback clock bracket samples before/after quantify offset-compatible
intervals/RTT uncertainty; wall-minus-monotonic drift is checked for every event.
Capture placement and probe overhead still impose uncertainty. No synchronization
of independent monotonic origins is assumed. Negative write-done-to-receipt values
are retained as overlapping observations, not clamped to causal transport time.

Primary slow cohort is every successful read >= nearest-rank dispatch p99 of that
same diagnostic run (about 16 reads); secondary cohort is >=p95 (about 79). Freeze
this rule now. Report component distributions for all/primary/secondary cohorts,
and complete timelines for the first, median, and maximum read of the primary
cohort ordered by dispatch duration. Never subtract independent p99s, sum component
percentiles, or convert aggregate profile time into a per-request explanation.

Use a disjoint wall timeline from dispatch -> request-write-start -> handler-entry
-> query-ready -> rows-encoded -> envelope-start -> envelope-ready -> enqueue ->
dequeue -> socket-write-start -> frame-received -> outer-decoded -> route-entry ->
pending-enqueue -> pending-dequeue -> benchmark receipt -> handoff -> decode-start
-> decode-done -> validation. Socket-write duration is reported separately because
it can overlap frame receipt. The write-start-to-frame-received boundary includes
transport, kernel buffering and client scheduling; it cannot isolate them.

Every probe reports dropped events and correlation failures. Missing/duplicate or
out-of-order stage sets remain visible; do not silently omit affected slow reads.
Overhead is diagnostic vs both neighboring ordinary results, as values and ranges,
not a significance estimate. Only two ordinary repetitions and ~16 observations
beyond p99 do not establish a stable production tail. Product acceptance unspecified.

Stop after this bounded attribution, including an inconclusive boundary result.
Any future runtime optimization requires separate review/authorization. Preserve
all defaults, validation, durability, buffers, snapshots and application behavior.
