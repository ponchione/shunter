# Subscription Evidence And Type/Index Matrix

Status: synthetic multi-way evidence campaign closed after Stage Z; generated
TypeScript flat-kind decoding, hosted TypeScript client execution, and
flat-kind backup/restore now have deterministic local gates; remaining items
stay evidence backlog
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
- `subscription/bench_test.go` includes one workload-derived RC taskboard
  `open_tasks_live` live-view delta fixture for the concrete `create_task`
  insert-open and `complete_task` delete-open reducer flows.
- `internal/gauntlettests/rc_app_workload_test.go` includes a bounded
  two-subscriber hosted protocol fanout gate for the same RC taskboard
  `open_tasks_live` declared view and reducer flow.
- `docs/performance-envelopes.md` records advisory benchmark snapshots and
  known gaps.
- Package tests already cover many subscription correctness paths.
- `internal/gauntlettests/type_index_canary_test.go` covers the flat-kind
  type/index canary through a hosted runtime with reducer writes, local reads,
  declared reads, protocol payloads, live subscription deltas, index seeks, and
  restart.
- `typescript/client/test/generated-type-index-decoding.test.ts` executes a
  generated flat-kind table decoder against protocol-shaped row-list payloads
  for the same canary value families.
- `typescript/client/test/hosted-type-index-canary.test.mjs` executes the
  TypeScript client runtime against a local hosted canary runtime and decodes
  declared query/view rows with the generated flat-kind decoder.

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

Exact gaps after Stage Z larger skew/fanout evidence publication:

- Stage B now has bounded benchmark rows for a 3-relation Cartesian
  multi-join, one-match vs 8x8 hot-key selectivity/skew, 1/10/100 changed
  endpoint rows, and `COUNT(*)` aggregate relation-shape variants across
  accepted `chain3`, `self_alias3`, `chain4`, and bounded `cross3` fixtures.
- Stage D now has bounded aggregate-function rows over the existing 128-row
  `chain3` fixture for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage I now extends those aggregate-function rows to the existing bounded
  128-row `chain4` fixture.
- Stage F now has bounded relation-count rows for a 5-relation, 128-row chain
  fixture in both table-shaped projection and `COUNT(*)` aggregate variants.
- Stage G now has bounded skew/fanout evidence for `hot_key_16x16` over the
  existing 128-row, one changed endpoint-row selectivity fixture.
- Stage H now has bounded Cartesian evidence for `cross3_rows_32` in both
  table-shaped projection and `COUNT(*)` aggregate variants.
- Stage J now extends aggregate-function rows to the existing bounded
  `cross3_rows_32` Cartesian fixture for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage K now extends aggregate-function rows to the existing bounded
  `hot_key_16x16` skew/fanout fixture for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage L now extends aggregate-function rows to the existing bounded
  `self_alias3` repeated-table fixture for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage M now extends bounded Cartesian evidence to `cross3_rows_40` for
  table-shaped projection, `COUNT(*)` aggregate relation-shape rows, and
  aggregate-function rows for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage N now extends bounded skew/fanout evidence to `hot_key_24x24` for
  table-shaped projection and aggregate-function rows for `COUNT(*)`,
  `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage O now extends bounded Cartesian evidence to `cross3_rows_48` for
  table-shaped projection, `COUNT(*)` aggregate relation-shape rows, and
  aggregate-function rows for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage P now extends bounded skew/fanout evidence to `hot_key_32x32` for
  table-shaped projection and aggregate-function rows for `COUNT(*)`,
  `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage Q now extends bounded Cartesian evidence to `cross3_rows_56` for
  table-shaped projection, `COUNT(*)` aggregate relation-shape rows, and
  aggregate-function rows for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage R now extends bounded skew/fanout evidence to `hot_key_40x40` for
  table-shaped projection and aggregate-function rows for `COUNT(*)`,
  `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage S now extends bounded Cartesian evidence to `cross3_rows_64` for
  table-shaped projection, `COUNT(*)` aggregate relation-shape rows, and
  aggregate-function rows for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage T now extends bounded skew/fanout evidence to `hot_key_48x48` for
  table-shaped projection and aggregate-function rows for `COUNT(*)`,
  `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage U now extends bounded Cartesian evidence to `cross3_rows_72` for
  table-shaped projection, `COUNT(*)` aggregate relation-shape rows, and
  aggregate-function rows for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage V now extends bounded skew/fanout evidence to `hot_key_56x56` for
  table-shaped projection and aggregate-function rows for `COUNT(*)`,
  `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage W now extends bounded Cartesian evidence to `cross3_rows_80` for
  table-shaped projection, `COUNT(*)` aggregate relation-shape rows, and
  aggregate-function rows for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage X now extends bounded skew/fanout evidence to `hot_key_64x64` for
  table-shaped projection and aggregate-function rows for `COUNT(*)`,
  `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage Y now extends bounded Cartesian evidence to `cross3_rows_88` for
  table-shaped projection, `COUNT(*)` aggregate relation-shape rows, and
  aggregate-function rows for `COUNT(*)`, `COUNT(column)`,
  `COUNT(DISTINCT column)`, and `SUM(column)`.
