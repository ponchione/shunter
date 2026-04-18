Continue Shunter in a fresh agent session.

Phase 1 wire-level protocol parity is closed.
Phase 1.5 end-to-end delivery parity — the `TransactionUpdate` /
`ReducerCallResult` envelope split AND the `CallReducer.flags`
(`NoSuccessNotify`) sub-slice — is closed. Do not re-open any
P0-PROTOCOL-00* slice and do not re-litigate the outcome-model decision.

Primary decision
- Treat `docs/spacetimedb-parity-roadmap.md` as the active development driver.
- Treat Phase 0, Phase 1, and Phase 1.5 (envelope split + `CallReducer.flags`) as materially landed.
- The next narrow parity slices are Phase 2 query-surface work
  (`OneOffQuery` SQL front door and the remaining `SubscribeMulti` /
  `SubscribeSingle` split / `QueryId` follow-through) or Phase 4
  recovery parity (`P0-RECOVERY-002`).

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
  deferrals (`EnergyQuantaUsed` remains zero because Shunter has no energy
  model; Shunter's former `failed_user` / `failed_panic` / `not_found`
  flat statuses are collapsed onto `StatusFailed`; `StatusOutOfEnergy`
  is shape-only).
- Latest broad verification: `955 passed in 9 packages`.

Pinned parity tests (do not flip without a named parity reason)
- `protocol/parity_message_family_test.go`:
  - `TestPhase15TransactionUpdateHeavyShape`
  - `TestPhase15TransactionUpdateLightShape`
  - `TestPhase15ReducerCallInfoShape`
  - `TestPhase15UpdateStatusVariants`
  - `TestPhase15TagReducerCallResultReserved`
  - `TestPhase2DeferralSubscribeNoMultiOrSingleVariants` (still open; Phase 2 Slice 2)
  - `TestPhase15CallReducerFlagsField` (closed sub-slice — positive-shape pin)
  - `TestPhase1DeferralOneOffQueryStructuredNotSQL` (still open; Phase 2 Slice 1)
- `subscription/fanout_worker_test.go::TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout`
- `subscription/phase0_parity_test.go::TestPhase0ParityCanonicalReducerDeliveryFlow`

Phase 1.5 deferrals still open
1. `EnergyQuantaUsed` — no energy model; keep zero and treat it as a
   permanent deferral unless an energy/quota subsystem is introduced.
2. Finer `StatusFailed` classification — Shunter's former
   `failed_user` / `failed_panic` / `not_found` distinctions collapsed into a
   single `StatusFailed.Error` message in Phase 1.5. Phase 3 may want to
   preserve the classification separately (or pin the collapse as permanent).

Recently landed (Phase 1.5 `CallReducer.flags` sub-slice)
- `CallReducerMsg.Flags byte` on the wire (reference `CallReducerFlags`:
  `FullUpdate=0`, `NoSuccessNotify=1`). Encoder appends a trailing u8 after
  `Args`; decoder rejects out-of-range bytes as `ErrMalformedMessage`.
- Flags propagates through `protocol.CallReducerRequest.Flags` →
  `executor.ReducerRequest.Flags` → `postCommitOptions.callerFlags` →
  `subscription.CallerOutcome.Flags`.
- `subscription/fanout_worker.go::deliver` now suppresses the caller's
  heavy `TransactionUpdate` when `CallerOutcomeCommitted` +
  `CallerOutcomeFlagNoSuccessNotify`. Failure / out-of-energy outcomes
  are never suppressed. Confirmed-read gating treats the caller as
  absent when suppressed.

Suggested next slice
- Phase 2 Slice 1 (`OneOffQuery` SQL front door), Phase 2 Slice 2
  server-response `QueryID` follow-through plus the remaining
  `SubscribeMulti` / `SubscribeSingle` split, or Phase 4
  `P0-RECOVERY-002` if recovery parity is the priority.

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
10. `SPEC-AUDIT.md` §3.4, §3.6, §3.9, §2.10 / §2.12 for the closed rows.
11. `TECH-DEBT.md` (scan for protocol / subscription entries that reference the
    renamed seam; most should be stale and no-ops).

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
