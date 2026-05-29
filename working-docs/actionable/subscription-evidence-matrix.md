# Subscription Evidence And Type/Index Matrix

Status: Stage H multi-way Cartesian evidence slice complete; remaining
items stay evidence backlog
Primary backlog items: `deferred-functionality-backlog.md` items 10, 11, 24,
and 31

## Purpose

Turn deferred subscription and type/index expansion into actionable evidence
work before changing semantics. The near-term goal is to measure and prove the
current supported surfaces under realistic shapes:

- maintained single-table ordered/windowed live views
- correctness-first multi-way joins
- aggregate behavior that already exists
- protocol/runtime/codegen behavior across Shunter's flat type system
- index behavior through root runtime and hosted protocol paths

This slice should produce benchmark rows, regression tests, and canary-shaped
fixtures. It should not immediately implement broader maintained joins,
aggregate windows, broad SQL, or new schema kinds.

## Current Boundary

Current subscription behavior:

- single-table non-aggregate declared live views can maintain `ORDER BY`,
  `LIMIT`, and `OFFSET` window membership after commits
- that implementation recomputes candidate windows after commits rather than
  using index-backed incremental top-N maintenance
- joins and multi-way joins are correctness-first
- multi-way live joins have opt-in guardrails:
  - `Config.SubscriptionMaxMultiJoinRelations`
  - `Config.SubscriptionMaxMultiJoinRowsPerRelation`
- default guardrail values are zero, preserving current unlimited behavior
- aggregate behavior exists but is intentionally narrow
- current type/schema/codegen model is flat but includes wide integers,
  timestamp, duration, UUID, JSON, bytes, and `arrayString`

Current evidence:

- `subscription/bench_test.go` includes equality subscriptions, fanout,
  ordered-window paths, join delta eval, multi-way join size fixtures, relation
  shape fixtures including a bounded 5-relation chain, bounded Stage B
  multi-way dimensions, delta index construction, and candidate collection.
- `docs/performance-envelopes.md` records advisory benchmark snapshots and
  known gaps.
- Package tests already cover many subscription correctness paths.
- `internal/gauntlettests/type_index_canary_test.go` covers the flat-kind
  type/index canary through a hosted runtime with reducer writes, local reads,
  declared reads, protocol payloads, live subscription deltas, index seeks, and
  restart.

Implementation anchors:

- `config.go` and `build.go` validate multi-way live-join guardrail config;
  zero means unlimited.
- `subscription/multi_join_limits.go` owns admission and delta-time limit
  checks and returns `ErrMultiJoinLimit`.
- `declared_read_catalog.go` validates declared read shapes, including the
  current single-table live window restrictions and aggregate live-view
  restrictions.
- `subscription/aggregate.go` implements the current aggregate live behavior
  for `COUNT` and `SUM`; unsupported shapes are rejected before execution.
- `subscription/bench_test.go` already contains the benchmark families this
  slice should extend or refresh.
- `types`, `schema`, `store`, `bsatn`, `protocol`, and `codegen` collectively
  define flat value kinds, contract export, storage encoding, protocol row
  payloads, and generated TypeScript decoders.
- `internal/gauntlettests` is the right place for hosted-runtime or protocol
  matrix coverage that should run through real runtime APIs.

Exact gaps after Stage H Cartesian evidence publication:

- Stage B now has bounded benchmark rows for a 3-relation Cartesian
  multi-join, one-match vs 8x8 hot-key selectivity/skew, 1/10/100 changed
  endpoint rows, and `COUNT(*)` aggregate relation-shape variants across
  accepted `chain3`, `self_alias3`, `chain4`, and bounded `cross3` fixtures.
- Stage D now has bounded aggregate-function rows over the existing 128-row
  `chain3` fixture for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage F now has bounded relation-count rows for a 5-relation, 128-row chain
  fixture in both table-shaped projection and `COUNT(*)` aggregate variants.
- Stage G now has bounded skew/fanout evidence for `hot_key_16x16` over the
  existing 128-row, one changed endpoint-row selectivity fixture.
