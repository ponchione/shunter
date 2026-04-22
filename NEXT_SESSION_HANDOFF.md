# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

For provenance of closed slices, use `git log` — this file tracks only current state and forward motion.

## Current state

- All OIs referenced in the 2026-04-22 audit chain (OI-008 through OI-012) are closed.
- Follow-on queue item for subscription `IndexRange` migration is closed.
- Follow-on queue item for subscription fan-out wiring in `cmd/shunter-example` is closed.
- Follow-on queue item for exposing executor inbox for scheduler wiring is closed — `Executor.SchedulerFor()` returns a wired `*Scheduler`, and `cmd/shunter-example` passes it to `Startup` and owns its Run goroutine.
- OI-001 A1 wire-close slice for `SubscriptionError.total_host_execution_duration_micros` is closed — the field is the reference-position first wire field (v1.rs:350), pinned by `protocol/parity_subscription_error_test.go` against the reference byte shape. Emit sites populate 0; duration measurement is deferred (see "Current active constraint" below).
- Closed-slice provenance, detailed verification history, and implementation narratives live in `rtk git log`.
- Before starting a new slice, verify any remembered closure claim against live code; this file is intentionally current-state only.

## Current active constraint from the last closed slice

- The example remains on anonymous auth; strict auth wiring is still out of scope for this queue.
- `Scheduler.ReplayFromCommitted` still uses `context.Background()`; a recovered schedule count exceeding the executor inbox capacity would block replay. Inherited backpressure, not introduced here.
- `SubscriptionError.TotalHostExecutionDurationMicros` is on the wire but always 0 — no receipt-timestamp seam is plumbed through the admission path (`protocol/handle_subscribe_*.go`, `protocol/handle_unsubscribe_*.go`) or the evaluation path (`subscription/eval.go`). Wire shape matches the reference; measured-value parity is a separate future slice.

## Next session: OI-001 A1 protocol wire-close — next concrete envelope/tag divergence

Prior A1 slice (`SubscriptionError` duration field) is closed. OI-001 still has remaining visible divergences. Pick one and repeat the scout → pick → close-or-pin pattern.

Known remaining divergences from the 2026-04-22 scout (not exhaustive — re-scout before picking):

- Applied-envelope field order — reference `SubscribeApplied { request_id, total_host_execution_duration_micros, query_id, rows }` vs Shunter `RequestID, QueryID, TableName, Rows, TotalHostExecutionDurationMicros` (duration is last, not position 2). Same divergence on `UnsubscribeSingleApplied`, `SubscribeMultiApplied`, `UnsubscribeMultiApplied`.
- `TransactionUpdate` field order — reference order is `status, timestamp, caller_identity, caller_connection_id, reducer_call, energy_quanta_used, total_host_execution_duration`; Shunter places timestamp after `ReducerCall`.
- `InitialConnection` vs reference `IdentityToken` — naming differs and field order differs (reference: `identity, token, connection_id`; Shunter wire: identity, connection_id, token).
- `TransactionUpdateLight.Update` — reference uses `DatabaseUpdate { tables: Vec<TableUpdate> }`; Shunter uses flat `[]SubscriptionUpdate`.
- `OneOffQueryResult` vs reference `OneOffQueryResponse` — reference uses `Option<error> + Vec<OneOffTable> + duration`; Shunter uses `Status byte + Rows + Error`.
- `CallReducer` wire field order — reference: `reducer, args, request_id, flags`; Shunter encodes `RequestID, ReducerName, Args, Flags`.
- `Unsubscribe.SendDropped` — extra byte in Shunter, not in reference.
- Applied-envelope rows shape — reference wraps `SubscribeRows { table_id, table_name, table_rows: TableUpdate }`; Shunter flattens to `TableName + Rows []byte` (no `table_id`, no `num_rows`).

Recommended slice framing for the next session:

1. **Scout first** — read `protocol/tags.go`, `protocol/wire_types.go`, `protocol/client_messages.go`, `protocol/server_messages.go`, `protocol/send_responses.go`, `protocol/send_txupdate.go`, and `protocol/fanout_adapter.go`, and diff each against `reference/SpacetimeDB/crates/client-api-messages/` (read-only). Produce a short list of still-visible divergences against what is already pinned (grep for `parity_` and `documented-divergence`).
2. **Pick exactly one** item off that list. Do not widen.
3. **Close or pin** — either (a) land a minimal code change that matches the reference envelope/tag, with a new `parity_*_test.go` pin against the reference byte shape; or (b) add a pinned compatibility test and a short decision doc under `docs/` stating the accepted divergence and why, matching the 2γ / subprotocol pattern.

Out of scope for that slice: A2 subscription parity, A3 recovery/store parity, OI-004/005/006 hardening. Do not reopen any of OI-008 … OI-012 or the `SubscriptionError` duration slice.

## Follow-on queue

- Populate `SubscriptionError.TotalHostExecutionDurationMicros` with a real measured duration. Needs a receipt-timestamp seam threaded through the admission and evaluation paths; current emit sites all populate 0.

Pick scope before starting. Do not open multiple OIs at once.

## Startup notes

- Read `CLAUDE.md` first, then `RTK.md` for command rules, then `docs/EXECUTION-ORDER.md` for sequencing.
- Use `git log` for slice provenance; this file is current-state only.
- Before changing a file, verify against live code — memory/ledger claims can drift.
