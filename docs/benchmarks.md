# Shunter Benchmark Workflow

Use Shunter benchmarks to compare narrow before/after changes with repeatable
data. Keep benchmark fixtures deterministic and package-local unless a helper
is clearly useful across multiple benchmark files.

## Baseline Comparison

Run the smallest package and benchmark pattern that covers the path under
change:

```bash
go test -run '^$' -bench '<pattern>' -benchmem -count=10 ./package > /tmp/before.txt
go test -run '^$' -bench '<pattern>' -benchmem -count=10 ./package > /tmp/after.txt
benchstat /tmp/before.txt /tmp/after.txt
```

Use `-count=10` or higher for comparisons because a single run is too exposed
to scheduler, CPU frequency, GC, and background-process noise. `benchstat`
needs repeated samples to report useful confidence intervals and to avoid
treating normal local variance as a regression or win.

Run benchmark commands with raw `go test -bench`, not `rtk go test -bench`.
RTK is the normal shell wrapper for this repo, but benchmark output is the
exception: the raw `Benchmark... ns/op B/op allocs/op` lines must be preserved
for `benchstat`, PR review, and release evidence.

## Choosing Scope

Start with a package and benchmark regex that matches the code being changed,
for example:

```bash
go test -run '^$' -bench 'BenchmarkOrderWindowRows' -benchmem -count=10 ./subscription
```

Prefer narrow package benchmarks before broad runs. Expand to a wider pattern,
multiple packages, or `./...` only when the change touches shared behavior,
cross-package contracts, or a release measurement sweep.

## Benchmark Quality

- Use deterministic inputs and stable seeds.
- Call `b.ReportAllocs()` for performance-sensitive paths.
- Build fixtures before the timed loop and call `b.ResetTimer()` after setup.
- Do not log in timed loops.
- Do not use network or filesystem work unless the benchmark is specifically
  measuring those systems.
- Avoid sleeps, timers, and wall-clock polling in timed loops.
- Avoid hidden global state leaks between sub-benchmarks or benchmark runs.
- Keep helpers local to one benchmark file unless more than one package or file
  genuinely benefits from sharing them.

## PR Reporting

For PRs that change performance-sensitive code, report:

- the exact `go test -bench` command and package pattern;
- the host/OS, Go version, and CPU when results matter for review;
- the before and after commit or branch being compared;
- the `benchstat` summary, including `ns/op`, `B/op`, and `allocs/op`;
- a short note explaining whether differences are expected, material, or
  within noise.

Do not present a one-run benchmark as proof of a performance change. If a broad
run is too expensive, include the narrow `-count=10` comparison and explain the
coverage boundary.

## External Canary Capacity Study

On Linux, with `opsboard-canary` checked out beside Shunter:

```bash
rtk proxy scripts/measure-canary-capacity /tmp/shunter-canary-capacity
```

The output directory must be new. `CANARY_CHECKOUT` selects another canary
checkout. The runner copies its nonignored working-tree source (including
untracked application files) and the measurement tests into a temporary module,
records source/fixture hashes, and points its Go dependency at the current
Shunter checkout, and uses Go 1.27.1. It also extends the older canary's test
signing key to meet the current HS256 minimum. Neither source checkout is
modified. `host.txt` records revisions, settings, hardware, and input hashes;
`seed.txt` records fixture sizes; `raw.txt` retains all benchmark samples.

The default `CANARY_CAPACITY_MODE=closed-loop` throughput matrix uses
1,024/8,192 additional tickets, 8/32 authenticated
WebSocket clients, and balanced 50% write / skewed 80% write workloads. Each
sample starts a fresh server process from the seeded fixture, runs for ten
seconds with two concurrent snapshots, and checks all expected deltas. Ten
samples per row and ten offline backup/restore samples per fixture use:

```bash
# From the staged canary module, with CANARY_CAPACITY_FIXTURES set by the runner:
go test ./internal/workflows -run '^$' -bench '^BenchmarkCanaryCapacity' -benchtime=1x -count=10 -timeout=45m -benchmem
```

