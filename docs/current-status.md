# Shunter current status

This file is the short answer to: what is actually here, how complete is it, and what still materially differs from SpacetimeDB.

## Short version

Shunter is no longer a docs-only clean-room exercise.
It is a substantial Go implementation with working subsystem code, passing tests, and a live parity/hardening backlog.

Best current description:
- implementation-present
- architecture-proven enough to keep building on
- not parity-complete with SpacetimeDB
- not fully hardened for serious private use yet

## Grounded evidence

Latest recorded broad verification in the current repo docs:
- full suite baseline: `Go test: 1444 passed in 10 packages`
- broad build verification: `rtk go build ./...` recorded as clean in the current-status / roadmap family of docs
- live implementation spans the core runtime packages (`types`, `bsatn`, `schema`, `store`, `commitlog`, `executor`, `subscription`, `protocol`, `query/sql`)
- `TECH-DEBT.md` is the open-issues tracker
- `docs/parity-phase0-ledger.md` is the scenario ledger
- `docs/spacetimedb-parity-roadmap.md` is the execution/prioritization guide

## Completion by lens

### 1. Execution-order completion
Status: major subsystem implementation is present

The repo already contains substantial code for:
- commit log / replay / recovery / compaction
- protocol delivery / reconnect / close behavior
- subscription registration, evaluation, and fanout
- executor and scheduler paths

The question is no longer “is there code for the planned subsystems?”
The answer there is mostly yes.

### 2. Operational completeness
Status: substantial prototype / runtime

The broad suite is green in the latest recorded baseline, which is strong evidence that the repo is operationally real.
That is not the same thing as parity-complete or fully hardened.

### 3. Spec and parity completeness
Status: partial, still being reconciled

The repo has moved past missing-subsystem work and into behavior reconciliation:
- live protocol and delivery parity are partly closed and partly deferred
- SQL/query parity has many narrow landed slices, but the surface is still intentionally narrower than the reference
- recovery/store parity still has active follow-ons
- hardening work remains concentrated in lifecycle/read-view/fanout themes

## Biggest current differences from SpacetimeDB

### Protocol / query surface
- legacy dual-subprotocol admission is still accepted as a compatibility deferral
- brotli remains recognized-but-unsupported
- one-off and subscription admission still share Shunter's narrower subscription-shaped SQL surface
- row-level security / per-client filtering remains absent

### Store / value model
- value model is still simpler overall than the reference
- no full composite/product parity model
- primary-key / auto-increment behavior remains simpler in important ways
- several column-kind widening slices landed (`Int128`, `Uint128`, `Int256`, `Uint256`, `Timestamp`, `ArrayString`), but that does not make the full model reference-equivalent

### Commitlog / recovery
- commitlog/recovery remains a clean-room rewrite, not format-compatible
- replay-horizon / validated-prefix behavior is closed as a parity slice
- Phase 4 Slice 2α offset-index work is closed
- Phase 4 Slice 2β typed error categorization is the current follow-on
- format-level record/log parity remains deferred after that

### Executor / scheduling
- bounded inbox model differs from the reference runtime model
- scheduled-reducer startup / firing ordering slice is closed, but some scheduler deferrals remain explicit

### Hardening
- protocol lifecycle still has open ownership/shutdown concerns (`OI-004`)
- snapshot/read-view lifetime remains an open theme even after many enumerated sub-hazards were closed (`OI-005`)
- fanout aliasing/mutation risk remains an open theme even after the narrow known sub-hazards were closed (`OI-006`)

## Current live priorities

Use this order:
1. externally visible parity gaps
2. correctness / concurrency bugs that undermine parity claims
3. capability gaps that block realistic usage
4. cleanup after parity direction is locked

In concrete repo terms, the main live drivers are:
- `TECH-DEBT.md` for open issues
- `docs/parity-phase0-ledger.md` for pinned scenario status
- `docs/spacetimedb-parity-roadmap.md` for execution order and phase framing
- `NEXT_SESSION_HANDOFF.md` for the immediate current slice only

## Best verdict

If the question is:

### “Is Shunter real?”
Yes.

### “Has the planned architecture been substantially implemented?”
Yes.

### “Is it already a close SpacetimeDB clone?”
No. It is closer at the architectural level than at the protocol/behavioral level.

### “Is it done enough to stop auditing?”
No.

## Practical recommendation

Treat the repo as a substantial private prototype with real implementation depth and an active parity/hardening follow-through phase.
Do not treat it as either:
- a fake research artifact, or
- a finished SpacetimeDB-equivalent runtime

For the concrete development driver, read:
1. `docs/spacetimedb-parity-roadmap.md`
2. `docs/parity-phase0-ledger.md`
3. `TECH-DEBT.md`
4. `NEXT_SESSION_HANDOFF.md`