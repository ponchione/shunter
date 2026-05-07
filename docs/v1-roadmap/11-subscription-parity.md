# Subscription Parity Roadmap

Status: active plan
Owner: unassigned
Scope: work required for Shunter subscriptions to reach SpacetimeDB-level
observable capability while keeping Shunter's own implementation and API
shape.

## Goal

Shunter should provide subscription behavior that is equivalent in capability to
the SpacetimeDB reference implementation for the query shapes Shunter chooses to
support:

- app authors express live reads as SQL, declared live views, generated client
  helpers, or root runtime APIs
- subscription admission validates unsupported or unsafe shapes before runtime
  registration
- initial rows and live deltas are correct under inserts, deletes, joins,
  visibility rules, and caller-specific filters
- candidate pruning avoids evaluating unrelated live queries without changing
  observable results
- protocol subscribe, unsubscribe, error, and fanout behavior is stable enough
  to build clients on top

The goal is capability parity, not source parity. `reference/SpacetimeDB/` is
read-only research material. Do not copy reference source into Shunter.

## Non-Goals

- Do not reintroduce fixed-hop path-edge types such as `JoinPath3Edge`.
- Do not expose compatibility shims for removed subscription internals.
- Do not require a public low-level pruning-index API unless a separate design
  identifies real external users.
- Do not turn raw subscription SQL into the full query SQL surface.
- Do not implement post-v1 SQL classes from `09-sql-read-scope.md` as part of
  this parity track.
- Do not broaden docs or API commitments ahead of tests.

## Reference Lesson

The relevant SpacetimeDB pattern is plan-centric:

- subscription SQL is compiled into subscription plans
- admission rejects plans that require unsupported or non-indexed live joins
- delta maintenance is expressed as insert/delete fragments over the plan
- pruning is a runtime optimization behind subscription/query APIs
- user-facing APIs are query/subscription builders, protocol messages, and
  generated clients rather than path-edge index types

Shunter does not need to match SpacetimeDB's crate structure or wire format to
match this capability. Shunter does need the same kind of end-to-end guarantees:
supported query shapes compile, register, initialize, update, prune, and clean
up correctly.

## Current Shunter State

Shunter already has substantial subscription infrastructure:

- `subscription.Manager` owns register, unregister, evaluate, and fanout flows.
- `PruningIndexes` includes value, range, direct join edge, direct join range
  edge, table fallback, and generic path traversal indexes.
- Generic traversal path indexes are implemented by
  `joinPathTraversalEdge`, `joinPathTraversalIndex`, and
  `joinRangePathTraversalIndex`.
- Fixed-hop path-edge wrappers have been removed. Internal tests now assert
  against generic traversal edges directly.
- Raw subscriptions, declared live views, one-off reads, declared queries, and
  protocol admission already have broad coverage in the current repo.
- Visibility filtering, caller identity, query hashing, initial rows, and live
  deltas have tests across important v1 read surfaces.
- Multi-way join pruning covers many shapes, including mixed equality/range
  filters, OR branches, required remote filters, repeated aliases, and
  multi-hop non-key-preserving traversal paths.

The main remaining gap is not one missing index structure. The gap is that
subscription capability is still spread across parser/admission code, predicate
placement, candidate collection, evaluation, declared-read metadata, protocol
lifecycles, and tests. Parity requires treating those pieces as one coherent
plan-to-delta system.

## Target Architecture

The long-term shape should be:

1. A supported live-read query is admitted through a single validation path.
2. Admission produces or references a normalized subscription plan.
3. The plan records:
   - emitted table or emitted projection shape
   - table aliases and table ids read by the subscription
   - required indexes and join-column contracts
   - local filters, remote filters, and join conditions
   - caller-specific parameters such as `:sender`
   - aggregate/order/window metadata when allowed by the read surface
4. Placement derives pruning index entries from the normalized plan.
5. Evaluation derives initial rows and live deltas from the same normalized
   semantics.
6. Candidate collection is a conservative optimization: it may over-include
   queries, but it must never omit a query whose result can change.
