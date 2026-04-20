# Shunter → SpacetimeDB operational-parity roadmap

This document is the working development driver for turning Shunter from a substantial clean-room prototype into a private implementation that achieves the same operational outcomes as SpacetimeDB.

It is not a product roadmap.
It is not a marketing document.
It is a parity roadmap.

## Mission

Build a clean-room Go implementation that is operationally equivalent to SpacetimeDB where it matters:
- clients can interact with it through the same kinds of protocol flows and observe the same kinds of outcomes
- reducers, subscriptions, transaction updates, durability, reconnect, and recovery behave closely enough that the system can stand in for SpacetimeDB in your own usage
- internal implementation choices may differ, as long as the externally observable result is equivalent or intentionally and explicitly deferred

## What “parity” means here

Parity does not require identical source structure or identical internal algorithms.
Parity does require the same externally meaningful outcomes in these areas:

1. Protocol / wire behavior
- subprotocol negotiation
- message tags and payload meanings
- compression behavior
- close codes / reconnect semantics
- reducer-call and query request/response behavior

2. Reducer execution behavior
- call acceptance/rejection
- success/failure surfaces
- caller-visible metadata
- scheduling and lifecycle semantics

3. Subscription behavior
- query surface and grouping model
- initial subscribe behavior
- transaction-update fanout
- caller suppression rules
- lag / backpressure behavior
- confirmed-read / durability visibility

4. Durability / recovery behavior
- transaction numbering
- log framing and replay behavior
- snapshot semantics
- compaction / recovery expectations

5. Schema / data model behavior
- supported value kinds and row encoding implications
- primary-key / auto-increment behavior
- system-table behavior
- developer-facing schema registration outcomes

Two guardrails matter during execution:
- outcome-equivalence matters more than architecture similarity; a shared high-level design is not enough
- parity should be judged on named client-visible scenarios, not on helper-level resemblance

The minimum scenario set this roadmap should keep in view is:
- connect → negotiate protocol → subscribe → call reducer → observe caller/non-caller results → disconnect/reconnect
- schedule reducer → fire at the intended time → restart/recover → verify post-restart scheduling behavior
- crash/restart/replay after committed writes with auto-increment / nextID / TxID invariants preserved

If Shunter chooses a different internal mechanism but still produces the same externally meaningful result, that is acceptable.
If Shunter produces a different externally meaningful result, that is not parity and must be either fixed or consciously deferred.

## Current grounded status

Broad evidence from the current audit pass:
- `rtk go test ./...` passes: `919 passed in 9 packages`
- implementation footprint: `209` Go files, `34807` lines of Go code in the main package pass (`auth`, `bsatn`, `commitlog`, `executor`, `protocol`, `schema`, `store`, `subscription`, `types`)
- the big execution-order implementation slices for commitlog recovery, protocol delivery, and subscription fanout are already present in live code
- `TECH-DEBT.md` still carries unresolved hardening and contract issues
- `docs/parity-phase0-ledger.md` and `docs/parity-phase1.5-outcome-model.md` record the parity decisions and pinned scenarios already closed

Working verdict:
- architecture implementation: substantial and real
- planned subsystem presence: mostly complete
- parity with SpacetimeDB outcomes: partial
- trustworthiness for serious private use: not yet high enough

## The current gap, in one sentence

Shunter is much closer to “independent Go implementation of the same broad architecture” than to “operationally equivalent SpacetimeDB.”

## Source material for this roadmap

This roadmap is grounded primarily in:
- `TECH-DEBT.md` — live code hardening / correctness backlog
- `docs/current-status.md` — current blunt summary
- `docs/parity-phase0-ledger.md` — parity scenario ledger and pinned tests
- `docs/parity-phase1.5-outcome-model.md` — heavy/light delivery outcome decision and remaining deferrals
- `docs/adr/2026-04-19-subscription-admission-model.md` — current subscription-admission authority decision
- live packages under `protocol/`, `subscription/`, `executor/`, `commitlog/`, `store/`, `schema/`, `types/`, `bsatn/`

## Priority rule

When deciding what to do next, use this order:

1. externally visible parity gaps
2. correctness / concurrency bugs that can invalidate parity claims
3. capability gaps that prevent the same workloads from running
4. internal cleanup / duplication / ergonomics

Do not spend primary effort on cleanup before the parity target is nailed down.

---

# 1. Gap inventory by parity severity

## Tier A — Must close for serious parity claims

These currently prevent Shunter from being credibly described as an operational SpacetimeDB implementation.

