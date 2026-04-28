# TECH-DEBT

This file tracks open issues and short closure markers for recently completed
campaigns that startup docs still reference. Resolved audit history belongs in
git history, not here.

Status convention:
- open issues stay listed until the concrete Shunter gap is closed and pinned by tests/docs
- closed entries are lightweight guardrails against reopening completed campaigns without fresh Shunter-visible evidence

Priority order:
1. externally visible Shunter correctness and product-contract gaps
2. correctness / concurrency bugs that undermine runtime claims
3. capability gaps that block realistic usage
4. cleanup after the Shunter product contract is locked

Reference-use principles:
- SpacetimeDB is an architectural reference, not a wire/client/business compatibility target
- correctness is judged by named Shunter client-visible scenarios, not helper-level resemblance
- same observable Shunter outcome beats same internal mechanism
- every behavior change needs an observable test
- intentional differences from SpacetimeDB must stay explicit when they affect docs, tests, or client behavior
- closed slice history belongs in tests and git history, not in startup docs
- non-goals are not tech debt; do not track SpacetimeDB-only compatibility gaps unless a Shunter client, runtime, or operator scenario needs them

Project direction (2026-04-26):
- Shunter is for self-hosted / personally operated apps with Shunter-owned Go APIs and clients
- SpacetimeDB compatibility matters only when it helps choose a proven runtime model
- SpacetimeDB client wire compatibility is not a product goal
- SpacetimeDB-style energy accounting is not a product goal; Shunter has no billing/metering/quota economy, and energy-shaped protocol fields should be removed rather than kept as inert compatibility baggage
- use reference behavior as evidence for runtime semantics, but prefer a simpler Shunter contract when compatibility-only details add cost without value
- SQL/query work is useful when it makes Shunter clients more correct, predictable, and expressive; it should not continue as open-ended reference-message or parser-error compatibility chasing

Closed reference-comparison baselines are not startup context and should not be reopened without fresh failing Shunter regression evidence:
- protocol subprotocol/compression/lifecycle/message-family baselines
- canonical reducer delivery and empty-fanout caller outcomes
- subscription row-shape and delivery baselines
- same-connection reused subscription-hash initial-snapshot elision
- scheduler startup/firing narrow slice
- recovery replay horizon and snapshot/replay invariant slices
- offset-index, typed error category, and record/log documented-decision slices

Active audit note (2026-04-26):
- hosted-runtime V1 is landed and verified; `docs/hosted-runtime-planning/V1/` is no longer the active implementation campaign
- the former lifecycle/fanout-aliasing audit placeholders were removed after the post-V1 audit found no concrete remaining open defect on the hosted-runtime path
- OI-002 is closed for current evidence and should reopen only from a fresh Shunter-visible failing example
- OI-003 is complete; `OI-003.md` remains the audit/progress authority for the closed recovery/store semantics campaign
- OI-005 remains open but narrowed to lower-level raw read-view/snapshot lifetime discipline as an accepted expert-API risk
- the next broad confidence campaign is `docs/RUNTIME-HARDENING-GAUNTLET.md`, not another known-issue comparison pass
- do not close behavior items solely because they are reachable through the hosted-runtime API; close or narrow them only when the underlying Shunter correctness gap is pinned by live tests

## Issue index

### OI-001: Protocol surface still needs a Shunter-owned contract cleanup

Status: open — narrowed to conditional protocol follow-ups
Severity: medium

Summary:
- all OI-001 A1 wire-shape and measured-duration comparison slices identified to date are closed and pinned
- the product contract is Shunter-native; SpacetimeDB client/wire compatibility is no longer a correctness goal
- `v1.bsatn.shunter` is the only Shunter token. Shunter does not advertise or accept the SpacetimeDB-specific protocol token.
- brotli remains recognized-but-unsupported; implement it only if Shunter clients need it
- several message-family and envelope details are intentionally Shunter-specific
- client/server protocol message decoders reject trailing bytes after a valid body; this is pinned across current v1 message families
- subscribe/unsubscribe response and executor-unavailable error shaping are shared across Single/Multi paths; keep future lifecycle/parser cleanup behavior-preserving
- reducer failure-arm collapse remains an explicit outcome-model follow-up only if Shunter clients need more machine-readable failure classes; see `docs/shunter-design-decisions.md#outcome-model`
- Shunter's flat rows/update shape is the current native protocol contract — see `docs/shunter-design-decisions.md#protocol-rows-shape`. Do not rewrite it solely to match SpacetimeDB's wrapper chain (`SubscribeRows` / `DatabaseUpdate` / `TableUpdate` / `CompressableQueryUpdate` / `BsatnRowList`).
- energy accounting is explicitly out of scope for Shunter's product model. The energy-shaped protocol surface has been removed: no `TransactionUpdate.EnergyQuantaUsed`, no `StatusOutOfEnergy`, no `CallerOutcomeOutOfEnergy`, and no subscription/fanout energy fields. Do not replace this with a quota/metering abstraction unless a future Shunter-local product need appears.