7. Unregister and disconnect remove every registry and pruning reference.

This does not require exposing `SubscriptionPlan` as a public type immediately.
It does require a clear internal boundary so admission, placement, evaluation,
and tests stop rediscovering query structure independently.

## Workstream 1: Support Matrix And Contracts

Purpose: define exactly what parity means for each live-read surface.

Steps:

1. Create a subscription-specific support matrix that references, but does not
   duplicate, `09-sql-read-scope.md`.
2. Split the matrix by surface:
   - raw `SubscribeSingle`
   - raw `SubscribeMulti`
   - declared live views
   - local/runtime subscription APIs
   - generated TypeScript live-read helpers once the SDK lands
3. For each surface, classify query features as:
   - supported and tested
   - rejected with stable diagnostics
   - accepted but scan/fallback-limited
   - post-v1 non-goal
4. Track at least these feature rows:
   - whole-table live read
   - single-table equality filter
   - single-table range filter
   - `!=` / `<>` represented through ranges
   - `IS NULL` / `IS NOT NULL` if supported
   - `AND`, `OR`, and parentheses
   - mixed equality/range OR filters
   - `:sender`
   - visibility filters
   - two-table indexed joins
   - cross joins accepted by declared live views
   - multi-way joins
   - repeated table aliases
   - column equality filters
   - non-key-preserving multi-hop join paths
   - projections
   - aggregates
   - `ORDER BY`, `LIMIT`, and `OFFSET`
5. For each supported row, list required tests:
   - parser/admission
   - initial rows
   - live insert delta
   - live delete delta
   - candidate pruning safety
   - unregister cleanup
   - protocol response shape when applicable

Acceptance criteria:

- The matrix makes every supported raw subscription and declared live-view
  feature explicit.
- Every explicit unsupported feature has a negative test at admission.
- Docs do not imply raw subscriptions support richer declared-live-view shapes.

Recommended verification:

```bash
rtk go test ./query/... ./protocol ./subscription
rtk git diff --check
```

## Workstream 2: Plan-Centric Admission

Purpose: make admission the single source of truth for live-read semantics.

Steps:

1. Identify the current paths that build subscription predicates and declared
   live-view metadata.
2. Introduce an internal normalized plan representation if the existing
   predicate tree cannot carry enough information. It should describe query
   semantics, not physical index implementation details.
3. Normalize table aliases so repeated-table joins are represented without
   losing alias identity.
4. Normalize local filters and remote filters into structures placement and
   evaluation can share.
5. Normalize join conditions into an ordered graph representation:
   - relation indexes
   - table ids
   - aliases
   - left/right columns
   - whether each hop can use an index
6. Carry caller-specific state explicitly:
   - `:sender`
   - permission context
   - visibility-filter dependencies
7. Carry result-shape state explicitly:
   - table-shaped result
   - projected columns
   - aggregate columns
   - ordering
   - limit/offset when supported
8. Fail admission when the plan requires unsupported live semantics, including:
   - unsupported SQL class
   - missing required join index for a live join
   - unsupported projection/aggregate/order/window on raw subscriptions
   - ambiguous or invalid alias references
   - incompatible visibility or permission context
9. Keep diagnostics stable across local, `SubscribeSingle`, and
   `SubscribeMulti` paths.

Acceptance criteria:

- Placement and evaluation can be explained from the normalized plan.
- Admission errors happen before executor registration.
- Unsupported SQL errors include enough context for protocol clients.

Recommended verification:

```bash
rtk go test ./query/... ./protocol ./subscription
rtk go vet ./query/... ./protocol ./subscription
```

## Workstream 3: Delta Fragment Evaluation

Purpose: make live deltas correct for every supported query shape.

Steps:

1. Inventory every current evaluator path:
   - single-table predicates
   - joins
   - cross joins
   - multi-way joins
   - projections
   - aggregates
   - declared live views
2. For each supported shape, define the delta equations in implementation
   terms:
   - changed left rows against committed right rows
   - changed right rows against committed left rows
   - same-transaction changed rows on both sides
   - deletes against post-commit state where needed
   - inserts and deletes for aggregate result changes
