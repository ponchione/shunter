# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

## What just landed (2026-04-21)

Tier-B hardening narrow sub-slice of `OI-004`: `ConnManager.CloseAll` disconnect-ctx sub-hazard closed. Closes the `Background`-rooted `Conn.Disconnect` call-site family: supervisor, sender overflow, and CloseAll now all derive a bounded ctx at the spawn point.

- Decision doc: `docs/hardening-oi-004-closeall-disconnect-context.md`
- Sharp edge: `protocol/conn.go::ConnManager.CloseAll` (graceful-shutdown entry per SPEC-005 §11.1, close code 1000) spawned a goroutine per registered `*Conn` and invoked `c.Disconnect(ctx, ...)` with the caller-supplied ctx threaded directly through. `Conn.Disconnect` forwards the ctx into `inbox.DisconnectClientSubscriptions` and `inbox.OnDisconnect` (steps 1-2 of the SPEC-005 §5.3 teardown); both honor ctx cancellation via `executor/protocol_inbox_adapter.go:58-63` and `awaitReducerStatus` at lines 133-145. The caller contract was unpinned — a caller that handed `CloseAll` a `context.Background()` (tests today, future OI-008 server lifecycle) made every per-conn inbox seam non-cancellable. A single hung inbox call (executor dispatch deadlock, inbox-drain stall, scheduler-held lock, executor crash waiting on never-fed respCh) pinned its supervisor-spawned teardown goroutine and transitively the `CloseAll` WaitGroup, which blocks process shutdown. Identical hazard class to the `superviseLifecycle` and `connManagerSender.enqueueOnConn` sites closed earlier the same day, at the graceful-shutdown boundary instead of the supervisor / overflow boundaries.
- Fix: bounded ctx. `CloseAll`'s per-conn goroutine now derives `context.WithTimeout(ctx, c.opts.DisconnectTimeout)` with `defer cancel()` before calling `Disconnect`. Reuses the existing `ProtocolOptions.DisconnectTimeout` field (5 s default) from the sender-disconnect slice — no new option, no new default. The outer ctx is still honored: a cancellation on the caller's ctx propagates into every per-conn derived ctx immediately (Go's `context.WithTimeout` on an already-cancelled parent cancels synchronously). But a Background-rooted caller now has per-conn teardown capped at `DisconnectTimeout` rather than unbounded. Happy path unchanged: inbox calls return promptly, per-conn Disconnect completes, deferred cancel fires, WaitGroup drains. Hang path: after `DisconnectTimeout` the inbox returns `ctx.Err()`; `Conn.Disconnect` proceeds to steps 3-5 unconditionally — `mgr.Remove` + `cancelRead` + `close(c.closed)` + optional `closeWithHandshake` — so the `*Conn` becomes collectible and the WaitGroup unblocks.
- New pins: `protocol/closeall_disconnect_timeout_test.go::{TestCloseAllBoundsDisconnectOnInboxHang, TestCloseAllDeliversOnInboxOK}`. Hang pin reuses the `blockingInbox` helper from `protocol/sender_disconnect_timeout_test.go`, sets `DisconnectTimeout = 150ms`, calls `CloseAll(context.Background(), inbox)`, waits for the goroutine to hit the inbox, and asserts CloseAll returns within `DisconnectTimeout + 1 s` slack with `conn.closed` fired, `mgr.Get == nil`, and both `DisconnectClientSubscriptions` + `OnDisconnect` counts == 1 (teardown proceeded through step 2 after step 1's ctx-bounded return). Happy-path pin uses a non-blocking `fakeInbox` and asserts completion well under `DisconnectTimeout`. Both run green under `-race -count=3`.
- `TECH-DEBT.md` OI-004 updated: sub-hazard closed with pin anchors; remaining OI-004 sub-hazards narrowed to other detached-goroutine sites in `conn.go` / `lifecycle.go` / `outbound.go` / `keepalive.go`, the `ClientSender.Send` no-ctx follow-on, the dispatch-handler ctx audit (`protocol/dispatch.go:168` spawns handler goroutines with `context.Background()` from `upgrade.go:201`), and the `forwardReducerResponse` ctx audit (`executor/protocol_inbox_adapter.go:128` spawns a goroutine with Background ctx; analog to the watchReducerResponse fix on the other seam).

Baseline note (2026-04-21): clean-tree full-suite was `Go test: 1144 passed in 10 packages` before this slice; after this slice's two new pins the baseline is `Go test: 1146 passed in 10 packages` (protocol package went from 270 to 272 tests).

Flaky test note: two pre-existing map-iteration-order-dependent tests surface intermittently on a clean tree. Neither is caused by this slice. Both worth a dedicated cleanup slice.
- `executor/TestParityP0Sched001ReplayEnqueuesByIterationOrder` depends on Go map iteration order matching RowID insertion order. Fix: either sort enqueues by `(next_run_at_ns, schedule_id)` or refactor the seed to avoid map-iteration dependence.
- `subscription/TestProjectedRowsBeforeAppendsDeletesAfterBagSubtraction` (`eval_projected_rows_test.go:31-70`) builds a mock committed view from a `map[TableID][]types.ProductValue`, then asserts `projectedRowsBefore(dv, 1)[0]` equals rowA and `[1]` equals rowB. Current-map-rows originate from `t.rows` map iteration order, which Go does not guarantee. Seen both under `-race` and without. Fix: sort `projectedRowsBefore` output deterministically or refactor the seed to a single-row table so ordering is not exercised.

Prior closed anchors in the same calendar week (still landed, included here for continuity):
- OI-004 `superviseLifecycle` disconnect-ctx — `docs/hardening-oi-004-supervise-disconnect-context.md`
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

With `P0-RECOVERY-001`, `P0-SCHED-001`, `P0-SUBSCRIPTION-001` closed, seven OI-005 sub-hazards closed (iter GC retention, iter use-after-Close, iter mid-iter-close, subscription-seam read-view lifetime, `CommittedSnapshot.IndexSeek` BTree-alias, `StateView.SeekIndex` BTree-alias, `StateView.SeekIndexRange` BTree-alias), the OI-006 slice-header aliasing sub-hazard closed, and four OI-004 sub-hazards closed (`watchReducerResponse` goroutine-leak, `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx, `superviseLifecycle` disconnect-ctx, `ConnManager.CloseAll` disconnect-ctx — the `Background`-rooted `Conn.Disconnect` call-site family now fully closed), the grounded options are:

### Option α — Continue Tier-B hardening

`TECH-DEBT.md` still carries:
- OI-004 remaining sub-hazards (other detached goroutines in `protocol/conn.go` / `lifecycle.go` / `outbound.go` / `keepalive.go`; `ClientSender.Send` no-ctx follow-on; dispatch-handler ctx audit; `forwardReducerResponse` ctx audit)
- OI-005 remaining sub-hazards (`state_view.go` / `committed_state.go` escape routes beyond `IndexSeek`, `StateView.SeekIndex`, and `StateView.SeekIndexRange`: `CommittedState.Table(id) *Table` raw-pointer exposure, `StateView.ScanTable` iterator surface)
- OI-006 remaining sub-hazards (row-payload sharing under the post-commit row-immutability contract; broader fanout assembly hazards in `subscription/fanout.go`, `subscription/fanout_worker.go`, `protocol/fanout_adapter.go` if any future path introduces in-place mutation)
- OI-008 (top-level bootstrap missing)

Pick one narrow sub-hazard and land a narrow fix with a focused test, following the shape of `docs/hardening-oi-004-closeall-disconnect-context.md` (latest) or `docs/hardening-oi-004-supervise-disconnect-context.md` (direct precedent). Concrete candidates:
- OI-004 dispatch-handler ctx: `protocol/dispatch.go:168` spawns handler goroutines with the `ctx` received by `runDispatchLoop`, which is hardcoded to `context.Background()` at `protocol/upgrade.go:201`. Handlers like `handleCallReducer` / `handleSubscribeSingle` / `handleSubscribeMulti` forward that ctx into `inbox.CallReducer` / `inbox.RegisterSubscriptionSet` / `inbox.UnregisterSubscriptionSet`, which then route through `executor.SubmitWithContext(ctx, cmd)` where a full command inbox (queue capacity hit) blocks on the executor select until ctx cancels. With Background root, a wedged executor accepts submits up to capacity and blocks the handler goroutine thereafter — the goroutine also holds an `inflightSem` slot at `dispatch.go:162-166`, so after `IncomingQueueMessages` concurrent hangs the connection hits "too many requests" and closes. Different hang class from the disconnect path (request/response, not teardown), but the same "Background-rooted caller" shape. A narrow slice would either derive per-request timeouts inside each handler (mirroring the DisconnectTimeout pattern) or tie the goroutines to `conn.closed` via a derived ctx.
- OI-004 `forwardReducerResponse` ctx: `executor/protocol_inbox_adapter.go:128` spawns `go a.forwardReducerResponse(ctx, req, respCh)` with the Background ctx from the dispatch loop. If the executor never feeds its internal `respCh` (crash mid-commit, hung reducer on a shutting-down engine), the goroutine leaks — same class as the `watchReducerResponse` hazard closed 2026-04-20 but on the adapter seam. A narrow slice would tie the goroutine to a lifecycle signal: either a bounded ctx or a select arm on the executor's shutdown channel.
- OI-005 `CommittedState.Table(id) *Table` raw-pointer escape: the method RLocks / RUnlocks and returns a raw `*Table` pointer whose internal `rows` map and indexes are mutated only under the `CommittedState` write lock. Today's callers (`CommittedSnapshot` methods hold the snapshot RLock for the use window; `StateView` accepts no lock but operates under the executor single-writer discipline; tests hold the pointer under test ownership) are safe, but the raw pointer itself is a separate escape surface. A narrow slice: either (a) return a narrower interface wrapper that re-checks snapshot openness on every access, or (b) document the contract explicitly and pin a test that a `*Table` pointer obtained outside the snapshot envelope is never used for reads. Option (b) is narrower and follows the subscription-seam pin precedent.
- OI-005 `StateView.ScanTable` iterator surface: reaches `sv.committed.Table(...)` through the raw `*Table` pointer. `StateView` is used inside reducer execution under the executor single-writer discipline, so today's pattern is safe, but the iterator closure captures `sv.committed` / `sv.tx` and yields rows without any `KeepAlive` / lock check. Row payloads are already `Copy()`d at `table.go:126`, but the outer map iteration walks `t.rows` live. If a caller retained the iterator across a commit boundary (currently forbidden by the executor's serialization, but unpinned), Go's concurrent-map-write detector would race. Note: `StateView.SeekIndex` and `StateView.SeekIndexRange` were both closed 2026-04-20 via `slices.Clone` / `slices.Collect` at the seek boundary — `ScanTable` has no materialized slice alias, but still has the single-writer-contract lifetime class; a narrow sub-slice would pin the contract via a focused test in the shape of `hardening-oi-005-subscription-seam-read-view-lifetime.md`.
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
10. `docs/hardening-oi-004-closeall-disconnect-context.md` (latest closed slice — recent precedent for a Tier-B hardening decision doc + pin on the CloseAll graceful-shutdown surface; closes the `Background`-rooted `Conn.Disconnect` call-site family)
11. `docs/hardening-oi-004-supervise-disconnect-context.md` (prior OI-004 sub-slice — direct precedent for the CloseAll-disconnect-ctx analog just landed)
12. `docs/hardening-oi-004-sender-disconnect-context.md` (earlier OI-004 sub-slice)
13. `docs/hardening-oi-004-watch-reducer-response-lifecycle.md` (earlier OI-004 sub-slice)
14. `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md` (prior OI-005 sub-slice — slice-aliasing precedent)
15. `docs/hardening-oi-005-state-view-seekindex-aliasing.md` (prior OI-005 sub-slice — precedent)
16. `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md` (prior OI-005 sub-slice — precedent)
17. `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md` (prior OI-005 sub-slice — contract-pin precedent)
18. `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md` (prior OI-005 sub-slice — precedent)
19. `docs/hardening-oi-006-fanout-aliasing.md` (prior closed Tier-B slice — precedent)
20. `docs/hardening-oi-005-snapshot-iter-useafterclose.md` (prior OI-005 sub-slice — precedent)
21. `docs/hardening-oi-005-snapshot-iter-retention.md` (earlier OI-005 sub-slice — additional precedent)
22. `docs/parity-p0-recovery-001-replay-horizon.md` (prior-closed parity slice — precedent for a narrow-and-pin parity decision doc)
23. `docs/parity-p0-sched-001-startup-firing.md` (prior-closed parity slice — alternative precedent)
24. `docs/parity-phase2-slice3-lag-policy.md` (earlier-closed parity slice — another precedent)
25. the specific code surfaces for whichever anchor (α/β/γ/δ) you pick

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
- **OI-004 `ConnManager.CloseAll` disconnect-ctx sub-hazard (2026-04-21)** — closes the `Background`-rooted `Conn.Disconnect` call-site family

## Suggested verification commands

Targeted:
- `rtk go test ./protocol -run 'TestCloseAll' -race -count=3 -v`
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
- already-landed parity pins still pass (including `TestCloseAllBoundsDisconnectOnInboxHang`, `TestCloseAllDeliversOnInboxOK`, `TestCloseAll_DisconnectsEveryConnection`, `TestCloseAll_EmptyManagerNoOp`, `TestSuperviseLifecycleBoundsDisconnectOnInboxHang`, `TestSuperviseLifecycleDeliversOnInboxOK`, `TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang`, `TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK`, `TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`, `TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation`, `TestWatchReducerResponseExitsOnConnClose`, `TestWatchReducerResponseDeliversOnRespCh`, `TestWatchReducerResponseExitsOnRespChClose`, `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnInsert`, `TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnRemove`, `TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join`, `TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable`, `TestCommittedSnapshotTableScanPanicsOnMidIterClose`, `TestCommittedSnapshotIndexRangePanicsOnMidIterClose`, `TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose`, `TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers`, `TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers`, `TestCommittedSnapshotTableScanPanicsAfterClose`, `TestCommittedSnapshotIndexScanPanicsAfterClose`, `TestCommittedSnapshotIndexRangePanicsAfterClose`, `TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`, `TestParityP0Recovery001SegmentSkipDoesNotOpenExhaustedSegment`, `TestParityP0Sched001PanicRetainsScheduledRow`, `TestPhase2Slice3DefaultOutgoingBufferMatchesReference`, and `TestSuperviseLifecycleInvokesDisconnectOnReadPumpExit`). Note: `TestParityP0Sched001ReplayEnqueuesByIterationOrder` is intermittently flaky on a clean tree (pre-existing, map-iteration-order dependent) — do not treat a single-run failure there as caused by your slice.
- full suite still passes. Clean-tree baseline before this slice: `Go test: 1144 passed in 10 packages`. After this slice's 2 new pins: `Go test: 1146 passed in 10 packages` (protocol package from 270 → 272).
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
- OI-004 `superviseLifecycle` disconnect-ctx sub-hazard closed
- **OI-004 `ConnManager.CloseAll` disconnect-ctx sub-hazard closed** — closes the `Background`-rooted `Conn.Disconnect` call-site family (supervisor, sender overflow, CloseAll now all derive a bounded ctx at the spawn point); other detached-goroutine surfaces in `conn.go` / `lifecycle.go` / `outbound.go` / `keepalive.go`, the `ClientSender.Send` no-ctx follow-on, the dispatch-handler ctx audit, and the `forwardReducerResponse` ctx audit remain open under OI-004
- next realistic anchors: further Tier-B hardening (α), broader SQL parity (β), format-level commitlog parity (γ), individual scheduler deferrals (δ)
- 10 packages, `rtk go test ./protocol` 272/272 passing after this slice; clean-tree full-suite baseline `Go test: 1146 passed in 10 packages` (1144 pre-slice + 2 new pins)
