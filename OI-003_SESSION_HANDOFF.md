# OI-003 Session Handoff

Use this handoff only when auditing the completed OI-003 recovery and store
semantics campaign.

Do not use this as the next implementation queue. OI-003 is closed for current
evidence in `OI-003.md`. New runtime confidence work should start from
`docs/RUNTIME-HARDENING-GAUNTLET.md`; new OI-003 work requires a fresh
Shunter-visible recovery/store failing example.

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

## Current Status

OI-003 is governed by `OI-003.md`, not by the short `TECH-DEBT.md` entry.
`OI-003.md` records all workstreams and locked decisions as complete for the
current evidence set.

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

None queued under OI-003. Do not begin broad rewrites of commitlog, snapshot,
or executor internals unless a missing invariant is represented by a fresh
failing or contract-pinning test.

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

Pinned Staticcheck is available as `rtk go tool staticcheck ./...`. Use it for
static-analysis visibility, but do not treat a broad green run as required
until OI-008 cleanup clears the known findings and any dirty compile blockers.

Run `rtk go fmt` on touched Go packages before finishing.
