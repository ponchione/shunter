# Phase 0 parity ledger

This file is the concrete Phase 0 harness companion to `docs/spacetimedb-parity-roadmap.md`.

Purpose:
- freeze the parity target in named client-visible scenarios
- map each major gap to live authoritative tests/files
- make the next implementation slices measurable against explicit outcomes instead of general architectural similarity

Status values:
- `open` — parity gap is still open and the scenario is not yet well locked by tests
- `in_progress` — the scenario is named and at least partially locked by live tests, but the parity gap remains open
- `closed` — the target outcome is both explicit and sufficiently covered for the current phase
- `deferred` — intentionally not being closed now

## Protocol conformance bucket

These are the strongest existing protocol-boundary tests that now serve as the Phase 0 wire-level conformance bucket.

| Bucket | Current status | Authoritative tests | Reference outcome being matched | Next slice note |
|---|---|---|---|---|
| `P0-PROTOCOL-001` subprotocol negotiation + upgrade admission | `closed` | `protocol/upgrade_test.go`, `protocol/parity_subprotocol_test.go` | WebSocket upgrade succeeds only on the supported protocol and auth path; strict mode rejects missing/invalid auth; anonymous mode mints identity/token on upgrade. | Phase 1 closed: reference subprotocol `v1.bsatn.spacetimedb` accepted and preferred; `v1.bsatn.shunter` retained as an intentional deferral until legacy-client cutover — see `docs/spacetimedb-parity-roadmap.md` A1. |
| `P0-PROTOCOL-002` compression envelope + tag behavior | `closed` | `protocol/compression_test.go`, `protocol/sender_test.go`, `protocol/parity_compression_test.go` | Frame wrapping, compression-tag decoding, gzip round-trips, malformed/unknown compression handling, and sender framing stay explicit at the protocol boundary. | Phase 1 closed: tag numbering parity-aligned (None=0x00, Brotli=0x01 reserved, Gzip=0x02). Brotli is a recognized-but-deferred tag returning `ErrBrotliUnsupported` and closing with 1002 reason `brotli unsupported`. Brotli implementation is a Phase 2+ decision. |
| `P0-PROTOCOL-003` handshake / lifecycle / close behavior | `closed` | `protocol/lifecycle_test.go`, `protocol/close_test.go`, `protocol/reconnect_test.go`, `protocol/backpressure_in_test.go`, `protocol/backpressure_out_test.go`, `protocol/parity_close_codes_test.go` | Connection admission, rejection, disconnect cleanup, close-on-policy/protocol error, reconnect allowance, and backpressure-triggered shutdown are all explicit at the client-visible boundary. | Phase 1 closed: `TestPhase1ParityCloseCodeConstants` pins all four close codes (`CloseNormal`=1000, `CloseProtocol`=1002, `ClosePolicy`=1008, `CloseInternal`=1011) against `websocket.Status*` constants. `TestPhase1ParityHandshakeRejectionStatuses` pins HTTP rejection status codes for five distinct rejection classes (strict-no-token→401, invalid-token→401, zero-connection-id→400, bad-compression→400, missing-subprotocol→400). Step 3.2 audit (10 call sites): conn.go:264 (CloseNormal — server shutdown; intentional 1000 vs reference 1001, see SPEC-AUDIT §3.8); dispatch.go:44 (CloseProtocol — all protocol-error paths route through closeProtocolError helper: unknown tag, malformed message, text frame, brotli, unsupported message type, decode error); dispatch.go:151 (ClosePolicy — inflight semaphore overflow / too many requests); keepalive.go:77 (ClosePolicy — idle timeout); lifecycle.go:116 (StatusPolicyViolation — OnConnect rejected); lifecycle.go:134 (StatusInternalError — InitialConnection encode failure); lifecycle.go:139 (StatusInternalError — InitialConnection write failure); sender.go:108 (StatusPolicyViolation — outbound send buffer full); upgrade.go:211 (StatusNormalClosure — superviseLifecycle launch on successful upgrade); upgrade.go:217 (StatusNormalClosure — graceful close fallback when supervise path not taken). No drift found: each site uses the correct code for its condition. |
| `P0-PROTOCOL-004` message-family / dispatch boundaries | `closed (divergences explicit)` | `protocol/dispatch_test.go`, `protocol/handle_subscribe_test.go`, `protocol/handle_callreducer_test.go`, `protocol/handle_unsubscribe_test.go`, `protocol/send_responses_test.go`, `protocol/parity_message_family_test.go` | Binary/text framing rules, unknown-tag handling, dispatch to subscribe/unsubscribe/call-reducer paths, and response-message routing remain explicit and testable. | Phase 1.5 flipped the `TransactionUpdate` / `ReducerCallResult` pins to the reference heavy / light / `UpdateStatus` shape (`TestPhase15TransactionUpdateHeavyShape`, `TestPhase15TransactionUpdateLightShape`, `TestPhase15UpdateStatusVariants`, `TestPhase15ReducerCallInfoShape`, `TestPhase15TagReducerCallResultReserved`). `CallReducer.flags` closed Phase 1.5 sub-slice: `TestPhase15CallReducerFlagsField` pins the positive-shape field list `{RequestID, ReducerName, Args, Flags}` and the fan-out worker suppresses the caller's heavy envelope on `StatusCommitted` + `NoSuccessNotify` (reference `sender = None` semantics). Phase 2 Slice 2 opener: `TestPhase2SubscribeCarriesQueryID` + `TestPhase2UnsubscribeCarriesQueryID` pin the reference `query_id: QueryId` field on `SubscribeMsg` / `UnsubscribeMsg`. Phase 2 Slice 2 response-side follow-through landed: `TestPhase2SubscribeAppliedCarriesQueryID`, `TestPhase2UnsubscribeAppliedCarriesQueryID`, `TestPhase2SubscribeAppliedCarriesHostExecutionDuration`, and `TestPhase2SubscriptionErrorOptionalShape` now pin the response envelopes. Wire round-trips are covered in `protocol/server_messages_test.go`; direct response/helper behavior in `protocol/send_responses_test.go`; subscribe/unsubscribe error surfaces in `protocol/handle_subscribe_test.go` / `protocol/handle_unsubscribe_test.go`; fanout translation in `protocol/fanout_adapter_test.go`; and executor-to-protocol reply-path preservation in `executor/protocol_inbox_adapter_test.go` / `executor/subscription_dispatch_test.go`. Remaining explicit deferral in this family is broader SQL/query-surface breadth. |

