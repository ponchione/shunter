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
- hosted-runtime V1 implementation is the current active campaign, tracked under `docs/hosted-runtime-planning/V1-*`
- that campaign is the resolution path for OI-014 and overlaps materially with OI-004, OI-005, and OI-006
- OI-002 remains the next parity/runtime-model campaign after the hosted-runtime V1 pass unless a fresh post-V1 audit changes priority
- do not close parity items solely because they are reachable through the new hosted-runtime API; close or narrow them only when the underlying parity/correctness gap is pinned by live tests

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
- OI-001 is no longer the next active batch for handoff purposes; after the active hosted-runtime V1 campaign, the next parity execution target is expected to be OI-002 / Tier A2 subscription-runtime parity unless a fresh post-V1 audit changes priority. The remaining OI-001 items are narrower compatibility/divergence follow-ons unless a user explicitly asks to reopen protocol wire-close work.

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
- authoritative pins for the latest A2 query-only closures now include `query/sql/parser_test.go` (`TestParseSingleTableColumnProjection`, `TestParseMultiColumnProjectionWithWhere`, `TestParseSingleTableColumnProjectionWithAlias`, `TestParseSingleTableColumnProjectionWithBareAliasAndWhere`, `TestParseMultiColumnProjectionWithAliasesAndWhere`, `TestParseQualifiedSingleTableColumnProjectionWithAlias`, `TestParseJoinColumnProjection`, `TestParseJoinColumnProjectionProjectsRight`, `TestParseJoinColumnProjectionAllowsMixedRelations`, `TestParseCountStarAliasProjection`, `TestParseCountStarBareAliasProjection`, `TestParseCountStarAliasProjectionWithWhere`, `TestParseCountStarBareAliasProjectionWithWhere`, `TestParseJoinWhereColumnEquality`, `TestParseRejectsJoinWhereColumnEqualityRequiresQualifiedColumns`, `TestParseJoinCountStarAliasProjection`, `TestParseJoinCountStarBareAliasProjectionWithWhere`, `TestParseRejectsUnsupported`), `protocol/handle_oneoff_test.go` (`TestHandleOneOffQuery_UnindexedJoinReturnsRows`, `TestHandleOneOffQuery_CrossJoinWhereColumnEqualityReturnsProjectedRows`, `TestHandleOneOffQuery_ParityBareColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityMultiColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityAliasedBareColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityAliasedBareColumnProjectionWithWhereReturnsProjectedRows`, `TestHandleOneOffQuery_ParityAliasedMultiColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityJoinColumnProjectionReturnsProjectedRows`, `TestHandleOneOffQuery_ParityJoinColumnProjectionProjectsRight`, `TestHandleOneOffQuery_ParityJoinColumnProjectionAllowsMixedRelations`, `TestHandleOneOffQuery_ParityCountAliasReturnsSingleAggregateRow`, `TestHandleOneOffQuery_ParityCountBareAliasReturnsSingleAggregateRow`, `TestHandleOneOffQuery_ParityCountAliasWithWhereReturnsSingleAggregateRow`, `TestHandleOneOffQuery_ParityCountAliasZeroRowsReturnsSingleZeroRow`, `TestHandleOneOffQuery_ParityJoinCountAliasReturnsSingleAggregateRow`, `TestHandleOneOffQuery_ParityJoinCountBareAliasWithWhereReturnsSingleAggregateRow`), `subscription/validate_test.go` (`TestValidateQueryPredicate_UnindexedJoinAllowed`, `TestValidateQueryPredicate_UnindexedJoinStillValidatesStructure`, `TestValidateJoinUnindexed`), and `protocol/handle_subscribe_test.go` (`TestHandleSubscribeSingle_UnindexedJoinStillRejected`, `TestHandleSubscribeSingle_CrossJoinWhereColumnEqualityStillRejected`, `TestHandleSubscribeSingle_ParityBareColumnProjectionRejected`, `TestHandleSubscribeSingle_ParityAliasedBareColumnProjectionRejected`, `TestHandleSubscribeSingle_ParityJoinColumnProjectionRejected`, `TestHandleSubscribeSingle_ParityCountAliasRejected`, `TestHandleSubscribeSingle_ParityCountBareAliasRejected`, `TestHandleSubscribeSingle_JoinCountAggregateStillRejected`), `TestParseJoinOnEqualityWithFilter`, `TestParseJoinOnEqualityWithFilterOnLeftSide`, `TestParseJoinOnEqualityWithFilterAndWhere`, `TestParseJoinOnEqualityParityWithWhereForm`, `TestParseRejectsJoinOnFilterMultipleConjuncts`, `TestParseRejectsJoinOnFilterOr`, `TestParseRejectsJoinOnFilterColumnVsColumn`, `TestParseRejectsJoinOnFilterUnqualifiedColumn`, `TestParseRejectsJoinOnFilterThirdRelation`, `TestHandleOneOffQuery_JoinOnEqualityWithFilterReturnsFilteredRows`, `TestHandleOneOffQuery_JoinOnEqualityWithFilterMatchesWhereForm`, `TestHandleSubscribeSingle_JoinOnEqualityWithFilterAccepted`, `TestHandleSubscribeSingle_JoinOnEqualityWithFilterUnindexedRejected`
- the supported SQL surface is still intentionally narrower than the reference SQL path
- row-level security / per-client filtering remains absent
- broader A2/runtime-model gaps remain; after the join-backed `COUNT(*) [AS] alias` closure, the next bounded residual should be chosen from fresh live evidence rather than carried forward from stale pre-closure handoff notes

