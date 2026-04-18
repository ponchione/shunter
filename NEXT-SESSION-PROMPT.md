Continue Shunter in a fresh agent session.

This is not another Phase 0 harness session.
Do not restart the parity audit, do not rebuild the ledger, and do not spend the session on broad runtime debt cleanup.

Primary decision
- Treat `docs/spacetimedb-parity-roadmap.md` as the active development driver.
- Treat Phase 0 as materially landed for now.
- Start with Phase 1: wire-level protocol envelope parity.
- Keep Phase 1.5 and later phases visible, but do not jump ahead unless a tiny protocol slice forces it.

What landed last session
- Added `docs/parity-phase0-ledger.md` as the concrete Phase 0 companion ledger.
- Patched `docs/spacetimedb-parity-roadmap.md` so the roadmap points at that ledger.
- Added `subscription/phase0_parity_test.go` with `TestPhase0ParityCanonicalReducerDeliveryFlow`.
- Verified:
  - `rtk go test ./subscription -run TestPhase0ParityCanonicalReducerDeliveryFlow`
  - `rtk go test ./subscription ./protocol ./executor ./commitlog ./store`
  - `rtk go test ./...`
- Latest broad result at handoff time: `920 passed in 9 packages`.

Why the next slice is Phase 1
- The Phase 0 target is now explicit enough that the next session should stop building harnesses and start closing the most visible parity differences.
- The ledger already maps the strongest existing protocol tests into a conformance bucket.
- The canonical cross-seam delivery scenario already exists and can anchor follow-on work.
- The best next leverage is now wire-level protocol parity, exactly as the roadmap says.

Chosen next slice
- Phase 1 — wire-level protocol envelope parity
- Start with the protocol boundary, not query/runtime/store semantics.

Required reading order
1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md`
   - especially Phase 1, Phase 1.5, Immediate next slices, and Rules for implementation work
8. `docs/parity-phase0-ledger.md`
9. `SPEC-AUDIT.md`
10. `TECH-DEBT.md`

Then inspect these live protocol files first
- `protocol/options.go`
- `protocol/upgrade.go`
- `protocol/tags.go`
- `protocol/wire_types.go`
- `protocol/compression.go`
- `protocol/sender.go`
- `protocol/server_messages.go`
- `protocol/client_messages.go`
- `protocol/dispatch.go`
- `protocol/close.go`
- `protocol/disconnect.go`
- `protocol/lifecycle.go`
- `protocol/conn.go`

And these tests first
- `protocol/upgrade_test.go`
- `protocol/lifecycle_test.go`
- `protocol/dispatch_test.go`
- `protocol/compression_test.go`
- `protocol/send_txupdate_test.go`
- `protocol/send_reducer_result_test.go`
- `protocol/reconnect_test.go`
- `protocol/backpressure_in_test.go`
- `protocol/backpressure_out_test.go`
- `protocol/close_test.go`
- `subscription/phase0_parity_test.go`

Hard rules
- Use RTK for every shell/git command.
- Stay in Phase 1.
- Do not re-open Phase 0 except for tiny maintenance edits if a just-landed protocol change makes the ledger stale.
- Do not widen into broad query-surface, scheduler, or recovery implementation.
- Every new test/doc artifact must name the external behavior being matched.
- Follow strict TDD for any new behavior change: failing test first, watch it fail, minimal fix, rerun focused tests, then broader verification.

Implementation target
Make the protocol boundary less observably different from SpacetimeDB.

Priority order inside Phase 1
1. Subprotocol decision
- Decide whether Shunter should accept the reference protocol identifiers directly or intentionally retain the Shunter token for now.
- If you do not close this fully, make the retained divergence explicit in docs/tests.

2. Compression-envelope/tag parity
- Tighten the current compression envelope/tag behavior toward the intended parity target.
- Keep the protocol-boundary tests as the source of truth.

3. Handshake / close-code alignment
- Tighten upgrade rejection, policy/protocol close paths, and lifecycle shutdown semantics.

4. Message-family cleanup at the frame boundary
- Reduce obviously Shunter-specific frame/message-family behavior where that difference is visible before deeper runtime semantics.

Suggested execution plan
1. Re-read the Phase 1 section of `docs/spacetimedb-parity-roadmap.md` and the protocol rows in `docs/parity-phase0-ledger.md`.
2. Inventory the exact current protocol divergences from `SPEC-AUDIT.md`.
3. Pick one narrow Phase 1 slice only.
4. Add a failing protocol-boundary test first.
5. Implement the minimum code change.
6. Rerun the focused protocol tests.
7. Rerun `rtk go test ./protocol`.
8. If the change touches cross-seam delivery behavior, rerun `rtk go test ./subscription` too.
9. Finish with `rtk go test ./...`.
10. Update the handoff docs again so the next session starts at the next unresolved parity slice rather than repeating work.

Suggested verification commands
- `rtk go test ./protocol -run <focused test>`
- `rtk go test ./protocol`
- `rtk go test ./subscription`
- `rtk go test ./...`

What success looks like next session
- At least one real Phase 1 protocol divergence is closed or made explicitly intentional.
- The change is locked by protocol-boundary tests, not just helper tests.
- `docs/spacetimedb-parity-roadmap.md` / `docs/parity-phase0-ledger.md` stay honest about what is now open vs closed.
- The next handoff clearly points either to the next Phase 1 sub-slice or, if Phase 1 is surprisingly finished, to Phase 1.5.

What not to do next session
- do not spend the session rebuilding the Phase 0 harness
- do not pivot into SQL/query-surface work
- do not pivot into scheduler/recovery implementation yet
- do not use TECH-DEBT cleanup as the main objective
- do not make broad refactors without a named parity behavior to justify them

Best current next implementation slice
- `Phase 1 — wire-level protocol envelope parity`
- strongest likely first target:
  - subprotocol decision and/or compression-envelope/tag parity

Stop rule
- Stop when one narrow Phase 1 parity slice is truly landed, tested, and documented.
- Do not broaden into Phase 1.5 in the same session unless the protocol slice is genuinely complete and verified.