- Stage H now has bounded Cartesian evidence for `cross3_rows_32` in both
  table-shaped projection and `COUNT(*)` aggregate variants.
- Remaining multi-way evidence gaps include larger Cartesian fixtures beyond
  the bounded 32-row cross shape, larger skew/fanout distributions beyond the
  bounded 16x16 row, relation counts beyond the bounded 5-relation chain
  fixture, aggregate-function rows beyond the bounded 128-row `chain3`
  fixture, and workload-derived application distributions.
- The hosted type/index canary now crosses reducer writes, declared reads,
  live subscriptions, protocol payloads, index seeks, and restart. Generated
  TypeScript decoding and backup/restore remain outside this canary; package
  tests continue to cover TypeScript decoder shape separately.
- Aggregate evidence includes package-level correctness coverage, focused
  Stage D `chain3` performance rows, and Stage E documentation/tests for
  current aggregate semantics. Larger aggregate shapes and workload-derived
  distributions remain outside the current envelope.
- Default multi-way join limits remain intentionally unlimited. The bounded
  Stage A through Stage H evidence is advisory, the worst local rows are not
  enough to select safe defaults, and apps can opt into guardrails through
  config.
- The codebase now has a canary app proving every supported flat kind through
  the hosted protocol path, plus existing package-level type and codegen
  coverage.

## Non-Goals

Do not use this slice to add:

- broad SQL behavior
- SQL mutations
- maintained join/window semantics beyond the current single-table subset
- index-backed top-N maintenance
- automatic hard default limits
- new schema type families such as nested products, sums, options, or
  recursive values
- distributed subscription planning
- planner-level cross-table RLS composition

Any semantic expansion should follow evidence that the current implementation
is insufficient for real hosted apps.

## Actionable Outcomes

1. Refresh and publish relation-shape benchmark rows currently mentioned but
   not included in the performance envelope. Completed for existing
   `chain3`, `self_alias3`, and `chain4` fixtures on 2026-05-28.