Execution note:
- OI-002 remains the next parity/runtime-model handoff issue after the active hosted-runtime V1 campaign; it is not the current implementation handoff while `docs/hosted-runtime-planning/V1-*` work is in progress
- the JOIN ON equality-plus-single-relation-filter widening slice is now closed and pinned; subscribe acceptance is treated as a transparent parser admission rather than a one-off-only divergence because the ON-form and WHERE-form produce indistinguishable parser output
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

### OI-004: Protocol lifecycle still needs hardening around goroutine ownership and shutdown

Status: open
Severity: high

Summary:
- several concrete sub-hazards were closed and pinned by regression tests
- the remaining issue is the broader lifecycle/shutdown theme, not those already-closed sub-slices
- hosted-runtime V1-D/V1-E now provide live mitigation for the normal root-runtime path: `Runtime.Start`, `Runtime.Close`, protocol graph ownership, `HTTPHandler`, `ListenAndServe`, swappable fan-out sender wiring, and `ConnManager.CloseAll(ctx, inbox)` before executor shutdown
- other detached goroutine sites and ownership seams remain watch items if a concrete leak site surfaces
- `ClientSender.Send` is still synchronous without its own ctx, but no concrete consumer currently requires widening that surface

Why this matters:
- lifecycle races and shutdown bugs undermine confidence even when nominal tests pass
- this is still one of the main blockers to calling the runtime trustworthy for serious private use

Primary code surfaces:
- `runtime_lifecycle.go`
- `runtime_network.go`
- `protocol/upgrade.go`
- `protocol/conn.go`
- `protocol/disconnect.go`
- `protocol/keepalive.go`
- `protocol/lifecycle.go`
- `protocol/outbound.go`
- `protocol/sender.go`
- `protocol/async_responses.go`

Source docs:
- `docs/hosted-runtime-planning/V1-D/`
- `docs/hosted-runtime-planning/V1-E/`
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B

Audit note:
- do not close OI-004 until V1-E/V1-H verification proves the hosted runtime can serve WebSocket clients, close connections, stop protocol delivery, and shut down without stranded goroutines; after that, either close this issue or replace it with concrete remaining lower-level protocol lifecycle hazards

### OI-005: Snapshot and committed-read-view lifetime rules still need stronger safety guarantees

Status: open
Severity: high

