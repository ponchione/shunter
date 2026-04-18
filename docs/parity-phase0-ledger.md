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
| `P0-PROTOCOL-003` handshake / lifecycle / close behavior | `closed` | `protocol/lifecycle_test.go`, `protocol/close_test.go`, `protocol/reconnect_test.go`, `protocol/backpressure_in_test.go`, `protocol/backpressure_out_test.go`, `protocol/parity_close_codes_test.go` | Connection admission, rejection, disconnect cleanup, close-on-policy/protocol error, reconnect allowance, and backpressure-triggered shutdown are all explicit at the client-visible boundary. | Phase 1 closed: `TestPhase1ParityCloseCodeConstants` pins all four close codes (`CloseNormal`=1000, `CloseProtocol`=1002, `ClosePolicy`=1008, `CloseInternal`=1011) against `websocket.Status*` constants. `TestPhase1ParityHandshakeRejectionStatuses` pins HTTP rejection status codes for five distinct rejection classes (strict-no-token→401, invalid-token→401, zero-connection-id→400, bad-compression→400, missing-subprotocol→400). Step 3.2 audit found no drift: all call sites use the correct code for their condition. One intentional divergence from reference: Shunter uses 1000 for server-initiated graceful shutdown where SpacetimeDB uses 1001 Away — documented in SPEC-AUDIT §3.8 as an explicit design decision. |
| `P0-PROTOCOL-004` message-family / dispatch boundaries | `in_progress` | `protocol/dispatch_test.go`, `protocol/handle_subscribe_test.go`, `protocol/handle_callreducer_test.go`, `protocol/handle_unsubscribe_test.go`, `protocol/send_responses_test.go` | Binary/text framing rules, unknown-tag handling, dispatch to subscribe/unsubscribe/call-reducer paths, and response-message routing remain explicit and testable. | Phase 1 should clean up any remaining message-family differences at the frame boundary before broader runtime parity work. |

## Canonical end-to-end delivery parity scenarios

| Scenario | Current status | Authoritative files/tests | Reference outcome being matched | Next slice note |
|---|---|---|---|---|
| `P0-DELIVERY-001` canonical reducer delivery flow (`connect -> subscribe -> call reducer -> caller result -> non-caller update -> confirmed-read gate`) | `in_progress` | `protocol/lifecycle_test.go`, `protocol/handle_subscribe_test.go`, `protocol/handle_callreducer_test.go`, `protocol/send_reducer_result_test.go`, `protocol/send_txupdate_test.go`, `subscription/fanout_worker_test.go`, `subscription/phase0_parity_test.go` | A caller should observe `ReducerCallResult`; non-callers should observe `TransactionUpdate`; confirmed-read clients must not receive delivery before durability; caller delivery must embed the caller's update rather than receiving a second standalone update. | Phase 1.5 should decide the final `ReducerCallResult` / `TransactionUpdate` public outcome model and then close the remaining protocol/runtime divergences in one slice. |
| `P0-DELIVERY-002` no-active-subscription / empty-fanout edge case | `open` | `subscription/eval.go`, `subscription/eval_test.go` (`TestEvalNoActiveSubsReturnsImmediately`) | The parity harness must keep visible that `subscription/eval.go` returns early when there are no active subscriptions or no relevant fanout, so later work does not silently paper over caller-result behavior in that edge case. | Keep this explicit when closing Phase 1.5 so caller-visible semantics are not accidentally hidden behind the current early-return seam. |

## Scheduler / recovery parity scenarios

| Scenario | Current status | Authoritative files/tests | Reference outcome being matched | Next slice note |
|---|---|---|---|---|
| `P0-SCHED-001` scheduled reducer startup replay ordering | `in_progress` | `executor/scheduler_replay_test.go`, `executor/scheduler_worker_test.go`, `executor/scheduler_firing_test.go` | On restart, past-due scheduled reducers are re-enqueued, future ones arm the next wakeup, and new schedule allocation resumes from the max recovered `schedule_id` instead of colliding with replayed rows. | Phase 3 / Slice 5 should compare this against the intended SpacetimeDB-visible startup/firing ordering and then close the remaining timing semantics deliberately. |
| `P0-RECOVERY-001` replay horizon and validated-prefix behavior | `in_progress` | `commitlog/replay_test.go`, `commitlog/recovery_test.go` | Replay should continue across valid segments, skip records at or below the resume horizon, stop at the validated prefix when the tail is damaged, and attach tx/segment context to replay/apply errors. | Phase 4 should decide how close replay tolerance vs fail-fast behavior must be to the reference implementation. |
| `P0-RECOVERY-002` `TxID` / `nextID` / sequence invariants across snapshot + replay | `open` | `store/recovery.go`, `store/snapshot.go`, `commitlog/replay.go`, `commitlog/recovery.go` | The parity target is one explicit recovery scenario proving that committed writes survive restart with the correct recovered transaction horizon and without reusing auto-allocated identifiers or sequence values. | This is the best first runtime/recovery implementation slice after the Phase 0 harness and early protocol/delivery work. |

## Recommended first implementation slice after Phase 0

Best next slice once this ledger and the current protocol/runtime tests are in place:
- `Phase 1 — wire-level protocol envelope parity`
- especially:
  - subprotocol decision
  - compression-envelope/tag parity
  - handshake / close-code alignment
  - message-family cleanup at the frame boundary

That keeps the next session focused on externally visible differences first, with `P0-DELIVERY-001` and `P0-RECOVERY-002` already available as the anchor scenarios for the follow-on phases.