2. Add missing benchmark dimensions for high-cardinality multi-way live views:
   - relation count
   - rows per relation
   - self-joins
   - cross joins
   - aggregate vs non-aggregate
   - selectivity/skew
   - changed row count

   Stage B subset completed on 2026-05-28 for a bounded `cross3` shape,
   one-match vs `hot_key_8x8` selectivity, 1/10/100 changed endpoint rows, and
   `COUNT(*)` aggregate relation-shape variants. Keep the remaining dimensions
   evidence-first and bounded.

   Stage F subset completed on 2026-05-29 for relation-count coverage beyond
   `chain4`: a bounded 5-relation, 128-row chain with one endpoint insert for
   table-shaped projection and `COUNT(*)`. The raw `-count=10` evidence is
   saved under `working-docs/release-evidence/2026-05-29-subscription-stage-f/`.

   Stage G subset completed on 2026-05-29 for skew/fanout coverage beyond
   `hot_key_8x8`: a bounded `hot_key_16x16` selectivity row over the existing
   128-row fixture with one changed endpoint row. The raw `-count=10` evidence
   is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-g/`.

   Stage H subset completed on 2026-05-29 for Cartesian coverage beyond
   `cross3_rows_24`: bounded `cross3_rows_32` relation-shape and `COUNT(*)`
   aggregate rows over the existing 3-relation Cartesian fixture with one
   changed endpoint row. The raw `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-h/`.
3. Decide whether default multi-way join limits need to change, using
   benchmark and canary evidence rather than speculation.

   Stage D policy decision, 2026-05-29: keep default multi-way join limits
   unlimited. The current bounded evidence does not justify release-facing
   default rejections, and app authors can use
   `Config.SubscriptionMaxMultiJoinRelations` and
   `Config.SubscriptionMaxMultiJoinRowsPerRelation` when they need explicit
   guardrails.
4. Add an end-to-end type/index matrix that crosses:
   - runtime writes
   - declared reads
   - live subscriptions
   - protocol row encoding
   - generated TypeScript decoding where applicable
   - backup/restore or restart when durability matters

   Stage C canary completed on 2026-05-29 for runtime writes, local reads,
   declared queries/views, raw and declared protocol reads, declared protocol
   live-view initial rows and deltas, protocol row-buffer detachment, equality
   and range index seeks, unique-index rejection, nullable flat columns,
   NaN rejection boundaries, and clean restart durability. Generated
   TypeScript decoding stays package-level until it can run as a deterministic
   hosted canary gate.
5. Keep aggregate work limited to tests/benchmarks and small correctness fixes
   unless app requirements demand broader semantics.

## Benchmark Matrix

Start with existing benchmark names:

- `BenchmarkEvalOrderedLimitedWindowDelta`
- `BenchmarkMultiWayLiveJoinEvalSizes`
- `BenchmarkMultiWayLiveJoinRelationShapes`
- `BenchmarkMultiWayLiveJoinSelectivity`
- `BenchmarkMultiWayLiveJoinChangedRows`
- `BenchmarkMultiWayLiveJoinAggregateRelationShapes`
- `BenchmarkMultiWayLiveJoinAggregateFunctions`
- `BenchmarkJoinFragmentEval`
- `BenchmarkFanOut1KClientsSameQuery`
- `BenchmarkFanOut1KClientsVariedQueries`
- `BenchmarkFanOut1KClientsSkewedHotKey`
- `BenchmarkFanOut1KClientsMultiTableVariedQueries`
- `BenchmarkDeltaIndexConstruction`
- `BenchmarkCandidateCollection`

Add or refresh dimensions:

| Dimension | Values |
| --- | --- |
| Relation count | 2, 3, 4, 5 where fixtures remain tractable |
| Rows per relation | 32, 128, 512, 1024 if runtime is acceptable |
| Shape | chain, star, self-alias, repeated alias, cross join |
| Aggregate | none, `COUNT(*)`, existing supported aggregate shapes |
| Selectivity | one match, low fanout, high fanout, skewed hot key |
| Delta size | 1 changed row, 10 changed rows, 100 changed rows |
| Window | no window, top-N, offset window for single-table only |
| Index help | indexed equality/range, table scan |

Benchmark naming should keep dimensions visible in sub-benchmark names so
`benchstat` output is reviewable.

Suggested focused command:

```bash
go test -run '^$' -bench 'Benchmark(MultiWayLiveJoin|JoinFragment|EvalOrderedLimitedWindowDelta|FanOut1K|DeltaIndex|CandidateCollection)' -benchmem -count=10 ./subscription
```

Use raw `go test` for benchmarks, not `rtk go test`, so benchmark rows are not
summarized away.

## Default Limit Policy

Current config leaves multi-way joins unlimited by default:

```go
SubscriptionMaxMultiJoinRelations int
SubscriptionMaxMultiJoinRowsPerRelation int
```

Do not change defaults until evidence answers:

- What fixture shape crosses unacceptable latency or allocation thresholds?
- Are those shapes natural in product apps or only synthetic extremes?
- Would default rejection break existing useful workloads?
- Can app authors set explicit limits in hosted templates instead?
- Are errors understandable at subscription admission and delta evaluation
  time?

If default limits are proposed, document:

- exact default values
- benchmark evidence behind them
- override behavior
- error surface
- release compatibility risk

Stage D decision, 2026-05-29: no default-limit change. Defaults stay zero
(`unlimited`) for compatibility. The published local rows are useful advisory
envelope evidence, but they do not identify safe general defaults for natural
application workloads. Hosted apps can still opt into relation-count and
rows-per-relation guardrails through config.

## End-To-End Type/Index Matrix

The matrix should exercise hosted-runtime behavior, not only package helpers.

Candidate module:

- create an internal gauntlet module with one table covering supported flat
  kinds
- include primary key and secondary indexes
- include at least one unique index if already supported in the relevant
  schema/store surface
- include declared queries and views that read through indexed predicates,
  ordered windows, and raw table scans
- include reducers that insert/update/delete representative rows
- export contract and generate TypeScript in a test or gate if practical

Type rows to cover:

- `bool`
- signed integer widths
- unsigned integer widths
- `int128`, `uint128`, `int256`, `uint256`
- `float32`, `float64`, including rejection of NaN at construction boundaries
- `timestamp`
- `duration`
- `uuid`
- `string`
- `bytes`
- `json`
- `arrayString`
- nullable columns where supported

Behavior to assert:

- reducer writes commit
- local read returns expected values
- declared query returns expected values
- raw SQL read, when policy allows it, returns expected values
- declared live view initial rows decode correctly
- subscription delta rows decode correctly
- protocol row payloads are detached from source buffers
- generated TypeScript row decoder shape matches contract metadata
- snapshot/restart or backup/restore preserves values that are durable
- indexes seek equality and range values correctly

Keep event tables separate if event semantics make durability assertions
confusing.

## Aggregate Evidence

Backlog item 24 stays mostly deferred. Actionable work now:

- add correctness tests for currently supported aggregate shapes:
  `COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM` over the
  numeric domains accepted by current validation
- add benchmark rows for aggregate multi-way joins already accepted by the
  subscription layer

  Stage D benchmark rows, 2026-05-29: published focused
  `BenchmarkMultiWayLiveJoinAggregateFunctions` rows for `COUNT(*)`,
  `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
  existing bounded `chain3` fixture.
