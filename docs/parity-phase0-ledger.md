# Phase 0 parity ledger

This file is the compact scenario ledger companion to `docs/spacetimedb-parity-roadmap.md`.

Purpose:
- freeze the parity target in named client-visible scenarios
- map each major gap to authoritative tests/files
- keep next-slice framing short and current

Status values:
- `open` — parity gap still open and not sufficiently locked
- `in_progress` — target is named and partly pinned, but work remains
- `closed` — current phase target is explicit and sufficiently covered
- `deferred` — intentionally not being closed now

## Protocol conformance bucket

| Bucket | Status | Authoritative tests/files | Current truth |
|---|---|---|---|
| `P0-PROTOCOL-001` subprotocol negotiation + upgrade admission | `closed` | `protocol/upgrade_test.go`, `protocol/parity_subprotocol_test.go` | Reference token is accepted/preferred; legacy `v1.bsatn.shunter` remains an intentional compatibility deferral. |
| `P0-PROTOCOL-002` compression envelope + tag behavior | `closed` | `protocol/compression_test.go`, `protocol/sender_test.go`, `protocol/parity_compression_test.go` | Tag numbering is parity-aligned; brotli is recognized-but-deferred. |
| `P0-PROTOCOL-003` handshake / lifecycle / close behavior | `closed` | `protocol/lifecycle_test.go`, `protocol/close_test.go`, `protocol/reconnect_test.go`, `protocol/backpressure_*_test.go`, `protocol/parity_close_codes_test.go` | Close-code and rejection behavior are pinned; remaining differences are explicit. |
| `P0-PROTOCOL-004` message-family / dispatch boundaries | `closed (divergences explicit)` | `protocol/dispatch_test.go`, `protocol/handle_*_test.go`, `protocol/send_responses_test.go`, `protocol/parity_message_family_test.go` | Heavy/light update shape, reducer outcome model, subscribe/query envelope follow-through, and `QueryID` wiring are landed; remaining protocol divergence is tracked in `OI-001`. |

## Canonical end-to-end delivery scenarios

| Scenario | Status | Authoritative tests/files | Current truth |
|---|---|---|---|
| `P0-DELIVERY-001` canonical reducer delivery flow | `closed` | `protocol/handle_callreducer_test.go`, `protocol/send_txupdate_test.go`, `protocol/fanout_adapter_test.go`, `subscription/fanout_worker_test.go`, `subscription/phase0_parity_test.go`, `executor/caller_metadata_test.go` | Caller heavy / non-caller light flow is pinned for the current public model. |
| `P0-DELIVERY-002` no-active-subscription / empty-fanout caller outcome | `closed` | `subscription/fanout_worker_test.go`, `subscription/eval.go` | Caller still gets the heavy outcome even with empty fanout. |

## Subscription / delivery parity scenarios

