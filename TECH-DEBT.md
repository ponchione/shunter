# TECH-DEBT

This file tracks grounded implementation/spec drift and audit findings discovered during code-vs-spec review.

Status conventions:
- open: confirmed mismatch or missing coverage
- resolved: fixed in code and/or docs
- doc-drift: implementation is acceptable, docs should be updated

## Audit phase plan

Current planned audit sequence follows `docs/EXECUTION-ORDER.md` Phase 1 foundation order, keeping the intentional contract-slice exceptions:
1. `SPEC-001 E1` Core Value Types — audited
2. `SPEC-006 E2` Struct Tag Parser — audited
3. `SPEC-006 E1` Schema Types & Type Mapping — audited
4. `SPEC-003 E1.1 + E1.2 + minimal E1.4 contract slice` — audited for early-gate sufficiency
5. `SPEC-006 E3.1` Builder core — audited
6. `SPEC-006 E4` Reflection-path registration — audited
7. `SPEC-006 E3.2` Reducer registration — audited
8. `SPEC-006 E5` Validation/Build/SchemaRegistry — audited; startup schema compatibility checking gap was fixed and logged as resolved `TD-004`
9. `SPEC-006 E6` Schema export — audited
10. `SPEC-001 E2` Schema & table storage — audited
11. `SPEC-001 E3` B-tree index engine — audited
12. `SPEC-001 E4` Table indexes & constraints — audited
13. `SPEC-001 E5` Transaction layer — audited
14. `SPEC-001 E6` Commit, rollback & changeset — audited
15. `SPEC-001 E7` Read-only snapshots — audited
16. `SPEC-001 E8` Auto-increment & recovery — audited
17. `SPEC-002 E1` BSATN codec — audited
18. `SPEC-002 E2` Record format & segment I/O — audited
19. `SPEC-003 E2` Reducer Registry — audited
20. `SPEC-003 E3` Executor Core — audited
21. `SPEC-003 E4` Reducer Transaction Lifecycle — audited
22. `SPEC-002 E3` Changeset Codec — audited
23. `SPEC-002 E5` Snapshot I/O — audited
24. `SPEC-002 E4` Durability Worker — audited
25. `SPEC-002 E6` Recovery — audited
26. `SPEC-002 E7` Log Compaction — audited
27. `SPEC-004 E1` Predicate Types & Query Hash — audited
28. `SPEC-004 E2` Pruning Indexes — audited
29. `SPEC-004 E3` DeltaView & Delta Computation — audited
30. `SPEC-004 E4` Subscription Manager — audited
31. `SPEC-004 E6.1-enabling contract slice` — audited
32. `SPEC-004 E5` Evaluation Loop — audited
33. `SPEC-004 E6 remainder` — next audit slice

Audit notes:
- `SPEC-006 E2` (`schema/tag.go`, `schema/tag_test.go`) appears operationally aligned with the tag-parser stories. No new debt logged from that slice at this time.
- `SPEC-006 E1` is mostly aligned operationally (`schema/types.go`, `schema/typemap.go`, `schema/naming.go`, `schema/valuekind_export.go`), but one concrete contract gap was found and logged below: no live `ErrSequenceOverflow` sentinel is defined anywhere even though the spec/decomposition assigns that contract to this foundation slice.
- The narrowed `SPEC-003` Phase-1 contract slice (`E1.1 + E1.2 + minimal E1.4`) appears operationally present enough for early dependency gating: foundation enums/IDs exist, reducer request/response types exist, and a minimal scheduler interface shell exists. The remaining meaningful executor gap is still the broader Epic 1 surface already tracked as `TD-002`, rather than a new blocker inside the intentionally narrowed slice.
- `SPEC-006 E3.1` builder core appears operationally aligned: `NewBuilder`, `TableDef`, `SchemaVersion`, `EngineOptions`, and chaining behavior are implemented and covered by tests. I have not logged a separate builder-core debt item from that slice.
- `SPEC-006 E4` reflection-path registration is mostly present (`schema/reflect.go`, `schema/reflect_build.go`, `schema/register_table.go`), but one concrete contract gap was found and logged below: anonymous embedded fields are processed before `shunter:"-"` exclusion, so excluded embedded structs are still flattened and excluded embedded pointer-to-struct fields still error.
- `SPEC-006 E3.2` reducer registration is functionally present (`schema/builder.go`, `schema/validate_schema.go`, `schema/registry.go`), but one API-surface gap was found and logged below: the schema package does not expose `ReducerHandler` / `ReducerContext` aliases even though SPEC-006 presents reducer registration as part of the schema-facing API surface.
- `SPEC-006 E5` validation/build work is now audited; Story 5.6 startup schema compatibility checking was fixed and logged as resolved `TD-004`, and Story 5.4's read-only `SchemaRegistry` contract is also fixed by returning detached lookup copies.
- `SPEC-006 E6` schema export is not implemented at all in live code: there is no `schema/export.go`, no export value types, no `Engine.ExportSchema()`, no JSON-contract tests, and no `cmd/shunter-codegen` tool surface. I logged the primary engine-surface gap below.
- `SPEC-001 E2` schema-backed table storage is operationally present (`store/table.go`, `store/validate.go`, `store/store_test.go`), but one important contract gap was found and logged below: inserted rows are not detached from caller-owned `ProductValue` slices, so stored rows remain externally mutable.
- `SPEC-001 E3` B-tree index engine is mostly present (`store/index_key.go`, `store/btree_index.go`, related tests), but Story 3.1's public `Bound` helper contract is entirely missing even though later range semantics docs still reference it. I logged the concrete API-surface gap below.
- `SPEC-001 E4` table indexes & constraints appear operationally aligned, but one concrete spec-vs-implementation drift item was found: Story 4.1 documents an `Index.unique` field, while the live implementation derives uniqueness solely from `IndexSchema.Unique` and omits the redundant field. I logged that below as doc drift rather than a product bug.
- `SPEC-001 E5` transaction-layer behavior is mostly present (`store/committed_state.go`, `store/tx_state.go`, `store/transaction.go`, `store/snapshot.go`, related tests), but one concrete contract gap was found and logged below: Story 5.3's public `StateView` surface is entirely missing even though transaction behavior currently inlines some of that logic.
- `SPEC-001 E6` commit/changeset behavior is mostly present (`store/changeset.go`, `store/commit.go`, related tests), but one concrete rollback contract gap was found and logged below: `Rollback` marks a flag that is never enforced, so rolled-back transactions remain reusable and can still commit mutations.
- `SPEC-001 E7` snapshot support is operationally present enough for basic row-count and commit-blocking behavior, but one concrete API-contract gap was found and logged below: `CommittedReadView` does not expose the documented `IndexScan` / Bound-based range surface.
- `SPEC-001 E8` has partial recovery support (`Sequence`, `ApplyChangeset`, `NextID`/`SetNextID`), but one major feature gap was found and logged below: autoincrement is not integrated into tables/transactions at all, so zero-valued inserts into autoincrement columns are not rewritten.
- `SPEC-002 E1` BSATN codec is mostly present (`bsatn/encode.go`, `bsatn/decode.go`, `bsatn/errors.go`, tests), but one concrete row-decoder contract gap was found and logged below: `DecodeProductValue` accepts extra encoded values silently instead of treating them as a row-shape mismatch.
- `SPEC-002 E2` record/segment I/O is operationally present enough for happy-path framing, CRC, and reader/writer behavior (`commitlog/segment.go`, `commitlog/errors.go`, `commitlog/commitlog_test.go`, `commitlog/phase4_acceptance_test.go`), but two concrete contract gaps were found and logged below: the public exported error/reader API does not match the documented surface, and `SegmentWriter` does not enforce that the first appended tx matches the segment's declared `startTx`.
- `SPEC-003 E2` reducer registry behavior is broadly present (`executor/registry.go`, registry-related executor tests, `executor/phase4_acceptance_test.go`), but one important immutability gap was found and logged below: `Lookup`/`All` return mutable internal reducer pointers, so callers can still mutate the supposedly frozen registry after startup.
- `SPEC-003 E3` executor core is partially present (`executor/executor.go`, `executor/executor_test.go`, `executor/phase4_acceptance_test.go`), but two concrete contract gaps were found and logged below: `SubmitWithContext` ignores reject-on-full semantics, and Story 3.4's subscription-command dispatch path is still absent.
- `SPEC-003 E4` reducer transaction lifecycle is operationally present in behavior (`executor/executor.go`, `executor/phase4_acceptance_test.go`, related lifecycle tests), but one important public contract gap was found and logged below: `ReducerContext` still exposes `DB` and `Scheduler` as `any`, not the typed `*Transaction` / `SchedulerHandle` surface the spec/decomposition promise.
- `SPEC-002 E3` changeset codec behavior is now audited; the public decoder surface was aligned to the documented two-argument API and tracked as resolved `TD-023`.
- `SPEC-002 E5` snapshot I/O is now audited and operationally present (`commitlog/snapshot_io.go`, `commitlog/snapshot_test.go`); the previously missing snapshot surface was fixed and tracked as resolved `TD-024`.
- `SPEC-002 E4` durability worker is now audited and operationally present (`commitlog/durability.go`, `commitlog/commitlog_test.go`, `commitlog/phase4_acceptance_test.go`); the reopen/resume truncation issue and missing `SnapshotInterval` option were fixed and tracked as resolved `TD-025` and `TD-026`.
- `SPEC-002 E6` recovery is mostly operationally present (`commitlog/segment_scan.go`, `snapshot_select.go`, `replay.go`, `recovery.go`, related tests), but two concrete recovery/resume gaps remain and are logged below: active-tail checksum mismatches after a valid prefix are still treated as hard failures instead of truncation-at-horizon, and append-open still truncates first-record corruption instead of failing closed.
- `SPEC-002 E7` log compaction appears operationally aligned (`commitlog/compaction.go`, `compaction_test.go`) with the current compaction stories. I did not log a new compaction debt item from that slice.
- `SPEC-004 E1` predicate/query-hash foundations appear operationally aligned (`subscription/predicate.go`, `validate.go`, `hash.go`, `register.go`, related tests). The sealed predicate interface was also verified via an external compile-only repro that failed with `unexported method sealed`. I did not log a new debt item from this slice.
- `SPEC-004 E2` pruning indexes appear operationally aligned (`subscription/value_index.go`, `join_edge_index.go`, `table_index.go`, `placement.go`, related tests). Tier 1/2/3 structures, placement, cleanup, and candidate-union behavior are present; I did not log a new debt item from this slice.
- `SPEC-004 E3` delta computation is mostly present (`subscription/delta_view.go`, `delta_single.go`, `delta_join.go`, `delta_dedup.go`, related tests), but one real implementation gap remains: Story 3.5's allocation-discipline contract is only partially implemented. The earlier Story 3.2/3.3 helper-signature doc drift has now been reconciled.
- `SPEC-004 E4` subscription-manager behavior is operationally present (`subscription/manager.go`, `query_state.go`, `register.go`, `unregister.go`, `disconnect.go`, related tests). The earlier Story 4.1 query-state drift, registration-request `ClientIdentity` drift, and `PostCommitMeta` interface drift have now been reconciled.
- `SPEC-004 E6.1-enabling contract slice` is operationally present (`subscription/fanout.go`, `fanout_worker.go`, related tests). The earlier Story 6.1 constructor/ownership drift and `FanOutMessage`-shape drift have now been reconciled.
- `SPEC-004 E5` evaluation-loop behavior is operationally present (`subscription/eval.go`, `eval_test.go`, `property_test.go`, `bench_test.go`), including the landed Story 5.2/5.3 follow-through on helper-surface docs and memoized encoding.
- Verification runs completed during audit:
  - `rtk go test ./schema`
  - `rtk go test ./schema ./executor`
  - `rtk go test ./schema ./store ./executor`
  - `rtk go test ./executor`
  - `rtk go test ./commitlog`
  - `rtk go test ./subscription`
  - latest recovery/compaction pass: `rtk go test ./...`
  - earlier broad pass: `rtk go test ./types ./bsatn ./schema ./store ./subscription ./executor ./commitlog`

### TD-116: SPEC-004 E3 Story 3.5 allocation-discipline contract is only partially implemented

Status: resolved
Severity: medium
First found: SPEC-004 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 5 / Step 5c (`SPEC-004 E3: DeltaView & Delta Computation`)

Resolution:
- `subscription/delta_pool.go` now owns the missing hot-path scratch pools: 4 KiB `[]byte` buffers, candidate scratch maps, reusable `[]ProductValue` slices, pooled DeltaView instances, and pooled delta-index maps.
- `subscription/delta_view.go` now builds DeltaViews from pooled scratch and exposes `(*DeltaView).Release()` so insert/delete row backing storage and delta-index maps are returned after evaluation/benchmark use.
- `subscription/eval.go` now reuses one candidate scratch allocation per evaluation cycle via `collectCandidatesInto(...)`, and `subscription/placement.go` routes the per-table helper through the same pooled candidate-set pattern.
- `subscription/hash.go` and `subscription/delta_dedup.go` now route canonical row/hash encoding through the pooled 4 KiB buffer lifecycle, dropping oversized buffers instead of retaining them.
- Focused regression tests now verify buffer reuse, oversized-buffer drop behavior, candidate-map reuse, and sequential DeltaView backing-slice reuse.

Verification:
- `rtk go test ./subscription`
- `rtk go test ./...`

### TD-117: SPEC-004 E3 story docs are stale on the public helper signatures used for delta evaluation

Status: resolved
Severity: low
First found: SPEC-004 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 5 / Step 5c (`SPEC-004 E3: DeltaView & Delta Computation`)

Resolution:
- Story 3.2 now documents the live exported helper signature `MatchRow(pred Predicate, table TableID, row ProductValue) bool` and explains the cross-table "no constraint" behavior used by join-filter evaluation.
- Story 3.3 now documents the live `JoinFragments` return struct plus `EvalJoinDeltaFragments(dv *DeltaView, join *Join, resolver IndexResolver) JoinFragments` signature.
- Story 3.3 also now refers to `DeltaView.CommittedIndexSeek` plus committed-row materialization instead of the stale `CommittedIndexScan` name.

Verification:
- re-read patched decomposition docs against `subscription/delta_single.go`, `subscription/delta_join.go`, and `subscription/delta_view.go`
- no code changes required

---

### TD-118: SPEC-004 E4 Story 4.1 still documents subscriber/query-registry shapes that cannot represent the live multi-subscription model

Status: resolved
Severity: medium
First found: SPEC-004 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 5 / Step 5d (`SPEC-004 E4: Subscription Manager`)

Summary:
- Story 4.1 had documented `queryState.subscribers` correctly as a nested per-connection subscription set, but `queryRegistry.bySub` and related acceptance criteria still treated `SubscriptionID` as globally unique.
- That shape could not represent two different connections reusing the same numeric `SubscriptionID`, and it underspecified the reverse-lookup contract the manager actually needs.
- The decomposition docs now model reverse lookup as `(connID, subID)`-aware and explicitly cover same-connection multi-subscription tracking plus cross-connection `SubscriptionID` reuse.

Why this matters:
- This is story-surface drift, not a runtime bug.
- The live manager behavior is coherent and covered by tests, but Story 4.1 still describes a narrower state model than the code actually exposes.
- Future implementation or audit work would incorrectly conclude that same-connection duplicate subscriptions are unsupported even though they are explicitly tested today.

Related code:
- `subscription/query_state.go:7-29`
  - live `queryState.subscribers` is `map[ConnectionID]map[SubscriptionID]struct{}` and `queryRegistry.bySub` is keyed by `subscriptionRef{connID, subID}`
- `subscription/query_state.go:59-77`
  - `addSubscriber(...)` records multiple subscription IDs per connection and increments `refCount` per subscription
- `subscription/query_state.go:83-117`
  - `removeSubscriber(...)` removes by `(connID, subID)` and only drops the per-connection entry when its subscription set becomes empty
- `subscription/query_state_test.go:35-52`
  - same connection can hold multiple subscription IDs for one query hash
- `subscription/manager_test.go:123-136`
  - same numeric `SubscriptionID` can be reused across different connections

Related spec / decomposition docs:
- `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.1-query-state.md:16-78`
  - now documents `subscriptionRef{connID, subID}`, a per-connection `byConn` set, and acceptance criteria for same-connection multi-subscription tracking plus cross-connection `SubscriptionID` reuse
- `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.3-unregister.md:27-41`
  - unregister cleanup now explicitly preserves other connections that reuse the same numeric `SubscriptionID`
- `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.4-disconnect-client.md:24-36`
  - disconnect cleanup now describes removing `(connID, subID)` reverse-lookup entries for the dropped connection

Current observed behavior:
- Story 4.1 now documents `subscriptionRef{connID, subID}` for reverse lookups, a per-connection subscription set in `byConn`, and acceptance criteria covering both same-connection multi-subscription tracking and same-`SubscriptionID` reuse across different connections.
- Stories 4.3 and 4.4 now describe unregister/disconnect cleanup in `(connID, subID)` terms instead of implying global `SubscriptionID` uniqueness.
- Verification for this docs-only resolution:
  - re-read the patched Story 4.1 / 4.3 / 4.4 docs against the TD-118 evidence and acceptance intent
  - `rtk grep -n "map\[SubscriptionID\]QueryHash|subID → queryHash|byConn, bySub" docs/decomposition/004-subscriptions/epic-4-subscription-manager`

Recommended resolution:
- Resolved by updating Story 4.1's `queryRegistry`/acceptance-criteria surface and tightening the Story 4.3/4.4 cleanup language to the `(connID, subID)`-aware model.

### TD-119: SPEC-004 §4.1 registration docs omit the client identity that the live query-hash path requires for parameterized subscriptions

Status: resolved
Severity: medium
First found: SPEC-004 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 5 / Step 5d (`SPEC-004 E4: Subscription Manager`)

Summary:
- SPEC-004 §3.4 says parameterized predicates hash the predicate structure plus client identity.
- The registration surface had drifted so the canonical request no longer carried that identity through the documented §4.1 / Story 4.2 flow, even though live code expected `ClientIdentity *types.Identity`.
- The docs now restore that field and tie it directly to `ComputeQueryHash(...)` for parameterized subscriptions.

Why this matters:
- This is a spec-surface drift item, not an implementation bug.
- Without a client-identity field on the registration request, the documented Epic 4 registration flow cannot actually produce the per-client query hashes that SPEC-004 §3.4 promises.
- The live implementation and tests already rely on the stronger request shape, so the docs are now the stale surface.

Related code:
- `subscription/manager.go:11-17`
  - live `SubscriptionRegisterRequest` includes `ClientIdentity *types.Identity`
- `subscription/register.go:19`
  - `Register(...)` computes the hash with `ComputeQueryHash(req.Predicate, req.ClientIdentity)`
- `subscription/manager_test.go:85-120`
  - distinct `ClientIdentity` values produce distinct registered query hashes