- document current empty-set behavior
- document current numeric-domain support
- document rejected shapes such as aggregate `ORDER BY`/`LIMIT`/`OFFSET`,
  `SUM(DISTINCT ...)`, and unsupported `SUM` source kinds
- add copy-isolation tests if `COUNT(DISTINCT)` or distinct sets expose
  mutable state
- add memory measurements only when aggregate workloads show real pressure

Do not broaden `SUM`, nullable aggregate semantics, or distinct memory
accounting without a concrete app-facing requirement.

Stage E semantics status, 2026-05-29: completed focused live aggregate coverage
and user-facing documentation for `COUNT(*)`, `COUNT(column)`,
`COUNT(DISTINCT column)`, `SUM(column)`, empty matches, all-null nullable sums,
supported `SUM` source/result kinds, rejected live aggregate window shapes,
`SUM(DISTINCT ...)`, unsupported `SUM` source kinds, and replacement-row live
deltas. Runtime semantics and default multi-way join guardrails stayed
unchanged. Remaining aggregate gaps are broader product/functionality work:
grouped aggregates, aggregate windows, live aggregate `ORDER BY`/`LIMIT` and
`OFFSET`, `SUM(DISTINCT ...)`, unsupported numeric families such as wide
integers, larger aggregate performance shapes, workload-derived distributions,
and distinct-memory accounting beyond the current immutable row-value contract.

## Staging

Stage A: publish existing evidence.

- refresh relation-shape benchmark rows from the current `subscription`
  benchmarks
- update `docs/performance-envelopes.md` with representative rows and known
  omissions
- leave default guardrails unchanged

Stage A status, 2026-05-28: completed for existing subscription evidence
publication. The refreshed command covered
`BenchmarkMultiWayLiveJoin(EvalSizes|RelationShapes)` at commit
`69df08549a22d8c2bb135131b3cc18900f370771`; no new benchmark cases were
needed, and default guardrails stayed unchanged.

Stage B: add missing subscription dimensions.

- add or extend benchmark subcases for self-alias, repeated alias, cross join,
  skew, and changed-row-count dimensions
- add aggregate subcases only for shapes already accepted today
- keep sub-benchmark names reviewable in `benchstat`

Stage B status, 2026-05-28: completed a bounded benchmark subset in
`subscription/bench_test.go` and published the focused `-count=10` evidence in
`docs/performance-envelopes.md`. The subset covers `cross3_rows_24`,
`one_match`, `hot_key_8x8`, `changed_1`, `changed_10`, `changed_100`, and
`COUNT(*)` aggregate relation-shape variants for accepted `chain3`,
`self_alias3`, `chain4`, and bounded `cross3` shapes. Runtime semantics and
default multi-way join guardrails stayed unchanged. The default-limit policy
remains deferred.

