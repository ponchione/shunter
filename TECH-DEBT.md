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

Active audit note (2026-04-24):
- hosted-runtime V1 is landed and verified; `docs/hosted-runtime-planning/V1-*` is no longer the active implementation campaign
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
- rows-shape wrapper-chain parity (`SubscribeRows` / `DatabaseUpdate` / `TableUpdate` / `CompressableQueryUpdate` / `BsatnRowList`) is closed as a documented divergence — see `docs/parity-phase2-slice4-rows-shape.md`. Carried-forward deferral: a coordinated close of the wrapper chain together with the SPEC-005 §3.4 row-list format is a separate multi-slice phase, not an OI-001 A1 wire-close slice.

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
- `docs/spacetimedb-parity-roadmap.md` Tier A1
- `docs/parity-phase0-ledger.md`
- `docs/parity-phase2-slice4-rows-shape.md`

Execution note:
- With hosted-runtime V1 landed, the next parity execution target is expected to be OI-002 / Tier A2 subscription-runtime parity unless a fresh post-V1 audit changes priority. The remaining OI-001 items are narrower compatibility/divergence follow-ons unless a user explicitly asks to reopen protocol wire-close work.

### OI-002: Query and subscription behavior still diverges from the target runtime model

Status: open
Severity: high

Summary:
- many narrow A2 parity slices are already closed and pinned: fan-out durability gating, join/cross-join multiplicity, one-off/ad hoc unindexed two-table join admission while subscribe still rejects unindexed joins, one-off cross-join `WHERE` column-equality admission while subscribe still rejects cross-join `WHERE`, projected-side bootstrap/final-delta ordering, projected-join delta ordering, `:sender` hash identity, neutral-`TRUE` normalization, same-table canonicalization (child order / grouping / duplicate leaves / absorption), overlength SQL admission, bare/grouped `FALSE`, distinct-table join-filter child-order canonicalization, self-join alias-sensitive join-filter canonicalization (child order + associative grouping + duplicate leaves + absorption), one-off `LIMIT` support on already-supported row projections while subscribe still rejects `LIMIT`, one-off-only single-table column-list projections while subscribe still rejects non-`*` projections, one-off-only `COUNT(*) [AS] alias` while subscribe still rejects aggregate projections, one-off-only explicit projection-column aliases while subscribe still rejects non-`*` projections, one-off-only join-backed explicit column-list projections on the existing two-table join surface while subscribe still rejects non-`*` projections, and one-off-only join-backed `COUNT(*) [AS] alias` aggregate projections while subscribe still rejects aggregate projections, and one-off/subscribe JOIN ON equality-plus-single-relation-filter widening as a transparent parser admission
- one-off SQL now also accepts the bounded query-only aggregate shapes `SELECT COUNT(*) AS n FROM t` and reference-style `SELECT COUNT(*) n FROM t` (with existing single-table predicates); the parser carries aggregate metadata while still rejecting missing aliases (`SELECT COUNT(*) FROM t`), the shared compile seam builds a one-column uint64 aggregate result shape for one-off callers, subscribe rejects parsed aggregates deliberately before executor registration, and one-off returns exactly one count row even when the matched input is empty
- one-off SQL now also accepts bounded explicit projection-column aliases on the existing single-table column-list projection surface: `query/sql/parser.go` now preserves source qualifier routing separately from output alias metadata, `protocol/handle_subscribe.go` renames one-off output-column schemas while preserving base-column indexes, and subscribe still rejects parsed non-`*` projections deliberately before executor registration
- one-off SQL now also accepts unindexed two-table joins on the ad hoc/query path while subscribe registration still rejects unindexed joins: `protocol/handle_oneoff.go` now uses `subscription.ValidateQueryPredicate(...)`, which preserves structural/schema validation but skips the subscription-only join-index admission check, and `subscription.ValidatePredicate(...)` / register-set validation still require a join index for subscriptions
- one-off SQL now also accepts the bounded query-only cross-join `WHERE` column-equality shape `SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32`: `query/sql/parser.go` carries a qualified column-vs-column predicate node, `protocol/handle_subscribe.go` compiles the exact one-off shape into the existing `subscription.Join` evaluator, and subscribe still rejects cross-join `WHERE` before executor registration
- one-off SQL now also accepts bounded join-backed query-only `COUNT(*) [AS] alias` on the existing two-table join surface: the parser carries aggregate metadata on joins, the shared compile seam builds a one-column uint64 aggregate result shape for one-off callers from join output, matched join-row multiplicity is counted, and subscribe still rejects parsed aggregates deliberately before executor registration
- one-off SQL now also accepts bounded mixed-relation explicit column projections on the existing two-table join surface, for example `SELECT o.id, product.quantity FROM Orders o JOIN Inventory product ON o.product_id = product.id`; the parser preserves per-column source qualifiers, the shared compile seam validates/provides per-column table identity, one-off projects from the joined left/right row pair, and subscribe still rejects column-list projections deliberately before executor registration
- one-off and subscribe SQL now also accept bounded `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface (e.g., `SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10`); the parser transparently folds the ON-extracted filter into `Statement.Predicate`, producing identical output to the semantically equivalent WHERE-form, so subscribe accepts as a direct consequence of admitting the parser shape, while multi-conjunct / OR / column-vs-column / unqualified / third-relation / three-way-join rejections remain in place
- post-commit `SubscriptionError` fan-out now honors the same confirmed-read durability gate as normal transaction updates: public/default confirmed-read recipients do not observe evaluation-origin subscription errors until `TxDurable` is ready, while error-before-update ordering is preserved after the gate opens
- post-commit fanout now emits a deterministic per-connection update slice ordered by internal subscription-registration/SubscriptionID order, so a connection with multiple affected subscriptions no longer observes Go map-iteration drift inside one `TransactionUpdate` / caller embedded update
- post-commit fanout, initial subscribe snapshots, final unsubscribe deltas, and protocol wire encoding now use the client-chosen `QueryID` as the visible subscription correlator; manager-internal `SubscriptionID` remains internal for registration/order bookkeeping and is no longer exposed by `protocol.SubscriptionUpdate`
- authoritative pins for the latest A2 query-only closures now include `query/sql/parser_test.go` (`TestParseSingleTableColumnProjection`, `TestParseMultiColumnProjectionWithWhere`, `TestParseSingleTableColumnProjectionWithAlias`, `TestParseSingleTableColumnProjectionWithBareAliasAndWhere`, `TestParseMultiColumnProjectionWithAliasesAndWhere`, `TestParseQualifiedSingleTableColumnProjectionWithAlias`, `TestParseJoinColumnProjection`, `TestParseJoinColumnProjectionProjectsRight`, `TestParseJoinColumnProjectionAllowsMixedRelations`, `TestParseCountStarAliasProjection`, `TestParseCountStarBareAliasProjection`, `TestParseCountStarAliasProjectionWithWhere`, `TestParseCountStarBareAliasProjectionWithWhere`, `TestParseJoinWhereColumnEquality`, `TestParseRejectsJoinWhereColumnEqualityRequiresQualifiedColumns`, `TestParseJoinCountStarAliasProjection`, `TestParseJoinCountStarBareAliasProjectionWithWhere`, `TestParseRejectsUnsupported`), `protocol/handle_oneoff_test.go` (`TestHandleOneOffQuery_UnindexedJoinReturnsRows`, `TestHandleOneOffQuery_CrossJoinWhereColumnEqualityReturnsProjectedRows`, `TestHandleOneOffQuery_ParityBareColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityMultiColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityAliasedBareColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityAliasedBareColumnProjectionWithWhereReturnsProjectedRows`, `TestHandleOneOffQuery_ParityAliasedMultiColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityJoinColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityJoinColumnProjectionProjectsRight`, `TestHandleOneOffQuery_ParityJoinColumnProjectionAllowsMixedRelations`, `TestHandleOneOffQuery_ParityCountAliasReturnsSingleAggregateRow`, `TestHandleOneOffQuery_ParityCountBareAliasReturnsSingleAggregateRow`, `TestHandleOneOffQuery_ParityCountAliasWithWhereReturnsSingleAggregateRow`, `TestHandleOneOffQuery_ParityCountAliasZeroRowsReturnsSingleZeroRow`, `TestHandleOneOffQuery_ParityJoinCountAliasReturnsSingleAggregateRow`, `TestHandleOneOffQuery_ParityJoinCountBareAliasWithWhereReturnsSingleAggregateRow`), `subscription/validate_test.go` (`TestValidateQueryPredicate_UnindexedJoinAllowed`, `TestValidateQueryPredicate_UnindexedJoinStillValidatesStructure`, `TestValidateJoinUnindexed`), and `protocol/handle_subscribe_test.go` (`TestHandleSubscribeSingle_UnindexedJoinStillRejected`, `TestHandleSubscribeSingle_CrossJoinWhereColumnEqualityStillRejected`, `TestHandleSubscribeSingle_ParityBareColumnProjectionRejected`, `TestHandleSubscribeSingle_ParityAliasedBareColumnProjectionRejected`, `TestHandleSubscribeSingle_ParityJoinColumnProjectionRejected`, `TestHandleSubscribeSingle_ParityCountAliasRejected`, `TestHandleSubscribeSingle_ParityCountBareAliasRejected`, `TestHandleSubscribeSingle_JoinCountAggregateStillRejected`), `TestParseJoinOnEqualityWithFilter`, `TestParseJoinOnEqualityWithFilterOnLeftSide`, `TestParseJoinOnEqualityWithFilterAndWhere`, `TestParseJoinOnEqualityParityWithWhereForm`, `TestParseRejectsJoinOnFilterMultipleConjuncts`, `TestParseRejectsJoinOnFilterOr`, `TestParseRejectsJoinOnFilterColumnVsColumn`, `TestParseRejectsJoinOnFilterUnqualifiedColumn`, `TestParseRejectsJoinOnFilterThirdRelation`, `TestHandleOneOffQuery_JoinOnEqualityWithFilterReturnsFilteredRows`, `TestHandleOneOffQuery_JoinOnEqualityWithFilterMatchesWhereForm`, `TestHandleSubscribeSingle_JoinOnEqualityWithFilterAccepted`, `TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected`
- the supported SQL surface is still intentionally narrower than the reference SQL path
- row-level security / per-client filtering remains absent
- broader A2/runtime-model gaps remain; after the QueryID fanout/protocol correlation closure, the next bounded residual should be chosen from fresh live evidence rather than carried forward from stale pre-closure handoff notes

Execution note:
- OI-002 is now the expected next parity/runtime-model handoff issue after hosted-runtime V1 unless a fresh post-V1 scout changes priority
- the JOIN ON equality-plus-single-relation-filter widening slice is closed and pinned; subscribe acceptance is treated as a transparent parser admission rather than a one-off-only divergence because the ON-form and WHERE-form produce indistinguishable parser output
- the confirmed-read `SubscriptionError` fan-out slice is closed and pinned by `subscription/fanout_worker_test.go::TestFanOutWorker_SubscriptionError_PublicProtocolDefault_WaitsForDurability`; normal transaction updates and evaluation-origin subscription errors now share the same public/default durability gate
- the deterministic per-connection fanout-ordering slice is closed and pinned by `subscription/eval_test.go::TestEvalFanoutOrdersUpdatesByRegistrationWithinConnection`; evaluator-produced update slices are stabilized before fanout/caller capture
- the QueryID fanout/protocol correlation slice is closed and pinned by `subscription/eval_test.go::TestEvalFanoutCarriesClientQueryIDForEachSubscription` and `protocol/fanout_adapter_test.go::TestEncodeSubscriptionUpdate_CarriesClientQueryID`; protocol `SubscriptionUpdate` now exposes client `QueryID`, not manager-internal `SubscriptionID`
- choose the next OI-002 batch only after a fresh post-change scout; do not blindly carry forward old handoff targets

Why this matters:
- the system can look architecturally right while still behaving differently under realistic subscription workloads
- query-surface limitations still cap how close clients can get to reference behavior

Primary code surfaces:
- `query/sql/parser.go`
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

Source docs:
- `docs/spacetimedb-parity-roadmap.md` Tier A2
- `docs/parity-phase0-ledger.md`

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
- `docs/spacetimedb-parity-roadmap.md` Tier A3
- `docs/parity-phase0-ledger.md`
- `docs/parity-phase4-slice2-offset-index.md`
- `docs/parity-phase4-slice2-errors.md`
- `docs/parity-phase4-slice2-record-shape.md`

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
- `docs/hosted-runtime-planning/V1-F/`
- `docs/decomposition/hosted-runtime-v1-contract.md`
- `docs/hosted-runtime-implementation-roadmap.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B

