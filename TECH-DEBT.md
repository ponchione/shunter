# TECH-DEBT

This file tracks open issues only.
Resolved audit history belongs in git history, not here.

Status conventions:
- open: confirmed issue or parity gap still requiring work
- deferred: intentionally not being closed now

Priority order:
1. externally visible parity gaps
2. correctness / concurrency bugs that undermine parity claims
3. capability gaps that block realistic usage
4. cleanup after parity direction is locked

Parity principles:
- parity is judged by named client-visible scenarios, not helper-level resemblance
- same observable outcome beats same internal mechanism
- every parity change needs an observable test
- divergences must stay explicit
- closed slice history belongs in tests and git history, not in startup docs

Closed parity baselines are not startup context and should not be reopened
without fresh failing regression evidence:
- protocol subprotocol/compression/lifecycle/message-family baselines
- canonical reducer delivery and empty-fanout caller outcomes
- subscription rows through `P0-SUBSCRIPTION-033`
- same-connection reused subscription-hash initial-snapshot elision
- scheduler startup/firing narrow slice
- recovery replay horizon and snapshot/replay invariant slices
- Phase 4 Slice 2 offset-index, typed error category, and record/log documented-divergence slices

Active audit note (2026-04-24):
- hosted-runtime V1 is landed and verified; `docs/hosted-runtime-planning/V1/` is no longer the active implementation campaign
- OI-004 and OI-006 were removed after the post-V1 audit found no concrete remaining open lifecycle or fanout-aliasing defect on the hosted-runtime path
- OI-005 remains open but narrowed to lower-level raw read-view/snapshot lifetime discipline as an accepted expert-API risk
- OI-002 is the expected next parity/runtime-model campaign unless a fresh post-V1 scout changes priority
- do not close parity items solely because they are reachable through the hosted-runtime API; close or narrow them only when the underlying parity/correctness gap is pinned by live tests

## Open issues

### OI-001: Protocol surface is still not wire-close enough to SpacetimeDB

Status: open
Severity: high

Summary:
- all OI-001 A1 wire-shape and measured-duration parity slices identified to date are closed and pinned
- legacy `v1.bsatn.shunter` admission is still accepted as a compatibility deferral
- brotli remains recognized-but-unsupported
- several message-family and envelope details remain intentionally divergent
- client-message decoders still need a body-consumption audit: some decoder paths can accept a valid prefix while ignoring trailing bytes, even though tests/comments around legacy payload rejection imply stricter behavior
- subscribe/unsubscribe handler logic still has avoidable duplication around parsing, lifecycle checks, and response shaping; clean it after the protocol behavior target is pinned
- reducer failure-arm collapse remains an explicit outcome-model follow-up; see `docs/parity-decisions.md#outcome-model`
- rows-shape wrapper-chain parity (`SubscribeRows` / `DatabaseUpdate` / `TableUpdate` / `CompressableQueryUpdate` / `BsatnRowList`) is closed as a documented divergence — see `docs/parity-decisions.md#protocol-rows-shape`. Carried-forward deferral: a coordinated close of the wrapper chain together with the SPEC-005 §3.4 row-list format is a separate multi-slice phase, not an OI-001 A1 wire-close slice.

Why this matters:
- protocol behavior is still one of the biggest blockers to serious parity claims
- even where semantics are close, the wire contract is still visibly Shunter-specific

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
- With hosted-runtime V1 landed, the next parity execution target is expected to be OI-002 subscription-runtime parity unless a fresh post-V1 audit changes priority. The remaining OI-001 items are narrower compatibility/divergence follow-ons unless a user explicitly asks to reopen protocol wire-close work.

### OI-002: Query and subscription behavior still diverges from the target runtime model

Status: open
Severity: high

