# TECH-DEBT

This file tracks open issues only.
Resolved audit history belongs in git history, not here.

Status convention:
- every issue listed below is open until the concrete Shunter gap is closed and pinned by tests/docs

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
- SQL/query work is useful when it makes Shunter clients more correct, predictable, and expressive; it should not continue as open-ended reference-message or parser-error parity chasing

Closed reference-comparison baselines are not startup context and should not be reopened without fresh failing Shunter regression evidence:
- protocol subprotocol/compression/lifecycle/message-family baselines
- canonical reducer delivery and empty-fanout caller outcomes
- subscription rows through `P0-SUBSCRIPTION-033`
- same-connection reused subscription-hash initial-snapshot elision
- scheduler startup/firing narrow slice
- recovery replay horizon and snapshot/replay invariant slices
- offset-index, typed error category, and record/log documented-decision slices

Active audit note (2026-04-26):
- hosted-runtime V1 is landed and verified; `docs/hosted-runtime-planning/V1/` is no longer the active implementation campaign
- OI-004 and OI-006 were removed after the post-V1 audit found no concrete remaining open lifecycle or fanout-aliasing defect on the hosted-runtime path
- OI-005 remains open but narrowed to lower-level raw read-view/snapshot lifetime discipline as an accepted expert-API risk
- OI-002 remains the expected next runtime-model campaign unless a fresh post-V1 scout changes priority
- do not close behavior items solely because they are reachable through the hosted-runtime API; close or narrow them only when the underlying Shunter correctness gap is pinned by live tests

## Open issues

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
- reducer failure-arm collapse remains an explicit outcome-model follow-up only if Shunter clients need more machine-readable failure classes; see `docs/parity-decisions.md#outcome-model`
- Shunter's flat rows/update shape is the current native protocol contract — see `docs/parity-decisions.md#protocol-rows-shape`. Do not rewrite it solely to match SpacetimeDB's wrapper chain (`SubscribeRows` / `DatabaseUpdate` / `TableUpdate` / `CompressableQueryUpdate` / `BsatnRowList`).
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
- `docs/parity-decisions.md#outcome-model`
- `docs/parity-decisions.md#protocol-rows-shape`

Execution note:
- The Shunter-native subprotocol, energy-removal, decoder body-consumption, and subscribe/unsubscribe response-helper cleanup slices are closed and pinned. Remaining OI-001 work is conditional: implement brotli only if clients need it, and redesign failure arms only if clients need a more machine-readable outcome contract.

### OI-002: Query and subscription behavior needs a Shunter-owned correctness pass

Status: open
Severity: high

Summary:
Current contract:
- Shunter's v1 SQL surface is intentionally narrow: single-table equality/range predicates with `AND`, plus the subset of joins and one-off projections already documented in SPEC-005.
- One-off reads and subscriptions should agree anywhere they share syntax and type semantics.
- Observable behavior should be stable for Shunter clients: accepted queries should return correct rows, rejected queries should fail before registration/execution, and errors should be diagnosable.
- SpacetimeDB behavior may guide tricky ordering/type decisions, but byte-for-byte parser error parity is not a product goal.

Current open work:
- Identifier handling is still too case-insensitive. Table names, column names, and relation aliases currently resolve through case-folding in several paths. Decide the Shunter rule, document it, then make quoted and unquoted identifiers follow it consistently across OneOff, SubscribeSingle, and SubscribeMulti.
- Literal keyword handling is leaky. Unquoted `TRUE`, `FALSE`, and `NULL` can fall through as column names in some projection/filter shapes when real lowercase columns exist. They should remain literals or be rejected according to Shunter's SQL subset.
- Join WHERE support is inconsistent. Inner-join field-vs-field comparisons and boolean filters that span both relation sides are parsed in some cases but not evaluated correctly. Cross-join WHERE handling is even narrower and should either be fully admitted/evaluated for the documented subset or rejected before registration.
- Projection on joined rows is fragile. OneOff join projection can project from the wrong relation or project twice, producing malformed rows for realistic explicit projections.
- Validation ordering still matters where it changes user-visible outcomes. Keep ordering work only when it prevents wrong acceptance, wrong rows, misleading errors, or subscribe/one-off drift; do not chase message text for its own sake.
- Legacy structured-query remnants remain alongside the SQL path: `Query` / `Predicate` wire types, `compileQuery`, `parseQueryString`, and one-off column match helpers make the live query model harder to reason about.
- One-off and subscription tests duplicate large scenario blocks. Consolidate shared fixtures where the same syntax/typing contract is being tested.
- `subscription/eval.go` contains a dead per-evaluation memoization map: it stores query hash results but never reads them. Remove it or reconnect it deliberately.

