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

## How to frame a session

Work in *batches*, not single slices. One session = one batch. Within a batch, land one commit per slice so each remains reviewable.

1. **Scout once, at the start of the session.** Read the live code surfaces that match the named batch scope, then diff only those against the corresponding reference/docs source. Grep for `parity_`, `documented-divergence`, and the current OI label to filter out already-closed slices. Produce the residual list for that batch only.
2. **Pick the batch's scope boundary** (one OI, or one named follow-on). Write out which items from the residual list fall inside it. Items outside stay for a future session — do not widen mid-batch.
3. **Close each item in turn**, one commit per slice, each with a new `parity_*_test.go` byte-shape pin or a short decision doc under `docs/` matching the Slice 4 / 2γ / subprotocol pattern. Run `rtk go test ./protocol/...` + dependent packages after each, not only at the end.
4. **Stop when** (a) the batch scope is exhausted, (b) the next item would need a new decision doc (stop and write the doc; do not just ship it), or (c) the next item crosses an OI/scope boundary. Do not stop mid-slice because "one slice is enough for today."
5. **Update this file + memory at the end**, then commit and hand off.

Do not open multiple OIs in one batch. Do not reopen closed slices. Do not silently widen into A3 or OI-004/005/006 hardening.

## Next session: continue OI-002 A2 accepted-shape normalization / validation drift after sender-hash closure

OI-001 A1 is exhausted for both wire-shape and measurement-parity work, and the first OI-002 fan-out delivery batch, the join/cross-join multiplicity batch, the one-off-vs-subscribe join-index validation seam, the committed join bootstrap/final-delta projected-order seam, the projected-join delta-order seam, and the `:sender` subscribe hash-identity seam are now closed. A fresh scout this session did not surface a stronger named residual than the remaining accepted-shape normalization / validation bucket, so the next open protocol-adjacent batch stays in OI-002 A2 and should keep working that bucket rather than reopening any closed join-ordering or `:sender` work.

Fresh-agent task:

- Scout and then close one bounded OI-002 A2 predicate normalization / validation mismatch on an already-accepted SQL shape.
- Compare the accepted SQL compile path against the runtime predicate model used by subscribe registration and one-off execution; look for a shape where parser acceptance exists but normalization / validation / hash identity / execution behavior still disagrees across seams.
- Do not reopen projected-join ordering, join multiplicity, join-index validation, or `:sender` hash identity unless fresh evidence shows a new regression distinct from those closed slices.
- Do not start with a broad SQL widening pass. Stay inside an already-accepted shape and prove the mismatch with focused tests before editing production code.
- If the scout does not confirm a real normalization / validation residual, stop after updating this handoff with the grounded next-best A2 seam instead of forcing speculative churn.

Fresh-agent context:

- Closed already in OI-002 A2:
  - recipient-level fan-out durability gating + dropped-client cleanup on eval failure
  - join/cross-join multiplicity across compile/hash identity, bootstrap, one-off, and delta
  - one-off vs subscribe unindexed-join admission parity via shared `subscription.ValidatePredicate(...)`
  - committed join bootstrap/unregister projected-side ordering regardless of usable index side
  - post-commit projected-join delta ordering via projected-fragment-first reconciliation plus encounter-order-preserving `ReconcileJoinDelta(...)`
  - subscribe-side `:sender` hash identity / mixed-batch parameterization provenance
- Do not reopen:
  - fan-out delivery parity
  - join/cross-join multiplicity
  - one-off-vs-subscribe join-index validation
  - committed join bootstrap/final-delta projected ordering
  - projected join delta ordering
  - `:sender` subscribe hash identity
  - rows-shape documented divergence
  - OI-001 A1 wire/message-family work
- The highest-yield next check is still an accepted SQL shape whose parser/compile path succeeds but whose normalized runtime predicate, validation outcome, canonical hash, or subscribe/one-off behavior still drifts across seams.
- Treat `query/sql/parser.go`, `protocol/handle_subscribe.go`, `protocol/handle_oneoff.go`, `subscription/predicate.go`, `subscription/validate.go`, and `subscription/hash.go` as the primary seam to compare.
- The question for the fresh agent is not whether projected joins are ordered or whether `:sender` hashing is fixed; it is whether any other already-accepted SQL shape still compiles into a runtime predicate model that disagrees between subscribe admission, one-off execution, and downstream identity/validation behavior.

Batch framing:

