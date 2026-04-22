# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

For provenance of closed slices, use `git log` — this file tracks only current state and forward motion.

## What just landed (2026-04-22, OI-008 — cmd/shunter-example bootstrap, closed)

First end-to-end embedding surface. Before this slice, the repo had substantial subsystem code but no runnable consumer binary — `schema.Engine.Start` is not a cohesive runtime bootstrap and there was no `cmd/` or `examples/` directory at repo root. OI-008 scope from the 2026-04-22 audit: prove the product is adoptable by wiring schema → executor → protocol server against a real store + commit-log directory.

Landed:

- **`cmd/shunter-example/main.go`** — minimal consumer that registers a one-table schema (`greetings`), one reducer (`say_hello`), opens a data directory with `commitlog.OpenAndRecoverDetailed`, bootstraps an initial snapshot on `ErrNoData`, boots the executor via `Executor.Startup(ctx, nil)`, starts an anonymous-auth WebSocket server on `/subscribe`, and exits cleanly on SIGINT/SIGTERM. Two inline glue adapters (`durabilityAdapter` for the `uint64`↔`types.TxID` shim on `DurabilityWorker`, `stateAdapter` for the `*CommittedSnapshot`↔`CommittedReadView` shape split on `CommittedState.Snapshot`) are the only non-obvious wiring — documented in `docs/embedding.md`.
- **`cmd/shunter-example/main_test.go`** — 3 smoke pins preventing bit-rot:
  - `TestBuildEngine_BootstrapThenRecover` — cold-boot (empty tempdir → ErrNoData → initial snapshot at TxID 0 → retry) and subsequent recovery against the same directory both succeed.
  - `TestBuildEngine_AdmitsAnonymousConnection` — WebSocket dial to `/subscribe` returns 101 Switching Protocols and the client reads the `InitialConnection` frame. Exercises the full schema → executor → protocol admission path.
  - `TestRun_ShutsDownCleanlyOnContextCancel` — `run()` spawned in a goroutine with a cancelled ctx returns nil and releases all resources (durability Close, executor Shutdown, http Shutdown) within 5s.
- **`docs/embedding.md`** — embedder walkthrough: wiring diagram, seven numbered steps (schema → open data dir → durability → reducer registry → executor → protocol server → shutdown), plus scope callouts for the three subsystems explicitly deferred (subscriptions, scheduled reducers, strict auth).

Intentionally deferred (not OI-008 scope, carry-forward):

- Subscription fan-out wiring. The example runs with the noop `SubscriptionManager` default so reducer writes commit but do not fan out. Real wiring needs a `schema.SchemaRegistry` adapter that adds `ColumnCount(TableID) int` (the subscription-layer `SchemaLookup` interface requires it but `schema.SchemaRegistry` does not satisfy it). Separate slice.
- `executor.Scheduler` wiring. `NewScheduler(inbox, cs, tableID)` takes the executor's unexported `inbox` channel; no exported accessor exists. Passing `nil` to `Executor.Startup` is the documented no-scheduler path and is what the example uses.
- `protocol/handle_oneoff_test.go` pre-existing drift against the working-tree `TableByName` 3-value return. Already called out as its own slice in OI-011 carry-forward; not regressed.

Verification:

- `rtk go build ./cmd/shunter-example` → clean
- `rtk go test ./cmd/shunter-example -count=1 -race` → 3 passed
- `rtk go vet ./cmd/shunter-example` → `Go vet: No issues found`
- `rtk go fmt ./cmd/shunter-example` → clean
- `rtk go test ./schema ./store ./subscription ./executor ./commitlog ./cmd/shunter-example -count=1` → 911 passed

Ledger / debt follow-through:

- `TECH-DEBT.md` OI-008 flipped from `open` to `closed 2026-04-22` with realized-surfaces summary, pin list, and deferred-scope notes.

## What just landed (2026-04-22, OI-012 — spec-text refresh, closed)

SPEC-002 / SPEC-005 / Story 5.5 doc-only refresh. Before this slice, the decomposition specs were stale against realized Phase 1.5 (outcome model) and Phase 2 Slice 1–2 (SQL + multi-query wire) code — a reader building against the spec text would wire against the wrong surfaces. No code changes.

