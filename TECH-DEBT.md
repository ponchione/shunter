# TECH-DEBT

This file tracks open issues only.

Resolved and doc-drift-only audit entries were intentionally removed during the 2026-04-20 cleanup so this file can stay focused on live work. Use git history if you need the old resolved ledger.

Status conventions:
- open: confirmed issue or parity gap still requiring work
- deferred: intentionally not being closed now

Priority order:
1. externally visible parity gaps
2. correctness / concurrency bugs that undermine parity claims
3. capability gaps that block realistic usage
4. cleanup that should wait until parity decisions are locked

## Open issues

### OI-001: Protocol surface is still not wire-close enough to SpacetimeDB

Status: open
Severity: high

Summary:
- The reference subprotocol token is preferred, but the legacy `v1.bsatn.shunter` token is still accepted.
- Brotli remains a recognized-but-unsupported compression mode.
- Several message-family and envelope details remain intentionally divergent.

Why this matters:
- Client-visible protocol behavior is still one of the biggest blockers to serious parity claims.
- Even where semantics are close, the wire contract is still visibly Shunter-specific in important places.

Primary code surfaces:
- `protocol/upgrade.go`
- `protocol/compression.go`
- `protocol/tags.go`
- `protocol/wire_types.go`
- `protocol/client_messages.go`
- `protocol/server_messages.go`
- `protocol/send_responses.go`
- `protocol/send_txupdate.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A1
- `docs/parity-phase0-ledger.md` protocol conformance bucket

### OI-002: Query and subscription behavior still diverges from the target runtime model

Status: open
Severity: high

Summary:
- The SQL/query surface is still deliberately narrow.
- Row-level security / per-client filtering remains absent.
- Join projection semantics now emit projected-width rows end-to-end (TD-142 Slice 14, 2026-04-20): `subscription.Join` carries `ProjectRight bool`, the canonical hash distinguishes `SELECT lhs.*` from `SELECT rhs.*`, and `evalQuery` / `initialQuery` / `evaluateOneOffJoin` all slice the LHS++RHS IVM fragments onto the SELECT side.
- Lag / slow-client policy closed 2026-04-20 (Phase 2 Slice 3): `DefaultOutgoingBufferMessages` aligned to reference `CLIENT_CHANNEL_CAPACITY = 16 * 1024`; overflow-disconnect semantics preserved; close-frame mechanism (`1008 "send buffer full"`) retained as an intentional divergence from the reference `abort_handle.abort()` path. See `docs/parity-phase2-slice3-lag-policy.md` and `docs/parity-phase0-ledger.md` row `P0-SUBSCRIPTION-001`.
- Scheduled-reducer startup / firing ordering closed 2026-04-20 (Phase 3 Slice 1, `P0-SCHED-001`): existing startup-replay / firing pins kept as parity-close; new parity pins lock the intentional divergences (past-due iteration order, panic-retains-row) with reference citations. Remaining deferrals recorded with reference anchors in `docs/parity-p0-sched-001-startup-firing.md`.
- Remaining anchors: broader SQL/query-surface parity and RLS. See `docs/parity-phase0-ledger.md`.

Why this matters:
- The system can look architecturally right while still behaving differently under realistic subscription workloads.
- Query-surface limitations still cap how close clients can get to reference behavior.

Primary code surfaces:
- `query/sql/parser.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_oneoff.go`
- `subscription/predicate.go`
- `subscription/validate.go`
- `subscription/eval.go`
- `subscription/manager.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `executor/executor.go`
- `executor/scheduler.go`

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A2
- `docs/parity-phase0-ledger.md` scheduler / recovery parity scenarios

### OI-003: Recovery and store semantics still differ in user-visible ways

Status: open
Severity: high

Summary:
- Value-model and changeset semantics remain simpler than the reference.
- Commitlog/recovery behavior is intentionally rewritten rather than format-compatible.
- Replay tolerance, sequencing, and snapshot/recovery behavior still need parity decisions and follow-through.

Why this matters:
- Storage and recovery semantics are central to the operational-replacement claim.
- Sequencing and replay mismatches are the kind of differences users feel only after a crash or restart.