### A1. Protocol surface is not wire-close enough
Grounded current protocol divergences and closures:
- subprotocol token: `v1.bsatn.spacetimedb` preferred; `v1.bsatn.shunter` still accepted (Phase 1 closed with a legacy-compatibility deferral)
- compression tags: tag numbering parity-aligned (Phase 1 closed; brotli is a recognized-but-deferred tag)
- `TransactionUpdate` heavy/light split: closed Phase 1.5
- `SubscribeMulti` / `SubscribeSingle` variant split + one-QueryID-per-query-set grouping: closed Phase 2 Slice 2
- `CallReducer.flags` (`FullUpdate` / `NoSuccessNotify`): closed Phase 1.5
- one-off query SQL-string flip: closed Phase 2 Slice 1b (2026-04-19)
- one-off query `message_id: Box<[u8]>` wire-shape parity: closed Phase 2 Slice 1c (`OneOffQueryMsg.MessageID []byte` / `OneOffQueryResult.MessageID []byte`)
- close-code behavior: closed Phase 1
- reducer-call result status enum: closed Phase 1.5 (`UpdateStatus` tagged union)

Impact:
- client interoperability and client-side expectation parity are poor
- even when semantics are “similar,” the protocol is visibly not SpacetimeDB-like

Primary code surfaces:
- `protocol/options.go`
- `protocol/tags.go`
- `protocol/wire_types.go`
- `protocol/client_messages.go`
- `protocol/server_messages.go`
- `protocol/compression.go`
- `protocol/sender.go`
- `protocol/dispatch.go`
- `protocol/send_responses.go`
- `protocol/async_responses.go`
- `protocol/send_txupdate.go`
- `protocol/send_reducer_result.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_unsubscribe.go`
- `protocol/handle_oneoff.go`
- `protocol/fanout_adapter.go`
- `protocol/conn.go`
- `protocol/close.go`
- `protocol/disconnect.go`
- `protocol/lifecycle.go`
- `protocol/upgrade.go`

### A2. Subscription/query model still diverges too much
Grounded current subscription/runtime-visible divergences:
- Go predicate builder instead of SQL subset surface
- bounded disconnect-on-lag fanout policy differs from SpacetimeDB’s queueing/slow-client model
- no row-level security / per-client filtering model
- message-routing seam differs for durability metadata
- caller-result / confirmed-read behavior currently crosses subscription, executor, and protocol seams instead of living behind one parity-tested contract
- scheduled-reducer timing and startup ordering remain parity-visible gaps when scheduled reducers matter to the workload

Impact:
- Shunter may be architecturally similar while still not behaving like SpacetimeDB under real subscription workloads
- current query shape limits parity of client behavior

Primary code surfaces:
- `subscription/predicate.go`
- `subscription/validate.go`
- `subscription/hash.go`
- `subscription/register.go`
- `subscription/unregister.go`
- `subscription/eval.go`
- `subscription/manager.go`
- `subscription/query_state.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `subscription/delta_single.go`
- `subscription/delta_join.go`
- `subscription/delta_dedup.go`
- `subscription/delta_view.go`
- `protocol/fanout_adapter.go`
- `executor/executor.go`
- `executor/scheduler.go`
- `executor/scheduler_worker.go`
- `executor/lifecycle.go`

### A3. Recovery/store behavior still differs in ways users can feel
Grounded current store/commitlog/runtime-visible divergences:
- Shunter value model is simpler
- Shunter commitlog/recovery is a rewrite and not format-compatible
- no offset index file
- replay strictness differs
- TxID origin differs
- auto-increment sequencing model is simpler

Impact:
- equivalent workloads may not recover, replay, or encode/decode the same way
- storage semantics are not yet strong enough for “same operational outcome” claims
- some of the biggest remaining hazards are ownership and sequencing hazards across executor + store + commitlog, not just isolated file-local differences

Primary code surfaces:
- `types/`
- `bsatn/encode.go`
- `bsatn/decode.go`
- `executor/executor.go`
- `executor/lifecycle.go`
- `store/commit.go`
- `store/recovery.go`
- `store/snapshot.go`
- `store/transaction.go`
- `commitlog/changeset_codec.go`
- `commitlog/segment.go`
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/snapshot_select.go`
- `commitlog/snapshot_io.go`
- `commitlog/compaction.go`

## Tier B — Must close for trustworthy private use, even if parity direction is already correct

