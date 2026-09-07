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
checkout. The runner archives its committed `HEAD`, copies the measurement
test into that temporary module, points its Go dependency at the current
Shunter checkout, and uses Go 1.27.1. It also extends the older canary's test
signing key to meet the current HS256 minimum. Neither source checkout is
modified. `host.txt` records revisions, settings, hardware, and input hashes;
`seed.txt` records fixture sizes; `raw.txt` retains all benchmark samples.

The fixed matrix uses 1,024/8,192 additional tickets, 8/32 authenticated
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
