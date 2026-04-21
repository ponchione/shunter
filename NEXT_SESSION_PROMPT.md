Continue Shunter parity work on `P0-SCHED-001`: scheduled reducer
startup / firing ordering.

The previous run (2026-04-20, commit following Phase 2 Slice 3) closed
the lag / slow-client policy slice (`P0-SUBSCRIPTION-001`). Per-client
outbound queue default is now aligned to reference
`CLIENT_CHANNEL_CAPACITY = 16 * 1024`. See
`docs/parity-phase2-slice3-lag-policy.md` for the shape of a landed
parity decision doc.

Baseline: `Go test: 1102 passed in 10 packages`.

## What you are walking into

`P0-SCHED-001` is the largest remaining `in_progress` row in
`docs/parity-phase0-ledger.md`. Shunter's scheduler currently re-enqueues
past-due scheduled reducers on restart, arms the next wakeup for future
ones, and resumes new schedule allocation from the max recovered
`schedule_id`. The reference's externally visible firing / startup
ordering has not yet been compared directly. That is this slice's job.

Your job is to either:
(a) close the row — emulate the reference's startup/firing ordering
    closely enough that the externally visible behavior is parity-close,
    pinned by focused tests, or
(b) narrow the row — name exactly which sub-scenarios are parity-close,
    pin those, and record the remaining sub-scenarios as explicit
    deferrals with reference-citing rationale.

Either outcome is acceptable. A half-landed middle is not. Ground the
decision against the reference code directly — do not guess from the
Shunter-side comments.

## Grounding the reference

Start with `reference/SpacetimeDB/crates/core/src/host/scheduler.rs`.
The comments at lines 90, 200, 211, 347, 406, and 426 are the entry
points into the startup/restart/firing semantics. Also skim
`reference/SpacetimeDB/crates/core/src/host/module_host.rs` for how the
scheduler hooks into module lifecycle.

Look for:
- at-startup: how past-due schedules are discovered and re-enqueued
- at-startup: whether firings happen before or after `client_connected`
  / other lifecycle reducers
- at-steady-state: how `fn_start` vs wall-clock `now()` is used for
  rescheduling (see comment at `scheduler.rs:211`)
- on-restart: how `Workload::Internal` vs user-driven scheduling
  distinguishes firings (comment at `:426`)
- on-failure: what happens when a scheduled reducer's execution fails
  or panics — does the schedule re-arm or not?
- ordering: when multiple past-due schedules exist at startup, what
  order do they fire in?

Do NOT copy any Rust code. Clean-room constraint still applies. Read
enough to describe the externally visible contract and pick a policy.

## Primary Shunter code surfaces

- `executor/scheduler.go` — scheduler registry / allocation
- `executor/scheduler_worker.go` — firing loop, wakeup arming
- `executor/lifecycle.go` — startup ordering w.r.t. OnConnect /
  OnDisconnect and scheduled firing
- `executor/executor.go` — dispatch seam
- `executor/sys_scheduled.go` — `sys_scheduled` system table surface
- `executor/scheduler_replay_test.go` — existing restart/replay pins
- `executor/scheduler_worker_test.go` — existing steady-state pins
- `executor/scheduler_firing_test.go` — existing firing-ordering pins
- `executor/scheduler_test.go` — allocation / horizon pins

## Decision framing

Concrete choices:

1. Emulate reference: compare every externally visible startup/firing
   behavior, align any that differ, pin each with a focused test. Land
   new tests at the executor boundary (dispatch + wakeup observable)
   rather than at a helper level. This is appropriate if the gap is
   narrow — e.g. one or two ordering decisions.

2. Narrow and pin: if the full parity set is too broad for one slice,
   pick the highest-leverage sub-scenarios (e.g. startup re-enqueue
   ordering + `fn_start` vs `now()` choice), close those, and mark the
   remaining sub-scenarios as explicit deferrals with reference-citing
   rationale. Prefer this path if chasing full parity would require
   substantial reshaping of the scheduler worker's wakeup model.