Open `TECH-DEBT.md` items show non-trivial correctness hazards.
The most important current themes are:
- protocol lifecycle races and unsafe channel-close behavior
- snapshot / read-view lifetime hazards
- subscription fanout aliasing risk
- recovery / RowID / nextID sharp edges
- inconsistent error surfaces that make the library harder to embed safely

These are not “polish.” They weaken the credibility of any parity claim.

## Tier C — Can wait until parity decisions are locked

These matter, but should not drive sequence yet:
- broad duplication cleanup
- enum stringers
- API cosmetic smoothing that does not change semantics
- deeper refactors of internal helpers before parity target is nailed down

---

# 2. Development strategy

## Principle 1: parity first, elegance second

If a subsystem is internally cleaner than SpacetimeDB but produces meaningfully different outcomes, prefer parity unless there is a specific reason not to.

## Principle 2: same outcome beats same mechanism

Examples:
- SQL does not need to be implemented with the same parser architecture, but the accepted query surface and resulting behavior should converge
- durability metadata can flow through a different seam internally if the client-observable effect matches

## Principle 3: every parity change needs an observable test

For each parity slice, add or tighten tests at the external contract boundary:
- protocol frame/message tests
- end-to-end reducer-call behavior tests
- subscribe / unsubscribe / reconnect tests
- recovery / replay tests
- snapshot and sequencing tests

## Principle 4: do not leave divergences implicit

For each gap, choose exactly one:
- close it now
- consciously defer it with a reason
- keep the divergence intentionally because it does not affect your required operational outcome

No silent drift.

---

# 3. Recommended execution order

## Phase 0 — Freeze the target and build the parity harness

### Goal
Create the machinery that lets Shunter measure itself against an outcome-based parity target instead of vague architectural similarity.

### Why first
Without a parity harness, later work will devolve into intuition and local cleanup.

### Deliverables
1. A parity-tracking ledger in this document or an adjacent appendix
   - status per gap: open / in progress / closed / intentionally deferred
   - current companion ledger: `docs/parity-phase0-ledger.md`
2. A protocol conformance bucket built by formalizing the strongest existing protocol tests
   - handshake / subprotocol expectations
   - message-tag and envelope expectations
   - close-code expectations
   - compression-envelope expectations
3. A small end-to-end parity scenario bucket
   - connect → subscribe → reducer call → caller/non-caller delivery → disconnect / reconnect
   - scheduled reducer firing and startup replay ordering
   - snapshot + replay + recovery with TxID / nextID / sequence invariants checked
4. A clear “reference outcome” note for each major gap
   - what exact externally observable behavior is being matched

### Files to add or modify
- update: `docs/spacetimedb-parity-roadmap.md` (this file) with an explicit per-gap ledger or appendix
- add companion ledger: `docs/parity-phase0-ledger.md`
- expand existing protocol tests before adding brand-new ones wholesale
- likely add focused end-to-end parity tests in `protocol/`, `subscription/`, `executor/`, `commitlog/`, and `store/` only where the current test suite does not already cover the scenario

### Acceptance gate
You can point to named parity tests for every later phase rather than only unit tests for internal helpers.

## Phase 1 — Wire-level protocol parity

### Goal
Make the handshake, wire framing, message-family boundaries, and close behavior much closer to SpacetimeDB before redesigning deeper query/runtime semantics.

### Why this phase is first after harness work
These are the most obvious client-visible mismatches and the most isolated place to start.

### Target outcomes
- protocol negotiation shape is compatible with the intended client story
- compression and framing behavior are intentional and parity-tested
- close / policy / protocol-error behavior is intentional and parity-tested
- message families stop being obviously Shunter-specific at the handshake/frame layer

### Required slices

#### 1. Subprotocol and message-family decision
Decide whether parity means:
- accept SpacetimeDB protocol identifiers directly, or
- maintain Shunter-specific identifiers but make message semantics otherwise equivalent

If the goal is your own SpacetimeDB implementation, the default recommendation is: move toward accepting SpacetimeDB identifiers.

Primary files:
- `protocol/options.go`
- `protocol/upgrade.go`
- `protocol/tags.go`
- `protocol/wire_types.go`

#### 2. Compression-envelope parity
Close the current divergence in compression tags and actual behavior.
Current audit evidence says the live path advertises a compression envelope but does not truly match the reference contract.

Primary files:
- `protocol/compression.go`
- `protocol/sender.go`
- `protocol/server_messages.go`
- `protocol/client_messages.go`
- `protocol/send_responses.go`
- `protocol/async_responses.go`

#### 3. Close-code and lifecycle parity
Align shutdown / policy / protocol-error close behavior.

