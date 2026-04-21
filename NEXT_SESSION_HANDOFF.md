# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

## What just landed (2026-04-21)

Targeted Tier-B hardening follow-through from Option α: the protocol lifecycle's outbound-writer supervision gap is now closed.

- Root cause confirmed in live code: `protocol/upgrade.go` spawned `runDispatchLoop`, `runKeepalive`, `runOutboundWriter`, and `superviseLifecycle`, but the supervisor only watched `dispatchDone` / `keepaliveDone`. If the outbound writer exited first on a write-side websocket failure from `protocol/outbound.go`, no disconnect was driven until some other goroutine happened to exit.
- Failure shape: delivery was already dead, but `ConnManager` still retained the `*Conn`, subscriptions were not reaped, and `c.closed` stayed open. That left the connection registered past the first owned goroutine exit and violated the intended ownership model for the per-connection lifecycle goroutine set.
- Fix shape: `protocol/upgrade.go` now wraps `runOutboundWriter` with `outboundDone`, and `protocol/disconnect.go::superviseLifecycle` treats `outboundDone` exactly like `dispatchDone` / `keepaliveDone`: any of the three first exits now triggers one bounded `Disconnect`, and the supervisor drains all three done channels before returning.
- New focused pin: `protocol/disconnect_test.go::TestSuperviseLifecycleInvokesDisconnectOnOutboundWriterExit` closes a synthetic `outboundDone` while the other two done channels remain open and proves the supervisor drives `Disconnect` immediately.
- Existing supervisor happy-path / bounded-ctx pins were updated, not replaced: `TestSuperviseLifecycleInvokesDisconnectOnReadPumpExit`, `TestSuperviseLifecycleBoundsDisconnectOnInboxHang`, `TestSuperviseLifecycleDeliversOnInboxOK`, and `TestClientInitiatedClose_DisconnectSequenceRuns` now include the outbound writer in the supervised goroutine set.
- New slice doc: `docs/hardening-oi-004-outbound-writer-supervision.md`.

Baseline note (2026-04-21): targeted protocol verification passed: `rtk go test ./protocol -run 'Test(SuperviseLifecycle(InvokesDisconnectOn(ReadPumpExit|OutboundWriterExit)|BoundsDisconnectOnInboxHang|DeliversOnInboxOK)|ClientInitiatedClose_DisconnectSequenceRuns)' -count=1 -v`. Broader protocol verification and repo-wide verification should be re-run after any next slice.

Flaky test note: no known clean-tree intermittent tests remain after the 2026-04-21 subscription, scheduler, and protocol lifecycle follow-through.

## Recommended next slice

The scheduler replay flake is closed. Prefer returning to the repo's grounded parity/hardening backlog instead of more test-stability cleanup.

Why this next:
- the known clean-tree intermittent tests are drained
- the remaining work is back on the real product/parity path rather than test harness noise
- Option α still has the clearest narrow hardening follow-through slices if a concrete seam surfaces

Expected shape of the next session:
1. Read the required startup docs in the listed order.
2. Pick the next grounded anchor from `TECH-DEBT.md`, `docs/spacetimedb-parity-roadmap.md`, or `docs/parity-phase0-ledger.md`.
3. Prefer a narrow Tier-B hardening slice under Option α unless live workload/reference evidence points directly to β/γ/δ.
4. If taking another scheduler slice, treat `docs/parity-p0-sched-001-startup-firing.md` as authoritative: the remaining open scheduler work is intended-time ordering, `fn_start`-clamped schedule-now, or one-shot panic deletion — not replay-map-order cleanup.
5. Re-run targeted package tests, then `rtk go test ./...`.
6. Update `docs/current-status.md` and this handoff with the new truth.