Related spec / decomposition docs:
- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md:112-145`
  - §4.1 now includes `ClientIdentity *Identity` on `SubscriptionRegisterRequest` and explicitly appends `ClientIdentity` bytes during query-hash computation for parameterized predicates
- `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.2-register.md:16-37`
  - registration steps now compute the hash with `req.ClientIdentity` and treat the request/result types as the canonical registration contract
- `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.5-manager-interface.md:29-31,61`
  - Story 4.5 keeps the canonical request type declaration tied to registration behavior and explicitly requires `SubscriptionRegisterRequest` to carry `ClientIdentity`

Current observed behavior:
- SPEC-004 §4.1 now documents `ClientIdentity *Identity` on `SubscriptionRegisterRequest` and ties parameterized query hashing directly to that field.
- Story 4.2 now computes `ComputeQueryHash` with `req.ClientIdentity`, and Story 4.5's acceptance criteria require the request type to carry `ClientIdentity` for parameterized-hash computation.
- Verification for this docs-only resolution:
  - re-read the patched SPEC-004 §4.1 and Story 4.2 / 4.5 registration surfaces against the TD-119 evidence
  - `rtk grep -n "ClientIdentity|client identity" docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.2-register.md docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.5-manager-interface.md`

Recommended resolution:
- Resolved by restoring `ClientIdentity` to the canonical SPEC-004 §4.1 registration request and aligning Story 4.2 / Story 4.5 so the parameterized query-hash path is explicitly modeled end-to-end.

### TD-120: SPEC-004 E4 still documents the pre-`PostCommitMeta` `SubscriptionManager.EvalAndBroadcast` interface

Status: resolved
Severity: medium
First found: SPEC-004 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 5 / Step 5d (`SPEC-004 E4: Subscription Manager`)

Summary:
- The Epic 4 manager-interface docs had drifted behind the live executor/subscription seam.
- Live `SubscriptionManager` requires `EvalAndBroadcast(txID, changeset, view, meta PostCommitMeta)`, and the concrete manager uses that metadata when assembling `FanOutMessage`.
- The docs now publish the same `PostCommitMeta`-carrying interface in both SPEC-004 §10.1 and Story 4.5.

Why this matters:
- This is interface-surface drift, not a package-runtime failure.
- The live post-commit path already depends on metadata such as durability state and caller result/context, so consumers written against the published E4 interface would not match the real boundary.
- Keeping Story 4.5 stale makes later SPEC-004 E5/E6 audit work harder because the documented seam is now one generation behind the code.

Related code:
- `subscription/manager.go:47-55`
  - live `SubscriptionManager` interface includes `EvalAndBroadcast(..., meta PostCommitMeta)`
- `subscription/eval.go:25-38`
  - `EvalAndBroadcast(...)` consumes `meta.TxDurable`, `meta.CallerConnID`, and `meta.CallerResult` when constructing `FanOutMessage`

Related spec / decomposition docs:
- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md:642-667`
  - §10.1 now documents `EvalAndBroadcast(txID, changeset, view, meta PostCommitMeta)` and declares the `PostCommitMeta` struct directly beneath the interface
- `docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.5-manager-interface.md:16-25`
  - Story 4.5 now repeats the same `EvalAndBroadcast(..., meta PostCommitMeta)` interface shape

Current observed behavior:
- SPEC-004 §10.1 and Story 4.5 both now publish `EvalAndBroadcast(..., meta PostCommitMeta)`.
- SPEC-004 §10.1 also declares `PostCommitMeta { TxDurable, CallerConnID, CallerResult }`, matching the live manager/eval seam used to assemble `FanOutMessage`.
- Verification for this docs-only resolution:
  - re-read the patched SPEC-004 §10.1 and Story 4.5 surfaces against the TD-120 evidence
  - `rtk grep -n "EvalAndBroadcast\(|PostCommitMeta" docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md docs/decomposition/004-subscriptions/epic-4-subscription-manager/story-4.5-manager-interface.md`

Recommended resolution:
- Resolved by aligning Story 4.5 and SPEC-004 §10.1 to the live `PostCommitMeta`-carrying `SubscriptionManager` interface.

---

### TD-121: SPEC-004 E6.1 story docs still publish a `FanOutWorker` constructor/API surface that no longer matches the live narrowed contract

Status: resolved
Severity: medium
First found: SPEC-004 E6.1-enabling contract audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 5 / Step 5e (`SPEC-004 E6.1-enabling contract slice`)

Summary:
- Story 6.1 had drifted behind the live narrowed worker contract.
- Live code injects the inbox, sender, and manager-owned dropped-client write channel, and it does not expose a worker `DroppedClients()` method.
- The story now matches that injected-worker ownership model instead of the older standalone worker-owns-everything shape.

Why this matters:
- This is a public API/story-surface drift item, not a runtime correctness bug.
- The execution-order doc explicitly narrowed this stop to an E6.1-enabling contract slice, but Story 6.1 still reads like the older standalone worker-owns-everything design.
- Future work following the current story literally would build against the wrong constructor and channel-ownership model.

Related code:
- `subscription/fanout_worker.go:32-49`
  - live `FanOutWorker` stores injected `<-chan FanOutMessage`, `FanOutSender`, and `chan<- ConnectionID`; it does not own an internally created dropped channel
- `subscription/fanout_worker.go:43-49`
  - live constructor is `NewFanOutWorker(inbox <-chan FanOutMessage, sender FanOutSender, dropped chan<- types.ConnectionID)`
- `subscription/manager.go:102-109`
  - manager owns the dropped-client channel and exposes read/send ends via `DroppedClients()` / `DroppedChanSend()`
- `subscription/fanout_worker_test.go:698-707`
  - acceptance-style wiring uses `mgr.DroppedChanSend()` when constructing the worker

Related spec / decomposition docs:
- `docs/decomposition/004-subscriptions/epic-6-fanout-delivery/story-6.1-fanout-worker.md:16-23,37-39,51-70`
  - Story 6.1 now models `inbox <-chan FanOutMessage`, `dropped chan<- ConnectionID`, the injected `NewFanOutWorker(...)` constructor, and manager-owned dropped-client drain semantics without a worker `DroppedClients()` method
- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md:589-599`
  - §8.5 continues to declare the manager-owned shared dropped-client channel and executor-side single-stream drain model

Current observed behavior:
- Story 6.1 now documents the injected `NewFanOutWorker(inbox <-chan FanOutMessage, sender FanOutSender, dropped chan<- ConnectionID)` constructor and removes the nonexistent worker `DroppedClients()` API.
- The story also now matches the live directional channel ownership: read-only inbox, write-only dropped-client sink, manager-owned executor drain.
- Verification for this docs-only resolution:
  - re-read patched Story 6.1 against `subscription/fanout_worker.go` and `subscription/manager.go`
  - `rtk grep -n "DroppedClients\(|NewFanOutWorker\(|dropped chan|worker-owned|inboxSize int" docs/decomposition/004-subscriptions/epic-6-fanout-delivery/story-6.1-fanout-worker.md`

Recommended resolution:
- Resolved by aligning Story 6.1 to the live injected-worker constructor and manager-owned dropped-channel contract.

### TD-122: SPEC-004 §8.1 / Story 6.1 still document an older `FanOutMessage` shape that omits live `TxID` and `Errors` fields

Status: resolved
Severity: medium
First found: SPEC-004 E6.1-enabling contract audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 5 / Step 5e (`SPEC-004 E6.1-enabling contract slice`)

Summary:
- The E6.1 fan-out handoff docs had drifted behind the live delivery seam.
- Live `subscription.FanOutMessage` carries `TxID`, `TxDurable`, `Fanout`, `Errors`, `CallerConnID`, and `CallerResult`, and the worker delivers `Errors` before normal updates while using `msg.TxID` for standalone `TransactionUpdate` delivery.
- SPEC-004 §8.1, Story 6.1, and Story 5.1 now publish that same handoff shape and ordering.

Why this matters:
- This is contract-surface drift, not a runtime failure.
- Story 5.1 / E6.1 are supposed to agree on the minimal handoff seam between evaluation and fan-out, but the docs currently omit fields already required by the real delivery path.
- The missing `TxID` field especially means the documented handoff cannot describe how standalone `TransactionUpdate` messages are assembled in live code.

Related code:
- `subscription/fanout.go:12-35`
  - live `FanOutMessage` includes `TxID`, `TxDurable`, `Fanout`, `Errors`, `CallerConnID`, and `CallerResult`
- `subscription/eval.go:31-38`
  - `EvalAndBroadcast(...)` populates `TxID`, `TxDurable`, `Fanout`, `Errors`, `CallerConnID`, and `CallerResult`
- `subscription/fanout_worker.go:107-144`
  - worker delivers `Errors` first, uses `msg.TxID` for `SendTransactionUpdate(...)`, and uses caller metadata for reducer-result diversion
- `subscription/fanout_worker_test.go:581-675`
  - tests cover `SubscriptionError` delivery and error-before-update ordering

Related spec / decomposition docs:
- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md:460,501-545,552-565`
  - Section 7 now sends `FanOutMessage{TxID, TxDurable, Fanout, Errors, CallerConnID, CallerResult}`, and §8.1 declares `FanOutMessage` with `TxID` plus `Errors` and states that `SubscriptionError` entries are delivered before normal updates
- `docs/decomposition/004-subscriptions/epic-6-fanout-delivery/story-6.1-fanout-worker.md:26-50`
  - Story 6.1 now repeats the six-field `FanOutMessage` shape and describes building `TransactionUpdate` for `msg.TxID` after delivering `msg.Errors` first
- `docs/decomposition/004-subscriptions/epic-5-evaluation-loop/story-5.1-eval-transaction.md:21-34`
  - Story 5.1 now summarizes step 5 as `FanOutMessage{TxID, TxDurable, Fanout, Errors, CallerConnID, CallerResult}` and notes error-before-update delivery ordering

Current observed behavior:
- SPEC-004 §8.1, Story 6.1, and Story 5.1 now all include `TxID` and `Errors` in the documented fan-out handoff shape.
- The spec/story text now also states that `SubscriptionError` entries are delivered before normal updates for the same batch and that standalone `TransactionUpdate` assembly uses `msg.TxID`.
- Verification for this docs-only resolution:
  - re-read patched SPEC-004 §8.1 and Stories 6.1 / 5.1 against `subscription/fanout.go`, `subscription/eval.go`, and `subscription/fanout_worker.go`
  - `rtk grep -n "FanOutMessage|Errors before normal updates|msg.TxID|FanOutMessage\{TxID" docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md docs/decomposition/004-subscriptions/epic-6-fanout-delivery/story-6.1-fanout-worker.md docs/decomposition/004-subscriptions/epic-5-evaluation-loop/story-5.1-eval-transaction.md`

Recommended resolution:
- Resolved by aligning SPEC-004 §8.1, Story 6.1, and Story 5.1 to the live `FanOutMessage` shape and error-before-update delivery ordering.

---

### TD-123: SPEC-004 E5 Story 5.3 memoized-encoding contract is still only a placeholder cache, not a real encoding-reuse path

Status: resolved
Severity: medium
First found: SPEC-004 Epic 5 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 5 / Step 5f (`SPEC-004 E5: Evaluation Loop`)

Resolution:
- The placeholder evaluation-loop cache is now backed by a real per-delivery-batch memoization path across the fan-out/protocol seam.
- `subscription` now owns an explicit `EncodingMemo` lifecycle object; `FanOutWorker.deliver(...)` creates one memo per `FanOutMessage` and passes it through all update/result sends for that batch.
- `protocol/FanOutSenderAdapter` now memoizes binary row-list encoding by shared `[]ProductValue` slice identity, so shared-query recipients reuse encoded row payload bytes even when per-recipient `SubscriptionID` values differ.
- Empty row-list payloads are handled without redundant encoding work, and separate delivery batches receive fresh memos so cached bytes do not leak across transactions.

Verification:
- `rtk go test ./protocol -run 'TestFanOutSenderAdapter_MemoizesRowEncodingAcrossTransactionUpdateCalls|TestFanOutSenderAdapter_MemoCacheDoesNotLeakAcrossTransactions'`
- `rtk go test ./subscription ./protocol`
- `rtk go test ./...`

### TD-124: SPEC-004 E5 Story 5.2 still documents a standalone `CollectCandidates(...)` helper that the package does not expose

Status: resolved
Severity: low
First found: SPEC-004 Epic 5 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 5 / Step 5f (`SPEC-004 E5: Evaluation Loop`)

Resolution:
- `docs/decomposition/004-subscriptions/epic-5-evaluation-loop/story-5.2-candidate-collection.md` now describes the live manager-owned whole-changeset entrypoint: `(*Manager).collectCandidates(changeset *Changeset, committed CommittedReadView) map[QueryHash]struct{}`.
- The story now explicitly states that `PruningIndexes` and `IndexResolver` come from the wired `Manager`, while `CollectCandidatesForTable(...)` remains the lower-level per-table helper owned by Story 2.4.
- This reconciles the decomposition with `subscription/eval.go` and removes the nonexistent package-level helper claim.

Verification:
- `rtk go test ./subscription ./protocol`
- `rtk go test ./...`

---

### TD-026: SPEC-002 E4 `CommitLogOptions` is missing the documented `SnapshotInterval` field

Status: resolved
Severity: medium
First found: SPEC-002 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4h (`SPEC-002 E4: Durability Worker`)

Summary:
- Story 4.1 and SPEC-002 §8 document `CommitLogOptions.SnapshotInterval uint64` with a default of 0.
- Live `commitlog.CommitLogOptions` now includes `SnapshotInterval`, and `DefaultCommitLogOptions()` now returns the documented default of 0.
- A focused regression test now guards that public options surface.

Why this matters:
- This is a public API contract gap in the documented options surface.
- The spec explicitly uses `SnapshotInterval` to describe when periodic snapshots should trigger, and Story 4.1 assigns that field to the durability-worker option struct.
- Callers written against the documented configuration contract do not compile today.

Related code:
- `commitlog/durability.go:11-18`
  - live `CommitLogOptions` has 5 fields and omits `SnapshotInterval`
- `commitlog/durability.go:20-29`
  - `DefaultCommitLogOptions()` therefore cannot return the documented default for `SnapshotInterval`

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-4-durability-worker/story-4.1-durability-handle.md:52-65`
  - documents `CommitLogOptions` including `SnapshotInterval uint64` and `DefaultCommitLogOptions()`
- `docs/decomposition/002-commitlog/epic-4-durability-worker/story-4.1-durability-handle.md:71-72`
  - acceptance criteria require documented defaults
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:529-540`
  - option catalog includes `SnapshotInterval`
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:410-414`
  - recommended default policy is `SnapshotInterval = 0`

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./commitlog`
- Targeted compile repro against the documented field failed:
  - `rtk go test ./.tmp_commitlog_e4_api`
  - observed error:
    - `opts.SnapshotInterval undefined (type commitlog.CommitLogOptions has no field or method SnapshotInterval)`

Recommended resolution options:
1. Preferred code fix:
   - add `SnapshotInterval uint64` to `CommitLogOptions`
   - set the default to 0 in `DefaultCommitLogOptions()`
   - add option-surface tests even if periodic snapshot behavior is implemented later
2. Alternative doc fix:
   - if periodic snapshot triggering is intentionally deferred beyond this slice, update Story 4.1 / SPEC-002 option docs so the option is not advertised yet
   - this is weaker than matching the current documented surface

Suggested follow-up tests:
- compile-time API test for `CommitLogOptions.SnapshotInterval`
- default-options test asserting `SnapshotInterval == 0`
- future integration test proving periodic snapshot triggering uses this field once snapshot I/O exists

### TD-025: SPEC-002 E4 `NewDurabilityWorker` recreates/truncates an existing active segment instead of opening/resuming it

Status: resolved
Severity: high
First found: SPEC-002 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4h (`SPEC-002 E4: Durability Worker`)

Summary:
- Story 4.1 says `NewDurabilityWorker` should "create or open active segment," but live code always calls `CreateSegment(dir, startTxID)`.
- `CreateSegment` uses `os.Create`, which truncates any existing segment file for that start TxID.
- As a result, constructing a durability worker against a directory that already has the active segment can silently discard previously written durable records.

Why this matters:
- This is a durability correctness bug, not just API drift.
- Even before full recovery wiring lands, the worker constructor should not destroy an existing segment it is supposed to open/resume.
- It also conflicts with Story 4.3's resume-after-crash ownership, which expects fresh-tail decisions to be based on recovery results, not unconditional truncation by constructor.

Related code:
- `commitlog/durability.go:51-64`
  - `NewDurabilityWorker(...)` always calls `CreateSegment(dir, startTxID)`
- `commitlog/segment.go:173-189`
  - `CreateSegment(...)` uses `os.Create(path)`, truncating an existing file with that name
- no alternate open/resume path exists in the live `commitlog` package

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-4-durability-worker/story-4.1-durability-handle.md:47-50`
  - constructor should create or open active segment
- `docs/decomposition/002-commitlog/epic-4-durability-worker/story-4.3-segment-rotation.md:23-25`
  - resume-after-crash logic owns opening a fresh next segment only when recovery says the writable tail must not be reused
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:428-434`
  - recovery determines valid replay horizon and damaged-tail handling before future writes resume

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./commitlog`
- Targeted runtime repro demonstrated truncation of an existing segment:
  - `rtk go run /tmp/commitlog_e4_reopen_repro.go`
  - created a segment with one record, size 29 bytes
  - constructed `NewDurabilityWorker(dir, 1, opts)` against the same directory
  - observed output: `before_size=29 after_size=8`
  - the existing segment was truncated back to header-only size instead of being opened/resumed

Recommended resolution options:
1. Preferred code fix:
   - teach `NewDurabilityWorker` to open/resume an existing active segment when appropriate rather than always calling `CreateSegment`
   - reserve fresh-segment creation for brand-new logs or explicit fresh-tail resume decisions from recovery/rotation logic
   - add tests covering both create-new and reopen-existing cases
2. Alternative temporary guard:
   - if reopen/resume is not ready yet, fail constructor when the target segment file already exists instead of truncating it
   - that still leaves resume incomplete, but it avoids silent data loss

Suggested follow-up tests:
- existing active segment is reopened without truncation
- brand-new directory still creates a fresh segment successfully
- damaged-tail resume path creates a fresh next segment only when recovery explicitly requests it
- `DurableTxID` initial value matches resume state once reopen logic exists

### TD-024: SPEC-002 E5 snapshot I/O surface is almost entirely unimplemented

Status: resolved
Severity: high
First found: SPEC-002 Epic 5 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4g (`SPEC-002 E5: Snapshot I/O`)

Summary:
- The commitlog package now implements the Epic 5 snapshot I/O surface: integrity helpers/constants, schema snapshot codec, file-backed snapshot writer, snapshot reader, and snapshot listing.
- Snapshots persist schema bytes, sequence state, per-table nextID state, and deterministic row data, with Blake3 verification on read.
- Focused regression coverage now guards the public API contract, hash/lock helpers, schema round-trip, snapshot round-trip, hash mismatch handling, list ordering, and concurrent snapshot exclusion.

Why this matters:
- Recovery and future compaction now have the bounded snapshot primitives that SPEC-002 Epic 5 promised.
- The previously missing public snapshot API surface is now exercised by package tests instead of existing only in docs.
- Snapshot reads now surface integrity failures as typed hash-mismatch errors and ignore in-progress snapshots via lockfile checks.

Related code:
- `commitlog/snapshot_io.go`
  - adds `SnapshotMagic`, `SnapshotVersion`, `SnapshotHeaderSize`
  - adds `ComputeSnapshotHash`, lockfile helpers, schema codec, writer, reader, and list APIs
- `commitlog/snapshot_test.go`
  - adds focused API/round-trip/integrity/concurrency coverage

Current observed behavior:
- Focused snapshot tests pass:
  - `rtk go test ./commitlog -run 'TestSnapshotPublicAPIContractCompiles|TestSnapshotHashAndLockHelpers|TestSchemaSnapshotCodecRoundTrip|TestCreateAndReadSnapshotRoundTrip|TestListSnapshotsSkipsLockAndSortsNewestFirst|TestReadSnapshotHashMismatch|TestConcurrentSnapshotReturnsInProgress' -count=1`
- Full package and repo verification pass:
  - `rtk go test ./commitlog`
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

### TD-023: SPEC-002 E3 `DecodeChangeset` public signature does not match the documented decoder surface

Status: resolved
Severity: medium
First found: SPEC-002 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4f (`SPEC-002 E3: Changeset Codec`)

Summary:
- `DecodeChangeset` now exposes the documented two-argument public API: `DecodeChangeset(data []byte, schema SchemaRegistry)`.
- The public decoder now sources its row-size limit from commitlog-owned defaults instead of requiring every caller to pass `maxRowBytes` explicitly.
- An unexported helper retains explicit-limit coverage for internal package tests, and focused regression coverage now guards both the public API contract and default max-row enforcement.

Why this matters:
- This is a public API contract gap: callers written against the documented changeset codec surface do not compile.
- The extra `maxRowBytes` parameter also shifts policy ownership from the commitlog package onto every caller, even though the spec/decomposition presents row-size enforcement as part of the codec/commitlog contract.
- Existing tests stay green because they use the live implementation signature rather than guarding the documented boundary.

Related code:
- `commitlog/changeset_codec.go:68-69`
  - live decoder signature is `DecodeChangeset(data []byte, reg schema.SchemaRegistry, maxRowBytes uint32)`
- `commitlog/changeset_codec.go:147-149`
  - row-length enforcement uses the caller-supplied `maxRowBytes`
- `commitlog/commitlog_test.go:145`
  - tests pass `DefaultCommitLogOptions().MaxRowBytes`
- `commitlog/phase4_acceptance_test.go:241,261,269,273`
  - acceptance tests also use the live three-argument signature

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-3-changeset-codec/story-3.2-changeset-decoder.md:16-27`
  - documents `func DecodeChangeset(data []byte, schema SchemaRegistry) (*Changeset, error)` and assigns row-size enforcement to the decoder behavior
