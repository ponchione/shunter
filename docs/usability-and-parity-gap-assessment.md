# Shunter usability and parity gap assessment

Date: 2026-04-20

This document captures the current grounded assessment of what remains between Shunter's present state and:
- something actually usable as a coherent embedded runtime
- something closer to SpacetimeDB-level operational effectiveness

It is intended as a stable reference snapshot, not a marketing summary.

## Current grounded status

Live verification at assessment time:
- `rtk go test ./...` → `Go test: 1095 passed in 10 packages`
- `rtk go build ./...` → `Go build: Success`

Current repo reality:
- substantial implementation exists across `schema`, `store`, `commitlog`, `executor`, `subscription`, `protocol`, `query/sql`, `bsatn`, `auth`, and `types`
- there is still no `main` package
- there is no `cmd/` entrypoint
- there is no example/demo app
- `schema.Engine.Start(...)` is only a schema-compatibility preflight, not a real runtime bootstrap

Grounding references:
- `README.md`
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md`
- `docs/parity-phase0-ledger.md`
- `TECH-DEBT.md`
- `schema/version.go`

## Executive summary

Shunter is already a real implementation, not a docs-only or speculative project.

The architecture is substantially present:
- in-memory store
- commit log + recovery
- serialized executor
- subscription delta fan-out
- persistent WebSocket protocol surface

But the project is still materially short of two separate targets:

### 1. "Actually usable"
This means a coherent embedded runtime that a developer can bootstrap, run, and trust for private experimental use without stitching the whole system together manually.

Verdict:
- moderately close structurally
- not yet close enough operationally/confidence-wise

### 2. "SpacetimeDB-level effectiveness"
This means operational equivalence in the client-visible sense defined in the parity roadmap:
- protocol behavior
- reducer/caller semantics
- subscription semantics
- durability/recovery semantics
- schema/data-model behavior

Verdict:
- still meaningfully far away
- much closer to "same broad architecture" than to "same effective runtime behavior"

## What remains to get to something actually usable

These are the minimum remaining gaps for a coherent private-use runtime.

### U1. A real engine/bootstrap surface

Current state:
- no single obvious runtime constructor/bootstrap flow
- no `main` package or `cmd/` entrypoint
- no example app
- `schema.Engine.Start(...)` only checks schema compatibility

Why it matters:
- the repo contains strong subsystems, but not yet one cohesive developer-facing runtime surface
- this is the biggest blocker to practical usability even though the internals are substantial

Needed:
- one obvious engine/bootstrap API
- one real example app or demo server
- one documented flow for: define schema → start runtime → connect client → run reducers → observe updates
- one owner for startup/shutdown/recovery/protocol lifecycle

Primary grounding:
- `README.md`
- `schema/version.go`
- `TECH-DEBT.md` OI-008

### U2. Protocol lifecycle hardening

Current state:
- current docs still call out protocol lifecycle races and unsafe close/channel behavior as a major open risk

Why it matters:
- even if tests pass, connection lifecycle bugs are exactly the class of issue that make a runtime feel untrustworthy under real usage

Needed:
- clearer goroutine ownership
- stricter shutdown/close/reconnect invariants
- less dependence on detached background lifecycle behavior

Primary grounding:
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `TECH-DEBT.md` OI-004

### U3. Snapshot/read-view lifetime safety

Current state:
- snapshot/read-view lifetime discipline is still treated as a sharp edge in current docs

Why it matters:
- if correctness depends too heavily on caller discipline around read-view lifetime, the runtime is not yet trustworthy

Needed:
- stronger safety around read-view/snapshot ownership and lifetime
- fewer ways to accidentally hold resources too long or distort concurrency assumptions

Primary grounding:
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `TECH-DEBT.md` OI-005

### U4. Recovery and replay closure for trustworthiness

Current state:
- recovery code exists and passes tests
- but key parity scenarios remain only `in_progress`

Open scenarios:
- `P0-SCHED-001` scheduled reducer startup replay ordering
- `P0-RECOVERY-001` replay horizon and validated-prefix behavior

Why it matters:
- a runtime is not really usable if restart/crash behavior still feels provisional or under-specified

Needed:
- explicit decisions on replay tolerance vs fail-fast behavior
- explicit startup ordering semantics for scheduled reducers
- parity tests that close these scenarios decisively

Primary grounding:
- `docs/parity-phase0-ledger.md`
- `TECH-DEBT.md` OI-007

## What remains to get closer to SpacetimeDB-level effectiveness

These are the major parity gaps still separating Shunter from an operational-equivalence claim.

### P1. Wire/protocol parity is still incomplete

Current state:
- many protocol slices are already closed
- but the protocol surface is still not fully wire-close to SpacetimeDB

Still-open differences include:
- legacy `v1.bsatn.shunter` subprotocol still accepted
- brotli still recognized-but-unsupported
- some remaining envelope/message-family divergences

Why it matters:
- if the protocol still feels custom, the runtime is not yet truly operationally equivalent even when some message shapes are close

Primary grounding:
- `docs/spacetimedb-parity-roadmap.md` Tier A1
- `TECH-DEBT.md` OI-001

### P2. Query and subscription parity is still one of the biggest gaps

Current state:
- many narrow SQL-backed parity slices are already landed
- but the query/subscription model still diverges in important ways

Still-open differences include:
- the SQL surface is still intentionally narrow
- Shunter still uses a Go-predicate-oriented model internally rather than matching a broader reference query surface
- row-level security / per-client filtering is absent
- lag/backpressure behavior differs
- caller-result / confirmed-read ownership still crosses seams differently than the ideal parity-tested contract
- scheduler timing remains parity-visible when workloads depend on it

Why it matters:
- this is one of the biggest reasons the repo is still closer to “same architecture” than “same effect”

Primary grounding:
- `docs/spacetimedb-parity-roadmap.md` Tier A2
- `docs/parity-phase0-ledger.md`
- `TECH-DEBT.md` OI-002

### P3. Store/recovery/data-model parity is still incomplete

Current state:
- architecture is present
- but store and recovery semantics are still simpler or different in user-visible ways

Current documented differences include:
- simpler value model
- thinner changeset metadata
- simpler primary-key / auto-increment model
- rewritten commitlog/recovery format rather than compatibility
- no offset index file
- different replay strictness
- different TxID origin

Why it matters:
- these differences are not cosmetic; they directly affect recovery expectations and runtime behavior after faults

Primary grounding:
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier A3
- `TECH-DEBT.md` OI-003

### P4. Hardening still matters even after parity direction is chosen

Current state:
- roadmap Tier B explicitly says there are still non-trivial correctness hazards

Current major themes:
- protocol lifecycle races and unsafe channel-close behavior
- snapshot / read-view lifetime hazards
- subscription fanout aliasing risk
- recovery / RowID / nextID sharp edges
- inconsistent error surfaces

Why it matters:
- these are not polish items; they directly reduce trust in any parity claim

Primary grounding:
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/current-status.md`
- `TECH-DEBT.md` OI-004 through OI-007