1. Scout the live parser/compile/runtime surfaces first: `query/sql/parser.go`, `query/sql/coerce.go`, `protocol/handle_subscribe.go`, `protocol/handle_oneoff.go`, `subscription/predicate.go`, `subscription/validate.go`, `subscription/hash.go`, and the relevant current tests.
2. Build the residual A2 list only for accepted-shape normalization / validation drift. Ignore already-closed fan-out delivery, multiplicity, join-index-validation, and projected-ordering seams unless they expose fresh evidence.
3. Prefer one accepted SQL shape whose parser acceptance already exists but where one of these still disagrees across seams:
   - normalized predicate structure
   - `ValidatePredicate(...)`
   - canonical query hash identity
   - subscribe vs one-off execution result/behavior
4. Land one commit per slice with focused parser/protocol/subscription tests, then finish with `rtk go test ./protocol/... ./subscription/... ./executor/...`.

Concrete deliverable for the fresh agent:

1. Write down the exact confirmed mismatch in one short bullet before coding.
2. Add failing focused tests first.
3. Fix only the minimal compile/runtime seam needed.
4. Re-run focused tests, then `rtk go test ./protocol/... ./subscription/... ./executor/...`, then `rtk go test ./...`.
5. Update `TECH-DEBT.md`, `docs/parity-phase0-ledger.md`, `docs/spacetimedb-parity-roadmap.md`, and this file in the same session.

Suggested starting reads for this batch:

- `query/sql/parser.go`
- `query/sql/coerce.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_oneoff.go`
- `subscription/predicate.go`
- `subscription/validate.go`
- `subscription/hash.go`
- `subscription/register_set.go`
- `subscription/eval.go`

Suggested starting test surfaces:

- `query/sql/parser_test.go`
- `query/sql/coerce_test.go`
- `protocol/handle_oneoff_test.go`
- `protocol/handle_subscribe_test.go`
- `subscription/validate_test.go`
- `subscription/hash_test.go`

Good candidate seams to scout before choosing the batch:

- accepted SQL shapes whose subscribe compile path and one-off compile path build different runtime predicates
- accepted shapes whose runtime predicate normalizes but then hashes or validates differently than the same shape admitted through another path
- parameter/literal coercion or alias/qualification follow-ons where parser acceptance already exists but runtime predicate structure still drifts

Useful scout heuristic:

- Start from an already-accepted SQL shape that has parser and public-path tests, then compare whether `compileSQLQueryString(...)`, `subscription.ValidatePredicate(...)`, `ComputeQueryHash(...)`, subscribe admission, and one-off execution all agree on the same runtime meaning.
- The ideal next mismatch is: parser acceptance already exists, but one of normalization / validation / hash identity / execution still diverges between accepted subscribe and one-off paths.

Stop conditions:

- stop at the first clean batch boundary that would require a new decision doc or would cross into A3 / hardening work
- do not reopen the just-closed projected-join ordering work, the rows-shape cluster, or A1 wire/message-family work in the same session
- if the scout shows the next meaningful gap is actually A3 or a new documented divergence, write that up here before handing off

Out of scope for this batch: OI-001 A3 recovery/store parity, OI-004/005/006 hardening, rows-shape cluster reopen, strict-auth wiring in `cmd/shunter-example`.

## Follow-on queue (pickable next, one per session)

- **OI-002 A2** — subscription-layer parity against `reference/SpacetimeDB/crates/core/src/subscription/`. Next candidate batch: predicate normalization / validation drift on already-accepted SQL shapes, or another bounded runtime/model residual after the closed fan-out delivery, multiplicity, join-index-validation, committed projected-ordering, and projected-join delta-ordering slices. Do not reopen those closed slices without fresh evidence.
- **OI-001 A3** — recovery / store parity against `reference/SpacetimeDB/crates/core/src/db/`. Batch scope: snapshot/replay invariants beyond what `P0-RECOVERY-*` already covered.
- **Coordinated wrapper-chain + row-list close** (Phase 2 Slice 4 carried-forward deferral). Requires a new decision doc reopening the SPEC-005 §3.4 `BsatnRowList` deferral together with the reference `SubscribeRows` / `DatabaseUpdate` / `TableUpdate` / `CompressableQueryUpdate` / `QueryUpdate` wrapper chain. Scope is large — do not start without a named consumer or a bandwidth trigger (SPEC-005 §3.4 "fixed-schema row delivery bottleneck").
- **Strict-auth wiring in `cmd/shunter-example`**. Currently anonymous-only. Batch scope: JWT identity, token rotation, `IdentityToken` round-trip, upgrade-path handshake.

Pick one follow-on per session and treat it as the batch scope. Do not interleave.

## Startup notes

- Read `CLAUDE.md` first, then `RTK.md` for command rules, then `docs/EXECUTION-ORDER.md` for sequencing.
- Use `rtk git log` for slice provenance; this file is current-state only.
- Before changing a file, verify against live code — memory/ledger claims can drift.