- `docs/decomposition/002-commitlog/epic-3-changeset-codec/story-3.2-changeset-decoder.md:29-38`
  - acceptance criteria include `ErrRowTooLarge` and schema-aware decode behavior, but not an extra caller-supplied limit argument
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:129,174-180`
  - codec policy is described as part of the commitlog payload format and schema-aware recovery path

Current observed behavior:
- Focused API-contract and commitlog package tests now pass:
  - `rtk go test ./commitlog -run 'TestCommitlogPublicAPIContractCompiles|TestDecodeChangesetUsesDefaultMaxRowBytes' -count=1`
  - `rtk go test ./commitlog`
- Repo-wide build still succeeds:
  - `rtk go build ./...`
- Full repo vet/test are currently blocked by unrelated protocol worktree drift (`runReadPump`-based protocol tests and ExecutorInbox mock signature churn), not by commitlog codec behavior:
  - `rtk go vet ./...`
  - `rtk go test ./...`

Recommended resolution options:
1. Preferred code fix:
   - expose the documented two-argument `DecodeChangeset(data, schema)` entrypoint
   - source the row-size policy from commitlog-owned defaults/options internally rather than requiring every caller to pass it explicitly
   - if a lower-level helper with explicit limit is still useful, keep it unexported or add it as a clearly separate advanced API
2. Alternative doc fix:
   - if the project intentionally wants callers to provide `maxRowBytes`, update Story 3.2 and nearby SPEC-002 docs to describe that explicit third parameter and its ownership clearly
   - that would formalize the current surface instead of leaving it as silent drift

Suggested follow-up tests:
- compile-time API test for the documented two-argument `DecodeChangeset`
- runtime test proving the public decoder still enforces the intended max-row limit without per-caller policy mistakes
- test that any low-level explicit-limit helper, if retained, stays behaviorally identical to the public decoder for default options

### TD-022: SPEC-003 E4 `ReducerContext` still exposes `DB` and `Scheduler` as `any` instead of typed contracts

Status: resolved
Severity: medium
First found: SPEC-003 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4e (`SPEC-003 E4: Reducer Transaction Lifecycle`)

Summary:
- `types.ReducerContext` now exposes cycle-safe typed reducer-facing interfaces instead of erased `any` fields.
- Reducers can call `ctx.DB` and `ctx.Scheduler` methods directly without type assertions.
- The executor now binds concrete store/scheduler adapters into that typed surface, and focused contract coverage guards the direct-call reducer API.

Why this matters:
- This is a public contract gap in the reducer execution API, not just an internal implementation detail.
- Reducer authors written to the documented runtime contract cannot compile direct `ctx.DB` or `ctx.Scheduler` usage today.
- The current `any` fields also weaken the lifetime/ownership guarantees the docs try to express, because the typed boundary is erased at the public API surface.

Related code:
- `types/reducer.go:8-17`
  - `ReducerContext` defines `DB any` and `Scheduler any`
- `executor/executor.go:236-242`
  - runtime populates those fields with a `*store.Transaction` and scheduler handle, but only behind `any`
- `executor/phase4_acceptance_test.go:197-241`
  - tests must type-assert `ctx.DB.(*store.Transaction)` rather than using the documented typed field directly
- `executor/contracts_test.go:96-108`
  - the minimal contract test constructs `types.ReducerContext` with `DB: nil` and does not guard the stronger typed field contract

Related spec / decomposition docs:
- `docs/decomposition/003-executor/SPEC-003-executor.md:233-238`
  - defines `ReducerContext` with `DB *Transaction` and `Scheduler SchedulerHandle`
- `docs/decomposition/003-executor/epic-1-core-types/story-1.2-reducer-types.md:56-70`
  - acceptance criteria explicitly require `ReducerContext` to reference `Transaction` and `SchedulerHandle`
- `docs/decomposition/003-executor/epic-4-reducer-execution/story-4.1-begin-phase.md:27-35`
  - begin phase constructs a typed `ReducerContext` with `DB: tx` and `Scheduler: ...`
- `docs/decomposition/003-executor/epic-4-reducer-execution/story-4.2-execute-phase.md:47-62`
  - execution docs and guardrails are written against the typed reducer runtime surface

Current observed behavior:
- Focused executor contract/runtime tests now pass:
  - `rtk go test ./executor -run 'TestReducerContractsMatchPhase1dSpec|TestPhase4HandleCallReducerBeginExecuteCommitRollback|TestSchedulerHandleCommitPersistsRow|TestSchedulerHandleRollbackDiscardsSchedule' -count=1`
  - `rtk go test ./executor`
- Full repo verification now passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

Recommended resolution options:
1. Preferred code fix:
   - move the canonical `ReducerContext` ownership back to the executor surface described by SPEC-003, with typed `DB` and `Scheduler` fields
   - if package cycles are the blocker, introduce a narrow shared interface/type owner rather than keeping the public reducer API erased as `any`
   - add compile-time contract tests proving reducer authors can call the documented `DB` / `Scheduler` methods directly
2. Alternative doc fix:
   - if the project intentionally wants an erased `any`-based reducer context, update SPEC-003 and downstream schema docs to describe that explicit type-assertion requirement
   - this would be a meaningful weakening of the current runtime contract and likely not the intended end state

Suggested follow-up tests:
- compile-time reducer contract test that direct `ctx.DB` transaction methods compile
- compile-time reducer contract test that direct `ctx.Scheduler` methods compile
- regression test ensuring typed adapter / reducer registration surfaces keep using the same canonical `ReducerContext` owner

### TD-021: SPEC-003 E3 subscription-command dispatch path is still missing

Status: resolved
Severity: high
First found: SPEC-003 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4d (`SPEC-003 E3: Executor Core`)

Summary:
- Executor-core dispatch now routes subscription register, unregister, and disconnect commands through the expected Story 3.4 handlers.
- `SubscriptionManager` now exposes the registration/unregistration surface required by the executor boundary.
- Focused regression tests now prove committed snapshots are acquired and closed around registration, and that unregister/disconnect delegation returns via the command response channels.

Why this matters:
- The executor now owns the atomic registration-sensitive read boundary that SPEC-003 describes.
- Epic 3 no longer overstates completeness while silently omitting subscription command routing.
- Snapshot-close guarantees are now exercised by tests instead of being left implicit.

Related code:
- `executor/interfaces.go`
  - adds `Register(...)` and `Unregister(...)` to `SubscriptionManager`
- `executor/executor.go`
  - extends `dispatch(...)` with register/unregister/disconnect cases
  - implements `handleRegisterSubscription`, `handleUnregisterSubscription`, and `handleDisconnectClientSubscriptions`
- `executor/subscription_dispatch_test.go`
  - adds focused routing/snapshot-close regression coverage

Current observed behavior:
- Focused dispatch tests pass:
  - `rtk go test ./executor -run 'TestRegisterSubscriptionDispatchUsesSnapshotAndClosesIt|TestRegisterSubscriptionDispatchClosesSnapshotOnError|TestUnregisterAndDisconnectSubscriptionDispatchDelegate' -count=1`
- Full package and repo verification pass:
  - `rtk go test ./executor`
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

### TD-020: SPEC-003 E3 `SubmitWithContext` ignores reject-on-full policy and returns context timeout instead of `ErrExecutorBusy`

Status: resolved
Severity: medium
First found: SPEC-003 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4d (`SPEC-003 E3: Executor Core`)

Summary:
- `Submit(...)` honors `rejectMode` and returns `ErrExecutorBusy` on a full inbox when configured to reject.
- `SubmitWithContext(...)` now mirrors that reject-on-full behavior before falling back to the blocking context-aware path when reject mode is disabled.
- A focused regression test now guards the reject-mode `SubmitWithContext(...)` contract.

Why this matters:
- Story 3.3 says `SubmitWithContext` is "same as Submit" plus caller-context cancellation while waiting.
- Under reject-on-full mode, callers should get the same immediate backpressure signal (`ErrExecutorBusy`) rather than being forced into timeout-based detection.
- This is a concrete behavioral contract gap in the public submission API, not just missing test coverage.

Related code:
- `executor/executor.go:121-138`
  - `Submit(...)` checks `e.rejectMode` and returns `ErrExecutorBusy` on a full inbox
- `executor/executor.go:140-153`
  - `SubmitWithContext(...)` does not check `e.rejectMode`; it only selects between inbox send and `ctx.Done()`
- repo-wide tests do not include a `SubmitWithContext` reject-on-full case

Related spec / decomposition docs:
- `docs/decomposition/003-executor/epic-3-executor-core/story-3.3-submit-methods.md:17-31`
  - `SubmitWithContext` is documented as the same policy as `Submit`, but with context cancellation support while waiting
- `docs/decomposition/003-executor/epic-3-executor-core/story-3.3-submit-methods.md:35-40`
  - acceptance criteria include full-inbox reject behavior and context-cancel-while-blocking behavior
- `docs/decomposition/003-executor/EPICS.md:71-78`
  - Epic 3 backpressure contract includes both blocking and `ErrExecutorBusy` reject modes
- `docs/decomposition/003-executor/SPEC-003-executor.md:86-88,655-656`
  - bounded inbox may block or return `ErrExecutorBusy`; shutdown/busy semantics are part of the core error surface

Current observed behavior:
- Existing package tests still pass:
  - `rtk go test ./executor`
- Targeted runtime repro showed the mismatch directly:
  - `rtk go run /tmp/executor_e3_submitctx_repro.go`
  - executor configured with `rejectMode=true` and a full inbox
  - observed output: `err=context deadline exceeded elapsed_ms=50`
  - expected per Story 3.3: immediate `ErrExecutorBusy`

Recommended resolution options:
1. Preferred code fix:
   - make `SubmitWithContext(...)` honor `rejectMode` the same way `Submit(...)` does
   - in reject mode, return `ErrExecutorBusy` immediately on full inbox
   - in blocking mode, keep the current context-aware wait semantics
2. Alternative doc fix:
   - if the intended behavior is for `SubmitWithContext` to always block-until-context regardless of reject mode, update Story 3.3 to describe that divergence explicitly
   - this seems less desirable because it makes the two submission APIs inconsistent under one executor configuration

Suggested follow-up tests:
- `SubmitWithContext` on full inbox with `rejectMode=true` returns `ErrExecutorBusy` immediately
- `SubmitWithContext` on full inbox with `rejectMode=false` blocks until either space opens or context is cancelled
- `Submit` and `SubmitWithContext` match on shutdown/fatal handling under the same executor state

### TD-019: SPEC-003 E2 frozen reducer registry remains externally mutable through `Lookup` and `All`

Status: resolved
Severity: high
First found: SPEC-003 Epic 2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4c (`SPEC-003 E2: Reducer Registry`)

Summary:
- The registry now returns detached `RegisteredReducer` copies from `Lookup(...)`, `LookupLifecycle(...)`, and `All()`.
- Post-freeze callers can no longer mutate the registry's internal reducer metadata through returned pointers.
- Focused regression coverage now proves lookup/all mutations do not affect later reducer or lifecycle lookups.

Why this matters:
- Story 2.2 and SPEC-003 treat freeze as the point where registration becomes immutable and concurrent reads become safe because the registry no longer changes.
- With the current pointer aliasing, post-start callers can still rewrite reducer metadata without going through `Register(...)`, so the freeze guarantee is only partial.
- This is not just a cosmetic encapsulation issue: downstream lifecycle lookup behavior can change after startup if a caller mutates a returned reducer's `Lifecycle` field.

Related code:
- `executor/registry.go:6-9`
  - registry stores `map[string]*RegisteredReducer`
- `executor/registry.go:49`
  - `Register(...)` stores the address of the local reducer struct directly in the map
- `executor/registry.go:53-57`
  - `Lookup(...)` returns the live internal pointer
- `executor/registry.go:60-67`
  - `LookupLifecycle(...)` returns the live internal pointer found during map iteration
- `executor/registry.go:69-76`
  - `All()` returns a slice of the same live internal pointers
- `executor/phase4_acceptance_test.go:43-78` and `executor/executor_test.go:113-155`
  - tests cover duplicate names, lifecycle name rules, and freeze rejection, but do not verify immutability of returned registry entries

Related spec / decomposition docs:
- `docs/decomposition/003-executor/SPEC-003-executor.md:184-187`
  - registration rules include uniqueness, reserved lifecycle names, and immutability after executor start
- `docs/decomposition/003-executor/EPICS.md:41-43`
  - Epic 2 explicitly includes `Freeze()` to make the registry immutable after startup
- `docs/decomposition/003-executor/epic-2-reducer-registry/story-2.1-registry.md:30-47`
  - `Lookup`/`All` are the public read APIs, and the design notes say lookup is safe for concurrent reads after freeze because the map is immutable
- `docs/decomposition/003-executor/epic-2-reducer-registry/story-2.2-lifecycle-validation.md:12-31`
  - freeze is defined as registry immutability after startup

Current observed behavior:
- Regression and package tests now pass:
  - `rtk go test ./executor -run 'TestReducerRegistry(FrozenLookupsReturnDetachedCopies|Basics|Lifecycle)|TestPhase4ReducerRegistryRules' -count=1`
  - `rtk go test ./executor`
- Full repo verification now passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

Recommended resolution options:
1. Preferred code fix:
   - store reducers by value internally or clone them on both insert and readout
   - make `Lookup`, `LookupLifecycle`, and `All` return detached copies so callers cannot mutate frozen internal state
   - add regression tests proving post-freeze mutations of returned values do not affect later lookups
2. Alternative contract change:
   - if pointer-returning APIs are intentional, update SPEC-003 Epic 2 docs to drop the stronger immutability/concurrent-read claim and describe the registry as internally mutable through returned handles
   - this would be a significant weakening of the current contract and likely not the desired direction

Suggested follow-up tests:
- mutate the reducer returned by `Lookup(...)` after `Freeze()` and assert a fresh lookup still returns the original metadata
- mutate the first entry returned by `All()` after `Freeze()` and assert `LookupLifecycle(...)` is unchanged
- verify `NewExecutor(...)` plus lifecycle dispatch continue to observe the originally registered lifecycle reducers even if caller-held copies are mutated

### TD-018: SPEC-002 E2 `SegmentWriter` does not enforce segment startTx alignment

Status: resolved
Severity: high
First found: SPEC-002 Epic 2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4b (`SPEC-002 E2: Record format & segment I/O`)

Summary:
- `SegmentWriter.Append(...)` now enforces that the first record written to a fresh segment has `tx_id == startTx`.
- Later appends still use the existing strict `tx_id > lastTx` monotonicity rule.
- Focused regression coverage now proves misaligned first appends fail while aligned first appends remain readable with matching segment/file metadata.

Why this matters:
- Story 2.3 defines `startTx` as "first TX ID in this segment," and later recovery/compaction stories rely on filename-derived start TX metadata as real history boundaries.
- If the first record in `00000000000000000100.log` can actually be tx 1, recovery-side ordering and coverage logic can be misled before Epic 6/7 code even runs.
- This is a durability correctness contract gap, not just missing polish around writer validation.

Related code:
- `commitlog/segment.go:163-170`
  - `SegmentWriter` stores both `startTx` and `lastTx`
- `commitlog/segment.go:173-189`
  - `CreateSegment(...)` names the file from `startTxID` and stores `startTx`
- `commitlog/segment.go:192-201`
  - `Append(...)` validates only strict monotonic increase relative to `lastTx`; it never checks first-record equality against `startTx`
- `commitlog/phase4_acceptance_test.go:152-222`
  - reader/writer tests cover EOF/truncation/corruption but do not assert first-record alignment with segment start metadata

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.3-segment-writer.md:18-35`
  - defines `startTx` as the first TX ID in the segment and requires `CreateSegment(dir, startTxID)` plus monotonic append validation
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.4-segment-reader.md:16-27`
  - reader `startTx` is defined as coming from the filename
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:39-53`
  - segment filenames are part of the on-disk ordering model
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md:428-429`
  - recovery begins by validating segment start TX IDs from filenames in sorted order

Current observed behavior:
- Focused regression and package tests now pass:
  - `rtk go test ./commitlog -run TestSegmentWriterEnforcesStartTxAlignment -count=1`
  - `rtk go test ./commitlog`
- Full repo verification now passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

Recommended resolution options:
1. Preferred code fix:
   - teach `SegmentWriter.Append(...)` to require `rec.TxID == sw.startTx` when `sw.lastTx == 0`
   - keep the existing strict-increase check for subsequent appends
   - add regression tests covering both aligned and misaligned first-appends
2. Alternative defensive fix:
   - if the writer should remain low-level, add a dedicated constructor or seal-time validation that ensures the first record actually matches the declared segment start before any reader/recovery path can consume it
   - if this route is chosen, document clearly which layer owns that invariant

Suggested follow-up tests:
- first append with `tx_id != startTx` returns an error
- first append with `tx_id == startTx` succeeds and reads back with matching `StartTxID()` / first record tx
- rotated segments opened by the durability worker start at exactly `previousLastTx + 1`

### TD-017: SPEC-002 E2 exported error/reader API does not match the documented surface

Status: resolved
Severity: medium
First found: SPEC-002 Epic 2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4b (`SPEC-002 E2: Record format & segment I/O`)

Summary:
- The documented typed error names are now exposed at the public surface via compatible exported aliases: `ErrBadVersion`, `ErrUnknownRecordType`, `ErrChecksumMismatch`, and `ErrRecordTooLarge`.
- `SegmentReader` now exposes the documented no-argument `Next() (*Record, error)` API.
- The public `Next()` path still enforces `DefaultCommitLogOptions().MaxRecordPayloadBytes`, and focused regression coverage now guards both the compile-time API surface and runtime max-payload behavior.

Why this matters:
- This is a public API contract gap: consumers written against the documented commitlog surface do not compile.
- The mismatch is broader than naming style; the spec/decomposition treats these names and signatures as shared contracts consumed by later recovery work.
- Package tests still pass because they use the implementation's actual names/signature rather than guarding the documented boundary.

Related code:
- `commitlog/errors.go:19-48`
  - exports `BadVersionError`, `UnknownRecordTypeError`, `ChecksumMismatchError`, and `RecordTooLargeError`
- `commitlog/segment.go:52-68`
  - `ReadSegmentHeader(...)` returns `*BadVersionError`
- `commitlog/segment.go:106-155`
  - `DecodeRecord(...)` returns `*ChecksumMismatchError`, `*UnknownRecordTypeError`, and `*RecordTooLargeError`
- `commitlog/segment.go:249-259`
  - `SegmentReader.Next(maxPayload uint32)` requires a max-payload argument not present in the documented API
- `commitlog/commitlog_test.go:54-67` and `commitlog/phase4_acceptance_test.go:59-150`
  - tests exercise the live names/signature only

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.1-segment-header.md:26-39`
  - bad version is documented as `ErrBadVersion`
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.2-record-framing.md:41-58`
  - names `ErrUnknownRecordType`, `ErrChecksumMismatch`, and `ErrRecordTooLarge`
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.4-segment-reader.md:25-42`
  - documents `OpenSegment(...)` plus `func (sr *SegmentReader) Next() (*Record, error)`
- `docs/decomposition/002-commitlog/epic-2-record-format-segment-io/story-2.5-segment-error-types.md:16-29`
  - assigns the typed-error field sets to `ErrBadVersion`, `ErrUnknownRecordType`, `ErrChecksumMismatch`, and `ErrRecordTooLarge`

