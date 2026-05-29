# Subscription Stage U Larger Cartesian Evidence Handoff - 2026-05-29

Read and execute this slice next time. Make a commit. Delete this original
handoff file when done. Report back with the commit, validation, benchmark
evidence gathered, and any dimensions intentionally left as known gaps.

## Startup Reading

Follow repo startup rules:

1. `RTK.md`
2. `working-docs/actionable/subscription-evidence-matrix.md`
3. Narrow sections only:
   - `docs/performance-envelopes.md` Stage S, Stage T, and current-read
     subscription known-gap sections
   - `subscription/bench_test.go` benchmark families and helpers named below

Do not open broad specs unless live code and the narrow docs cannot answer a
contract question.

## Current State

- Last completed slice commit:
  `411bb733b756f75b3a2362523f830ca507a8db3f`
- Stage T added bounded `hot_key_48x48` skew/fanout rows for table-shaped
  projection and aggregate-function rows for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Runtime semantics changed in Stage T: no.
- Default multi-way join guardrails changed in Stage T: no.
- Remaining multi-way evidence gaps include larger Cartesian fixtures beyond
  the bounded 64-row cross shape, larger skew/fanout distributions beyond the
  bounded 48x48 row, relation counts beyond the bounded 5-relation chain
  fixture, larger aggregate-function self-alias distributions beyond the
  bounded `self_alias3` fixture, and workload-derived application
  distributions.

## Goal

Close one narrow Cartesian evidence gap by inventorying the existing
`cross3_rows_64` coverage and adding one larger bounded Cartesian fixture only
if it stays local-review-sized.

Prefer the smallest useful slice:

1. Start from the existing `cross3_rows_64` rows in
   `BenchmarkMultiWayLiveJoinRelationShapes`,
   `BenchmarkMultiWayLiveJoinAggregateRelationShapes`, and
   `BenchmarkMultiWayLiveJoinAggregateFunctions`.
2. Smoke the existing `cross3_rows_64` table-shaped, `COUNT(*)`, and
   aggregate-function rows first.
3. Add a single larger Cartesian size, preferably `cross3_rows_72`, for:
   - table-shaped projection in `BenchmarkMultiWayLiveJoinRelationShapes`
   - `COUNT(*)` relation-shape row in
     `BenchmarkMultiWayLiveJoinAggregateRelationShapes`
   - aggregate-function rows in `BenchmarkMultiWayLiveJoinAggregateFunctions`
     only if smoke runs stay clearly cheap
4. If `cross3_rows_72` is too expensive or noisy for focused `-count=10` runs,
   do not try larger Cartesian sizes, skew/fanout sizes, relation counts,
   self-alias shapes, or workload-derived fixtures in this slice. Update the
   matrix with the concrete finding.

## Scope

Allowed:

- Add or refine sub-benchmarks in `subscription/bench_test.go` for existing
  supported 3-relation Cartesian multi-way live-join shapes.
- Update `docs/performance-envelopes.md` with representative rows and honest
  known gaps.
- Update `working-docs/actionable/subscription-evidence-matrix.md` with
  Stage U status and remaining evidence gaps.
- Save raw benchmark output under
  `working-docs/release-evidence/2026-05-29-subscription-stage-u/` if the
  evidence is being published into `docs/performance-envelopes.md`.

Not allowed without a strong, documented finding:

- Do not change runtime subscription semantics.
- Do not change default multi-way join guardrails.
- Do not add new SQL syntax, aggregate functions, grouped aggregates, or
  aggregate windows.
- Do not broaden aggregate validation or numeric-domain support.
- Do not add speculative app workload fixtures.
- Do not create a soak/load benchmark that is unsuitable for local review.
- Do not turn advisory benchmark rows into release gates.

## Inventory First

Before editing, inventory current coverage in `subscription/bench_test.go`:

- `BenchmarkMultiWayLiveJoinRelationShapes`
- `BenchmarkMultiWayLiveJoinAggregateRelationShapes`
- `BenchmarkMultiWayLiveJoinAggregateFunctions`
- `benchmarkMultiWayLiveJoinShape`
- `benchmarkMultiWayLiveJoinShapeAggregate`
- `benchmarkMultiWayLiveJoinDelta`
- `benchmarkMultiJoinCommitted`
- `benchmarkMultiJoinInsertChangeset`
- `benchmarkMultiJoinCross3Predicate`
- `countStarAggregate`
- `countMultiJoinRIDAggregate`
- `countDistinctMultiJoinTIDAggregate`
- `sumMultiJoinRIDAggregate`

Then compare against `docs/performance-envelopes.md` and the evidence matrix.
Only add the larger Cartesian case if the existing `cross3_rows_64` rows remain
bounded and reviewable in smoke runs.

## Likely Implementation

Keep benchmark names explicit enough for `benchstat`, for example:

- `MultiWayLiveJoinRelationShapes/cross3_rows_72`
- `MultiWayLiveJoinAggregateRelationShapes/cross3_rows_72/count`
- `MultiWayLiveJoinAggregateFunctions/cross3_rows_72/count_star`
- `MultiWayLiveJoinAggregateFunctions/cross3_rows_72/count_column`
- `MultiWayLiveJoinAggregateFunctions/cross3_rows_72/count_distinct`
- `MultiWayLiveJoinAggregateFunctions/cross3_rows_72/sum`

Prefer existing fixture builders over new helper layers:

- add one `cross3_rows_72` `b.Run` with `const crossSize = 72` to
  `BenchmarkMultiWayLiveJoinRelationShapes`
- add one `cross3_rows_72/count` `b.Run` with `const crossSize = 72` to
  `BenchmarkMultiWayLiveJoinAggregateRelationShapes`
- add one aggregate-function loop for `cross3_rows_72` with
  `const crossSize = 72` to `BenchmarkMultiWayLiveJoinAggregateFunctions`
- keep the existing changed row pattern:
  `types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}`
- keep the existing committed fixture pattern:
  `benchmarkMultiJoinCommitted(crossSize, false)` before
  `benchmarkMultiJoinCommitted(crossSize, true)`

The current Cartesian fixture uses a 3-relation cross shape and one changed
endpoint row. A `cross3_rows_72` endpoint insert emits a 72x72 Cartesian
fragment while keeping the fixture bounded and explicit. Document why any
published rows remain local-review-sized.

## Validation

Correctness after editing benchmarks/docs:

```bash
rtk go fmt ./subscription
rtk go test ./subscription
rtk git diff --check
```

Run `rtk go vet ./subscription` if any exported APIs, interfaces, or non-test
behavior changed. A benchmark-only/docs-only slice should not need staticcheck
unless the implementation touches shared runtime code.

Focused benchmark evidence, using raw `go test` rather than `rtk go test`:

```bash
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinRelationShapes$/^cross3_rows_(64|72)$' -benchmem -count=10 ./subscription
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateRelationShapes$/^cross3_rows_(64|72)$/^count$' -benchmem -count=10 ./subscription
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^cross3_rows_(64|72)$' -benchmem -count=10 ./subscription
```

Use `benchstat` if publishing benchmark rows. If `benchstat` is not available
as a local tool, prefer:

```bash
rtk go run golang.org/x/perf/cmd/benchstat@latest <raw-output-file>
```

Record the exact commands and raw output paths in the performance envelope.

## Completion Criteria

- Existing Cartesian benchmark/docs coverage has been inventoried.
- One bounded larger Cartesian gap is closed, or the matrix records why the
  shape is not suitable for this local slice.
- Published evidence in `docs/performance-envelopes.md` is backed by saved raw
  output when applicable.
- Runtime semantics and default guardrails remain unchanged.
- Validation passes.
- This handoff file is deleted.
- A commit is created.
