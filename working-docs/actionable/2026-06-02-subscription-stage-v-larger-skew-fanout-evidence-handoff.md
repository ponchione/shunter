# Subscription Stage V Larger Skew/Fanout Evidence Handoff - 2026-06-02

Read and execute this slice next time. Make a commit. Delete this original
handoff file when done. Report back with the commit, validation, benchmark
evidence gathered, and any dimensions intentionally left as known gaps.

## Startup Reading

Follow repo startup rules:

1. `RTK.md`
2. `working-docs/actionable/subscription-evidence-matrix.md`
3. Narrow sections only:
   - `docs/performance-envelopes.md` Stage T, Stage U, and current-read
     subscription known-gap sections
   - `subscription/bench_test.go` benchmark families and helpers named below

Do not open broad specs unless live code and the narrow docs cannot answer a
contract question.

## Current State

- Last completed slice commit:
  `78bce3f67812649ff5b65a47e20fb4fe229577df`
- Stage U added bounded `cross3_rows_72` Cartesian rows for table-shaped
  projection, `COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and
  `SUM(column)`.
- Runtime semantics changed in Stage U: no.
- Default multi-way join guardrails changed in Stage U: no.
- Remaining multi-way evidence gaps include larger Cartesian fixtures beyond
  the bounded 72-row cross shape, larger skew/fanout distributions beyond the
  bounded 48x48 row, relation counts beyond the bounded 5-relation chain
  fixture, larger aggregate-function self-alias distributions beyond the
  bounded `self_alias3` fixture, and workload-derived application
  distributions.

## Goal

Close one narrow skew/fanout evidence gap by inventorying the existing
`hot_key_48x48` coverage and adding one larger bounded hot-key fixture only if
it stays local-review-sized.

Prefer the smallest useful slice:

1. Start from the existing `hot_key_48x48` rows in
   `BenchmarkMultiWayLiveJoinSelectivity` and
   `BenchmarkMultiWayLiveJoinAggregateFunctions`.
2. Smoke the existing `hot_key_48x48` table-shaped and aggregate-function rows
   first.
3. Add a single larger skew/fanout size, preferably `hot_key_56x56`, for:
   - table-shaped projection in `BenchmarkMultiWayLiveJoinSelectivity`
   - aggregate-function rows in `BenchmarkMultiWayLiveJoinAggregateFunctions`
     only if smoke runs stay clearly cheap
4. If `hot_key_56x56` is too expensive or noisy for focused `-count=10` runs,
   do not try larger skew/fanout sizes, Cartesian sizes, relation counts,
   self-alias shapes, or workload-derived fixtures in this slice. Update the
   matrix with the concrete finding.

## Scope

Allowed:

- Add or refine sub-benchmarks in `subscription/bench_test.go` for existing
  supported 3-relation skew/fanout multi-way live-join shapes.
- Update `docs/performance-envelopes.md` with representative rows and honest
  known gaps.
- Update `working-docs/actionable/subscription-evidence-matrix.md` with
  Stage V status and remaining evidence gaps.
- Save raw benchmark output under
  `working-docs/release-evidence/2026-06-02-subscription-stage-v/` if the
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

- `BenchmarkMultiWayLiveJoinSelectivity`
- `BenchmarkMultiWayLiveJoinAggregateFunctions`
- `benchmarkMultiWayLiveJoinDelta`
- `benchmarkMultiJoinSelectivityCommitted`
- `benchmarkMultiJoinInsertChangeset`
- `countStarAggregate`
- `countMultiJoinRIDAggregate`
- `countDistinctMultiJoinTIDAggregate`
- `sumMultiJoinRIDAggregate`

Then compare against `docs/performance-envelopes.md` and the evidence matrix.
Only add the larger skew/fanout case if the existing `hot_key_48x48` rows
remain bounded and reviewable in smoke runs.

## Likely Implementation

Keep benchmark names explicit enough for `benchstat`, for example:

- `MultiWayLiveJoinSelectivity/rows_128/hot_key_56x56`
- `MultiWayLiveJoinAggregateFunctions/hot_key_56x56/count_star`
- `MultiWayLiveJoinAggregateFunctions/hot_key_56x56/count_column`
- `MultiWayLiveJoinAggregateFunctions/hot_key_56x56/count_distinct`
- `MultiWayLiveJoinAggregateFunctions/hot_key_56x56/sum`

Prefer existing fixture builders over new helper layers:

- add one `{name: "hot_key_56x56", hotFanout: 56}` row to
  `BenchmarkMultiWayLiveJoinSelectivity`
- add one `{name: "hot_key_56x56", hotFanout: 56}` row to the skew loop in
  `BenchmarkMultiWayLiveJoinAggregateFunctions`
- keep the existing changed row pattern:
  `types.ProductValue{types.NewUint64(9000), types.NewUint64(1)}`
- keep the existing committed fixture pattern:
  `benchmarkMultiJoinSelectivityCommitted(size, skew.hotFanout, nil)` before
  `benchmarkMultiJoinSelectivityCommitted(size, skew.hotFanout,
  []types.ProductValue{changed})`

The current skew/fanout fixture uses a 128-row, 3-relation chain and one
changed endpoint row. A `hot_key_56x56` endpoint insert matches key `1`, the
hot key shared by 56 rows on each upstream relation, while keeping the fixture
bounded and explicit. Document why any published rows remain
local-review-sized.

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
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinSelectivity$/^rows_128$/^hot_key_(48x48|56x56)$' -benchmem -count=10 ./subscription
go test -run '^$' -bench '^BenchmarkMultiWayLiveJoinAggregateFunctions$/^hot_key_(48x48|56x56)$' -benchmem -count=10 ./subscription
```

Use `benchstat` if publishing benchmark rows. If `benchstat` is not available
as a local tool, prefer:

```bash
rtk go run golang.org/x/perf/cmd/benchstat@latest <raw-output-file>
```

Record the exact commands and raw output paths in the performance envelope.

## Completion Criteria

- Existing skew/fanout benchmark/docs coverage has been inventoried.
- One bounded larger skew/fanout gap is closed, or the matrix records why the
  shape is not suitable for this local slice.
- Published evidence in `docs/performance-envelopes.md` is backed by saved raw
  output when applicable.
- Runtime semantics and default guardrails remain unchanged.
- Validation passes.
- This handoff file is deleted.
- A commit is created.
