# Shunter → SpacetimeDB operational-parity roadmap

This document is the working development driver for turning Shunter from a substantial clean-room prototype into a private implementation that achieves the same operational outcomes as SpacetimeDB where it matters.

It is not a product roadmap.
It is not a marketing document.
It is a parity roadmap.

## Mission

Build a clean-room Go implementation that is operationally equivalent to SpacetimeDB where it matters:
- clients can use the same kinds of protocol flows and observe the same kinds of outcomes
- reducers, subscriptions, delivery, durability, reconnect, and recovery are close enough for real private use
- internal mechanisms may differ if the externally visible result is equivalent or consciously deferred

## What parity means here

Parity does not require identical source structure or identical internal algorithms.
Parity does require the same externally meaningful outcomes in these areas:
- protocol / wire behavior
- reducer execution behavior
- subscription behavior
- durability / recovery behavior
- schema / data model behavior

Guardrails:
- outcome-equivalence matters more than architecture similarity
- parity should be judged on named client-visible scenarios, not helper-level resemblance
- different externally visible behavior must be fixed or consciously deferred

## Current grounded status

Latest live repo state:
- `rtk go test ./...` → `Go test: 1712 passed in 11 packages`
- `rtk go build ./...` → `Go build: Success`
- major runtime packages are already implemented in live Go code
- `docs/parity-phase0-ledger.md` carries the scenario ledger
- `TECH-DEBT.md` carries the open backlog

Working verdict:
- architecture implementation: substantial and real
- planned subsystem presence: mostly complete
- parity with SpacetimeDB outcomes: partial
- trustworthiness for serious private use: not yet high enough

## The current gap, in one sentence

Shunter is much closer to “independent Go implementation of the same broad architecture” than to “operationally equivalent SpacetimeDB.”

## Source material for this roadmap

This roadmap is grounded primarily in:
- `TECH-DEBT.md`
- `docs/current-status.md`
- `docs/parity-phase0-ledger.md`
- `docs/parity-phase1.5-outcome-model.md`
- `docs/adr/2026-04-19-subscription-admission-model.md`
- live code under `protocol/`, `subscription/`, `executor/`, `commitlog/`, `store/`, `schema/`, `types/`, `bsatn/`

## Priority rule

When deciding what to do next, use this order:
1. externally visible parity gaps
2. correctness / concurrency bugs that can invalidate parity claims
3. capability gaps that prevent the same workloads from running
4. internal cleanup / duplication / ergonomics

Do not spend primary effort on cleanup before the parity target is nailed down.

# 1. Gap inventory by parity severity

## Tier A — Must close for serious parity claims

### A1. Protocol surface is not wire-close enough
Current grounded state:
- reference subprotocol token is preferred, but legacy `v1.bsatn.shunter` is still accepted
- compression tag numbering is parity-aligned, but brotli remains a deferred capability
- major envelope/message-family work is partly closed, but wire divergence remains visible

Main code surfaces:
- `protocol/options.go`
- `protocol/tags.go`
- `protocol/wire_types.go`
- `protocol/client_messages.go`
- `protocol/server_messages.go`
- `protocol/compression.go`
- `protocol/dispatch.go`
- `protocol/send_responses.go`
- `protocol/send_txupdate.go`
- `protocol/fanout_adapter.go`
- `protocol/upgrade.go`