## Canonical end-to-end delivery parity scenarios

| Scenario | Current status | Authoritative files/tests | Reference outcome being matched | Next slice note |
|---|---|---|---|---|
| `P0-DELIVERY-001` canonical reducer delivery flow (`connect -> subscribe -> call reducer -> caller heavy -> non-caller light -> durability-gated delivery`) | `closed` | `protocol/handle_callreducer_test.go`, `protocol/send_txupdate_test.go`, `protocol/fanout_adapter_test.go`, `subscription/fanout_worker_test.go`, `subscription/phase0_parity_test.go`, `executor/caller_metadata_test.go` | Caller observes the heavy `TransactionUpdate` with `StatusCommitted` / `StatusFailed` / `StatusOutOfEnergy`; non-callers with row-touches observe `TransactionUpdateLight` carrying the caller's `request_id`; the current public-protocol path waits on `TxDurable` before heavy/light delivery, and the caller's row delta is embedded in `StatusCommitted.Update`. Caller metadata (`CallerIdentity`, `ReducerCall.{ReducerName,ReducerID,Args}`, `Timestamp`, `TotalHostExecutionDuration`) is populated from the executor seam. | Phase 1.5 outcome-model decision landed (`docs/parity-phase1.5-outcome-model.md`); `CallReducer.flags` / `NoSuccessNotify` sub-slice is closed; caller-metadata wiring sub-slice closed. Remaining Phase 1.5 deferral: `EnergyQuantaUsed` is a permanent zero — no energy model. |
| `P0-DELIVERY-002` no-active-subscription / empty-fanout caller outcome | `closed` | `subscription/fanout_worker_test.go::TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout`, `subscription/eval.go` | A reducer-originated commit with no active subscriptions or an empty changeset still delivers the caller's heavy `TransactionUpdate` envelope; `EvalAndBroadcast` only short-circuits when there is neither caller metadata nor row-touches. | Closed Phase 1.5 — see the decision doc for the explicit guard. |

## Subscription / delivery parity scenarios

| Scenario | Current status | Authoritative files/tests | Reference outcome being matched | Next slice note |
|---|---|---|---|---|
| `P0-SUBSCRIPTION-001` per-connection outbound lag / slow-client policy | `closed (divergences explicit)` | `protocol/options.go`, `protocol/sender.go`, `protocol/parity_lag_policy_test.go::TestPhase2Slice3DefaultOutgoingBufferMatchesReference`, `protocol/backpressure_out_test.go`, `subscription/fanout_worker.go` | Per-client outbound queue is bounded at the reference `CLIENT_CHANNEL_CAPACITY = 16 * 1024` slots (`reference/SpacetimeDB/crates/core/src/client/client_connection.rs:657`). Overflow disconnects the client; the connection's subscriptions are reaped on teardown. Caller-heavy invariant (`P0-DELIVERY-002`) is preserved for remaining connected clients. | Phase 2 Slice 3 closed 2026-04-20 — see `docs/parity-phase2-slice3-lag-policy.md`. Mechanism differences retained as intentional: Shunter sends `StatusPolicyViolation (1008) "send buffer full"` on overflow; reference aborts the per-client tokio task (`client_connection.rs:394-416`). Externally visible outcome matches (client disconnected, subscriptions reclaimed). |

