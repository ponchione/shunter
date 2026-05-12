# Changelog

Shunter uses source versions from `VERSION` and release tags named `vX.Y.Z`.

## v1.0.0 - 2026-05-12

- Promoted the Shunter v1 compatibility line to the stable `v1.0.0` release
  version after release qualification.
- Release qualification covers the Go hardening command set, TypeScript SDK
  tests, and the external `opsboard-canary` quick/full gates.

## v0.1.1 - 2026-05-12

- Added network-level protocol coverage for slow WebSocket readers and
  write-timeout backpressure, proving an unread client does not block or
  corrupt unrelated fanout delivery.
- Runtime startup now requires rebuilding a fresh Runtime before retrying when
  a startup migration mutates in-memory state but fails before durability
  confirmation.
- Runtime startup now refreshes recovered state and commit-log resume state
  after a failed startup that already durably committed migration hooks, so
  non-dirty migration failures can be retried on the same Runtime.
- Snapshot creation through `Runtime.CreateSnapshot` now captures the current
  committed horizon and snapshot body under one read lock.
- Executor response delivery now uses nonblocking sends, and subscription
  register/unregister commands skip snapshot acquisition when already canceled.
- Commit-log CRC and store value/index hash paths now avoid per-call hasher
  allocations.
- Subscription fanout backpressure now waits directly on inbox capacity,
  shutdown, or context cancellation instead of polling with short timers.
- Hardened protocol outbound delivery against externally closed outbound queues.
- Commit log durability workers now reject non-positive segment sizes and drain
  batch sizes at startup.
- Live subscription filtered cross-join deltas now skip committed table scans
  for unchanged sides of the join.
- Store index seeks avoid redundant RowID slice cloning, and bounded B-tree
  scans reuse their upper bound key.
- Observability error redaction now bounds the raw scan window before
  redacting/truncating long error strings.
- Live subscription initial row limits now apply after supported
  `ORDER BY`/`OFFSET`/`LIMIT` initial-snapshot windowing, and streaming
  single-table `LIMIT` snapshots stop scanning once enough rows are gathered.
- Ordered live subscription initial snapshots now bound their retained row window
  when `LIMIT`, `OFFSET`, or `InitialRowLimit` makes the final result bounded.
- Protocol server writes now use a configurable `ProtocolConfig.WriteTimeout`
  and negotiated gzip skips small frames below the compression threshold.
- Store commit changesets now emit per-table insert/delete rows in stable RowID
  order.
- Subscription fanout now records blocked enqueue duration and supports an
  optional fanout enqueue context.
- Multi-way live subscription deltas now expand from changed relation rows
  instead of diffing full before/after join products.
- Runtime durability waits no longer report success when a pending durability
  waiter is closed without confirming the requested transaction.
- Protocol, BSATN, and commit-log encoders now reject oversized length-prefixed
  payloads instead of silently truncating lengths to `uint32`.
- Release qualification now explicitly includes the external `opsboard-canary`
  gates and recording the Shunter/canary commits used for the run.
- Recovery now rejects directory artifacts named as the first commit-log
  segment instead of treating the data directory as empty, while preserving
  rollover directory cleanup behavior.
- Added `Config.AuthIssuers` for strict JWT issuer allowlist validation.
- Strict JWT validation now rejects future `iat` claims.
- Contract diff and workflow policy checks now classify stable v1 contract
  permission/codegen drift according to the compatibility matrix.
- Runtime builds now write `shunter.datadir.json` metadata that keeps Shunter
  build version and app module version separate and rejects mismatched module
  data directories.