Summary:
- A2 is still open, but the closed SQL/query slice history is intentionally not repeated here.
- No queued active child issue; same-connection reused subscription-hash initial-snapshot elision is closed and pinned by `subscription/register_set_test.go::TestRegisterSetSameConnectionReusedHashEmitsEmptyUpdate` and `TestRegisterSetCrossConnectionReusedHashStillEmitsInitialSnapshot`. `SubscriptionError.table_id` on request-origin error paths now always emits `None` (reference v1 parity); pinned by `executor/protocol_inbox_adapter_test.go::TestProtocolInboxAdapter_RegisterSubscriptionSet_SingleTableErrorEmitsNilTableID` alongside the pre-existing multi-table nil pin.
- Remaining broad risks: the supported SQL surface is still narrower than the reference path, row-level security / per-client filtering is absent, and subscription behavior still spans several seams rather than one fully parity-locked contract.
- Legacy structured-query remnants remain alongside the SQL path: `Query` / `Predicate` wire types, `compileQuery`, `parseQueryString`, and one-off column match helpers make the live query model harder to reason about.
- One-off and subscription tests duplicate large scenario blocks; this makes future query behavior changes more expensive and increases the chance that one path drifts from the other.
- `subscription/eval.go` contains a dead per-evaluation memoization map: it stores query hash results but never reads them. The actual useful duplicate suppression appears to live in fanout batching, so this should be removed or reconnected deliberately.

Execution note:
- `NEXT_SESSION_HANDOFF.md` owns the immediate OI-002 startup path.
- Do not read or reproduce the closed `P0-SUBSCRIPTION-*` sequence for new work; tests and git history are the archive.
- Choose the next OI-002 batch by a fresh scout; do not carry forward historical handoff targets.

Why this matters:
- the system can look architecturally right while still behaving differently under realistic subscription workloads
- query-surface limitations still cap how close clients can get to reference behavior

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

### OI-003: Recovery and store semantics still differ in user-visible ways

Status: open
Severity: high

Summary:
- value-model and changeset semantics remain simpler than the reference
- commitlog/recovery behavior is intentionally rewritten rather than format-compatible
- replay tolerance, sequencing, and snapshot/recovery behavior still need follow-through

Why this matters:
- storage and recovery semantics are central to the operational-replacement claim
- sequencing and replay mismatches are the kind of differences users feel after crash/restart

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

Source docs:
- `docs/parity-decisions.md#commitlog-record-shape`

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

### OI-007: Recovery sequencing and replay-edge behavior is narrowed to remaining format/scheduler deferrals

Status: open — narrowed after reader-side zero-header EOS closure
Severity: medium

Summary:
- reader-side zero-header EOS / preallocated-zero-tail tolerance is now closed and pinned: `DecodeRecord` and recovery scanning treat an all-zero Shunter record header as end-of-stream, so `ScanSegments` / `ReplayLog` stop at the last real tx instead of classifying preallocated zero tails as damaged user data
- authoritative pins: `commitlog/replay_test.go::TestReplayLogPreallocatedZeroTailStopsAtLastRecord` and `commitlog/wire_shape_test.go::TestWireShapeShunterZeroRecordHeaderActsAsEOS`
- remaining live carried-forward deferrals from Phase 4 Slice 2γ (no broader wire-format rewrite landed; 2γ remains a documented-divergence slice):
  - reference byte-compatible magic (`(ds)^2` vs `SHNT`)
  - commit grouping (N-tx framing unit)
  - `epoch` field + `set_epoch` API
  - V0/V1 version split
  - writer-side preallocation/fallocate support (reader tolerance is in place, but Shunter still does not emit preallocated segment files)
  - checksum-algorithm negotiation rename
  - forked-offset detection (`Traversal::Forked`)
  - full records-buffer format parity (couples to BSATN / types / schema / subscription / executor)
  - `Append<T>` payload-return API
- remaining scheduler deferrals stay open (see `docs/parity-decisions.md#scheduler-startup-and-firing`)

Why this matters:
- these gaps mainly show up under restart, crash, and replay conditions
- they materially affect the operational-replacement claim

Primary code surfaces:
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`

Source docs:
- `docs/parity-decisions.md#scheduler-startup-and-firing`
- `docs/parity-decisions.md#commitlog-record-shape`

### OI-008: Cleanup-only test and label debt obscures the live behavior

Status: open
Severity: medium

Summary:
- stale test names and labels still point at retired docs or closed audit slices, including `OI-004`, `OI-006`, `TD-057`, `P0-DELIVERY-*`, and phase-style acceptance labels
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
- `protocol/*_test.go`
- `subscription/fanout_worker_test.go`
- `subscription/eval.go`
- `docs/hosted-runtime-planning/`

## Deferred issues

### DI-001: Energy accounting remains a permanent parity deferral

Status: deferred
Severity: low

Summary:
- `EnergyQuantaUsed` remains pinned at zero because Shunter does not implement an energy/quota subsystem

Why this matters:
- this is an intentional parity gap and should stay explicit

Source docs:
- `docs/parity-decisions.md#outcome-model`