Use `CANARY_CAPACITY_COUNT=1 CANARY_CAPACITY_DURATION=1s` only for a harness
smoke check. Sustained-run timers, loopback traffic, and filesystem work are
intentional here. The custom metrics describe server RSS/heap, operation
latency percentiles, throughput, and snapshot/backup/restore duration;
`B/op` and `allocs/op` describe the benchmark's client process. Use repeated
rows with `benchstat` as above for a comparison. A single study records an
advisory envelope, with its workload and limits in
[performance envelopes](performance-envelopes.md#2026-09-07-external-canary-capacity).

### Scheduled arrivals

Use a distinct mode for offered load; historical closed-loop names keep their
original boundaries. For example (use a fresh output path):

```bash
rtk proxy env CANARY_CAPACITY_MODE=scheduled-arrival CANARY_CAPACITY_RATE=4000 CANARY_CAPACITY_SHAPE=burst CANARY_CAPACITY_COUNT=2 scripts/measure-canary-capacity /tmp/shunter-arrivals
```

`CANARY_CAPACITY_FIXTURES` can select existing offline fixtures; the runner hashes
all bytes instead of regenerating them. Each `sample-N/result.json` retains every
operation and delivery; `sample-N/raw.txt` and the combined `raw.txt` retain custom
benchmark metrics. From an already staged canary, with the same environment:

```bash
go test ./internal/workflows -run '^TestCanaryArrival' -count=1
go test ./internal/workflows -run '^$' -bench '^BenchmarkCanaryScheduledArrival$' -benchtime=1x -count=1 -timeout=170s -benchmem
```

This mode uses 32 clients, 100 operations/client (override with
`CANARY_CAPACITY_PER_CLIENT`), 8,192 added tickets, PCG seed 20260907, 50% writes,
eight subscribers/project, and two snapshots at offered-window thirds. Even due
offsets are `(ordinal*32+client)/rate` seconds; burst offsets are `ordinal*32/rate`.
One common origin and one active request/client preserve all choices and order.
A response wait never shifts the schedule. The complete offered window is
`3200/rate` seconds, including its final interarrival gap. Successful completion
requires every reply, complete ordered delivery drain, and exact recovered state
and accepted audit checks. Individual requests have 30s deadlines; completion/drain
has a 90s cap, with a 180s outer-process cap and a sampled 1-GiB server RSS stop.
These bounds are experimental limits, not product latency budgets.

The report separates offered, dispatch, reply-completion and successful counts.
Dispatch/completion rates use the larger of the offered window and the final
corresponding timestamp; in-window counts/rates and after-window counts are
explicit. A completion includes row decoding and validation, including a failed
response. A timeout terminates the client wait but is not a response completion.
Backlog is reconstructed from every intended arrival, dispatch and terminal wait:
awaiting dispatch plus in-flight waits equals outstanding work. Resource cancellation before the phase deadline counts as an error, not a timeout.
Unknown writes
are counted separately after a timeout; failed jobs preserve recovered data and
reconcile attempted writes against audit rows. No timeout implies rejection.

Read dispatch/scheduled p95/p99 use nearest rank in milliseconds, per run.
`read-response` ends at the benchmark reader after outer decoding and protocol
routing; it is not raw socket receipt. `read-handoff`, `read-row-decode`, and
`read-validation` separate the reply channel, application decoding, and exhaustive
row comparison. Diagnostic-only instrumentation can split the earlier boundaries.
Raw operation timestamps share one in-process monotonic phase origin. Server host
wire durations are microseconds, converted to milliseconds by dividing by 1000.

Client `B/op` is allocation per entire benchmark invocation. Server `TotalAlloc`
is a separate process delta from setup completion through drain (-1 if the final
capture is unavailable); retained heap is
after untimed GC. RSS/heap peaks are 10ms samples, not exact maxima. The server
sampler retains counters only. Client operation/delivery buffer metrics report
backing-array bytes (excluding strings/maps); those buffers grow with operation
count. Diagnostic buffers must be accounted separately. No product tail acceptance
budget or statistical confidence is implied by two repetitions.
