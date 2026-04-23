# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

For provenance of closed slices, use `rtk git log` — this file tracks only current state and forward motion. Do not re-narrate closed slices here.

## Current state (live only)

- All OIs referenced in the 2026-04-22 audit chain (OI-008 through OI-012) are closed.
- All OI-001 A1 protocol wire-close slices identified to date are closed: `SubscriptionError` duration field + RequestID/QueryID None parity, `CallReducer` field order, `TransactionUpdate` field order + `EnergyQuantaUsed` u64→u128 width, applied-envelope field order (all four variants), `IdentityToken` rename + field order, `Unsubscribe.SendDropped` removal, `OneOffQueryResponse` rename + field order. All pinned in `protocol/parity_*_test.go`.
- Phase 2 Slice 4 rows-shape cluster (flat `[]SubscriptionUpdate` / `TableName+Rows` vs reference `SubscribeRows` / `DatabaseUpdate` wrapper chain) is closed as documented divergence — `docs/parity-phase2-slice4-rows-shape.md`. Reopening requires a new decision doc that *also* closes SPEC-005 §3.4 `BsatnRowList` row-list deferral. Do not attempt a partial close.
- `cmd/shunter-example` runs schema → commitlog recovery → durability → executor → WebSocket with anonymous auth, subscription fan-out, and a real `*Scheduler` owned by the example.
- Before changing a file, verify against live code — memory/ledger claims can drift.

## Live constraints (carry forward)

- The OI-001 A1 receipt-timestamp seam batch is closed: `SubscriptionError.TotalHostExecutionDurationMicros`, `OneOffQueryResponse.TotalHostExecutionDuration`, `Subscribe{Single,Multi}Applied.TotalHostExecutionDurationMicros`, and `Unsubscribe{Single,Multi}Applied.TotalHostExecutionDurationMicros` now emit measured non-zero microsecond values through the admission / evaluation / one-off paths, and `TransactionUpdate.Timestamp` / `TransactionUpdate.TotalHostExecutionDuration` now carry microseconds to match reference SATS semantics.
- `EnergyQuantaUsed` on the wire is a 16-byte u128 LE that always emits zeros. Shunter has no energy model; widening to honest u128 values is not on the roadmap.
- `cmd/shunter-example` remains on anonymous auth; strict auth wiring is still out of scope for this queue.
- `Scheduler.ReplayFromCommitted` still uses `context.Background()`; a recovered schedule count exceeding the executor inbox capacity would block replay. Inherited backpressure, not introduced here.
- `TECH-DEBT.md` now marks OI-002 / Tier A2 as the next active execution issue; use that as the tie-breaker if stale docs suggest reopening A1 protocol work.
- The first OI-002 fan-out delivery batch is now closed: `subscription/fanout_worker.go` delivers fast-read recipients without global `TxDurable` blocking while confirmed-read recipients still wait, and `subscription/eval.go` now marks eval-failure connections dropped for executor-side cleanup instead of pruning only the failing subscription.
- The OI-002 join/cross-join multiplicity batch is now closed: `subscription.CrossJoin` carries projection-side/self-alias identity, cross joins preserve cartesian multiplicity across bootstrap/one-off/delta paths, and one-off equi-join projection now preserves bag semantics instead of semijoin-style dedup.
- The one-off-vs-subscribe unindexed-join validation seam is now closed: `protocol/handle_oneoff.go` runs the shared `subscription.ValidatePredicate(...)` gate before snapshot evaluation, so one-off SQL rejects the same unindexed join admission shapes subscribe registration already rejects.
- The committed join projected-order seam is now closed: `subscription/register_set.go` enumerates projected-side rows first for bootstrap and unregister final-delta joins, so accepted join SQL no longer flips visible committed-row order solely because the usable index lives on the opposite join side.
- The projected join delta-order seam is now closed too: `subscription/eval.go` now projects join fragments before reconciliation so partner churn cancels at the projected-row bag level, `subscription/delta_dedup.go` preserves fragment encounter order for surviving rows, and focused `subscription/delta_dedup_test.go` + `subscription/eval_test.go` pins cover projected-left/right ordering plus no-op churn cases.
- The `:sender` subscribe hash-identity seam is now closed: `protocol/handle_subscribe.go` preserves caller-bound parameter provenance, `protocol/lifecycle.go` / `executor/protocol_inbox_adapter.go` forward per-predicate hash identities, and `subscription/register_set.go` hashes mixed batches per predicate instead of request-globally. Literal bytes queries no longer collide with the caller-parameterized form, and mixed batches only parameterize marked predicates.
- The neutral-`TRUE` accepted-shape normalization seam is now closed too: `protocol/handle_subscribe.go` normalizes SQL predicate trees before runtime lowering, and `subscription/hash.go` / `subscription/register_set.go` canonicalize neutral-`AllRows` runtime forms so single-table `TRUE AND/OR ...` shapes share the same runtime meaning/query-state identity as their simplified equivalents while join-backed `TRUE AND rhs-filter` shapes no longer drift into malformed validation-failing filters.
- The accepted same-table commutative child-order and associative-grouping seams are now both closed: `subscription/hash.go` canonicalizes same-table `AND` / `OR` groups by flattening same-kind children, sorting them by canonical child bytes, and rebuilding one deterministic tree shape so child order and 3+-leaf grouping no longer fork query hash/query-state identity while one-off row meaning stays unchanged.
- Important guardrails from the just-landed associative-grouping close:
  - `subscription/register_set.go` now validates the original predicate before canonicalization and validates the canonicalized form again afterward, so reductions cannot mask wider invalid shapes such as `ErrTooManyTables`.
  - the canonicalization fence is intentionally narrower than `Tables()==1`: join-containing compounds are excluded from flatten/sort/rebuild so this closure stays bounded to accepted same-table non-join boolean trees.
  - authoritative slice tests now include `TestHandleOneOffQuery_SameTableGroupedAndReturnsSameRows`, `TestHandleOneOffQuery_SameTableGroupedOrReturnsSameRows`, `TestQueryHashSameTableAndAssociativeGroupingCanonicalized`, `TestQueryHashSameTableOrAssociativeGroupingCanonicalized`, `TestRegisterSet_SameTableAndAssociativeGroupingSharesQueryState`, `TestRegisterSet_SameTableOrAssociativeGroupingSharesQueryState`, `TestRegisterSet_CanonicalizationDoesNotMaskTooManyTables`, and `TestQueryHashJoinCompoundOrderMatters`.