Landed:

- **SPEC-002 §3.1 / §3.3 BSATN kinds** (`docs/decomposition/002-commitlog/SPEC-002-commitlog.md`): disclaimer updated from "0–12 for 13 scalars" to "0–18 for 19 scalar kinds". `ValueKind` table extended with tags 13 Int128, 14 Uint128, 15 Int256, 16 Uint256, 17 Timestamp, 18 ArrayString (payload shapes matching `bsatn/encode.go`). Widening-history note added pointing at `types/value.go` + `bsatn/encode.go` as canonical sources.
- **SPEC-005 tag tables** (`docs/decomposition/005-protocol/SPEC-005-protocol.md` §6): Client→Server expanded to 6 tags (adds `SubscribeMulti`=5, `UnsubscribeMulti`=6; renames `Subscribe` → `SubscribeSingle`). Server→Client expanded to 10 tags with tag 7 flagged **RESERVED** (formerly `ReducerCallResult`), and new tags 8 `TransactionUpdateLight`, 9 `SubscribeMultiApplied`, 10 `UnsubscribeMultiApplied`.
- **SPEC-005 §7 SQL wire surface**: §7.1 Subscribe rewritten as `SubscribeSingle` + `query_string`; §7.1.1 replaces structured `Query{table_name, predicates[]}` with the Phase 2 Slice 1 SQL subset; §7.1b adds `SubscribeMulti`; §7.2 split to `UnsubscribeSingle` / §7.2b `UnsubscribeMulti`; §7.3 `CallReducer` gains the `flags` byte (`FullUpdate` / `NoSuccessNotify`); §7.4 `OneOffQuery` flipped to `message_id + query_string`.
- **SPEC-005 §8 Phase 1.5 outcome model**: §8.5 `TransactionUpdate` rewritten as the heavy caller-bound envelope carrying `UpdateStatus{Committed|Failed|OutOfEnergy}` + `CallerIdentity`/`CallerConnectionID`/`ReducerCall`/`Timestamp`/`EnergyQuantaUsed`/`TotalHostExecutionDuration`; §8.7 flagged RESERVED with pointer to `docs/parity-phase1.5-outcome-model.md` + pin tests; §8.8 added for `TransactionUpdateLight`; §8.10/§8.11 for SubscribeMulti/UnsubscribeMulti Applied envelopes. §9/§10/§11/§13/§15/§16/§17 all updated consequentially (state machine, cache rules, `ClientSender` interface, divergences, verification).
- **Story 5.5 acceptance**: `docs/decomposition/006-schema/epic-5-validation-build/story-5.5-reducer-schema-validation.md` acceptance bullets rewritten to assert via `errors.Is(err, ErrX)` against OI-011 canonical sentinels, with OI-011 pin tests (`schema/oi011_pins_test.go`, `schema/audit_regression_test.go`) cross-referenced as authoritative.

Verification:

- no code changes
- `rtk grep` spot-checks for symbols cited in the refreshed specs: `TagReducerCallResult` / `TagTransactionUpdateLight` / `StatusCommitted` / `ReducerCallInfo` / `CallReducerFlagsNoSuccessNotify` / `KindArrayString` / `ErrReservedReducerName` all present in `protocol/tags.go`, `protocol/server_messages.go`, `protocol/client_messages.go`, `types/value.go`, `schema/errors.go`.

Ledger / debt follow-through:

- `TECH-DEBT.md` OI-012 flipped from `open` to `closed 2026-04-22` with realized-refresh summary and source-doc cross-references.

## What just landed (2026-04-22, OI-011 — schema contract drift, closed)

SPEC-006 §7 / §13 compliance closure. Before this slice, one of `schema`'s two spec-canonical sub-interfaces (`IndexResolver`) had a shadowed duplicate declaration in `subscription/placement.go`, and the six sentinel-level schema validation errors declared in `schema/errors.go` were not consistently returned from `Build()` — several validation paths returned bare `fmt.Errorf` strings, and a pattern-mismatched table name surfaced as `ErrEmptyTableName`. Additionally `store/errors.go` and `subscription/errors.go` each declared their own `ErrColumnNotFound`, breaking `errors.Is` across the schema boundary.