Summary:
- the enumerated narrow sub-hazards were closed and pinned by regression tests
- the remaining issue is the broader lifetime/ownership theme around read handles and raw access surfaces
- hosted-runtime V1-F mitigates the normal root-runtime read path by exposing callback-scoped `Runtime.Read(ctx, fn)` and closing the committed snapshot before returning
- current safety still relies partly on discipline and observational pins rather than machine-enforced lifetime

Why this matters:
- long-lived or misused read views can distort concurrency assumptions
- this weakens confidence in subscription evaluation and recovery-side read paths

Primary code surfaces:
- `runtime_local.go`
- `store/snapshot.go`
- `store/committed_state.go`
- `store/state_view.go`
- `subscription/eval.go`
- `executor/executor.go`

Source docs:
- `docs/hosted-runtime-planning/V1-F/`
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B

Audit note:
- after V1-F verification, narrow this issue to any remaining lower-level raw snapshot/read-view surfaces if the root hosted-runtime API is pinned safe

### OI-006: Subscription fanout still carries aliasing and cross-subscriber mutation risk concerns

Status: open
Severity: medium

Summary:
- the known narrow slice-header and row-payload-sharing sub-hazards were closed and pinned by regression tests
- hosted-runtime V1-E adds protocol-backed fan-out delivery through a private swappable sender, so the new root-runtime delivery path should be included in the next fanout aliasing audit
- the remaining issue is broader fanout/read-only-discipline risk if future code introduces in-place mutation or shared-state assumptions

Why this matters:
- cross-subscriber mutation or aliasing bugs are subtle and can silently corrupt delivery behavior
- this weakens confidence in both parity and correctness claims