Why this matters:
- protocol behavior is still one of the biggest blockers to serious Shunter client/runtime claims
- the wire contract needs to be owned, documented, and tested as Shunter's protocol instead of treated as a compatibility exercise

Primary code surfaces:
- `protocol/upgrade.go`
- `protocol/compression.go`
- `protocol/tags.go`
- `protocol/wire_types.go`
- `protocol/client_messages.go`
- `protocol/server_messages.go`
- `protocol/send_responses.go`
- `protocol/send_txupdate.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/shunter-design-decisions.md#outcome-model`
- `docs/shunter-design-decisions.md#protocol-rows-shape`

Execution note:
- The Shunter-native subprotocol, energy-removal, decoder body-consumption, and subscribe/unsubscribe response-helper cleanup slices are closed and pinned. Remaining OI-001 work is conditional: implement brotli only if clients need it, and redesign failure arms only if clients need a more machine-readable outcome contract.

### OI-002: Query and subscription behavior needs a Shunter-owned correctness pass

Status: closed for current evidence
Severity: high

Summary:
Current contract:
- Shunter's v1 SQL surface is intentionally narrow: single-table equality/range predicates with `AND` / `OR`, plus the subset of joins and one-off projections already documented in SPEC-005.
- One-off reads and subscriptions should agree anywhere they share syntax and type semantics.
- Observable behavior should be stable for Shunter clients: accepted queries should return correct rows, rejected queries should fail before registration/execution, and errors should be diagnosable.
- SpacetimeDB behavior may guide tricky ordering/type decisions, but byte-for-byte parser error matching is not a product goal.

Current open work:
- None in the runtime model as of the latest OI-002 scout. Projection, validation-ordering, identifier lookup, join-WHERE policy, structured-query protocol cleanup, and clear duplicated fixture blocks are pinned by focused tests.
- Future OI-002 work should be opened only from a fresh Shunter-visible failing example: wrong accepted/rejected query, wrong rows, misleading user-visible error, or one-off/subscription drift. Do not reopen parser-message matching or historical projection risk without new evidence.

Execution note:
- `NEXT_SESSION_HANDOFF.md` should not queue more OI-002 runtime-model work unless the next user supplies a fresh failing scenario.
- Completed OI-002 slices belong in tests and git history, not this open-issues file. Do not reopen them without a fresh Shunter-visible failing example.

Why this matters:
- the system can look architecturally right while still behaving differently under realistic subscription workloads
- query-surface limitations and subscribe/one-off drift directly cap what Shunter clients can build safely

