# Read Surface Reference

Status: current v1 reference note
Scope: choosing among Shunter's app-facing read surfaces.

Use this page as the compact support reference for Shunter's current v1 read
surfaces.

## Which Read Should I Use?

| Need | Use |
| --- | --- |
| In-process state assertion or admin read | `Runtime.Read` |
| Named request/response read in the app contract | `Module.Query` plus `Runtime.CallQuery` |
| Named live read in the app contract | `Module.View` plus `Runtime.SubscribeView` |
| External client request/response read | Protocol one-off query or declared query |
| External client live updates | Protocol raw subscription or declared view |
| Generated client helper | Declared query or declared view |

## Runtime.Read

`Runtime.Read` exposes callback-scoped committed-state access through
`LocalReadView`.

Available operations:

- `TableScan`
- `GetRow`
- `SeekIndex`
- `SeekIndexRange`
- `RowCount`

The view is valid only during the callback.

## Declared Query

Declared queries are named request/response reads. They can carry SQL,
optional parameter schemas, permission metadata, read-model metadata, and
migration metadata.

Use them when a read is stable enough to expose to clients or review in a
contract.

## Declared View

Declared views are named live reads. They can carry SQL, permission metadata,
optional parameter schemas, read-model metadata, and migration metadata.

Use them for stable subscriptions and generated client surfaces.

## Declared-Read Parameters

Declared queries and views may use app parameters in executable SQL with
placeholders such as `:topic`. Each app placeholder must match a column in the
schema attached with `WithQueryParameters` or `WithViewParameters`; `sender` is
reserved for caller identity and cannot be declared as an app parameter.

Local callers pass ordered `types.ProductValue` values with
`WithDeclaredReadParameters`. Protocol clients send encoded parameter bytes
only through protocol v2 declared-read request messages. Generated TypeScript
helpers expose typed params objects and hide the BSATN product encoding.

No-parameter declared reads keep their existing local, protocol v1, and
generated-helper call shapes.

## Metadata-Only Declarations

For both queries and views, empty SQL means the declaration is metadata-only.
It is exported in contracts but cannot be executed.

## Permissions And Visibility

Permissions decide whether a caller may use a read surface. Visibility filters
narrow which rows that caller can see.

Use both when a surface should be admitted only for certain callers and then
row-filtered by identity.

Table read policies apply to external raw SQL table reads. Declared queries and
views should carry their own `PermissionMetadata` when they are app-facing
contract surfaces.

## SQL Compatibility

Shunter's SQL support is intentionally narrow and read-oriented. Supported
shapes differ by read surface:

- Protocol one-off raw SQL supports committed-snapshot single-table reads,
  bounded joins and multi-way joins, projections and aliases, `COUNT`/`SUM`
  aggregates including `COUNT(DISTINCT column)`, `ORDER BY`, `LIMIT`, and
  `OFFSET`.
- Declared queries use the one-off executor through `QueryDeclaration.SQL`,
  `Runtime.CallQuery`, and the protocol declared-query path. They may expose
  private tables when declaration permission allows the caller. Empty SQL is
  metadata-only. Declared app placeholders are allowed only when backed by the
  declaration parameter schema.
- Raw protocol subscriptions support table-shaped single-table and join reads.
  Table read policies and visibility filters apply. Raw subscriptions reject
  projections, aggregates, `ORDER BY`, `LIMIT`, and `OFFSET`.
- Declared live views support table-shaped reads, joins, self joins, cross
  joins, multi-way joins, projections over the emitted relation, and
  `COUNT`/`SUM` aggregate views, including join and multi-way aggregate views.
  `COUNT(DISTINCT column)` is supported. Aggregate aliases must use `AS`.
  `ORDER BY`, `LIMIT`, and `OFFSET` shape the initial snapshot only; Shunter
  does not maintain top-N/windowed live view membership after commits.
  Non-aggregate post-commit delivery remains row deltas over matching rows, and
  aggregate views emit replacement aggregate rows when the aggregate changes.
  Declared app placeholders follow the same parameter rules as declared
  queries.
- Local ad hoc raw SQL is out of scope for v1. Use `Runtime.Read`,
  `Runtime.CallQuery`, or `Runtime.SubscribeView` instead.
