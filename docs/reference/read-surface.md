# Read Surface Reference

Status: rough draft
Scope: choosing among Shunter's app-facing read surfaces.

The authoritative support matrix is `docs/v1-compatibility.md`. This page is a
short decision guide.

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
permission metadata, read-model metadata, and migration metadata.

Use them when a read is stable enough to expose to clients or review in a
contract.

## Declared View

Declared views are named live reads. They can carry SQL, permission metadata,
read-model metadata, and migration metadata.

Use them for stable subscriptions and generated client surfaces.

## Metadata-Only Declarations

For both queries and views, empty SQL means the declaration is metadata-only.
It is exported in contracts but cannot be executed.

## Permissions And Visibility

Permissions decide whether a caller may use a read surface. Visibility filters
narrow which rows that caller can see.

Use both when a surface should be admitted only for certain callers and then
row-filtered by identity.

## SQL Compatibility

Shunter's SQL support is intentionally narrow and read-oriented. Supported
shapes differ by read surface. Check `docs/v1-compatibility.md` before relying
on a SQL feature in an app contract.
