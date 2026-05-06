# SQL And Read Scope

Status: open
Owner: unassigned
Scope: the amount of SQL and declared-read behavior Shunter actually needs for
v1.

## Goal

Define a deliberately small SQL/read surface that supports real Shunter apps
without turning v1 into a broad SQL database project.

Shunter's write model is reducer-owned. SQL should primarily serve reads,
declared queries, and live subscriptions. Mutation through SQL is not required
for v1.

## Current State

Shunter already has a meaningful SQL/read implementation:

- one-off raw SQL
- declared queries
- raw subscriptions
- declared live views
- predicates with visibility filtering
- joins and multi-way joins
- projections for query surfaces
- aggregates for query surfaces
- ordering, limits, and offsets for query surfaces
- narrower live subscription behavior

The risk is not lack of parser ambition. The risk is accepting SQL shapes whose
runtime, auth, visibility, or live delta semantics are not precise enough for a
stable v1 contract.

## SpacetimeDB Reference Lesson

SpacetimeDB does not use one universal SQL surface for every operation.
Subscription SQL is intentionally narrower than one-off query SQL, and
mutations are normally reducer-owned. Shunter should follow that shape.

Useful SpacetimeDB lessons:

- keep live subscription SQL smaller than query SQL
- require clear indexed access for expensive live joins
- make generated/typed client reads the common path
- keep reducers as the normal write boundary

Do not copy SpacetimeDB's wire protocol, DML surface, or language/runtime model
as a v1 requirement.

## Recommended v1 Query SQL

Support for one-off raw SQL and declared queries should include:

- `SELECT *`
- `SELECT table.*`
- explicit column projections
- column aliases if already supported consistently
- single-table predicates with:
  - `=`, `!=`, `<>`, `<`, `<=`, `>`, `>=`
  - `IS NULL`
  - `IS NOT NULL`
  - boolean literals
  - integer, string, bytes/hex, UUID, timestamp, and nullable literals where
    the type system supports them
  - `AND`, `OR`, and parentheses
  - `:sender`
- single-table `ORDER BY`, including multi-column order where supported
- `LIMIT`
- `OFFSET`
- two-table inner joins
- multi-way inner joins
- cross joins only when filters or limits make the intended behavior explicit
- `COUNT(*)`
- `COUNT(column)`
- `COUNT(DISTINCT column)`
- `SUM(numeric_column)`
- visibility filtering before query evaluation

Index use should be opportunistic for queries, with scan fallback allowed when
the documented performance envelope permits it.

## Recommended v1 Live Read SQL

Support for raw subscriptions and declared live views should be narrower:

- whole-table subscriptions
- table-shaped join subscriptions
- predicates using the same stable predicate subset as queries
- `:sender`
- visibility filtering before matching and delta delivery
- two-table joins where join semantics and index requirements are documented
- multi-way table-shaped joins only under documented size/index constraints
- declared view projections only if initial rows, deltas, contract export, and
  codegen all agree on the exact projected row shape

Do not include live support for these in v1 unless a separate design proves the
delta semantics:

- live `ORDER BY`
- live `LIMIT`
- live `OFFSET`
- live aggregates
- live grouped results
- live arbitrary expression projections

## Explicit v1 Non-Goals

Do not implement for v1:

- SQL `INSERT`
- SQL `UPDATE`
- SQL `DELETE`
- `GROUP BY`
- `HAVING`
- arbitrary scalar functions
- arithmetic expressions as a general feature
- subqueries
- `UNION`, `INTERSECT`, or `EXCEPT`
- outer joins
- natural joins
- recursive queries
- JSON path/query operators
- full-text search
- transaction control SQL
- `SET` or `SHOW`
- SQL procedures

Reducers, declared reads, and generated clients should stay the primary
application model.

## Decisions To Make

1. Decide the exact grammar supported by each read surface.
2. Decide whether declared queries are always a subset of one-off query SQL or
   whether they may add metadata-only declarations.
3. Decide whether declared live views support projections in v1.
4. Decide index requirements for raw/live joins.
5. Decide whether query scan fallback is allowed by default.
6. Decide how unsupported SQL errors are represented in local and protocol APIs.
7. Decide whether raw SQL remains documented as an escape hatch while generated
   declared reads are the recommended app path.

## Implementation Work

- Create a read-surface support matrix in docs.
- Add parser/planner tests for every supported and rejected shape.
- Add runtime tests that prove auth and visibility are applied before query
  evaluation and live delivery.
- Add protocol tests for unsupported SQL errors.
- Add contract/codegen tests for declared query and declared view result shapes.
- Add performance tests for expensive supported shapes.
- Remove or label docs that imply broader SQL support than the code guarantees.

## Verification

Run targeted query/protocol/subscription tests, then:

```bash
rtk go test ./...
rtk go vet ./...
```

For SQL scope work, include negative tests. Unsupported SQL should fail
predictably and with useful errors.

## Done Criteria

- A v1 read-surface matrix exists.
- Every supported SQL shape has tests.
- Every explicitly rejected SQL class has at least one negative test.
- Declared queries, declared live views, raw SQL, and raw subscriptions agree
  with the matrix.
- App-author docs recommend declared/generated reads first and raw SQL as an
  escape hatch.