Closed context to keep out of startup work:
- The literal source-text, typed error, LIMIT, JOIN ON ordering, boolean-constant masking, join-keyword, unindexed-subscription-join, and missing-field text slices closed on 2026-04-25 / 2026-04-26 and are pinned by tests. Do not repeat that history here or reopen it without a fresh Shunter-visible regression.
- Same-connection reused subscription-hash initial-snapshot elision is closed and pinned by `subscription/register_set_test.go::TestRegisterSetSameConnectionReusedHashEmitsEmptyUpdate` and `TestRegisterSetCrossConnectionReusedHashStillEmitsInitialSnapshot`.
- `SubscriptionError.table_id` on request-origin error paths now emits `None`; this is pinned by `executor/protocol_inbox_adapter_test.go::TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleTableErrorEmitsNilTableID`.

Execution note:
- `NEXT_SESSION_HANDOFF.md` owns the immediate OI-002 startup path.
- Do not read or reproduce the closed `P0-SUBSCRIPTION-*` sequence for new work; tests and git history are the archive.
- Choose the next OI-002 batch by a fresh scout organized around the current open-work bullets above.

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

### OI-003: Recovery and store semantics need Shunter operational hardening

Status: open
Severity: high

Summary:
- Shunter's value model, changeset format, commit log, and snapshot/recovery flow are intentionally Shunter-owned, not byte-format compatible with SpacetimeDB.
- remaining work should focus on crash/restart correctness, deterministic replay, snapshot compatibility, compaction safety, and clear operator failure modes.
- format differences are tech debt only when they produce a Shunter data-loss, recovery, observability, or operational limitation.

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
- byte-compatible record headers, epoch APIs, multi-transaction commit grouping, V0/V1 reference versioning, records-buffer format parity, and `Append<T>` payload-return APIs are not tracked debt unless a Shunter operational requirement appears

Why this matters:
- these gaps mainly show up under restart, crash, and replay conditions
- they materially affect whether Shunter is trustworthy for personally operated apps

Primary code surfaces:
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`

### OI-008: Cleanup-only test and label debt obscures the live behavior

Status: open
Severity: medium

Summary:
- stale test names and labels still point at retired docs or closed audit slices, including `OI-004`, `OI-006`, `TD-057`, `P0-DELIVERY-*`, `parity`, and phase-style acceptance labels
- `docs/parity-decisions.md` has a historical filename and several "accepted deferral" sections. Rename/reframe it as Shunter design decisions once code/tests no longer cite the old path, or keep a short redirect doc if a rename would create churn.
- `AGENTS.md`, `docs/README.md`, `HOSTED_RUNTIME_PLANNING_HANDOFF.md`, and `NEXT_SESSION_HANDOFF.md` still describe TECH-DEBT work as "parity" work in places. Update those labels to "correctness / TECH-DEBT" and keep SpacetimeDB reference language scoped to design evidence.
- protocol and subscription tests still use `parity_*` filenames and `Test*Parity*` names for many Shunter-owned contract pins. Rename opportunistically when touching those packages; do not do a repository-wide churn-only rename unless the tree is otherwise quiet.
- comments in `protocol/`, `subscription/`, `query/sql/`, `executor/`, and `commitlog/` still cite reference paths or parity history. Keep citations that explain why a Shunter decision was made, but rewrite comments that imply ongoing SpacetimeDB interop or byte-for-byte compatibility goals.
- `commitlog/phase4_acceptance_test.go::TestDurabilityWorkerBatchesAndFsyncs` has dead fsync-count instrumentation (`countingSegmentWriter`, `syncCount`, and `_ = counting`) that no longer validates the behavior its name implies
- several async tests rely on fixed `time.Sleep` windows, especially in fanout-worker coverage; these should move to condition/event based waits before the suite grows more parallel or slower
- duplicated protocol scenario tests should be collapsed where they are testing shared behavior rather than genuinely different one-off vs subscription contracts
- historical hosted-runtime planning files still contain superseded sequencing notes, such as older V1-G plans describing V1-H as the immediate next slice; prune or archive these when hosted-runtime planning resumes
- dead-code tooling is not part of the local validation path yet; `rtk staticcheck ./...` was unavailable during the sweep, and `go vet` does not catch several of these cleanup issues

Why this matters:
- stale labels make failure output point maintainers toward closed or nonexistent work
- duplicated tests and fixed sleeps slow down behavior changes while still missing some real regressions
- dead instrumentation gives a false sense that low-level durability behavior is being asserted

Primary code surfaces:
- `commitlog/phase4_acceptance_test.go`
- `docs/parity-decisions.md`
- `AGENTS.md`
- `docs/README.md`
- `NEXT_SESSION_HANDOFF.md`
- `HOSTED_RUNTIME_PLANNING_HANDOFF.md`
- `protocol/*_test.go`
- `subscription/fanout_worker_test.go`
- `subscription/eval.go`
- `docs/hosted-runtime-planning/`