Primary code surfaces:
- `types/`
- `bsatn/encode.go`
- `bsatn/decode.go`
- `store/commit.go`
- `store/recovery.go`
- `store/snapshot.go`
- `store/transaction.go`
- `commitlog/changeset_codec.go`
- `commitlog/segment.go`
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/snapshot_io.go`
- `commitlog/compaction.go`
- `executor/executor.go`

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A3
- `docs/parity-phase0-ledger.md` recovery parity scenarios

### OI-004: Protocol lifecycle still needs hardening around goroutine ownership and shutdown

Status: open
Severity: high

Summary:
- Connection lifecycle code still relies on detached background goroutines and shutdown paths that are harder to reason about than a single owned lifecycle context.
- This is the main correctness/hardening theme still called out by the current-status and parity docs.
- `watchReducerResponse` goroutine-leak sub-hazard closed 2026-04-20: `protocol/async_responses.go::watchReducerResponse` previously blocked unconditionally on `<-respCh`, so if the executor accepted a CallReducer but never sent on or closed the response channel (executor crash mid-commit, hung reducer, engine shutdown with in-flight work) the goroutine leaked for the lifetime of the process and held its `*Conn` alive past disconnect. The goroutine body is now split into `runReducerResponseWatcher` and selects on both `respCh` and `conn.closed`, tying the watcher to the owning `Conn`'s SPEC-005 §5.3 teardown. Pinned by `protocol/async_responses_test.go::{TestWatchReducerResponseExitsOnConnClose, TestWatchReducerResponseDeliversOnRespCh, TestWatchReducerResponseExitsOnRespChClose}`. See `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`.
- `connManagerSender.enqueueOnConn` overflow-disconnect background-ctx sub-hazard closed 2026-04-21: the SPEC-005 §10.1 overflow path in `protocol/sender.go:106` previously spawned `go conn.Disconnect(context.Background(), ...)`. `Conn.Disconnect` threads the ctx into `inbox.DisconnectClientSubscriptions` and `inbox.OnDisconnect` (both honor ctx cancellation via the adapter's select arm in `executor/protocol_inbox_adapter.go:58-63` and `awaitReducerStatus` at `executor/protocol_inbox_adapter.go:133-145`), so with a Background ctx either hang — executor dispatch deadlock, inbox-drain stall, executor crash waiting on never-fed respCh — left the detached goroutine holding the `*Conn` and its transitive state forever. `closeOnce.Do` had latched but the body never reached `close(c.closed)`, so dispatch / keepalive / write loops for that conn could not exit either. The overflow path now derives a bounded ctx from `context.WithTimeout(context.Background(), conn.opts.DisconnectTimeout)` (default 5 s) and defers its cancel; a hung inbox call returns `ctx.Err()` after the timeout and Disconnect proceeds to steps 3-5 of the SPEC-005 §5.3 teardown unconditionally. Pinned by `protocol/sender_disconnect_timeout_test.go::{TestEnqueueOnConnOverflowDisconnectBoundsOnInboxHang, TestEnqueueOnConnOverflowDisconnectDeliversOnInboxOK}`. See `docs/hardening-oi-004-sender-disconnect-context.md`.
- `superviseLifecycle` disconnect-ctx sub-hazard closed 2026-04-21: the per-connection supervisor at `protocol/disconnect.go::superviseLifecycle` received `context.Background()` hardcoded by the only production call site (`protocol/upgrade.go:211`) and forwarded it directly into `c.Disconnect(ctx, ...)`. Same hang class as the overflow site: a hung `inbox.DisconnectClientSubscriptions` or `inbox.OnDisconnect` left the supervisor goroutine (and therefore the `*Conn` via `closeOnce` latched without `close(c.closed)`) pinned for the process lifetime. Supervisor now derives `context.WithTimeout(ctx, c.opts.DisconnectTimeout)` (reuses the existing 5 s default) and defers its cancel before calling `Disconnect`; Disconnect still proceeds to steps 3-5 of the teardown after the bounded step 1/2 returns `ctx.Err()`. Pinned by `protocol/supervise_disconnect_timeout_test.go::{TestSuperviseLifecycleBoundsDisconnectOnInboxHang, TestSuperviseLifecycleDeliversOnInboxOK}`. See `docs/hardening-oi-004-supervise-disconnect-context.md`.

Why this matters:
- Lifecycle races and unsafe close behavior undermine confidence in the protocol even when nominal tests pass.
- This is one of the main blockers to calling the runtime trustworthy for serious private use.

Remaining sub-hazards:
- other detached goroutines in the protocol lifecycle surface (`protocol/conn.go`, `protocol/lifecycle.go`, `protocol/outbound.go`, `protocol/keepalive.go`) if a specific leak site surfaces
- `ClientSender.Send` is still synchronous without its own ctx; a Send-ctx parameter would let callers propagate a shorter cancellation scope than `DisconnectTimeout` into the overflow path, but no concrete consumer needs that today
- `ConnManager.CloseAll` forwards whatever ctx the caller passes; the contract assumes the caller bounds shutdown but no pin enforces it

Primary code surfaces:
- `protocol/upgrade.go`
- `protocol/conn.go`
- `protocol/disconnect.go`
- `protocol/keepalive.go`
- `protocol/lifecycle.go`
- `protocol/outbound.go`
- `protocol/sender.go`
- `protocol/async_responses.go`

Source docs:
- `docs/current-status.md` open hardening / correctness picture
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-004-watch-reducer-response-lifecycle.md` (watchReducerResponse sub-hazard closure)
- `docs/hardening-oi-004-sender-disconnect-context.md` (sender overflow-disconnect background-ctx sub-hazard closure)
- `docs/hardening-oi-004-supervise-disconnect-context.md` (supervise-lifecycle disconnect-ctx sub-hazard closure)