## Distance assessment

This section intentionally uses rough qualitative estimates, not fake precision.

### Distance to "actually usable"

Best interpretation:
- the necessary subsystems already exist
- build and test are green
- the remaining work is mostly integration, hardening, and developer-surface coherence

Assessment:
- structurally: moderately close
- operationally/confidence-wise: still meaningfully unfinished

Rough estimate:
- about 60–70% of the way there structurally
- about 40–50% of the way there operationally/confidence-wise

Interpretation:
- the system pieces exist
- trusting them together as one coherent runtime still needs non-trivial work

### Distance to SpacetimeDB-level effectiveness

Assessment:
- architecture similarity: high
- outcome parity: partial
- trust/effectiveness parity: still significantly behind

Rough estimate:
- architecture: roughly 75–85% there
- operational parity: roughly 40–55% there
- true “same effectiveness” in practice: roughly 25–40% there

Interpretation:
- Shunter is already a real clean-room implementation of the broad concept
- it is not yet close enough to claim the same client-visible operational effectiveness

## What should happen next, in order

If the goal is to turn the current codebase into something both usable and meaningfully closer to SpacetimeDB effect, the best sequence is:

### Phase A. Build one coherent runtime/bootstrap story
- create one top-level engine/bootstrap API
- wire store + executor + commitlog + protocol into one obvious runtime surface
- add one example/demo app
- document one real embedding path

### Phase B. Harden lifecycle and read-view correctness
- protocol lifecycle ownership
- shutdown/close/reconnect invariants
- snapshot/read-view lifetime guarantees

### Phase C. Close the two remaining in-progress parity scenarios
- scheduled reducer startup replay ordering (`P0-SCHED-001`)
- replay horizon / validated-prefix behavior (`P0-RECOVERY-001`)

### Phase D. Expand query/subscription parity
- broader SQL/query-surface support where it materially affects workloads
- tighter subscription semantics
- closer lag/backpressure behavior
- decide whether row-level/per-client filtering belongs in the parity target

### Phase E. Tighten store/recovery semantics
- replay behavior
- sequence/TxID invariants
- snapshot guarantees
- changeset/data-model parity where it actually affects workloads

### Phase F. Only then do broad cleanup and polish
- duplication cleanup
- API smoothing
- docs cleanup
- non-semantic refactors

## Bottom line

Shunter is already a legitimate clean-room implementation effort with substantial real code.

But there is still a large gap between:
- “the core subsystems exist and pass tests”
and
- “this is a coherent, trustworthy, SpacetimeDB-level effective runtime.”

The project is no longer blocked on architecture.
It is blocked on:
- integration into one usable runtime surface
- lifecycle and recovery hardening
- closure of remaining client-visible parity gaps

That is why it feels close and far away at the same time.

## Reference checklist

Use this when revisiting progress:

Questions to ask:
- Is there now a real bootstrap/runtime entry surface?
- Is there now a demo/example proving end-to-end usage?
- Are protocol lifecycle and read-view hazards materially reduced?
- Are `P0-SCHED-001` and `P0-RECOVERY-001` closed?
- Has the SQL/query surface moved beyond the currently narrow parity slices?
- Are wire/protocol divergences now small enough to stop mattering in practice?
- Are recovery/store semantics close enough that restart/fault behavior feels trustworthy?

If most of those are still “no,” then Shunter is still a substantial prototype rather than a truly usable replacement.