3. Ensure the evaluator handles all changed-table positions in multi-way joins,
   not only endpoint changes.
4. Ensure non-key-preserving path traversals consider:
   - committed intermediate rows
   - same-transaction inserted intermediate rows
   - same-transaction deleted intermediate rows
   - committed RHS rows
   - same-transaction RHS rows
5. Ensure projection and aggregate deltas preserve the same output shape as
   initial rows.
6. Make deduplication rules explicit for joins that can reach the same emitted
   row through multiple paths.
7. Add tests comparing incremental output against a fresh post-commit
   evaluation for each supported shape.
8. Add tests where changed rows do not affect the subscription, proving no
   spurious deltas are emitted.

Acceptance criteria:

- Every supported live-read shape has an initial-row test and at least one
  insert/delete delta test.
- Complex live predicates have property or differential tests comparing pruned
  incremental results to baseline evaluation.
- Candidate pruning may over-include, but emitted deltas match baseline.

Recommended verification:

```bash
rtk go test ./subscription
rtk go test ./protocol
rtk go test ./...
```

## Workstream 4: Index Contracts

Purpose: require the indexes needed for safe and efficient live maintenance.

Steps:

1. Define index requirements for each live-read class:
   - single-table value/range filters
   - direct join filter edges
   - direct join range edges
   - join existence edges
   - generic path traversal edges
   - declared live-view aggregate joins
2. Decide where scan fallback is allowed:
   - raw subscriptions should be conservative
   - declared live views may allow more only if performance and correctness are
     documented
   - one-off reads can be broader than live reads
3. Add admission checks for required join-column indexes.
4. Add admission checks for path traversal hops:
   - every seek column used during traversal must have an index
   - RHS join column must have an index
   - relation alias identity must be preserved
5. Track required indexes in manager state so delta views include every column
   needed for evaluation.
6. Add cleanup tests proving index reference counts drop on unregister and
   disconnect.
7. Add tests that removing one of several subscriptions sharing an index does
   not remove the index from active subscription tracking too early.

Acceptance criteria:

- Unsupported unindexed live joins fail at admission or intentionally fall back
  in a documented way.
- Every accepted live join has the columns needed to evaluate deltas.
- Unregistering all subscriptions leaves no index, registry, or pruning state.

Recommended verification:

```bash
rtk go test ./subscription
rtk go test ./protocol
```

## Workstream 5: Generic Candidate Pruning

Purpose: keep pruning hop-count agnostic and behavior-preserving.

Steps:

1. Keep direct join and generic path traversal indexes as internal pruning
   structures.
2. Do not recreate fixed-hop tables for two-hop through eight-hop paths.
3. Represent traversal paths with one generic edge descriptor:
   - ordered tables
   - per-hop source columns
   - per-hop target columns
   - RHS filter column
   - validated hop count up to `joinPathTraversalMaxHops`
4. Derive value and range traversal placements from normalized plans.
5. Ensure candidate collection traverses paths through committed and changed
   rows consistently.
6. Prove all supported hop counts through tests that vary length but share the
   same generic helper.
7. Add upper-bound tests at `joinPathTraversalMaxHops`.
8. Add rejection/fallback tests beyond the supported hop limit.
9. Add mismatch tests proving candidate collection prunes when endpoint filters
   do not match.
10. Add overlap tests proving candidate collection includes when intermediate
    rows are inserted or deleted in the same transaction.

Acceptance criteria:

- No public `JoinPathNEdge` or `JoinRangePathNEdge` API exists.
- Tests construct generic traversal edges, not fixed-hop wrappers.
- Candidate correctness is unchanged for current supported path lengths.
- New path lengths up to the generic limit require no new index type.

Recommended verification:

```bash
rtk go doc ./subscription
rtk go test ./subscription
rtk go tool staticcheck ./subscription
```

## Workstream 6: Protocol Lifecycle Parity

Purpose: make observable subscribe and unsubscribe behavior stable.

Steps:

1. Pin `SubscribeSingle`, `SubscribeMulti`, and declared-view subscribe
   admission behavior for:
   - success
   - parse failure
   - unsupported SQL
   - missing index requirement
   - auth/read-policy rejection
   - duplicate query id
   - initial row limit
2. Pin response envelopes for:
   - subscribe applied
   - unsubscribe applied
   - subscription error
   - multi-query partial failure, if supported
3. Confirm request id and query id propagation in every response.
4. Confirm initial rows are sent before transaction updates for a newly applied
   subscription.
5. Confirm unsubscribe removes the query before later transaction updates.
6. Confirm disconnect removes all query state and pruning state.
7. Confirm backpressure behavior:
   - send buffer full
   - dropped client signal
   - fanout worker cleanup
8. Confirm ordering constraints with reducer calls and transaction updates.
9. Keep golden wire tests current for every stable protocol shape.

Acceptance criteria:

- Protocol clients can implement subscribe/unsubscribe without relying on
  internal manager details.
- Failure behavior is stable enough for generated clients.
- No rejected subscription reaches executor registration.

Recommended verification:

```bash
rtk go test ./protocol
rtk go test ./subscription
rtk go test ./...
```

## Workstream 7: Auth, Caller, And Visibility Semantics

Purpose: ensure caller-specific live results are correct.

Steps:

1. Treat `:sender` as part of query shape and query hashing where needed.
2. Ensure initial rows and live deltas evaluate with the same caller identity.
3. Ensure visibility filters are applied before:
   - one-off query results
   - raw subscription initial rows
   - declared live-view initial rows
   - live insert deltas
   - live delete deltas
4. Ensure private table/read-policy admission happens before registration.
5. Ensure visibility changes caused by writes produce correct deltas for
   affected subscriptions.
6. Add tests with multiple connections and different identities sharing the same
   query text.
7. Add tests proving caller-specific query hashes do not incorrectly merge
   incompatible subscriptions.
8. Add tests proving equivalent caller-independent subscriptions can still share
   evaluation.

Acceptance criteria:

- Caller-specific subscriptions cannot leak rows across identities.
- Visibility filters affect candidate selection and final evaluation correctly.
- Query sharing is safe under auth and visibility constraints.

Recommended verification:

```bash
rtk go test ./protocol ./subscription
rtk go test ./...
```

## Workstream 8: Declared Live Views And Codegen

Purpose: make the recommended app-facing live-read path reliable.

Steps:

1. Keep declared live views as the richer live-read surface compared with raw
   subscriptions.
2. Ensure declared live-view contract metadata includes:
   - emitted table
   - projection columns
   - aggregate columns
   - order/limit/offset metadata where supported
   - auth/permission metadata
3. Ensure contract hashes change when live-view shape changes.
4. Ensure generated clients can subscribe without handwritten protocol code.
5. Add typed decoding for generated TypeScript helpers when the SDK runtime
   lands.
6. Add declared live-view tests for:
   - initial rows
   - table-shaped deltas
   - projection deltas
   - aggregate deltas
   - unsubscribe
   - reconnect or resubscribe behavior
7. Add compatibility tests that generated fixtures remain stable across
   supported metadata changes.

Acceptance criteria:

- Declared live views are the documented normal path for app-authored live
  reads.
- Contract/codegen metadata is sufficient for clients to decode results.
- Raw SQL remains an escape hatch, not the primary typed experience.

Recommended verification:

```bash
rtk go test ./codegen ./protocol ./subscription ./...
```

## Workstream 9: Differential And Property Testing

Purpose: prove pruning and incremental evaluation are behavior-preserving.

Steps:

1. Keep a baseline evaluator that ignores pruning and evaluates all registered
   queries directly.
2. For each random changeset, compare:
   - pruned candidate evaluation
   - baseline all-query evaluation
   - fresh post-commit initial evaluation where applicable
3. Expand random predicate generation to include:
   - equality
   - range
   - `!=`
   - mixed OR filters
   - direct joins
   - multi-way joins
   - repeated aliases
   - visibility filters
   - `:sender`