Primary files:
- `protocol/dispatch.go`
- `protocol/close.go`
- `protocol/disconnect.go`
- `protocol/keepalive.go`
- `protocol/lifecycle.go`
- `protocol/conn.go`

### Acceptance gate
A protocol-focused client should be able to negotiate, send, receive, and close without hitting obviously Shunter-specific handshake/frame/close mismatches.

## Phase 1.5 — First end-to-end delivery parity slice

### Goal
Close the first cross-seam parity path: reducer call → caller result → non-caller update → confirmed-read behavior.

### Why this is its own phase
The live code already routes this behavior through executor, subscription, and protocol together. Treating it as “protocol only” hides the real seam.

### Target outcomes
- reducer-call result and transaction-update behavior are tested as one scenario
- caller/non-caller delivery ordering is intentional
- confirmed-read behavior is explicit and tested
- no active-subscription edge case silently drops caller-visible result paths

### Required slices
- `TransactionUpdate` / `ReducerCallResult` outcome model decision
- caller/non-caller routing and suppression rules
- confirmed-read / durability visibility in the ordinary public flow
- no-subscription / empty-changeset edge-case behavior

Primary files:
- `executor/executor.go`
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`
- `protocol/send_txupdate.go`
- `protocol/send_reducer_result.go`
- `protocol/server_messages.go`
- `protocol/handle_callreducer.go`

### Acceptance gate
One canonical end-to-end reducer flow should prove the intended caller/non-caller result and durability behavior without relying on helper-level interpretation.

## Phase 2 — Query and subscription-surface parity

### Goal
Make Shunter’s query/subscription behavior converge on SpacetimeDB’s operational outcomes even if the evaluator remains internally different.

### Target outcomes
- subscription registration model supports equivalent real workloads
- query grouping and identity model behave like the reference
- delta results are equivalent in content and delivery semantics
- lag handling and confirmed-read delivery are consciously matched or intentionally deferred

### Required slices

#### 1. Introduce a SQL-compatible or SQL-accepting front door
The evaluator can keep its predicate IR, but parity requires a closer query surface.
Recommended path:
- add a SQL-ish parser/translator that compiles into the existing predicate structures where possible
- keep the current predicate builder as an internal or advanced surface, not the parity surface

Primary files:
- `protocol/wire_types.go`
- `protocol/client_messages.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_oneoff.go`
- `subscription/predicate.go`
- `subscription/validate.go`
- `subscription/register.go`

#### 2. Add query-set grouping semantics — closed Phase 2 Slice 2
`SubscribeMulti` / `SubscribeSingle` variant split landed; one-QueryID-per-query-set grouping semantics now match the reference. Positive pins: `TestPhase2Subscribe{Single,Multi}Shape`, `TestPhase2Unsubscribe{Single,Multi}Shape`, `TestPhase2Subscribe{Single,Multi}AppliedShape`, `TestPhase2Unsubscribe{Single,Multi}AppliedShape`, `TestPhase2TagByteStability`. Set-based manager API (`RegisterSet` / `UnregisterSet`) + set-based executor commands (`RegisterSubscriptionSetCmd` / `UnregisterSubscriptionSetCmd`). Remaining Phase 2 Slice 2 deferrals: `TotalHostExecutionDurationMicros` on applied envelopes, `SubscriptionError.TableID` / optional-field shape, SQL-string form for `SubscribeMulti.Queries` (paired with Phase 2 Slice 1).

Primary files:
- `protocol/wire_types.go`
- `protocol/client_messages.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_unsubscribe.go`
- `protocol/conn.go`
- `subscription/query_state.go`
- `subscription/register.go`
- `subscription/unregister.go`
- `subscription/manager.go`

#### 3. Revisit lag / backpressure semantics
The current bounded fail-fast behavior may be acceptable for your own use, but it is not parity.
Decide whether to:
- emulate SpacetimeDB’s deeper queue/lazy slow-client semantics, or
- keep the current policy and explicitly accept this as one of the few permanent divergences

Primary files:
- `subscription/fanout_worker.go`
- `subscription/fanout.go`
- `protocol/outbound.go`
- `protocol/sender.go`
- `protocol/fanout_adapter.go`

#### 4. Confirmed-read and delivery ordering parity
Ensure the system matches the expected visibility rules for durable vs non-durable transaction updates and caller/non-caller delivery ordering.

Primary files:
- `subscription/eval.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`
- `executor/executor.go`
- `commitlog/durability.go`

#### 5. RLS / per-caller filtering decision
This is a strategic branch point.
If you want operational equivalence for serious workloads, eventually you need a caller-aware filtering story.
If you do not need it for your use case, mark it explicitly deferred rather than leaving the absence implicit.

Primary files:
- `subscription/hash.go`
- `subscription/register.go`
- `protocol/handle_subscribe.go`
- likely future auth-aware query compilation surfaces

### Acceptance gate
Equivalent subscription scenarios should produce equivalent registration behavior, delta content, and user-visible delivery order for the workloads you care about.

## Phase 3 — Runtime and lifecycle parity

### Goal
Close gaps in reducer execution, lifecycle behavior, scheduling, and failure semantics.

### Target outcomes
- reducer call success/failure surfaces feel like SpacetimeDB
- scheduling behavior produces the same application-visible result
- lifecycle reducers and client lifecycle handling align closely enough to substitute operationally

### Important sequencing note
If the target workload depends on scheduled reducers, scheduling timing/startup-order work should not wait until the end of runtime cleanup. It is a parity surface, not just an internal detail.

### Required slices

#### 1. Scheduling semantics and startup ordering
Close the audit-documented scheduling differences first when scheduled reducers matter.
The goal is not scheduler code similarity; it is the same observable firing/update behavior.

Primary files:
- `executor/scheduler.go`
- `executor/scheduler_worker.go`
- `executor/lifecycle.go`
- `executor/executor.go`
- relevant scheduler tests

#### 2. Reducer outcome model
Map Shunter’s reducer status model to the operational outcomes you actually need:
- committed
- user failure
- internal failure
- out-of-energy equivalent or consciously deferred
- not-found handling in the correct layer

Primary files:
- `executor/executor.go`
- `executor/reducer.go`
- `protocol/send_reducer_result.go`
- `protocol/server_messages.go`

#### 3. Post-commit failure and fatality semantics
Shunter currently differs in how post-commit failures are treated.
Align the system with the operational guarantees you want to inherit from SpacetimeDB.

Primary files:
- `executor/executor.go`
- `commitlog/durability.go`
- `subscription/eval.go`
- `subscription/fanout_worker.go`

### Acceptance gate
Reducer-facing application code should observe the same categories of outcomes, and scheduled/lifecycle behavior should no longer feel distinctly “Shunter-ish.”

## Phase 4 — Durability, recovery, and store parity

### Goal
Close the biggest durability/recovery/store gaps that affect operational equivalence.

### Target outcomes
- transaction numbering and replay semantics are intentional and stable
- recovery behavior matches the reference outcome expectations for the supported cases
- snapshot and sequence behavior do not diverge in ways that break application assumptions

### Required slices

#### 1. TxID / nextID / sequence ownership first
Before chasing deeper format parity, lock the ownership and replay invariants that already span executor, store, and commitlog.
This includes recovered TxID behavior, replay/nextID/sequence behavior, and the fact that live sequence repair currently happens in commitlog recovery rather than store alone.

Primary files:
- `executor/executor.go`
- `executor/lifecycle.go`
- `store/commit.go`
- `store/recovery.go`
- `store/snapshot.go`
- `store/transaction.go`
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/snapshot_select.go`
- `commitlog/snapshot_io.go`
- `commitlog/changeset_codec.go`