- Live subscription join deltas now avoid per-row committed table rescans when only the changed side's join column is indexed.
- Live subscription initial join snapshots now choose the lower-cost indexed scan side, including filter-reduced candidate sets, when both join columns are indexed.
- Live subscription initial join snapshots now use indexed required equality/range filters to skip unnecessary join probes.
- Live subscription multi-way joins now use local equality/range filter pruning for distinct relation tables instead of table-wide fallback.
- Live subscription multi-way joins now use indexed join-condition existence pruning for distinct relation tables, including same-transaction opposite-side changes.
- Live subscription multi-way joins now use alias-aware local equality/range pruning for repeated relation tables when every relation instance has a required local filter.
- Live subscription multi-way joins now use indexed join-condition existence pruning for repeated relation tables when every relation instance has an indexed condition edge.
- Live subscription multi-way joins now combine alias-local filter pruning with indexed join-condition existence pruning for repeated relation tables.
- Live subscription multi-way joins now use indexed existence pruning for required filter-level column equalities, including repeated relation aliases.
- Live subscription multi-way joins now use per-relation indexed existence pruning for disjunctive filter-level column equalities when every OR branch covers that relation.
- Live subscription multi-way joins now combine local value/range pruning with indexed column-equality existence pruning for mixed-branch OR filters when every OR branch covers that relation.
- Live subscription multi-way joins now use indexed remote value/range filter-edge pruning for required relation-local filters, including repeated relation aliases.
- Live subscription multi-way joins now use two-hop indexed path-edge pruning for non-key-preserving remote value/range filters.
- Live subscription multi-way joins now use three-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use four-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use five-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use six-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use seven-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use eight-hop indexed path-edge pruning for covered remote value/range filters, including same-transaction middle and endpoint changes.
- Live subscription multi-way joins now use bounded generic path-edge pruning for longer non-key-preserving remote value/range filters without adding fixed per-hop index types.
- Live subscription multi-way split-OR branches now use branch-local column-equality connectors to place remote value/range filter-edge pruning.
- Live subscription multi-way split-OR column-equality branches now fall back instead of keeping partial existence-edge pruning when any branch lacks indexed coverage.
- Live subscription multi-way joins now split OR filters with alias-local value/range branches on directly joined relation instances into local and filter-edge pruning placements.
- Live subscription multi-way joins now split OR filters across same-key transitive condition paths into endpoint-local and remote filter-edge pruning placements.
- Live subscription join value/range filter-edge pruning now admits candidates from same-transaction opposite-side inserts and deletes.
- Live subscription joins now use direct split-OR local/filter-edge pruning when the OR is a required child of an AND filter.
- Live subscription joins now use indexed existence-edge pruning for direct column-equality branches inside split OR filters.
- Live subscription joins now use remote filter/existence branch edges for split OR filters whose changed side has no local branch.
- Live subscription range pruning lookups now return each candidate hash once when overlapping range branches from the same query match a value.
- Live subscriptions and declared views now support two-table column-equality join filters, including inner-join `WHERE` column comparisons and cross-join `WHERE` equality lowering with literal filters.
- Live subscription join candidate pruning now uses required range filters on the opposite joined side when that side's join column is indexed.
- Live subscription cross-side OR pruning now treats not-equals filters as split range placements instead of falling back to broad join-existence candidates.
- Live subscription delta candidate pruning now uses range predicates instead of table-wide fallback when the predicate shape is safely range-constrained.
- Live subscription initial and final snapshots now use matching single-column indexes for equality and compound single-table filters.
- One-off and declared single-table queries now use matching composite indexes for multi-column `ORDER BY`, including mixed directions.
- One-off and declared multi-way join queries now use matching single-column indexes when probing joined relations.
- One-off and declared aggregate queries now ignore null inputs for `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(nullable_numeric_column)`, returning `NULL` for nullable sums with no non-null inputs.
- Declared live views now support column projections over their emitted relation, including projected initial rows and subscription deltas.
- Declared live-view projection deltas now suppress no-op insert/delete replacement rows after the final projected shape is applied.
- Declared live views now support single-table `COUNT(*)` and `COUNT(column)` aggregate rows, including visibility-filtered counts and delete-old/insert-new deltas when the count changes.
- Declared live views now support single-table `SUM(column)` aggregate rows for numeric columns, including nullable sum semantics and delete-old/insert-new deltas when the sum changes.
- Declared live views now support single-table `COUNT(DISTINCT column)` aggregate rows, including visibility-filtered distinct counts and delete-old/insert-new deltas when the distinct count changes.
- Declared live views now support two-table indexed join `COUNT(*)` aggregate rows, including visibility-filtered counts and delete-old/insert-new deltas when the count changes.
- Declared live views now support two-table indexed join `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` aggregate rows, including visibility-filtered values and delete-old/insert-new deltas when aggregate values change.
- Declared live views now support two-table cross-join `COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` aggregate rows, including visibility-filtered values and delete-old/insert-new deltas when aggregate values change.
- Declared live views now support multi-way join `COUNT(*)`, `COUNT(column)`, `COUNT(DISTINCT column)`, and `SUM(column)` aggregate rows, including visibility-filtered values and delete-old/insert-new deltas when aggregate values change.
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
- Protocol reducer failure strings now label app reducer errors, app panics,
  permission denials, and Shunter runtime failures.
- Added the initial `@shunter/client` TypeScript runtime foundation in
  `typescript/client` and updated generated TypeScript bindings to import its
  shared runtime types.
- Added `@shunter/client` protocol compatibility helpers and a managed
  subscription handle primitive with idempotent unsubscribe behavior.
- Added a minimal `createShunterClient` TypeScript WebSocket lifecycle shell
  with token query propagation, subprotocol negotiation, state callbacks,
  `connect()`, `close()`, and idempotent `dispose()`.
- Added TypeScript decoding for the initial server `IdentityToken` frame so
  `createShunterClient().connect()` resolves with identity and connection ID
  metadata.
- Added raw TypeScript reducer request encoding and a connected-client
  `callReducer` send path for the v1 `CallReducerMsg` wire shape.