Current observed behavior:
- Focused API-contract and package tests now pass:
  - `rtk go test ./commitlog -run 'TestCommitlogPublicAPIContractCompiles|TestSegmentReaderNextUsesDefaultMaxPayload' -count=1`
  - `rtk go test ./commitlog`
- Full repo verification now passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

Recommended resolution options:
1. Preferred code fix:
   - expose the documented typed errors at the advertised names (either by renaming the structs or exporting compatible aliases/wrappers)
   - add a no-argument `SegmentReader.Next()` entrypoint that enforces the configured/default max payload internally, keeping `Next(maxPayload)` private or as a lower-level helper if needed
   - add compile-time contract coverage so future drift is caught
2. Alternative doc fix:
   - if the repo intentionally prefers `BadVersionError`-style names and an explicit `Next(maxPayload)` API, update the Epic 2 decomposition/spec docs to describe that surface consistently before later recovery stories treat the current docs as canonical

Suggested follow-up tests:
- compile-time API test for the documented error type names
- compile-time API test for `SegmentReader.Next()` with no arguments
- runtime test proving whichever public `Next` surface remains still enforces `MaxRecordPayloadBytes` without caller footguns

### TD-001: Invalid-float error contract drift across `types` and `store`

Status: resolved
Severity: medium
First found: SPEC-001 Epic 1 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 1 / Step 1a (`SPEC-001 E1: Core Value Types`)

Summary:
- The canonical `ErrInvalidFloat` sentinel now lives in `types`, where float-value construction actually happens.
- `types.NewFloat32` and `types.NewFloat64` now wrap that sentinel on NaN rejection, so callers can reliably use `errors.Is(err, ErrInvalidFloat)`.
- `store.ErrInvalidFloat` now aliases the same sentinel, keeping store-side validation aligned with the constructor path instead of defining a competing error value.

Why this matters:
- The invalid-float error contract is now owned by the same package that rejects invalid float values.
- Downstream paths like BSATN decode and store validation can share one error classification.
- Focused regression coverage now guards the constructor-side `errors.Is` contract.

Related code:
- `types/value.go`
  - defines canonical `ErrInvalidFloat`
  - `NewFloat32` / `NewFloat64` wrap that sentinel on NaN rejection
- `store/errors.go`
  - `ErrInvalidFloat` now aliases `types.ErrInvalidFloat`
- `types/value_test.go`
  - asserts `errors.Is(err, ErrInvalidFloat)` for both NaN constructor paths

Current observed behavior:
- Focused regression coverage passes:
  - `rtk go test ./types ./schema ./executor`
- Full repo verification passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

### TD-002: SPEC-003 Epic 1 command/interface/error surface is only partially defined

Status: resolved
Severity: medium
First found: Phase 1 planning pass while moving from schema foundations toward the executor contract slice
Execution-order context:
- `docs/EXECUTION-ORDER.md:157-160` explicitly allows a narrowed executor contract slice in Phase 1: `SPEC-003 E1.1 + E1.2 + minimal E1.4 contract slice`
- This debt item tracked the fuller `SPEC-003 Epic 1` decomposition surface beyond that minimum early-gate slice

Summary:
- The executor package now exposes the missing Epic 1 command-shell and error contracts that were still absent from the documented surface.
- `RegisterSubscriptionCmd`, `UnregisterSubscriptionCmd`, and `DisconnectClientSubscriptionsCmd` now exist at the executor boundary.
- `ErrCommitFailed` is now defined alongside the other executor sentinels, and focused contract tests guard the complete Epic 1 command/interface/error surface.

Why this matters:
- Later phases can now reference the intended executor-owned command catalog without inventing ad-hoc local shells.
- The executor error catalog now matches Story 1.5's seven-sentinel contract.
- The existing `DurabilityHandle` and `SubscriptionManager` interfaces are now covered by explicit contract tests instead of being left implicitly present.

Related code:
- `executor/command.go`
  - adds `SubscriptionRegisterRequest` / `SubscriptionRegisterResult` aliases to the canonical subscription types
  - adds `RegisterSubscriptionCmd`, `UnregisterSubscriptionCmd`, and `DisconnectClientSubscriptionsCmd`
- `executor/errors.go`
  - adds `ErrCommitFailed`
- `executor/contracts_test.go`
  - adds compile-time/shape coverage for the full Epic 1 command/interface/error surface

Current observed behavior:
- Focused regression coverage passes:
  - `rtk go test ./types ./schema ./executor`
- Full repo verification passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

### TD-003: `ErrSequenceOverflow` is specified but not defined anywhere in live code

Status: resolved
Severity: medium
First found: SPEC-006 Epic 1 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 1 / Step 1c (`SPEC-006 E1: Schema Types & Type Mapping`)

Summary:
- The schema/type-mapping slice correctly provides `AutoIncrementBounds(...)`, and the canonical schema-owned `ErrSequenceOverflow` sentinel now exists alongside that bounds contract.
- This restores the documented schema-side overflow error surface that later auto-increment runtime enforcement can wrap with `errors.Is(..., ErrSequenceOverflow)`.
- Focused regression coverage now guards that `ErrSequenceOverflow` is present as a usable sentinel.

Why this matters:
- The auto-increment bounds contract is only half surfaced today: callers can ask what the bounds are, but there is no canonical sentinel for overflow.
- This creates ambiguity about which package owns overflow classification and prevents future `errors.Is(..., ErrSequenceOverflow)` checks from being standardized.
- The missing sentinel weakens the shared boundary between schema validation metadata and store/runtime auto-increment behavior.

Related code:
- `schema/valuekind_export.go:31-55`
  - implements `AutoIncrementBounds(k ValueKind) (min int64, max uint64, ok bool)`
- `schema/validate_structure.go:62`
  - uses `AutoIncrementBounds` only to validate whether a type is integer-eligible
- `schema/errors.go:5-17`
  - defines several schema validation errors, but no `ErrSequenceOverflow`
- `store/sequence.go:5-37`
  - sequence implementation exists, but no overflow error contract is defined there either
- Repository-wide search for `ErrSequenceOverflow` returned no Go-code matches

Related spec / decomposition docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md:68`
  - says inserts fail with `ErrSequenceOverflow` when auto-increment exceeds the type range
- `docs/decomposition/006-schema/SPEC-006-schema.md:579`
  - error catalog lists `ErrSequenceOverflow`
- `docs/decomposition/006-schema/EPICS.md:19`
  - Epic 1 scope includes auto-increment numeric bounds metadata used to enforce `ErrSequenceOverflow`
- `docs/decomposition/006-schema/EPICS.md:234`
  - error table assigns `ErrSequenceOverflow` to Epic 1
- `docs/decomposition/006-schema/epic-1-schema-types/story-1.4-valuekind-export-bounds.md:20-37`
  - explicitly ties `AutoIncrementBounds` to the `ErrSequenceOverflow` contract

Current observed behavior:
- `AutoIncrementBounds` exists and is well-tested
- no canonical overflow sentinel exists yet in `schema`, `store`, or any shared package
- this is a spec-contract gap, not a current test failure

Recommended resolution options:
1. Preferred code fix:
   - define `ErrSequenceOverflow` in the canonical owning package for this contract
   - use that sentinel from the eventual store-side auto-increment enforcement path
   - add tests asserting overflow failures wrap the canonical sentinel
2. Alternative doc fix:
   - if ownership should belong to SPEC-001/store rather than SPEC-006/schema, update the SPEC-006 spec/decomposition error ownership text so the bounds contract remains in schema but the runtime error ownership moves explicitly to store

Suggested follow-up tests:
- store-side sequence overflow tests for every integer `ValueKind`
- `errors.Is` coverage for the chosen canonical `ErrSequenceOverflow` sentinel
- cross-package test proving the auto-increment runtime path and schema bounds metadata agree on overflow behavior

### TD-016: SPEC-002 E1 `DecodeProductValue` does not reject extra encoded columns

Status: resolved
Severity: medium
First found: SPEC-002 Epic 1 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4a (`SPEC-002 E1: BSATN codec`)

Summary:
- `DecodeProductValue(r, schema)` now treats extra encoded columns as a row-shape mismatch instead of silently succeeding.
- A focused regression test now covers the schema-N / encoded-(N+1) case directly.
- The existing `DecodeProductValueFromBytes(...)` tests were tightened to assert the documented `ErrRowLengthMismatch` behavior for trailing bytes.

Why this matters:
- This is a contract gap in the row decoder itself, not just a missing convenience wrapper.
- Callers that use `DecodeProductValue` directly on a framed stream can incorrectly accept malformed rows that contain extra encoded values, leaving trailing data to confuse higher layers.
- The spec/decomposition explicitly distinguishes both "too few" and "too many" values as row-shape failures.

Related code:
- `bsatn/decode.go:121-135`
  - `DecodeProductValue(...)` decodes `len(ts.Columns)` values and returns success immediately
- `bsatn/decode.go:137-151`
  - only `DecodeProductValueFromBytes(...)` checks for trailing bytes after row decode
- `bsatn/phase4_acceptance_test.go:145-178`
  - tests cover short rows and trailing bytes in `DecodeProductValueFromBytes`, but not extra-column acceptance in `DecodeProductValue`

Related spec / decomposition docs:
- `docs/decomposition/002-commitlog/epic-1-bsatn-codec/story-1.3-product-value-codec.md:20-29`
  - requires fewer OR more values than schema to be treated as row-shape/length mismatch
- `docs/decomposition/002-commitlog/epic-1-bsatn-codec/story-1.3-product-value-codec.md:34-39`
  - acceptance criteria explicitly include schema expects 3 columns but encoded row has 4 → error
- `docs/decomposition/002-commitlog/epic-1-bsatn-codec/story-1.4-bsatn-error-types.md:18-21`
  - row-shape and row-length error taxonomy for malformed rows

Current observed behavior:
- Targeted regression test now passes:
  - `rtk go test ./bsatn -run TestDecodeProductValueShapeMismatchAndFromBytesLengthMismatch -count=1`
- Broader package verification passes:
  - `rtk go test ./bsatn`
- Repo-wide verification passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

Recommended resolution options:
1. Preferred code fix:
   - make the row-level decoding path reject extra encoded values when the caller expects a full row payload
   - one approach: keep `DecodeProductValueFromBytes` as the strict entrypoint and tighten docs/callers so direct `DecodeProductValue` is only used with an exact row-limited reader
   - alternatively, change `DecodeProductValue` semantics or add a strict variant and update callers/tests accordingly
2. Minimum test fix required either way:
   - add a regression test covering a schema with N columns and encoded data for N+1 values
   - assert the chosen strict API returns row-shape/length failure rather than silently succeeding

Suggested follow-up tests:
- schema expects 2 columns, encoded stream has 3 values → strict row decode fails
- short row still fails with the correct shape/length classification
- framed row decode paths in later commitlog stories use the strict variant so extra trailing columns cannot slip through

### TD-015: SPEC-001 E8 auto-increment sequence is not integrated into table/transaction inserts

Status: resolved
Severity: high
First found: SPEC-001 Epic 8 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3g (`SPEC-001 E8: Auto-Increment & Recovery`)

Summary:
- `schema.ColumnSchema` now preserves autoincrement metadata into the built registry so the store can detect autoincrement columns at runtime.
- `store.Table` now initializes optional per-table sequence state (`sequence`, `sequenceCol`) from schema metadata and exposes `SequenceValue` / `SetSequenceValue` for recovery.
- `Transaction.Insert(...)` now rewrites zero-valued autoincrement columns before constraint checks, while preserving explicit non-zero values.

Why this matters:
- Story 8.1 is not just a utility-type story; it requires observable insert-time behavior for autoincrement columns.
- Without that integration, schemas that declare `AutoIncrement` currently validate but do not behave per spec.
- This also leaves Story 8.3 incomplete in practice because there is no per-table sequence state to export or restore.

Related code:
- `store/sequence.go:5-37`
  - standalone `Sequence` exists and works in isolation
- `store/table.go:10-17`
  - `Table` has no `sequence` or `sequenceCol` fields
- `store/transaction.go:31-85`
  - `Insert(...)` validates, checks constraints, allocates RowID, and stores the row as-is; it never rewrites a zero-valued autoincrement column
- `store/recovery.go:67-72`
  - `TableExportState` mentions `SequenceValue`, but there is no live table sequence field backing it

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.1-sequence.md:32-48`
  - requires `Table` sequence integration and zero-means-auto-assign behavior in `Transaction.Insert`
- `docs/decomposition/001-store/epic-8-auto-increment-recovery/story-8.3-state-export.md:23-29,37-42`
  - expects per-table sequence state accessors/restore hooks
- `docs/decomposition/001-store/SPEC-001-store.md:628-645`
  - store spec defines autoincrement behavior and overflow expectations tied to inserted rows, not just a helper object

Current observed behavior:
- Targeted RED test failed before the fix because the recovery accessors were absent:
  - `rtk go test ./store -run 'TestTransactionInsertAutoIncrementAssignsSequentialValues|TestTransactionInsertAutoIncrementPreservesExplicitValue|TestTableSequenceStateAccessorsRoundTrip' -count=1`
  - observed compile errors included:
    - `tbl.SequenceValue undefined`
    - `tbl.SetSequenceValue undefined`
- Targeted store verification now passes:
  - `rtk go test ./store -run 'TestTransactionInsertAutoIncrementAssignsSequentialValues|TestTransactionInsertAutoIncrementPreservesExplicitValue|TestTableSequenceStateAccessorsRoundTrip' -count=1`
  - `rtk go test ./store`
- Broader verification now passes:
  - `rtk go test ./schema ./store`
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

Recommended resolution options:
1. Preferred code fix:
   - add optional sequence state to `Table` (`sequence`, `sequenceCol`)
   - initialize it from schema autoincrement metadata in `NewTable`
   - in `Transaction.Insert`, rewrite zero values in the sequence column before constraint checks
   - add `SequenceValue` / `SetSequenceValue` accessors to complete Story 8.3
2. If deferring the full feature intentionally:
   - document Story 8.1 / 8.3 as incomplete in planning/decomposition notes so autoincrement columns are not mistaken as working end-to-end today

Suggested follow-up tests:
- zero-valued insert into autoincrement column produces 1, then 2, then 3
- non-zero autoincrement column value is preserved as explicit caller choice
- export/restore of sequence state round-trips correctly once sequence integration exists
- overflow classification uses the canonical `ErrSequenceOverflow` contract once that separate gap is resolved

### TD-014: SPEC-001 E7 `CommittedReadView` is missing the documented `IndexScan` and Bound-based `IndexRange` API

Status: resolved
Severity: medium
First found: SPEC-001 Epic 7 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3f (`SPEC-001 E7: Read-Only Snapshots`)

Summary:
- `CommittedReadView` now exposes the documented snapshot-side `IndexScan(tableID, indexID, value)` and Bound-based `IndexRange(tableID, indexID, lower, upper)` methods.
- `CommittedSnapshot` now resolves matching RowIDs to `(RowID, ProductValue)` pairs for both point scans and range scans, with Bound filtering applied in key order.
- Legacy helper methods (`IndexSeek`, `GetRow`) were kept alongside the new surface so this API-alignment fix stays narrow and does not force unrelated subscription/protocol refactors in the same slice.

Why this matters:
- This is a public API contract gap, not just an internal refactor. Consumers written to the documented snapshot interface do not compile.
- The missing `IndexScan` convenience and Bound-based range endpoints are part of how the spec expects callers (especially subscription-side consumers) to perform committed reads.
- The mismatch is amplified by the earlier Epic 3 gap: the `Bound` helper surface is also absent, so the documented range-read API cannot be expressed at all.

Related code:
- `store/snapshot.go:12-18`
  - live `CommittedReadView` exposes `IndexSeek` and `IndexRange(... *IndexKey)`
- `store/snapshot.go:41-63`
  - implementation provides exact-key lookup by `IndexKey` and range lookup by `*IndexKey`
- repo-wide search found no snapshot-side `IndexScan` method
- repo-wide search found no snapshot-side Bound-based overload/variant

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-7-read-only-snapshots/story-7.1-committed-read-view.md:16-24`
  - requires `IndexScan(tableID, indexID, value Value)` and `IndexRange(... Bound, Bound)` on `CommittedReadView`
- `docs/decomposition/001-store/epic-7-read-only-snapshots/story-7.1-committed-read-view.md:48-54`
  - documents point lookup as row-resolving index scan and range scan as Bound-derived traversal
- `docs/decomposition/001-store/epic-7-read-only-snapshots/story-7.1-committed-read-view.md:63-68`
  - acceptance criteria explicitly cover `IndexScan` and Bound/unbounded range behavior

Current observed behavior:
- Targeted RED build/test failed before the fix because the snapshot API surface was missing:
  - `rtk go test ./store -run 'TestCommittedSnapshotIndexScanByPrimaryKey|TestCommittedSnapshotIndexScanMissingValueReturnsEmpty|TestCommittedSnapshotIndexRangeBoundSemantics' -count=1`
  - observed errors included:
    - `snap.IndexScan undefined`
    - `cannot use Inclusive(...) as *IndexKey`
    - `cannot use UnboundedLow() as *IndexKey`
- Targeted verification now passes:
  - `rtk go test ./store -run 'TestCommittedSnapshotIndexScanByPrimaryKey|TestCommittedSnapshotIndexScanMissingValueReturnsEmpty|TestCommittedSnapshotIndexRangeBoundSemantics' -count=1`
  - `rtk go test ./store`
  - `rtk go test ./subscription`
  - `rtk go test ./protocol`
- Repo-wide verification now passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

Recommended resolution options:
1. Preferred code fix:
   - extend `CommittedReadView` with the documented `IndexScan` and Bound-based `IndexRange` API
   - implement those methods on `CommittedSnapshot`, resolving RowIDs to rows as the spec describes
   - keep `IndexSeek`/`*IndexKey` helpers internally if useful, but expose the documented public contract too
2. Alternative doc fix:
   - if the project intentionally prefers raw `IndexKey`-based snapshot APIs, update Story 7.1 and nearby docs to reflect that simplification explicitly
   - this would also need coordinated cleanup of the remaining Bound references

Suggested follow-up tests:
- compile-time/public API test for `CommittedReadView.IndexScan` and Bound-based `IndexRange`
- snapshot tests covering PK/non-existent point scans via `IndexScan`
- snapshot range tests covering unbounded/inclusive/exclusive semantics once Bound exists

### TD-013: SPEC-001 E6 rollback does not make transactions unusable

Status: resolved
Severity: high
First found: SPEC-001 Epic 6 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3e (`SPEC-001 E6: Commit, Rollback & Changeset`)

Summary:
- `Rollback(tx)` currently only sets `tx.rolledBack = true`.
- No transaction operation checks that flag, and rollback does not clear or detach `TxState`.
- As a result, a rolled-back transaction can still accept new mutations and can still be committed successfully, directly violating Story 6.4.

Why this matters:
- Story 6.4 requires rollback to discard the transaction and make subsequent `Insert` / `Delete` / `Update` / `Commit` panic or return error.
- The current behavior is not just incomplete cleanup; it enables silent reuse of an invalid transaction object, which can produce committed mutations after callers think the transaction was discarded.
- This is a correctness bug in the transaction lifecycle contract, not just missing defensive polish.

Related code:
- `store/commit.go:55-58`
  - `Rollback(tx)` only sets `tx.rolledBack = true`
- `store/transaction.go:31-247`
  - `Insert`, `Delete`, `Update`, and read paths do not check `rolledBack`
- `store/commit.go:12-52`
  - `Commit(...)` also does not check `rolledBack`

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.4-rollback.md:16-21`
  - rollback should clear/discard tx state and make the transaction unusable afterward
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.4-rollback.md:23-30`
  - acceptance criteria require no committed-state effect and require using a transaction after rollback to panic or return error
- `docs/decomposition/001-store/epic-6-commit-rollback-changeset/story-6.2-commit.md:41`
  - atomicity/design intent assumes discarded transactions are not later reused to mutate committed state

Current observed behavior:
- Existing targeted commit tests still pass:
  - `rtk go test ./store -run 'TestCommitApplies|TestCommitNetEffectInsertDelete|TestCommitProducesLocalChangesetsAcrossTransactions|TestApplyChangeset|TestTransactionUpdate'`
- Targeted runtime repro from the audit showed rollback reuse is live:
  - create tx
  - insert row
  - call `Rollback(tx)`
  - call `tx.Insert(...)` again
  - call `Commit(cs, tx)`
- observed output:
  - `post-rollback insert rowID=2 err=<nil>`
  - `post-rollback commit err=<nil> empty=false`
  - `committed rows after reuse=2`

Recommended resolution options:
1. Preferred code fix:
   - make rollback discard tx-local state (`tx.tx = nil` or equivalent cleared sentinel state)
   - add a reusable guard checked by `Insert`, `Delete`, `Update`, `GetRow`, `ScanTable`, and `Commit` so a rolled-back transaction panics or returns a deterministic error
   - add targeted regression tests for post-rollback method calls
2. Alternative implementation shape:
   - if panic is undesirable, return a stable sentinel error like `ErrTransactionClosed` from all post-rollback operations
   - whichever route is chosen, it must be consistent and tested

Suggested follow-up tests:
- rollback after inserts/deletes leaves committed state unchanged
- post-rollback `Insert`, `Delete`, `Update`, and `Commit` each fail deterministically
- provisional RowIDs consumed before rollback are not reused by subsequent fresh transactions

### TD-012: SPEC-001 E5 Story 5.3 `StateView` API is entirely missing

Status: resolved
Severity: medium
First found: SPEC-001 Epic 5 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3d (`SPEC-001 E5: Transaction Layer`)

Summary:
- The store package now exposes the documented `StateView` abstraction and `NewStateView(...)` constructor.
- `StateView` implements `GetRow`, `ScanTable`, `SeekIndex`, and `SeekIndexRange` across committed and tx-local state, including delete filtering and tx-local insert inclusion.
- `Transaction.GetRow` and `Transaction.ScanTable` now route through `StateView`, restoring the intended layering instead of leaving visibility logic fully inlined.

Why this matters:
- The transaction layer now has the reusable visibility seam Story 5.3 describes.
- Consumers can use the documented API surface directly instead of depending on ad-hoc transaction internals.
- Index-sensitive tx-local visibility behavior is now covered by focused regression tests.

Related code:
- `store/state_view.go`
  - adds `RowIterator`, `StateView`, `NewStateView`, `GetRow`, `ScanTable`, `SeekIndex`, and `SeekIndexRange`
- `store/transaction.go`
  - reuses `StateView` for row/table visibility reads
- `store/state_view_test.go`
  - adds focused behavior tests for row visibility, merged scans, index queries, and nil-map handling

Current observed behavior:
- Focused package tests pass:
  - `rtk go test ./store`
- Full repo verification passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

### TD-011: SPEC-001 E4 Story 4.1's documented `Index.unique` field appears to be stale doc drift

Status: resolved
Severity: low
First found: SPEC-001 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3c (`SPEC-001 E4: Table Indexes & Constraints`)

Summary:
- The live implementation of table indexes and constraints appears operationally correct: index wrappers are created, synchronous maintenance works, PK/unique/set-semantics enforcement is present, and targeted tests pass.
- The stale docs have been updated so `Index` is described as wrapping `IndexSchema` + `BTreeIndex`, with uniqueness and primary-ness derived from `schema.Unique` / `schema.Primary` instead of a redundant cached field.
- Story 4.1 and the nearby SPEC text now match the simpler live implementation shape.

Why this is classified as doc drift, not a product bug:
- The behavior the field was meant to support is present.
- Using `IndexSchema` as the single source of truth is simpler and avoids redundant state that could drift.
- I found no acceptance-criteria failure caused by the missing field; the mismatch is in the documented struct shape, not the operational constraint behavior.

Related code:
- `store/index.go:8-19`
  - `Index` contains only `schema *schema.IndexSchema` and `btree *BTreeIndex`
  - `NewIndex(...)` does not copy `schema.Unique` into a separate field
- `store/table.go:155-175`
  - uniqueness / PK enforcement reads `idx.schema.Unique` and `idx.schema.Primary` directly
- `store/commit.go:86-97`
  - commit revalidation also reads schema uniqueness directly

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-4-table-indexes-constraints/story-4.1-index-wrapper.md:16-27`
  - documents `Index` with fields `schema`, `unique`, and `btree`