### A2. Subscription/query model still diverges too much
Current grounded state:
- many narrow SQL/query parity slices are landed and pinned
- the supported SQL surface remains intentionally narrower than the reference SQL path
- the join/cross-join multiplicity batch is now closed across compile/hash identity, bootstrap, one-off execution, and delta evaluation
- one-off SQL now runs the shared `subscription.ValidatePredicate(...)` gate before snapshot evaluation, so unindexed join admission matches subscribe registration instead of bypassing join-index validation
- committed join bootstrap plus unregister final-delta rows now preserve projected-side enumeration order regardless of which join side provides the usable index, matching the existing one-off projected-side baseline for accepted join shapes
- post-commit projected join delta rows now preserve the same projected-side semantics on both projected-left and projected-right accepted join shapes: fragments are projected before reconciliation so partner churn cancels at the projected-row bag level, and `ReconcileJoinDelta(...)` no longer reorders surviving rows via map iteration; focused `subscription/delta_dedup_test.go` / `subscription/eval_test.go` pins lock the behavior
- accepted subscribe SQL using `:sender` now preserves caller-bound parameter provenance through compile/register hashing, so literal bytes queries no longer share a query hash/query-state identity with the parameterized caller form and mixed subscribe batches only parameterize the marked predicates
- accepted SQL with neutral `TRUE` terms now normalizes before runtime lowering and canonical hashing, so single-table `TRUE AND/OR ...` shapes share the same runtime identity as their simplified equivalents and join-backed `TRUE AND rhs-filter` shapes no longer fail later via malformed runtime filters
- accepted single-table same-table associative `AND` / `OR` SQL with 3+ leaves now canonicalizes grouping at the query-hash/query-state seam too, so left- vs right-associated trees no longer diverge solely because of parenthesization while parser/runtime semantics stay unchanged
- row-level security / per-client filtering remains absent
- subscription behavior still spans multiple seams rather than one fully parity-locked contract

Main code surfaces:
- `query/sql/parser.go`
- `subscription/predicate.go`
- `subscription/validate.go`
- `subscription/hash.go`
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`
- `executor/executor.go`
- `executor/scheduler.go`

### A3. Recovery/store behavior still differs in ways users can feel
Current grounded state:
- value model and changeset semantics remain simpler than the reference
- commitlog/recovery is still a clean-room rewrite, not format-compatible
- replay-horizon / validated-prefix parity slice is closed
- Phase 4 Slice 2 (2α offset-index, 2β typed error categorization, 2γ wire-shape divergence audit + canonical pin suite) is closed for the current phase framing
- carried-forward replay-edge and format-level deferrals now live under `TECH-DEBT.md` OI-007

Main code surfaces:
- `types/`
- `bsatn/encode.go`
- `bsatn/decode.go`
- `store/commit.go`
- `store/recovery.go`
- `store/snapshot.go`
- `store/transaction.go`
- `commitlog/changeset_codec.go`
- `commitlog/segment.go`
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/snapshot_io.go`
- `commitlog/compaction.go`

## Tier B — Must close for trustworthy private use

Current themes:
- protocol lifecycle ownership and shutdown hardening (`OI-004`)
- snapshot/read-view lifetime guarantees (`OI-005`)
- fanout aliasing/mutation risk (`OI-006`)
- bootstrap / top-level engine story (`OI-008`)

These are not the best first target if a stronger Tier A parity slice is available, but they matter for calling the runtime trustworthy.

## Tier C — Can wait until parity decisions are locked

Examples:
- broad cleanup and smell reduction
- structural refactors that do not change externally visible parity
- deeper ergonomics work once parity-critical seams are stable

# 2. Development strategy

## Principle 1: parity first, elegance second
Do not prefer internal neatness over externally visible parity closure.

## Principle 2: same outcome beats same mechanism
A different mechanism is acceptable when the public result is equivalent.

## Principle 3: every parity change needs an observable test
Every accepted or rejected parity shape should be pinned at the relevant admission/runtime seam.

## Principle 4: do not leave divergences implicit
If behavior intentionally differs, record that explicitly and keep it narrow.

# 3. Recommended execution order

## Phase 0 — Freeze the target and build the parity harness
Status: effectively complete for the current repo.

Key outputs already exist in:
- `docs/parity-phase0-ledger.md`
- `docs/parity-phase1.5-outcome-model.md`
- protocol/delivery/recovery parity pin suites

## Phase 1 — Wire-level protocol parity
Status: major core slices closed, remaining divergence tracked under `OI-001`.

Still-open direction:
- finish remaining protocol wire-close follow-through
- keep legacy-compatibility/deferred behavior explicit

## Phase 1.5 — First end-to-end delivery parity slice
Status: effectively closed for the current target.

Remaining deferral:
- `EnergyQuantaUsed` remains zero because there is no energy model

## Phase 2 — Query/subscription surface follow-through
Status: many narrow slices closed; broader parity still open.

