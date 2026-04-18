Continue Shunter in a fresh agent session.

Phase 1 wire-level protocol parity is closed.
Phase 1.5 first end-to-end delivery parity slice — the `TransactionUpdate` /
`ReducerCallResult` outcome-model split — is also closed. Do not re-open
any P0-PROTOCOL-00* slice and do not re-litigate the outcome-model decision.

Primary decision
- Treat `docs/spacetimedb-parity-roadmap.md` as the active development driver.
- Treat Phase 0, Phase 1, and the Phase 1.5 envelope split as materially landed.
- The next narrow slice is the remaining Phase 1.5 sub-slice: `CallReducer.flags`.
  The handoff below points at the specific deferral rows.

What landed last session (Phase 1.5 envelope split)
- Heavy `TransactionUpdate{Status, CallerIdentity, CallerConnectionID, ReducerCall,
  Timestamp, EnergyQuantaUsed, TotalHostExecutionDuration}` added to the wire surface.
- Delta-only `TransactionUpdateLight{RequestID, Update}` added for non-caller delivery.
- `UpdateStatus` tagged union (`StatusCommitted{Update}`, `StatusFailed{Error}`,
  `StatusOutOfEnergy{}`) replaces the former flat `uint8` status.
- `ReducerCallInfo{ReducerName, ReducerID, Args, RequestID}` embedded in heavy.
- `ReducerCallResult` removed from the wire surface; `TagReducerCallResult` stays
  reserved and the decoder rejects it.
- Dispatch rules (`subscription/fanout_worker.go`):
  - caller always receives the heavy envelope when `CallerConnID` is set —
    including when `Fanout[CallerConnID]` is empty and when no subscriptions
    are active (the `P0-DELIVERY-002` pin).
  - non-callers whose rows were touched receive the light envelope carrying
    the caller's `request_id`.
  - confirmed-read gating continues to wait on `TxDurable` for any heavy or
    light delivery.
- `subscription.PostCommitMeta.CallerResult` was renamed to `CallerOutcome`;
  `subscription.ReducerCallResult` forward-declaration was replaced by
  `subscription.CallerOutcome{Kind, Error, CallerIdentity, ReducerName,
  ReducerID, Args, RequestID, Timestamp, EnergyQuantaUsed, TotalHostExecutionDuration}`.
- `protocol/handle_callreducer.go` now synthesizes heavy `StatusFailed` envelopes
  for pre-acceptance rejections (lifecycle-reducer-name collision, executor-unavailable).
- `CallReducerRequest.ResponseCh` is now `chan<- TransactionUpdate` (heavy).
- `docs/parity-phase1.5-outcome-model.md` records the decision and the explicit
  deferrals (all numeric metadata is stubbed zero; Shunter's former
  `failed_user` / `failed_panic` / `not_found` flat statuses are collapsed onto
  `StatusFailed`; `StatusOutOfEnergy` is shape-only).
- Latest broad verification: `940 passed in 9 packages`.

Pinned parity tests (do not flip without a named parity reason)
- `protocol/parity_message_family_test.go`:
  - `TestPhase15TransactionUpdateHeavyShape`
  - `TestPhase15TransactionUpdateLightShape`
  - `TestPhase15ReducerCallInfoShape`
  - `TestPhase15UpdateStatusVariants`
  - `TestPhase15TagReducerCallResultReserved`
  - `TestPhase1DeferralSubscribeNoQueryIdOrMultiVariants` (still open; Phase 2 Slice 2)
  - `TestPhase1DeferralCallReducerNoFlagsField` (still open; next sub-slice)
  - `TestPhase1DeferralOneOffQueryStructuredNotSQL` (still open; Phase 2 Slice 1)
- `subscription/fanout_worker_test.go::TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout`
- `subscription/phase0_parity_test.go::TestPhase0ParityCanonicalReducerDeliveryFlow`

Phase 1.5 deferrals still open (each independently landable)
1. `CallReducer.flags` — notably `NoSuccessfulUpdate` to suppress caller-echo
   delivery. Flip `TestPhase1DeferralCallReducerNoFlagsField` when the field
   lands on the wire; wire suppression into the fan-out worker's caller-dispatch
   branch; add a parity test for the suppressed caller-echo path.
2. `TransactionUpdate.CallerIdentity` population — today zeroed at the executor
   seam. Source is the originating `CallReducerCmd.Caller.Identity`; thread it
   into `PostCommitMeta.CallerOutcome.CallerIdentity`. Phase 3 runtime-parity
   concern, but structurally small.
3. `TransactionUpdate.ReducerCall.ReducerID` — look up the reducer's numeric ID
   from the reducer registry (`schema/` or `executor/registry.go`) and
   populate it in `CallerOutcome.ReducerID`.
4. `TransactionUpdate.ReducerCall.ReducerName` / `Args` — currently zeroed.
   Thread from the originating `CallReducerCmd`.
5. `Timestamp` (reducer start time, nanoseconds) — record in the executor
   before dispatch; pass via `PostCommitMeta.CallerOutcome.Timestamp`.
6. `TotalHostExecutionDuration` — measure from dispatch to post-commit in the
   executor; pass via `CallerOutcome`.
7. `EnergyQuantaUsed` — no energy model; keep zero and mark as a permanent
   deferral unless the workload requires it.
8. Finer `StatusFailed` classification — Shunter's former
   `failed_user` / `failed_panic` / `not_found` distinctions collapsed into a
   single `StatusFailed.Error` message in Phase 1.5. Phase 3 may want to
   preserve the classification separately (or pin the collapse as permanent).

Suggested next slice: `CallReducer.flags`
- Scope is genuinely narrow (one uint8 on one client message).
- Closes one of the Phase 1.5 deferrals with clear parity tests.
- Does not touch the executor's commit path, so fits in one session easily.

Required reading order
1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md`
8. `docs/parity-phase0-ledger.md`
9. `docs/parity-phase1.5-outcome-model.md`
10. `SPEC-AUDIT.md` §3.4, §3.9, §3.5 (`CallReducer.flags`), and §2.10 / §2.12
    for the newly-closed rows.
11. `TECH-DEBT.md` (scan for protocol / subscription entries that reference the
    renamed seam; most should be stale and no-ops).

Primary source files to inspect when wiring `CallReducer.flags`
- `protocol/client_messages.go` (add `Flags uint8`)
- `protocol/tags.go` (no tag change needed)
- `protocol/handle_callreducer.go`
- `protocol/lifecycle.go` (`CallReducerRequest.Flags`)
- `subscription/fanout_worker.go` (caller-dispatch suppression)
- `subscription/fanout.go` (`CallerOutcome.Flags` or separate meta field)
- `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs` (flags spec)

Hard rules
- Use RTK for every shell/git command.
- Strict TDD for any behavior change: failing test first, watch it fail, minimal fix.
- Every new test/doc artifact must name the external behavior being matched.
- No silent divergences: close, defer-with-reason, or keep-with-reason.
- Phase 0 harness is frozen. Phase 1 + Phase 1.5 envelope split are frozen.
- Do not pivot into SQL/query work, scheduler work, or broad tech-debt cleanup.

Stop rule
- Stop when one narrow Phase 1.5 sub-slice is landed, tested, and documented.
- Do not broaden into Phase 2 in the same session.