- `docs/decomposition/001-store/epic-4-table-indexes-constraints/story-4.1-index-wrapper.md:51-52`
  - acceptance criteria explicitly mention each `Index` having the correct unique flag

Current observed behavior:
- Targeted constraint/index tests passed during audit:
  - `rtk go test ./store -run 'TestTablePKViolation|TestTableSetSemantics|TestTableDeleteReinsert|TestTransactionInsertUndeletesCommittedPrimaryKey|TestTransactionInsertUndeletesCommittedSetSemanticsRow|TestCommitDeleteIdenticalReinsertCollapsesToEmptyChangeset'`
- No grounded runtime failure was found in Epic 4 behavior itself; the mismatch is limited to the documented internal struct shape.

Recommended resolution:
- Update Story 4.1 to describe the current simpler shape:
  - `Index` wraps `IndexSchema` + `BTreeIndex`
  - uniqueness and primary-ness are derived from `schema.Unique` / `schema.Primary`
- If the project actually wants a cached `unique` field for performance or clarity, add it deliberately and test it; otherwise the docs should stop promising the redundant field.

Suggested follow-up checks:
- When the docs are patched, re-scan nearby acceptance text for any other references assuming duplicated `unique` state on `Index`
- Keep Epic 4 tests focused on observable behavior (constraint enforcement / index maintenance), not internal redundant fields

### TD-010: SPEC-001 E3 is missing the documented `Bound` type and helper constructors

Status: resolved
Severity: medium
First found: SPEC-001 Epic 3 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3b (`SPEC-001 E3: B-Tree Index Engine`)

Summary:
- The core `IndexKey`, `BTreeIndex`, `SeekRange`, `Scan`, and multi-column behavior are implemented and tested.
- The public `Bound` type plus `UnboundedLow`, `UnboundedHigh`, `Inclusive`, and `Exclusive` helper constructors now exist in the `store` package.
- Focused constructor/API regression coverage now guards the documented Story 3.1 surface.

Why this matters:
- Even though the current runtime path uses `SeekRange(low, high *IndexKey)` with nil for unbounded endpoints, the decomposition/spec still defines `Bound` as part of the Epic 3 public contract.
- This is a real API-surface gap: consumers written to the documented Story 3.1 surface do not compile.
- The missing helper type also leaves the codebase without a clean forward-compatible place to express inclusive/exclusive endpoint semantics if the planned future variant lands.

Related code:
- `store/index_key.go:5-42`
  - implements `IndexKey`, `NewIndexKey`, `Len`, `Part`, `Compare`, `Equal`, but no `Bound`
- `store/btree_index.go:85-110`
  - `SeekRange(low, high *IndexKey)` is implemented directly using nil for unbounded endpoints
- repo-wide search under `store/` returned no `Bound`, `UnboundedLow`, `UnboundedHigh`, `Inclusive`, or `Exclusive`

Related spec / decomposition docs:
- `docs/decomposition/001-store/epic-3-btree-index-engine/story-3.1-index-key.md:35-48`
  - requires the `Bound` struct and four convenience constructors
- `docs/decomposition/001-store/epic-3-btree-index-engine/story-3.1-index-key.md:56-57`
  - acceptance criteria explicitly cover Bound construction semantics
- `docs/decomposition/001-store/epic-3-btree-index-engine/story-3.3-range-scan.md:26-28`
  - later story still references Bound semantics even though the current signature uses `*IndexKey`

Current observed behavior:
- Existing targeted index tests pass:
  - `rtk go test ./store -run 'TestIndexKeyCompare|TestIndexKeyMultiColumn|TestIndexKeyPrefixOrdering|TestBTreeInsertSeek|TestBTreeRemove|TestBTreeSeekRange|TestBTreeScan|TestExtractKey|TestBTreeSeekRangeNilBoundsAndMultiColumnBytesOrdering'`
- Targeted public-API compile repro from the audit failed:
  - temporary package referenced `store.Bound`, `store.UnboundedLow()`, `store.UnboundedHigh()`, `store.Inclusive(...)`, and `store.Exclusive(...)`
  - `rtk go test ./.tmp_store_bound_api` failed with undefined symbol errors for all of them

Recommended resolution options:
1. Preferred code fix:
   - add the `Bound` type and helper constructors in `store/index_key.go` (or another Epic 3-owned file)
   - keep the current `SeekRange(*IndexKey, *IndexKey)` API if desired, but surface the documented helper contract so later evolution has a stable home
   - add compile-time/public API tests for the new symbols
2. Alternative doc fix:
   - if the project intentionally simplified away `Bound`, update Story 3.1 and any later references so the public contract is consistently the nil-or-`*IndexKey` API only
   - this would be doc-drift cleanup rather than a code feature gap, but it needs to be made explicit

Suggested follow-up tests:
- compile-time API test for `Bound` and the four helper constructors
- direct constructor tests asserting unbounded/inclusive/exclusive flags are set correctly
- if a future `SeekBounds` variant is added, acceptance tests for mixed inclusive/exclusive endpoint semantics

### TD-009: SPEC-001 E2 table storage does not detach inserted `ProductValue`s from caller mutation

Status: resolved
Severity: high
First found: SPEC-001 Epic 2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 3 / Step 3a (`SPEC-001 E2: Schema & Table Storage`)

Summary:
- The store's table layer keeps the caller's `ProductValue` slice directly when inserting rows.
- Because `ProductValue` is a slice, mutating the caller-owned row after `InsertRow(...)` changes the stored row in place.
- `GetRow(...)` also returns that same stored slice directly, so callers can mutate committed table contents simply by editing the returned row.

Why this matters:
- SPEC-001 explicitly requires `ProductValue` contents to be immutable once inserted into the store or a transaction buffer.
- The spec also requires caller-provided bytes to be copied on insert unless exclusive ownership can be proven. The current table layer does not create a detached row copy at insert time, so slice-level mutation is observable even when individual `Value` bytes are internally copied.
- This breaks the basic storage contract for E2 and creates a hidden aliasing hazard for every later subsystem built on table reads/writes.

Related code:
- `store/table.go:52-73`
  - `InsertRow(...)` stores `row` directly in `t.rows[id]` with no `ProductValue.Copy()` or equivalent detach step
- `store/table.go:102-105`
  - `GetRow(...)` returns the stored `ProductValue` directly
- `types/product_value.go:46-58`
  - a deep-copy helper already exists (`ProductValue.Copy()`), but the store layer does not use it here
- `docs/decomposition/001-store/SPEC-001-store.md:83-84`
  - spec invariants say bytes are copied on insert and `ProductValue` contents are immutable once inserted

Related spec / decomposition docs:
- `docs/decomposition/001-store/SPEC-001-store.md:80-84`
  - required immutability invariants for inserted values/rows
- `docs/decomposition/001-store/epic-2-schema-table-storage/story-2.2-table-row-storage.md:25-37`
  - table storage story owns the insert/get/delete/scan surface where the aliasing occurs
- `docs/decomposition/001-store/epic-2-schema-table-storage/story-2.3-row-validation.md:12-34`
  - validation is intentionally separate; storage still owns what gets retained after insert

Current observed behavior:
- Existing targeted package tests still pass:
  - `rtk go test ./store -run 'TestTableInsertGetDelete|TestTableScan|TestValidateRow|TestAllocRowIDNeverResets'`
- Targeted runtime repro from the audit:
  - insert `row := ProductValue{NewString("hello")}`
  - mutate `row[0] = NewString("mutated-after-insert")`
  - `GetRow(...)` then returns `mutated-after-insert`
  - mutate the row returned by `GetRow(...)`
  - subsequent `GetRow(...)` returns `mutated-via-getrow`
  - observed output:
    - `after caller mutation: mutated-after-insert`
    - `after getrow mutation: mutated-via-getrow`

Recommended resolution options:
1. Preferred code fix:
   - copy rows on insertion into table storage (`row.Copy()`), and return detached copies from `GetRow(...)`, `DeleteRow(...)`, and `Scan()` if the contract is meant to be fully read-only to callers
   - at minimum, ensure inserted rows are detached from caller-owned memory so post-insert mutation cannot rewrite stored state
2. Follow-up design clarification:
   - decide whether row retrieval APIs are also intended to be immutable snapshots; if yes, defensive copies are needed on read paths too
   - if read-path mutability is intentionally allowed, update the spec/docs because that is not how the current invariants read

Suggested follow-up tests:
- mutate the caller-owned `ProductValue` after `InsertRow(...)` and assert stored data is unchanged
- mutate the `ProductValue` returned from `GetRow(...)` and assert future reads are unchanged
- repeat with `Bytes` columns to ensure both row-slice and byte-slice immutability guarantees hold together

### TD-008: SPEC-006 E6 engine-side schema export surface is entirely missing

Status: resolved
Severity: high
First found: SPEC-006 Epic 6 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 2 / Step 2e (`SPEC-006 E6: Schema export`)

Summary:
- The schema package now exposes the full engine-side export surface: `SchemaExport`, `TableExport`, `ColumnExport`, `IndexExport`, `ReducerExport`, and `(*Engine).ExportSchema()`.
- `ExportSchema()` now walks the immutable `SchemaRegistry` and returns a detached value snapshot with user tables, system tables, reducers, and lifecycle reducers.
- Focused regression coverage now guards ordering, lifecycle export flags, JSON round-trip behavior, and detached-snapshot semantics.

Why this matters:
- The repo now has the documented runtime-to-tooling bridge for future codegen and `schema.json` export.
- Client/tooling surfaces no longer depend on internal schema structs or missing ad-hoc traversal logic.
- The exported value shape is now covered by tests rather than only by decomposition docs.

Related code:
- `schema/export.go`
  - adds export value types and `Engine.ExportSchema()`
- `schema/export_test.go`
  - adds focused export ordering, lifecycle, JSON round-trip, and detachment coverage

Current observed behavior:
- Focused package tests pass:
  - `rtk go test ./schema`
- Full repo verification passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

### TD-007: SPEC-006 E5 `SchemaRegistry` table lookups are mutable, violating the read-only contract

Status: resolved
Severity: high
First found: SPEC-006 Epic 5 Story 5.4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 2 / Step 2d (`SPEC-006 E5: Validation, Build & SchemaRegistry`)

Summary:
- `SchemaRegistry.Table(...)` and `TableByName(...)` now return detached deep copies of `TableSchema` instead of pointers into internal registry storage.
- Returned copies preserve the existing pointer-returning API shape while protecting internal `Columns` and `Indexes` slices from caller mutation.
- Focused regression tests now prove that mutating a looked-up schema does not affect later registry reads.

Why this matters:
- Downstream subsystems can now treat `SchemaRegistry` as the immutable schema truth that Story 5.4 promises.
- The concurrency guarantee is restored by ensuring post-build callers cannot mutate registry-owned state.
- Nested slice state (`Columns`, `Indexes`, and index column lists) is now detached rather than only shallow-copied.

Related code:
- `schema/registry.go`
  - stores lookup indexes internally and clones `TableSchema` on `Table(...)` / `TableByName(...)`
  - adds `cloneTableSchema(...)` for deep-copying nested slices
- `schema/build_test.go`
  - adds regression coverage for both ID and name lookup immutability

Current observed behavior:
- Focused regression coverage passes:
  - `rtk go test ./types ./schema ./executor`
- Full repo verification passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

### TD-006: SPEC-006 E3.2 does not expose schema-facing `ReducerHandler` / `ReducerContext` aliases

Status: resolved
Severity: medium
First found: SPEC-006 Epic 3 Story 3.2 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 2 / Step 2c (`SPEC-006 E3.2: Reducer registration`)

Summary:
- Reducer registration behavior remains implemented in `schema/builder.go`, and the schema package now exposes `ReducerHandler` and `ReducerContext` aliases at the schema-facing API boundary.
- Public builder and registry signatures now use the schema-owned names instead of leaking `types.*` directly.
- Focused regression coverage now guards that callers can use reducer registration via `schema.ReducerHandler` / `*schema.ReducerContext`.

Why this matters:
- The decomposition/spec treats reducer registration as part of the schema builder API, not as a requirement for callers to import an internal/shared `types` package.
- This is an API-shape mismatch, not just a naming preference: code written to the documented `schema` surface cannot compile today.
- Leaving the low-level package exposed here weakens the intended ownership boundary between schema registration and executor/runtime internals.

Related code:
- `schema/builder.go:11-12`
  - builder lifecycle fields are typed as `func(*types.ReducerContext) error`
- `schema/builder.go:20-23`
  - reducer entries store `types.ReducerHandler`
- `schema/builder.go:90-115`
  - public `Reducer`, `OnConnect`, and `OnDisconnect` methods all use `types.*` in their signatures
- `schema/registry.go:11-14,23-26,75-92`
  - `SchemaRegistry` and implementation also expose `types.ReducerHandler` / `*types.ReducerContext`
- `types/reducer.go:6-18`
  - canonical reducer types currently live only in `types`

Related spec / decomposition docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md:225-246`
  - SPEC-006 presents reducer registration as part of the schema API using `ReducerHandler` / `ReducerContext` in the schema-facing examples
- `docs/decomposition/006-schema/epic-3-builder-registration/story-3.2-reducer-registration.md:18-30`
  - Story 3.2 deliverable requires a `ReducerHandler` type alias re-exported from SPEC-003 or defined here if SPEC-003 is not yet built
- `docs/decomposition/006-schema/epic-3-builder-registration/story-3.2-reducer-registration.md:43-45`
  - design notes treat lifecycle vs ordinary reducer signatures as intentional API surface owned by this slice
- `docs/EXECUTION-ORDER.md:176`
  - execution order explicitly calls this slice out as the producer of `Reducer`, `OnConnect`, `OnDisconnect` registration

Current observed behavior:
- Operational behavior is otherwise healthy:
  - `rtk go test ./schema -run 'TestBuilder|TestRegistry|TestBuildDuplicateReducerName|TestBuildReducerReservedName'`
    passed during audit
- Public-API compile repro from the audit:
  - temporary package using `var _ schema.ReducerHandler` and `var _ *schema.ReducerContext`
  - `rtk go test ./.tmp_schema_api_audit`
    failed with:
    - `undefined: schema.ReducerHandler`
    - `undefined: schema.ReducerContext`

Recommended resolution options:
1. Preferred code fix:
   - add schema-package aliases such as `type ReducerHandler = types.ReducerHandler` and `type ReducerContext = types.ReducerContext`
   - update public schema signatures and registry interfaces to use the schema-owned names
   - add compile-time tests proving callers can use reducer registration via `schema.ReducerHandler` / `*schema.ReducerContext`
2. Alternative doc fix:
   - if the project intentionally wants reducer registration to expose `types.*`, update SPEC-006 §4.3 and Story 3.2 to document that leakage explicitly
   - this would still be a less clean public API than the current decomposition promises

Suggested follow-up tests:
- compile-time API test that `schema.ReducerHandler` and `schema.ReducerContext` exist
- builder/registry tests using only schema-package names in public signatures
- a regression test preventing future reintroduction of `types.*` into schema-facing examples/contracts

### TD-005: SPEC-006 E4 does not honor `shunter:"-"` on anonymous embedded fields

Status: resolved
Severity: medium
First found: SPEC-006 Epic 4 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 2 / Step 2b (`SPEC-006 E4: Reflection path`)

Summary:
- The reflection path now parses the `shunter` tag before anonymous-embedding handling, so `shunter:"-"` exclusion applies first for every field.
- Excluded anonymous embedded structs are skipped instead of flattened, and excluded anonymous embedded pointer-to-struct fields are skipped instead of erroring.
- Focused reflection and `RegisterTable` regression tests now guard the exclusion-on-anonymous-embed contract.

Why this matters:
- The spec's ordered field-discovery contract is not just stylistic; it defines which reflected fields are part of the public schema surface.
- Today, callers cannot use `shunter:"-"` to suppress an embedded helper/base struct even though the spec says exclusion happens before embedding logic.
- The missing case is easy to miss because the current tests cover exclusion and embedding separately, but not exclusion on an anonymous embedded field.

Related code:
- `schema/reflect.go:31-65`
  - skips unexported fields, then immediately processes anonymous fields before tag parsing
  - `ParseTag(...)` is not called until after the embedded-pointer error / recursive flattening path
- `schema/register_table.go:20-30`
  - `RegisterTable[T]` depends directly on `discoverFields`, so the bad ordering affects the public API
- `schema/reflect_test.go:71-118`
  - has coverage for `shunter:"-"` on ordinary fields and for embedded pointer rejection, but no combined anonymous-embedded exclusion case

Related spec / decomposition docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md:478-483`
  - field-discovery order requires `shunter:"-"` skip before anonymous-embedding handling