Audit note:
- keep OI-005 as the accepted lower-level/expert API discipline marker; do not reopen it for the now-pinned executor post-commit panic-close gap unless a fresh concrete leak/reproducer appears

### OI-007: Recovery sequencing and replay-edge behavior still needs targeted parity closure

Status: open
Severity: medium

Summary:
- live carried-forward deferrals from Phase 4 Slice 2γ (no wire-format change landed; rejected as documented-divergence slice):
  - reference byte-compatible magic (`(ds)^2` vs `SHNT`)
  - commit grouping (N-tx framing unit)
  - `epoch` field + `set_epoch` API
  - V0/V1 version split
  - zero-header EOS sentinel + preallocation-friendly writes
  - checksum-algorithm negotiation rename
  - forked-offset detection (`Traversal::Forked`)
  - full records-buffer format parity (couples to BSATN / types / schema / subscription / executor)
  - `Append<T>` payload-return API
- remaining scheduler deferrals stay open (see `docs/parity-p0-sched-001-startup-firing.md`)

Why this matters:
- these gaps mainly show up under restart, crash, and replay conditions
- they materially affect the operational-replacement claim

Primary code surfaces:
- `commitlog/replay.go`
- `commitlog/recovery.go`
- `commitlog/replay_test.go`
- `commitlog/recovery_test.go`

Source docs:
- `docs/parity-p0-recovery-001-replay-horizon.md`
- `docs/parity-p0-sched-001-startup-firing.md`
- `docs/parity-phase0-ledger.md`
- `docs/parity-phase4-slice2-offset-index.md`
- `docs/parity-phase4-slice2-errors.md`
- `docs/parity-phase4-slice2-record-shape.md`

## Deferred issues

### DI-001: Energy accounting remains a permanent parity deferral

Status: deferred
Severity: low

Summary:
- `EnergyQuantaUsed` remains pinned at zero because Shunter does not implement an energy/quota subsystem

Why this matters:
- this is an intentional parity gap and should stay explicit

Source docs:
- `docs/parity-phase1.5-outcome-model.md`
- `docs/parity-phase0-ledger.md`