## How to frame a session

Work in *batches*, not single slices. One session = one batch. Within a batch, land one commit per slice so each remains reviewable.

1. **Scout once, at the start of the session.** Read the live code surfaces that match the named batch scope, then diff only those against the corresponding reference/docs source. Grep for `parity_`, `documented-divergence`, and the current OI label to filter out already-closed slices. Produce the residual list for that batch only.
2. **Pick the batch's scope boundary** (one OI, or one named follow-on). Write out which items from the residual list fall inside it. Items outside stay for a future session — do not widen mid-batch.
3. **Close each item in turn**, one commit per slice, each with a new `parity_*_test.go` byte-shape pin or a short decision doc under `docs/` matching the Slice 4 / 2γ / subprotocol pattern. Run `rtk go test ./protocol/...` + dependent packages after each, not only at the end.
4. **Stop when** (a) the batch scope is exhausted, (b) the next item would need a new decision doc (stop and write the doc; do not just ship it), or (c) the next item crosses an OI/scope boundary. Do not stop mid-slice because "one slice is enough for today."
5. **Update this file + memory at the end**, then commit and hand off.

Do not open multiple OIs in one batch. Do not reopen closed slices. Do not silently widen into A3 or OI-004/005/006 hardening.

## Next session: continue OI-002 A2 with same-table duplicate-leaf idempotence canonicalization for accepted `AND` / `OR` SQL

The commutative child-order and associative-grouping slices are now closed: `protocol/handle_oneoff_test.go` proves accepted single-table same-table reordered/grouped variants already returned identical rows, while `subscription/hash_test.go` and `subscription/manager_test.go` now pin that reordered and regrouped variants share one canonical query hash and one shared query state.

Fresh post-close scout this session:
- the next bounded residual at the same identity seam is duplicate-leaf idempotence drift on already-accepted same-table shapes.
- a local scout program this session now shows:
  - `hash(a)=6980b026b01eacb3b587a919a32b6605eb2c6092ffbd6d15ca8056baf934dbd2`
  - `hash(a AND a)=3dd0e1eea6b84bcda11b722f9bbcdd68b63fd215cd374ce817af431520d575c3`
  - `hash(a OR a)=b468624ee0cafdd1a6df81fbc078616761823bc1dd0b67dcfedb2048cb31d18a`
- the same scout also showed identical row-level meaning for sample matching and non-matching rows via `subscription.MatchRow(...)`, so this still looks like a canonical query-identity seam rather than a runtime-evaluation bug.
- that keeps the next bounded residual inside OI-002 A2 predicate normalization/canonical identity work without reopening parser widening, joins, `:sender`, neutral-`TRUE`, or fan-out work.

