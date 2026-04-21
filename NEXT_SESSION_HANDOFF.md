# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

## What just landed (2026-04-20)

Tier-B hardening narrow sub-slice of `OI-005` (snapshot / read-view lifetime) — snapshot iterator GC retention.

- Decision doc: `docs/hardening-oi-005-snapshot-iter-retention.md`
- Sharp edge: `store/snapshot.go` iterators (`TableScan`, `IndexScan`, `IndexRange`) previously returned `iter.Seq2` closures that captured `*Table` but not `*CommittedSnapshot`. A caller holding only the iter could let the snapshot become unreachable between iter construction and iter consumption; the finalizer could then fire mid-`range`, release the `RLock`, and race a concurrent writer on `Table.rows`.
- Fix: each iterator now `defer runtime.KeepAlive(s)` so the returned closure retains `*CommittedSnapshot` for the iter's lifetime.
- New parity pin:
  - `store/snapshot_iter_retention_test.go::TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration` — obtains an iter, drops the snapshot reference, runs `runtime.GC()` repeatedly, asserts a concurrent `cs.Lock()` does not acquire while the iter is held, then releases the iter and asserts the write lock is eventually acquired.
- `TECH-DEBT.md` OI-005 updated: sub-hazard closed with pin anchor; remaining sub-hazards (use-after-Close on returned iters, cross-goroutine sharing rules, subscription-seam read-view lifetime, `state_view.go` / `committed_state.go` shared-state escape routes) enumerated explicitly and stay open.

Baseline after this slice: `Go test: 1106 passed in 10 packages`.

Prior closed anchors in the same calendar day (still landed, included here for continuity):
- Phase 4 Slice 2 replay-horizon / validated-prefix (`P0-RECOVERY-001`) — `docs/parity-p0-recovery-001-replay-horizon.md`
- Phase 3 Slice 1 scheduled-reducer startup / firing ordering (`P0-SCHED-001`) — `docs/parity-p0-sched-001-startup-firing.md`
- Phase 2 Slice 3 lag / slow-client policy (`P0-SUBSCRIPTION-001`) — `docs/parity-phase2-slice3-lag-policy.md`

## Next realistic parity / hardening anchors

With `P0-RECOVERY-001`, `P0-SCHED-001`, `P0-SUBSCRIPTION-001` closed and one OI-005 sub-hazard closed, the grounded options are:

### Option α — Continue Tier-B hardening

`TECH-DEBT.md` still carries:
- OI-004 (protocol lifecycle / goroutine ownership)
- OI-005 remaining sub-hazards (use-after-Close on iters, cross-goroutine snapshot sharing, subscription-seam read-view lifetime, `state_view.go` / `committed_state.go` shared-state escape routes)
- OI-006 (subscription fanout aliasing / cross-subscriber mutation risk)
- OI-008 (top-level bootstrap missing)

Pick one narrow sub-hazard and land a narrow fix with a focused test, following the shape of `docs/hardening-oi-005-snapshot-iter-retention.md`. Concrete candidates:
- OI-005 use-after-Close: iterators currently check `ensureOpen()` at construction, not inside the body. A caller who calls `Close` between iter construction and range would silently race the freed RLock. Narrow fix: embed an `atomic.Load` check at iter body entry that short-circuits (or panics consistently) if closed. Focused test: construct iter, Close, iterate → expect zero rows or a documented panic.
- OI-006 fanout aliasing: `subscription/eval.go::evaluate` shares `Inserts` / `Deletes` slices across subscribers. Currently safe because all downstream consumers treat them read-only, but no test pins that invariant. Narrow fix: either add a defensive copy at the connID fan-out edge or pin the read-only invariant with a test that attempts to mutate a received update and asserts other subscribers' updates are unaffected.

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
10. `docs/hardening-oi-005-snapshot-iter-retention.md` (latest closed slice — recent precedent for a Tier-B hardening decision doc + pin)
11. `docs/parity-p0-recovery-001-replay-horizon.md` (prior-closed parity slice — precedent for a narrow-and-pin parity decision doc)
12. `docs/parity-p0-sched-001-startup-firing.md` (prior-closed parity slice — alternative precedent)
13. `docs/parity-phase2-slice3-lag-policy.md` (earlier-closed parity slice — another precedent)
14. the specific code surfaces for whichever anchor (α/β/γ/δ) you pick

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
- **OI-005 snapshot iterator GC retention sub-hazard (2026-04-20)**

## Suggested verification commands

Targeted:
- `rtk go test ./store -run 'TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration' -race -count=3 -v`
- `rtk go test ./...`

## Acceptance gate

Do not call the work done unless all are true:

- reference-backed or debt-anchored target shape was checked directly against reference material or current live code
- every newly accepted or rejected shape has focused tests
- already-landed parity pins still pass (including `TestCommittedSnapshotIteratorKeepsSnapshotAliveMidIteration`, `TestParityP0Recovery001SegmentSkipDoesNotOpenExhaustedSegment`, `TestParityP0Sched001ReplayEnqueuesByIterationOrder`, `TestParityP0Sched001PanicRetainsScheduledRow`, and `TestPhase2Slice3DefaultOutgoingBufferMatchesReference`)
- full suite still passes (current baseline: `Go test: 1106 passed in 10 packages`)
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
- OI-005 iterator-GC retention sub-hazard closed; broader OI-005 lifetime concerns stay open with enumerated sub-hazards
- next realistic anchors: further Tier-B hardening (α), broader SQL parity (β), format-level commitlog parity (γ), individual scheduler deferrals (δ)
- 10 packages, 1106 tests passing as of 2026-04-20
