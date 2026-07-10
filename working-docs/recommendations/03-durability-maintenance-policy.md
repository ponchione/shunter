# Operationalize Durability Maintenance

Status: recommended operability slice

Promotion trigger: a real hosted app requires a repeatable production snapshot,
compaction, backup, and recovery routine.

Owners: root runtime, commitlog, CLI/app-owned maintenance commands,
observability, operator docs

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

1. Define supported maintenance sequences for quiet-period and graceful-drain
   operation using existing root APIs.
2. Decide whether policy remains app-owned or merits a narrow advanced runtime
   controller; do not assume runtime automation is necessary.
3. Expose bounded progress/result diagnostics for snapshot and compaction if
   operators cannot currently distinguish slow work from stalled work.
4. Record snapshot horizon, durable horizon, compacted segment range, duration,
   bytes, and terminal error through existing observability conventions.
5. Add a maintained app-owned command or script that exercises the policy.
6. Extend the hosted binary gate to prove the chosen sequence when that can run
   deterministically and quickly.
7. Document restore drills and compatibility preflight as part of the policy.

## Non-Goals

- online backup of an actively changing DataDir unless separately approved
- partial table restore
- remote commit-log replication
- reference-format snapshots or logs
- silently deleting history without a completed, selected snapshot

## Completion Evidence

- one documented supported maintenance policy
- deterministic integration coverage for snapshot, compaction, backup, and
  restored startup
- operator-visible success and failure results
- measured execution on the product operating fixture
- unchanged recovery correctness and DataDir safety tests