- Stage Z now extends bounded skew/fanout evidence to `hot_key_72x72` for
  table-shaped projection and aggregate-function rows for `COUNT(*)`,
  `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`.
- Remaining multi-way evidence gaps include larger Cartesian fixtures beyond
  the bounded 88-row cross shape, larger skew/fanout distributions beyond the
  bounded 72x72 row, relation counts beyond the bounded 5-relation chain
  fixture, larger aggregate-function self-alias distributions beyond the
  bounded `self_alias3` fixture, and workload-derived multi-way application
  distributions.
- The first concrete workload-derived subscription delta benchmark is covered:
  `BenchmarkRCAppOpenTasksLiveViewDelta` maps the release-candidate taskboard
  app's `open_tasks_live` declared view to the in-process subscription manager
  and measures `create_task` insert-open and `complete_task` delete-open
  deltas. Raw `-count=10` evidence is saved under
  `working-docs/release-evidence/2026-06-02-workload-derived-subscription/`.
- The first bounded workload-derived protocol fanout correctness gate is
  covered: `TestReleaseCandidateExampleAppProtocolFanoutStrictAuth` subscribes
  two strict-auth WebSocket clients to the same RC taskboard
  `open_tasks_live` declared view and verifies that both receive the same
  `create_task` insert and `complete_task` delete deltas. This is not timing
  evidence; the current concrete RC reducer flow mutates task state across
  commits, so a steady hosted protocol benchmark would need additional real
  workload support rather than a size-only synthetic loop.
- The first bounded workload-derived hosted subscription timing row is
  covered: `BenchmarkDeclaredReadHostedSubscriptionReducerDelta` drives the
  existing chat declared-read workload through a strict-auth local WebSocket
  caller plus one subscriber. Each measured operation performs a real
  `insert_message_with_body` reducer call, reads the caller
  `TransactionUpdate`, reads the subscriber insert `TransactionUpdateLight`,
  performs a real `delete_message_by_id` reducer call for the same row, and
  reads the subscriber delete `TransactionUpdateLight`. The table cardinality
  returns to baseline each iteration. Raw `-count=10` evidence is saved under
  `working-docs/release-evidence/2026-06-02-workload-derived-subscription-hosted-timing/`.
- The hosted type/index canary now crosses reducer writes, declared reads,
  live subscriptions, protocol payloads, index seeks, restart, and offline
  backup/restore. Generated TypeScript decoding now has deterministic
  package-level gates that cross contract export, generated bindings,
  protocol-shaped row-list payloads, the TypeScript runtime, and a local hosted
  runtime reached through the TypeScript client.
- Aggregate evidence includes package-level correctness coverage, focused
  Stage D `chain3`, Stage I `chain4`, Stage J `cross3_rows_32`, Stage K
  `hot_key_16x16`, Stage L `self_alias3`, Stage M `cross3_rows_40`, Stage N
  `hot_key_24x24`, Stage O `cross3_rows_48`, Stage P `hot_key_32x32`,
  Stage Q `cross3_rows_56`, Stage R `hot_key_40x40`, Stage S
  `cross3_rows_64`, Stage T `hot_key_48x48`, Stage U `cross3_rows_72`,
  Stage V `hot_key_56x56`, Stage W `cross3_rows_80`, Stage X
  `hot_key_64x64`, Stage Y `cross3_rows_88`, and Stage Z
  `hot_key_72x72` performance rows, plus Stage E documentation/tests for
  current aggregate semantics. Larger
  aggregate shapes and workload-derived distributions remain outside the
  current envelope.
- Default multi-way join limits remain intentionally unlimited. The bounded
  Stage A through Stage Z evidence is advisory, the worst local rows are not
  enough to select safe defaults, and apps can opt into guardrails through
  config.
