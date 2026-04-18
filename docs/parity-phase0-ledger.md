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
| `P0-PROTOCOL-001` subprotocol negotiation + upgrade admission | `closed` | `protocol/upgrade_test.go`, `protocol/parity_subprotocol_test.go` | WebSocket upgrade succeeds only on the supported protocol and auth path; strict mode rejects missing/invalid auth; anonymous mode mints identity/token on upgrade. | Phase 1 closed: reference subprotocol `v1.bsatn.spacetimedb` accepted and preferred; `v1.bsatn.shunter` retained as an intentional deferral until legacy-client cutover — see `SPEC-AUDIT.md` subprotocol bullet. |
| `P0-PROTOCOL-002` compression envelope + tag behavior | `closed` | `protocol/compression_test.go`, `protocol/sender_test.go`, `protocol/parity_compression_test.go` | Frame wrapping, compression-tag decoding, gzip round-trips, malformed/unknown compression handling, and sender framing stay explicit at the protocol boundary. | Phase 1 closed: tag numbering parity-aligned (None=0x00, Brotli=0x01 reserved, Gzip=0x02). Brotli is a recognized-but-deferred tag returning `ErrBrotliUnsupported` and closing with 1002 reason `brotli unsupported`. Brotli implementation is a Phase 2+ decision. |
| `P0-PROTOCOL-003` handshake / lifecycle / close behavior | `closed` | `protocol/lifecycle_test.go`, `protocol/close_test.go`, `protocol/reconnect_test.go`, `protocol/backpressure_in_test.go`, `protocol/backpressure_out_test.go`, `protocol/parity_close_codes_test.go` | Connection admission, rejection, disconnect cleanup, close-on-policy/protocol error, reconnect allowance, and backpressure-triggered shutdown are all explicit at the client-visible boundary. | Phase 1 closed: `TestPhase1ParityCloseCodeConstants` pins all four close codes (`CloseNormal`=1000, `CloseProtocol`=1002, `ClosePolicy`=1008, `CloseInternal`=1011) against `websocket.Status*` constants. `TestPhase1ParityHandshakeRejectionStatuses` pins HTTP rejection status codes for five distinct rejection classes (strict-no-token→401, invalid-token→401, zero-connection-id→400, bad-compression→400, missing-subprotocol→400). Step 3.2 audit (10 call sites): conn.go:264 (CloseNormal — server shutdown; intentional 1000 vs reference 1001, see SPEC-AUDIT §3.8); dispatch.go:44 (CloseProtocol — all protocol-error paths route through closeProtocolError helper: unknown tag, malformed message, text frame, brotli, unsupported message type, decode error); dispatch.go:151 (ClosePolicy — inflight semaphore overflow / too many requests); keepalive.go:77 (ClosePolicy — idle timeout); lifecycle.go:116 (StatusPolicyViolation — OnConnect rejected); lifecycle.go:134 (StatusInternalError — InitialConnection encode failure); lifecycle.go:139 (StatusInternalError — InitialConnection write failure); sender.go:108 (StatusPolicyViolation — outbound send buffer full); upgrade.go:211 (StatusNormalClosure — superviseLifecycle launch on successful upgrade); upgrade.go:217 (StatusNormalClosure — graceful close fallback when supervise path not taken). No drift found: each site uses the correct code for its condition. |
| `P0-PROTOCOL-004` message-family / dispatch boundaries | `closed (divergences explicit)` | `protocol/dispatch_test.go`, `protocol/handle_subscribe_test.go`, `protocol/handle_callreducer_test.go`, `protocol/handle_unsubscribe_test.go`, `protocol/send_responses_test.go`, `protocol/parity_message_family_test.go` | Binary/text framing rules, unknown-tag handling, dispatch to subscribe/unsubscribe/call-reducer paths, and response-message routing remain explicit and testable. | Phase 1.5 flipped the `TransactionUpdate` / `ReducerCallResult` pins to the reference heavy / light / `UpdateStatus` shape (`TestPhase15TransactionUpdateHeavyShape`, `TestPhase15TransactionUpdateLightShape`, `TestPhase15UpdateStatusVariants`, `TestPhase15ReducerCallInfoShape`, `TestPhase15TagReducerCallResultReserved`). `CallReducer.flags` closed Phase 1.5 sub-slice: `TestPhase15CallReducerFlagsField` pins the positive-shape field list `{RequestID, ReducerName, Args, Flags}` and the fan-out worker suppresses the caller's heavy envelope on `StatusCommitted` + `NoSuccessNotify` (reference `sender = None` semantics). Remaining deferrals: `SubscribeMulti` / `SubscribeSingle` / `QueryId` (Phase 2 Slice 2) and SQL `OneOffQuery` (Phase 2 Slice 1). |