Stage C: add the type/index canary.

- build an internal module or root-runtime fixture with the supported flat
  kinds
- write through reducers, read through declared reads, subscribe over the
  protocol, and verify restart or backup/restore for durable values
- include generated TypeScript decoding only when the local gate can run it
  deterministically

Stage C status, 2026-05-29: completed the hosted runtime canary in
`internal/gauntlettests/type_index_canary_test.go`. The canary table includes
the current flat kinds (`bool`, signed/unsigned integer widths, wide integers,
floats, timestamp, duration, UUID, string, bytes, JSON, `arrayString`, and a
nullable string), primary/secondary/unique indexes, reducer insert/update/delete
paths, local table and index reads, declared query/view reads, raw SQL and
declared protocol reads, declared protocol live-view initial rows and deltas,
protocol row-buffer detachment checks, NaN construction rejections, and restart
verification. Backup/restore and generated TypeScript decoding remain outside
this hosted canary.

Stage D: decide policy.

- review whether unlimited defaults remain acceptable
- if changing defaults, document exact values, override behavior, error
  surface, compatibility risk, and benchmark evidence

Stage D status, 2026-05-29: completed a bounded aggregate-function evidence
slice in `subscription/bench_test.go` and published the focused `-count=10`
rows in `docs/performance-envelopes.md`. The slice covers `COUNT(*)`,
`COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the existing
128-row `chain3` fixture. Runtime semantics and default multi-way join
guardrails stayed unchanged. The policy decision is to keep defaults zero
(`unlimited`) because the bounded evidence remains advisory, the worst local
rows are not enough to select safe defaults, and apps can still opt into
guardrails through config.

Stage E: document and pin current aggregate semantics.

- add focused correctness tests only for missing current behavior
- document supported aggregate functions, null/empty behavior, numeric domains,
  rejected live aggregate shapes, and replacement-row live deltas
- leave runtime semantics and guardrail defaults unchanged

Stage E status, 2026-05-29: completed in `subscription/aggregate_test.go`,
`docs/how-to/reads-queries-views.md`, and `docs/reference/read-surface.md`.
The slice pins live aggregate empty/all-null behavior, replacement deltas across
empty/non-empty transitions, accepted `SUM` result domains, and unsupported
`SUM` source kinds. Existing declared-read tests already cover live aggregate
`ORDER BY`/`LIMIT`/`OFFSET` rejection, `SUM(DISTINCT ...)` rejection, and string
`SUM` rejection. Existing protocol and declared-query tests already cover
one-off/declared-query nullable aggregate semantics. No aggregate feature set
was broadened.

Stage F: close one bounded multi-way evidence gap.

- inventory current multi-way benchmark/docs coverage
- add a cheap relation-count extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage F status, 2026-05-29: completed a bounded 5-relation chain extension in
`subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice covers
`MultiWayLiveJoinRelationShapes/chain5_rows_128` and
`MultiWayLiveJoinAggregateRelationShapes/chain5_rows_128/count` alongside the
existing focused multi-way benchmark families. Runtime semantics and default
multi-way join guardrails stayed unchanged. Larger relation counts, larger
Cartesian/skew fixtures, broader aggregate-function shapes, and app-derived
workload distributions remain evidence backlog.

Stage G: close one bounded multi-way skew/fanout evidence gap.

- inventory current multi-way benchmark/docs coverage
- add a cheap skew/fanout extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage G status, 2026-05-29: completed a bounded `hot_key_16x16` selectivity
extension in `subscription/bench_test.go` and published the focused
`-count=10` rows in `docs/performance-envelopes.md`. The slice extends the
existing 128-row, one changed endpoint-row selectivity fixture beyond
`hot_key_8x8` while keeping the benchmark local-review-sized. Runtime semantics
and default multi-way join guardrails stayed unchanged. Larger skew/fanout
distributions beyond 16x16, larger Cartesian fixtures, broader aggregate
skew/function shapes, relation counts beyond the bounded 5-relation chain, and
app-derived workload distributions remain evidence backlog.