- The codebase now has a canary app proving every supported flat kind through
  the hosted protocol path, offline backup/restore, package-level generated
  TypeScript decoder execution, and hosted TypeScript client execution for the
  same flat-kind shape.
- Synthetic multi-way size expansion is capped here for the local advisory
  envelope. Do not continue with Stage AA/AB size-only slices unless a real app
  workload, regression investigation, release-gate threshold, or renewed
  default-limit proposal needs the specific shape.
- The generated TypeScript decoding, hosted TypeScript client execution, and
  backup/restore gaps are closed for the deterministic local flat-kind gate.
  Remaining matrix gaps include broader workload-derived application fanout
  distributions beyond the bounded two-subscriber RC protocol gate and
  one-subscriber hosted timing row, broader application timing, and
  multi-table/multi-way distributions.

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

   Stage I subset completed on 2026-05-29 for aggregate-function shape coverage
   beyond `chain3`: bounded `chain4` rows for `COUNT(*)`, `COUNT(column)`,
   `COUNT(DISTINCT column)`, and `SUM(column)` over the existing 128-row,
   one endpoint insert fixture. The raw `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-i/`.

   Stage J subset completed on 2026-05-29 for aggregate-function distribution
   coverage across the existing Cartesian fixture: bounded `cross3_rows_32`
   rows for `COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and
   `SUM(column)` over the 3-relation Cartesian fixture with one changed
   endpoint row. The raw `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-j/`.

   Stage K subset completed on 2026-05-29 for aggregate-function distribution
   coverage across the existing skew/fanout fixture: bounded `hot_key_16x16`
   rows for `COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and
   `SUM(column)` over the 3-relation chain fixture with one changed endpoint
   row matching a 16x16 fanout fragment. The raw `-count=10` evidence is saved
   under
   `working-docs/release-evidence/2026-05-29-subscription-stage-k/`.

   Stage L subset completed on 2026-05-29 for aggregate-function distribution
   coverage across the existing self-alias fixture: bounded `self_alias3` rows
   for `COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and
   `SUM(column)` over the 3-alias, 2-physical-table fixture with one changed
   endpoint row inserted into table 2 alias 2. The raw `-count=10` evidence is
   saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-l/`.

   Stage M subset completed on 2026-05-29 for larger bounded Cartesian
   coverage: `cross3_rows_40` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation Cartesian fixture with one changed endpoint row emitting a 40x40
   Cartesian fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-m/`.

   Stage N subset completed on 2026-05-29 for larger bounded skew/fanout
   coverage: `hot_key_24x24` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation chain fixture with one changed endpoint row matching a 24x24
   fanout fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-n/`.

   Stage O subset completed on 2026-05-29 for larger bounded Cartesian
   coverage: `cross3_rows_48` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation Cartesian fixture with one changed endpoint row emitting a 48x48
   Cartesian fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-o/`.

   Stage P subset completed on 2026-05-29 for larger bounded skew/fanout
   coverage: `hot_key_32x32` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation chain fixture with one changed endpoint row matching a 32x32
   fanout fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-p/`.

   Stage Q subset completed on 2026-05-29 for larger bounded Cartesian
   coverage: `cross3_rows_56` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation Cartesian fixture with one changed endpoint row emitting a 56x56
   Cartesian fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-q/`.

   Stage R subset completed on 2026-05-29 for larger bounded skew/fanout
   coverage: `hot_key_40x40` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation chain fixture with one changed endpoint row matching a 40x40
   fanout fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-r/`.

   Stage S subset completed on 2026-05-29 for larger bounded Cartesian
   coverage: `cross3_rows_64` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation Cartesian fixture with one changed endpoint row emitting a 64x64
   Cartesian fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-s/`.

   Stage T subset completed on 2026-05-29 for larger bounded skew/fanout
   coverage: `hot_key_48x48` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation chain fixture with one changed endpoint row matching a 48x48
   fanout fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-t/`.

   Stage U subset completed on 2026-06-02 for larger bounded Cartesian
   coverage: `cross3_rows_72` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation Cartesian fixture with one changed endpoint row emitting a 72x72
   Cartesian fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-05-29-subscription-stage-u/`.

   Stage V subset completed on 2026-06-02 for larger bounded skew/fanout
   coverage: `hot_key_56x56` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation chain fixture with one changed endpoint row matching a 56x56
   fanout fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-06-02-subscription-stage-v/`.

   Stage W subset completed on 2026-06-02 for larger bounded Cartesian
   coverage: `cross3_rows_80` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation Cartesian fixture with one changed endpoint row emitting an
   80x80 Cartesian fragment. The raw focused `-count=10` evidence is saved
   under `working-docs/release-evidence/2026-06-02-subscription-stage-w/`.

   Stage X subset completed on 2026-06-02 for larger bounded skew/fanout
   coverage: `hot_key_64x64` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation chain fixture with one changed endpoint row matching a 64x64
   fanout fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-06-02-subscription-stage-x/`.

   Stage Y subset completed on 2026-06-02 for larger bounded Cartesian
   coverage: `cross3_rows_88` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation Cartesian fixture with one changed endpoint row emitting an
   88x88 Cartesian fragment. The raw focused `-count=10` evidence is saved
   under `working-docs/release-evidence/2026-06-02-subscription-stage-y/`.

   Stage Z subset completed on 2026-06-02 for larger bounded skew/fanout
   coverage: `hot_key_72x72` rows for table-shaped projection, `COUNT(*)`,
   `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` over the
   3-relation chain fixture with one changed endpoint row matching a 72x72
   fanout fragment. The raw focused `-count=10` evidence is saved under
   `working-docs/release-evidence/2026-06-02-subscription-stage-z/`.

   Close-out decision, 2026-06-02: stop synthetic multi-way size expansion at
   the Stage Z envelope. The current evidence is sufficient to keep the
   high-cardinality multi-way benchmark rows advisory and keep default
   guardrails unchanged. Larger Cartesian, skew/fanout, relation-count,
   self-alias, and workload-derived distributions remain backlog items with
   explicit resume triggers, not the next active local-review campaign.
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
   NaN rejection boundaries, and clean restart durability.

   TypeScript decoding add-on, 2026-06-02: added a generated flat-kind fixture
   and runtime TypeScript test that decode protocol-shaped row-list payloads
   for the canary value families, including nullable string present/null cases.
   This stays package-level and deterministic.

   Hosted TypeScript client add-on, 2026-06-02: added a deterministic package
   test that starts a local hosted canary runtime, connects through the
   TypeScript client, performs protocol reducer writes, declared query reads,
   and declared live-view subscription updates, then decodes hosted row bytes
   with the generated flat-kind decoder.

   Backup/restore add-on, 2026-06-02: added a deterministic hosted-runtime
   restore gate that writes the current flat-kind canary rows, waits for
   durability, runs `BackupDataDir` and `RestoreDataDir`, rebuilds from the
   restored DataDir, and asserts restored table scans, declared reads, primary
   key seeks, unique index seeks, secondary equality seeks, and secondary range
   seeks.
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
- `BenchmarkRCAppOpenTasksLiveViewDelta`
- `BenchmarkDeclaredReadHostedSubscriptionReducerDelta`
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
- generated TypeScript row decoder shape matches contract metadata and executes
  against protocol-shaped row-list payloads
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

  Stage I benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the existing bounded `chain4` fixture. `COUNT(column)`
  and `SUM(column)` track `COUNT(*)` in the focused run, while
  `COUNT(DISTINCT column)` adds allocation without becoming a latency standout.

  Stage J benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the existing bounded `cross3_rows_32` Cartesian fixture.
  `COUNT(column)` and `SUM(column)` are slower than `COUNT(*)` in this
  distribution while retaining similar allocation, and `COUNT(DISTINCT column)`
  is the slowest new Cartesian aggregate-function row without overtaking the
  existing `self_alias3/count` latency standout.

  Stage K benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the existing bounded `hot_key_16x16` skew/fanout
  fixture. `COUNT(column)` and `SUM(column)` track `COUNT(*)` closely in this
  skew fixture, and `COUNT(DISTINCT column)` adds allocation without becoming
  a new latency standout.

  Stage L benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the existing bounded `self_alias3` repeated-table
  fixture. `COUNT(column)` and `SUM(column)` target table 2 alias 2 and track
  `COUNT(*)` closely, while `COUNT(DISTINCT column)` adds allocation without
  adding more latency spread. The self-alias aggregate rows remain the focused
  aggregate latency standout and make larger self-alias distributions better
  suited to a longer-running benchmark lane.

  Stage M benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the larger bounded `cross3_rows_40` Cartesian fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while adding latency in this Cartesian distribution, and
  `COUNT(DISTINCT column)` remains the slowest Stage M Cartesian aggregate row
  without overtaking the existing `self_alias3` latency standout.

  Stage N benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the larger bounded `hot_key_24x24` skew/fanout fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)`, and `COUNT(DISTINCT column)` adds allocation and allocation count
  without becoming a latency standout.

  Stage O benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the larger bounded `cross3_rows_48` Cartesian fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while adding latency in this Cartesian distribution, and
  `COUNT(DISTINCT column)` remains the slowest Stage O Cartesian aggregate row
  without overtaking the existing `self_alias3` latency standout.

  Stage P benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the larger bounded `hot_key_32x32` skew/fanout fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)`, and `COUNT(DISTINCT column)` adds allocation and allocation count
  without becoming a latency standout.

  Stage Q benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the larger bounded `cross3_rows_56` Cartesian fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while adding latency in this Cartesian distribution, and
  `COUNT(DISTINCT column)` remains the slowest Stage Q Cartesian aggregate row
  while staying local-review-sized.

  Stage R benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the larger bounded `hot_key_40x40` skew/fanout fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)`, and `COUNT(DISTINCT column)` adds allocation and allocation count
  without becoming a latency standout.

  Stage T benchmark rows, 2026-05-29: extended the same aggregate-function
  benchmark family to the larger bounded `hot_key_48x48` skew/fanout fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)`, and `COUNT(DISTINCT column)` adds allocation and allocation count
  without becoming a latency standout.

  Stage U benchmark rows, 2026-06-02: extended the same aggregate-function
  benchmark family to the larger bounded `cross3_rows_72` Cartesian fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while adding latency in this Cartesian distribution, and
  `COUNT(DISTINCT column)` remains the slowest Stage U Cartesian aggregate row
  while staying local-review-sized.

  Stage V benchmark rows, 2026-06-02: extended the same aggregate-function
  benchmark family to the larger bounded `hot_key_56x56` skew/fanout fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)`, and `COUNT(DISTINCT column)` adds allocation and allocation count
  without becoming a latency standout.

  Stage W benchmark rows, 2026-06-02: extended the same aggregate-function
  benchmark family to the larger bounded `cross3_rows_80` Cartesian fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)` while adding latency in this Cartesian distribution, and
  `COUNT(DISTINCT column)` remains the slowest Stage W Cartesian aggregate row
  while staying local-review-sized.

  Stage X benchmark rows, 2026-06-02: extended the same aggregate-function
  benchmark family to the larger bounded `hot_key_64x64` skew/fanout fixture.
  `COUNT(column)` and `SUM(column)` remain allocation-stable relative to
  `COUNT(*)`, and `COUNT(DISTINCT column)` adds allocation and allocation count
  without becoming a latency standout.
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
verification. Generated TypeScript decoding remains outside this hosted canary.

Stage C TypeScript decoding add-on, 2026-06-02: added
`typescript/client/test/fixtures/flat_type_index_canary.ts` as a generated
fixture, guarded by a Go golden test, and
`typescript/client/test/generated-type-index-decoding.test.ts` as an executed
runtime test. The gate decodes protocol-shaped row-list payloads through the
generated `flat_values` decoder for bool, signed/unsigned integer widths,
128/256-bit integers, float32/float64 finite values, timestamp, duration, UUID,
string, bytes, JSON, `arrayString`, and nullable string present/null cases.
No new value kinds, protocol semantics, subscription semantics, or hosted app
dependencies were added.

Stage C hosted TypeScript client add-on, 2026-06-02: added
`typescript/client/test/fixtures/hosted_type_index_canary` as a narrow local Go
server fixture and `typescript/client/test/hosted-type-index-canary.test.mjs`
as an executed package test. The gate starts a real hosted canary runtime on an
ephemeral loopback port, connects through `createShunterClient`, writes rows
through protocol reducer calls, runs the declared active-row query, subscribes
to the declared active-row live view, receives a live update after a reducer
write, and decodes hosted protocol row bytes with the generated `flat_values`
decoder. The representative rows assert bool, signed/unsigned integer widths,
128/256-bit integers, finite float32/float64 values, timestamp, duration, UUID,
string, bytes, JSON, `arrayString`, and nullable string present/null cases. No
new value kinds, protocol semantics, subscription semantics, broad SDK surface,
external hosted-app dependency, or synthetic multi-way benchmark rows were
added.

Stage C backup/restore add-on, 2026-06-02: added
`TestRuntimeGauntletFlatTypeIndexCanaryBackupRestore` in
`internal/gauntlettests/type_index_canary_test.go`. The gate writes canary rows
with nullable string present and null cases, active/inactive buckets, wide
integer values, timestamp, duration, UUID, bytes, JSON, and `arrayString`
values through reducers; waits for durability; closes the runtime; backs up and
restores the DataDir; rebuilds from the restored DataDir; and reuses local
scan, declared query/view, primary key, unique index, secondary equality index,
and secondary range index assertions. No new value kinds, backup semantics,
storage semantics, protocol semantics, subscription semantics, SDK surface, or
hosted external-app dependencies were added. Workload-derived application
distributions remain a known gap.

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

Stage I: close one bounded aggregate-function shape evidence gap.

- inventory current aggregate-function benchmark/docs coverage
- add a cheap non-`chain3` aggregate-function extension only if it stays
  local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage I status, 2026-05-29: completed a bounded `chain4` aggregate-function
extension in `subscription/bench_test.go` and published the focused
`-count=10` rows in `docs/performance-envelopes.md`. The slice extends
`BenchmarkMultiWayLiveJoinAggregateFunctions` beyond the existing `chain3`
fixture with `chain4/count_star`, `chain4/count_column`,
`chain4/count_distinct`, and `chain4/sum`. Runtime semantics and default
multi-way join guardrails stayed unchanged. Larger aggregate-function fixtures
beyond the bounded 128-row `chain4` chain, larger aggregate-function Cartesian
fixtures beyond bounded `cross3_rows_32`, aggregate-function skew/self-alias
distributions, relation counts beyond the bounded 5-relation chain, and
app-derived workload distributions remain evidence backlog.

Stage J: close one bounded aggregate-function Cartesian distribution gap.

- inventory current aggregate-function Cartesian benchmark/docs coverage
- add a cheap Cartesian aggregate-function extension only if it stays
  local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage J status, 2026-05-29: completed bounded `cross3_rows_32`
aggregate-function rows in `subscription/bench_test.go` and published the
focused `-count=10` rows in `docs/performance-envelopes.md`. The slice extends
`BenchmarkMultiWayLiveJoinAggregateFunctions` with
`cross3_rows_32/count_star`, `cross3_rows_32/count_column`,
`cross3_rows_32/count_distinct`, and `cross3_rows_32/sum` over the existing
3-relation Cartesian fixture; one changed endpoint row emits a 32x32 Cartesian
fragment. Runtime semantics and default multi-way join guardrails stayed
unchanged. Larger Cartesian fixtures beyond 32 rows, aggregate-function
skew/self-alias distributions, relation counts beyond the bounded 5-relation
chain, and app-derived workload distributions remain evidence backlog.

Stage K: close one bounded aggregate-function skew/fanout distribution gap.

- inventory current aggregate-function skew/fanout benchmark/docs coverage
- add a cheap skew/fanout aggregate-function extension only if it stays
  local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage K status, 2026-05-29: completed bounded `hot_key_16x16`
aggregate-function rows in `subscription/bench_test.go` and published the
focused `-count=10` rows in `docs/performance-envelopes.md`. The slice extends
`BenchmarkMultiWayLiveJoinAggregateFunctions` with
`hot_key_16x16/count_star`, `hot_key_16x16/count_column`,
`hot_key_16x16/count_distinct`, and `hot_key_16x16/sum` over the existing
128-row, 3-relation skew/fanout fixture; one changed endpoint row matches a
16x16 fanout fragment. Runtime semantics and default multi-way join guardrails
stayed unchanged. Larger skew/fanout distributions beyond 16x16, larger
Cartesian fixtures beyond 32 rows, aggregate-function self-alias
distributions, relation counts beyond the bounded 5-relation chain, and
app-derived workload distributions remain evidence backlog.

Stage L: close one bounded aggregate-function self-alias distribution gap.

- inventory current aggregate-function self-alias benchmark/docs coverage
- add a cheap self-alias aggregate-function extension only if it stays
  local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage L status, 2026-05-29: completed bounded `self_alias3`
aggregate-function rows in `subscription/bench_test.go` and published the
focused `-count=10` rows in `docs/performance-envelopes.md`. The slice extends
`BenchmarkMultiWayLiveJoinAggregateFunctions` with
`self_alias3/count_star`, `self_alias3/count_column`,
`self_alias3/count_distinct`, and `self_alias3/sum` over the existing 128-row,
3-alias repeated-table fixture; one changed endpoint row is inserted into table
2 alias 2. Runtime semantics and default multi-way join guardrails stayed
unchanged. The focused run remained local-review-sized but took 405.559s, so
larger self-alias distributions, larger skew/fanout distributions beyond
16x16, larger Cartesian fixtures beyond 32 rows, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage M: close one larger bounded Cartesian evidence gap.

- inventory current multi-way Cartesian benchmark/docs coverage
- add a larger Cartesian extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage M status, 2026-05-29: completed bounded `cross3_rows_40` Cartesian rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
Cartesian fixture beyond `cross3_rows_32` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row emits a 40x40 Cartesian fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger Cartesian fixtures
beyond 40 rows, larger skew/fanout distributions beyond 16x16, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage N: close one larger bounded skew/fanout evidence gap.

- inventory current multi-way skew/fanout benchmark/docs coverage
- add a larger skew/fanout extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage N status, 2026-05-29: completed bounded `hot_key_24x24` skew/fanout rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
skew/fanout fixture beyond `hot_key_16x16` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row matches a 24x24 fanout fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger skew/fanout
distributions beyond 24x24, larger Cartesian fixtures beyond 40 rows, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage O: close one larger bounded Cartesian evidence gap.

- inventory current multi-way Cartesian benchmark/docs coverage
- add a larger Cartesian extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage O status, 2026-05-29: completed bounded `cross3_rows_48` Cartesian rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
Cartesian fixture beyond `cross3_rows_40` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row emits a 48x48 Cartesian fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger Cartesian fixtures
beyond 48 rows, larger skew/fanout distributions beyond 24x24, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage P: close one larger bounded skew/fanout evidence gap.

- inventory current multi-way skew/fanout benchmark/docs coverage
- add a larger skew/fanout extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage P status, 2026-05-29: completed bounded `hot_key_32x32` skew/fanout rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
skew/fanout fixture beyond `hot_key_24x24` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row matches a 32x32 fanout fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger skew/fanout
distributions beyond 32x32, larger Cartesian fixtures beyond 48 rows, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage Q: close one larger bounded Cartesian evidence gap.

- inventory current multi-way Cartesian benchmark/docs coverage
- add a larger Cartesian extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage Q status, 2026-05-29: completed bounded `cross3_rows_56` Cartesian rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
Cartesian fixture beyond `cross3_rows_48` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row emits a 56x56 Cartesian fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger Cartesian fixtures
beyond 56 rows, larger skew/fanout distributions beyond 32x32, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage R: close one larger bounded skew/fanout evidence gap.

- inventory current multi-way skew/fanout benchmark/docs coverage
- add a larger skew/fanout extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage R status, 2026-05-29: completed bounded `hot_key_40x40` skew/fanout rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
skew/fanout fixture beyond `hot_key_32x32` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row matches a 40x40 fanout fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger skew/fanout
distributions beyond 40x40, larger Cartesian fixtures beyond 56 rows, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage S: close one larger bounded Cartesian evidence gap.

- inventory current multi-way Cartesian benchmark/docs coverage
- add a larger Cartesian extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage S status, 2026-05-29: completed bounded `cross3_rows_64` Cartesian rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
Cartesian fixture beyond `cross3_rows_56` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row emits a 64x64 Cartesian fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger Cartesian fixtures
beyond 64 rows, larger skew/fanout distributions beyond 40x40, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage T: close one larger bounded skew/fanout evidence gap.

- inventory current multi-way skew/fanout benchmark/docs coverage
- add a larger skew/fanout extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage T status, 2026-05-29: completed bounded `hot_key_48x48` skew/fanout rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
skew/fanout fixture beyond `hot_key_40x40` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row matches a 48x48 fanout fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger skew/fanout
distributions beyond 48x48, larger Cartesian fixtures beyond 64 rows, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage U: close one larger bounded Cartesian evidence gap.

- inventory current multi-way Cartesian benchmark/docs coverage
- add a larger Cartesian extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage U status, 2026-06-02: completed bounded `cross3_rows_72` Cartesian rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
Cartesian fixture beyond `cross3_rows_64` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row emits a 72x72 Cartesian fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger Cartesian fixtures
beyond 72 rows, larger skew/fanout distributions beyond 48x48, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage V: close one larger bounded skew/fanout evidence gap.

- inventory current multi-way skew/fanout benchmark/docs coverage
- add a larger skew/fanout extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage V status, 2026-06-02: completed bounded `hot_key_56x56` skew/fanout rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
skew/fanout fixture beyond `hot_key_48x48` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row matches a 56x56 fanout fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger skew/fanout
distributions beyond 56x56, larger Cartesian fixtures beyond 72 rows, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage W: close one larger bounded Cartesian evidence gap.

- inventory current multi-way Cartesian benchmark/docs coverage
- add a larger Cartesian extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage W status, 2026-06-02: completed bounded `cross3_rows_80` Cartesian rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
Cartesian fixture beyond `cross3_rows_72` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row emits an 80x80 Cartesian fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger Cartesian fixtures
beyond 80 rows, larger skew/fanout distributions beyond 56x56, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage X: close one larger bounded skew/fanout evidence gap.

- inventory current multi-way skew/fanout benchmark/docs coverage
- add a larger skew/fanout extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage X status, 2026-06-02: completed bounded `hot_key_64x64` skew/fanout rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
skew/fanout fixture beyond `hot_key_56x56` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row matches a 64x64 fanout fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger skew/fanout
distributions beyond 64x64, larger Cartesian fixtures beyond 80 rows, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage Y: close one larger bounded Cartesian evidence gap.

- inventory current multi-way Cartesian benchmark/docs coverage
- add a larger Cartesian extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage Y status, 2026-06-02: completed bounded `cross3_rows_88` Cartesian rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
Cartesian fixture beyond `cross3_rows_80` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row emits an 88x88 Cartesian fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger Cartesian fixtures
beyond 88 rows, larger skew/fanout distributions beyond 64x64, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

Stage Z: close one larger bounded skew/fanout evidence gap.

- inventory current multi-way skew/fanout benchmark/docs coverage
- add a larger skew/fanout extension only if it stays local-review-sized
- publish focused benchmark evidence and keep default guardrails unchanged

Stage Z status, 2026-06-02: completed bounded `hot_key_72x72` skew/fanout rows
in `subscription/bench_test.go` and published the focused `-count=10` rows in
`docs/performance-envelopes.md`. The slice extends the existing 3-relation
skew/fanout fixture beyond `hot_key_64x64` for table-shaped projection,
`COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)`; one
changed endpoint row matches a 72x72 fanout fragment. Runtime semantics and
default multi-way join guardrails stayed unchanged. Larger skew/fanout
distributions beyond 72x72, larger Cartesian fixtures beyond 88 rows, larger
aggregate-function self-alias distributions, relation counts beyond the
bounded 5-relation chain, and app-derived workload distributions remain
evidence backlog.

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
   Completed through the Stage Z advisory envelope.
2. Add benchmark dimensions that are cheap and deterministic.
   Completed for the current synthetic multi-way campaign; further size-only
   rows require an explicit resume trigger.
3. Add type/index gauntlet module or root-runtime test fixture.
   Completed for hosted Go/protocol paths.
4. Add generated TypeScript coverage for the matrix only if it can run in a
   deterministic local gate.
   Completed for package-level generated decoder execution over the flat-kind
   canary shape.
5. Review multi-way join limits after evidence exists.
   Completed for the current envelope: defaults stay unlimited.
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

Close-out status, 2026-06-02:

- Synthetic multi-way relation-shape and aggregate evidence is complete for
  the current local advisory envelope through Stage Z.
- Default multi-way join policy is decided for now: keep defaults unlimited and
  rely on app-owned opt-in guardrails.
- Current aggregate semantics have focused correctness and performance
  evidence; semantic expansion remains product backlog.
- Generated TypeScript decoding, hosted TypeScript client execution, and
  backup/restore for the current flat-kind canary shape have deterministic
  local gates.
- The RC taskboard `open_tasks_live` workload-derived subscription delta has a
  deterministic local benchmark and published raw evidence. The same hosted
  protocol path has a bounded two-subscriber correctness gate. A bounded
  hosted timing row now covers the existing chat declared-read insert/delete
  reducer cycle through a strict-auth local WebSocket caller and one
  subscriber. Broader workload-derived fanout timing and distribution benchmark
  evidence remains backlog until a real workload or release gate needs it.
