# P0-SCHED-001 — scheduled reducer startup / firing ordering

Status: closed for the current parity target, with explicit deferrals.

This is now a short current-state record. The older source-reading and
session-plan detail was removed because the behavior is pinned in live
tests and summarized in `docs/parity-phase0-ledger.md`.

## Current contract

- Startup replay scans `sys_scheduled`, enqueues past-due schedules,
  arms future schedules, and returns the maximum observed schedule id
  so post-restart allocation does not collide.
- Successful one-shot firings delete their schedule row.
- Successful interval firings advance from the intended fire time.
- Missing schedule rows during a cancel race are tolerated.
- Shunter intentionally preserves scan order for past-due replay
  rather than sorting by intended fire time.
- Shunter intentionally retains a one-shot schedule row when the
  scheduled reducer panics or returns an error.

Authoritative pins:

- `executor/scheduler_replay_test.go`
- `executor/scheduler_firing_test.go`
- `executor/scheduler_worker_test.go`
- `executor/sys_scheduled_test.go`

## Remaining deferrals

Keep these as explicit scheduler follow-ons only if workload evidence
or a parity regression makes them worth reopening:

- Reference-style `fn_start` clamping for the first repeated schedule
  time. Shunter currently uses wall-clock time at `ScheduleRepeat`.
- Reference-style deletion of one-shot rows after panic. Shunter rolls
  back the reducer transaction and leaves the row for retry.
- Sorting past-due startup replay by intended time. Current scan-order
  behavior is pinned as intentional.
- Reference commitlog workload labeling for scheduled firings. Shunter
  commitlog records remain Shunter-specific.
- Startup ordering relative to lifecycle hooks. Recheck only if
  scheduler/lifecycle bootstrap work is reopened.

## Reading rule

Use this document only to understand the remaining scheduler deferrals.
For current scenario status, use `docs/parity-phase0-ledger.md`.
For prioritization, use `TECH-DEBT.md`.