- `docs/decomposition/006-schema/SPEC-006-schema.md:485-487`
  - flattened embedding and unexported-field behavior are separate rules after the ordered per-field decision
- `docs/decomposition/006-schema/epic-4-reflection-engine/story-4.1-field-discovery.md:26-35`
  - Story 4.1 deliverable lists `shunter:"-"` skip before embedded non-pointer recursion / embedded pointer rejection
- `docs/decomposition/006-schema/epic-4-reflection-engine/story-4.3-register-table-integration.md:16-21`
  - `RegisterTable[T]` is supposed to expose the reflection pipeline faithfully through the public API

Current observed behavior:
- Existing package tests still pass: `rtk go test ./schema`
- Targeted runtime repro from the audit:
  - `ExcludedEmbedded struct { Embedded \`shunter:"-"\`; Name string }` registers as columns `[id name]` instead of skipping the embedded fields
  - `ExcludedEmbeddedPtr struct { *Embedded \`shunter:"-"\`; Name string }` returns `schema error: ExcludedEmbeddedPtr.Embedded: embedded pointer-to-struct is not supported` instead of skipping the excluded field

Recommended resolution options:
1. Preferred code fix:
   - in `discoverFields`, parse the tag before anonymous-embedding handling
   - if `td.Exclude` is true, skip the field immediately regardless of whether it is ordinary or anonymous
   - preserve the current path/error context for non-excluded embedded pointer failures
2. Test fix required alongside code fix:
   - add reflection-path tests for excluded anonymous embedded struct and excluded anonymous embedded pointer-to-struct cases
   - add a public `RegisterTable` integration test proving the built schema omits excluded embedded fields

Suggested follow-up tests:
- `discoverFields` should skip `Embedded \`shunter:"-"\`` entirely
- `discoverFields` should skip `*Embedded \`shunter:"-"\`` instead of erroring
- `RegisterTable` + `Build` should produce only non-excluded outer fields when an embedded helper struct is tagged out

### TD-004: SPEC-006 Story 5.6 schema compatibility checking is entirely missing

Status: resolved
Severity: high
First found: SPEC-006 Epic 5 audit
Execution-order context:
- not on the earliest critical path for Phase 1, but it is part of the current implemented `validation/build` surface and is explicitly required by Epic 5 before schema/runtime startup can be considered spec-complete

Summary:
- The schema package now implements startup compatibility checking with `SnapshotSchema`, `ErrSchemaMismatch`, `SchemaMismatchError`, and `CheckSchemaCompatibility(...)`.
- `Engine.Start(...)` now runs that comparison against optional startup snapshot metadata supplied through `EngineOptions`.
- Focused tests now cover matching schemas, version mismatches, structural diffs, nil-snapshot success, and Start-time enforcement.

Why this matters:
- Runtime startup now rejects incompatible registered/snapshot schema combinations instead of silently accepting drift.
- The missing Epic 5 comparison seam is now present for future recovery integration.
- Mismatch failures now carry specific diff detail instead of a generic startup error.

Related code:
- `schema/version.go`
  - adds `SnapshotSchema`, `ErrSchemaMismatch`, `SchemaMismatchError`, `CheckSchemaCompatibility(...)`, and `Engine.Start(...)` compatibility enforcement
- `schema/builder.go`
  - adds `EngineOptions.StartupSnapshotSchema`
- `schema/version_test.go`
  - adds focused compatibility and Start-time regression coverage

Current observed behavior:
- Focused package tests pass:
  - `rtk go test ./schema`
- Full repo verification passes:
  - `rtk go build ./...`
  - `rtk go vet ./...`
  - `rtk go test ./...`

### TD-114: SPEC-002 E6 active-tail CRC mismatch after a valid prefix is treated as a hard failure instead of a truncation horizon

Status: resolved
Severity: high
First found: SPEC-002 Epic 6 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4i (`SPEC-002 E6: Recovery`)

Resolution:
- `commitlog/segment_scan.go` now treats active-tail checksum mismatches the same as truncated tail damage when at least one valid record precedes the damage.
- `ScanSegments(...)` now stops at the last valid contiguous record, returns that horizon, and marks the last segment `AppendByFreshNextSegment` for both short-read and CRC-mismatch damaged tails.
- First-record corruption in the active segment and checksum mismatches in sealed segments remain hard failures.
- Regression coverage now locks both payload-byte and CRC-byte active-tail corruption after a valid prefix to the fresh-next-segment path.

Verification:
- `rtk go test ./commitlog`
- `rtk go test ./...`

### TD-115: SPEC-002 E6 append-open truncates first-record corruption instead of failing closed

Status: resolved
Severity: high
First found: SPEC-002 Epic 6 audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 4 / Step 4i (`SPEC-002 E6: Recovery`)

Resolution:
- `commitlog/segment.go` now makes `OpenSegmentForAppend(...)` fail closed when decode damage appears before any valid record in the segment.
- Truncate-and-resume behavior is now limited to damaged tails with a valid prefix; corrupt-first-record / no-valid-prefix cases return the underlying decode error and preserve the file contents.
- The durability resume path preserves that fail-closed behavior because `NewDurabilityWorkerWithResumePlan(...)` still uses `OpenSegmentForAppend(...)` for `AppendInPlace` resumes.
- Regression coverage now proves: corrupt-first-record reopen fails without truncation, damaged tails after a valid prefix still truncate to the last good offset, and append-in-place durability reopen fails closed on corrupt-first-record segments.

Verification:
- `rtk go test ./commitlog`
- `rtk go test ./...`

### TD-125: `ErrNullableColumn` sentinel + `Build()` enforcement not wired in live `schema/` package

Status: resolved
Severity: low
First found: Lane B Session 4.5 reconciliation pass
Spec ref: SPEC-006 §9 / §13, SPEC-001 §3.1, SPEC-001 Story 2.1, SPEC-006 Story 5.1

Resolution:
- `schema/errors.go` now declares `ErrNullableColumn`.
- `schema.ColumnDefinition` now carries a `Nullable` field, `schema/build.go` propagates it into `ColumnSchema.Nullable`, and `schema/reflect_build.go` explicitly sets reflection-path columns to `Nullable: false`.
- `schema/validate_structure.go` now enforces the v1 rule at build time, so any table definition containing `Nullable: true` fails `Build()` with `ErrNullableColumn`.
- Focused regression coverage now verifies that the builder path rejects nullable columns instead of relying on construction-time omission.

Verification:
- `rtk go test ./schema -run TestBuildRejectsNullableColumn`
- `rtk go test ./schema ./commitlog`
- `rtk go test ./...`

### TD-126: Snapshot-recovery `ErrSchemaMismatch` does not wrap `ErrNullableColumn` when recovery traces back to a stored nullable flag

Status: resolved
Severity: low
First found: Lane B Session 4.5 reconciliation pass
Spec ref: SPEC-006 §13, SPEC-002 §6.1, SPEC-002 Story 6.2

Resolution:
- `commitlog.SchemaMismatchError` now carries an optional wrapped cause via `Unwrap()`.
- `commitlog/snapshot_select.go` now returns a `SchemaMismatchError` that wraps `schema.ErrNullableColumn` when recovery encounters a snapshot column with `nullable = 1`.
- Focused regression coverage now verifies both `errors.As(..., *SchemaMismatchError)` and `errors.Is(..., schema.ErrNullableColumn)` on nullable-snapshot rejection.

Verification:
- `rtk go test ./commitlog -run TestSelectSnapshotNullableColumnWrapsErrNullableColumn`
- `rtk go test ./schema ./commitlog`
- `rtk go test ./...`

### TD-127: v1 snapshot rejection of `nullable = 1` relies on equality-mismatch coincidence, not an independent rule

Status: resolved
Severity: low
First found: Lane B Session 4.5 reconciliation pass
Spec ref: SPEC-002 §5.3, SPEC-002 §6.1 step 4b, SPEC-002 Story 6.2

Resolution:
- `commitlog/snapshot_select.go` now rejects `snapCol.Nullable == true` before the general registry-vs-snapshot equality check.
- That direct branch makes the v1 policy independent of registry state, so nullable snapshots are still rejected even if a future/fake registry also reports `Nullable: true`.
- Focused regression coverage now proves the direct-rejection path by selecting a nullable snapshot against a registry intentionally mutated to the same nullable shape; recovery still fails with `ErrNullableColumn`.

Verification:
- `rtk go test ./commitlog -run TestSelectSnapshotRejectsNullableSnapshotEvenWhenRegistryAlsoNullable`
- `rtk go test ./schema ./commitlog`
- `rtk go test ./...`

### TD-128: `FsyncMode` enum + `ErrUnknownFsyncMode` rejection not wired in live `commitlog/` durability worker

Status: resolved
Severity: low
First found: Lane B Session 8 reconciliation pass
Spec ref: SPEC-002 §8 (CommitLogOptions), SPEC-002 §13 OQ#3 (Write-ahead guarantee level), SPEC-002 §4.4

Resolution:
- `commitlog/durability.go` now declares the spec-facing `FsyncMode` enum and adds `CommitLogOptions.FsyncMode` with `DefaultCommitLogOptions()` defaulting to `FsyncBatch`.
- `commitlog/errors.go` now declares `ErrUnknownFsyncMode`.
- `NewDurabilityWorkerWithResumePlan(...)` now validates `opts.FsyncMode` at construction time and rejects every v1 mode except `FsyncBatch`, including reserved `FsyncPerTx`.
- Focused regression coverage now verifies both plain and resume-plan worker constructors reject unsupported fsync modes with `ErrUnknownFsyncMode`.

Verification:
- `rtk go test ./commitlog -run 'TestNewDurabilityWorkerRejectsUnknownFsyncMode|TestNewDurabilityWorkerWithResumePlanRejectsUnknownFsyncMode'`
- `rtk go test ./schema ./commitlog`
- `rtk go test ./...`

### TD-129: SPEC-004 E6 confirmed-read gating skips caller-only reducer-result delivery

Status: resolved
Severity: medium
First found: SPEC-004 E6 remainder audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 8 / Step 8a (`SPEC-004 E6 remainder: Fan-Out & Delivery`)

Resolution:
- `subscription/fanout_worker.go` now treats the caller connection as a confirmed-read recipient when `CallerConnID` and `CallerResult` are present, even if `msg.Fanout` is empty.
- Confirmed-read gating now waits on `TxDurable` for caller-only reducer-result delivery batches, matching Story 6.4's caller-only acceptance criterion instead of only scanning non-caller fanout recipients.
- Focused regression coverage now proves the worker blocks reducer-result delivery until durability is signaled for caller-only confirmed-read batches.

Verification:
- `rtk go test ./subscription -run 'TestFanOutWorker_ConfirmedRead_Waits|TestFanOutWorker_ConfirmedReadCallerOnly_Waits'`
- `rtk go test ./subscription ./protocol`
- `rtk go test ./...`

### TD-130: protocol fanout adapter path bypasses active-subscription ordering guard

Status: resolved
Severity: medium
First found: SPEC-004 E6 remainder / SPEC-005 Epic 5 follow-through audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 8 / Step 8a (`SPEC-004 E6 remainder: Fan-Out & Delivery`)

Resolution:
- `protocol/sender.go` now enforces the protocol ordering invariant at the typed sender boundary: `SendTransactionUpdate` validates every referenced subscription is `SubActive`, and `SendReducerResult` does the same for embedded committed caller updates.
- This closes the live gap where `protocol.FanOutSenderAdapter` could bypass `DeliverTransactionUpdate(...)` / `DeliverReducerCallResult(...)` helper validation and enqueue updates for pending subscriptions directly through `ClientSender`.
- Focused regression coverage now proves adapter-driven transaction updates and reducer-call results both reject pending subscriptions with `ErrSubscriptionNotActive`.

Verification:
- `rtk go test ./protocol -run 'TestFanOutSenderAdapter_SendTransactionUpdateRejectsPendingSubscription|TestFanOutSenderAdapter_SendReducerResultRejectsPendingSubscription'`
- `rtk go test ./protocol ./subscription`
- `rtk go test ./...`

### TD-131: FanOutWorker defaults to fast-read delivery instead of protocol-v1 confirmed reads

Status: resolved
Severity: medium
First found: SPEC-004 E6 remainder audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 8 / Step 8a (`SPEC-004 E6 remainder: Fan-Out & Delivery`)

Resolution:
- `subscription/fanout_worker.go` now treats every protocol-visible recipient as confirmed-read by default whenever `TxDurable` is present: any non-empty `Fanout` batch or caller-result batch waits for durability readiness before delivery.
- This aligns the live worker with Story 6.4 / SPEC-005's protocol-v1 rule that WebSocket clients always observe confirmed-read behavior because there is no negotiated wire-level fast-read mode.
- Nil `TxDurable` still skips the wait for test/internal zero-value paths, preserving the documented internal escape hatch without making public protocol delivery fast-read by default.
- Focused regression coverage now verifies the default public-protocol path waits for `TxDurable` even without explicit per-connection opt-in.

Verification:
- `rtk go test ./subscription -run 'TestFanOutWorker_PublicProtocolDefault_WaitsForDurability|TestFanOutWorker_ConfirmedRead_Waits|TestFanOutWorker_ConfirmedReadCallerOnly_Waits|TestFanOutWorker_NilTxDurable_Skips'`
- `rtk go test ./subscription ./protocol`
- `rtk go test ./...`

### TD-132: fanout-path SubscriptionError delivery dropped subscribe request correlation and emitted wire `request_id = 0`

Status: resolved
Severity: medium
First found: SPEC-004 E6 / SPEC-005 delivery-seam audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 8 / Step 8a (`SPEC-004 E6 remainder: Fan-Out & Delivery`)

Resolution:
- `subscription/query_state.go` now stores per-subscription delivery metadata, including the original subscribe `RequestID`, instead of only tracking a bare membership set.
- `subscription/register.go` persists that `RequestID` when a subscription is registered, and `subscription/eval.go` now re-emits it in queued `SubscriptionError` batches when reevaluation fails.
- `subscription/fanout_worker.go` / `protocol/fanout_adapter.go` now pass the full `subscription.SubscriptionError` payload across the fanout seam, so the protocol wire `SubscriptionError` keeps the original request correlation instead of silently zeroing it.
- Focused regression coverage now proves both the subscription reevaluation path and the protocol adapter preserve the original subscribe request identity.

Verification:
- `rtk go test ./protocol ./subscription -run 'TestFanOutSenderAdapter_SendSubscriptionErrorPreservesRequestID|TestEvalErrorQueuesSubscriptionErrorWithoutDroppingConnection'`
- `rtk go test ./protocol ./subscription`
- `rtk go test ./...`

### TD-133: SPEC-004 / SPEC-005 docs lagged the live SubscriptionError request-correlation seam after TD-132

Status: resolved
Severity: low
First found: SPEC-004 E6 / SPEC-005 delivery-seam audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 8 / Step 8a (`SPEC-004 E6 remainder: Fan-Out & Delivery`)

Resolution:
- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` now documents the live `FanOutSender.SendSubscriptionError(connID, subErr SubscriptionError)` seam, includes `RequestID` on the subscription-side `SubscriptionError` shape, and clarifies that reevaluation errors preserve request identity when still known.
- `docs/decomposition/004-subscriptions/epic-6-fanout-delivery/story-6.1-fanout-worker.md` now explains that the fanout-side subscription-error seam carries the full payload rather than only `(subID, message)`.
- `docs/decomposition/005-protocol/SPEC-005-protocol.md` and Story 5.2 now describe `request_id = 0` as the genuinely uncorrelated case, not the blanket reevaluation case.

Verification:
- `rtk grep -n "SendSubscriptionError|RequestID|uncorrelated spontaneous" docs/decomposition/004-subscriptions docs/decomposition/005-protocol`
- `rtk go test ./...`

### TD-134: SendSubscribeApplied could leave a subscription spuriously active after delivery failed

Status: resolved
Severity: medium
First found: SPEC-005 Epic 5 response-delivery audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 7 / Step 7e (`SPEC-005 E5: server message delivery / ClientSender`)

Resolution:
- `protocol/send_responses.go` now removes the subscription tracker entry if `SendSubscribeApplied` fails after activation, so a failed delivery cannot leave the connection thinking the subscription became active when no `SubscribeApplied` ever committed to the wire.
- Focused regression coverage now proves failed `SubscribeApplied` delivery returns the send error and releases the subscription instead of leaving it active.

Verification:
- `rtk go test ./protocol -run 'TestSendSubscribeApplied'`
- `rtk go test ./protocol`
- `rtk go test ./...`

### TD-135: accepted protocol commands allocated response channels but never delivered async executor results

Status: resolved
Severity: medium
First found: SPEC-005 Epic 4/5 dispatch-to-delivery seam audit
Execution-order slice: `docs/EXECUTION-ORDER.md` Phase 7 / Steps 7d-7e (`SPEC-005 E4/E5`)

Resolution:
- `protocol/handle_subscribe.go`, `protocol/handle_unsubscribe.go`, and `protocol/handle_callreducer.go` now attach async response watchers after successful executor submission.
- New `protocol/async_responses.go` provides a conn-local sender plus watcher helpers that turn executor response-channel results into actual outbound `SubscribeApplied`, `UnsubscribeApplied`, `SubscriptionError`, and `ReducerCallResult` messages.
- Focused regression coverage now proves accepted subscribe/unsubscribe/reducer commands no longer stop at channel allocation and instead deliver their async executor responses onto the connection's outbound queue.

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribe_DeliversAsyncSubscribeApplied|TestHandleUnsubscribe_DeliversAsyncUnsubscribeApplied|TestHandleCallReducer_DeliversAsyncReducerResult'`
- `rtk go test ./protocol`
- `rtk go test ./...`

## Code review audit (2026-04-15)

This section is distinct from the per-spec audit above. It is a broad code-quality sweep across all live Go packages (`auth`, `bsatn`, `commitlog`, `executor`, `protocol`, `schema`, `store`, `subscription`, `types`) — non-test code only — looking for:

1. **Correctness bugs** — races, durability hazards, broken invariants, error swallowing, byte-order/size handling
2. **Go idiom misuse** — non-idiomatic error chains, `any` where generics fit, hand-rolled stdlib equivalents, panics in library code, missing `Stringer`/docs
3. **Duplication** — parallel switch ladders, copy-pasted helpers, repeated kind/predicate walks

Methodology: six parallel `code-reviewer` agents, one per package slice, plus a cross-package duplication sweep. Findings are grounded by direct file reads; speculative items are flagged `FOLLOWUP`.

ID scheme: `TD-027` through `TD-12x`, grouped by package. Severity tags inline: `BUG` (broken / data-loss / race), `SMELL` (works but wrong shape), `DUP` (duplication), `FOLLOWUP` (verify before fixing).

Verification commands not re-run for this pass — these are static-read findings. Each item should be verified by adding a focused test or compile-only repro before remediation.

### A. Concurrency & durability — critical

These are correctness-fatal under concurrent load or restart and are the highest-priority items in this pass.

- **TD-027** [BUG][resolved] `executor/executor.go`, `executor/executor_test.go`, `executor/pipeline_test.go`, `executor/lifecycle_test.go` — `fatal`/`shutdown` are now stored with `atomic.Bool`, submit paths are serialized against inbox close with a `submitMu` + existing `closeOnce`, and concurrent shutdown/submit no longer races the channel close path. Focused verification:
  - `rtk go test -race ./executor -run 'TestExecutorShutdownConcurrentSubmittersDoNotPanic|TestSubmitAfterPostCommitFatalReturnsExecutorFatal' -count=1`
  - `rtk go test ./executor`