### OI-005: Snapshot and committed-read-view lifetime rules still need stronger safety guarantees

Status: open
Severity: high

Summary:
- Snapshot/read-view lifetime discipline is still treated as a sharp edge in the surrounding docs.
- This is an architectural correctness concern, not cosmetic cleanup.
- Snapshot iterator GC retention sub-hazard closed 2026-04-20: `*CommittedSnapshot.TableScan` / `IndexScan` / `IndexRange` returned closures that captured `*Table` but not `*CommittedSnapshot`, so a caller holding only the iter could let the snapshot become unreachable, fire the finalizer, release the RLock mid-`range`, and race a concurrent writer on `Table.rows`. Each iterator now `defer runtime.KeepAlive(s)`s the snapshot so the closure retains it for the iter's lifetime. Pinned by `store/snapshot_iter_retention_test.go::TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`. See `docs/hardening-oi-005-snapshot-iter-retention.md`.
- Snapshot iterator use-after-Close sub-hazard closed 2026-04-20: the same three iterator bodies previously did not re-check `s.ensureOpen()` at iter-body entry, so a sequential `construct → Close → iterate` pattern silently raced the freed RLock. Each iterator body now calls `s.ensureOpen()` after the `KeepAlive` defer, converting the mis-use into a deterministic `"store: CommittedSnapshot used after Close"` panic matching the construction-time contract. Pinned by `store/snapshot_iter_useafterclose_test.go::{TestCommittedSnapshotTableScanPanicsAfterClose, TestCommittedSnapshotIndexScanPanicsAfterClose, TestCommittedSnapshotIndexRangePanicsAfterClose}`. See `docs/hardening-oi-005-snapshot-iter-useafterclose.md`.
- Snapshot iterator mid-iter-close sub-hazard closed 2026-04-20: the three iterator bodies previously checked `s.ensureOpen()` only once at iter-body entry, so a partially consumed iter whose owner called `Close()` mid-iteration (same goroutine caller body or another goroutine holding a reference) continued yielding subsequent rows against a released RLock. Each iter-body for-loop now re-calls `s.ensureOpen()` per-iteration so the next step after `Close()` panics with the construction-time contract message rather than silently yielding. Pinned by `store/snapshot_iter_mid_iter_close_test.go::{TestCommittedSnapshotTableScanPanicsOnMidIterClose, TestCommittedSnapshotIndexRangePanicsOnMidIterClose, TestCommittedSnapshotRowsFromRowIDsPanicsOnMidIterClose}`. Defense-in-depth only — cannot eliminate the machine-level race window between the check and an in-flight `t.rows` read; full ownership discipline still required from callers. See `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`.
- Subscription-seam read-view lifetime sub-hazard closed 2026-04-20: `subscription/eval.go::EvalAndBroadcast` receives a borrowed `store.CommittedReadView`, and `executor/executor.go:540-541` calls `view.Close()` immediately after the synchronous return. The no-view-escape-past-return contract was load-bearing but unpinned; today's code keeps it (the view reference does not land in `FanOutMessage`, no goroutine spawned from `evaluate` outlives the call, `DeltaView.Release` fires in `defer`), but nothing asserted it. A contract comment on `EvalAndBroadcast` and a `trackingView` wrapper pin the invariant: after `EvalAndBroadcast` returns and the test closes the tracker, the fan-out inbox is drained and the tracker asserts zero post-close method invocations — under both Join (Tier-2 + join delta) and single-table eval paths. Pinned by `subscription/eval_view_lifetime_test.go::{TestEvalAndBroadcastDoesNotUseViewAfterReturn_Join, TestEvalAndBroadcastDoesNotUseViewAfterReturn_SingleTable}`. No production-code behavior change. See `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`.
- `CommittedSnapshot.IndexSeek` BTree-alias escape route closed 2026-04-20: `store/snapshot.go::CommittedSnapshot.IndexSeek` forwarded `BTreeIndex.Seek` which returns a live alias of the index entry's internal `[]types.RowID`. A caller that retained the slice past `Close()` would race any subsequent writer's `slices.Insert` / `slices.Delete` on the same key — either in-place-shifted `Delete` or capacity-case `Insert`. Current callers (`subscription/eval.go:286`, `subscription/register_set.go:{92,117}`, `subscription/delta_join.go:{85,122}` via `subscription/delta_view.go:165`, `subscription/placement.go:162`) use the slice synchronously in a for-range and did not retain, but the contract was unpinned. `IndexSeek` now returns `slices.Clone(idx.Seek(key))` so callers cannot alias BTree-internal storage past the public read-view boundary. Pinned by `store/snapshot_indexseek_aliasing_test.go::{TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnInsert, TestCommittedSnapshotIndexSeekReturnsIndependentSliceAfterCloseOnRemove}`. See `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`.
- `StateView.SeekIndex` BTree-alias escape route closed 2026-04-20: the `StateView.SeekIndex` iterator body ranged over `idx.Seek(key)` directly — the same aliased `[]RowID` from the BTree entry. Go's `for _, v := range` captures the slice header once but reads from the backing array every iteration; a mid-iter in-place `slices.Delete` on the entry (yield callback reaching into a contract-violating path) shifts the tail down and drifts the yielded RowIDs. Today no caller triggers this under executor single-writer discipline, but the contract was unpinned. The iterator now ranges over `slices.Clone(idx.Seek(key))` so iteration is decoupled from BTree-internal storage, mirroring the `CommittedSnapshot.IndexSeek` fix. Pinned by `store/state_view_seekindex_aliasing_test.go::TestStateViewSeekIndexIteratesIndependentSliceAfterBTreeMutation`. See `docs/hardening-oi-005-state-view-seekindex-aliasing.md`.
- `StateView.SeekIndexRange` BTree-alias escape route closed 2026-04-20: `StateView.SeekIndexRange` ranged over `idx.BTree().SeekRange(low, high)` directly — an `iter.Seq` that walks `b.entries` live (outer loop reads `len(b.entries)` and indexes the backing array each step). A yield callback that reaches into the BTree and drops the last RowID of an entry behind the cursor fires `slices.Delete(b.entries, idx, idx+1)` and shifts the tail down in place; the outer `i++` then skips one entry that was present at seek time. Today no caller triggers this under executor single-writer discipline, but the contract was unpinned. The iterator now ranges over `slices.Collect(idx.BTree().SeekRange(low, high))` so iteration walks an independent materialized copy of the range, mirroring the `StateView.SeekIndex` fix. Pinned by `store/state_view_seekindexrange_aliasing_test.go::TestStateViewSeekIndexRangeIteratesIndependentRowIDsAfterBTreeMutation`. See `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`.