4. Add deterministic seeds for every prior bug.
5. Add metamorphic tests:
   - predicate branch order does not change results
   - equivalent range forms do not change results
   - unregister order does not leave indexes dirty
   - duplicate subscribers share results safely
6. Add same-transaction path tests:
   - only RHS changed
   - only first middle relation changed
   - only later middle relation changed
   - all path relations changed
   - deletes across path relations
7. Add negative property checks for unsupported shapes:
   - rejected before registration
   - stable error class
   - no dirty manager state after rejection

Acceptance criteria:

- Property tests cover both correctness and cleanup.
- Every found seed can be replayed deterministically.
- Baseline and pruned evaluation agree for all supported generated shapes.

Recommended verification:

```bash
rtk go test ./subscription
rtk go test ./protocol
rtk go test ./...
```

## Workstream 10: Performance And Operational Envelopes

Purpose: document what workloads Shunter can support credibly.

Steps:

1. Add benchmarks for:
   - raw subscription admission
   - declared live-view admission
   - initial snapshot for small, medium, and large tables
   - single-table live delta
   - direct join live delta
   - multi-way join live delta
   - generic path traversal candidate collection
   - aggregate live delta
   - fanout to many connections
2. Include data size, schema, indexes, command, machine notes, and commit hash
   in benchmark docs.
3. Decide which thresholds are CI gates and which are release-note guidance.
4. Document index requirements in app-author terms.
5. Document fallback behavior when a subscription cannot be pruned narrowly.
6. Add runbook notes for high fanout and expensive initial snapshots.

Acceptance criteria:

- Users know the supported live-read workload envelope.
- Expensive query shapes have documented index guidance.
- Performance regressions are measurable, not anecdotal.

Recommended verification:

```bash
rtk go test ./subscription
rtk go test ./...
```

Run benchmark commands explicitly when changing benchmarked code paths.

## Query Shape Matrix To Build

Use this as the starting table for implementation tracking. Each row should
eventually link to tests.

| Shape | Raw Subscribe | Declared Live View | Initial Rows | Deltas | Pruning | Admission |
| --- | --- | --- | --- | --- | --- | --- |
| Whole table | supported | supported | required | required | table fallback | allow |
| Single-table equality | supported | supported | required | required | value index | allow |
| Single-table range | supported | supported | required | required | range index | allow |
| Single-table `!=` | supported if normalized | supported if normalized | required | required | range index | allow |
| Mixed equality/range OR | supported if covered | supported if covered | required | required | value/range union | allow |
| `:sender` filter | supported | supported | required | required | caller-safe | allow |
| Visibility filter | supported | supported | required | required | caller-safe | allow |
| Two-table indexed join | supported | supported | required | required | join edge/existence/table | require indexes |
| Two-table remote value filter | supported | supported | required | required | join edge | require RHS join index |
| Two-table remote range filter | supported | supported | required | required | join range edge | require RHS join index |
| Multi-way key-preserving join | supported if indexed | supported if indexed | required | required | direct/transitive edges | require indexes |
| Multi-way non-key-preserving path | supported if indexed | supported if indexed | required | required | generic path traversal | require path indexes |
| Repeated aliases | supported if covered | supported if covered | required | required | alias-aware placement | allow only covered aliases |
| Projection | reject raw | supported | required | required | same as predicate | reject/allow by surface |
| Aggregate `COUNT`/`SUM` | reject raw | supported subset | required | required | same as predicate | reject/allow by surface |
| `ORDER BY` initial snapshot | reject raw | supported subset | required | no ordered live diff unless defined | same as predicate | reject/allow by surface |
| `LIMIT`/`OFFSET` initial snapshot | reject raw | supported subset | required | no windowed live diff unless defined | same as predicate | reject/allow by surface |
| Grouped aggregate | post-v1 | post-v1 | none | none | none | reject |
| Outer join | post-v1 | post-v1 | none | none | none | reject |
| Subquery | post-v1 | post-v1 | none | none | none | reject |