Prefer option 2 if the reference scheduler's semantics are materially
different from Shunter's current model (new concurrency hazards,
widened contract). Prefer option 1 if the gap is narrow and closable
with small focused changes.

Write the decision down under `docs/parity-p0-sched-001-*.md` before
touching code, matching the structure of
`docs/parity-phase2-slice3-lag-policy.md` and
`docs/parity-phase1.5-outcome-model.md`.

## Mandatory reading order

1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md` — especially Tier A2 and
   Phase 3 scheduling framing
8. `docs/parity-phase0-ledger.md` — especially the `P0-SCHED-001` row
9. `TECH-DEBT.md` — especially OI-002 and OI-007
10. `docs/parity-phase2-slice3-lag-policy.md` — latest landed parity
    decision doc; copy its structure
11. `docs/parity-phase1.5-outcome-model.md` — older precedent for the
    decision-doc shape
12. `executor/scheduler.go`, `executor/scheduler_worker.go`,
    `executor/lifecycle.go`, `executor/executor.go`,
    `executor/sys_scheduled.go`
13. `executor/scheduler_replay_test.go`,
    `executor/scheduler_worker_test.go`,
    `executor/scheduler_firing_test.go`, `executor/scheduler_test.go`
14. reference files cited above (read-only grounding)

## Shell discipline

Use `rtk` for shell commands. Examples:
- `rtk git status --short --branch`
- `rtk go test ./executor -run '<targets>' -v`
- `rtk go test ./...`

## Acceptance gate

Do not call the work done unless all are true:

- reference behavior was read directly, not assumed from existing
  Shunter comments
- a written decision doc lives under `docs/parity-p0-sched-001-*.md`
  explaining the choice and its rationale, ≤200 lines, citing specific
  reference file:line anchors
- the code matches the decision: either the new emulated policy is
  implemented with focused tests, or the narrow sub-scenarios that are
  parity-close are explicitly pinned and the remaining sub-scenarios
  are recorded as explicit deferrals with reference-citing rationale
- existing scheduler pins (`scheduler_replay_test.go`,
  `scheduler_worker_test.go`, `scheduler_firing_test.go`,
  `scheduler_test.go`) either pass unchanged or are replaced with
  tests that lock the new contract — never silently weakened
- `protocol/parity_lag_policy_test.go::TestPhase2Slice3DefaultOutgoingBufferMatchesReference`
  and other closed-phase pins still pass
- full suite passes (current baseline:
  `Go test: 1102 passed in 10 packages`)
- `TECH-DEBT.md`, `docs/current-status.md`,
  `docs/parity-phase0-ledger.md`, and `NEXT_SESSION_HANDOFF.md` reflect
  the landing

## What is already closed (do not reopen)

- Protocol conformance P0-PROTOCOL-001..004
- Delivery parity P0-DELIVERY-001..002
- Recovery invariant P0-RECOVERY-002
- Subscription lag / slow-client policy P0-SUBSCRIPTION-001
- TD-142 Slices 1–14 (all narrow SQL parity shapes)
- Phase 1.5 outcome model + caller metadata wiring

## Deliverables

Either:
- decision doc + code + tests closing `P0-SCHED-001`

Or:
- decision doc + narrowed pins for the parity-close sub-scenarios +
  explicit reference-citing deferrals for the remainder, with the
  ledger row updated to reflect the narrowed scope

Either way, update:
- TECH-DEBT.md (OI-002 / OI-007 bullets)
- docs/current-status.md
- docs/parity-phase0-ledger.md (`P0-SCHED-001` row)
- NEXT_SESSION_HANDOFF.md

Final status snapshot right now:
- 10 packages, 1102 tests passing (baseline after Phase 2 Slice 3)
- `P0-SUBSCRIPTION-001` closed 2026-04-20
- next realistic anchors after `P0-SCHED-001`: replay horizon
  (`P0-RECOVERY-001`), Tier-B hardening (OI-004..007), broader SQL
  parity beyond TD-142