What landed already:
- subscription envelope follow-through
- lag / slow-client policy parity slice (`docs/parity-phase2-slice3-lag-policy.md`)
- rows-shape documented-divergence close covering applied / light / committed envelopes (`docs/parity-phase2-slice4-rows-shape.md`)
- fan-out delivery parity for recipient-level durability gating plus dropped-client cleanup on eval failure
- many narrow SQL/query slices and rejection pins

What remains:
- broader query/subscription parity beyond the narrow landed shapes
- predicate normalization / validation drift and other remaining bounded A2 runtime/model gaps still need follow-on slices after the now-closed one-off-vs-subscribe join-index validation, committed join bootstrap/final-delta ordering, projected-join delta-ordering, `:sender` hash-identity, neutral-`TRUE` normalization, single-table commutative child-order, and single-table associative-grouping seams
- any future one-off widening should be deliberate, not accidental
- RLS/per-client filtering remains absent
- coordinated wrapper-chain + `BsatnRowList` close is a carried-forward deferral under `docs/parity-phase2-slice4-rows-shape.md` and SPEC-005 §3.4

## Phase 3 — Scheduler/reducer lifecycle parity
Status: current narrow startup/firing slice is closed.

What remains:
- explicit scheduler deferrals only if workload evidence surfaces
- avoid reopening closed narrow scheduler work without evidence

## Phase 4 — Recovery / commitlog / durability parity
Status: active multi-follow-on phase.

Current sub-slice state:
- `P0-RECOVERY-001` replay-horizon / validated-prefix slice: closed
- Phase 4 Slice 2α offset-index file: closed
- Phase 4 Slice 2β typed `Traversal` / `Open` error categories: closed
- Phase 4 Slice 2γ record/log shape parity: closed as a documented-divergence slice with a wire-shape pin suite; carried-forward deferrals remain open in `TECH-DEBT.md` OI-007

Current practical rule:
- do not reopen 2α / 2β / 2γ unless a regression or workload trigger appears
- treat the carried-forward 2γ deferrals as separate future decisions, not as hidden active work

## Phase 5 — Schema and capability parity
Status: selective capability widening only.

Current reading rule:
- only widen the model where workloads or named parity anchors justify it
- do not treat “reference supports more types” as sufficient by itself

## Phase 6 — Hardening and cleanup after parity direction is locked
Status: ongoing, but secondary to active Tier A parity where a stronger next slice exists.

Examples:
- lifecycle hardening after concrete leak evidence
- read-view/fanout ownership tightening
- cleanup and deduplication after parity-critical seams settle

# 4. Immediate execution guidance

When choosing the next slice:
1. check `NEXT_SESSION_HANDOFF.md` for the immediate active slice
2. confirm that the slice is still consistent with `docs/parity-phase0-ledger.md`
3. check `TECH-DEBT.md` for open themes that would outweigh it
4. stay narrow and pin behavior with tests

## Current best next direction

The best current narrow-ready direction is still predicate normalization / validation drift inside OI-002 A2, but the associative-grouping seam is now closed. A fresh post-fix scout points at same-table duplicate-leaf idempotence drift at the same canonical identity seam: a local probe now shows `hash(a) != hash(a AND a) != hash(a OR a)` even though `subscription.MatchRow(...)` returns identical booleans for sample matching and non-matching rows.

Current candidate directions are:
- accepted same-table duplicate-leaf SQL such as `a`, `a AND a`, and `a OR a`, whose user-visible row semantics are already identical but whose canonical query hash / query-state identity still diverges solely because redundant leaves survive into hashing
- another bounded OI-002 A2 runtime/model residual only if a fresh scout shows it is stronger than the duplicate-leaf idempotence seam
- Tier B hardening when a concrete live risk is stronger than the next parity slice
- a carried-forward 2γ deferral only if a workload trigger justifies opening a new decision doc
- scheduler/bootstrap follow-through only when workload or integration evidence surfaces

# 5. Reading rule for this document

Use this file for:
- prioritization
- phase framing
- parity-first execution order

Do not use it as a historical changelog.
For narrow implementation detail, read the linked ledger/decision docs instead.