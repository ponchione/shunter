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
- `rtk go test ./...` → `Go test: 1770 passed in 11 packages`
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
- one-off SQL now uses a query/ad hoc validation seam before snapshot evaluation, so unindexed two-table joins scan and return projected rows without requiring subscription indexes; subscribe registration still uses `subscription.ValidatePredicate(...)` and still rejects unindexed joins
- committed join bootstrap plus unregister final-delta rows now preserve projected-side enumeration order regardless of which join side provides the usable index, matching the existing one-off projected-side baseline for accepted join shapes
- post-commit projected join delta rows now preserve the same projected-side semantics on both projected-left and projected-right accepted join shapes: fragments are projected before reconciliation so partner churn cancels at the projected-row bag level, and `ReconcileJoinDelta(...)` no longer reorders surviving rows via map iteration; focused `subscription/delta_dedup_test.go` / `subscription/eval_test.go` pins lock the behavior
- accepted subscribe SQL using `:sender` now preserves caller-bound parameter provenance through compile/register hashing, so literal bytes queries no longer share a query hash/query-state identity with the parameterized caller form and mixed subscribe batches only parameterize the marked predicates
- accepted SQL with neutral `TRUE` terms now normalizes before runtime lowering and canonical hashing, so single-table `TRUE AND/OR ...` shapes share the same runtime identity as their simplified equivalents and join-backed `TRUE AND rhs-filter` shapes no longer fail later via malformed runtime filters
- accepted single-table same-table associative `AND` / `OR` SQL with 3+ leaves now canonicalizes grouping at the query-hash/query-state seam too, so left- vs right-associated trees no longer diverge solely because of parenthesization while parser/runtime semantics stay unchanged
- accepted single-table same-table duplicate-leaf `AND` / `OR` SQL now canonicalizes idempotent redundant leaves at that same query-hash/query-state seam, so `a`, `a AND a`, and `a OR a` share one canonical query hash and one shared query state while one-off row semantics stay unchanged
- accepted single-table same-table absorption-equivalent `AND` / `OR` SQL now canonicalizes bounded absorption-law variants at that same query-hash/query-state seam, so `a OR (a AND b)` and `a AND (a OR b)` share one canonical query hash and one shared query state with `a` while one-off row semantics stay unchanged
- overlength SQL is now rejected before recursive parse/compile work on subscribe single, subscribe multi, and one-off query paths via a shared 50,000-byte parser guard, matching the reference's explicit protection against deeply nested boolean-query stack blowups
- reference-backed bare and grouped `FALSE` predicates now follow through coherently on the already-supported SQL surface: parser acceptance, subscribe lowering, one-off execution, canonical hashing, validation, bootstrap, and single-table delta evaluation all share an explicit `subscription.NoRows` runtime meaning
- accepted distinct-table joins whose same-table filter leaves differ only by commutative child order now also share one canonical query hash / query-state identity: `subscription/hash.go` canonicalizes `Join.Filter` for distinct-table joins while preserving join structure (`Left`/`Right`, join columns, aliases, projection side)
- accepted aliased self-joins whose same-side filter differs only by commutative child order now also share one canonical query hash / query-state identity: `subscription/hash.go` now applies a bounded alias-aware child-order canonicalization inside self-join `Join.Filter` without enabling the broader alias-blind same-table flatten/dedupe/absorb pipeline
- accepted aliased self-joins whose same-side filter differs only by associative grouping now also share one canonical query hash / query-state identity: `subscription/hash.go` now flattens and deterministically rebuilds same-kind self-join filter groups in an alias-aware way while keeping duplicate-leaf / absorption reductions fenced for later slices
- accepted aliased self-joins whose same-side filter differs only by exact duplicate leaves now also share one canonical query hash / query-state identity: `subscription/hash.go` now dedupes byte-identical alias-aware self-join filter children after the self-join-local flatten/sort step, so `a`, `a AND a`, and `a OR a` no longer fork query-state identity while one-off row semantics stay unchanged
- accepted aliased self-joins whose same-side filter differs only by bounded absorption-equivalent shapes now also share one canonical query hash / query-state identity: `subscription/hash.go` now applies a bounded self-join-local absorption pass after flatten/sort/dedupe, so `a`, `a OR (a AND b)`, and `a AND (a OR b)` no longer fork query-state identity while alias identity remains encoded in canonical child bytes
- one-off SQL now accepts a narrow trailing `LIMIT <unsigned int>` on the already-supported row-projection surface while subscribe still rejects `LIMIT`: `query/sql/parser.go` carries optional limit metadata, `protocol/handle_subscribe.go` blocks it deliberately on subscribe admission, and `protocol/handle_oneoff.go` applies the cap after existing row evaluation
- one-off SQL now also accepts bounded single-table explicit column-list projections such as `SELECT u32 FROM t` and `SELECT u32, active FROM t WHERE ...` while subscribe still rejects non-`*` projections: `query/sql/parser.go` carries resolved projection metadata, `protocol/handle_subscribe.go` validates schema-backed projection columns and rejects the shape deliberately for subscribe callers, and `protocol/handle_oneoff.go` projects rows only after the existing row-match + optional `LIMIT` path
- one-off SQL now also accepts bounded query-only `COUNT(*) [AS] alias` on already-supported single-table shapes while subscribe still rejects aggregate projections: `query/sql/parser.go` carries aggregate metadata for both `SELECT COUNT(*) AS n FROM t` and reference-style `SELECT COUNT(*) n FROM t`, still rejects missing aliases, `protocol/handle_subscribe.go` rejects parsed aggregates deliberately for subscribe callers while building a one-column uint64 result shape for one-off callers, and `protocol/handle_oneoff.go` returns exactly one count row even when the matched input is empty
- one-off SQL now also accepts bounded explicit projection-column aliases on the existing single-table column-list projection surface while subscribe still rejects non-`*` projections: `query/sql/parser.go` preserves source qualifier routing separately from output alias metadata, `protocol/handle_subscribe.go` renames one-off output-column schemas without disturbing base-column indexes, and `protocol/handle_oneoff.go` continues projecting row values against the alias-aware compiled schema
- one-off SQL now also accepts bounded join-backed explicit column-list projections on the existing two-table join surface while subscribe still rejects non-`*` projections: `query/sql/parser.go` now admits qualified join-side column lists and fences them to one projected relation instance, `protocol/handle_subscribe.go` threads schema-backed projection metadata through join compilation, and `protocol/handle_oneoff.go` reuses the existing post-join row-shaping seam so join results are projected after join evaluation and optional `LIMIT`
- one-off SQL now also accepts the bounded query-only cross-join `WHERE` column-equality shape `SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32`: the parser carries a qualified column-vs-column predicate node, one-off compiles the exact shape into the existing join evaluator, and subscribe still rejects cross-join `WHERE` before executor registration
- one-off SQL now also accepts the next bounded cross-join `WHERE` follow-through with one qualified column-literal filter, for example `SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32 AND s.enabled = TRUE`: the parser keeps the column equality and filter in one predicate tree, one-off lowers it into an existing `subscription.Join` plus `Join.Filter`, and subscribe still rejects cross-join `WHERE` before executor registration
- one-off SQL now also accepts the bounded combination of cross-join `WHERE` column-equality (with optional one qualified column-literal filter) and join-backed `COUNT(*) [AS] alias` aggregate projection, for example `SELECT COUNT(*) AS n FROM t JOIN s WHERE t.id = s.t_id AND s.active = TRUE`: the parser preserves aggregate metadata on the cross-join WHERE shape, one-off routes the query through the same `subscription.Join` evaluator with matched-pair counting into a single uint64 aggregate row, and subscribe still rejects (aggregate-projection guard fires before the cross-join `WHERE` guard) before executor registration
- one-off SQL now also accepts bounded join-backed query-only `COUNT(*) [AS] alias` on the existing two-table join surface: the parser carries aggregate metadata on joins, one-off counts matched join rows with multiplicity into a one-column uint64 result using the requested alias, and subscribe still rejects aggregate projections before executor registration
- one-off SQL now also accepts bounded mixed-relation explicit column projections on the existing two-table join surface: parser/compile metadata preserves each projected column's source relation, one-off row shaping can pull from both sides of the matched join pair, and subscribe still rejects column-list projections before executor registration
- post-commit evaluation-origin `SubscriptionError` delivery now honors the same confirmed-read durability gate as normal transaction updates for default/public recipients; errors still precede normal updates for the same batch after `TxDurable` is ready
- post-commit fanout now stabilizes each connection's update slice by internal subscription-registration/SubscriptionID order before fanout/caller capture, removing Go map-iteration drift from multi-subscription `TransactionUpdate` payloads
- subscription update fanout/protocol correlation now uses the client-chosen `QueryID` end-to-end: registration metadata stores it, eval/initial/final deltas stamp it, protocol encode/decode carries it, and manager-internal `SubscriptionID` no longer appears in `protocol.SubscriptionUpdate`
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
- broader query/subscription parity beyond the narrow landed shapes (now after the closed fan-out delivery, multiplicity, one-off-vs-subscribe join-index-validation split, committed bootstrap/final-delta projected-ordering, projected-join delta-ordering, `:sender` hash-identity, neutral-`TRUE` normalization, single-table commutative child-order canonicalization, single-table associative-grouping canonicalization, single-table duplicate-leaf idempotence canonicalization, single-table absorption-law canonicalization, overlength-SQL admission guard, bare/grouped `FALSE` predicate follow-through, distinct-table join-filter child-order canonicalization, self-join alias-sensitive join-filter child-order canonicalization, self-join alias-sensitive join-filter associative-grouping canonicalization, self-join alias-sensitive join-filter duplicate-leaf idempotence canonicalization, self-join alias-sensitive join-filter absorption-law reduction, one-off query `LIMIT` support on the existing row-projection surface with subscribe-side rejection retained, one-off-only single-table column-list projection support with subscribe-side rejection retained, one-off-only `COUNT(*) [AS] alias` support with subscribe-side rejection retained, one-off-only explicit projection-column alias support with subscribe-side rejection retained, one-off-only join-backed explicit column-list projection support with subscribe-side rejection retained, one-off/ad hoc unindexed two-table join admission with subscribe-side rejection retained, one-off-only cross-join `WHERE` column-equality admission with subscribe-side rejection retained, one-off-only join-backed `COUNT(*) [AS] alias` aggregate projection with subscribe-side rejection retained, one-off-only mixed-relation explicit join-column projection with subscribe-side rejection retained, one-off/subscribe JOIN ON equality-plus-single-relation-filter widening, confirmed-read `SubscriptionError` durability gating, deterministic per-connection fanout update ordering, and client QueryID fanout/protocol correlation)
- pick the next bounded A2 residual only after a fresh scout; the pre-projection, unindexed-join, cross-join `WHERE` equality, cross-join `WHERE` equality-plus-filter, cross-join `WHERE` + `COUNT(*)` aggregate combination, join-backed count aggregate, JOIN ON filter, SubscriptionError durability-gating, per-connection ordering, and QueryID fanout/protocol targets are now closed
- any future one-off widening should be deliberate, not accidental

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

The cross-join `WHERE` + `COUNT(*)` aggregate combination slice is now closed too. The best current direction is a fresh bounded OI-002 scout to identify the next residual with live code/doc evidence rather than carrying forward now-closed projection-family, unindexed-join, cross-join `WHERE` equality/equality-plus-filter, cross-join `WHERE` + `COUNT(*)` combination, join-backed count aggregate, JOIN ON filter, SubscriptionError durability-gating, per-connection ordering, or QueryID fanout/protocol targets.

Current candidate directions are:
- fresh OI-002 scout for the next bounded query/subscription residual
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