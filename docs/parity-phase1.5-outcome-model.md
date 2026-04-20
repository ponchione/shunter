# Phase 1.5 — `TransactionUpdate` / `ReducerCallResult` outcome model decision

This document records the Phase 1.5 outcome-model decision called out in
`docs/spacetimedb-parity-roadmap.md` §4 Slice 3 and the Phase 0 ledger row
`P0-DELIVERY-001`. It is the written-down companion to the parity tests
that lock the chosen shape.

## Reference shape (target)

`reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`:

- No standalone `ReducerCallResult` envelope. Caller observes the reducer
  outcome via `TransactionUpdate` (heavy) carrying:
  - `status: UpdateStatus<F>` — tagged union: `Committed(DatabaseUpdate)` |
    `Failed(Box<str>)` | `OutOfEnergy`
  - `timestamp: Timestamp`
  - `caller_identity: Identity`
  - `caller_connection_id: ConnectionId`
  - `reducer_call: ReducerCallInfo<F>` — `reducer_name`, `reducer_id`,
    `args`, `request_id`
  - `energy_quanta_used: EnergyQuanta`
  - `total_host_execution_duration: TimeDuration`
- Non-callers whose subscribed rows are touched receive
  `TransactionUpdateLight<F>` — `request_id` + delta-only `update`.
- Caller dispatch rule: caller always receives heavy `TransactionUpdate`
  on success / `Failed` / `OutOfEnergy`. Non-callers with row-touches
  receive light. Non-callers with no row-touches receive nothing.

## Shunter shape today (origin)

`protocol/server_messages.go`:

- `TransactionUpdate{TxID, Updates}` — flat envelope used for all
  recipients. No `UpdateStatus`. No caller metadata. No `ReducerCallInfo`.
- `ReducerCallResult{RequestID, Status uint8, TxID, Error, Energy uint64,
  TransactionUpdate []SubscriptionUpdate}` — separate envelope invented
  for caller-side delivery. `Status` is a flat enum (`0=committed`,
  `1=failed_user`, `2=failed_panic`, `3=not_found`). Energy hardcoded 0.
- Dispatch: `subscription/fanout_worker.go` strips caller's updates from
  fanout, embeds them in `ReducerCallResult`; non-callers get the flat
  `TransactionUpdate`.

## Decision: Option 2 — adopt structural shape now, defer numeric metadata

Adopted now (closes Phase 1.5):

- New wire envelopes:
  - `TransactionUpdate{Status: UpdateStatus, CallerIdentity,
    CallerConnectionID, ReducerCall: ReducerCallInfo, Timestamp,
    EnergyQuantaUsed, TotalHostExecutionDuration}` — heavy, caller-bound.
  - `TransactionUpdateLight{RequestID, Update}` — non-caller, delta-only.
- New tagged union `UpdateStatus` with three arms: `Committed{Update}`,
  `Failed{Error string}`, `OutOfEnergy{}`.
- `ReducerCallInfo{ReducerName, ReducerID, Args, RequestID}`.
- Dispatch rule:
  - Caller always receives `TransactionUpdate` (heavy) on success /
    failure / OOE. Caller's row-touches embedded in
    `UpdateStatus::Committed.Update`.
  - Non-caller with row-touches receives `TransactionUpdateLight`.
  - Non-caller with no row-touches receives nothing.
- `ReducerCallResult` envelope is removed from the protocol surface.
  `TagReducerCallResult` byte stays reserved (do not reuse the tag value)
  to avoid silent re-allocation if a future contributor reintroduces a
  separate envelope.

Deferred explicitly (numeric / observability fields, not envelope shape):

- `Timestamp` — server-side reducer start time. **Landed** in the
  Phase 1.5 caller-metadata sub-slice. Source is `time.Now().UnixNano()`
  captured at reducer dispatch. Pinned by
  `executor/caller_metadata_test.go::TestCallerOutcomeCarriesTimestampAndDuration`.
- `EnergyQuantaUsed` — energy accounting. **Stubbed as zero** in Phase
  1.5. Shunter has no energy model; this is a permanent stub unless an
  energy/quota subsystem is introduced. Marked as a long-term deferral
  keyed on a future Phase 3 / Phase 5 decision.