Landed:

- **Interface dedup** (`subscription/placement.go` / `subscription/predicate.go`): local `IndexResolver` interface removed from `placement.go`; `subscription/predicate.go` now re-exports the canonical type as `type IndexResolver = schema.IndexResolver`. `SchemaLookup` was already canonical in `schema/registry.go`; `protocol/handle_subscribe.go` retains a narrower local `SchemaLookup` (single-method `TableByName`) which SPEC-006 §7 explicitly sanctions as a consumer-side narrowing.
- **Sentinel canonicalization** (`store/errors.go` / `subscription/errors.go`): `ErrColumnNotFound` in both packages now aliases `schema.ErrColumnNotFound` so `errors.Is` matches across boundaries (SPEC-001 §9, SPEC-004 EPICS Epic 1).
- **Validation-gate rewiring** (`schema/validate_structure.go` / `schema/validate_schema.go`): invalid-pattern table names → `ErrInvalidTableName` (was `ErrEmptyTableName`), empty column names → `ErrEmptyColumnName`, missing-index-column refs → `ErrColumnNotFound`, nil reducer/lifecycle handlers → `ErrNilReducerHandler`, reserved reducer names → `ErrReservedReducerName`, duplicate OnConnect/OnDisconnect → `ErrDuplicateLifecycleReducer`.

Pins landed:

- `schema/oi011_pins_test.go` (new, 7 pins):
  - `SchemaRegistry` satisfies both `SchemaLookup` and `IndexResolver` (compile-time shape check).
  - Reserved reducer name `OnConnect` / `OnDisconnect` → `errors.Is(err, ErrReservedReducerName)`.
  - Nil reducer handler → `ErrNilReducerHandler`.
  - Nil lifecycle (`OnConnect(nil)`, `OnDisconnect(nil)`) → `ErrNilReducerHandler`.
  - Duplicate `OnConnect` / duplicate `OnDisconnect` → `ErrDuplicateLifecycleReducer`.
  - Invalid-pattern table name (`"123bad"`) → `ErrInvalidTableName` (and does **not** masquerade as `ErrEmptyTableName`).
  - Empty column name → `ErrEmptyColumnName`.
  - Missing index column → `ErrColumnNotFound`.
- `subscription/oi011_pins_test.go` (new, 2 pins):
  - `subscription.IndexResolver` is a type alias equivalent to `schema.IndexResolver`.
  - `subscription.ErrColumnNotFound == schema.ErrColumnNotFound`; `errors.Is` matches across a wrap.
- `store/oi011_pins_test.go` (new, 1 pin):
  - `store.ErrColumnNotFound == schema.ErrColumnNotFound`.
- `schema/audit_regression_test.go` migrated from `strings.Contains` assertions to `errors.Is` against the new sentinels for reducer/lifecycle/missing-column audits.

Explicitly out of scope (carried forward):

- `docs/decomposition/006-schema/epic-5-validation-build/story-5.5-reducer-schema-validation.md` acceptance references pre-sentinel text; doc refresh folds into OI-012.
- Subscription-layer migration to `StateView.SeekIndexBounds` (still carried from OI-010).
- `schema/registry.go` working-tree diff (TableByName returning `(TableID, *TableSchema, bool)`) is an unrelated upstream change the session started on; `protocol/handle_oneoff_test.go` is stale against that three-value return. Not OI-011 scope; flag as its own slice when that registry change is committed.

Verification:

- `rtk go test ./schema -count=1` → 121 passed.
- `rtk go test ./schema ./subscription ./store -count=1` → 551 passed.
- `rtk go vet ./schema ./subscription ./store` → `Go vet: No issues found`.
- `rtk go fmt ./schema ./subscription ./store` → clean.

Ledger / debt follow-through:

- `TECH-DEBT.md` OI-011 flipped from `open` to `closed 2026-04-22` with realized-surfaces summary, pin list, and deferred-scope notes.