Why this matters:
- Long-lived or misused read views can distort concurrency assumptions and make correctness depend on caller discipline.
- It also weakens confidence in subscription evaluation and recovery-side read paths.

Remaining sub-hazards:
- `state_view.go` / `committed_state.go` shared-state escape routes beyond `IndexSeek`, `StateView.SeekIndex`, and `StateView.SeekIndexRange` (e.g. `CommittedState.Table(id) *Table` raw-pointer exposure; `StateView.ScanTable` iterator surface)

Primary code surfaces:
- `store/snapshot.go`
- `store/committed_state.go`
- `store/state_view.go`
- `subscription/eval.go`
- `executor/executor.go`

Source docs:
- `docs/current-status.md` open hardening / correctness picture
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-005-snapshot-iter-retention.md` (iter-retention sub-hazard closure)
- `docs/hardening-oi-005-snapshot-iter-useafterclose.md` (iter use-after-Close sub-hazard closure)
- `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md` (iter mid-iter-close sub-hazard closure)
- `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md` (subscription-seam read-view lifetime sub-hazard closure)
- `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md` (IndexSeek BTree-alias escape route closure)
- `docs/hardening-oi-005-state-view-seekindex-aliasing.md` (StateView.SeekIndex BTree-alias escape route closure)
- `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md` (StateView.SeekIndexRange BTree-alias escape route closure)

### OI-006: Subscription fanout still carries aliasing and cross-subscriber mutation risk concerns

Status: open
Severity: medium

Summary:
- Fanout and update assembly remain a live hardening concern around shared slices/maps and per-subscriber isolation.
- The parity docs treat this as one of the main non-cosmetic remaining risks.
- Per-subscriber `Inserts` / `Deletes` slice-header aliasing sub-hazard closed 2026-04-20: `subscription/eval.go::evaluate` previously distributed the same slice header across every subscriber of a query, so any downstream replace/append on one subscriber's slice would silently corrupt every other subscriber's view of the same commit. Each subscriber now receives an independent slice header for `Inserts` / `Deletes`; row payloads (`types.ProductValue`) remain shared under the post-commit row-immutability contract. Pinned by `subscription/eval_fanout_aliasing_test.go::{TestEvalFanoutInsertsHeaderIsolatedAcrossSubscribers, TestEvalFanoutDeletesHeaderIsolatedAcrossSubscribers}`. See `docs/hardening-oi-006-fanout-aliasing.md`.

Why this matters:
- Cross-subscriber mutation or aliasing bugs are subtle and can silently corrupt delivery behavior.
- This weakens confidence in both parity and basic correctness claims.

Remaining sub-hazards:
- row-payload (`types.ProductValue`) sharing across subscribers (governed by the post-commit row-immutability contract; only relevant if a future consumer mutates row contents in place)
- broader fanout assembly hazards in `subscription/fanout.go`, `subscription/fanout_worker.go`, and `protocol/fanout_adapter.go` if any future path introduces in-place mutation

Primary code surfaces:
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/current-status.md` open hardening / correctness picture
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-006-fanout-aliasing.md` (slice-header aliasing sub-hazard closure)

### OI-007: Recovery sequencing and replay-edge behavior still needs targeted parity closure

Status: open
Severity: medium

Summary:
- Replay-horizon / validated-prefix behavior (`P0-RECOVERY-001`) closed 2026-04-20 via narrow-and-pin (`docs/parity-p0-recovery-001-replay-horizon.md`). All four ledger sub-behaviors are parity-close under observation; the internal-mechanism difference (segment-level short-circuit vs reference per-commit `adjust_initial_offset`) is pinned as intentional. Remaining commitlog parity work — typed error enums, offset index file, format-level log / changeset parity — rolls up under `OI-003` as broader Phase 4 scope.
- Scheduler startup / firing ordering (`P0-SCHED-001`) closed 2026-04-20 via narrow-and-pin (`docs/parity-p0-sched-001-startup-firing.md`). Remaining scheduler deferrals (`fn_start`-clamped schedule "now", one-shot panic deletion, intended-time past-due ordering) are recorded there with reference anchors; reopen if workload evidence surfaces.
- The already-closed snapshot+replay invariant work did not eliminate the broader sequencing/replay parity backlog (format-level, offset index, etc.).

Why this matters:
- These are the kinds of gaps that only show up under restart, crash, and replay conditions.
- They materially affect the “operational replacement” claim.

Primary code surfaces:
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`