Primary code surfaces:
- `query/sql/parser.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_oneoff.go`
- `subscription/predicate.go`
- `subscription/validate.go`
- `subscription/eval.go`
- `subscription/manager.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `executor/executor.go`
- `executor/scheduler.go`

### OI-003: Recovery and store semantics needed Shunter operational hardening

Status: closed for current evidence
Severity: high

Planning/progress authority: `OI-003.md`

This `TECH-DEBT.md` entry is now only a closure marker for OI-003. Use
`OI-003.md` for design decisions, workstream boundaries, progress tracking, and
audit history.

Summary:
- Shunter's value model, changeset format, commit log, and snapshot/recovery flow are intentionally Shunter-owned, not byte-format compatible with SpacetimeDB.
- the OI-003 campaign pinned crash/restart correctness, deterministic replay, snapshot compatibility, compaction safety, and clear operator failure modes for the current evidence set.
- format differences are tech debt only when they produce a Shunter data-loss, recovery, observability, or operational limitation.
- future recovery/store work should start from a fresh Shunter-visible failing example or from the runtime hardening gauntlet finding a concrete gap.

Why this matters:
- storage and recovery semantics are central to the "run my apps on this" claim
- restart behavior is where runtime correctness becomes operational trust

Primary code surfaces:
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
- `executor/executor.go`

### OI-005: Lower-level read-view/snapshot lifetime discipline remains an expert-API contract

Status: open — narrowed to accepted lower-level/expert API risk
Severity: low

Summary:
- hosted-runtime V1-F closes the normal root-runtime read-path concern: `Runtime.Read(ctx, fn)` exposes a callback-scoped `LocalReadView`, defers committed snapshot close before returning, and is pinned by tests for readiness/closed-state behavior, committed-row access, and post-read commit progress
- the previously identified snapshot/StateView aliasing and use-after-close sub-hazards are closed and pinned by store, subscription, and executor regression tests
- the concrete executor post-commit panic-close gap is now closed: `executor.postCommit` defers the acquired committed read-view close immediately after `snapshotFn()`, and `TestPostCommitPanicInEvalSetsFatal` asserts the view is closed even when `EvalAndBroadcast` panics
- remaining risk is intentionally lower-level and specific: raw `store.CommittedState.Snapshot()` / `store.CommittedReadView` still require caller-owned explicit close; `CommittedState.Table` and `StateView` still rely on documented envelope/single-executor discipline; subscription committed views remain borrowed and must not escape
- `Runtime.Read` callbacks remain snapshot-scoped and should not synchronously wait on reducer/write work while holding the snapshot; treat that as expert API discipline unless a concrete normal-runtime deadlock reproducer appears

Why this matters:
- leaked raw committed snapshots can stall commits until explicitly closed or until the best-effort finalizer runs
- the root runtime API and executor post-commit path no longer expose a known unclosed-snapshot path
- the remaining lower-level APIs preserve v1 simplicity but require callers to honor explicit read-view ownership rules

Primary code surfaces:
- `runtime_local.go`
- `store/snapshot.go`
- `store/committed_state.go`
- `store/state_view.go`
- `subscription/delta_view.go`
- `executor/executor.go`

Source docs:
- `docs/hosted-runtime-planning/V1/V1-F/`
- `docs/decomposition/hosted-runtime-v1-contract.md`
- `docs/hosted-runtime-implementation-roadmap.md`

Audit note:
- keep OI-005 as the accepted lower-level/expert API discipline marker; do not reopen it for the now-pinned executor post-commit panic-close gap unless a fresh concrete leak/reproducer appears

### OI-007: Replay-edge and scheduler restart behavior still need operational pins

Status: open — narrowed after reader-side zero-header EOS closure
Severity: medium

Summary:
- reader-side zero-header EOS / preallocated-zero-tail tolerance is now closed and pinned: `DecodeRecord` and recovery scanning treat an all-zero Shunter record header as end-of-stream, so `ScanSegments` / `ReplayLog` stop at the last real tx instead of classifying preallocated zero tails as damaged user data
- authoritative pins: `commitlog/replay_test.go::TestReplayLogPreallocatedZeroTailStopsAtLastRecord` and `commitlog/wire_shape_test.go::TestWireShapeShunterZeroRecordHeaderActsAsEOS`
- remaining work should be scoped to Shunter restart scenarios:
  - replay ordering around partial final records, damaged segment tails, and snapshot/log boundaries
  - clearer corruption/fork detection where it helps a single-node operator distinguish "recoverable tail" from "unsafe history"
  - writer-side preallocation/fallocate support only if it materially improves Shunter durability or startup behavior
  - scheduler replay/firing pins for restart, missed timers, and dangling-client lifecycle interactions
- byte-compatible record headers, epoch APIs, multi-transaction commit grouping, V0/V1 reference versioning, records-buffer format compatibility, and `Append<T>` payload-return APIs are not tracked debt unless a Shunter operational requirement appears

Why this matters:
- these gaps mainly show up under restart, crash, and replay conditions
- they materially affect whether Shunter is trustworthy for personally operated apps

Primary code surfaces:
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`

### OI-008: Cleanup-only test and label debt obscures the live behavior

Status: closed — cleanup completed and verified on 2026-04-28
Severity: medium

Summary:
- legacy compatibility, audit-slice, delivery-slice, and phase-slice names were removed from test filenames, test identifiers, comments, and active docs where they obscured Shunter-owned behavior
- `docs/shunter-design-decisions.md` is now the implementation-facing decision document; `docs/parity-decisions.md` remains only as a short redirect for old links
- protocol and subscription contract pins now use behavior-oriented filenames and `TestShunter*` / descriptive names
- comments in `protocol/`, `subscription/`, `query/sql/`, `executor/`, and `commitlog/` now frame SpacetimeDB references as design evidence, not ongoing interop or byte-for-byte goals
- dead durability-worker fsync-count instrumentation was removed and the test now asserts the durable transaction behavior it actually covers
- fixed sleep windows in async/fanout-worker and runtime polling coverage were replaced with condition/event waits or stability assertions
- duplicated protocol message-family field-order checks were collapsed where rows-shape coverage already owns the same contract
- stale hosted-runtime planning next-slice notes now point readers to `HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead of presenting completed work as live sequencing
- pinned Staticcheck is now green through `rtk go tool staticcheck ./...`

Why this matters:
- stale labels make failure output point maintainers toward closed or nonexistent work
- duplicated tests and fixed sleeps slow down behavior changes while still missing some real regressions
- dead instrumentation gives a false sense that low-level durability behavior is being asserted

Primary code surfaces:
- `commitlog/commitlog_contract_test.go`
- `docs/shunter-design-decisions.md`
- `docs/parity-decisions.md`
- `AGENTS.md`
- `CLAUDE.md`
- `docs/README.md`
- `NEXT_SESSION_HANDOFF.md`
- `HOSTED_RUNTIME_PLANNING_HANDOFF.md`
- `protocol/*_test.go`
- `subscription/fanout_worker_test.go`
- `docs/hosted-runtime-planning/`