## What just landed (2026-04-22, OI-010 — store range-bounds API, closed)

SPEC-001 §4.6 / §5.4 compliance closure. Before this slice, `StateView` was the spec's "unified read interface" in name only for exclusive-bound range predicates — the `BTreeIndex.SeekBounds` primitive and `StateView.SeekIndexBounds` wire-through were both missing. Subscription predicates with strict inequality on string/bytes/float keys had no expressible path through the transaction-layer read surface.

Landed:

- `BTreeIndex.SeekBounds(low, high Bound) iter.Seq[types.RowID]` (`store/btree_index.go`) — Bound-parameterized range scan. Binary-search start position from `low.Value`; if the key exists and `low` is exclusive, advance past the matching entry. Per-entry upper-bound check (`Inclusive`: `cmp > 0` stops; exclusive: `cmp >= 0` stops). `Bound.Unbounded` on either side skips the corresponding check. Supports early break in `iter.Seq`. Empty index / empty range / inverted range all yield nothing as expected.
- `Index.SeekBounds(low, high Bound)` (`store/index.go`) — thin wrapper over the BTree primitive so `*Index` callers match SPEC-001 §4.6's public surface.
- `StateView.SeekIndexBounds(tableID, indexID, low, high Bound) iter.Seq[types.RowID]` (`store/state_view.go`) — unified read path. Delegates committed side to `idx.BTree().SeekBounds(low, high)` and `slices.Collect`-s the range at the StateView boundary (OI-005 aliasing hazard closure — same pattern as `SeekIndexRange`). Filters through `sv.tx.IsDeleted` + live `Table.GetRow` visibility. Tx-local inserts linear-scanned; each candidate's extracted key is checked against both bounds via the package-level `matchesLowerBound` / `matchesUpperBound` helpers already used by `CommittedSnapshot.IndexRange`.

Pins landed:

- `store/btree_index_seekbounds_test.go` (new, 16 pins):
  - Bound edges (1-6): inclusive-inclusive, exclusive-exclusive, inclusive-exclusive (= SeekRange half-open equivalence), exclusive-inclusive, unbounded low, unbounded high.
  - Full-scan equivalence (7): both unbounded == `Scan()`.
  - Empty / degenerate (8-9, 14): `low > high`, exclusive endpoints at same value, empty index.
  - Same-value / same-key ordering (10, 13): `[3,3]` yields one key; multiple rowIDs under one key yielded ascending.
  - Exclusive-low-at-existing-key vs exclusive-low-between-keys (11-12): spec §4.6 ordering semantics.
  - Early break (15): `iter.Seq` break contract.
  - Wrapper passthrough (16): `Index.SeekBounds` == `Index.BTree().SeekBounds`.
- `store/state_view_seekindexbounds_test.go` (new, 13 pins):
  - Bound edges × merged state (1-3): `(2,4]`, `[2,4)`, both-unbounded = ScanTable-by-index.
  - Tx-layer interactions (4-7): `tx.deletes` filter; tx-local insert in range included; tx-local insert outside range excluded; tx-local insert at exclusive-low boundary dropped.
  - Empty-tx baseline (8): empty StateView matches raw committed BTree result.
  - Unknown identifiers (9-10): unknown tableID / unknown indexID return empty iterators (no panic, no error).
  - Deleted-committed mid-path (11): `Table.DeleteRow` before iteration is masked by the `GetRow` visibility check.
  - OI-005 aliasing pin (12): BTree mid-iter mutation does not drift iteration — mirrors `TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`.
  - Early break (13): consumer can break without consuming full range.

Production-code touches outside `store/`: none. Subscription-layer consumer migration (`subscription/eval.go`) is explicitly deferred — current Tier-3 fallback is safe.

Explicitly out of scope (carried forward):

- Migration of `subscription/eval.go` to `StateView.SeekIndexBounds`. The spec now has the surface; the consumer rewire is a separate follow-on slice.

Verification:

- `rtk go test ./store -run "SeekBounds|SeekIndexBounds" -count=1 -v` → 29 passed.
- `rtk go test ./store -count=1` → 108 passed.
- `rtk go test ./... -count=1` → 1589 passed (1560 baseline + 29 new).
- `rtk go vet ./store` → `Go vet: No issues found`.
- `rtk go fmt ./store` → clean.

