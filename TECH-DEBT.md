# TECH-DEBT

This file tracks open issues only.
Resolved audit history belongs in git history and the narrow slice docs, not here.

Status conventions:
- open: confirmed issue or parity gap still requiring work
- deferred: intentionally not being closed now

Priority order:
1. externally visible parity gaps
2. correctness / concurrency bugs that undermine parity claims
3. capability gaps that block realistic usage
4. cleanup after parity direction is locked

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
- OI-001 is no longer the next active batch for handoff purposes; the next execution target is OI-002 / Tier A2 subscription-runtime parity. The remaining OI-001 items are narrower compatibility/divergence follow-ons unless a user explicitly asks to reopen protocol wire-close work.

### OI-002: Query and subscription behavior still diverges from the target runtime model

Status: open
Severity: high

Summary:
- many narrow SQL/query parity slices are now landed and pinned
- the surface is still intentionally narrower than the reference SQL path
- the fan-out delivery parity batch is now partly closed: fast-read recipients can bypass durability while confirmed-read recipients still wait, and eval failures now mark the whole connection dropped for executor-side cleanup instead of pruning only the failing subscription
- the join/cross-join multiplicity batch is now closed across compile/hash identity, bootstrap, one-off query execution, and post-commit delta evaluation
- one-off SQL now reuses `subscription.ValidatePredicate(...)` before snapshot evaluation, so unindexed join admission matches subscribe registration instead of bypassing shared join-index validation
- committed join bootstrap plus unregister final-delta rows now preserve projected-side enumeration order regardless of which join side provides the usable index, matching the existing one-off projected-side baseline for accepted join shapes
- post-commit projected join deltas now preserve projected-side semantics too: join fragments are projected before reconciliation so partner churn cancels at the projected-row bag level, and `ReconcileJoinDelta(...)` emits surviving rows in fragment encounter order instead of map iteration order; focused `subscription/delta_dedup_test.go` + `subscription/eval_test.go` pins cover projected-left/right ordering and no-op churn cases
- accepted subscribe SQL using `:sender` now preserves parameterized hash identity through protocol compile → executor adapter → subscription registration, so literal bytes queries no longer collapse onto the same query hash/state as the parameterized caller-bound form and mixed subscribe batches only parameterize the marked predicates
- accepted SQL with neutral `TRUE` terms now normalizes before runtime lowering, so single-table `TRUE AND/OR ...` shapes compile to the same runtime meaning/hash identity as their simplified equivalents and join-backed `TRUE AND rhs-filter` shapes no longer drift into malformed `And{nil, ...}` validation failures
- accepted single-table same-table commutative `AND` / `OR` SQL now canonicalizes child order at the query-hash seam, so already-equivalent one-off row results share one canonical query hash and one shared query state regardless of source child order
- accepted single-table same-table associative `AND` / `OR` SQL with 3+ leaves now canonicalizes grouping at the same identity seam, so left- vs right-associated trees share one canonical query hash and one shared query state while parser/runtime meaning stays unchanged
- accepted single-table same-table duplicate-leaf `AND` / `OR` SQL now also canonicalizes idempotent redundant leaves at that identity seam, so `a`, `a AND a`, and `a OR a` share one canonical query hash and one shared query state while one-off row meaning remains unchanged
- row-level security / per-client filtering remains absent
- broader query/subscription parity is still open beyond the landed narrow shapes, especially predicate normalization / validation drift and other bounded A2 gaps that remain after the closed join-index-validation + committed-ordering + projected-join-delta-ordering + sender-parameter-hash-identity + neutral-TRUE-normalization + commutative-child-order + associative-grouping + duplicate-leaf-idempotence seams

