# Dependency Considerations

Last reviewed: 2026-05-01

This note captures adopted dependencies, dependency suggestions, and explicit
rejections from the dependency scan of the current Shunter codebase. Candidate
sections are not an implementation plan and do not add anything to `go.mod` by
themselves.

Current repo context:

- Shunter is intentionally dependency-light today.
- Direct runtime dependencies are currently limited to `github.com/coder/websocket`,
  `github.com/golang-jwt/jwt/v5`, `github.com/prometheus/client_golang`,
  and `lukechampine.com/blake3`.
- Direct test dependencies now include `github.com/google/go-cmp v0.6.0`,
  `github.com/prometheus/client_model v0.6.2`, `go.uber.org/goleak v1.3.0`,
  and `pgregory.net/rapid v1.2.0`.
- Pinned Go tool dependencies now include
  `honnef.co/go/tools/cmd/staticcheck v0.7.0`.
- `github.com/coder/websocket` is replaced with the local Shunter fork
  `github.com/ponchione/websocket v1.8.14-shunter.1`.
- The broad suite passed after adopting `goleak` with:

```bash
rtk go test ./... -count=1
# Go test: 2387 passed in 11 packages
```

## Adopted Runtime Dependencies

### `github.com/coder/websocket`

Used for WebSocket transport in the `protocol` package.

Notes:

- The dependency is replaced with the local Shunter fork
  `github.com/ponchione/websocket v1.8.14-shunter.1`.
- Keep protocol code on the context-aware API shape.
- Do not add a second WebSocket library without a concrete transport
  requirement.

Docs: https://pkg.go.dev/github.com/coder/websocket

### `github.com/prometheus/client_golang`

Used by `observability/prometheus` to adapt Shunter's fixed metrics model to
Prometheus collectors and an HTTP metrics handler.

Notes:

- Prometheus stays outside the root `shunter` package.
- The root package exposes Shunter-owned metrics interfaces; the Prometheus
  package is an optional adapter.
- Do not use default global Prometheus registration from Shunter-owned code.

Docs: https://pkg.go.dev/github.com/prometheus/client_golang/prometheus

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

### `github.com/google/go-cmp/cmp`

Added as a direct test dependency for readable structured diffs.

Notes:

- Use `cmp.Diff` for nested structs, slices, maps, protocol messages,
  schema descriptions, subscription updates, and recovery state where raw
  equality failures are hard to diagnose.
- Keep simple scalar and single-field checks as direct comparisons.
- Use semantic comparers for Shunter domain values such as `types.Value` and
  `types.ProductValue`; their `Equal` methods express the intended semantics
  better than comparing implementation details.
- Do not add broader assertion frameworks.

Docs: https://pkg.go.dev/github.com/google/go-cmp/cmp

### `pgregory.net/rapid`

Added as a direct test dependency for shrinkable property and state-model
coverage over Shunter-owned invariants.

Enabled in:

- `bsatn`
- `query/sql`
- `store`
- `commitlog`

Notes:

- Keep Rapid imports in `_test.go` files only.
- Prefer package-local generators while the generated data is package-specific.
- Keep default check counts in normal test runs; use higher counts only for
  manual stress runs.

Docs: https://pkg.go.dev/pgregory.net/rapid

## Adopted Tool Dependencies

### `honnef.co/go/tools/cmd/staticcheck`

Pinned as a Go tool dependency at `v0.7.0`.

Run it with:

```bash
rtk go tool staticcheck ./...
```

Notes:

- The tool is pinned in `go.mod`; no `tools.go` file is needed with this Go
  toolchain.
- Use it for cleanup/static-analysis visibility.
- Staticcheck is expected to pass; record unrelated failures instead of fixing
  them in unrelated slices.

## Strong Candidates

These are the highest-value additions to consider first.

### `golang.org/x/sync/errgroup`

Use selectively for related goroutine groups that need error propagation and
context cancellation.

Why it fits:

- Some lifecycle paths currently coordinate goroutines manually with channels,
  `sync.WaitGroup`, and context cancellation.
- It may simplify serving/lifecycle supervision where multiple goroutines are
  part of one task and the first error should cancel the rest.

Candidate surfaces:

- `network.go`
- `lifecycle.go`
- protocol supervision / dispatch code

Do not use it as a blanket replacement for every `WaitGroup`.

Docs: https://pkg.go.dev/golang.org/x/sync/errgroup

## Later Candidates

These are plausible, but should wait for a concrete slice.

### `github.com/jonboulle/clockwork`

Use only if timing-heavy tests keep growing or timing flake becomes a recurring
cost.

Why it might fit:

- Several tests still exercise timer-driven scheduler, keepalive, protocol, and
  runtime gauntlet behavior where a fake clock could reduce wall-clock time.
- A fake clock could make these tests faster and less flaky.

Why to wait:

- It requires injecting a clock interface into production code.
- That is worthwhile only when touching timing-heavy code anyway.

Docs: https://pkg.go.dev/github.com/jonboulle/clockwork

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

- SPEC-005 reserves brotli as recognized-but-unsupported.
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