Fresh-agent task:
- close one bounded OI-002 A2 canonicalization slice for accepted same-table duplicate-leaf SQL such as `a`, `a AND a`, and `a OR a`, where visible row meaning is unchanged but canonical query hash/query-state identity still diverges because redundant leaves survive into hashing
- prove first that accepted duplicate-leaf variants already return the same visible rows / query meaning, then pin the hash/query-state drift, then fix only the canonical identity seam
- keep the fix bounded to accepted same-table shapes; do not widen SQL admission or alter join/cross-join semantics

Fresh-agent context:
- closed already in OI-002 A2:
  - recipient-level fan-out durability gating + dropped-client cleanup on eval failure
  - join/cross-join multiplicity across compile/hash identity, bootstrap, one-off, and delta
  - one-off vs subscribe unindexed-join admission parity via shared `subscription.ValidatePredicate(...)`
  - committed join bootstrap/unregister projected-side ordering regardless of usable index side
  - post-commit projected-join delta ordering via projected-fragment-first reconciliation plus encounter-order-preserving `ReconcileJoinDelta(...)`
  - subscribe-side `:sender` hash identity / mixed-batch parameterization provenance
  - neutral-`TRUE` predicate normalization across compile/hash/register seams
  - accepted same-table commutative child-order canonicalization
  - accepted same-table associative-grouping canonicalization
- do not reopen:
  - fan-out delivery parity
  - join/cross-join multiplicity
  - one-off-vs-subscribe join-index validation
  - committed join bootstrap/final-delta projected ordering
  - projected join delta ordering
  - `:sender` subscribe hash identity
  - neutral-`TRUE` normalization
  - accepted same-table commutative child-order work
  - accepted same-table associative-grouping work
  - rows-shape documented divergence
  - OI-001 A1 wire/message-family work

Suggested starting reads for the next batch:
- `subscription/hash.go`
- `subscription/hash_test.go`
- `subscription/manager_test.go`
- `protocol/handle_oneoff_test.go`
- `query/sql/parser.go`
- `protocol/handle_subscribe.go`

Suggested verification path for the next batch:
1. Add focused failing tests for same-table duplicate-leaf `AND` / `OR` shapes.
2. Re-run focused protocol/subscription tests.
3. Run `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol ./subscription ./executor -count=1`.
4. Run `PATH=/usr/local/go/bin:$PATH rtk go test ./... -count=1`.

Out of scope for this batch: OI-001 A3 recovery/store parity, OI-004/005/006 hardening, rows-shape cluster reopen, strict-auth wiring in `cmd/shunter-example`.

## Follow-on queue (pickable next, one per session)

- **OI-002 A2** — subscription-layer parity against `reference/SpacetimeDB/crates/core/src/subscription/`. Next candidate batch: accepted same-table duplicate-leaf idempotence canonicalization for already-supported `AND` / `OR` SQL, or another bounded predicate normalization / validation residual if a fresh scout shows it is stronger. Do not reopen the closed fan-out delivery, multiplicity, join-index-validation, committed projected-ordering, projected-join delta-ordering, `:sender`, neutral-`TRUE`, commutative-child-order, or associative-grouping slices without fresh evidence.
- **OI-001 A3** — recovery / store parity against `reference/SpacetimeDB/crates/core/src/db/`. Batch scope: snapshot/replay invariants beyond what `P0-RECOVERY-*` already covered.
- **Coordinated wrapper-chain + row-list close** (Phase 2 Slice 4 carried-forward deferral). Requires a new decision doc reopening the SPEC-005 §3.4 `BsatnRowList` deferral together with the reference `SubscribeRows` / `DatabaseUpdate` / `TableUpdate` / `CompressableQueryUpdate` / `QueryUpdate` wrapper chain. Scope is large — do not start without a named consumer or a bandwidth trigger (SPEC-005 §3.4 "fixed-schema row delivery bottleneck").
- **Strict-auth wiring in `cmd/shunter-example`**. Currently anonymous-only. Batch scope: JWT identity, token rotation, `IdentityToken` round-trip, upgrade-path handshake.

Pick one follow-on per session and treat it as the batch scope. Do not interleave.

## Startup notes

- Read `CLAUDE.md` first, then `RTK.md` for command rules, then `docs/EXECUTION-ORDER.md` for sequencing.
- Use `rtk git log` for slice provenance; this file is current-state only.
- Before changing a file, verify against live code — memory/ledger claims can drift.