| Scenario | Status | Authoritative tests/files | Current truth |
|---|---|---|---|
| `P0-SUBSCRIPTION-001` per-connection outbound lag / slow-client policy | `closed (divergences explicit)` | `protocol/options.go`, `protocol/sender.go`, `protocol/parity_lag_policy_test.go`, `protocol/backpressure_out_test.go`, `subscription/fanout_worker.go` | Queue depth is aligned to reference capacity; overflow-disconnect outcome matches, while close mechanism remains an intentional divergence. |
| `P0-SUBSCRIPTION-002` fan-out durability gating + dropped-client cleanup | `closed` | `subscription/fanout_worker_test.go`, `subscription/eval_test.go`, `executor/pipeline_test.go` | Fast-read recipients can receive post-commit delivery before durability while confirmed-read recipients still wait on `TxDurable`; eval failures now mark the whole connection dropped and rely on executor-side `DisconnectClient` drain for cleanup. |
| `P0-SUBSCRIPTION-003` projected join/cross-join multiplicity | `closed` | `protocol/handle_oneoff_test.go`, `protocol/handle_subscribe_test.go`, `subscription/manager_test.go`, `subscription/eval_test.go`, `subscription/hash_test.go` | Existing accepted join/cross-join SQL forms now preserve bag/cartesian multiplicity across compile/hash identity, bootstrap, one-off execution, and post-commit delta evaluation, including aliased self-cross-join projection identity. |
| `P0-SUBSCRIPTION-004` projected join delta ordering | `closed` | `subscription/delta_dedup_test.go`, `subscription/eval_test.go`, `subscription/manager_test.go`, `protocol/handle_oneoff.go` | Accepted projected join shapes now preserve projected-side row semantics across one-off execution, bootstrap/final-delta enumeration, and post-commit delta emission: partner churn cancels at the projected-row bag level, and `ReconcileJoinDelta(...)` emits surviving rows in fragment encounter order instead of map iteration order. |
| `P0-SUBSCRIPTION-005` `:sender` parameter hash identity | `closed` | `protocol/handle_subscribe_test.go`, `executor/protocol_inbox_adapter_test.go`, `subscription/manager_test.go` | Accepted subscribe SQL using `:sender` now keeps caller-bound parameter provenance through compile/register hashing, so literal bytes queries no longer collide with the parameterized caller form and mixed batches only parameterize the marked predicates. |
| `P0-SUBSCRIPTION-006` neutral-`TRUE` predicate normalization | `closed` | `protocol/handle_subscribe_test.go`, `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, `subscription/manager_test.go` | Accepted SQL with neutral `TRUE` terms now normalizes before runtime lowering and canonical hashing: single-table `TRUE AND/OR ...` shapes collapse to the same runtime meaning and query-state identity as their simplified equivalents, and join-backed `TRUE AND rhs-filter` no longer lowers into malformed validation-failing filters. |
| `P0-SUBSCRIPTION-007` accepted single-table commutative child-order canonicalization | `closed` | `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, `subscription/manager_test.go` | Accepted single-table same-table `AND` / `OR` SQL whose user-visible row results were already equal now also shares canonical query hash / query-state identity when only child order changes; parser/runtime source order is preserved outside the bounded canonical identity seam. |
| `P0-SUBSCRIPTION-008` accepted single-table associative-grouping canonicalization | `closed` | `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, `subscription/manager_test.go` | Accepted single-table same-table grouped `AND` / `OR` SQL with 3+ leaves now also shares canonical query hash / query-state identity when only parenthesization changes; the canonicalization stays bounded to the same-table identity seam and leaves parser/runtime semantics untouched. |
| `P0-SUBSCRIPTION-009` accepted single-table duplicate-leaf idempotence canonicalization | `closed` | `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, `subscription/manager_test.go` | Accepted single-table same-table duplicate-leaf `AND` / `OR` SQL now also shares canonical query hash / query-state identity with the single-leaf equivalent (`a`, `a AND a`, `a OR a`) while one-off row semantics stay unchanged; the canonicalization remains bounded to the same-table identity seam. |
| `P0-SUBSCRIPTION-010` accepted single-table absorption-law canonicalization | `closed` | `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, `subscription/manager_test.go` | Accepted single-table same-table absorption-equivalent `AND` / `OR` SQL now also shares canonical query hash / query-state identity with the absorbed leaf (`a OR (a AND b)` → `a`, `a AND (a OR b)` → `a`) while one-off row semantics stay unchanged; original and canonicalized predicates are still both validated so the reduction cannot mask broader invalid shapes. |
| `P0-SUBSCRIPTION-011` overlength SQL admission guard | `closed` | `protocol/parity_one_off_query_response_test.go`, `protocol/parity_subscription_duration_test.go`, `query/sql/parser.go` | Overlength SQL now rejects before recursive parse/compile work on one-off, subscribe single, and subscribe multi paths via one shared 50,000-byte parser guard; the rejection surfaces the reference-aligned maximum-length message and short-circuits before snapshot or executor work. |
| `P0-SUBSCRIPTION-012` bare/grouped `FALSE` predicate follow-through | `closed` | `query/sql/parser_test.go`, `protocol/handle_subscribe_test.go`, `protocol/handle_oneoff_test.go`, `subscription/{predicate,validate,hash,manager,delta_single,placement}_test.go` | Reference-backed bare and grouped `FALSE` terms now parse, normalize, hash, validate, bootstrap, and execute coherently on the already-supported SQL surface: `FALSE` lowers to `subscription.NoRows`, `FALSE AND X` stays empty, and `FALSE OR X` shares the simplified comparison path. |
| `P0-SUBSCRIPTION-013` distinct-table join-filter child-order canonicalization | `closed` | `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, `subscription/manager_test.go` | Accepted distinct-table joins whose filter differs only by same-table commutative child order now share one canonical query hash and one shared query state while one-off visible rows stay unchanged; canonicalization is fenced to `Join.Filter` on distinct-table joins. |
| `P0-SUBSCRIPTION-014` self-join alias-sensitive join-filter child-order canonicalization | `closed` | `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, `subscription/manager_test.go` | Accepted aliased self-joins whose same-side filter differs only by commutative child order now share one canonical query hash and one shared query state while one-off visible rows stay unchanged; canonicalization stays bounded to alias-aware immediate-child ordering inside self-join `Join.Filter`, leaving self-join grouping / duplicate-leaf / absorption work for later bounded scouts. |
| `P0-SUBSCRIPTION-015` self-join alias-sensitive join-filter associative-grouping canonicalization | `closed` | `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, `subscription/manager_test.go` | Accepted aliased self-joins whose same-side filter differs only by associative grouping now share one canonical query hash and one shared query state while one-off visible rows stay unchanged; canonicalization now flattens and deterministically rebuilds same-kind self-join filter groups in an alias-aware way without pulling in duplicate-leaf or absorption reductions. |
| `P0-SUBSCRIPTION-016` self-join alias-sensitive join-filter duplicate-leaf idempotence canonicalization | `closed` | `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, `subscription/manager_test.go`, `subscription/hash.go` | Accepted aliased self-joins whose same-side filter differs only by exact duplicate leaves now share one canonical query hash and one shared query state while one-off visible rows stay unchanged; `subscription/hash.go` now dedupes byte-identical alias-aware self-join filter children after the self-join-local flatten/sort step without widening into absorption reductions. |

## Scheduler / recovery parity scenarios

| Scenario | Status | Authoritative tests/files | Current truth |
|---|---|---|---|
| `P0-SCHED-001` scheduled reducer startup replay ordering | `closed (divergences explicit)` | `executor/scheduler_*_test.go` | Startup replay and firing behavior are pinned; remaining scheduler differences are explicit deferrals. |
| `P0-RECOVERY-001` replay horizon and validated-prefix behavior | `closed (divergences explicit)` | `commitlog/replay_test.go`, `commitlog/recovery_test.go`, `commitlog/segment_scan_test.go`, `commitlog/parity_replay_horizon_test.go` | Continue/skip/stop/error-context behavior is parity-close; segment-level skip remains an intentional internal divergence. |
| `P0-RECOVERY-002` `TxID` / `nextID` / sequence invariants across snapshot + replay | `closed` | `commitlog/recovery_test.go`, `store/recovery.go`, `store/snapshot.go`, `commitlog/replay.go`, `commitlog/recovery.go` | Snapshot+replay invariant work is closed for the current phase. |

## Current next-slice framing

Phase 1 / 1.5 / 2 / 3 / Phase 4 Slice 2 (2α / 2β / 2γ) / the narrow `P0-RECOVERY-001` recovery slice are all closed for the current phase framing.

Phase 2 sub-slice record (for reference; all closed):

| Sub-slice | Status | Decision doc | Current truth |
|---|---|---|---|
| Slice 3 — per-client outbound lag / slow-client policy | `closed (divergences explicit)` | `docs/parity-phase2-slice3-lag-policy.md` | Queue depth aligned to reference `CLIENT_CHANNEL_CAPACITY = 16384`; overflow-disconnect outcome matches; 1008 close-frame mechanism is an intentional parity-explicit divergence. |
| Slice 4 — applied / light / committed rows shape | `closed (divergences recorded)` | `docs/parity-phase2-slice4-rows-shape.md` | Documented-divergence close covering the flat `[]SubscriptionUpdate` / `TableName+Rows` shape on `Subscribe{Single,Multi}Applied`, `Unsubscribe{Single,Multi}Applied`, `TransactionUpdateLight`, `StatusCommitted`. The wrapper chain (`SubscribeRows` / `DatabaseUpdate` / `TableUpdate` / `CompressableQueryUpdate` / `BsatnRowList`) collapses onto the SPEC-005 §3.4 row-list deferral, so a partial close produces no parity value. New pin file `protocol/parity_rows_shape_test.go` rolls up the canonical-contract layer. |

Phase 4 Slice 2 sub-slice record (for reference; all closed):

| Sub-slice | Status | Decision doc | Current truth |
|---|---|---|---|
| 2α — per-segment offset index file | `closed` | `docs/parity-phase4-slice2-offset-index.md` | Fully landed across Sessions 1-5, including replay seek integration, durability wiring, compaction cleanup, and crash-tail handling. |
| 2β — typed `Traversal` / `Open` error enums | `closed` | `docs/parity-phase4-slice2-errors.md` | Five category sentinels (`ErrTraversal`, `ErrOpen`, `ErrDurability`, `ErrSnapshot`, `ErrIndex`) + `wrapCategory` helper + `Is` methods on the nine typed structs landed across Sessions 1-2. |
| 2γ — record / log shape format compatibility | `closed (divergences recorded)` | `docs/parity-phase4-slice2-record-shape.md` | Sessions 1-2 landed: Session 1 locked the field-by-field divergence audit (documented-divergence close rather than byte-parity rewrite); Session 2 landed the 33-pin wire-shape contract suite at `commitlog/wire_shape_test.go`. Named out-of-scope deferrals carried forward in `TECH-DEBT.md` OI-007. |

## Reading rule

Use this file for current scenario status only.
If you need implementation detail, read the linked decision doc or the narrow slice doc instead of expanding this ledger into a historical changelog.

## Current broad backlog themes

What remains is better thought of as a small set of live themes than as a long historical slice list:
- protocol wire-close follow-through
- broader query/subscription parity beyond the narrow landed shapes (now after the closed fan-out delivery, multiplicity, one-off-vs-subscribe join-index-validation, committed bootstrap/final-delta projected-ordering, projected-join delta-ordering, `:sender` hash-identity, neutral-`TRUE` normalization, single-table commutative child-order canonicalization, single-table associative-grouping canonicalization, single-table duplicate-leaf idempotence canonicalization, single-table absorption-law canonicalization, overlength-SQL admission guard, bare/grouped `FALSE` predicate follow-through, distinct-table join-filter child-order canonicalization, self-join alias-sensitive join-filter child-order canonicalization, self-join alias-sensitive join-filter associative-grouping canonicalization, and self-join alias-sensitive join-filter duplicate-leaf idempotence canonicalization)
- the next bounded A2 scout is self-join alias-sensitive join-filter absorption-law reduction, because live `subscription/hash.go` now flattens, sorts, and dedupes exact duplicate alias-aware leaves in the self-join path but still does not remove bounded absorption-equivalent self-join filter shapes
- recovery/store parity follow-ons after 2γ (carried-forward deferrals in `TECH-DEBT.md` OI-007)
- hardening themes tracked in `TECH-DEBT.md`

For prioritization, read:
1. `docs/spacetimedb-parity-roadmap.md`
2. `TECH-DEBT.md`
3. `NEXT_SESSION_HANDOFF.md`