Prior closed anchors in the same calendar week (still landed, included here for continuity):
- OI-006 fanout per-subscriber slice-header aliasing sub-hazard — `docs/hardening-oi-006-fanout-aliasing.md`
- OI-005 `CommittedState.Table(id) *Table` raw-pointer contract pin — `docs/hardening-oi-005-committed-state-table-raw-pointer.md`
- OI-005 `StateView.ScanTable` iterator surface — `docs/hardening-oi-005-state-view-scan-aliasing.md`
- OI-004 dispatch-handler ctx sub-hazard — `docs/hardening-oi-004-dispatch-handler-context.md`
- OI-004 `forwardReducerResponse` ctx / Done lifecycle — `docs/hardening-oi-004-forward-reducer-response-context.md`
- OI-004 `ConnManager.CloseAll` disconnect-ctx sub-hazard — `docs/hardening-oi-004-closeall-disconnect-context.md`
- OI-004 outbound-writer supervision sub-hazard — `docs/hardening-oi-004-outbound-writer-supervision.md`
- OI-004 `superviseLifecycle` disconnect-ctx — `docs/hardening-oi-004-supervise-disconnect-context.md`
- OI-004 `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx — `docs/hardening-oi-004-sender-disconnect-context.md`
- OI-004 `watchReducerResponse` goroutine-leak escape route — `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
- OI-005 `StateView.SeekIndexRange` BTree-alias escape route — `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`
- OI-005 `StateView.SeekIndex` BTree-alias escape route — `docs/hardening-oi-005-state-view-seekindex-aliasing.md`
- OI-005 `CommittedSnapshot.IndexSeek` BTree-alias escape route — `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`
- OI-005 subscription-seam read-view lifetime sub-hazard — `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`
- OI-005 snapshot iterator mid-iter-close defense-in-depth sub-hazard — `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`
- OI-005 snapshot iterator use-after-Close sub-hazard — `docs/hardening-oi-005-snapshot-iter-useafterclose.md`
- OI-005 snapshot iterator GC retention sub-hazard — `docs/hardening-oi-005-snapshot-iter-retention.md`
- Phase 4 Slice 2 replay-horizon / validated-prefix (`P0-RECOVERY-001`) — `docs/parity-p0-recovery-001-replay-horizon.md`
- Phase 3 Slice 1 scheduled-reducer startup / firing ordering (`P0-SCHED-001`) — `docs/parity-p0-sched-001-startup-firing.md`
- Phase 2 Slice 3 lag / slow-client policy (`P0-SUBSCRIPTION-001`) — `docs/parity-phase2-slice3-lag-policy.md`

## Next realistic parity / hardening anchors

With `P0-RECOVERY-001`, `P0-SCHED-001`, `P0-SUBSCRIPTION-001` closed, all nine OI-005 enumerated sub-hazards closed (iter GC retention, iter use-after-Close, iter mid-iter-close, subscription-seam read-view lifetime, `CommittedSnapshot.IndexSeek` BTree-alias, `StateView.SeekIndex` BTree-alias, `StateView.SeekIndexRange` BTree-alias, `StateView.ScanTable` iterator surface, `CommittedState.Table(id) *Table` raw-pointer contract pin), both enumerated OI-006 sub-hazards closed (slice-header aliasing, row-payload sharing contract pin), and six OI-004 sub-hazards closed (`watchReducerResponse`, `connManagerSender.enqueueOnConn` overflow-disconnect, `superviseLifecycle` disconnect-ctx, `ConnManager.CloseAll` disconnect-ctx — closes the `Background`-rooted `Conn.Disconnect` call-site family — `forwardReducerResponse` ctx / Done lifecycle, dispatch-handler ctx), the grounded options are:

### Option α — Continue Tier-B hardening

`TECH-DEBT.md` still carries:
- OI-004 remaining sub-hazards (other detached goroutines in `protocol/conn.go` / `lifecycle.go` / `outbound.go` / `keepalive.go`; `ClientSender.Send` no-ctx follow-on)
- OI-005: enumerated sub-hazards list now empty; OI-005 remains open as a theme because the envelope rule for raw `*Table` access is enforced by discipline and observational pins rather than machine-enforced lifetime. Promoting to a narrower interface wrapper that re-checks snapshot openness on every access, or a generation-counter invalidation model on `*Table` itself, would be its own broader decision doc
- OI-006: enumerated sub-hazards list now empty; OI-006 remains open as a theme because the read-only row-payload contract is enforced by discipline and observational pins rather than machine-enforced immutability at the `types.ProductValue` boundary. Broader fanout assembly hazards in `subscription/fanout.go`, `subscription/fanout_worker.go`, and `protocol/fanout_adapter.go` stay in scope if any future path introduces in-place mutation
- OI-008 (top-level bootstrap missing)