#### 2. Commitlog behavior parity
Decide how close you need to get on:
- replay tolerance vs fail-fast behavior
- offset indexing
- snapshot / compaction visibility
- record/log shape compatibility where it affects your required workloads

Primary files:
- `commitlog/segment.go`
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/segment_scan.go`
- `commitlog/snapshot_select.go`
- `commitlog/snapshot_io.go`
- `commitlog/changeset_codec.go`
- `commitlog/compaction.go`

#### 3. Value/encoding capability parity
The current value model is still materially smaller than SpacetimeDB’s.
If your target workloads need those richer capabilities, this is mandatory.

Primary files:
- `types/value.go`
- `bsatn/encode.go`
- `bsatn/decode.go`
- `schema/typemap.go`
- `schema/valuekind_export.go`

### Acceptance gate
Crash/restart/recovery tests and replay tests should confirm stable application-visible behavior that matches the intended SpacetimeDB outcome model for supported features.

## Phase 5 — Schema and capability parity

### Goal
Close the remaining capability and developer-surface gaps that prevent Shunter from functioning as “your SpacetimeDB.”

### Target outcomes
- schema registration supports the data model and system behaviors your target workloads need
- auto-increment / primary-key / system-table behavior is compatible enough for equivalent apps
- the public API does not push callers into obviously non-SpacetimeDB-like usage

### Required slices
- richer type/model support if needed by target workloads
- auto-increment and sequence semantics review
- system-table and lifecycle conventions review
- exported schema/introspection behavior where operationally relevant

Primary files:
- `schema/builder.go`
- `schema/build.go`
- `schema/export.go`
- `schema/reflect_build.go`
- `schema/validate_structure.go`
- `schema/errors.go`
- `schema/typemap.go`
- `schema/valuekind_export.go`
- `store/`
- `types/`

### Acceptance gate
Target applications can define schema and system behaviors without hitting obvious capability cliffs relative to the reference model.

## Phase 6 — Hardening and cleanup after parity direction is locked

### Goal
Burn down the remaining `TECH-DEBT.md` backlog without re-litigating parity goals.

### Scope
- protocol lifecycle races
- snapshot/read-view hazards
- fanout aliasing bugs
- cleanup of internal duplication once the target semantics are stable

### Important rule
Do not make this Phase 1 by accident.
Parity direction first. Cleanup second.

---

# 4. Immediate next slices

If work starts now, the highest-leverage near-term sequence is:

## Slice 1 — Build the parity harness
- add parity-focused protocol and runtime scenario tests
- define the reference-outcome statement for each open Tier A gap
- ensure the next changes are judged against externally visible behavior, not just helper-level tests

## Slice 2 — Wire-level protocol envelope parity
- subprotocol decision
- compression-envelope/tag parity
- handshake / close-code alignment
- message-family cleanup at the frame boundary

## Slice 3 — First end-to-end delivery parity slice
- one canonical subscribe → reducer → caller/non-caller delivery scenario
- `TransactionUpdate` / `ReducerCallResult` model decision
- confirmed-read semantics in the ordinary public flow
- no-subscription / empty-changeset edge cases

## Slice 4 — Query/subscription surface parity
- SQL/string front door or equivalent compatibility layer
- subscription-set grouping model
- one-off-query alignment

## Slice 5 — Runtime and recovery invariants
- scheduled-reducer timing / startup-order parity when relevant
- replay / nextID / sequence / TxID correctness
- lag/slow-client policy and remaining recovery/store behavior decisions

---

# 5. Rules for implementation work

## Rule 1: every parity slice must name the external behavior being matched
Bad: “refactor protocol sender”
Good: “make reducer-call success/failure/update delivery match the intended SpacetimeDB client-visible outcome”

## Rule 2: every closed slice must add tests that prove the behavior
Prefer:
- end-to-end tests
- API boundary tests
- frame/message tests
- replay/recovery scenario tests

## Rule 3: keep docs honest as work lands
Whenever a divergence is intentionally closed or intentionally retained:
- update this roadmap
- update `docs/current-status.md` if the overall status changes
- update `docs/parity-phase0-ledger.md`, `docs/current-status.md`, and `TECH-DEBT.md` if the live truth changes

## Rule 4: do not silently widen scope
Non-goals unless explicitly promoted:
- distributed clustering
- hosted/cloud features
- multi-language/WASM runtime
- product-market ergonomics for general users

---

# 6. Definition of done

Shunter is close enough to claim serious operational parity with SpacetimeDB when all of the following are true:

1. Protocol
- the intended client workflows no longer hit obviously Shunter-specific protocol differences
- subscribe, reducer-call, one-off query, update delivery, and close behavior are parity-close

2. Runtime behavior
- reducer outcomes, scheduling, lifecycle behavior, and caller/non-caller update flows are parity-close for target workloads

3. Subscription behavior
- the query/subscription surface and delta fanout outcomes are parity-close for target workloads

4. Durability / recovery
- crash/restart/replay behavior is predictably equivalent for supported features

5. Remaining debt
- no unresolved high-severity correctness issues remain in protocol, subscription, executor, store, or commitlog that undermine the parity claim

6. Documentation
- the remaining divergences are few, explicit, and consciously accepted

If those conditions are not met, Shunter is still a substantial prototype — not yet your own operational SpacetimeDB.

---

# 7. Operator summary

If you only remember one thing, remember this:

Do not spend the next stretch making Shunter “cleaner.”
Spend it making Shunter less observably different.

That means:
- wire-level protocol parity first
- one end-to-end delivery parity slice second
- query/subscription-surface parity third
- runtime/recovery semantics immediately after that, with scheduling pulled forward when the workload depends on it
- cleanup after the parity target is locked