## Scheduler / recovery parity scenarios

| Scenario | Current status | Authoritative files/tests | Reference outcome being matched | Next slice note |
|---|---|---|---|---|
| `P0-SCHED-001` scheduled reducer startup replay ordering | `closed (divergences explicit)` | `executor/scheduler_replay_test.go`, `executor/scheduler_worker_test.go`, `executor/scheduler_firing_test.go`, `executor/scheduler_replay_test.go::TestParityP0Sched001ReplayPreservesScanOrderWithoutSorting`, `executor/scheduler_firing_test.go::TestParityP0Sched001PanicRetainsScheduledRow` | On restart, past-due scheduled reducers are re-enqueued, future ones arm the next wakeup, and new schedule allocation resumes from the max recovered `schedule_id`. Firing semantics: one-shot success deletes the row atomically with the reducer's tx; interval success advances `next_run_at_ns` to `intended + repeat` (fixed-rate); cancel-race at firing still commits (at-least-once). Intentional divergences pinned: (a) replay preserves committed scan order for past-due rows rather than sorting by `next_run_at_ns`; the committed-state `TableScan` surface is explicitly unordered, and reference DelayQueue bucket order is also non-strict; (b) `sys_scheduled` rows are retained on reducer panic (reference `scheduler.rs:445-455` deletes one-shot rows even on panic). | Phase 3 Slice 1 closed 2026-04-20 — see `docs/parity-p0-sched-001-startup-firing.md`. Remaining deferrals (all with reference citations): `fn_start`-clamped schedule "now" (`scheduler.rs:211-215`), one-shot panic deletion (`scheduler.rs:445-455`), past-due intended-time ordering, `Workload::Internal` commitlog labelling (tracked under `OI-003` / `P0-RECOVERY-001`), startup ordering relative to lifecycle hooks (tracked under `OI-008`), and the drain-in-flight-rx step (not applicable — Shunter has no equivalent channel). |
| `P0-RECOVERY-001` replay horizon and validated-prefix behavior | `closed (divergences explicit)` | `commitlog/replay_test.go`, `commitlog/recovery_test.go`, `commitlog/segment_scan_test.go`, `commitlog/parity_replay_horizon_test.go::TestParityP0Recovery001SegmentSkipDoesNotOpenExhaustedSegment` | Replay continues across valid segments, skips records at or below the resume horizon, stops at the validated prefix when the tail is damaged, fails closed when the first commit of the last segment is corrupt, and attaches tx/segment context to replay/apply errors. Shunter's short-circuit at segment granularity (skipping a whole segment when `LastTx <= fromTxID`, `replay.go:21-23`) is an intentional divergence from the reference's per-commit `CommitInfo::adjust_initial_offset` (`src/commitlog.rs:834-845`) — same externally visible outcome. | Phase 4 Slice 2 closed 2026-04-20 — see `docs/parity-p0-recovery-001-replay-horizon.md`. Remaining deferrals (typed `Traversal`/`Open` error enums, offset index file, format-level log / changeset parity) tracked under `OI-003` as broader Phase 4 scope decisions. |
| `P0-RECOVERY-002` `TxID` / `nextID` / sequence invariants across snapshot + replay | `closed` | `commitlog/recovery_test.go`, `store/recovery.go`, `store/snapshot.go`, `commitlog/replay.go`, `commitlog/recovery.go` | Snapshot + replay recovery now preserves the recovered transaction horizon, restores persisted `nextID`, and keeps auto-increment sequence state aligned with replayed auto-assigned rows without letting explicit non-zero inserts incorrectly jump the recovered sequence. | Closed by `TestOpenAndRecoverDetailedSnapshotReplayIgnoresExplicitAutoincrementRowsWhenRestoringSequence` plus the existing snapshot+replay recovery pin. Remaining recovery follow-up is broader replay-tolerance / reference-parity nuance under `P0-RECOVERY-001`, not this allocation-state invariant. |

## Recommended next implementation slice after Phase 1.5 envelope work

Phases 1 and 1.5 (envelope split + `CallReducer.flags`) are closed.

Remaining Phase 1.5 follow-ups:
- `EnergyQuantaUsed` stays zero; Shunter has no energy model. Marked permanent deferral unless an energy/quota subsystem is introduced.

