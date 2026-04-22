# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

For provenance of closed slices, use `git log` — this file tracks only current state and forward motion.

## What just landed (2026-04-22, Subscription fan-out wiring in `cmd/shunter-example` — follow-on queue #1 (post-OI-008), closed)

Narrow consumer-side wiring of `subscription.Manager` + `FanOutWorker` + `protocol.FanOutSenderAdapter` into the OI-008 example binary. Before this slice, the example ran with the `executor` package's `noopSubs` default (`executor.go:688-699`), so reducer writes committed and durability queued but no subscriber ever received a delta. Queue #1 called for exactly this wiring plus a `ColumnCount` adapter closing the gap between `schema.SchemaRegistry` (missing `ColumnCount`) and `subscription.SchemaLookup` (requires it — used by `eval.go:401` for join row projection width).

Landed:

- `cmd/shunter-example/main.go` `buildEngine`:
  - `schemaLookupAdapter` (struct over `schema.SchemaRegistry`) — forwards `TableExists` / `ColumnExists` / `ColumnType` / `HasIndex` / `TableName` straight through, synthesizes `ColumnCount(TableID) int` via `reg.Table(t)` + `len(ts.Columns)`. `reg` itself satisfies `subscription.IndexResolver` (schema/registry.go:162) so no second adapter is needed on that seam.
  - `fanOutInbox := make(chan subscription.FanOutMessage, 256)` — cap sized to match the executor inbox default so a commit backlog cannot rendezvous-stall `EvalAndBroadcast`.
  - `subs := subscription.NewManager(schemaLookupAdapter{reg}, reg, subscription.WithFanOutInbox(fanOutInbox))`.
  - `executor.ExecutorConfig.Subscriptions = subs` — flips the executor off `noopSubs` onto the live Manager so every committed transaction's post-commit evaluation runs through the real fan-out path.
  - `conns := protocol.NewConnManager()` is now constructed in `buildEngine` (previously inline in `buildProtocolServer`) so the same `*ConnManager` is shared by both admission (`protocol.Server.Conns`) and delivery (`protocol.NewClientSender`). Without this, `ClientSender.SendTransactionUpdateLight` would fail with `ErrConnNotFound` on every non-caller recipient.
  - `inboxAdapter := executor.NewProtocolInboxAdapter(exec)` hoisted into `buildEngine` — used both by `Server.Executor` and by `NewClientSender` (for its disconnect-on-buffer-overflow path at `protocol/sender.go:107-123`).
  - `clientSender := protocol.NewClientSender(conns, inboxAdapter)` → `fanOutSender := protocol.NewFanOutSenderAdapter(clientSender)` → `worker := subscription.NewFanOutWorker(fanOutInbox, fanOutSender, subs.DroppedChanSend())`. `DroppedChanSend()` (manager.go:130) is the write end of the Manager's dropped-client channel so the executor's post-commit drain at `executor.go:664-674` sees both evaluator-side eval errors and delivery-side buffer-overflow drops on one channel.
  - `go worker.Run(ctx)` + `workerDone := make(chan struct{})` shutdown plumbing. `engineGraph.shutdown` ordering: `exec.Shutdown() → <-runDone → close(fanOutInbox) → <-workerDone → dw.Close()`. Rationale: executor must stop publishing before we close the inbox (sending to a closed channel panics); worker must drain before durability closes so any in-flight `SendTransactionUpdateLight` completes before the commit-log goes away.
  - `buildProtocolServer` signature changed to `(inbox *executor.ProtocolInboxAdapter, conns *protocol.ConnManager, reg, cs)` — no more per-call `NewConnManager` / `NewProtocolInboxAdapter` allocation that would fork admission and delivery onto separate `*ConnManager` instances.
- `docs/embedding.md` rewritten: wiring diagram now shows the subscription fan-out graph; steps renumbered to nine (was seven) with new §5 "Wire the subscription fan-out graph" (Manager + `schemaLookupAdapter`), §7 "Wire the fan-out worker" (ClientSender + FanOutSenderAdapter + worker goroutine), and §9 shutdown-ordering updated to include the `close(fanOutInbox)` step. "Subscriptions — deferred" bullet removed from the out-of-scope list.

Pins landed:

- `cmd/shunter-example/main_test.go` `TestFanOut_SubscriberReceivesReducerInsert` (new, 1 pin): two WebSocket clients dial `/subscribe` with the reference subprotocol. Subscriber sends `SubscribeSingleMsg{QueryString: "SELECT * FROM greetings"}` and reads `SubscribeSingleApplied` (asserting `TableName == "greetings"`). Caller sends `CallReducerMsg{ReducerName: "say_hello", Args: []byte("hola"), Flags: FullUpdate}`. Subscriber must then receive `TransactionUpdateLight` (tag 8) whose `Update[0].TableName == "greetings"` and whose `Inserts` payload is non-empty (proves the reducer's row round-tripped through `FanOutSenderAdapter.encodeRows`). `readUntilTag` helper swallows intermediate frames so the pin tolerates interleaving. Pre-wiring, this test would time out at the `readUntilTag(TransactionUpdateLight)` step because `noopSubs.EvalAndBroadcast` returns without publishing.

Intentionally out of scope (carried forward on queue):

- `executor.Scheduler` wiring still needs an exported accessor for the unexported `inbox` channel (queue #2). Example continues to pass `nil` to `Executor.Startup`.
- Protocol `AuthModeStrict` wiring — the example stays on anonymous auth.

Verification:

- `rtk go build ./cmd/shunter-example` → clean
- `rtk go vet ./cmd/shunter-example` → `Go vet: No issues found`
- `rtk go fmt ./cmd/shunter-example` → clean
- `rtk go test ./cmd/shunter-example -count=1 -race` → 4 passed (3 baseline + 1 new)
- `rtk go test ./... -count=1` → 1621 passed in 11 packages (baseline 1620 + 1 new)

Ledger / debt follow-through:

- Not an OI — narrow consumer-side wiring of landed spec surfaces (subscription Manager / FanOutWorker / FanOutSenderAdapter were all already in the tree; only the example binary was on `noopSubs`). No TECH-DEBT entry. Follow-on queue item #1 closed; queue reduces to item #2 (expose executor inbox for scheduler wiring).

## What just landed (2026-04-22, Subscription `IndexRange` migration — follow-on queue #1 (post-OI-010), closed)

Narrow consumer-side rewire of `subscription/register_set.go` `initialQuery` default branch onto `CommittedReadView.IndexRange`. Prerequisite (efficient `CommittedSnapshot.IndexRange` backed by `BTreeIndex.SeekBounds`) landed earlier 2026-04-22. Before this slice, a bare `ColRange` predicate on an indexed column still took the `view.TableScan(t) + MatchRow` fallback — a full ordered walk with per-row bound recheck. The range-bounds primitive existed all the way down to the BTree, but subscription did not consume it.

Landed:

- `subscription/register_set.go` `initialQuery` default branch: when the predicate is a bare `ColRange` and `m.resolver.IndexIDForColumn(r.Table, r.Column)` resolves, the branch now calls `view.IndexRange(t, idxID, lower, upper)` directly. `lower`/`upper` are built by shape-copy of the subscription `Bound` into `store.Bound` (identical `Value/Inclusive/Unbounded` fields). Compound shapes (`And`, `Or`), `ColEq`/`ColNe`/`AllRows`, ranges on an unindexed column, and nil-resolver all stay on the `TableScan+MatchRow` fallback. No residual `MatchRow` is applied after `IndexRange` — the BTree range walk already honors `Bound` semantics via `matchesLowerBound/UpperBound` (see `store/snapshot.go:157-177`), and the single-column `IndexKey.Compare` reduces to the same `Value.Compare` ordering the residual would use. `UnregisterSet` final-delta computation rides the same `initialQuery` dispatch and therefore also hits `IndexRange` for indexed `ColRange` subs.
- `subscription/testharness_test.go` `mockCommitted.IndexRange` — was a stub returning the empty iterator; now honors `Bound` semantics via per-row `Value.Compare` filtered against the column mapped by `idxCol[(tableID, indexID)]`. Required because `buildMockCommitted` registers `syntheticIndexID(t, col)` for every declared-indexed column, so the rewired `initialQuery` now routes through `IndexRange` on fixtures the property-test suite relies on.

Pins landed:

- `subscription/register_set_indexrange_test.go` (new, 9 pins, via `countingCommitted` wrapper recording `tableScanCalls` / `indexRangeCalls` / `indexSeekCalls`):
  - `IndexedColRangeUsesIndexRange` — inclusive-inclusive on indexed col → 1 `IndexRange`, 0 `TableScan`, 3 rows.
  - `IndexedColRangeExclusiveBounds` — `(2,5)` on 1..5 → 2 rows.
  - `IndexedColRangeUnboundedLow` / `UnboundedHigh` — half-open shapes.
  - `UnindexedColRangeUsesTableScan` — predicate on declared-but-unindexed column 1 → 0 `IndexRange`, 1 `TableScan`.
  - `NilResolverUsesTableScan` — `NewManager(s, nil)` → fallback even on would-be-indexed col.
  - `CompoundAndStaysOnTableScan` — `And{ColRange, ColEq}` stays on TableScan+MatchRow (intentional narrow scope).
  - `IndexedColRangeEmpty` — `low > high` → 1 `IndexRange`, 0 rows (semantic preservation).
  - `UnregisterSetFinalDeltaIndexedColRangeUsesIndexRange` — tear-down final-delta also uses `IndexRange`.

Verification:

- `rtk go build ./subscription` → clean
- `rtk go vet ./subscription` → `Go vet: No issues found`
- `rtk go fmt ./subscription` → clean
- `rtk go test ./subscription -count=1` → 329 passed (baseline 320 + 9 new)
- `rtk go test ./... -count=1` → 1620 passed in 11 packages (baseline 1611 + 9 new)

Ledger / debt follow-through:

- Not an OI — narrow consumer migration onto a landed spec deliverable. No TECH-DEBT entry. Follow-on queue item #1 closed; queue reduces to items #2 (subscription fan-out wiring in `cmd/shunter-example`) and #3 (expose executor inbox for scheduler wiring).

## What just landed (2026-04-22, CommittedSnapshot.IndexRange SPEC-001 §7.2 drift fix)

Narrow store-side drift closure against `SPEC-001 §7.2` / `docs/decomposition/001-store/epic-7-read-only-snapshots/story-7.1-committed-read-view.md:56` ("Calls `Index.SeekBounds(lower, upper)`"). Before this slice, `CommittedSnapshot.IndexRange` (`store/snapshot.go:119`) used `idx.BTree().Scan()` + per-row `ExtractKey` + `matchesLowerBound`/`matchesUpperBound` filter — a full ordered BTree walk regardless of bound narrowness. This was the v0 inefficient path referenced indirectly in the OI-010 follow-on queue entry about "Subscription `SeekIndexBounds` migration"; the queue wording said "`StateView.SeekIndexBounds`" but subscription consumes `CommittedReadView`, not `StateView`, so the real prerequisite was closing the SPEC-001 §7.2 drift on the CommittedReadView surface so subscription's eventual rewire to `view.IndexRange(...)` does not regress perf.

Landed:

- `store/snapshot.go` `CommittedSnapshot.IndexRange` rewritten to delegate to `slices.Collect(idx.BTree().SeekBounds(lower, upper))`. Binary-search start point now hits directly via the OI-010 `BTreeIndex.SeekBounds` primitive; per-row key extraction + bound recheck removed (redundant — SeekBounds already filters by Bound semantics). OI-005 aliasing closure preserved by the `slices.Collect` at the CommittedReadView boundary, mirroring the `StateView.SeekIndexBounds` precedent.

Pins landed:

- `store/snapshot_indexrange_seekbounds_test.go` (new, 8 pins):
  - `ExclusiveLowInclusiveHigh` — `(2,4]` on 1..5 yields {3,4}.
  - `ExclusiveLowExclusiveHigh` — `(2,4)` yields {3}.
  - `UnboundedHigh` — `[3,+∞)` yields {3,4,5}.
  - `BothUnboundedEqualsOrderedScan` — full ordered row-by-row scan.
  - `EmptyRangeLowGreaterThanHigh` — `[4,2]` yields empty.
  - `ExclusiveEndpointsAtSameKey` — `(3,3)` yields empty.
  - `EarlyBreak` — `iter.Seq2` break contract on consumer.
  - `IteratesIndependentRowIDsAfterBTreeMutation` — OI-005 aliasing pin; BTree `Remove` mid-iter cannot drift iteration (mirrors `TestStateViewSeekIndexBoundsIteratesIndependentRowIDsAfterBTreeMutation`).

No production-code touches outside `store/snapshot.go`. No consumer migration in this slice — subscription's `initialQuery` TableScan+MatchRow fallback for bare `ColRange` predicates remains on the Tier-3-equivalent path pending the follow-on slice (now unblocked).

Verification:

- `rtk go build ./store` → clean
- `rtk go vet ./store` → `Go vet: No issues found`
- `rtk go fmt ./store` → clean
- `rtk go test ./store -count=1` → 117 passed (baseline 108 before OI-010 + 1 existing `TestCommittedSnapshotIndexRangeBoundSemantics` + 8 new)
- `rtk go test ./... -count=1` → 1611 passed in 11 packages (baseline 1603 + 8 new)

Ledger / debt follow-through:

- Not an OI — narrow drift-closure slice against a landed spec deliverable. No TECH-DEBT entry. Follow-on queue item #1 ("Subscription `SeekIndexBounds` migration") wording is now accurate to reword as "Subscription `IndexRange` migration" — the CommittedReadView surface is efficient; the consumer rewire to `view.IndexRange(t, idxID, lower, upper)` for bare `ColRange` predicates in `subscription/register_set.go` `initialQuery` is the next narrow slice.

## What just landed (2026-04-22, protocol test-arity cleanup)

Mechanical compile-drift closure against the landed `schema.SchemaRegistry.TableByName` 3-value signature `(TableID, *TableSchema, bool)`. Before this slice, `protocol/handle_oneoff_test.go` (23 sites) and `protocol/handle_subscribe_test.go` (17 call sites + 1 local adapter wrapping `reg.TableByName`) still destructured the old 2-value return, so `go test ./protocol` failed to build and full-repo `go test ./...` excluded `./protocol`. Not an OI — it was Priority 1 on the follow-on queue, a stale-test drift against an already-landed registry change.

Landed:

- `protocol/handle_oneoff_test.go` — 23 sites rewritten from `xReg, ok := eng.Registry().TableByName(...)` to `_, xReg, ok := eng.Registry().TableByName(...)`. `xReg` was already being consumed as `*TableSchema` (via `xReg.ID`, `xReg.Columns`), so the edit is a prefix-only addition of the ignored TableID.
- `protocol/handle_subscribe_test.go` — same 17-site destructure rewrite. Additionally the local narrow adapter `registrySchemaLookup.TableByName` (wraps `schema.SchemaRegistry`) simplified from a reassemble-the-tuple body to a straight passthrough `return r.reg.TableByName(name)` now that the wrapped return shape matches the adapter's interface contract.

No production-code touches. No new pins — the handle_oneoff / handle_subscribe test suites already exercise the 3-tuple return end-to-end; this slice just unblocks them.

Verification:

- `rtk go build ./protocol` → clean
- `rtk go vet ./protocol` → `Go vet: No issues found`
- `rtk go test ./protocol -count=1` → 435 passed
- `rtk go vet ./...` → clean
- `rtk go test ./... -count=1` → 1603 passed in 11 packages (baseline 1589 in 10 — `./protocol` now participates in the full-repo run)

Ledger / debt follow-through:

- Not an OI; TECH-DEBT.md OI-012 carry-forward note at `TECH-DEBT.md:405` is now stale (the described drift is gone) but left as-is for history — future readers verify against the closed-slice `git log` entry and the current green `go test ./...`.

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

OI-008 / OI-009 / OI-010 / OI-011 / OI-012 are all closed. Follow-on queue items #1 (IndexRange consumer migration) and #1 (subscription fan-out wiring in `cmd/shunter-example`) are both closed. No remaining `open` OIs. Pick one from the queue below, open no more than one at a time.

## Follow-on queue

1. **Expose executor inbox for scheduler wiring** — `NewScheduler(inbox chan<- ExecutorCommand, ...)` reaches the executor's unexported `inbox`. Production embedders that want sys_scheduled replay need an exported accessor (e.g. `Executor.SchedulerFor(tableID)` or `Executor.Inbox()`). Lets the OI-008 example pass a real `*Scheduler` to `Startup`.

Pick scope before starting. Do not open multiple OIs at once.

## Startup notes

- Read `CLAUDE.md` first, then `RTK.md` for command rules, then `docs/EXECUTION-ORDER.md` for sequencing.
- Use `git log` for slice provenance; this file is current-state only.
- Before changing a file, verify against live code — memory/ledger claims can drift.