- **TD-028 / TD-029 / TD-030** [BUG][resolved] `commitlog/durability.go`, `commitlog/phase4_acceptance_test.go` — durability enqueue/close now uses a dedicated close signal plus sender tracking so blocked producers wake with the intended close panic instead of crashing on `send on closed channel`; `Close()` waits for in-flight senders before closing the work channel; the batch drain loop now exits immediately on closed-channel detection via a labeled break; and the reopen/resume path remains guarded by focused restart coverage so an existing active segment is resumed instead of truncated. Focused verification:
  - `rtk go test ./commitlog -run 'TestDurabilityWorkerCloseWhileEnqueueBlockedReturnsControlledClosePanic|TestDurabilityWorkerCloseAfterSingleQueuedItemDoesNotSpinOnClosedDrain|TestDurabilityWorkerReopensExistingSegment' -count=1`
  - `rtk go test ./commitlog`

- **TD-031 / TD-032 / TD-033** [BUG][resolved] `store/commit.go`, `store/audit_regression_test.go` — commit now prevalidates delete targets and staged insert conflicts under the committed-state write lock before mutating any table state, preserving the Story 6.2 atomicity invariant; delete application now fails loudly on missing rows instead of silently skipping them; and rollback-to-commit rejection remains guarded by regression coverage. Focused verification:
  - `rtk go test ./store -run 'TestCommitAtomicityFailureLeavesCommittedStateUnchanged|TestCommitMissingDeleteTargetReturnsErrorWithoutMutation|TestRollbackBlocksPostRollbackCommit' -count=1`
  - `rtk go test ./store`

- **TD-034** [BUG][resolved] `store/committed_state.go`, `store/recovery.go`, `store/audit_regression_test.go` — `CommittedState` table-map access now consistently uses its `RWMutex`: `RegisterTable` takes the write lock, `Table`/`TableIDs` take read locks, and internal callers that already hold the write lock use narrow locked helpers to avoid self-deadlock during commit/recovery paths. Focused verification:
  - `rtk go test -race ./store -run 'TestCommittedStateRegisterAndLookupAreRaceFree' -count=1`
  - `rtk go test ./store`

- **TD-035** [BUG] `store/snapshot.go:28-31` — `Snapshot()` acquires `RLock` and stores no goroutine ownership. A leaked snapshot (no `Close`) silently blocks all commits forever. Same-goroutine `Lock` after `RLock` deadlocks (no recursive locking). Fix: add finalizer or document the contract more loudly with a runtime guard.

- **TD-036** [BUG] `executor/registry.go:6-9` — `ReducerRegistry` has zero synchronization. `Register` writes the map; `Lookup`/`LookupLifecycle`/`All` are called from the executor goroutine but also from any caller that holds the registry. The `frozen` flag is a plain bool. If anyone calls `Register` concurrently with `Lookup` (e.g. registration races startup), this races. Fix: document "single-threaded build, frozen before publish" + `Freeze()` happens-before fence (atomic.Bool), or guard with `sync.RWMutex`.

- **TD-037 / TD-038 / TD-039** [BUG][resolved] `executor/scheduler_worker.go`, `executor/scheduler_worker_test.go` — the scheduler worker now stops each per-iteration timer explicitly instead of stacking deferred `timer.Stop()` calls inside the run loop; enqueue is context-aware so a blocked inbox no longer wedges shutdown; and `drainResponses` keeps draining briefly after cancellation so in-flight executor writers do not strand on a full response buffer. Regression coverage lives in `TestSchedulerRunStopsOnCtxCancel`, `TestSchedulerNotifyTriggersRescan`, `TestSchedulerRunCancelsWhileEnqueueBlocked`, and `TestSchedulerDrainResponsesKeepsDrainingAfterCancel`.

- **TD-040** [BUG] `protocol/disconnect.go:44` — `close(c.OutboundCh)` is unsafe: any future writer goroutine sending on this channel will panic. The fan-out worker (Phase 8) and Epic 4 write loop are documented to send on `OutboundCh`; closing it from `Disconnect` (a non-sender) without synchronization sets up a "send on closed channel" panic the moment those layers land. Test at `disconnect_test.go:44` enforces the wrong invariant. Fix: drop the `close(OutboundCh)` here entirely; let the writer goroutine drain on `<-c.closed` and close the channel itself, OR add a `sendMu` + `sendClosed bool` guard on every send.

- **TD-041** [BUG] `protocol/upgrade.go:177-184` — Read pump and keepalive goroutines use `context.Background()`, severing them from `r.Context()` and any engine shutdown context. When the host process shuts down or the request context cancels, the goroutines keep running until the peer drops or idle timeout fires. Fix: derive a per-connection context from a long-lived server context (add `Server.ctx`), pass that to both pumps, and cancel it inside `Disconnect` so the read pump's `c.ws.Read(ctx)` unblocks immediately.

- **TD-042** [BUG] `protocol/disconnect.go:46-48` and `protocol/keepalive.go:80-82` — Two detached close goroutines, no `sync.WaitGroup` to track them, no use of `CloseHandshakeTimeout` from `ProtocolOptions` (defined at `options.go:26`, never read). On engine shutdown these accumulate (one per connection ever opened in this process if peers ignore Close handshakes for the full 5 s). Fix: track via `WaitGroup` on `Server`; use `CloseHandshakeTimeout` to bound the close.

- **TD-043** [BUG] `protocol/lifecycle.go:74-90` — Race between `mgr.Add(c)` (line 74) and the `mgr.Remove(c.ID)` on write-failure (lines 82, 87). A concurrent fan-out goroutine that resolves the ID between `Add` and the failed `Write` will deliver onto a `Conn` whose socket is about to close, and the delivery write races the close. The comment at line 71 ("Register before first send") flags the very window that is broken. Fix: hold a `Conn.admitMu` (write-locked here, read-locked by senders) across the entire admit-or-close transition.

- **TD-044** [BUG] `protocol/keepalive.go:48` — `context.WithTimeout(ctx, c.opts.PingInterval)` uses the ping cadence as the ping deadline. If `PingInterval == IdleTimeout/2` (default 15 s / 30 s) a stuck peer keeps `Ping` blocked for a full interval before the idle check runs, doubling the effective idle window. Fix: derive ping timeout from a separate option, or cap at `IdleTimeout - (now - lastActivity)`.

- **TD-045** [BUG] `subscription/manager.go:67-73` — `evaluate` mutates loop variable `u` (`u.SubscriptionID = subID`) and appends it. Because `u.Inserts`/`u.Deletes` slices alias the same backing array across every `(connID, subID)` tuple, downstream code that retains and mutates one connection's `Inserts` (e.g. encoding into a buffer that filters by predicate per-client, future row-redaction layer) corrupts every other subscriber. Silent cross-subscriber data leakage. Fix: `slices.Clone` the row slices, or document slice-immutable contract aggressively.

### B. Logic & error-handling bugs

- **TD-046** [BUG] `bsatn/decode.go:138-152` — `DecodeProductValueFromBytes` fabricates `RowShapeMismatchError{Got: len(ts.Columns)+1}` when trailing bytes exist. The `+1` is a lie — there could be many trailing bytes representing many extra columns, or fewer than one column's worth of garbage. Callers reading `Got` and computing `Got - Expected` get a meaningless `1`. Fix: introduce `ErrTrailingBytes = errors.New("bsatn: trailing bytes after row")` and return that wrapped with `r.pos`/`len(r.data)` for diagnostics.

- **TD-047** [BUG] `bsatn/decode.go:142-146` — When `DecodeProductValueFromBytes` catches `*RowShapeMismatchError` it discards the rich error and returns the bare `ErrRowLengthMismatch` sentinel, losing the table name and column counts. Callers can't render diagnostic messages or test for specific shape mismatches. Fix: return the original `shapeErr` (already implements `Error()`), or `fmt.Errorf("%w: %v", ErrRowLengthMismatch, shapeErr)`.

- **TD-048** [BUG] `auth/jwt.go:78` — `fmt.Errorf("%w: %v", ErrJWTInvalid, err)` discards the underlying error's `Unwrap` chain. The inner `err` from `jwt.Parse` may be `jwt.ErrTokenExpired`, `jwt.ErrTokenSignatureInvalid`, etc. — callers can no longer use `errors.Is(err, jwt.ErrTokenExpired)` to distinguish expired from malformed. SPEC-005 §4.3 likely wants different HTTP responses for expired vs malformed tokens. Fix: `fmt.Errorf("%w: %w", ErrJWTInvalid, err)` (Go 1.20+ multi-`%w`) so both unwrap chains are preserved.

- **TD-049** [BUG] `schema/tag.go:30-37` — Duplicate-key detection over-collapses `index` and `index:<name>` into the same key `"index"`. `unique,index:guild_score` triggers the duplicate trap on a second `index:`; `index,index:foo` is rejected as "duplicate" rather than the targeted "plain+named both appear" error. Fix: separate the dup-set into per-base-key buckets and key on whether the directive is parametric, or detect duplicates only for atoms (`primarykey`, `unique`, `name`, `-`) and let the index-combination rules handle index variants.

- **TD-050** [BUG] `schema/build.go:84-94` — Inner triple loop (`indexes -> columns -> td.Columns` linear search) for resolving column-name to index is O(I·C·C). When a column name is missing, the inner loop silently leaves `cols[k]==0` (pointing at the first column) instead of failing — `validateStructure` already guarantees referenced columns exist, so this is latent, but a future refactor will re-introduce the bug. Fix: precompute `colIdx := map[string]int{}` and panic on missing (invariant violation post-validation).

- **TD-051** [BUG] `store/recovery.go:36-39` — `ApplyChangeset` allocates a fresh RowID per replayed insert via `table.AllocRowID()`, but does not advance `nextID` past the maximum RowID seen during replay if inserts and deletes interleave. When recovery finishes, the next runtime insert can collide with a previously-allocated-and-deleted ID if the snapshot persisted a higher `nextID` that wasn't restored. Fix: explicitly seed `nextID` after replay or ensure recovery snapshot restores it via `SetNextID`. Tracked partially in `TableExportState` but never wired.

- **TD-052** [BUG][resolved] `commitlog/segment.go`, `commitlog/commitlog_test.go` — `OpenSegment` now rejects malformed and non-canonical segment filenames instead of silently accepting a bogus `startTx`, and regression coverage includes `TestOpenSegmentRejectsMalformedFilename` plus `TestOpenSegmentRejectsNonCanonicalFilename`.

- **TD-053** [BUG][resolved] `executor/executor.go`, `executor/phase4_acceptance_test.go` — reducer response delivery now goes through `sendReducerResponse`, which treats a nil `ResponseCh` as fire-and-forget instead of panicking on direct channel send. Regression coverage: `TestPhase4HandleCallReducerNilResponseChannelDoesNotPanic`.

- **TD-054** [BUG][resolved] `executor/executor.go`, `executor/phase4_acceptance_test.go` — reducer panic handling now preserves the original panic error chain when the panic value is itself an `error` by joining it with `ErrReducerPanic`, instead of flattening it through `%v`. Regression coverage: `TestPhase4HandleCallReducerBeginExecuteCommitRollback` asserts both `errors.Is(resp.Error, ErrReducerPanic)` and `errors.Is(resp.Error, errReducerBoom)`.

- **TD-055** [BUG] `executor/scheduler.go:115-127` — `Cancel` returns `false` when the schedule exists but `tx.Delete` failed, indistinguishable from "not found." Fix: change signature to `(bool, error)` or log the delete error before returning false.

- **TD-056** [BUG] `executor/lifecycle.go:165-174` — `deleteSysClientsRow` ranges `tx.ScanTable` and calls `tx.Delete` *during* iteration; if `ScanTable` returns a live (non-snapshot) iterator, deleting the row mutates the underlying structure during traversal. Returning immediately after the first delete is the only thing that saves it. Fix: collect the rowID first, then delete, or document `ScanTable`'s mutation safety.

- **TD-057** [BUG][resolved] `executor/registry.go`, `executor/registry_td057_test.go` — `ReducerRegistry` now caches lifecycle reducers in direct indexed slots during `Register`, so `LookupLifecycle` no longer scans the full reducer map and returns the exact slot for each lifecycle kind. Regression coverage: `TestRegisterCachesLifecycleSlots`.

- **TD-058** [BUG][resolved] `subscription/delta_dedup.go`, `subscription/delta_dedup_test.go` — audit confirmed `ReconcileJoinDelta` already nets multiplicities correctly across the full 4+4 fragment inputs; added `TestReconcileDistributedFragmentsNetCount` to lock in distributed-fragment netting behavior, so this debt was stale rather than a live bug.

- **TD-059** [BUG][resolved] `subscription/eval.go`, `subscription/eval_test.go`, `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md`, `docs/decomposition/004-subscriptions/epic-5-evaluation-loop/story-5.1-eval-transaction.md` — evaluation errors now queue `SubscriptionError` and unregister only the broken subscription(s)/query state instead of disconnecting whole connections, so unrelated subscriptions on the same connection stay alive. Regression coverage: `TestEvalErrorQueuesSubscriptionErrorWithoutDroppingConnection`.

- **TD-060** [BUG][resolved] `subscription/placement.go`, `subscription/placement_test.go` — audit confirmed join placement already handles a `Join` with an LHS-side filter by putting that table in Tier 1 and the opposite table in Tier 2, so RHS-only changes are still collected through JoinEdge. Added `TestPlaceJoinWithFilterOnLHSStillTracksRHSChangesViaJoinEdge` to lock in the intended invariant; this debt was stale rather than a live bug.

- **TD-061** [BUG][resolved] `subscription/register.go`, `subscription/manager_test.go` — join bootstrap now falls back to driving the RHS and probing an LHS index when validation accepted the join via the left side only, instead of hard-failing on missing RHS resolver state. Regression coverage: `TestRegisterJoinBootstrapFallsBackToLeftIndex`.

- **TD-062** [BUG] `protocol/compression.go:36-46` — `EncodeFrame` swallows the `WrapCompressed` error and silently downgrades to uncompressed instead of returning an error. The "we never panic in delivery" comment hides that the client negotiated gzip and is now receiving a frame the spec says must be gzipped. Fix: return `([]byte, error)` and let the caller decide.

- **TD-063** [BUG] `protocol/options.go:66` — `panic` on `crypto/rand.Read` failure inside library code; comment justifies it but library functions should return errors. Fix: return `(types.ConnectionID, error)`; callers in `upgrade.go:214` already have an error path.

- **TD-064** [FOLLOWUP] `bsatn/decode.go:98,111` — `make([]byte, n)` where `n` is an attacker-controlled `uint32` from the wire. A malicious peer can send `n = 0xFFFFFFFF` and force a 4 GiB allocation before the `io.ReadFull` fails. Verify whether SPEC-005 / project-brief caps message size at the transport layer (websocket frame max). If not, add `if n > MaxStringLen { return ..., ErrStringTooLarge }`.

- **TD-065** [FOLLOWUP] `auth/jwt.go:67-76` — Keyfunc returns `config.SigningKey` for any HMAC method (HS256/HS384/HS512). Comment says "v1 supports HS256 only" but the type assertion accepts any `*jwt.SigningMethodHMAC`. Verify intent; if HS256 only, check `t.Method.Alg() == "HS256"` explicitly.

- **TD-136** [BUG][resolved] `protocol/handle_subscribe_single.go`, `protocol/send_responses.go`, `protocol/conn.go` — Root-cause close on branch `phase-2-slice-2-td140-admission-model`. The C1 gate (`SendSubscribeSingleApplied` early-return on `IsPending`) was first narrowly dropped in `706f18d` / `71b679f`, then eliminated structurally along with the tracker itself in the Group B admission-model slice: `protocol.SubscriptionTracker`, `Conn.Subscriptions`, and the `Reserve`/`Activate`/`IsPending`/`IsActive` surface are all removed. Admission is now manager-authoritative (`subscription.Manager.querySets`). `SendSubscribeSingleApplied` is a straight push identical in shape to `SendSubscribeMultiApplied`. Covered by `TestTD136_SubscribeSingleAppliedReachesWireWithoutTrackerSeed` and the synchronous `Reply` ordering pins in `admission_ordering_test.go`. See ADR `docs/adr/2026-04-19-subscription-admission-model.md`.

  Original entry: Severity: P1 (latent; surfaces when host adapter lands). `handleSubscribeSingle` dropped the `conn.Subscriptions.Reserve(queryID)` call that the pre-slice `handleSubscribe` used. `SendSubscribeSingleApplied` still guards delivery behind `conn.Subscriptions.IsPending(msg.QueryID)`: in production the applied envelope silently early-returns and never reaches the wire. Unit tests mask this because the test harness manually calls `conn.Subscriptions.Reserve(7)` before invoking the handler. Latent because no production host binary exercises the pipeline end-to-end in-tree (Task 10 of the Phase 2 Slice 2 plan was intentionally skipped — `protocol.ExecutorInbox` has only test-fake implementers). Surfaced in Phase 2 Slice 2 final review (branch `phase-2-slice-2-subscribe-multi`, 2026-04-19).

- **TD-137** [BUG][resolved] `protocol/send_txupdate.go`, `subscription/register_set.go` — Root-cause close on branch `phase-2-slice-2-td140-admission-model`. The C2 fan-out admission gate was first narrowly dropped in `706f18d`, and the supporting tracker state was removed structurally in the Group B admission-model slice. `validateActiveSubscriptionUpdates` is deleted. Fan-out now relies solely on `connOnlySender.Send`'s closed-channel guard plus `ErrConnNotFound` / `ErrClientBufferFull` — the same admission path that live subscriptions already use. Covered by `TestTD137_DeliverTransactionUpdateLightNoAdmissionGate` and the end-to-end register → commit → fan-out ordering tests. See ADR `docs/adr/2026-04-19-subscription-admission-model.md`.

  Original entry: Severity: P1 (latent; surfaces when host adapter lands). Pre-slice, the wire `QueryID` and internal `SubscriptionID` were the same uint32 — one tracker key. Post-slice, `SubscriptionUpdate.SubscriptionID` is manager-allocated via `m.nextSubID++` in `subscription/register_set.go`, which is NOT the wire `QueryID`. But `protocol/send_txupdate.go:57-64` still checks `conn.Subscriptions.IsActive(update.SubscriptionID)`. Even if TD-136 is fixed by restoring `Reserve(queryID)`, the Reserve stores QueryID while the check reads SubscriptionID — they disagree. Every `TransactionUpdateLight` through `DeliverTransactionUpdateLight` would be rejected. Not caught by tests because `send_txupdate_test.go` manually seeds the tracker with the exact internal ID it uses. Surfaced in Phase 2 Slice 2 final review (branch `phase-2-slice-2-subscribe-multi`, 2026-04-19).

- **TD-138** [FOLLOWUP][resolved] `executor/command.go` (`RegisterSubscriptionSetCmd`, `UnregisterSubscriptionSetCmd`) — Incidentally resolved by the Group B admission-model slice on branch `phase-2-slice-2-td140-admission-model`. Both commands now carry a `Reply` closure with signature `func(Result, error)` — the error is a distinct parameter rather than being dropped to log, so Register and Unregister are now symmetric at the delivery boundary. The separate `UnregisterSubscriptionSetResponse{Result, Err}` envelope is also gone, replaced by the same closure pattern. No further code change required.

  Original entry: `RegisterSubscriptionSetCmd` sends a zero-valued `SubscriptionSetRegisterResult` on error, logging the error but dropping it on the floor. `UnregisterSubscriptionSetCmd` uses a richer `UnregisterSubscriptionSetResponse{Result, Err}` envelope. The two should be symmetric: add `RegisterSubscriptionSetResponse{Result, Err}` so callers can distinguish error outcomes without log-scraping. Low urgency; can be taken alongside TD-139.