Execution note:
- OI-002 remains the next active handoff issue, but the fan-out delivery batch, the join/cross-join multiplicity batch, the one-off-vs-subscribe join-index validation seam, the committed join bootstrap/final-delta projected-order seam, the projected-join delta-order seam, the sender-parameter hash-identity seam, the neutral-`TRUE` normalization seam, the accepted same-table commutative child-order seam, the accepted same-table associative-grouping seam, and the accepted same-table duplicate-leaf idempotence seam are now closed. The next bounded A2 batch should start from another remaining runtime/model gap after a fresh scout rather than reopening those closed slices or closed A1 protocol work.

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
- several concrete sub-hazards were closed and pinned in narrow slice docs
- the remaining issue is the broader lifecycle/shutdown theme, not those already-closed sub-slices
- other detached goroutine sites and ownership seams remain watch items if a concrete leak site surfaces
- `ClientSender.Send` is still synchronous without its own ctx, but no concrete consumer currently requires widening that surface

Why this matters:
- lifecycle races and shutdown bugs undermine confidence even when nominal tests pass
- this is still one of the main blockers to calling the runtime trustworthy for serious private use

Primary code surfaces:
- `protocol/upgrade.go`
- `protocol/conn.go`
- `protocol/disconnect.go`
- `protocol/keepalive.go`
- `protocol/lifecycle.go`
- `protocol/outbound.go`
- `protocol/sender.go`
- `protocol/async_responses.go`

Source docs:
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-004-watch-reducer-response-lifecycle.md`
- `docs/hardening-oi-004-sender-disconnect-context.md`
- `docs/hardening-oi-004-supervise-disconnect-context.md`
- `docs/hardening-oi-004-closeall-disconnect-context.md`
- `docs/hardening-oi-004-forward-reducer-response-context.md`
- `docs/hardening-oi-004-dispatch-handler-context.md`
- `docs/hardening-oi-004-outbound-writer-supervision.md`

### OI-005: Snapshot and committed-read-view lifetime rules still need stronger safety guarantees

Status: open
Severity: high

Summary:
- the enumerated narrow sub-hazards were closed and pinned
- the remaining issue is the broader lifetime/ownership theme around read handles and raw access surfaces
- current safety still relies partly on discipline and observational pins rather than machine-enforced lifetime

Why this matters:
- long-lived or misused read views can distort concurrency assumptions
- this weakens confidence in subscription evaluation and recovery-side read paths

Primary code surfaces:
- `store/snapshot.go`
- `store/committed_state.go`
- `store/state_view.go`
- `subscription/eval.go`
- `executor/executor.go`

Source docs:
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-005-snapshot-iter-retention.md`
- `docs/hardening-oi-005-snapshot-iter-useafterclose.md`
- `docs/hardening-oi-005-snapshot-iter-mid-iter-close.md`
- `docs/hardening-oi-005-subscription-seam-read-view-lifetime.md`
- `docs/hardening-oi-005-committed-snapshot-indexseek-aliasing.md`
- `docs/hardening-oi-005-state-view-seekindex-aliasing.md`
- `docs/hardening-oi-005-state-view-seekindexrange-aliasing.md`
- `docs/hardening-oi-005-state-view-scan-aliasing.md`
- `docs/hardening-oi-005-committed-state-table-raw-pointer.md`

### OI-006: Subscription fanout still carries aliasing and cross-subscriber mutation risk concerns

Status: open
Severity: medium

Summary:
- the known narrow slice-header and row-payload-sharing sub-hazards were closed and pinned
- the remaining issue is broader fanout/read-only-discipline risk if future code introduces in-place mutation or shared-state assumptions

Why this matters:
- cross-subscriber mutation or aliasing bugs are subtle and can silently corrupt delivery behavior
- this weakens confidence in both parity and correctness claims