Ledger / debt follow-through:

- `TECH-DEBT.md` OI-010 flipped from `open` to `closed 2026-04-22` with realized-surfaces summary + verification results + deferred-scope note (consumer migration).

Clean-tree baseline at session close: `Go test: 1589 passed in 10 packages`.

## Spec-compliance audit (2026-04-22)

A 6-agent audit of `docs/decomposition/` specs against live code surfaced 4 real open gaps and 1 spec-text-stale theme. All tracked in `TECH-DEBT.md`; OI-009, OI-010, and OI-011 closed.

- **OI-009** — Executor startup orchestration + dangling-client sweep. **Closed 2026-04-22**.
- **OI-010** — Store `SeekBounds` + `StateView.SeekIndexBounds`. **Closed 2026-04-22**.
- **OI-011** — Schema contract drift from SPEC-006 (interface dedup + sentinel canonicalization + Build-time validation gates). **Closed 2026-04-22** (this session).
- **OI-012** — `docs/decomposition/` spec texts (SPEC-002 §3.3 BSATN kinds, SPEC-005 outcome-model + SQL wire surface) stale vs realized Phase 1.5 / Phase 2 decisions. Doc-only cleanup.
- **OI-008** (extended) — no `cmd/` or `examples/` directory at repo root. Confirmed absent.

Non-blockers also surfaced (no OI, intentional / performance-only / spec-deferred): BSATN 19-vs-13 kinds is the intentional column-kind widening (→ fold into OI-012 spec refresh); subscription `ColNe` / `Or` / `CrossJoinProjected` have no pruning placement but Tier-3 fallback is safe; commitlog snapshot-retention policy is `deferred v1` by SPEC-002 §7 itself; subscription memoized-encoding hook is PHASE-5-DEFERRED §2.

## Next session: pick one narrow slice from the follow-on queue

OI-008 / OI-009 / OI-010 / OI-011 / OI-012 are all closed. No remaining `open` OIs. Pick one from the queue below, open no more than one at a time.

## Follow-on queue

In priority order, all narrow-ready:

1. **`schema/registry.go` `TableByName` arity commit** — the working-tree diff at `schema/registry.go` returning `(TableID, *TableSchema, bool)` is landed functionally and is what the OI-008 example + `protocol.SchemaLookup` are built against, but `protocol/handle_oneoff_test.go` still calls the 2-value return at ~10 sites and the package does not build under test. Commit the registry diff and update the stale test call sites. First-class slice — unblocks full-repo `go test ./...`.
2. **Subscription `SeekIndexBounds` migration** — rewire `subscription/eval.go` predicate scans off the Tier-3 fallback and onto the `StateView.SeekIndexBounds` surface landed in OI-010.
3. **Subscription fan-out wiring in `cmd/shunter-example`** — wire `subscription.Manager` + `FanOutWorker` + `protocol.FanOutSenderAdapter` into the example so reducer writes actually fan out to subscribers. Requires an adapter that widens `schema.SchemaRegistry` with `ColumnCount(TableID) int` (subscription `SchemaLookup` demands it; `schema.SchemaRegistry` does not satisfy it). Adds real subscription coverage to the example's smoke tests. Follow-on to OI-008.
4. **Expose executor inbox for scheduler wiring** — `NewScheduler(inbox chan<- ExecutorCommand, ...)` reaches the executor's unexported `inbox`. Production embedders that want sys_scheduled replay need an exported accessor (e.g. `Executor.SchedulerFor(tableID)` or `Executor.Inbox()`). Lets the OI-008 example pass a real `*Scheduler` to `Startup`.

Pick scope before starting. Do not open multiple OIs at once.

## Startup notes

- Read `CLAUDE.md` first, then `RTK.md` for command rules, then `docs/EXECUTION-ORDER.md` for sequencing.
- Use `git log` for slice provenance; this file is current-state only.
- Before changing a file, verify against live code — memory/ledger claims can drift.
