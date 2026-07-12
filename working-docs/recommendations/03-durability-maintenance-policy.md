# Operationalize Durability Maintenance

Status: generic maintenance baseline completed 2026-07-12; product policy
targets remain trigger-dependent

Promotion trigger: a real hosted app requires a repeatable production snapshot,
compaction, backup, and recovery routine.

Owners: root runtime, commitlog, CLI/app-owned maintenance commands,
observability, operator docs

## Current Result

The generic policy remains app-owned and offline-first. The canonical
hosted-chat maintenance binary now has a `prepare-backup` command that opens a
quiesced, existing DataDir without starting runtime services, schedulers,
startup migration hooks, or protocol serving; creates a snapshot; waits for its
horizon to be durable; compacts only against that completed snapshot ID; and
closes before reporting success. Invalid formats and missing paths fail before
mutation. Its deterministic recovery drill then copies
the complete DataDir offline, restores it into a fresh directory, runs
module-linked compatibility preflight, restarts, and verifies declared-query
state. The hosted-chat gate exercises the same sequence.

Existing snapshot metrics/logs plus command exit status and JSON result cover
the demonstrated visibility needs; the drill did not reveal a runtime
instrumentation hole. Production cadence, retention, RPO/RTO, and execution
targets cannot be selected until a real application supplies them.

## Why

Shunter already exposes synchronous snapshots, snapshot-covered compaction,
offline backup/restore, durability waiters, and compatibility preflight. The
remaining gap is a supported policy for composing them safely and observing
their result. Automatic snapshotting is currently disabled in runtime defaults.

## Outcome

An app-owner can state when snapshots occur, when sealed log segments are
compacted, how backups are coordinated, and how RPO/RTO expectations are
verified.

## Work

1. [x] Define supported maintenance sequences for quiet-period and graceful-drain
   operation using existing root APIs.
2. [x] Keep policy app-owned; no runtime controller is justified by the generic
   drill.
3. [x] Reuse existing diagnostics and command results; add instrumentation only
   if operators cannot distinguish slow work from stalled work.
4. [ ] Compacted segment range/bytes and progress instrumentation were not
   added: the command reports recovered/completed snapshot horizons and a
   terminal compaction result, and the generic drill showed no visibility hole.
   Revisit only with a measured product/operator need.
5. [x] Add a maintained app-owned command or script that exercises the policy.
6. [x] Extend the hosted binary gate to prove the chosen sequence when that can run
   deterministically and quickly.
7. [x] Document restore drills and compatibility preflight as part of the policy.

## Non-Goals

- online backup of an actively changing DataDir unless separately approved
- partial table restore
- remote commit-log replication
- reference-format snapshots or logs
- silently deleting history without a completed, selected snapshot

## Completion Evidence

- [x] one documented supported maintenance policy
- [x] deterministic integration coverage for snapshot, compaction, backup, and
  restored startup
- [x] operator-visible success and failure results
- [ ] measured cadence and recovery objectives on a real product operating
  fixture; dormant until a product exists
- [x] unchanged recovery correctness and DataDir safety tests