- **TD-139** [FOLLOWUP] `protocol/lifecycle.go` (`RegisterSubscriptionSetRequest`) — `protocol.RegisterSubscriptionSetRequest.Predicates` is typed `[]any` to avoid import cycles; the adapter casts each element to `subscription.Predicate` on the way through. A thin type alias or a minimal shared interface would restore compile-time safety without creating a cycle. Low urgency; consolidate with TD-138 if taken.

- **TD-140** [DECISION][resolved] `protocol/` subscription tracker vs `executor`/`subscription` set-registry — Decided and executed on branch `phase-2-slice-2-td140-admission-model`. Shape 1 (manager-authoritative) per ADR `docs/adr/2026-04-19-subscription-admission-model.md`: `protocol.SubscriptionTracker` is removed, `subscription.Manager.querySets` is the single admission authority, and §9.4 ordering is preserved by synchronously enqueuing the Applied envelope on `Conn.OutboundCh` inside the executor main-loop goroutine (via a `Reply` closure on `RegisterSubscriptionSetCmd` / `UnregisterSubscriptionSetCmd`). Fan-out gating on wire or internal id is removed; `validateActiveSubscriptionUpdates` is deleted. Closes TD-136 and TD-137 at root and incidentally closes TD-138 (see those entries). TD-139 (predicate-typing compile-time safety) is unrelated and remains open.

  Original entry: Two admission models currently coexist: (a) per-connection `conn.Subscriptions` tracker (`IsPending`/`IsActive` guards in `send_responses.go` and `send_txupdate.go`) and (b) executor set-registry. The Single path uses both; the Multi path bypasses the tracker. This is the root cause of TD-136 and TD-137. Pick one authoritative model and retire the other before introducing a host adapter. Options: (a) extend the tracker to understand the manager-allocated internal `SubscriptionID` (executor notifies connection on register completion), or (b) drop the tracker entirely and rely on executor set-registry + fan-out `DroppedClients()` channel for admission. Decision gates TD-136 + TD-137 fixes and any host-adapter slice.

- **TD-141** [FOLLOWUP] `protocol/client_messages.go` (`OneOffQueryMsg`) — Phase 2 Slice 1c, wire-shape parity. Reference `OneOffQuery.message_id: Box<[u8]>` (`reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs:247`) is an opaque byte array; Shunter currently carries `RequestID uint32` to stay symmetric with other request envelopes. The SQL-string flip landed in Slice 1b (2026-04-19) but the ID-shape divergence was deliberately deferred. Follow-up work: rename `RequestID` → `MessageID`, change the type to `[]byte`, update encoder/decoder (length-prefixed byte string), flip `OneOffQueryResult.RequestID` correlation, add positive-shape pin `TestPhase2Slice1COneOffQueryMessageIDBytes`. No dependency on C1/C2/F1-F3. Narrow single-slice scope.

- **TD-142** [FOLLOWUP] `query/sql/parser.go` — The minimum-viable grammar accepts only `SELECT * FROM T [WHERE col = lit (AND col = lit)*]`. Anything outside (non-`*` projection, comparison operators other than `=`, `OR`, `JOIN`, `ORDER BY`, `LIMIT`, qualified columns, aggregates, subqueries) is rejected with `ErrUnsupportedSQL`. Reference SpacetimeDB accepts broader SQL. Widen only when a pinned parity scenario demands it; each widening should land with explicit pins for the new accepted shape and the newly removed rejection case.

### C. Idiom & style smells

- **TD-066** [SMELL] `types/value.go:203-207` — `mustKind` panics on type mismatch. Acceptable for in-package programmer-error catches, but no `Try`/error-returning variant exists for callers consuming untrusted `Value`s constructed via reflection or generic code paths (e.g. SQL planner). Future callers will either swallow panics with `recover` or duplicate the kind check. Fix: add `(Value).TryAsXxx() (T, error)` accessors or `(T, bool)` ala map lookup; keep `AsXxx` as the panic-on-misuse convenience.

- **TD-067** [SMELL] `types/value.go:128-132` and `:196-201` — `NewBytes` defensively copies input, `AsBytes` also copies on every read. Two copies per round-trip is wasteful for read-mostly workloads. Fix: keep defensive copy in `NewBytes`; add `(Value).BytesView() []byte` for read-only access where caller promises not to mutate; document the contract.

- **TD-068** [SMELL] `auth/jwt.go:90,94,102,106,109` — Repeated `mc[k].(string/float64)` type assertions with discarded `ok`. Five sites; mild duplication. Fix: add `func stringClaim(m jwt.MapClaims, k string) string` and `floatClaim`. Note: `mc["exp"].(float64)` is safe in practice because `encoding/json` decodes all numbers as `float64` — leave a comment.

- **TD-069** [SMELL][resolved] `auth/mint.go` now uses a single captured `now := time.Now()` for `iat`/`exp` claim construction.

- **TD-070** [SMELL][resolved] `schema/registry.go` no longer takes or discards the dead `userTableCount` parameter; the unused seam was removed.

- **TD-071** [SMELL] `schema/validate_schema.go:36-44` — Multiple `fmt.Errorf("...")` calls without `%w` wrapping (e.g. `"reducer name must not be empty"`, `"OnConnect handler must not be nil"`, `"duplicate OnConnect registration"`). Callers can't `errors.Is/As`. Fix: define `ErrEmptyReducerName`, `ErrNilReducerHandler`, `ErrDuplicateLifecycleHandler`, etc., and wrap with `%w`.

- **TD-072** [SMELL] `schema/validate_schema.go:25-26` — `tableNamePattern.MatchString` failure is reported with `ErrEmptyTableName` even though the table name isn't empty — it's structurally invalid. Wrong sentinel. Fix: introduce `ErrInvalidTableName` and wrap.

- **TD-073** [SMELL] `schema/typemap.go:24` — `t == byteSliceType || (t.Kind() == reflect.Slice && t.Elem() == byteElemType)` — the first clause is the redundant one (named-slice types like `type X []byte` satisfy the second clause). Drop one clause or document the dual check.

- **TD-074** [SMELL] `schema/valuekind_export.go:25` — `k >= 0` is always true for a `ValueKind` derived from `int`, but `int` is signed. Negative inputs return `""` silently; schema package then builds `""` type strings into export. Fix: return `("",false)` or panic on out-of-range.

- **TD-075** [SMELL] `schema/reflect.go:53` — Comment claims "Non-struct anonymous field — fall through to normal processing", but anonymous non-struct fields will then try `f.Type` → `GoTypeToValueKind`, and `f.Name` is the type name (e.g. `int64`), which `ToSnakeCase` then mangles. Either explicitly support promoted scalar embeds with a column-name override or reject with a clear error.

- **TD-076** [SMELL][resolved] `schema/errors.go` has been `gofmt`'d and the misaligned declaration cleaned up.

- **TD-077** [SMELL][resolved] `executor/registry.go` has been `gofmt`'d and the stale struct-field padding removed.

- **TD-078** [SMELL][resolved] `executor/executor.go` now uses `capacity` instead of shadowing the predeclared `cap` builtin.

- **TD-079** [SMELL] `executor/executor.go:204` — `default: log.Printf("executor: unknown command type %T", cmd)` silently drops unknown commands. A future command with a `ResponseCh` could deadlock its caller. Should panic in dev or return an error to whatever channel the command carries.

- **TD-080** [SMELL] `executor/executor.go:60-102` — `NewExecutor` takes 5 positional parameters including a magic `recoveredTxID uint64` and panics on multiple invariants. Convert to functional options or split: `NewExecutor(cfg, deps)` plus a separate `Recover(txID)` step.

- **TD-081** [SMELL] `executor/executor.go:386-389` — `isUserCommitError` hardcodes three error sentinels. Add a `store.IsConstraintError(err) bool` upstream so this list doesn't drift.

- **TD-082** [SMELL] `executor/executor.go:392-400` — No-op fakes (`noopDurability`, `noopSubs`) live in production code. Move to a `noop.go` file or `internal/executortest`.

- **TD-083** [SMELL] `executor/executor.go:69,73` — `dur := cfg.Durability; if dur == nil { dur = noopDurability{} }` repeated for `subs`. Silently substituting a no-op for a missing durability handle makes mis-wired prod builds undetectable. Require non-nil and panic at construction; let test helpers pass explicit no-ops.

- **TD-084** [SMELL] `executor/errors.go` and exported types throughout (`ScheduleID`, `CallSource`, `ReducerStatus`, `LifecycleKind`) — `String()` method missing on every enum, so `log.Printf("status=%d")` patterns proliferate (see `scheduler_worker.go:173`). Add `Stringer` impls or use `go:generate stringer`.

- **TD-085** [SMELL] `store/transaction.go:14-17` — `Transaction` has no `Context` field. All methods are non-cancellable; a long-running transaction over a deleted table or huge scan can't be aborted. Add `ctx context.Context` per the SDK conventions.

- **TD-086** [SMELL] `store/errors.go:8-19` — Sentinel errors are declared but `transaction.go:35,143,154,159,178,183` use `fmt.Errorf("%w: %d", ErrTableNotFound, tableID)` ad hoc. Should use a `TableNotFoundError` struct (consistent with `TypeMismatchError` style already in the file).

- **TD-087** [SMELL] `commitlog/durability.go:69-88` — Function panics for programmer errors AND for runtime conditions (channel send-on-closed-channel race after close). Panicking in a library on a runtime durability failure is not idiomatic Go; return an error or expose a `Wait()`/error channel.

- **TD-088** [SMELL] `store/changeset.go:9-20` — `Changeset.TxID` is `types.TxID` but `commit.go:22` always assigns `0` and `durability.go:32` carries `txID uint64` separately. The field is dead/misleading. Either populate it in `Commit` or remove it.

- **TD-089** [SMELL] `subscription/delta_view.go:120-147` — `DeltaIndexScan` panics when the column has no built index but returns nil silently when the table has no rows. Inconsistent contract: callers cannot distinguish "no rows" from "you forgot activeColumns". Fix: return `(rows, error)` or precondition-check at construction.

- **TD-090** [SMELL] `subscription/eval.go:222-228` — `evalQuerySafe` mixes named returns with explicit `return updates, nil` while a deferred `recover` writes to the named `err`. Behavior is correct (defer-set values win) but brittle and confusing. Fix: use bare `return` so defer-set values are returned cleanly.

- **TD-091** [SMELL] `subscription/manager.go:50-51` — `memo := make(map[QueryHash]*memoizedResult)` is built and only assigned to (`memo[hash] = ...`) but never read. Either wire it into the fanout payload now or delete.

- **TD-092** [SMELL] `subscription/eval.go:42` — `_ = txID` discards a parameter the signature requires; the `FanOutMessage` uses `txID` directly. Either log/use or drop from `evaluate`'s signature.

- **TD-093** [SMELL] `subscription/register.go:35` and `register.go:73-79` — `_ = qs` after the dedup branch (unused); IIFE wrapping `iterateAll` to convert `iter.Seq2` into a slice is dead weight (`iterateAll` already returns a slice).

- **TD-094** [SMELL] `subscription/eval.go:236-239` — `evalPanic.Error()` does not include the panic cause. Use `fmt.Sprintf("subscription: evaluation panic for query %s: %v", e.hash, e.cause)`.

- **TD-095** [SMELL] `protocol/disconnect.go:38,41` — `log.Printf` inside library code; no logger injection. Fix: take a `*slog.Logger` on `Server`/`Conn` and route through it.

- **TD-096** [SMELL] `protocol/client_messages.go:241` and `protocol/compression.go:95,100` — `fmt.Errorf("%w: %v", ...)` chains the sentinel with `%v` instead of `%w`, so the underlying decode error can't be unwrapped past the sentinel. Fix: `errors.Join(ErrMalformedMessage, err)` or use `%w` for both.

- **TD-097** [SMELL] `protocol/options.go:26` — `CloseHandshakeTimeout` is defined and never read. Either wire it through into the close paths in `disconnect.go` and `keepalive.go` or remove it.

- **TD-098** [FOLLOWUP] `schema/build.go:107` — `b.built = true` is set on the caller-visible builder before the `Engine` is returned; if `newSchemaRegistry` panics, `b` is permanently sealed. Verify whether `Build()` is intended to be retryable on partial failure; if not, document the one-shot semantics; if yes, set `b.built` only on the successful `return`.

### D. Duplication patterns

- **TD-099** [DUP] **Six parallel kind ladders over `ValueKind`.** Sites: `bsatn/encode.go:29-101` (encode switch), `bsatn/decode.go:35-119` (decode switch), `types/value.go:33-47` (`kindNames`), `:217-235` (`Equal`), `:244-268` (`Compare`), `:289-312` (`writePayload`), `:316-334` (`payloadLen`), `schema/typemap.go` (Go-kind switch), `schema/valuekind_export.go` (export-string table), `schema/validate_structure.go:122` (`isValidValueKind` range check). Adding a 14th kind requires editing ~9 sites; the compiler warns on none (default arms swallow). Fix: define a single `var kindInfo = [...]struct { name string; payloadLen int; ... }` table indexed by `ValueKind`, replace all switches; add a test asserting `len(kindNames) == lastKind+1`.

- **TD-100** [DUP] **Hand-rolled little-endian wire helpers in three packages.** Sites: `bsatn/{encode,decode}.go`, `commitlog/segment.go`, `protocol/{client_messages.go:249-294, server_messages.go:329-340}`. *Internal dup within `protocol`*: `writeUint32`/`readUint32` in `client_messages.go` and `writeUint64`/`readUint64` in `server_messages.go` are the same shape. Each package ships its own `(value, off, err)` triple-return helper and bounds check, repeated ~20 times across both message files. Fix: extract an `internal/wire` package with offset-owning `Reader`/`Writer` structs.

- **TD-101** [DUP] **Three nested-hashset structures in `subscription`.** Sites: `subscription/value_index.go:24-36`, `subscription/join_edge_index.go:34-57`, `subscription/table_index.go:18-25` — three parallel set-of-hash maps with copy-pasted Add/Remove/Lookup/empty-cleanup logic. Cleanup ladder in `value_index.go:81-100` is replicated almost verbatim in `join_edge_index.go:73-89`. Fix: generic `nestedHashSet[K1, K2 comparable]` would eliminate ~150 LOC.

- **TD-102** [DUP] **Three predicate type-switch walks in `subscription`.** Sites: `subscription/delta_single.go:32-58`, `subscription/placement.go:150-182`, `subscription/eval.go:107-127`. Fix: single `Visit(pred, Visitor)` (or `iter.Seq[Predicate]`) — one place to update when new predicate kinds land.

- **TD-103** [DUP] **Three Tier-2 join-traversal loops in `subscription`.** Sites: `subscription/placement.go`, `subscription/eval.go:146-217`, `subscription/delta_join.go`. Each does LHS row → IndexSeek RHS → bounds-check `RHSFilterCol` → lookup. Fix: extract `forEachJoinMatch(view, edge, lhsRow, fn)`.

- **TD-104** [DUP] **"Commit + Rollback-on-error + assign txID + postCommit" tail duplicated.** Sites: `executor/lifecycle.go:52-62` and `executor/executor.go:293-311`. Fix: extract `commitAndPostCommit(tx, ret, ch) (txID, error)`.

- **TD-105** [DUP] **"Build CallerCtx / ReducerCtx + deferred panic recover + classify outcome" duplicated.** Sites: `executor/lifecycle.go:231-265` and `executor/executor.go:126-157`. Fix: extract `runReducer(rr, tx, caller, args) ([]byte, ReducerStatus, error)` shared between lifecycle and CallReducer pipelines.

- **TD-106** [DUP] **"Find schedule row by ID" loop duplicated.** Sites: `executor/scheduler.go:36-52` (`advanceOrDeleteSchedule`) and `executor/scheduler.go:115-127` (`Cancel`). Both share the "match by `SysScheduledColScheduleID == target`" predicate. Fix: extract `findScheduleRowID(tx, tableID, id) (rowID, row, bool)`.

- **TD-107** [DUP] **Unique-violation check duplicated across transaction and commit paths.** Sites: `store/transaction.go:42-86` (Insert constraint checks: committed unique, tx-local unique, hash-set) and `store/commit.go` (`revalidateInsertAgainstCommitted`). Two copies will drift. Fix: extract `checkUniqueAgainst(table, idx, key, txState)` helper.

- **TD-108** [DUP] **`store/snapshot.go:33-79` — five methods all `t, ok := s.cs.Table(id); if !ok { return zero }`.** Also no `s.closed` check before delegating to `s.cs` — using a closed snapshot reads from state without holding the read lock. Fix: extract `withTable[T](id, zero, fn)` helper.

- **TD-109** [DUP] **`commitlog/changeset_codec.go:38-62` — Inserts and Deletes loops are byte-for-byte identical** (count + per-row length-prefixed bytes). Fix: extract `writeRowList(buf, rows)` and `readRowList(data, ts, max)`.

- **TD-110** [DUP] **`commitlog/segment.go:75-83` and `:87-104` — `ComputeRecordCRC` and `EncodeRecord` redundantly serialize the 14-byte header twice.** Fix: shared header-bytes helper.

- **TD-111** [DUP] **`commitlog/segment.go:108-114, 128-132, 136-140` — three identical `if err == io.ErrUnexpectedEOF { return nil, ErrTruncatedRecord }` blocks.** Fix: wrap once at the top using `errors.Is`.

- **TD-112** [DUP] **`protocol/{client,server}_messages.go` — four parallel encode/decode switch ladders** for client and server messages. Fix: define `type ClientMessage interface { Tag() uint8; encodeBody(*bytes.Buffer) error }` (and matching `ServerMessage`), attach methods, drop the switches.

- **TD-113** [DUP] **`types/connection_id.go:11` and `types/identity.go:10` — `IsZero` identical except for the array length.** Three-line dup. Acceptable as-is given only two sites; flag if a third array-id type is added.

### E. Cross-cutting themes

These are not single sites but pattern-level observations that should inform refactor priority:

- **`%w` discipline is weak across the repo.** Nine `fmt.Errorf("...: %v", err)` sites that should be `%w` (or `errors.Join`). Inconsistency makes `errors.Is/As`-based dispatch impossible at higher layers (e.g. protocol mapping JWT errors to HTTP statuses).

- **`any` typing on hot APIs** (`types/reducer.go:15-16` `DB`/`Scheduler`, `executor` command default case, several protocol message decoders). Confirms TD-022 and extends. Defeats compile-time safety on the most-used surface.

- **Sentinel-error-vs-typed-error inconsistency.** `store/errors.go` and `bsatn/errors.go` use `errors.New` sentinels heavily; `schema/errors.go` mixes sentinels with `Errorf` strings; `protocol/errors.go` is mostly sentinels but key sites like `client_messages.go:241` break the chain. Pick one convention per package and document it.

- **Library code panics in three packages** (`commitlog/durability.go`, `protocol/options.go`, `types/value.go` accessor `mustKind`). At least the durability and options panics should be returned errors.

- **Six executor goroutines + two protocol pumps + one scheduler + one durability worker = nine long-lived goroutines, none of which use `sync.WaitGroup` for shutdown bookkeeping.** Shutdown ordering is documented nowhere; race-detector runs would likely surface several of the items above.

- **Subscription pkg has the largest refactor leverage**: TD-101, TD-102, TD-103 together would shrink ~500 LOC of duplication and remove the most likely future-drift source in a 2.3k-LOC package.

- **No `gofmt` gate visible.** TD-076 (`schema/errors.go`) and TD-077 (`executor/registry.go`) both indicate `gofmt -w` was not run before commit.

### F. Remediation priority

Recommended order (highest impact first):

1. **TD-027 through TD-045** (concurrency + durability) — correctness-fatal under load or restart; passing tests do not catch these
2. **TD-031, TD-032, TD-033** (commit non-atomicity, dead `Rollback`, silent delete-skip) — data-loss class
3. **TD-046 through TD-063** (logic bugs) — visible to users as wrong results or wrong errors
4. **TD-099, TD-100, TD-101** (largest dup patterns) — removing these unlocks safer future edits
5. **TD-066 through TD-098** (idiom smells) — opportunistic cleanup
6. **TD-064, TD-065, TD-098** (FOLLOWUPs) — verify before fixing

Each item should land with at least one focused test or compile-only repro before fix, per the audit method already established in this file.
