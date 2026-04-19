Continue Shunter in a fresh agent session.

Phase 1 wire-level protocol parity is closed.
Phase 1.5 end-to-end delivery parity — the `TransactionUpdate` /
`ReducerCallResult` envelope split AND the `CallReducer.flags`
(`NoSuccessNotify`) sub-slice — is closed. Do not re-open any
P0-PROTOCOL-00* slice and do not re-litigate the outcome-model decision.

Phase 2 Slice 2 is closed. `QueryID` naming parity landed on both
request envelopes (`SubscribeMsg` / `UnsubscribeMsg`) and response
envelopes (`SubscribeApplied` / `UnsubscribeApplied` /
`SubscriptionError`), and the `SubscribeMulti` / `SubscribeSingle`
variant split landed with one-QueryID-per-query-set grouping semantics
that match the reference.

Primary decision
- Treat `docs/spacetimedb-parity-roadmap.md` as the active development driver.
- Treat Phase 0, Phase 1, Phase 1.5 (envelope split + `CallReducer.flags`),
  and Phase 2 Slice 2 (QueryID naming + SubscribeMulti/SubscribeSingle
  variant split) as materially landed.
- The next narrow parity slices are Phase 2 Slice 1 (`OneOffQuery` SQL
  front door), Phase 2 lag / slow-client policy, or Phase 4 recovery
  parity (`P0-RECOVERY-002`).

What landed last session (Phase 2 Slice 2 variant split)
- New client-side envelopes: `SubscribeSingleMsg` (renamed from the
  former `SubscribeMsg`) and `SubscribeMultiMsg{RequestID, QueryID,
  Queries []Predicate}`; mirror unsubscribe envelopes
  `UnsubscribeSingleMsg` / `UnsubscribeMultiMsg`.
- New server-side envelopes: `SubscribeSingleApplied` /
  `UnsubscribeSingleApplied` (renamed from the former Applied pair)
  and `SubscribeMultiApplied` / `UnsubscribeMultiApplied` that scope
  the delivered rows to the full multi-query set keyed by `QueryID`.
- Set-based subscription-manager API: `RegisterSet` /
  `UnregisterSet` on `subscription.SubscriptionManager`, consuming
  `SubscriptionSetRegisterRequest` / `SubscriptionSetRegisterResult` /
  `SubscriptionSetUnregisterResult`. The former `Register` /
  `Unregister` methods and their request/result types were removed.
- Set-based executor commands: `executor.RegisterSubscriptionSetCmd` /
  `executor.UnregisterSubscriptionSetCmd` replace the former
  single-subscription commands.
- Protocol handlers split: `handleSubscribeSingle` /
  `handleSubscribeMulti` / `handleUnsubscribeSingle` /
  `handleUnsubscribeMulti` dispatch on the new envelope tags; the
  single-path keeps one predicate in the set, the multi-path submits
  the whole predicate list atomically.
- `ErrQueryIDAlreadyLive` is returned by `RegisterSet` when a client
  reuses a live `(ConnID, QueryID)` pair (reference behavior:
  `add_subscription_multi try_insert` at
  `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:1050`).
- Task 10 (host adapter wiring through `protocol.ExecutorInbox`) was
  intentionally skipped: no host adapter exists in-repo — the
  interface is implemented only by test fakes, and there is no `cmd/`
  binary. Host-side wiring is a downstream follow-up when the host
  binary is introduced.
- Latest broad verification target: `983 passed in 9 packages`.

