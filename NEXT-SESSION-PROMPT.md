Continue Shunter in a fresh agent session.

Phase 1 wire-level protocol parity is closed. Do not re-open any P0-PROTOCOL-00* slice.
Do not rebuild harnesses, do not restart the parity audit, and do not spend the session on broad tech-debt cleanup.

Primary decision
- Treat `docs/spacetimedb-parity-roadmap.md` as the active development driver.
- Treat Phase 0 and Phase 1 as materially landed.
- Start with Phase 1.5: first end-to-end delivery parity slice.
- Specifically: the outcome model for `TransactionUpdate` / `ReducerCallResult`, caller/non-caller routing, and the no-subscription / empty-changeset edge cases.

What landed last session (Phase 1 wire-level parity)
All four P0-PROTOCOL-00* slices are now closed or closed-with-divergences-explicit:

- `P0-PROTOCOL-001` subprotocol — closed.
  `v1.bsatn.spacetimedb` accepted and preferred. `v1.bsatn.shunter` retained as an explicit deferral.
  Locked by `protocol/parity_subprotocol_test.go`.

- `P0-PROTOCOL-002` compression tags — closed.
  Reference byte numbering in force: None=0x00, Brotli=0x01 reserved, Gzip=0x02.
  Brotli returns `ErrBrotliUnsupported` and closes 1002 with reason `"brotli unsupported"`.
  Locked by `protocol/parity_compression_test.go`.

- `P0-PROTOCOL-003` close codes + handshake rejection — closed.
  10 call sites audited; drift tests in `protocol/parity_close_codes_test.go`.

- `P0-PROTOCOL-004` message-family — closed (divergences explicit).
  Five deferrals pinned in `protocol/parity_message_family_test.go`.

- Latest broad result at handoff time: `939 passed in 9 packages`.

Why the next slice is Phase 1.5
- The wire envelope is now parity-complete or explicitly diverged.
- The highest remaining observability gap is the cross-seam delivery path: what the client actually receives when a reducer commits.
- The roadmap (Phase 1.5, §4 Slice 3) calls this the "first end-to-end delivery parity slice."
- Closing it requires a decision on the outcome model before touching routing or edge-case suppression rules.

Chosen next slice
- Phase 1.5 — first end-to-end delivery parity slice.
- Primary focus: `TransactionUpdate` / `ReducerCallResult` outcome model, caller/non-caller routing, `confirmed-read` / durability visibility in the ordinary public flow, and no-subscription / empty-changeset edge-case behavior.

Required reading order
1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md`
   - especially Phase 1.5, §4 Slice 3, §5 Rules
8. `docs/parity-phase0-ledger.md`
9. `SPEC-AUDIT.md`
10. `TECH-DEBT.md`

Primary source files to inspect first
- `executor/executor.go`
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `protocol/fanout_adapter.go`
- `protocol/send_txupdate.go`
- `protocol/send_reducer_result.go`
- `protocol/server_messages.go`
- `protocol/handle_callreducer.go`

Primary test anchors
- `protocol/send_txupdate_test.go`
- `protocol/send_reducer_result_test.go`
- `protocol/handle_callreducer_test.go`
- `subscription/phase0_parity_test.go`
- `subscription/fanout_worker_test.go`
- `protocol/parity_message_family_test.go`
  When the outcome-model decision lands, flip the TxUpdate and ReducerCallResult pins here and update the matching SPEC-AUDIT row.

Current parity harness anchors (do not modify without a named parity reason)
- `subscription/phase0_parity_test.go`
- `protocol/parity_subprotocol_test.go`
- `protocol/parity_compression_test.go`
- `protocol/parity_close_codes_test.go`
- `protocol/parity_message_family_test.go`

Hard rules
- Use RTK for every shell/git command.
- Strict TDD for any behavior change: failing test first, watch it fail, minimal fix, rerun focused tests, then broader verification.
- Every new test/doc artifact must name the external behavior being matched.
- No silent divergences: either close, consciously defer, or keep-with-reason.
- Phase 0 harness is frozen. Do not re-open it.
- Do not pivot into SQL/query work, scheduler work, or broad tech-debt cleanup.

Decisions the next session must make (in order)
1. Outcome model: heavy/light split for `TransactionUpdate` (as in the reference), or keep the current unified shape as a deliberate design choice.
   Either way, the decision must be written down and locked by a parity test.
2. Caller/non-caller routing: confirm or correct the current suppression rules against the reference behavior.
3. `confirmed-read` / durability visibility: verify the ordinary public flow is correct or document what differs.
4. No-subscription / empty-changeset edge case: lock the expected behavior with a test.

Suggested execution plan
1. Re-read Phase 1.5 and §4 Slice 3 in `docs/spacetimedb-parity-roadmap.md`.
2. Trace one full reducer-commit path through `executor`, `subscription`, and `protocol` to understand the current delivery seam.
3. Pick the outcome-model decision first. It gates everything else.
4. Write a failing parity test that encodes the chosen shape.
5. Implement the minimum change to make it pass.
6. Rerun `rtk go test ./protocol`.
7. Rerun `rtk go test ./subscription`.
8. Finish with `rtk go test ./...`.
9. Update `SPEC-AUDIT.md` for any row that changed.
10. Update the handoff docs so the next session starts at the next unresolved Phase 1.5 sub-slice.

Suggested verification commands
- `rtk go test ./protocol -run <focused test>`
- `rtk go test ./protocol`
- `rtk go test ./subscription`
- `rtk go test ./...`

What success looks like next session
- The outcome-model decision is made, written, and locked by a parity test.
- At least one of the Phase 1.5 delivery-path divergences is closed or explicitly deferred.
- `SPEC-AUDIT.md` rows for the affected slices are updated.
- The next handoff clearly points at the next unresolved Phase 1.5 sub-slice or, if all four decisions are closed, Phase 2.

What not to do next session
- do not re-open P0-PROTOCOL-00* slices
- do not spend the session rebuilding harnesses already in place
- do not pivot into SQL/query-surface work
- do not pivot into scheduler or recovery implementation
- do not use TECH-DEBT cleanup as the main objective
- do not broaden into Phase 2 until Phase 1.5 is genuinely complete and verified

Stop rule
- Stop when one narrow Phase 1.5 slice is truly landed, tested, and documented.
- Do not broaden into Phase 2 in the same session.