## Milestones

### Milestone A: Documentation And Matrix

Deliverables:

- this roadmap
- a filled support matrix with links to existing tests
- explicit list of unsupported subscription SQL classes

Exit criteria:

- no ambiguous subscription feature claims remain in app-facing docs
- each matrix row has an owner status: done, partial, or missing

### Milestone B: Admission Consolidation

Deliverables:

- internal normalized live-read plan or equivalent shared structure
- admission checks for unsupported live shapes
- stable diagnostics across local and protocol paths

Exit criteria:

- rejected subscriptions leave no manager or executor state
- accepted subscriptions expose required indexes and table dependencies

### Milestone C: Generic Pruning Completion

Deliverables:

- no fixed-hop path-edge shims
- generic traversal tests through the supported hop limit
- candidate collection tests for committed and same-transaction path rows

Exit criteria:

- path-edge behavior is hop-count agnostic
- pruning safety property tests include generic traversal paths

### Milestone D: Delta Fragment Coverage

Deliverables:

- baseline-vs-incremental tests for every supported shape
- insert/delete tests for all changed-table positions in joins
- aggregate/projection delta tests for declared live views

Exit criteria:

- supported live-read shapes produce the same result as fresh evaluation

### Milestone E: Protocol And Client Readiness

Deliverables:

- protocol golden coverage for all stable subscription envelopes
- generated client live-view helpers once SDK runtime exists
- reconnect/unsubscribe/backpressure tests

Exit criteria:

- a normal client can subscribe, receive initial rows and deltas, unsubscribe,
  and handle errors without internal knowledge

### Milestone F: Performance Envelope

Deliverables:

- benchmark coverage for admission, initial rows, deltas, pruning, and fanout
- documented index guidance and workload limits

Exit criteria:

- release notes can state supported subscription workload expectations

## Open Decisions

1. Should Shunter expose an internal-looking `SubscriptionPlan` for advanced
   users, or keep plan/pruning APIs entirely package-internal for v1?
2. Which declared live-view aggregate shapes are v1-stable for deltas, not just
   initial rows?
3. Are raw subscriptions permanently narrower than declared live views, or only
   narrower for v1?
4. Where should scan fallback be allowed for live reads, if anywhere?
5. What exact diagnostics should missing live-join indexes return?
6. How much generated TypeScript live-view decoding is required before v1?
7. Which subscription benchmarks should fail CI versus remain advisory?

## Done So Far

- Generic traversal indexes exist:
  - `joinPathTraversalEdge`
  - `joinPathTraversalIndex`
  - `joinRangePathTraversalIndex`
- Fixed-hop path-edge compatibility wrappers were removed in commit
  `0a43bbd subscription: remove fixed path edge wrappers`.
- `PruningIndexes` now keeps only generic traversal path indexes for path-edge
  pruning.
- Tests that previously asserted fixed path-edge placement now construct
  generic traversal edges directly.
- `rtk go doc ./subscription` no longer lists fixed `JoinPathNEdge` or
  `JoinRangePathNEdge` types.

## Standard Verification

For subscription parity implementation slices, run targeted commands first:

```bash
rtk go fmt ./subscription ./protocol ./query/...
rtk go test ./subscription
rtk go test ./protocol
rtk go vet ./subscription ./protocol
rtk git diff --check
```

When behavior, admission, protocol, or shared read-surface contracts change,
expand to:

```bash
rtk go test ./...
rtk go tool staticcheck ./...
```

When exported APIs, protocol payloads, contract metadata, codegen, or docs
change, also run the package-specific golden/compatibility tests that own those
surfaces.

## Tracking Rules

- Keep this document implementation-facing.
- Update this document when a milestone lands or an open decision is resolved.
- Do not add speculative code structure just because it appears in the roadmap.
- Prefer tests that assert observable behavior over tests that assert internal
  placement details.
- Internal placement tests are still valuable when they prove pruning safety,
  cleanup, or admission contracts.
- Keep SpacetimeDB references as context. Do not copy source from
  `reference/SpacetimeDB/`.
