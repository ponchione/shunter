# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

## What just landed (2026-04-21)

Tier-B hardening narrow sub-slice of `OI-004`: `Conn.superviseLifecycle` disconnect-ctx sub-hazard closed.

- Decision doc: `docs/hardening-oi-004-supervise-disconnect-context.md`
- Sharp edge: `protocol/disconnect.go::superviseLifecycle` received `context.Background()` hardcoded by the only production call site (`protocol/upgrade.go:211`) and forwarded it directly into `c.Disconnect(ctx, ...)`. `Conn.Disconnect` threads the ctx into `inbox.DisconnectClientSubscriptions` and `inbox.OnDisconnect` at steps 1-2 of the SPEC-005 §5.3 teardown; both honor ctx cancellation via the adapter's select arm in `executor/protocol_inbox_adapter.go:58-63` and `awaitReducerStatus` at `executor/protocol_inbox_adapter.go:133-145`. With a Background root, any hung inbox call — executor dispatch deadlock, inbox-drain stall, executor crash waiting on never-fed respCh, `DisconnectClientSubscriptions` holding an internal lock against a wedged scheduler — pinned the supervisor goroutine. `closeOnce.Do` had latched but the body never reached `close(c.closed)`, so `runDispatchLoop`, `runKeepalive`, and the write loop for that conn could not exit either. Exact same hazard class as the `connManagerSender.enqueueOnConn` overflow site closed earlier the same day (`docs/hardening-oi-004-sender-disconnect-context.md`), at the supervisor boundary instead of the overflow-teardown boundary.
- Fix: bounded ctx. `superviseLifecycle` now derives `context.WithTimeout(ctx, c.opts.DisconnectTimeout)` with `defer cancel()` after selecting on `dispatchDone` / `keepaliveDone`, then forwards that ctx into `Conn.Disconnect`. Reuses the existing `ProtocolOptions.DisconnectTimeout` field (5 s default) from the sender-disconnect slice — no new option, no new default. Happy path unchanged: inbox returns promptly, supervisor exits, deferred `cancel` fires. Hang path: after `DisconnectTimeout` the inbox returns `ctx.Err()`; `Conn.Disconnect` logs the error (disconnect cannot be vetoed per SPEC-003 §10.4) and proceeds to steps 3-5 unconditionally — `mgr.Remove` + `cancelRead` + `close(c.closed)` + optional `closeWithHandshake` — so the `*Conn` becomes collectible.
- New pins: `protocol/supervise_disconnect_timeout_test.go::{TestSuperviseLifecycleBoundsDisconnectOnInboxHang, TestSuperviseLifecycleDeliversOnInboxOK}`. Hang pin reuses the `blockingInbox` helper from `protocol/sender_disconnect_timeout_test.go`, sets `DisconnectTimeout = 150ms`, closes `dispatchDone`, waits for the supervisor to hit the inbox, and asserts supervisor exit within `DisconnectTimeout + 1 s` slack with `conn.closed` fired, `mgr.Get == nil`, and both `DisconnectClientSubscriptions` + `OnDisconnect` counts == 1 (teardown proceeded through step 2 after step 1's ctx-bounded return). Happy-path pin uses a non-blocking `fakeInbox` and asserts completion well under `DisconnectTimeout`. Both run green under `-race -count=3`.
- `TECH-DEBT.md` OI-004 updated: sub-hazard closed with pin anchors; remaining OI-004 sub-hazards narrowed to other detached-goroutine sites in `conn.go` / `lifecycle.go` / `outbound.go` / `keepalive.go`, the `ClientSender.Send` no-ctx follow-on, and the `ConnManager.CloseAll` caller-contract follow-on.

Baseline note (2026-04-21): clean-tree full-suite was `Go test: 1142 passed in 10 packages` before this slice; after this slice's two new pins the baseline is `Go test: 1144 passed in 10 packages` (protocol package went from 268 to 270 tests).

Flaky test note: `executor/TestParityP0Sched001ReplayEnqueuesByIterationOrder` depends on Go map iteration order matching RowID insertion order, which is not guaranteed. Failure is pre-existing and unrelated to this slice. Worth a dedicated slice to either sort enqueues by `(next_run_at_ns, schedule_id)` or refactor the seed to avoid map-iteration dependence.

Prior closed anchors in the same calendar week (still landed, included here for continuity):
- OI-004 `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx — `docs/hardening-oi-004-sender-disconnect-context.md`
- OI-004 `watchReducerResponse` goroutine-leak escape route — `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
- OI-005 `StateView.SeekIndexRange` BTree-alias escape route — `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`
- OI-005 `StateView.SeekIndex` BTree-alias escape route — `docs/hardening-oi-005-state-view-seekindex-aliasing.md`
- OI-005 `CommittedSnapshot.IndexSeek` BTree-alias escape route — `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`
- OI-005 subscription-seam read-view lifetime sub-hazard — `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`
- OI-005 snapshot iterator mid-iter-close defense-in-depth sub-hazard — `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`
- OI-006 fanout per-subscriber slice-header aliasing sub-hazard — `docs/hardening-oi-006-fanout-aliasing.md`
- OI-005 snapshot iterator use-after-Close sub-hazard — `docs/hardening-oi-005-snapshot-iter-useafterclose.md`
- OI-005 snapshot iterator GC retention sub-hazard — `docs/hardening-oi-005-snapshot-iter-retention.md`
- Phase 4 Slice 2 replay-horizon / validated-prefix (`P0-RECOVERY-001`) — `docs/parity-p0-recovery-001-replay-horizon.md`
- Phase 3 Slice 1 scheduled-reducer startup / firing ordering (`P0-SCHED-001`) — `docs/parity-p0-sched-001-startup-firing.md`
- Phase 2 Slice 3 lag / slow-client policy (`P0-SUBSCRIPTION-001`) — `docs/parity-phase2-slice3-lag-policy.md`

## Next realistic parity / hardening anchors

With `P0-RECOVERY-001`, `P0-SCHED-001`, `P0-SUBSCRIPTION-001` closed, seven OI-005 sub-hazards closed (iter GC retention, iter use-after-Close, iter mid-iter-close, subscription-seam read-view lifetime, `CommittedSnapshot.IndexSeek` BTree-alias, `StateView.SeekIndex` BTree-alias, `StateView.SeekIndexRange` BTree-alias), the OI-006 slice-header aliasing sub-hazard closed, and three OI-004 sub-hazards closed (`watchReducerResponse` goroutine-leak, `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx, `superviseLifecycle` disconnect-ctx), the grounded options are:

### Option α — Continue Tier-B hardening

`TECH-DEBT.md` still carries:
- OI-004 remaining sub-hazards (other detached goroutines in `protocol/conn.go` / `lifecycle.go` / `outbound.go` / `keepalive.go`; `ClientSender.Send` no-ctx follow-on; `ConnManager.CloseAll` caller-contract follow-on)
- OI-005 remaining sub-hazards (`state_view.go` / `committed_state.go` escape routes beyond `IndexSeek`, `StateView.SeekIndex`, and `StateView.SeekIndexRange`: `CommittedState.Table(id) *Table` raw-pointer exposure, `StateView.ScanTable` iterator surface)
- OI-006 remaining sub-hazards (row-payload sharing under the post-commit row-immutability contract; broader fanout assembly hazards in `subscription/fanout.go`, `subscription/fanout_worker.go`, `protocol/fanout_adapter.go` if any future path introduces in-place mutation)
- OI-008 (top-level bootstrap missing)

Pick one narrow sub-hazard and land a narrow fix with a focused test, following the shape of `docs/hardening-oi-004-supervise-disconnect-context.md` (latest) or `docs/hardening-oi-004-sender-disconnect-context.md` (direct precedent). Concrete candidates:
- OI-004 `ConnManager.CloseAll` caller-contract pin: `protocol/conn.go:137-154` forwards the caller's ctx straight into `Conn.Disconnect` per-connection, so engine shutdown is fully at the mercy of whoever supplies that ctx. Today the only callers are the server lifecycle (well-behaved) and tests, but the contract is unpinned. A narrow slice: either (a) add an internal bounded-ctx derive mirroring the overflow / supervisor fix, or (b) pin the caller-contract via a focused test that a `context.Background()` caller does NOT leak the `*Conn` (fails loud if behavior changes without intent). Option (a) is the tighter defence but couples CloseAll to `DisconnectTimeout`; option (b) is contract-only and matches the `subscription/eval_view_lifetime_test.go` precedent.
- OI-005 `CommittedState.Table(id) *Table` raw-pointer escape: the method RLocks / RUnlocks and returns a raw `*Table` pointer whose internal `rows` map and indexes are mutated only under the `CommittedState` write lock. Today's callers (`CommittedSnapshot` methods hold the snapshot RLock for the use window; `StateView` accepts no lock but operates under the executor single-writer discipline; tests hold the pointer under test ownership) are safe, but the raw pointer itself is a separate escape surface. A narrow slice: either (a) return a narrower interface wrapper that re-checks snapshot openness on every access, or (b) document the contract explicitly and pin a test that a `*Table` pointer obtained outside the snapshot envelope is never used for reads. Option (b) is narrower and follows the subscription-seam pin precedent.
- OI-005 `StateView.ScanTable` iterator surface: reaches `sv.committed.Table(...)` through the raw `*Table` pointer. `StateView` is used inside reducer execution under the executor single-writer discipline, so today's pattern is safe, but the iterator closure captures `sv.committed` / `sv.tx` and yields rows without any `KeepAlive` / lock check. Row payloads are already `Copy()`d at `table.go:126`, but the outer map iteration walks `t.rows` live. If a caller retained the iterator across a commit boundary (currently forbidden by the executor's serialization, but unpinned), Go's concurrent-map-write detector would race. Note: `StateView.SeekIndex` and `StateView.SeekIndexRange` were both closed 2026-04-20 via `slices.Clone` / `slices.Collect` at the seek boundary — `ScanTable` has no materialized slice alias, but still has the single-writer-contract lifetime class; a narrow sub-slice would pin the contract via a focused test in the shape of `hardening-oi-005-subscription-seam-read-view-lifetime.md`.
- OI-004 dispatch-handler ctx: `protocol/dispatch.go:168` spawns handler goroutines with the `ctx` received by `runDispatchLoop`, which is hardcoded to `context.Background()` at `protocol/upgrade.go:201`. Handlers like `handleCallReducer` / `handleSubscribeSingle` forward that ctx into `inbox.CallReducer` / `inbox.RegisterSubscriptionSet` / `inbox.UnregisterSubscriptionSet`. Different hang class from the disconnect path — the inbox contract here is request/response rather than teardown — but the same "Background-rooted caller" shape. A narrow slice would audit whether any handler call forwards ctx into an executor seam that can block unboundedly and, if so, derive a per-request timeout.
- Flaky `executor/TestParityP0Sched001ReplayEnqueuesByIterationOrder` cleanup: replace the map-iteration-order contract with a deterministic one (sort by `(next_run_at_ns, schedule_id)`; or refactor the seed so only one ordering is observed). This is borderline — the test was pinned as "iteration-order semantics" intentionally, so touching it requires re-anchoring to reference behavior; reference uses `DelayQueue` which is not strictly sorted either.

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
10. `docs/hardening-oi-004-supervise-disconnect-context.md` (latest closed slice — recent precedent for a Tier-B hardening decision doc + pin on a protocol-lifecycle goroutine-ownership surface)
11. `docs/hardening-oi-004-sender-disconnect-context.md` (prior OI-004 sub-slice — direct precedent for the supervise-disconnect-ctx analog just landed)
12. `docs/hardening-oi-004-watch-reducer-response-lifecycle.md` (earlier OI-004 sub-slice)
13. `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md` (prior OI-005 sub-slice — slice-aliasing precedent)
14. `docs/hardening-oi-005-state-view-seekindex-aliasing.md` (prior OI-005 sub-slice — precedent)
15. `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md` (prior OI-005 sub-slice — precedent)
16. `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md` (prior OI-005 sub-slice — contract-pin precedent)
17. `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md` (prior OI-005 sub-slice — precedent)
18. `docs/hardening-oi-006-fanout-aliasing.md` (prior closed Tier-B slice — precedent)
19. `docs/hardening-oi-005-snapshot-iter-useafterclose.md` (prior OI-005 sub-slice — precedent)
20. `docs/hardening-oi-005-snapshot-iter-retention.md` (earlier OI-005 sub-slice — additional precedent)
21. `docs/parity-p0-recovery-001-replay-horizon.md` (prior-closed parity slice — precedent for a narrow-and-pin parity decision doc)
22. `docs/parity-p0-sched-001-startup-firing.md` (prior-closed parity slice — alternative precedent)
23. `docs/parity-phase2-slice3-lag-policy.md` (earlier-closed parity slice — another precedent)
24. the specific code surfaces for whichever anchor (α/β/γ/δ) you pick

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
- **OI-004 `superviseLifecycle` disconnect-ctx sub-hazard (2026-04-21)**

## Suggested verification commands

Targeted:
- `rtk go test ./protocol -run 'TestSuperviseLifecycle' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestEnqueueOnConnOverflowDisconnect' -race -count=3 -v`
- `rtk go test ./protocol -run 'TestWatchReducerResponse' -race -count=3 -v`
- `rtk go test ./store -run 'TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation' -race -count=3 -v`
- `rtk go test ./store -run 'TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedSnapshotIndexSeekReturnsIndependentSlice' -race -count=3 -v`
- `rtk go test ./subscription -run 'TestEvalAndBroadcastDoesNotUseViewAfterReturn' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedSnapshot(TableScan|IndexRange|RowsFromRowIDs)PanicsOnMidIterClose' -race -count=3 -v`
- `rtk go test ./subscription -run 'TestEvalFanout(Inserts|Deletes)HeaderIsolatedAcrossSubscribers' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedSnapshot(TableScan|IndexScan|IndexRange)PanicsAfterClose' -race -count=3 -v`
- `rtk go test ./store -run 'TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration' -race -count=3 -v`
- `rtk go test ./...`

## Acceptance gate

Do not call the work done unless all are true:

- reference-backed or debt-anchored target shape was checked directly against reference material or current live code
- every newly accepted or rejected shape has focused tests
- already-landed parity pins still pass (including `TestSuperviseLifecycleBoundsDisconnectOnInboxHang`, `TestSuperviseLifecycleDeliversOnInboxOK`, `TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang`, `TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK`, `TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`, `TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation`, `TestWatchReducerResponseExitsOnConnClose`, `TestWatchReducerResponseDeliversOnRespCh`, `TestWatchReducerResponseExitsOnRespChClose`, `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnInsert`, `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnRemove`, `TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join`, `TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable`, `TestCommittedSnapshotTableScanPanicsOnMidIterClose`, `TestCommittedSnapshotIndexRangePanicsOnMidIterClose`, `TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose`, `TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers`, `TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers`, `TestCommittedSnapshotTableScanPanicsAfterClose`, `TestCommittedSnapshotIndexScanPanicsAfterClose`, `TestCommittedSnapshotIndexRangePanicsAfterClose`, `TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`, `TestParityP0Recovery001SegmentSkipDoesNotOpenExhaustedSegment`, `TestParityP0Sched001PanicRetainsScheduledRow`, `TestPhase2Slice3DefaultOutgoingBufferMatchesReference`, and `TestSuperviseLifecycleInvokesDisconnectOnReadPumpExit`). Note: `TestParityP0Sched001ReplayEnqueuesByIterationOrder` is intermittently flaky on a clean tree (pre-existing, map-iteration-order dependent) — do not treat a single-run failure there as caused by your slice.
- full suite still passes. Clean-tree baseline before this slice: `Go test: 1142 passed in 10 packages`. After this slice's 2 new pins: `Go test: 1144 passed in 10 packages` (protocol package from 268 → 270).
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
- OI-006 fanout per-subscriber slice-header aliasing sub-hazard closed; row-payload sharing and broader fanout assembly hazards stay open with enumerated sub-hazards
- OI-004 `watchReducerResponse` goroutine-leak sub-hazard closed
- OI-005 `StateView.SeekIndex` BTree-alias escape route closed
- OI-005 `StateView.SeekIndexRange` BTree-alias escape route closed; `CommittedState.Table(id) *Table` raw-pointer exposure and `StateView.ScanTable` iterator surface remain open under OI-005
- OI-004 `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx sub-hazard closed
- OI-004 `superviseLifecycle` disconnect-ctx sub-hazard closed; other detached-goroutine surfaces in `conn.go` / `lifecycle.go` / `outbound.go` / `keepalive.go`, the `ClientSender.Send` no-ctx follow-on, and the `ConnManager.CloseAll` caller-contract follow-on remain open under OI-004
- next realistic anchors: further Tier-B hardening (α), broader SQL parity (β), format-level commitlog parity (γ), individual scheduler deferrals (δ)
- 10 packages, `rtk go test ./protocol` 270/270 passing after this slice; clean-tree full-suite baseline `Go test: 1144 passed in 10 packages` (1142 pre-slice + 2 new pins)