- Added minimal TypeScript reducer response correlation for full-update
  `callReducer` calls, resolving on committed `TransactionUpdate` frames and
  rejecting on failed reducer updates.
- Added a TypeScript reducer result helper that wraps heavy
  `TransactionUpdate` frames in committed/failed result envelopes.
- Added raw TypeScript declared-query request encoding and
  `OneOffQueryResponse` correlation for byte-level generated query helpers.
- Added a TypeScript raw declared-query result helper that exposes table names,
  raw RowList bytes, split row byte arrays, message ID, duration, and raw frame.
- Added raw TypeScript declared-view subscription request encoding,
  `SubscribeMultiApplied`/`SubscriptionError` correlation, and an idempotent
  `UnsubscribeMulti` send path.
- Added raw TypeScript table subscription request encoding,
  `SubscribeSingleApplied`/`SubscriptionError` correlation, and an idempotent
  `UnsubscribeSingle` send path.
- Added raw TypeScript subscription update callback plumbing for accepted
  declared-view/table subscriptions, including `TransactionUpdateLight`
  decoding.
- TypeScript declared-view/table unsubscribe promises now settle on matching
  unsubscribe acknowledgements or subscription errors instead of resolving
  immediately after send.
- Added TypeScript raw RowList decoding for live server row-batch payloads,
  including per-row byte arrays on decoded one-off query and table initial-row
  envelopes.
- TypeScript raw subscription updates now expose optional decoded insert/delete
  row byte arrays when their payloads are RowList envelopes.
- TypeScript declared-view and table subscriptions can now opt into managed
  subscription handles backed by server-acknowledged unsubscribe paths.
- Generated TypeScript clients now export module-scoped aliases for the
  reducer result envelope and raw declared-query result envelope.
- TypeScript table subscriptions now accept caller-supplied row decoders for
  decoded initial-row and update callbacks while preserving raw callbacks.
- TypeScript declared-query results can now be decoded with caller-supplied
  table row decoders while preserving raw declared-query result helpers.
- Generated TypeScript table and declared-query helpers now expose/pass through
  row decoder option surfaces for decoded table rows and query results.
- TypeScript table subscription handles now hold decoded initial rows when
  callers pass both `returnHandle: true` and a row decoder.
- Generated TypeScript bindings now include schema-aware BSATN table row
  decoders and default generated table subscription helpers to those decoders.
- TypeScript managed table subscription handles now apply RowList insert/delete
  updates using raw row bytes as local row identity.
- The TypeScript runtime now supports explicit opt-in reconnect with bounded
  retry, token-provider refresh per attempt, and subscription replay after a
  fresh identity handshake.
- Hardened the TypeScript runtime lifecycle around stale WebSocket events,
  reconnect token failures, caller close/dispose during reconnect attempts,
  and unsubscribe cleanup during reconnect or failed unsubscribe paths.
- TypeScript declared-view and table subscriptions now stop delivering updates
  as soon as caller unsubscribe begins, even before the server acknowledgement.
- Generated TypeScript reducer helpers now include full-update result-envelope
  wrappers alongside the existing raw byte helpers.
- TypeScript reducer result helpers now convert connected-client reducer
  failures into failed result envelopes for generated helper callers.
- The TypeScript runtime now treats missing or unsupported connected server
  message tags as protocol failures instead of silently ignoring them.
- The TypeScript runtime now fails connected clients on unscoped subscription
  evaluation errors so pending operations and live handles settle explicitly.
- TypeScript table subscription `onRows` and `onInitialRows` callbacks now
  receive raw row bytes when no row decoder is supplied.
- TypeScript declared-view and table subscriptions now reject explicit
  request/query IDs that are pending, active, or awaiting unsubscribe
  acknowledgement.
- TypeScript declared-view and table subscriptions now skip occupied
  request/query IDs when auto-allocating subscription IDs.
- The TypeScript runtime now fails connected clients on scoped subscription
  errors for already accepted subscriptions when no pending operation matches.
- TypeScript reducer calls now reject explicit request IDs that would collide
  with an in-flight full-update reducer response.
- TypeScript reducer calls and declared queries now skip occupied in-flight IDs
  when auto-allocating reducer request IDs and declared-query message IDs.
- TypeScript declared queries now expose a stable validation error code when
  callers reuse an in-flight message ID.
- TypeScript table `onRawRows` callbacks now receive cloned row bytes so raw
  callback mutation cannot corrupt decoded initial rows or managed handles.
- TypeScript token providers now fail before WebSocket creation when they
  resolve to non-string values.
- The TypeScript runtime now defines explicit reducer argument encoder helpers
  for callers that map typed reducer args to raw `Uint8Array` payloads.
- TypeScript declared-view subscriptions now accept the server's single-table
  initial acknowledgement shape used by table-returning declared views.

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