Pick one narrow sub-hazard and land a narrow fix with a focused test, following the shape of `docs/hardening-oi-006-row-payload-sharing.md` (latest; contract-pin precedent at a row-payload sharing seam with observational identity+mutation-leak pins), `docs/hardening-oi-005-committed-state-table-raw-pointer.md` (prior contract-pin precedent at a raw-pointer seam), `docs/hardening-oi-005-state-view-scan-aliasing.md` (materialization precedent), `docs/hardening-oi-004-dispatch-handler-context.md` (derived-ctx lifecycle wire precedent), or `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md` (earlier contract-pin precedent). Concrete candidates:
- OI-004 `ClientSender.Send` no-ctx follow-on: `protocol/sender.go` sends are synchronous without their own ctx, so callers cannot propagate a shorter cancellation scope than `DisconnectTimeout` into the overflow path. No concrete consumer needs this today; defer until a specific seam surfaces.
- OI-004 remaining detached-goroutine audit in `protocol/conn.go` / `lifecycle.go` / `keepalive.go`: each would be its own narrow sub-slice if a specific leak site surfaces. The dispatch-loop and disconnect paths are now audited and pinned; the outbound-writer supervision gap is also closed. `closeWithHandshake` fire-and-forget goroutines at `keepalive.go:77`, `disconnect.go:49`, `dispatch.go:46`, and `dispatch.go:188` are already bounded via `context.WithTimeout(context.Background(), CloseHandshakeTimeout)` inside `closeWithHandshake` at `close.go:25-29` — those are not open sub-hazards.
- OI-008 top-level bootstrap: larger-scope work; would need its own decision doc and parity alignment (no `cmd/` entrypoint, no polished embedding surface, no `main` package).
- scheduler replay-map-order cleanup is now closed: the brittle map-iteration-sensitive parity pin was replaced with deterministic helper-level coverage (`TestParityP0Sched001ReplayPreservesScanOrderWithoutSorting`) without changing live replay sorting semantics
- Flaky `subscription/TestProjectedRowsBeforeAppendsDeletesAfterBagSubtraction` cleanup: refactor the seed to a single-row table so the map-iteration ordering is not exercised, or add deterministic sorting inside `projectedRowsBefore` (semantic change — the function returns rows in `current ++ tx-deletes` order, and sorting would change the observed order for every caller).

### Option β — Broader SQL/query-surface parity beyond TD-142

TD-142 drained the named narrow slices. Widening beyond those is new parity work. Surfaces: `query/sql/parser.go`, `subscription/predicate.go`, `protocol/handle_subscribe_{single,multi}.go`.

### Option γ — Format-level commitlog parity (Phase 4 Slice 2 follow-on)

With the replay-horizon / validated-prefix slice closed, the remaining commitlog parity work is format-level:
- offset index file (reference `src/index/indexfile.rs`, `src/index/mod.rs`)
- record / log shape compatibility (reference `src/commit.rs`, `src/payload/txdata.rs`)
- typed `error::Traversal` / `error::Open` enums
- snapshot / compaction visibility vs reference `repo::resume_segment_writer` contract

These are larger scope than a single narrow slice; each would need its own decision doc.

### Option δ — Pick one of the `P0-SCHED-001` deferrals

Each remaining scheduler deferral is a candidate for its own focused slice if workload evidence surfaces:
- `fn_start`-clamped schedule "now" (plumb reducer dispatch timestamp into `schedulerHandle`; ref `scheduler.rs:211-215`)
- one-shot panic deletion (second-commit post-rollback path; ref `scheduler.rs:445-455`)
- past-due ordering by intended time (sort in `scanAndTrackMaxWithContext`)

