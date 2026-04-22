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

- `SubscriptionError.TotalHostExecutionDurationMicros`, `OneOffQueryResponse.TotalHostExecutionDuration`, and `Subscribe{Single,Multi}Applied.TotalHostExecutionDurationMicros` / `Unsubscribe{Single,Multi}Applied.TotalHostExecutionDurationMicros` are all on the wire but always 0 — no receipt-timestamp seam is plumbed through the admission path (`protocol/handle_subscribe_*.go`, `protocol/handle_unsubscribe_*.go`), the evaluation path (`subscription/eval.go`), or the one-off path (`protocol/handle_oneoff.go`). Wire shapes match reference; measured-value parity is the receipt-timestamp-seam batch (see below).
- `TransactionUpdate.Timestamp` and `TransactionUpdate.TotalHostExecutionDuration` are i64 with the same width as reference `Timestamp` / `TimeDuration`, but Shunter populates nanoseconds while reference SATS structs store microseconds (`reference/SpacetimeDB/crates/sats/src/timestamp.rs:11-13`, `.../time_duration.rs:17-19`). Bundled into the receipt-timestamp-seam batch.
- `EnergyQuantaUsed` on the wire is a 16-byte u128 LE that always emits zeros. Shunter has no energy model; widening to honest u128 values is not on the roadmap.
- `cmd/shunter-example` remains on anonymous auth; strict auth wiring is still out of scope for this queue.
- `Scheduler.ReplayFromCommitted` still uses `context.Background()`; a recovered schedule count exceeding the executor inbox capacity would block replay. Inherited backpressure, not introduced here.

## How to frame a session

Work in *batches*, not single slices. One session = one batch. Within a batch, land one commit per slice so each remains reviewable.

1. **Scout once, at the start of the session.** Read `protocol/tags.go`, `protocol/wire_types.go`, `protocol/client_messages.go`, `protocol/server_messages.go`, `protocol/send_responses.go`, `protocol/send_txupdate.go`, `protocol/fanout_adapter.go`, and diff each against `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs` (read-only). Grep for `parity_` and `documented-divergence` to filter out already-closed items. Produce the full residual list.
2. **Pick the batch's scope boundary** (one OI, or one named follow-on). Write out which items from the residual list fall inside it. Items outside stay for a future session — do not widen mid-batch.
3. **Close each item in turn**, one commit per slice, each with a new `parity_*_test.go` byte-shape pin or a short decision doc under `docs/` matching the Slice 4 / 2γ / subprotocol pattern. Run `rtk go test ./protocol/...` + dependent packages after each, not only at the end.
4. **Stop when** (a) the batch scope is exhausted, (b) the next item would need a new decision doc (stop and write the doc; do not just ship it), or (c) the next item crosses an OI/scope boundary. Do not stop mid-slice because "one slice is enough for today."
5. **Update this file + memory at the end**, then commit and hand off.

Do not open multiple OIs in one batch. Do not reopen closed slices. Do not silently widen into A2/A3 or OI-004/005/006 hardening.

## Next session: receipt-timestamp seam (OI-001 A1 measurement-parity batch)

OI-001 A1 is exhausted for *wire-shape* residuals. The remaining A1 work is measurement parity: four duration fields on the wire that always emit 0.

Fields to populate with real measured values in this batch:

- `SubscriptionError.TotalHostExecutionDurationMicros`
- `OneOffQueryResponse.TotalHostExecutionDuration`
- `SubscribeSingleApplied.TotalHostExecutionDurationMicros`
- `SubscribeMultiApplied.TotalHostExecutionDurationMicros`
- `UnsubscribeSingleApplied.TotalHostExecutionDurationMicros`
- `UnsubscribeMultiApplied.TotalHostExecutionDurationMicros`

Also in scope: `TransactionUpdate.Timestamp` and `TransactionUpdate.TotalHostExecutionDuration` unit conversion (ns → µs) to match reference SATS semantics. This is a *unit* change, not a shape change — the wire is still i64.

Seam work required (scout before coding):

1. **Admission path**: thread a receipt-timestamp (captured at request decode, before dispatch) through `protocol/handle_subscribe_single.go`, `protocol/handle_subscribe_multi.go`, `protocol/handle_unsubscribe_single.go`, `protocol/handle_unsubscribe_multi.go`, `protocol/handle_oneoff.go`. The emit site computes `time.Since(receipt)` in microseconds at Reply/response time.
2. **Evaluation path**: `subscription/eval.go` post-commit evaluation-origin `SubscriptionError` emission needs a receipt timestamp from the enclosing commit / fan-out context. `protocol/fanout_adapter.go::SendSubscriptionError` should accept the measured duration rather than defaulting to 0.
3. **TransactionUpdate unit flip**: change `CallerOutcome.Timestamp` and `CallerOutcome.TotalHostExecutionDuration` semantics from nanoseconds to microseconds at the subscription/executor boundary. Audit every populator (`executor/...`, subscription pipeline) and flip `time.Now().UnixNano()` / `elapsed.Nanoseconds()` to the microsecond equivalent. Update pin tests in `protocol/parity_transaction_update_test.go` to document the unit.
4. **Per-slice pins**: each of the six duration fields gets an updated parity test that asserts a *non-zero* duration on the wire after seam wiring, not just the field position. Existing byte-shape pins should continue to pass.

Close criteria for the batch:

- All six `*Applied` / error / one-off duration fields populate non-zero values when a real request takes observable time.
- `TransactionUpdate` timestamp + duration values round-trip cleanly in microseconds.
- `rtk go test ./protocol/... ./subscription/... ./executor/... ./cmd/shunter-example/...` passes.
- One commit per sub-slice; final commit updates this file.

If a real blocker surfaces (e.g., the evaluation path needs a new `CommitFanout` field that ripples through SPEC-004 contracts), stop the batch at a clean sub-slice boundary, write the blocker up at the bottom of this file as a new follow-on, and hand off.

Out of scope for this batch: OI-001 A2 subscription parity, OI-001 A3 recovery/store parity, OI-004/005/006 hardening, rows-shape cluster reopen.

## Follow-on queue (pickable next, one per session)

- **OI-001 A2** — subscription-layer parity against `reference/SpacetimeDB/crates/core/src/subscription/`. Needs its own scout pass; SPEC-004 E1-E5 is the relevant surface. Batch scope: predicate parity, evaluation-ordering parity, fan-out delivery parity.
- **OI-001 A3** — recovery / store parity against `reference/SpacetimeDB/crates/core/src/db/`. Batch scope: snapshot/replay invariants beyond what `P0-RECOVERY-*` already covered.
- **Coordinated wrapper-chain + row-list close** (Phase 2 Slice 4 carried-forward deferral). Requires a new decision doc reopening the SPEC-005 §3.4 `BsatnRowList` deferral together with the reference `SubscribeRows` / `DatabaseUpdate` / `TableUpdate` / `CompressableQueryUpdate` / `QueryUpdate` wrapper chain. Scope is large — do not start without a named consumer or a bandwidth trigger (SPEC-005 §3.4 "fixed-schema row delivery bottleneck").
- **Strict-auth wiring in `cmd/shunter-example`**. Currently anonymous-only. Batch scope: JWT identity, token rotation, `IdentityToken` round-trip, upgrade-path handshake.

Pick one follow-on per session and treat it as the batch scope. Do not interleave.

## Startup notes

- Read `CLAUDE.md` first, then `RTK.md` for command rules, then `docs/EXECUTION-ORDER.md` for sequencing.
- Use `rtk git log` for slice provenance; this file is current-state only.
- Before changing a file, verify against live code — memory/ledger claims can drift.
