# OI-003 Session Handoff

Use this handoff for OI-003 recovery and store semantics work.

Do not use `NEXT_SESSION_HANDOFF.md` for OI-003. That file remains the active
handoff for OI-002 query/subscription parity agents.

## Startup

Required reading before editing:

1. `RTK.md`
2. This file
3. `OI-003.md`

Then inspect only the package docs, code, and tests for the selected OI-003
workstream.

Recommended initial Go inspection:

```bash
rtk go list -json ./store ./commitlog ./types ./bsatn ./executor
rtk go doc ./commitlog
rtk go doc ./store
rtk go doc ./executor.DurabilityHandle
rtk go doc ./commitlog.OpenAndRecoverDetailed
rtk go doc ./commitlog.RecoveryResumePlan
rtk go doc ./commitlog.ReplayLog
```

## Current Objective

OI-003 is now governed by `OI-003.md`, not by the short `TECH-DEBT.md` entry.

Initial decisions D-001 through D-009 are locked in `OI-003.md`:

- `StatusCommitted` is an accepted-commit acknowledgement, not an fsync
  durability acknowledgement.
- recovery source of truth is newest compatible snapshot at/below durable
  horizon plus contiguous newer log replay.
- only bounded damaged active tails after a validated prefix are recoverable.
- snapshot compatibility is an exact current-registry match; readable schema
  mismatch fails closed.
- sequence/autoincrement recovery uses encoded metadata checked against
  observed committed values.
- compaction deletes only sealed, fully covered segments after a verified
  snapshot and durable horizon.
- operator errors use existing category sentinels, structured error types, and
  stable wrapping context.
- recovered store equivalence includes rows, indexes, uniqueness, sequences,
  and next-row-ID state.
- unknown storage versions fail closed; migration is out of scope.

## Next Work

Start with `OI-003-A Recovery Contract Inventory`.

Suggested first slice:

- inventory existing tests against D-001 through D-009
- identify the smallest missing contract pins
- add failing or contract-pinning tests before changing behavior
- prioritize sequence/autoincrement recovery, compaction safety, and snapshot
  compatibility because those have the clearest data-loss risk
- specifically audit sequence recovery around `commitlog/recovery.go` helpers
  before claiming D-005 is implemented

Do not begin broad rewrites of commitlog, snapshot, or executor internals until
the missing invariant is represented by a test.

## Boundaries

Keep separate unless a concrete reproducer crosses the boundary:

- OI-002 query/subscription correctness
- OI-007 scheduler restart and replay-edge behavior
- OI-005 lower-level read-view lifetime discipline
- hosted-runtime V1.5 declaration work

Expect OI-002 agents to keep modifying OI-002 files and `NEXT_SESSION_HANDOFF.md`.
Ignore unrelated dirty worktree state unless it directly affects the selected
OI-003 package.

## Validation

For docs-only changes, no Go validation is required.

For OI-003 code changes, run targeted tests first, then broaden:

```bash
rtk go test ./store ./commitlog ./types ./bsatn ./executor -count=1
rtk go vet ./store ./commitlog ./types ./bsatn ./executor
rtk go test ./... -count=1
```

Run `rtk go fmt` on touched Go packages before finishing.