Stage H: close one bounded multi-way Cartesian evidence gap.

- inventory current multi-way Cartesian benchmark/docs coverage
- add a cheap Cartesian extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage H status, 2026-05-29: completed a bounded `cross3_rows_32` Cartesian
extension in `subscription/bench_test.go` and published the focused
`-count=10` rows in `docs/performance-envelopes.md`. The slice extends the
existing 3-relation Cartesian fixture beyond `cross3_rows_24` for both
table-shaped projection and `COUNT(*)` aggregate rows; one changed endpoint row
emits a 32x32 Cartesian fragment. Runtime semantics and default multi-way join
guardrails stayed unchanged. Larger Cartesian fixtures beyond 32 rows, larger
skew/fanout distributions beyond 16x16, broader aggregate skew/function shapes,
relation counts beyond the bounded 5-relation chain, and app-derived workload
distributions remain evidence backlog.

## Risks

- Benchmark rows can become misleading if they mix machine-local exploratory
  runs with release evidence. Keep raw output location and command explicit.
- A broad type matrix can turn into new type-system design. Limit it to
  existing flat kinds and existing nullable support.
- Generated TypeScript coverage may add npm runtime cost to a Go gate. Keep it
  optional until it is deterministic enough for release qualification.
- Limit changes are release-facing behavior. Do not change defaults without
  changelog and release-qualification updates.

## Performance Envelope Updates

When benchmark evidence changes:

1. Run focused benchmarks with `-count=10`.
2. Save raw output under `working-docs/release-evidence/<slice>/` when it is
   release-relevant, or under `/tmp` for exploratory evidence that will be
   summarized.
3. Run `benchstat`.
4. Add representative rows to `docs/performance-envelopes.md`.
5. Record known gaps honestly.

Do not turn advisory rows into release-blocking gates without updating
`working-docs/release-qualification.md`.

## Implementation Sequence

1. Refresh current relation-shape benchmarks and publish missing rows.
2. Add benchmark dimensions that are cheap and deterministic.
3. Add type/index gauntlet module or root-runtime test fixture.
4. Add generated TypeScript coverage for the matrix only if it can run in a
   deterministic local gate.
5. Review multi-way join limits after evidence exists.
6. Only then decide whether any implementation changes are warranted.

## Likely Touched Files

- `subscription/bench_test.go`
- `subscription/multi_join_test.go`
- `subscription/multi_join_limits.go`
- `subscription/aggregate_test.go`
- `internal/gauntlettests/*`
- `declared_read_test.go`
- `declared_read_protocol_test.go`
- `codegen/codegen_test.go`
- `typescript/client/test/*`
- `docs/performance-envelopes.md`
- `working-docs/release-qualification.md`

## Validation

Correctness:

```bash
rtk go test ./types ./schema ./bsatn ./store ./protocol ./subscription ./codegen ./internal/gauntlettests .
```

Benchmarks:

```bash
go test -run '^$' -bench 'Benchmark(MultiWayLiveJoin|JoinFragment|EvalOrderedLimitedWindowDelta|FanOut1K|DeltaIndex|CandidateCollection)' -benchmem -count=10 ./subscription
```

If generated TypeScript matrix coverage changes:

```bash
rtk go test ./codegen
rtk npm --prefix typescript/client test
rtk npm --prefix typescript/client run build
```

If release-facing docs or defaults change:

```bash
rtk go test ./...
rtk go vet ./...
rtk go tool staticcheck ./...
```

## Completion Criteria

This slice is complete when:

- relation-shape benchmark rows are refreshed and published or explicitly
  recorded as too expensive for the local envelope
- the high-cardinality multi-way join evidence is sufficient to keep or change
  default limit policy
- an end-to-end type/index matrix exists for the currently supported flat type
  system
- aggregate behavior has focused correctness/performance evidence for the
  semantics Shunter already supports
- no broader SQL, cross-table RLS composition, or richer type-system work was
  introduced by accident
