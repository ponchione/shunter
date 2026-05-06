# Changelog

Shunter uses source versions from `VERSION` and release tags named `vX.Y.Z`.

## v0.1.1-dev

- Current development line.
- Live subscription join deltas now avoid per-row committed table rescans when only the changed side's join column is indexed.
- Live subscription initial join snapshots now choose the lower-cost indexed scan side, including filter-reduced candidate sets, when both join columns are indexed.
- Live subscription initial join snapshots now use indexed required equality/range filters to skip unnecessary join probes.
- Live subscriptions and declared views now support two-table column-equality join filters, including inner-join `WHERE` column comparisons and cross-join `WHERE` equality lowering with literal filters.
- Live subscription join candidate pruning now uses required range filters on the opposite joined side when that side's join column is indexed.
- Live subscription cross-side OR pruning now treats not-equals filters as split range placements instead of falling back to broad join-existence candidates.
- Live subscription delta candidate pruning now uses range predicates instead of table-wide fallback when the predicate shape is safely range-constrained.
- Live subscription initial and final snapshots now use matching single-column indexes for equality and compound single-table filters.
- One-off and declared single-table queries now use matching composite indexes for multi-column `ORDER BY`, including mixed directions.
- One-off and declared multi-way join queries now use matching single-column indexes when probing joined relations.
- One-off and declared aggregate queries now ignore null inputs for `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(nullable_numeric_column)`, returning `NULL` for nullable sums with no non-null inputs.
- Declared live views now support column projections over their emitted relation, including projected initial rows and subscription deltas.
- Declared live views now support single-table `COUNT(*)` and `COUNT(column)` aggregate rows, including visibility-filtered counts and delete-old/insert-new deltas when the count changes.
- Declared live views now support single-table `SUM(column)` aggregate rows for numeric columns, including nullable sum semantics and delete-old/insert-new deltas when the sum changes.
- Declared live views now support single-table `COUNT(DISTINCT column)` aggregate rows, including visibility-filtered distinct counts and delete-old/insert-new deltas when the distinct count changes.
- Declared live views now support single-table `ORDER BY` initial snapshots for table-shaped and projected views while retaining row-delta semantics after commits.
- Declared live views now support single-table `LIMIT` initial snapshots for table-shaped and projected views while retaining row-delta semantics after commits.
- Declared live views now support single-table `OFFSET` initial snapshots for table-shaped and projected views while retaining row-delta semantics after commits.
- One-off and declared SQL queries support multi-way joins, and table-shaped multi-way joins now work in live subscriptions and executable declared views.
- Generated TypeScript clients now include a table-name-to-row-type map and a table subscriber callback type derived from it.
- Added canonical JSON column values with schema export, BSATN encoding, SQL literal coercion, store/index support, subscription hashing, contract validation, and TypeScript `unknown` codegen.
- Added nullable column semantics across `types.Value`, schema reflection/export, schema-aware row BSATN, store validation/indexing, SQL `IS NULL` predicates, snapshots/recovery, contract diff, and TypeScript `T | null` codegen.
- Hardened composite secondary index behavior across unique enforcement, reducer index seeks, snapshot/recovery rebuilds, and detached contract validation.
- Documented fixed-point persisted values as an app-owned scaled integer convention over deterministic integer column kinds.
- Added generic auth principals on reducer caller context, populated from validated protocol JWT claims and local call options without changing Shunter identity semantics.

## v0.1.0 - 2026-05-05

- First Shunter release suitable for use as a normal Go module dependency.
- Published Shunter's WebSocket fork as `github.com/ponchione/websocket v1.8.15-shunter.1` and removed the downstream-only `replace` requirement.
- Added `Runtime.WaitUntilDurable` for app-owned durable acknowledgements.
- Added root `IndexBound`, `Inclusive`, `Exclusive`, `UnboundedLow`, `UnboundedHigh`, and index key helpers.
- Added indexed local reads through `LocalReadView.SeekIndex` and `LocalReadView.SeekIndexRange`.
- Added indexed reducer reads through `ReducerDB.SeekIndex` and `ReducerDB.SeekIndexRange`.
- Added root aliases for reducer-facing `ReducerDB`, `Value`, `ProductValue`, `RowID`, and `TxID`.
- Gzip-negotiated protocol connections now gzip-compress post-handshake server messages while keeping client messages uncompressed.
- Pinned one-off aggregate empty-input semantics for `COUNT` and `SUM`.
- Added `shunter.CheckDataDirCompatibility` for app-owned offline startup schema preflights.
- Added `shunter.RunDataDirMigrations` for app-owned offline executable migrations.
- Added `shunter.RunModuleDataDirMigrations` for offline execution of hooks registered with `Module.MigrationHook`.
- Added app-owned startup migration hooks through `Module.MigrationHook`.
- Tightened generated TypeScript client callback types to use contract-derived table, reducer, and executable declared query/view name unions.
- Improved startup snapshot schema mismatch diagnostics to report multiple structural differences in one failure.
- Added backup/restore guidance to dry-run contract migration plans for blocking or data-rewrite changes.
- Added reusable offline `DataDir` backup and restore helpers for app-owned binaries.
- Added generic CLI commands for offline runtime `DataDir` backup and restore.
- Added runtime snapshot creation and commit log compaction helpers for app-owned maintenance workflows.
- Added reusable runtime and host health/readiness inspection helpers.
- Added `Host.ListenAndServe` for app-owned serving of multi-module hosts.
- Added app-owned contract export and runtime-to-codegen file helpers in `contractworkflow`.
- Added narrow `OFFSET <unsigned-integer>` support for one-off SQL and declared queries.
- Added narrow `ORDER BY` support for unique projection output names in one-off SQL and declared queries.
- Added multi-column `ORDER BY` support for one-off SQL and executable declared queries.
- Added aggregate output alias `ORDER BY` support for one-off SQL and executable declared queries.
- One-off single-table `ORDER BY <column> ASC`/`DESC` can now scan a matching single-column index while rechecking filters and visibility.
- Added narrow `COUNT(<column>)` support for one-off SQL and declared queries.
- Added narrow `SUM(<numeric-column>)` support for one-off SQL and declared queries.
- Reject commits before state mutation when the executor TxID allocator is exhausted.
- Reject reducer registration before reducer ID allocation can wrap.
- Fixed store index keys so caller-owned `bytes` and `arrayString` values cannot mutate committed index entries after insert.