## Canonical end-to-end delivery parity scenarios

| Scenario | Current status | Authoritative files/tests | Reference outcome being matched | Next slice note |
|---|---|---|---|---|
| `P0-DELIVERY-001` canonical reducer delivery flow (`connect -> subscribe -> call reducer -> caller heavy -> non-caller light -> durability-gated delivery`) | `closed` | `protocol/handle_callreducer_test.go`, `protocol/send_txupdate_test.go`, `protocol/fanout_adapter_test.go`, `subscription/fanout_worker_test.go`, `subscription/phase0_parity_test.go` | Caller observes the heavy `TransactionUpdate` with `StatusCommitted` / `StatusFailed` / `StatusOutOfEnergy`; non-callers with row-touches observe `TransactionUpdateLight` carrying the caller's `request_id`; the current public-protocol path waits on `TxDurable` before heavy/light delivery, and the caller's row delta is embedded in `StatusCommitted.Update`. | Phase 1.5 outcome-model decision landed (`docs/parity-phase1.5-outcome-model.md`). Next Phase 1.5 sub-slices: `CallReducer.flags` (notably `NoSuccessfulUpdate`); caller-identity / reducer-id / timestamp / duration / energy metadata wiring (currently stubbed zero). |
| `P0-DELIVERY-002` no-active-subscription / empty-fanout caller outcome | `closed` | `subscription/fanout_worker_test.go::TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout`, `subscription/eval.go` | A reducer-originated commit with no active subscriptions or an empty changeset still delivers the caller's heavy `TransactionUpdate` envelope; `EvalAndBroadcast` only short-circuits when there is neither caller metadata nor row-touches. | Closed Phase 1.5 — see the decision doc for the explicit guard. |

## Scheduler / recovery parity scenarios

| Scenario | Current status | Authoritative files/tests | Reference outcome being matched | Next slice note |
|---|---|---|---|---|
| `P0-SCHED-001` scheduled reducer startup replay ordering | `in_progress` | `executor/scheduler_replay_test.go`, `executor/scheduler_worker_test.go`, `executor/scheduler_firing_test.go` | On restart, past-due scheduled reducers are re-enqueued, future ones arm the next wakeup, and new schedule allocation resumes from the max recovered `schedule_id` instead of colliding with replayed rows. | Phase 3 / Slice 5 should compare this against the intended SpacetimeDB-visible startup/firing ordering and then close the remaining timing semantics deliberately. |
| `P0-RECOVERY-001` replay horizon and validated-prefix behavior | `in_progress` | `commitlog/replay_test.go`, `commitlog/recovery_test.go` | Replay should continue across valid segments, skip records at or below the resume horizon, stop at the validated prefix when the tail is damaged, and attach tx/segment context to replay/apply errors. | Phase 4 should decide how close replay tolerance vs fail-fast behavior must be to the reference implementation. |
| `P0-RECOVERY-002` `TxID` / `nextID` / sequence invariants across snapshot + replay | `open` | `store/recovery.go`, `store/snapshot.go`, `commitlog/replay.go`, `commitlog/recovery.go` | The parity target is one explicit recovery scenario proving that committed writes survive restart with the correct recovered transaction horizon and without reusing auto-allocated identifiers or sequence values. | This is the best first runtime/recovery implementation slice after the Phase 0 harness and early protocol/delivery work. |

## Recommended next implementation slice after Phase 1.5 envelope work

Phases 1 and 1.5 (envelope split + `CallReducer.flags`) are closed.

Remaining Phase 1.5 follow-ups (each independently landable):
- populate `TransactionUpdate.CallerIdentity` (currently zeroed — Phase 3 carries the identity source from the executor commit seam)
- populate `TransactionUpdate.ReducerCall.ReducerID` from the reducer registry
- populate `TransactionUpdate.ReducerCall.ReducerName` / `Args` (currently zeroed at the executor seam)
- emit server-side `Timestamp`, `EnergyQuantaUsed`, `TotalHostExecutionDuration` (currently all zero; see the decision doc)

After Phase 1.5 is fully drained: Phase 2 (`SubscribeMulti` / SQL OneOffQuery / `QueryId` / lag policy) and Phase 4 (`P0-RECOVERY-002`) are the next anchor scenarios.
