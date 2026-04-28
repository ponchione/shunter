# Dependency Considerations

Last reviewed: 2026-04-28

This note captures dependency suggestions and explicit rejections from the
dependency scan of the current Shunter codebase. It is not an implementation
plan and does not add anything to `go.mod` by itself.

Current repo context:

- Shunter is intentionally dependency-light today.
- Direct runtime dependencies are currently limited to `github.com/coder/websocket`,
  `github.com/golang-jwt/jwt/v5`, and `lukechampine.com/blake3`.
- Direct test dependencies now include `go.uber.org/goleak v1.3.0`.
- `github.com/coder/websocket` is replaced with the local Shunter fork
  `github.com/ponchione/websocket v1.8.14-shunter.1`.
- The broad suite passed after adopting `goleak` with:

```bash
rtk go test ./... -count=1
# Go test: 2387 passed in 11 packages
```

## Adopted Test Dependencies

### `go.uber.org/goleak`

Added as a test-only dependency for goroutine leak detection.

Enabled in:

- root runtime tests
- `protocol`
- `executor`
- `subscription`
- `commitlog`

Notes:

- Use package-level `TestMain` with `goleak.VerifyTestMain(m)`.
- Do not use per-test `VerifyNone` in packages with parallel tests; upstream
  documents that `VerifyNone` is incompatible with `t.Parallel`.
- Prefer explicit test cleanup over `IgnoreTopFunction` /
  `IgnoreAnyFunction`. Use ignores only for proven benign external/library
  goroutines, with a short comment.

Docs: https://pkg.go.dev/go.uber.org/goleak

## Strong Candidates

These are the highest-value additions to consider first.

### `github.com/google/go-cmp/cmp`

Use as a test dependency for readable structured diffs.

Why it fits:

- The suite compares nested values across protocol messages, row payloads,
  schema descriptions, subscription updates, and recovery state.
- `cmp.Diff` would make failures easier to diagnose than raw `reflect.DeepEqual`
  failures or custom mismatch text.
- It should stay test-only.

Docs: https://pkg.go.dev/github.com/google/go-cmp/cmp

### `pgregory.net/rapid`

Use as a property-based and state-machine testing dependency.

Why it fits:

- Shunter has several custom invariant-heavy surfaces:
  - `bsatn` encode/decode round trips
  - `store` transaction/index behavior
  - `commitlog` replay/recovery invariants
  - `query/sql` parser and coercion edge cases
- Rapid supports shrinking, complex generated data, and state-machine tests.
- It has no non-stdlib dependencies of its own.

Best first packages:

- `bsatn`
- `store`
- `commitlog`
- `query/sql`

Docs: https://pkg.go.dev/pgregory.net/rapid

### `honnef.co/go/tools/cmd/staticcheck`

Pin as a tool, not as a runtime library.

Why it fits:

- `TECH-DEBT.md` already calls out dead-code and test-label cleanup that `go vet`
  does not cover well.
- The repo has enough concurrency, tests, and stale-audit cleanup that an
  analysis pass would likely find useful maintenance issues.
- Prefer pinning a released tool version instead of relying on an ambient
  developer install.

Docs: https://pkg.go.dev/honnef.co/go/tools

### `golang.org/x/sync/errgroup`

Use selectively for related goroutine groups that need error propagation and
context cancellation.

Why it fits:

- Some lifecycle paths currently coordinate goroutines manually with channels,
  `sync.WaitGroup`, and context cancellation.
- It may simplify serving/lifecycle supervision where multiple goroutines are
  part of one task and the first error should cancel the rest.

Candidate surfaces:

- `runtime_network.go`
- `runtime_lifecycle.go`
- protocol supervision / dispatch code

Do not use it as a blanket replacement for every `WaitGroup`.

Docs: https://pkg.go.dev/golang.org/x/sync/errgroup

## Later Candidates

These are plausible, but should wait for a concrete slice.

### `github.com/jonboulle/clockwork`

Use only if timing-heavy tests keep growing or timing flake becomes a recurring
cost.

Why it might fit:

- There are many tests using fixed `time.Sleep` / `time.After` windows,
  especially around scheduler, keepalive, fanout worker, protocol backpressure,
  and runtime gauntlet behavior.
- A fake clock could make these tests faster and less flaky.

Why to wait:

- It requires injecting a clock interface into production code.
- That is worthwhile only when touching timing-heavy code anyway.

Docs: https://pkg.go.dev/github.com/jonboulle/clockwork

### `github.com/prometheus/client_golang`

Consider when Shunter needs operator-facing runtime metrics.

Good metric candidates:

- active connections
- outbound queue depth / drops
- fanout send failures
- transaction latency
- durability queue depth
- recovery result/status
- scheduler firing counts

Why it may fit:

- Prometheus is a direct fit for self-hosted operator visibility.
- It is simpler than full distributed tracing if the first need is runtime
  health and counters/histograms.

Docs: https://pkg.go.dev/github.com/prometheus/client_golang/prometheus

### `go.opentelemetry.io/otel`

Consider when Shunter needs tracing or broad observability integration.

Why it may fit:

- Useful for following a reducer call through protocol admission, executor,
  store commit, durability, subscription evaluation, and fanout.
- Better fit once hosted-runtime users need integrations with external
  observability stacks.

Why to wait:

- It is heavier than Prometheus metrics.
- Instrumentation shape should follow a stable runtime API.

Docs: https://pkg.go.dev/go.opentelemetry.io/otel

### `github.com/andybalholm/brotli`

Consider only if Shunter clients need brotli compression.

Why it may fit:

- OI-001 notes brotli is recognized-but-unsupported.
- This package provides Go brotli reader/writer support.

Why to wait:

- Brotli is not currently a Shunter product requirement.
- Adding it just because the protocol recognizes the concept would increase
  surface area without clear user value.

Docs: https://pkg.go.dev/github.com/andybalholm/brotli

## Rejected For Now

These should not be added without a fresh, concrete requirement.

### Full SQL parser packages

Examples: Vitess SQL parser or similar broad SQL parsers.

Reason:

- `query/sql` is intentionally narrow and Shunter-owned.
- A broad parser would likely admit or represent much more SQL surface than
  Shunter wants to support.
- The hard part here is Shunter's accepted/rejected contract and one-off vs
  subscription semantics, not tokenizing arbitrary SQL.

### Embedded storage engines

Examples: bbolt, Pebble, Badger.

Reason:

- Shunter's custom in-memory store, changeset model, commit log, snapshot, and
  recovery behavior are core product/runtime logic.
- Replacing that with an embedded KV/storage engine would be an architectural
  rewrite, not a dependency cleanup.
- Consider only if the product direction changes toward using an externalized
  storage substrate.

### Assertion frameworks

Examples: testify-style assertion suites.

Reason:

- Standard `testing` plus `go-cmp` is enough for this repo.
- Assertion frameworks add style churn without solving the main test-readability
  problem.

### Broad logging framework

Examples: zap/logrus/zerolog as a default dependency.

Reason:

- The standard library now has `log/slog`.
- If Shunter needs structured logging, start with `slog` and a narrow runtime
  logging interface before taking a larger logging dependency.

## Default Stance

Prefer additions that are:

- test-only or tool-only first
- targeted at existing known risk
- easy to remove if the slice changes
- compatible with Shunter's self-hosted, Go-native product direction

Avoid dependencies that:

- widen Shunter's protocol or SQL contract by accident
- replace core runtime/storage semantics prematurely
- create framework gravity before the hosted-runtime API stabilizes