- `TotalHostExecutionDuration` — measured reducer wall time. **Landed**
  in the Phase 1.5 caller-metadata sub-slice. Measured from dispatch to
  `postCommit` with `time.Since(start).Nanoseconds()`. Pinned by
  `executor/caller_metadata_test.go::TestCallerOutcomeCarriesTimestampAndDuration`.
- `CallerIdentity`, `ReducerCall.{ReducerName, ReducerID, Args}` —
  **Landed** in the Phase 1.5 caller-metadata sub-slice. Identity and
  `ReducerName` / `Args` thread from `CallReducerCmd.Request`;
  `ReducerID` is assigned monotonically at `ReducerRegistry.Register`
  and read back via `Lookup`. Pinned by
  `executor/caller_metadata_test.go`.
- `OutOfEnergy` arm — present in the tagged union for shape parity but
  never emitted by the executor in Phase 1.5. No energy model exists.
  Phase 3 runtime parity decides whether the arm is ever produced or
  stays as a wire-only placeholder.
- `Failed` arm error-string format — Shunter currently distinguishes
  `failed_user` vs `failed_panic` vs `not_found`. Reference collapses all
  reducer-side failures into a single `Failed(Box<str>)` arm and routes
  not-found / executor-unavailable through other layers. Phase 1.5 maps
  Shunter's three flat statuses onto `Failed` with the existing error
  text; the lost distinction is now tracked as an explicit Phase 3
  reducer-outcome follow-up in `docs/spacetimedb-parity-roadmap.md`.
- Reducer-call rejection paths that today never reach the commit seam
  (`handle_callreducer.go`: lifecycle-reducer-name rejection, executor
  unavailable) — Phase 1.5 emits a synthetic heavy `TransactionUpdate`
  carrying `UpdateStatus::Failed{Error}` so the caller observes outcome
  through the same envelope as the success path. `TxID` for these
  synthetic updates is `0` because no transaction was ever opened; this
  is an explicit divergence-with-reason and is locked by a parity test.
- Caller suppression rules for empty-changeset / no-active-subscription
  edge cases — addressed by the `P0-DELIVERY-002` parity slice in the
  same Phase 1.5 sub-session. Decision summary lives there once written.

## Why Option 2 (and not 1 or 3)

- Option 1 (full adopt with all metadata fields wired immediately)
  overruns the slice. Timestamp + duration + reducer-id allocation +
  energy plumbing each touch executor + schema + reducer registry. Out
  of scope for one Phase 1.5 sub-slice.
- Option 3 (pin current Shunter shape as deliberate divergence) makes
  Phase 1.5 close nothing externally visible. The roadmap explicitly
  names Phase 1.5 as the *first end-to-end delivery parity slice*; an
  empty slice contradicts the phase definition.
- Option 2 closes the envelope shape (the part clients actually
  switch on at the protocol boundary) and converts the remaining
  divergences from "shape" to "field values" — the easier-to-audit kind.

## What this decision blocks / unblocks

Unblocks:
- `P0-DELIVERY-001` row in `docs/parity-phase0-ledger.md` can move from
  `in_progress` to `closed` once the parity test pins the new shape and
  the dispatch path matches.
- Phase 1.5 caller/non-caller routing decision (Decision 2 in the
  handoff) — the new envelope split makes routing explicit.
- Phase 1.5 confirmed-read decision (Decision 3) — confirmed-read gating
  in `subscription/fanout_worker.go` continues to operate on
  `TxDurable` regardless of envelope split; revisit only if the new
  caller-bound heavy envelope changes the gating contract.

Does not unblock:
- Phase 2 query-surface parity (SubscribeMulti / QueryId / SQL).
- Phase 3 runtime parity (timestamp, duration, energy, failure-arm
  collapse).
- Phase 4 durability parity (TxID origin, replay, snapshot invariants).

## Authoritative artifacts

- This document — written record of the decision and deferrals.
- `protocol/parity_message_family_test.go` — pin tests for the new
  envelope shape. The two existing pins (`TransactionUpdateNoHeavyLight`,
  `ReducerCallResultFlatStatus`) flip when this decision lands.
- `docs/parity-phase0-ledger.md` — `P0-DELIVERY-001` row updated to
  point at this decision.
- `docs/spacetimedb-parity-roadmap.md` — roadmap-level summary of the
  remaining protocol-shape and stub-field deferrals after the Phase 1.5
  decision.
