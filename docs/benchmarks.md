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
