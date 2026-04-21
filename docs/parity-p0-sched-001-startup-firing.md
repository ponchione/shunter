# P0-SCHED-001 — scheduled reducer startup / firing ordering

Records the `P0-SCHED-001` parity decision called out in
`docs/parity-phase0-ledger.md` and
`docs/spacetimedb-parity-roadmap.md` Phase 3 Slice 1. Written
companion to the parity pins that lock the chosen shape.

## Reference shape (target)

`reference/SpacetimeDB/crates/core/src/host/scheduler.rs`:

- `SchedulerStarter::start` (`scheduler.rs:89-155`) is the
  restart/startup entry. Called from `Host::launch_module` at
  `host_controller.rs:1097`, strictly **after** dangling
  `client_disconnected` reducers and `clear_all_clients().await`
  (`host_controller.rs:1079-1095`).
- Startup sequence:
  1. Drain in-flight `rx` (`scheduler.rs:98-105`) so init-time
     schedules that already wrote rows aren't also replayed from the
     in-flight message.
  2. Open a read tx with `Workload::Internal` (`scheduler.rs:96`).
  3. Iterate `ST_SCHEDULED`; insert each row into `DelayQueue` at
     `now_instant + schedule_at.to_duration_from(now_ts)`
     (`scheduler.rs:118-141`). Past-due → duration near zero, fires
     in DelayQueue bucket order (approximates intended-time order;
     not strictly sorted).
  4. Spawn `SchedulerActor::run` (`scheduler.rs:144-154`).
- `Scheduler::schedule` (`scheduler.rs:201-246`) clamps "now" to
  `fn_start.max(Timestamp::now())` (`scheduler.rs:215`) so long /
  stutter-prone reducers don't shorten delays relative to the
  reducer's own start timestamp.
- Firing (`scheduler.rs:329-494`):
  - Per-call tx opens with `Workload::Internal`
    (`scheduler.rs:399`); `tx.ctx` patched to `Workload::Reducer`
    before running (`scheduler.rs:426-439`) so commitlog records
    reducer name/caller/timestamp/args.
  - `panic::catch_unwind` (`scheduler.rs:445`) isolates panics.
  - After reducer returns, `delete_scheduled_function_row` runs
    **regardless** of trap/panic (`scheduler.rs:448`): one-shot
    rows are **deleted** in a fresh tx
    (`scheduler.rs:501-522, 538-560`); interval rows are left and
    re-inserted into the queue via `Reschedule`.
  - `NoSuchModule` leaves the row (`scheduler.rs:346-348`).

Summary: past-due replay is eager, ordering is DelayQueue-bucket
(≈ intended-time), scheduling "now" is clamped to reducer start, and
**one-shot rows are removed even on panic** so scheduled reducers do
not retry forever on persistent failure.

## Shunter shape today

- `scheduler_worker.go::Scheduler.scanAndTrackMaxWithContext`
  (`scheduler_worker.go:151-176`) scans `sys_scheduled` via
  committed-state snapshot; rows with `next_run_at_ns <= now` are
  enqueued as `CallReducerCmd{Source: CallSourceScheduled}` in
  committed-state iteration order (≈ RowID), not by
  `next_run_at_ns`.
- `Scheduler.ReplayFromCommitted` (`scheduler_worker.go:133-144`) is
  the startup entry; returns max observed `schedule_id`.
  `executor.go:105-107` uses `maxScheduleID` on `NewExecutor` to
  seed the post-restart sequence past it.
- `scheduler.go::schedulerHandle.ScheduleRepeat`
  (`scheduler.go:92-95`) computes first fire as
  `time.Now().Add(interval).UnixNano()` — wall-clock, no clamp to
  reducer dispatch.
- `executor.go::handleCallReducer` (`executor.go:335-459`) fires in
  the reducer's own tx. On success: `advanceOrDeleteSchedule`
  mutates atomically (one-shot → delete, interval → advance to
  `intended + repeat`, `scheduler.go:36-52`). On user error or
  panic: `store.Rollback(tx)` (`executor.go:387, 402`); the row is
  unchanged and will be re-enqueued on the next scan.
- No in-flight drain: schedules reach `sys_scheduled` only via a
  reducer's tx, so no separate schedule-request channel exists.
- No `Workload::Internal` tag: Shunter's commitlog model is
  tag-less; this is a clean-room format divergence tracked under
  `OI-003` / `P0-RECOVERY-001`, not re-opened here.
- No top-level bootstrap wires `ReplayFromCommitted` relative to
  lifecycle hooks; called directly by tests (tracked by `OI-008`).

## Decision: narrow and pin (Option 2)

Close what is parity-close; pin the remaining divergences as
intentional with reference citations. Full emulation would reshape
the firing tx model or require new timestamp plumbing — better as a
later slice once scheduled-reducer workloads surface the gap.

**Closed as parity-close (existing pins):**

- Past-due re-enqueue on restart:
  `TestSchedulerReplayEnqueuesPastDue` (ref `scheduler.rs:118-130`).
- Future rows arm next wakeup: `TestSchedulerReplayArmsTimerForFuture`
  (ref `scheduler.rs:130`).