Primary code surfaces:
- `runtime_network.go`
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/hosted-runtime-planning/V1-E/`
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B

Audit note:
- keep open until the protocol-backed hosted-runtime fanout path has explicit aliasing/no-cross-subscriber-mutation coverage, or until the audit confirms existing lower-level pins fully cover the new sender path

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

### OI-013: SchemaRegistry direct subscription-lookup compatibility (closed 2026-04-22)

Status: closed 2026-04-22
Severity: medium

Realized closure:
- `schema.SchemaLookup` and `schema.SchemaRegistry` now expose `ColumnCount(TableID) int`, so the built registry satisfies `subscription.SchemaLookup` directly.
- `protocol.SchemaLookup` now embeds the schema-owned lookup surface, which lets one-off query admission reuse subscription validation helpers without a protocol-local adapter seam.
- The example bootstrap and hosted bootstrap docs now pass `reg` directly to `subscription.NewManager(...)`; the old `schemaLookupAdapter` shim was removed.
- A compile-time pin now asserts `schema.SchemaRegistry` satisfies `subscription.SchemaLookup` directly.

Verification:
- `rtk go test ./subscription -count=1`
- `rtk go test ./... -count=1`

Source docs / surfaces:
- `schema/registry.go`
- `protocol/handle_subscribe.go`
- `cmd/shunter-example/main.go`
- `docs/hosted-runtime-bootstrap.md`
- `subscription/oi013_registry_lookup_test.go`

### OI-014: Hosted runtime V1 surface is in progress but not fully proven as the normal app path

Status: open — actively resolving under hosted-runtime V1
Severity: medium

Summary:
- the root importable package now exists and exposes the core hosted-runtime vocabulary: `Module`, `Config`, `Runtime`, and `Build`
- the live root runtime now owns build/recovery foundation, lifecycle start/close, protocol serving hooks, local reducer/read helpers, and initial describe/export helpers
- V1-H hello-world replacement is still the remaining proof that the top-level API has replaced manual subsystem wiring as the normal app-author path
- the old manual example/docs may still make subsystem assembly look like the primary hosted-runtime story until V1-H demotes or replaces them

Why this matters:
- this is the active bridge between "working subsystems/example" and "Shunter is a usable hosted runtime/server"
- until the V1-H proof lands, the public API may exist without being the demonstrated default user path
- the residual risk is now less about missing root symbols and more about end-to-end proof, docs/examples, and any remaining V1 polish

Primary code surfaces:
- `module.go`
- `config.go`
- `runtime.go`
- `runtime_build.go`
- `runtime_lifecycle.go`
- `runtime_network.go`
- `runtime_local.go`
- `runtime_describe.go`
- `cmd/shunter-example/main.go`
- `docs/hosted-runtime-bootstrap.md`

Grounded evidence from the 2026-04-24 audit pass:
- `rtk go list .` now succeeds for `github.com/ponchione/shunter`
- `rtk go doc . Runtime` lists `Build`, `Start`, `Close`, `HTTPHandler`, `ListenAndServe`, `CallReducer`, `Read`, `Describe`, and `ExportSchema`
- `runtime.go` stores module identity/config plus recovered state, reducer registry, lifecycle-owned workers, protocol graph fields, and serving state
- `runtime_network.go` wires `protocol.Server`, `executor.ProtocolInboxAdapter`, `protocol.ConnManager`, `protocol.NewClientSender`, protocol-backed fan-out delivery, `HTTPHandler`, and `ListenAndServe`
- `runtime_local.go` provides local reducer calls through the runtime-owned executor and callback-scoped snapshot reads
- `runtime_describe.go` provides initial detached module/runtime description and schema export helpers

Remaining resolution criteria:
- complete and verify V1-H: a hosted hello-world example must build/start/serve through the top-level API, connect over WebSocket, call a reducer, observe subscription updates, and shut down cleanly
- update or demote the old manual bootstrap docs/example so new app authors do not treat subsystem assembly as the normal path
- after V1-H verification, close OI-014 or split only concrete residual polish items into narrower issues

### OI-008: The repo still lacks a coherent top-level engine/bootstrap story (closed 2026-04-22)

Status: closed
Severity: medium

Summary (closed):
- `cmd/shunter-example/main.go` is the first end-to-end bootstrap: schema → committed state → commit-log durability → executor → protocol server, with graceful SIGINT/SIGTERM shutdown. Anonymous auth by default so the server can be dialed without an external IdP.
- `docs/hosted-runtime-bootstrap.md` documents the minimal wiring surface with a diagram and step-by-step walkthrough. Two glue adapters (`durabilityAdapter` for the `uint64`↔`types.TxID` shim, `stateAdapter` for `*CommittedState`↔`CommittedStateAccess`) are called out as the only non-obvious wiring. Adapters live in `main.go`, not in a shared package, so they stay discoverable alongside the example.
- Cold-boot path bootstraps an empty committed state and writes an initial snapshot at TxID 0 when `commitlog.OpenAndRecoverDetailed` returns `ErrNoData`, then re-runs recovery. This is the `main.go openOrBootstrap` helper.

Realized surfaces:
- `cmd/shunter-example/main.go` — `run(ctx, addr, dataDir)`, `buildEngine(ctx, dataDir)`, `openOrBootstrap(dir, reg)`, `durabilityAdapter`, `stateAdapter`, `sayHello` reducer
- `cmd/shunter-example/main_test.go` — 3 smoke pins: `TestBuildEngine_BootstrapThenRecover` (cold-boot + recovery-replay), `TestBuildEngine_AdmitsAnonymousConnection` (WebSocket dial → 101 Upgrade → InitialConnection), `TestRun_ShutsDownCleanlyOnContextCancel` (ctx cancel → clean exit)
- `docs/hosted-runtime-bootstrap.md` — hosted bootstrap walkthrough with wiring diagram, seven numbered steps, and scope callouts

Intentionally out of scope (carried forward):
- Strict auth — example uses anonymous so it is dialable without an IdP.
- The broader hosted-runtime surface gap beyond this bootstrap example is tracked separately under OI-014.
- The subscription `SchemaLookup` adapter seam (`ColumnCount`) is tracked separately under OI-013.
- `protocol/handle_oneoff_test.go` was already stale against the working-tree `TableByName` 3-value return before this session; still out of scope per OI-011 carry-forward note. Not regressed by this slice.

Verification:
- `rtk go build ./cmd/shunter-example` → clean
- `rtk go test ./cmd/shunter-example -count=1 -race` → 3 passed
- `rtk go vet ./cmd/shunter-example` → `Go vet: No issues found`
- `rtk go fmt ./cmd/shunter-example` → clean
- `rtk go test ./schema ./store ./subscription ./executor ./commitlog ./cmd/shunter-example -count=1` → 911 passed (baseline 908 from these 5 packages + 3 new)

Source docs:
- `docs/hosted-runtime-bootstrap.md`
- `cmd/shunter-example/main.go`
- `cmd/shunter-example/main_test.go`

### OI-009: Executor startup orchestration + dangling-client sweep (closed 2026-04-22)

Status: closed
Severity: high

Summary (closed):
- `Executor.Startup(ctx, *Scheduler)` is the single owner for the executor-side startup sequence required by SPEC-003 §10.6 / §13.5 / Story 3.6. Runs scheduler replay → dangling-client sweep → flip `externalReady`. Idempotent via `sync.Once`; if the sweep's post-commit pipeline latches executor-fatal mid-sequence, Startup returns the error and leaves the gate closed.
- `Executor.sweepDanglingClients` (Story 7.5) iterates surviving `sys_clients` rows from the post-recovery committed state and deletes each via a fresh cleanup transaction — no OnDisconnect reducer is invoked, reusing the cleanup-only pattern already at `executor/lifecycle.go:70-92`. The cleanup commit still runs the post-commit pipeline with `source=CallSourceLifecycle` so subscribers observe each delete.
- External admission is gated on `SubmitWithContext` (the protocol adapter's only submit entrypoint). Pre-Startup calls return `ErrExecutorNotStarted`; `Submit` (the in-process / test entrypoint) is deliberately ungated so direct internal callers own their ordering. Scope matches SPEC-003's "external reducer or subscription-registration command" wording.
- Scheduler `ReplayFromCommitted()` is called before the sweep when a non-nil `*Scheduler` is passed; past-due `sys_scheduled` rows are enqueued into the executor inbox ahead of any external admission.

Realized surfaces:
- `executor/executor.go` — `Startup(ctx, *Scheduler) error`, `externalReady atomic.Bool`, `startupOnce sync.Once`, gated `SubmitWithContext`
- `executor/lifecycle.go` — `sweepDanglingClients(ctx) error`
- `executor/errors.go` — `ErrExecutorNotStarted`
- `executor/startup_test.go` — 14 pins across two sections: external-gate pins (1-4), sweep pins (5-10), replay/handoff/cancel pins (11-14)

Intentionally out of scope for this slice (captured here so a future slice can decide):
- Starting `Scheduler.Run` / `Executor.Run` from inside Startup. The method takes `*Scheduler` only for the replay step; goroutine lifecycle remains the caller's responsibility. Folding goroutine ownership would broaden the surface past Story 3.6's "single orchestration point" acceptance criterion. Revisit if / when a top-level `cmd/shunter-example` bootstrap lands (OI-008).
- Bounded-inbox backpressure during replay. A recovered schedule count exceeding `InboxCapacity` would block the replay step. Inherited from existing `Scheduler.enqueueWithContext` behavior; not made worse.

Verification:
- `rtk go test ./executor -run Startup -count=1` → 14 passed
- `rtk go test ./... -count=1` → 1560 passed (baseline 1546 + 14 new)
- `rtk go vet ./executor` → no issues
- `rtk go fmt ./executor` → clean

Source docs:
- `docs/decomposition/003-executor/SPEC-003-executor.md` §10.6, §13.5
- `docs/decomposition/003-executor/epic-3-executor-core/story-3.6-startup-orchestration.md`
- `docs/decomposition/003-executor/epic-7-lifecycle-reducers/story-7.5-startup-dangling-client-sweep.md`

### OI-010: Store range-bounds API incomplete vs SPEC-001 (closed 2026-04-22)

Status: closed
Severity: high

Summary (closed):
- `BTreeIndex.SeekBounds(low, high Bound) iter.Seq[RowID]` landed in `store/btree_index.go` per SPEC-001 §4.6 / Story 3.3. Independent inclusive / exclusive / unbounded endpoints. Binary-search start position with exclusive-bound advance; upper bound enforced per-entry; early break supported.
- `Index.SeekBounds(low, high Bound)` thin wrapper in `store/index.go` (passes through to the underlying BTreeIndex) so `*Index` callers match the spec surface exactly.
- `StateView.SeekIndexBounds(tableID, indexID, low, high Bound)` landed in `store/state_view.go` per SPEC-001 §5.4 / Story 5.3. Delegates to `BTreeIndex.SeekBounds` for the committed range, filters through `tx.deletes` + live `Table.GetRow` visibility, then linear-scans `tx.inserts` with `matchesLowerBound` / `matchesUpperBound` for the tx-local side. BTree walk is `slices.Collect`-ed at the StateView boundary — same OI-005 aliasing hazard closure as `SeekIndexRange`.

Realized surfaces:
- `store/btree_index.go` — `SeekBounds(low, high Bound) iter.Seq[types.RowID]`
- `store/index.go` — `Index.SeekBounds(low, high Bound) iter.Seq[types.RowID]`
- `store/state_view.go` — `StateView.SeekIndexBounds(tableID, indexID, low, high Bound) iter.Seq[types.RowID]`
- `store/btree_index_seekbounds_test.go` — 16 pins (inclusive/exclusive/mixed/unbounded/empty/same-key ordering/early-break/Index-wrapper passthrough)
- `store/state_view_seekindexbounds_test.go` — 13 pins (bound edges × tx-local/committed × tx.deletes × unknown table/index × deleted-committed filter × BTree-mutation aliasing × early-break)

Intentionally out of scope for this slice (captured here so a future slice can decide):
- Consumer migration of `subscription/eval.go` to the new StateView surface. Current Tier-3 fallback is safe; migration is a separate follow-on.

Verification:
- `rtk go test ./store -run "SeekBounds|SeekIndexBounds" -count=1` → 29 passed
- `rtk go test ./store -count=1` → 108 passed
- `rtk go test ./... -count=1` → 1589 passed (baseline 1560 + 29 new)
- `rtk go vet ./store` → no issues
- `rtk go fmt ./store` → clean

Source docs:
- `docs/decomposition/001-store/SPEC-001-store.md` §4.6, §5.4
- `docs/decomposition/001-store/epic-3-btree-index-engine/story-3.3-range-scan.md`
- `docs/decomposition/001-store/epic-5-transaction-layer/story-5.3-state-view.md`

### OI-011: Schema contract drift from SPEC-006

Status: closed 2026-04-22
Severity: medium

Realized closure (2026-04-22):
- Interface canonicalization: `SchemaLookup` + `IndexResolver` already lived in `schema/registry.go` and embedded in `SchemaRegistry` per SPEC-006 §7. A duplicate `IndexResolver` declaration in `subscription/placement.go` was removed; `subscription` now re-exports the canonical type via `type IndexResolver = schema.IndexResolver` in `subscription/predicate.go`. `protocol/handle_subscribe.go` retains a narrower local `SchemaLookup` (single-method `TableByName`) which is the spec-sanctioned consumer-side narrowing (SPEC-006 §7 "consumer packages may also declare narrower local interfaces").
- Sentinels: all six sentinels — `ErrReservedReducerName`, `ErrNilReducerHandler`, `ErrDuplicateLifecycleReducer`, `ErrInvalidTableName`, `ErrEmptyColumnName`, `ErrColumnNotFound` — confirmed present in `schema/errors.go`. `ErrColumnNotFound` is now the canonical schema-layer value; `store/errors.go` and `subscription/errors.go` aliased via `= schema.ErrColumnNotFound` so `errors.Is` matches across package boundaries (SPEC-001 §9, SPEC-004 EPICS Epic 1).
- Validation gates: `schema/validate_structure.go` now routes invalid-pattern table names through `ErrInvalidTableName` (was `ErrEmptyTableName`), empty column names through `ErrEmptyColumnName`, and missing-index-column refs through `ErrColumnNotFound`. `schema/validate_schema.go` now wraps reserved-name / nil-handler / duplicate-lifecycle paths in the matching sentinels instead of bare string errors.

Pins landed:
- `schema/oi011_pins_test.go` (new, 7 pins): `SchemaRegistry` satisfies both interfaces; `Build()` returns `ErrReservedReducerName` / `ErrNilReducerHandler` / `ErrDuplicateLifecycleReducer` / `ErrInvalidTableName` / `ErrEmptyColumnName` / `ErrColumnNotFound` at the expected call sites.
- `subscription/oi011_pins_test.go` (new, 2 pins): `subscription.IndexResolver` alias equivalence; `subscription.ErrColumnNotFound == schema.ErrColumnNotFound` with `errors.Is` across wraps.
- `store/oi011_pins_test.go` (new, 1 pin): `store.ErrColumnNotFound == schema.ErrColumnNotFound`.
- `schema/audit_regression_test.go`: migrated existing reducer/lifecycle/missing-column audits from `strings.Contains` to `errors.Is` against the new sentinels.

Verification:
- `rtk go test ./schema -count=1` → 121 passed (114 prior + 7 new).
- `rtk go test ./schema ./subscription ./store -count=1` → 551 passed.
- `rtk go vet ./schema ./subscription ./store` → `Go vet: No issues found`.
- `rtk go fmt ./schema ./subscription ./store` → clean.

Explicitly out of scope (carried forward):
- `docs/decomposition/006-schema/epic-5-validation-build/story-5.5-reducer-schema-validation.md` acceptance still references pre-sentinel text; doc refresh folds into OI-012.
- `subscription/eval.go` Tier-3 fallback rewire to `StateView.SeekIndexBounds` (carried from OI-010).

Source docs:
- `docs/decomposition/006-schema/SPEC-006-schema.md` §7, §9, §13
- `docs/decomposition/006-schema/epic-5-validation-build/story-5.4-registry-interfaces.md`
- `docs/decomposition/006-schema/epic-5-validation-build/story-5.5-validation-errors.md`

### OI-012: Decomposition spec docs stale vs realized code (closed 2026-04-22)

Status: closed 2026-04-22
Severity: low

Realized closure (2026-04-22):
- **SPEC-002 §3.1 / §3.3 — BSATN widening documented.** Disclaimer updated from "0–12 for 13 scalars" to "0–18 for 19 scalar kinds". `ValueKind` table extended with tags 13 (Int128, 16 bytes LE two's-complement), 14 (Uint128, 16 bytes LE), 15 (Int256, 32 bytes LE two's-complement), 16 (Uint256, 32 bytes LE), 17 (Timestamp, int64 Unix-nanos), 18 (ArrayString, uint32 element count + length-prefixed UTF-8 elements). Widening-history note added pointing at `types/value.go` + `bsatn/encode.go` as canonical sources; direction-of-drift is widening-only, existing tags never renumber.
- **SPEC-005 §6 — Message tag tables aligned with `protocol/tags.go`.** Client→Server expanded from 4 to 6 tags (adds `SubscribeMulti`=5, `UnsubscribeMulti`=6; renames `Subscribe`/`Unsubscribe` → `SubscribeSingle`/`UnsubscribeSingle`). Server→Client expanded from 7 to 10 tags with tag 7 flagged **RESERVED** (formerly `ReducerCallResult`, held to prevent silent reallocation) and new tags 8 `TransactionUpdateLight`, 9 `SubscribeMultiApplied`, 10 `UnsubscribeMultiApplied`.
- **SPEC-005 §7 — SQL wire surface documented.** §7.1 `Subscribe` rewritten as `SubscribeSingle` carrying `query_string`; §7.1.1 Query Format replaces the structured `Query{table_name, predicates[]}` description with the Phase 2 Slice 1 SQL subset (`query/sql.Parse`). §7.1b adds `SubscribeMulti` (`query_strings: []string`). §7.2 split into `UnsubscribeSingle` + §7.2b `UnsubscribeMulti`. §7.3 `CallReducer` gains the `flags` byte (`FullUpdate` / `NoSuccessNotify`). §7.4 `OneOffQuery` now carries `message_id: bytes + query_string: string` per Phase 2 Slice 1c.
- **SPEC-005 §8 — Phase 1.5 outcome model documented.** §8.2/§8.3 renamed Applied envelopes with the Single/Multi split. §8.4 `SubscriptionError` updated to the Optional<RequestID/QueryID/TableID> shape. §8.5 `TransactionUpdate` rewritten as the heavy caller-bound envelope carrying `UpdateStatus` (`Committed{update}` | `Failed{error}` | `OutOfEnergy{}`), `CallerIdentity`, `CallerConnectionID`, `ReducerCall` (`ReducerCallInfo`), `Timestamp`, `EnergyQuantaUsed` (stub), `TotalHostExecutionDuration`. §8.7 marked RESERVED (tag 7) with pointers to the Phase 1.5 decision doc and pin tests. §8.8 added for `TransactionUpdateLight` (non-caller delta-only). §8.10/§8.11 documented SubscribeMulti/UnsubscribeMulti Applied envelopes.
- **SPEC-005 §9 / §10 / §11 / §13 / §15 / §16 / §17 — consequential rewrites.** State machine / client-cache rules / ordering guarantees / `ClientSender` interface / Divergences table / Verification table all updated to reference the Single/Multi families, the heavy/light envelope split, `Status::Committed.update` for caller delta, and the reserved-tag-7 rule. Open Question 1 (query language evolution) marked closed by Phase 2 Slice 1.
- **Story 5.5 — acceptance bullets cite canonical sentinels.** `docs/decomposition/006-schema/epic-5-validation-build/story-5.5-reducer-schema-validation.md` acceptance rewritten to assert via `errors.Is(err, ErrX)` against the OI-011 sentinels (`ErrReservedReducerName`, `ErrNilReducerHandler`, `ErrDuplicateLifecycleReducer`, `ErrDuplicateReducerName`, `ErrSchemaVersionNotSet`, `ErrReservedTableName`, `ErrNoTables`). OI-011 pin tests cross-referenced as authoritative.

Verification:
- no code changes; doc-only refresh
- `rtk grep` spot-checks: `TagReducerCallResult` / `TransactionUpdateLight` / `StatusCommitted` / `ReducerCallInfo` / `CallReducerFlagsNoSuccessNotify` / `ErrReservedReducerName` / `KindArrayString` all present in the live code surfaces the refreshed specs now cite

Source docs:
- `docs/decomposition/002-commitlog/SPEC-002-commitlog.md` §3.1, §3.3
- `docs/decomposition/005-protocol/SPEC-005-protocol.md` §6, §7, §8, §9, §10, §11, §13, §15, §16, §17
- `docs/decomposition/006-schema/epic-5-validation-build/story-5.5-reducer-schema-validation.md`
- `docs/parity-phase1.5-outcome-model.md` (source for the realized outcome-model shape)
- `docs/spacetimedb-parity-roadmap.md` Phase 2 (narrative for the SQL / multi-query slices)
- `types/value.go` + `bsatn/encode.go` (canonical source for the 19 ValueKind tags)
- `protocol/tags.go` + `protocol/server_messages.go` + `protocol/client_messages.go` (canonical source for the tag table and envelope shapes)

Explicitly out of scope (carried forward):
- The broader hosted-runtime surface gap is tracked under OI-014 rather than this doc-refresh issue.
- The subscription `SchemaLookup` adapter seam is tracked under OI-013 rather than this doc-refresh issue.
- `schema/registry.go` working-tree diff expanding `TableByName` to `(TableID, *TableSchema, bool)`; `protocol/handle_oneoff_test.go` is stale against that three-value return. Flag as its own slice when the registry change is committed; explicitly NOT folded into OI-012.

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