Prefer Option α over β/γ/δ unless workload or reference evidence directly surfaces a specific gap.

## First, what you are walking into

The repo already has substantial implementation. Do not treat this as a docs-only project. Do not do a broad audit. Do not restart parity analysis from zero.

Your job is to continue from the current live state. Pick the next grounded anchor from `docs/spacetimedb-parity-roadmap.md`, `docs/parity-phase0-ledger.md`, or `TECH-DEBT.md`.

Clean-room reminder:
- parity target means matching externally meaningful behavior where required, not translating Rust source into Go
- `reference/SpacetimeDB/` stays research-only and read-only; do not copy, transliterate, or mechanically port code from it
- re-derive behavior from public docs, reference outcomes, and live Shunter contracts, then implement natively in Go

## Mandatory reading order

1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md`
8. `docs/parity-phase0-ledger.md`
9. `TECH-DEBT.md`
10. `docs/hardening-oi-006-row-payload-sharing.md` (latest closed slice — contract pin at the row-payload sharing seam with identity + mutation-leak observational pins; closes the second enumerated OI-006 sub-hazard)
11. `docs/hardening-oi-006-fanout-aliasing.md` (prior OI-006 sub-slice — slice-header isolation precedent; the 2026-04-21 slice is its complement)
12. `docs/hardening-oi-005-committed-state-table-raw-pointer.md` (prior OI-005 contract-pin precedent at a raw-pointer seam without a direct materialization option; closes the last enumerated OI-005 sub-hazard)
13. `docs/hardening-oi-005-state-view-scan-aliasing.md` (prior OI-005 sub-slice — `StateView.ScanTable` materialization precedent)
14. `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md` (prior-same-family OI-005 sub-slice — direct materialization precedent at the `StateView` boundary)
15. `docs/hardening-oi-005-state-view-seekindex-aliasing.md` (prior OI-005 sub-slice — precedent)
16. `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md` (prior OI-005 sub-slice — precedent)
17. `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md` (prior OI-005 sub-slice — earlier contract-pin precedent for seams without a materialization option)
18. `docs/hardening-oi-004-dispatch-handler-context.md` (prior OI-004 sub-slice — derived-ctx lifecycle wire precedent)
19. `docs/hardening-oi-004-forward-reducer-response-context.md` (prior OI-004 sub-slice — Done-channel lifecycle signal pattern)
20. `docs/hardening-oi-004-closeall-disconnect-context.md` (prior OI-004 sub-slice — bounded-ctx precedent)
21. `docs/hardening-oi-004-supervise-disconnect-context.md` (prior OI-004 sub-slice)
22. `docs/hardening-oi-004-sender-disconnect-context.md` (prior OI-004 sub-slice)
23. `docs/hardening-oi-004-watch-reducer-response-lifecycle.md` (prior OI-004 sub-slice)
24. `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md` (prior OI-005 sub-slice — precedent)
25. `docs/hardening-oi-005-snapshot-iter-useafterclose.md` (prior OI-005 sub-slice — precedent)
26. `docs/hardening-oi-005-snapshot-iter-retention.md` (earlier OI-005 sub-slice — additional precedent)
27. `docs/parity-p0-recovery-001-replay-horizon.md` (prior-closed parity slice — precedent for a narrow-and-pin parity decision doc)
28. `docs/parity-p0-sched-001-startup-firing.md` (prior-closed parity slice — alternative precedent)
29. `docs/parity-phase2-slice3-lag-policy.md` (earlier-closed parity slice — another precedent)
30. the specific code surfaces for whichever anchor (α/β/γ/δ) you pick

## Shell discipline

Use `rtk` for shell commands. Examples:
- `rtk git status --short --branch`
- `rtk go test ./store -run 'TestName' -v`
- `rtk go test ./...`

## Important repo note

Keep `.hermes/plans/2026-04-18_073534-phase1-wire-level-parity.md` unless you deliberately update the contract that depends on it. A test expects it.

## What is already landed (do not reopen)

- Protocol conformance P0-PROTOCOL-001..004
- Delivery parity P0-DELIVERY-001..002
- Recovery invariant P0-RECOVERY-002
- TD-142 Slices 1–14 (all narrow SQL parity shapes, including join projection emitted onto the SELECT side)
- Phase 1.5 outcome model + caller metadata wiring
- Phase 2 Slice 3 lag / slow-client policy (2026-04-20) — `P0-SUBSCRIPTION-001`
- Phase 3 Slice 1 scheduled reducer startup / firing ordering (2026-04-20) — `P0-SCHED-001`
- Phase 4 Slice 2 replay-horizon / validated-prefix behavior (2026-04-20) — `P0-RECOVERY-001`
- OI-005 snapshot iterator GC retention sub-hazard (2026-04-20)
- OI-005 snapshot iterator use-after-Close sub-hazard (2026-04-20)
- OI-006 fanout per-subscriber slice-header aliasing sub-hazard (2026-04-20)
- OI-005 snapshot iterator mid-iter-close defense-in-depth sub-hazard (2026-04-20)
- OI-005 subscription-seam read-view lifetime sub-hazard (2026-04-20)
- OI-005 `CommittedSnapshot.IndexSeek` BTree-alias escape route (2026-04-20)
- OI-004 `watchReducerResponse` goroutine-leak escape route (2026-04-20)
- OI-005 `StateView.SeekIndex` BTree-alias escape route (2026-04-20)
- OI-005 `StateView.SeekIndexRange` BTree-alias escape route (2026-04-20)
- P1-07 executor response-channel contract + protocol-forwarding cancel-safe + Submit-time validation (2026-04-20, landed in commit `40b2152 baseline`)
- OI-004 `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx sub-hazard (2026-04-21)
- OI-004 `superviseLifecycle` disconnect-ctx sub-hazard (2026-04-21)
- OI-004 `ConnManager.CloseAll` disconnect-ctx sub-hazard (2026-04-21) — closes the `Background`-rooted `Conn.Disconnect` call-site family
- OI-004 `forwardReducerResponse` ctx / Done lifecycle sub-hazard (2026-04-21) — closes the executor-adapter twin of the earlier protocol-side `watchReducerResponse` leak
- OI-004 dispatch-handler ctx sub-hazard (2026-04-21) — request-side analog to `forwardReducerResponse`
- OI-005 `StateView.ScanTable` iterator surface (2026-04-21) — `StateView.ScanTable` now pre-collects committed rows into an `[]entry{id, row}` slice before entering the yield loop; closes the last `StateView` iter-surface escape route alongside the earlier `SeekIndex` / `SeekIndexRange` closures
- OI-005 `CommittedState.Table(id) *Table` raw-pointer contract pin (2026-04-21) — contract comments on `CommittedState.Table` and `CommittedState.TableIDs` document the three legal envelopes (`CommittedSnapshot` RLock lifetime, executor single-writer discipline, commitlog recovery bootstrap) and the three hazards (escape past envelope, stale-after-re-register, non-executor-goroutine read without RLock); three pin tests assert pointer identity, stale-after-re-register hazard shape, and snapshot RLock lifetime. Closes the last enumerated OI-005 sub-hazard
- **OI-006 row-payload sharing contract pin (2026-04-21)** — contract comments on `subscription/eval.go::evaluate` per-subscriber fanout loop, `subscription/fanout_worker.go::FanOutSender`, and `protocol/fanout_adapter.go::encodeRows` document the post-commit row-immutability contract and enumerate the three hazards the read-only discipline prevents (in-place Value mutation on any downstream path, ProductValue append-within-shared-cap followed by tail mutation, store-side mutation of already-committed rows); two pin tests in `subscription/eval_fanout_row_payload_sharing_test.go` assert `[]Value` backing-array identity across subscribers for `Inserts` / `Deletes` and the in-place-mutation-leak hazard shape. Closes the second enumerated OI-006 sub-hazard