Primary code surfaces:
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`

Source docs:
- `docs/current-status.md`
- `docs/spacetimedb-parity-roadmap.md` Tier B
- `docs/hardening-oi-006-fanout-aliasing.md`
- `docs/hardening-oi-006-row-payload-sharing.md`

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
- `protocol.SchemaLookup` now embeds the schema-owned lookup surface, which lets one-off query admission reuse `subscription.ValidatePredicate(...)` without a protocol-local adapter seam.
- The example bootstrap and embedder docs now pass `reg` directly to `subscription.NewManager(...)`; the old `schemaLookupAdapter` shim was removed.
- A compile-time pin now asserts `schema.SchemaRegistry` satisfies `subscription.SchemaLookup` directly.

Verification:
- `rtk go test ./subscription -count=1`
- `rtk go test ./... -count=1`

Source docs / surfaces:
- `schema/registry.go`
- `protocol/handle_subscribe.go`
- `cmd/shunter-example/main.go`
- `docs/embedding.md`
- `subscription/oi013_registry_lookup_test.go`

### OI-014: Shunter still lacks a true embeddable library surface despite the new example bootstrap

Status: open
Severity: medium

Summary:
- the example/bootstrap story is now real, but the module still does not expose the project-brief-style library surface implied by `go get github.com/ponchione/shunter`.
- there is no root importable package and no `engine/` package implementing a top-level runtime owner.
- embedders still have to hand-wire schema, commitlog, executor, subscription, protocol, scheduler, and multiple shim adapters in host code.
- `schema.EngineOptions` advertises runtime knobs (`DataDir`, `ExecutorQueueCapacity`, `DurabilityQueueCapacity`, `EnableProtocol`) that are not consumed by the live runtime path; only `StartupSnapshotSchema` is read by `Engine.Start()`.

Why this matters:
- this is now the main remaining gap between "there is a working example" and "Shunter is a usable Go library you can embed"
- the current public surface is still subsystem-oriented rather than application-embedder-oriented
- no-op or effectively inert runtime options are risky because embedders may believe they are configuring behavior that the system ignores

Primary code surfaces:
- `schema/build.go`
- `schema/builder.go`
- `schema/version.go`
- `cmd/shunter-example/main.go`
- `docs/embedding.md`
- repo/module root (`go.mod`)

Grounded evidence:
- `docs/project-brief.md:5-9,28,138,203-210` describes Shunter as an embeddable Go library and sketches an `engine/` package for top-level initialization/lifecycle.
- `rtk go list` at the repo root fails with `no Go files in /home/gernsback/source/shunter`.
- `rtk go list ./engine` fails with `stat /home/gernsback/source/shunter/engine: directory not found`.
- Compile-only audit repro (`rtk go test ./.tmp_audit_rootpkg`) fails because `github.com/ponchione/shunter` is not importable as a package.
- `cmd/shunter-example/main.go:99-205` shows the real host-facing bring-up still requires manual assembly of the major subsystems plus the remaining glue adapters that are not yet hidden behind a top-level engine API.
- `docs/embedding.md:10-40,83-124,191-199` documents the explicit subsystem wiring sequence and the two remaining adapter seams (`durabilityAdapter`, `stateAdapter`) that embedders still carry in host code.
- `schema/builder.go:116-128` defines runtime-facing `EngineOptions`, but `schema/version.go:134-135` only consumes `StartupSnapshotSchema`; the other knobs are not used in the live code path.

Recommended resolution options:
- introduce a real public engine package (or root package) that owns config, bring-up, and shutdown across the existing subsystems
- thread the currently advertised `EngineOptions` fields into that surface if they are meant to stay public
- or explicitly narrow the product/docs claim from "embeddable library" to "subsystem toolkit + example wiring" until a true engine API exists

Suggested follow-up tests:
- compile-only smoke proving the advertised public import path/package exists
- minimal start/stop embedder smoke test using the intended top-level API instead of example-local wiring
- config-effect pins proving the exposed runtime options actually influence runtime construction

### OI-008: The repo still lacks a coherent top-level engine/bootstrap story (closed 2026-04-22)

Status: closed
Severity: medium

Summary (closed):
- `cmd/shunter-example/main.go` is the first end-to-end bootstrap: schema → committed state → commit-log durability → executor → protocol server, with graceful SIGINT/SIGTERM shutdown. Anonymous auth by default so the server can be dialed without an external IdP.
- `docs/embedding.md` documents the minimal wiring surface with a diagram and step-by-step walkthrough. Two glue adapters (`durabilityAdapter` for the `uint64`↔`types.TxID` shim, `stateAdapter` for `*CommittedState`↔`CommittedStateAccess`) are called out as the only non-obvious wiring. Adapters live in `main.go`, not in a shared package, so they stay discoverable alongside the example.
- Cold-boot path bootstraps an empty committed state and writes an initial snapshot at TxID 0 when `commitlog.OpenAndRecoverDetailed` returns `ErrNoData`, then re-runs recovery. This is the `main.go openOrBootstrap` helper.

Realized surfaces:
- `cmd/shunter-example/main.go` — `run(ctx, addr, dataDir)`, `buildEngine(ctx, dataDir)`, `openOrBootstrap(dir, reg)`, `durabilityAdapter`, `stateAdapter`, `sayHello` reducer
- `cmd/shunter-example/main_test.go` — 3 smoke pins: `TestBuildEngine_BootstrapThenRecover` (cold-boot + recovery-replay), `TestBuildEngine_AdmitsAnonymousConnection` (WebSocket dial → 101 Upgrade → InitialConnection), `TestRun_ShutsDownCleanlyOnContextCancel` (ctx cancel → clean exit)
- `docs/embedding.md` — embedder walkthrough with wiring diagram, seven numbered steps, and scope callouts

Intentionally out of scope (carried forward):
- Strict auth — example uses anonymous so it is dialable without an IdP.
- The broader embedder/library-surface gap beyond this bootstrap example is tracked separately under OI-014.
- The subscription `SchemaLookup` adapter seam (`ColumnCount`) is tracked separately under OI-013.
- `protocol/handle_oneoff_test.go` was already stale against the working-tree `TableByName` 3-value return before this session; still out of scope per OI-011 carry-forward note. Not regressed by this slice.

Verification:
- `rtk go build ./cmd/shunter-example` → clean
- `rtk go test ./cmd/shunter-example -count=1 -race` → 3 passed
- `rtk go vet ./cmd/shunter-example` → `Go vet: No issues found`
- `rtk go fmt ./cmd/shunter-example` → clean
- `rtk go test ./schema ./store ./subscription ./executor ./commitlog ./cmd/shunter-example -count=1` → 911 passed (baseline 908 from these 5 packages + 3 new)

Source docs:
- `docs/embedding.md`
- `cmd/shunter-example/main.go`
- `cmd/shunter-example/main_test.go`

### OI-009: Executor startup orchestration + dangling-client sweep (closed 2026-04-22)

Status: closed
Severity: high

Summary (closed):
- `Executor.Startup(ctx, *Scheduler)` is the single owner for the executor-side startup sequence required by SPEC-003 §10.6 / §13.5 / Story 3.6. Runs scheduler replay → dangling-client sweep → flip `externalReady`. Idempotent via `sync.Once`; if the sweep's post-commit pipeline latches executor-fatal mid-sequence, Startup returns the error and leaves the gate closed.
- `Executor.sweepDanglingClients` (Story 7.5) iterates surviving `sys_clients` rows from the post-recovery committed state and deletes each via a fresh cleanup transaction — no OnDisconnect reducer is invoked, reusing the cleanup-only pattern already at `executor/lifecycle.go:70-92`. The cleanup commit still runs the post-commit pipeline with `source=CallSourceLifecycle` so subscribers observe each delete.
- External admission is gated on `SubmitWithContext` (the protocol adapter's only submit entrypoint). Pre-Startup calls return `ErrExecutorNotStarted`; `Submit` (the in-process / test entrypoint) is deliberately ungated so embedder-direct callers own their ordering. Scope matches SPEC-003's "external reducer or subscription-registration command" wording.
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
- The broader embedder/library-surface gap is tracked under OI-014 rather than this doc-refresh issue.
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