Pinned parity tests (do not flip without a named parity reason)
- `protocol/parity_message_family_test.go`:
  - `TestPhase15TransactionUpdateHeavyShape`
  - `TestPhase15TransactionUpdateLightShape`
  - `TestPhase15ReducerCallInfoShape`
  - `TestPhase15UpdateStatusVariants`
  - `TestPhase15TagReducerCallResultReserved`
  - `TestPhase15CallReducerFlagsField` (closed sub-slice — positive-shape pin)
  - `TestPhase2SubscribeCarriesQueryID` (closed Phase 2 Slice 2 request side)
  - `TestPhase2UnsubscribeCarriesQueryID` (closed Phase 2 Slice 2 request side)
  - `TestPhase2SubscribeAppliedCarriesQueryID` (closed Phase 2 Slice 2 response side)
  - `TestPhase2UnsubscribeAppliedCarriesQueryID` (closed Phase 2 Slice 2 response side)
  - `TestPhase2SubscriptionErrorCarriesQueryID` (closed Phase 2 Slice 2 response side)
  - `TestPhase2SubscribeSingleShape` (closed Phase 2 Slice 2 variant split)
  - `TestPhase2SubscribeMultiShape` (closed Phase 2 Slice 2 variant split)
  - `TestPhase2UnsubscribeSingleShape` (closed Phase 2 Slice 2 variant split)
  - `TestPhase2UnsubscribeMultiShape` (closed Phase 2 Slice 2 variant split)
  - `TestPhase2SubscribeSingleAppliedShape` (closed Phase 2 Slice 2 variant split)
  - `TestPhase2UnsubscribeSingleAppliedShape` (closed Phase 2 Slice 2 variant split)
  - `TestPhase2SubscribeMultiAppliedShape` (closed Phase 2 Slice 2 variant split)
  - `TestPhase2UnsubscribeMultiAppliedShape` (closed Phase 2 Slice 2 variant split)
  - `TestPhase2TagByteStability` (tag-byte stability pin)
  - `TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration` (still open; applied envelope lacks `TotalHostExecutionDurationMicros`)
  - `TestPhase2DeferralSubscriptionErrorNoTableID` (still open; `SubscriptionError.TableID` / optional-field shape)
  - `TestPhase2DeferralSubscribeMultiQueriesStructured` (still open; `SubscribeMulti.Queries` is a structured predicate list, not a SQL string list — paired with Phase 2 Slice 1)
  - `TestPhase1DeferralOneOffQueryStructuredNotSQL` (still open; Phase 2 Slice 1)
- `subscription/fanout_worker_test.go::TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout`
- `subscription/phase0_parity_test.go::TestPhase0ParityCanonicalReducerDeliveryFlow`

Phase 2 deferrals still open
1. `OneOffQuery` SQL front door (Phase 2 Slice 1) — pinned by
   `TestPhase1DeferralOneOffQueryStructuredNotSQL` and
   `TestPhase2DeferralSubscribeMultiQueriesStructured`.
2. `TotalHostExecutionDurationMicros` on applied envelopes — pinned
   by `TestPhase2DeferralSubscribeAppliedNoHostExecutionDuration`.
3. `SubscriptionError.TableID` / optional-field shape — pinned by
   `TestPhase2DeferralSubscriptionErrorNoTableID`.
4. Lag / slow-client policy — Shunter's bounded disconnect-on-lag
   fanout still diverges from the reference queueing model; pinned
   as an explicit deferral pending the Phase 2 policy decision.
5. Host adapter wiring on `protocol.ExecutorInbox` — no host binary
   exists in-repo; recorded as a downstream follow-up when a host
   binary introduces a production implementer of the inbox.

Phase 1.5 deferrals still open
1. `EnergyQuantaUsed` — no energy model; keep zero and treat it as a
   permanent deferral unless an energy/quota subsystem is introduced.
2. Finer `StatusFailed` classification — Shunter's former
   `failed_user` / `failed_panic` / `not_found` distinctions collapsed into a
   single `StatusFailed.Error` message in Phase 1.5. Phase 3 may want to
   preserve the classification separately (or pin the collapse as permanent).

Suggested next slice
- Phase 2 Slice 1 (`OneOffQuery` SQL front door) — the remaining
  Phase 2 protocol-surface parity anchor now that the variant split
  is closed. Would also let `TestPhase2DeferralSubscribeMultiQueriesStructured`
  and `TestPhase1DeferralOneOffQueryStructuredNotSQL` flip from
  deferral pins to positive pins.
- Or Phase 4 `P0-RECOVERY-002` (TxID / nextID / sequence invariants
  across snapshot + replay) if recovery parity is the priority.

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