## Suggested verification commands

Targeted:
- `rtk go test ./subscription -run 'TestEvalFanoutRowPayloadsSharedAcrossSubscribers' -race -count=3 -v`
- `rtk go test ./subscription -run 'TestEvalFanout' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedStateTable' -race -count=3 -v`
- `rtk go test ./store -run 'TestStateViewScanTableIteratesIndependentOfMidIterCommittedDelete' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestDispatchLoop_HandlerCtx' -race -count=3 -v`
- `rtk go test ./executor -run 'TestProtocolInboxAdapter_ForwardReducerResponse' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestCloseAll' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestSuperviseLifecycle' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestEnqueueOnConnOverflowDisconnect' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestWatchReducerResponse' -race -count=3 -v`
- `rtk go test ./store -run 'TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation' -race -count=3 -v`
- `rtk go test ./store -run 'TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedSnapshotIndexSeekReturnsIndependentSlice' -race -count=3 -v`
- `rtk go test ./subscription -run 'TestEvalAndBroadcastDoesNotUseViewAfterReturn' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedSnapshot(TableScan|IndexRange|RowsFromRowIDs)PanicsOnMidIterClose' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedSnapshot(TableScan|IndexScan|IndexRange)PanicsAfterClose' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration' -race -count=3 -v`
- `rtk go test ./...`

