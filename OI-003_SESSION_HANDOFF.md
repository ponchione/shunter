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

Initial decisions D-001 through D-019 are locked in `OI-003.md`:

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
- complete snapshots require no lock file, valid file/header/hash, and schema
  compatibility; direct-write-plus-lock is live behavior to inventory, while
  temp-file-plus-rename is the OI-003 target.
- OI-003 crash tests target observable file artifacts, not kernel/fsync lies or
  arbitrary torn-sector models.
- recovery report shape should be defined, but initial slices should not widen
  public APIs solely for observability.
- OI-003 does not add a durable-response mode.
- offset indexes are advisory replay accelerators, never recovery truth.
- snapshot txID is the replay boundary and must describe state through that txID.
- recovery reporting should become a first-class detailed API/report surface.
- snapshot writing should move toward temp-file-plus-rename completion.
- snapshot writers should receive or verify a committed-state horizon instead
  of permanently trusting caller txID discipline.
- compaction should be idempotent and resumable after partial cleanup failures.

## Next Work

Start with `OI-003-A Recovery Contract Inventory`.

Suggested first slice:

- inventory existing tests against D-001 through D-019
- identify the smallest missing contract pins
- add failing or contract-pinning tests before changing behavior
- prioritize sequence/autoincrement recovery, compaction safety, and snapshot
  compatibility because those have the clearest data-loss risk
- specifically audit sequence recovery around `commitlog/recovery.go` helpers
  before claiming D-005 is implemented
- audit snapshot txID/horizon discipline before claiming D-015 is implemented
- audit snapshot write atomicity and temp-file rename requirements before
  claiming D-017 is implemented
- audit compaction partial-failure states before claiming D-019 is implemented

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