Source docs:
- `docs/parity-p0-recovery-001-replay-horizon.md` (replay-horizon closure)
- `docs/parity-p0-sched-001-startup-firing.md` (scheduler deferrals)
- `docs/parity-phase0-ledger.md` row `P0-RECOVERY-001` (closed)

### OI-008: The repo still lacks a coherent top-level engine/bootstrap story

Status: open
Severity: medium

Summary:
- There is still no `main` package, `cmd/` entrypoint, example app, or single polished bootstrap surface.
- `schema.Engine.Start(...)` is a startup schema-compatibility check, not the unified runtime bootstrap implied by the original architecture sketches.

Why this matters:
- The subsystem work is real, but the developer-facing embedding story is still weaker than the implementation depth underneath it.
- This makes it harder to judge the project as a usable replacement even if many internals are already substantial.

Primary code surfaces:
- `schema/version.go`
- `README.md`
- repo root package layout

Source docs:
- `README.md`
- `docs/current-status.md`

## Deferred issues

### DI-001: Energy accounting remains a permanent parity deferral

Status: deferred
Severity: low

Summary:
- `EnergyQuantaUsed` remains pinned at zero because Shunter does not implement an energy/quota subsystem.

Why this matters:
- This is an intentional parity gap, but it should remain explicit so it does not get mistaken for accidental completeness.

Source docs:
- `docs/parity-phase1.5-outcome-model.md`
- `docs/parity-phase0-ledger.md`