## Acceptance gate

Do not call the work done unless all are true:

- reference-backed or debt-anchored target shape was checked directly against reference material or current live code
- every newly accepted or rejected shape has focused tests
- already-landed parity pins still pass (including `TestEvalFanoutRowPayloadsSharedAcrossSubscribersForInserts`, `TestEvalFanoutRowPayloadsSharedAcrossSubscribersForDeletes`, `TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers`, `TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers`, `TestCommittedStateTableSameEnvelopeReturnsSamePointer`, `TestCommittedStateTableRetainedPointerIsStaleAfterReRegister`, `TestCommittedStateTableSnapshotEnvelopeHoldsRLockUntilClose`, `TestStateViewScanTableIteratesIndependentOfMidIterCommittedDelete`, `TestDispatchLoop_HandlerCtxCancelsOnConnClose`, `TestDispatchLoop_HandlerCtxCancelsOnOuterCtx`, `TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneWhenRespChHangs`, `TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnReqDoneAlreadyClosed`, `TestProtocolInboxAdapter_ForwardReducerResponse_ExitsOnContextCancelWhenOutboundBlocked`, `TestCloseAllBoundsDisconnectOnInboxHang`, `TestCloseAllDeliversOnInboxOK`, `TestCloseAll_DisconnectsEveryConnection`, `TestCloseAll_EmptyManagerNoOp`, `TestSuperviseLifecycleBoundsDisconnectOnInboxHang`, `TestSuperviseLifecycleDeliversOnInboxOK`, `TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang`, `TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK`, `TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`, `TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation`, `TestWatchReducerResponseExitsOnConnClose`, `TestWatchReducerResponseDeliversOnRespCh`, `TestWatchReducerResponseExitsOnRespChClose`, `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnInsert`, `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnRemove`, `TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join`, `TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable`, `TestCommittedSnapshotTableScanPanicsOnMidIterClose`, `TestCommittedSnapshotIndexRangePanicsOnMidIterClose`, `TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose`, `TestCommittedSnapshotTableScanPanicsAfterClose`, `TestCommittedSnapshotIndexScanPanicsAfterClose`, `TestCommittedSnapshotIndexRangePanicsAfterClose`, `TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`, `TestParityP0Recovery001SegmentSkipDoesNotOpenExhaustedSegment`, `TestParityP0Sched001PanicRetainsScheduledRow`, `TestPhase2Slice3DefaultOutgoingBufferMatchesReference`, and `TestSuperviseLifecycleInvokesDisconnectOnReadPumpExit`).
- full suite still passes. Clean-tree baseline remains `Go test: 1157 passed in 10 packages`. No known clean-tree intermittent test remains after the 2026-04-21 flake cleanup follow-through.
- docs and handoff reflect the new truth exactly

## Deliverables for the next session

Either:
- code + tests closing the next reference-backed parity slice or Tier-B hardening sub-hazard