With caller-metadata wiring closed, Phase 1.5 is fully drained except the permanent energy deferral. Phase 2 Slice 2 `SubscribeMulti` / `SubscribeSingle` variant split is closed (see `P0-PROTOCOL-004`), the four applied subscription envelopes now carry reference-style `TotalHostExecutionDurationMicros`, `SubscriptionError` now carries explicit optional request/query/table fields on the wire, and Phase 2 Slice 1c `OneOffQuery.message_id` wire parity is closed via `TestPhase2Slice1COneOffQueryMessageIDBytes`.

The SQL/query widening work also has substantial landed follow-through already: same-table qualified WHERE columns, case-insensitive identifier resolution, reference-style double-quoted identifiers, query-builder-style parenthesized WHERE predicates, and alias-qualified `OR` predicates with mixed qualified/unqualified column references across the same narrow single-table / join-backed surfaces, single-table alias / qualified-star forms, ordered comparisons, non-equality comparisons, same-table `OR` predicates, and several narrow join-backed slices now work through parser, subscribe admission, and one-off query handling. `TD-142` Slice 14 (2026-04-20) closes the projection gap that was flagged during Slice 11/12 design: `subscription.Join` carries `ProjectRight bool`, canonical hashing distinguishes `SELECT lhs.*` from `SELECT rhs.*`, and `evalQuery` / `initialQuery` / `evaluateOneOffJoin` slice the IVM's LHS++RHS concat fragments onto the SELECT side so subscribers see rows shaped like one concrete table, matching reference `SubscriptionPlan::subscribed_table_id` at `reference/SpacetimeDB/crates/subscription/src/lib.rs:367`. Pinned by `subscription/hash_test.go::TestQueryHashJoinProjectionDiffers`, `subscription/manager_test.go::TestRegisterJoinBootstrapProjectsRight`, `subscription/eval_test.go::TestEvalJoinSubscriptionProjectsRight`, `query/sql/parser_test.go::TestParseAliasedSelfEquiJoinProjectsRight`, `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_AliasedSelfEquiJoinProjectsRight`, and `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_AliasedSelfEquiJoinProjectsRight`.

What remains should be framed as live parity backlog, not as historical TD cleanup:
- broader SQL/query-surface parity beyond the currently pinned narrow slices
- format-level commitlog parity (offset index, record / log shape compatibility), tracked under `OI-003`

Phase 2 Slice 3 (lag / slow-client policy) closed 2026-04-20 via
`docs/parity-phase2-slice3-lag-policy.md`. The decision aligned
`DefaultOutgoingBufferMessages` with reference `CLIENT_CHANNEL_CAPACITY =
16 * 1024` and pinned the remaining close-mechanism difference (1008
close frame vs tokio task abort) as intentional, pinned by
`protocol/parity_lag_policy_test.go::TestPhase2Slice3DefaultOutgoingBufferMatchesReference`.
See `P0-SUBSCRIPTION-001`.

Phase 4 Slice 2 (`P0-RECOVERY-001` replay horizon / validated-prefix
behavior) closed 2026-04-20 via
`docs/parity-p0-recovery-001-replay-horizon.md`. Narrow-and-pin
outcome: all four ledger sub-behaviors (continue across valid
segments, skip below resume horizon, stop at validated prefix on tail
damage, attach tx/segment context to errors) are parity-close under
observation; existing pins re-asserted;
`commitlog/parity_replay_horizon_test.go::TestParityP0Recovery001SegmentSkipDoesNotOpenExhaustedSegment`
locks the internal-mechanism difference (segment-level short-circuit
vs reference per-commit `adjust_initial_offset`) as an intentional
optimization with matching externally visible outcome. Remaining
commitlog parity work (typed error enums, offset index, format-level
parity) is tracked under `OI-003` as broader Phase 4 scope.

Phase 3 Slice 1 (`P0-SCHED-001` scheduled reducer startup / firing
ordering) closed 2026-04-20 via
`docs/parity-p0-sched-001-startup-firing.md`. Narrow-and-pin outcome:
existing startup-replay / firing pins kept as parity-close; replay now
uses a deterministic parity pin
`executor/scheduler_replay_test.go::TestParityP0Sched001ReplayPreservesScanOrderWithoutSorting`
that locks the no-sort divergence at the replay helper seam without
assuming Go map iteration order, and
`executor/scheduler_firing_test.go::TestParityP0Sched001PanicRetainsScheduledRow`
locks the panic-retains-row divergence with explicit reference
citations. Remaining sub-scenarios (`fn_start`-clamped schedule
"now", one-shot panic deletion, intended-time past-due ordering,
`Workload::Internal` commitlog labelling, lifecycle-hook startup
ordering, drain-rx step) are deferred with reference anchors in the
decision doc.
