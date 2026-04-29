# V2.5 Task 08: Read Plan Visibility Expansion

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Depends on:
- Task 04 auth-aware raw SQL admission
- Task 06 protocol and codegen declared reads
- Task 07 visibility filter declarations

Objective: apply row-level visibility filters to every external read path
before one-off execution or subscription evaluation can leak data.

This is the highest-risk V2.5 implementation task. Read the full design first.

## Required Context

Read:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- `docs/features/V2.5/07-visibility-filter-declarations.md`

Inspect:

```sh
rtk go doc ./subscription.Predicate
rtk go doc ./subscription.Join
rtk go doc ./subscription.CrossJoin
rtk go doc ./subscription.Manager
rtk rg -n "Predicate|Tables\\(\\)|Join|CrossJoin|Alias|ProjectRight|evalQuery|Initial|Delta|RegisterSubscription" subscription
rtk rg -n "compileSQLQueryString|compileSQLPredicate|ProjectionColumns|Aggregate|Limit|UsesCallerIdentity" protocol
rtk rg -n "handleOneOffQuery|countOneOffMatches|evaluateOneOffJoin|sendOneOff" protocol
```

## Target Behavior

For every external read path:

- raw one-off query
- raw subscription
- named declared query
- named declared view

the runtime must apply visibility filters for each table relation before rows
participate in query evaluation.

If a table has multiple filters, OR them together.

If a table appears more than once in a self-join, apply filters independently
for each alias/relation occurrence.

If `AllowAllPermissions` is true, bypass row-level visibility filters.

## Non-Negotiable Leak Rule

Do not post-filter only projected rows.

This is insufficient:

```sql
SELECT public_table.*
FROM public_table
JOIN filtered_table
  ON public_table.owner = filtered_table.owner
```

The filtered table can leak information through join participation even though
its rows are not projected. Visibility must restrict `filtered_table` before
the join evaluates.

## Read Plan Direction

The existing `subscription.Predicate` tree may not preserve enough relation
context for full visibility expansion. Add or refactor toward a relation-aware
read plan that preserves:

- table ID
- source table name
- table alias
- projected/returned table
- join edges
- relation-local filters
- query-level filters
- whether caller identity is used
- visibility filters expanded per relation

Lower the expanded plan into existing predicates only where safe. Extend
subscription/one-off evaluation when the current predicate model cannot express
the required policy.

## Execution Paths

One-off reads:

- visibility expansion must happen before `countOneOffMatches`,
  `evaluateOneOffJoin`, row collection, projection, aggregate, and limit
  processing.
- limits apply after visibility filtering.
- aggregates count only visible rows.

Subscriptions:

- initial rows include only visible rows.
- post-commit deltas include only rows entering/leaving the caller-visible
  result.
- two clients with different identities may have different query hashes/plans
  when filters use `:sender`.
- existing `PredicateHashIdentities` behavior may need extension or replacement
  if visibility filters introduce caller-specific predicates.

Named declared reads:

- apply visibility by default after declaration permission succeeds.
- do not bypass visibility merely because the declaration SQL is module-owned.

## Tests To Add First

Add focused failing tests for:

- raw one-off returns only rows visible to caller
- raw subscription initial state returns only rows visible to caller
- raw subscription deltas include only caller-visible changes
- declared query applies visibility after declaration permission succeeds
- declared view applies visibility after declaration permission succeeds
- two clients with different identities see different rows for same SQL
- multiple filters for one table OR together
- self-join applies filters per alias
- join with filtered non-projected table does not leak through join
  participation
- aggregate one-off counts only visible rows
- limit applies after visibility filtering
- `AllowAllPermissions` bypasses visibility filters

## Validation

Run at least:

```sh
rtk go fmt ./protocol ./subscription ./query/sql . ./executor
rtk go test ./protocol -count=1
rtk go test ./subscription -count=1
rtk go test . -run 'Test.*(Visibility|Read|Query|View|Subscribe|Permission)' -count=1
rtk go vet ./protocol ./subscription ./query/sql . ./executor
```

Run full validation before marking complete:

```sh
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

## Completion Notes

When complete, update this file with:

- read plan/predicate changes
- exact visibility expansion strategy
- one-off/subscription behavior for caller-specific filters
- validation commands run
- any performance risks that remain outside the correctness work

Completed 2026-04-29.

Read plan / predicate changes:

- The Task 08 implementation expands validated visibility-filter metadata into
  the existing relation-aware `subscription.Predicate` execution plan.
- `protocol.VisibilityFilter` is the protocol-layer runtime metadata shape used
  by raw SQL and declared-read paths. Runtime metadata is mapped from stored
  `VisibilityFilterDescription` values.
- `protocol.CompileSQLQueryStringWithVisibility` compiles raw SQL and then
  calls `protocol.ApplyVisibilityFilters`; declared reads call
  `ApplyVisibilityFilters` after declaration permission succeeds.
- Single-table predicates are expanded as:
  `query_predicate AND (filter_1 OR filter_2 ...)`.
- Equi-joins are expanded by attaching per-relation visibility predicates to
  `subscription.Join.Filter`.
- `subscription.CrossJoin` now has an optional `Filter` field so cartesian
  plans can enforce the same per-relation visibility semantics in initial
  snapshots, one-off reads, deltas, validation, hashing, and active-column
  collection.

Visibility enforcement semantics:

- Visibility filters are applied to every external read path:
  raw one-off SQL, raw subscriptions, named declared queries, and named
  declared views.
- Base table raw-read admission is unchanged and still happens through the
  authorized schema lookup before visibility expansion.
- Declared reads still bypass base-table raw-read policy but do not bypass
  row-level visibility.
- Multiple filters for a table are ORed in stored declaration order.
- Self-joins retag each expanded filter per relation alias, so the left and
  right table occurrences are filtered independently.
- Join and cross-join filters are evaluated before row emission, projection,
  aggregate counting, limit handling, subscription initial rows, and
  post-commit deltas. This prevents a filtered non-projected table from leaking
  rows through join participation.
- `AllowAllPermissions` bypasses row-level visibility, matching the existing
  admin/dev bypass semantics for read authorization.
- Caller-specific filters using `:sender` are compiled with the caller identity
  and mark the compiled query as caller-identity-sensitive for subscription
  hashing.

Validation commands run:

```sh
rtk go fmt ./protocol ./subscription ./query/sql . ./executor
rtk go test ./protocol -run VisibilityExpansion -count=1
rtk go test . -run 'TestDeclared(Query|View)AppliesVisibility' -count=1
rtk go test ./subscription -run 'Test.*CrossJoin|TestEvalSelfEquiJoinWithAliasedWhere' -count=1
rtk go test ./protocol -count=1
rtk go test ./subscription -count=1
rtk go test . -run 'Test.*(Visibility|Read|Query|View|Subscribe|Permission)' -count=1
rtk go vet ./protocol ./subscription ./query/sql . ./executor
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

Task 09 assumptions and follow-up:

- Task 09 can treat visibility-filter execution as enforced for all external
  read surfaces.
- Visibility filters remain deliberately restricted to Task 07's validated
  single-table, table-shape SQL subset.
- The remaining performance risk is filtered cross joins: correctness uses a
  full pair evaluation when a `CrossJoin.Filter` is present. This is acceptable
  for Task 08 correctness but is a future optimization candidate if filtered
  cartesian plans become common.