Or:
- a grounded blocker report naming the exact representation/runtime issue preventing a narrow landing

And in either case:
- update `TECH-DEBT.md` if any OI changes state
- update `docs/current-status.md`
- update `docs/parity-phase0-ledger.md` if a parity scenario moves
- update `NEXT_SESSION_HANDOFF.md`

## Final status snapshot right now

As of this handoff:
- `TD-142` fully drained
- Phase 2 Slice 3 closed — per-client outbound queue aligned to reference `CLIENT_CHANNEL_CAPACITY`; close-frame mechanism retained as intentional divergence
- Phase 3 Slice 1 closed — `P0-SCHED-001` scheduled-reducer startup / firing ordering narrow-and-pinned
- Phase 4 Slice 2 closed — `P0-RECOVERY-001` replay-horizon / validated-prefix behavior narrow-and-pinned
- P1-07 executor response-channel contract + protocol-forwarding cancel-safe + Submit-time validation landed in commit `40b2152 baseline`
- OI-005 iterator-GC retention sub-hazard closed
- OI-005 iterator use-after-Close sub-hazard closed
- OI-005 iterator mid-iter-close defense-in-depth sub-hazard closed
- OI-005 subscription-seam read-view lifetime sub-hazard closed
- OI-005 `CommittedSnapshot.IndexSeek` BTree-alias escape route closed
- OI-005 `StateView.SeekIndex` BTree-alias escape route closed
- OI-005 `StateView.SeekIndexRange` BTree-alias escape route closed
- OI-005 `StateView.ScanTable` iterator surface closed — last `StateView` iter-surface escape route
- OI-005 `CommittedState.Table(id) *Table` raw-pointer contract pin closed — last enumerated OI-005 sub-hazard
- OI-006 fanout per-subscriber slice-header aliasing sub-hazard closed
- **OI-006 row-payload sharing contract pin closed** — contract comments on `subscription/eval.go::evaluate`, `subscription/fanout_worker.go::FanOutSender`, and `protocol/fanout_adapter.go::encodeRows` name the post-commit row-immutability contract and the read-only downstream discipline; two pin tests assert backing-array identity across subscribers and the in-place-mutation-leak hazard shape. Closes the second enumerated OI-006 sub-hazard; OI-006's remaining-sub-hazards list is now "broader fanout assembly hazards if any future path introduces in-place mutation". OI-006 stays open as a theme because the read-only contract is enforced by discipline and observational pins rather than machine-enforced immutability
- OI-004 `watchReducerResponse` goroutine-leak sub-hazard closed
- OI-004 `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx sub-hazard closed
- OI-004 `superviseLifecycle` disconnect-ctx sub-hazard closed
- OI-004 `ConnManager.CloseAll` disconnect-ctx sub-hazard closed — closes the `Background`-rooted `Conn.Disconnect` call-site family (supervisor, sender overflow, CloseAll now all derive a bounded ctx at the spawn point)
- OI-004 outbound-writer supervision sub-hazard closed — supervisor now watches `outboundDone` alongside dispatch/keepalive and disconnects delivery-dead conns promptly
- OI-004 `forwardReducerResponse` ctx / Done lifecycle sub-hazard closed — closes the executor-adapter twin of the earlier protocol-side `watchReducerResponse` leak
- OI-004 dispatch-handler ctx sub-hazard closed — `runDispatchLoop` now derives a `handlerCtx` that cancels on `c.closed`
- Other detached-goroutine surfaces in `conn.go` / `lifecycle.go` / `keepalive.go` and the `ClientSender.Send` no-ctx follow-on remain open under OI-004
- next realistic anchors: further Tier-B hardening (α), broader SQL parity (β), format-level commitlog parity (γ), individual scheduler deferrals (δ)
- targeted flaky-test cleanup in `subscription/delta_pool_test.go`, `subscription/eval_projected_rows_test.go`, and scheduler replay parity coverage is now closed; no known clean-tree intermittent test remains
- 10 packages, clean-tree full-suite baseline `Go test: 1157 passed in 10 packages`