- Recovered max `schedule_id` seeds post-restart sequence:
  `TestSchedulerReplayReturnsMaxID`,
  `TestNewExecutorResetsSchedSeqFromExistingRows` (reference has no
  analog because `DelayQueue` has no id allocator — same outcome:
  no id collisions post-restart).
- One-shot success deletes row: `TestFiringOneShotDeletesRow` (ref
  `scheduler.rs:540-548`).
- Interval success advances to `intended + repeat`:
  `TestFiringRepeatAdvancesNextRun`,
  `TestFiringFixedRateUsesIntendedFireTime` (ref
  `scheduler.rs:513-520`).
- Cancel-race tolerance: `TestFiringMissingRowSucceeds` (ref
  `scheduler.rs:610-613` returns `Ok(None)`; Shunter commits
  unconditionally — same at-least-once outcome).

**New parity pins landed here:**

- `TestParityP0Sched001ReplayPreservesScanOrderWithoutSorting` — pins
  the intentional divergence that Shunter preserves whatever committed
  scan order it is given for past-due rows rather than sorting by
  `next_run_at_ns`. The committed-state `TableScan` surface is
  explicitly unordered, so the parity pin now targets the
  order-preservation seam directly instead of assuming map iteration
  matches RowID insertion order. Reference's DelayQueue bucket ordering
  is also non-strict, so this is not a client-visible regression for
  well-separated schedules.
- `TestParityP0Sched001PanicRetainsScheduledRow` — pins the
  intentional divergence that Shunter preserves `sys_scheduled` rows
  on reducer panic (consistent with reducer-error) while reference
  (`scheduler.rs:445-455`) deletes one-shot rows regardless.

**Deferred with reference-citing rationale:**

- **`fn_start`-clamped schedule "now"** (`scheduler.rs:211-215`).
  Shunter `ScheduleRepeat` uses wall-clock `time.Now()`; reference
  uses `max(reducer_start, time.Now())`. Bridging means plumbing
  dispatch timestamp into `SchedulerHandle`
  (`executor/scheduler.go:61`). Externally visible only when a
  reducer's dispatch-to-Schedule-call interval is non-trivial —
  first fire comes slightly later than reference.
- **Panic deletes one-shot rows** (`scheduler.rs:445-455`). Reference
  runs delete in a fresh tx after the reducer's tx regardless of
  panic; Shunter rolls back and retains the row. Bridging means
  emitting a second commit post-rollback — atomicity / post-commit
  pipeline change. Until then, a persistently-panicking one-shot
  will fire on every scheduler wake in Shunter.
- **Past-due iteration order**. Reference: DelayQueue-bucket
  (≈ intended time). Shunter: TableScan-ordered (≈ RowID). Both
  weak. A sort-by-`next_run_at_ns` would close this but is held
  back until a workload surfaces the need.
- **`Workload::Internal` / commitlog labelling of scheduled firings**
  (`scheduler.rs:96, 426-439`). Shunter's commitlog is tag-less;
  tracked under `OI-003` / `P0-RECOVERY-001`.
- **Startup ordering relative to lifecycle hooks**
  (`host_controller.rs:1079-1097`). No top-level bootstrap yet
  (`OI-008`). When a bootstrap exists, `ReplayFromCommitted` should
  be called after pending lifecycle hooks drain.
- **Drain-in-flight-rx before replay** (`scheduler.rs:98-105`). Not
  applicable: Shunter schedules reach `sys_scheduled` only through a
  reducer's tx. Same outcome (no duplicates), different mechanism —
  not a parity gap.

## Why narrow-and-pin and not full emulation

- Full emulation needs: (a) `fn_start` plumbed into `schedulerHandle`,
  (b) second-commit post-rollback for one-shot panic deletion, and
  (c) sort-by-intended-time on past-due enqueue. Each has its own
  atomicity / API implications and would balloon the slice.
- Each remaining divergence has a conscious rationale and a reference
  citation. A future slice driven by workload evidence can close
  them individually.
- Option 1 (emulate now) widens scope without closing an observed
  complaint. "Do nothing" leaves the ledger row open and the
  divergences implicit — the state Phase 0 was built to eliminate.

## Authoritative artifacts

- This document.
- `executor/scheduler_firing_test.go::TestParityP0Sched001PanicRetainsScheduledRow`
  — new pin locking the panic-retains-row divergence.
- `executor/scheduler_replay_test.go::TestParityP0Sched001ReplayPreservesScanOrderWithoutSorting`
  — new pin locking the replay-order-preservation-without-sorting
  divergence without depending on unordered map iteration.
- Existing pins re-asserted as parity-close: replay /
  seq-reset / firing tests listed above.
- `docs/parity-phase0-ledger.md` — `P0-SCHED-001` row moves from
  `in_progress` to `closed (divergences explicit)`.
- `TECH-DEBT.md` — `OI-007` drops the scheduler bullet; retains
  `P0-RECOVERY-001` replay-horizon anchor. `OI-002` scheduler line
  updated.
- `docs/spacetimedb-parity-roadmap.md` — Phase 3 Slice 1 marked
  closed with pointer to this doc.
