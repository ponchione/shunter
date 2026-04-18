Continue Shunter implementation work in a fresh session.

This is not a broad audit pass, not a Phase 0 harness-building session, and not a general runtime-debt sweep.
This session should begin with Phase 1 from `docs/spacetimedb-parity-roadmap.md`: wire-level protocol envelope parity.

Context
- `REMAINING.md` still says all tracked execution-order implementation slices are complete.
- The parity-roadmap audit is done and remains the active development driver.
- Phase 0 is now materially real enough to stop rebuilding harnesses for the moment.
- Last session added:
  - `docs/parity-phase0-ledger.md`
  - roadmap references to that ledger in `docs/spacetimedb-parity-roadmap.md`
  - `subscription/phase0_parity_test.go` with `TestPhase0ParityCanonicalReducerDeliveryFlow`
- Last session verified:
  - `rtk go test ./subscription -run TestPhase0ParityCanonicalReducerDeliveryFlow`
  - `rtk go test ./subscription ./protocol ./executor ./commitlog ./store`
  - `rtk go test ./...`
- Latest broad result at handoff: `920 passed in 9 packages`

Chosen next slice
- Phase 1 — wire-level protocol envelope parity
- Scope: one narrow externally visible protocol-parity fix at a time
- Do not jump ahead into Phase 1.5 delivery semantics unless the chosen protocol change truly forces a tiny cross-seam follow-through

Primary objective
Close one real protocol-boundary divergence and lock it with parity-oriented tests.

Good candidate targets, in order
1. subprotocol decision
2. compression-envelope/tag parity
3. handshake / close-code alignment
4. message-family cleanup at the frame boundary

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

Then inspect the live protocol/test surfaces most relevant to Phase 1
- protocol code:
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
- protocol tests:
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
- anchor context:
  - `docs/parity-phase0-ledger.md`
  - `subscription/phase0_parity_test.go`

Hard rules
- Use RTK for every shell/git command.
- Stay inside Phase 1.
- Prefer protocol-boundary tests over internal helper churn.
- Do not widen into SQL/query/runtime/store parity implementation.
- Every change must name the external behavior being matched.
- Use strict TDD for any behavior change: failing test first, verify failure, minimum fix, rerun targeted tests, then broader verification.

Concrete deliverables
1. Land one narrow Phase 1 parity slice
- Good examples:
  - explicit subprotocol acceptance/rejection contract
  - compression envelope/tag behavior change
  - close-code alignment in one concrete rejection/error path
  - one message-family/frame-boundary correction

2. Lock it with parity-facing tests
- Prefer the existing protocol test buckets and add a small number of parity-oriented cases if needed.
- Keep tests framed around client-visible behavior, not implementation trivia.

3. Keep the docs honest
- Update `docs/parity-phase0-ledger.md` if the protocol bucket status meaningfully changes.
- Update `docs/spacetimedb-parity-roadmap.md` if the live truth about open/closed Phase 1 work changes.
- Update the next-session prompts again before stopping so the next agent does not repeat the same slice.

Suggested execution plan
1. Read the Phase 1 roadmap section and the Phase 0 protocol rows in the ledger.
2. Inventory the specific current divergence you want to close from `SPEC-AUDIT.md`.
3. Pick a single narrow protocol slice.
4. Add the failing test first.
5. Run the focused test and confirm it fails for the intended reason.
6. Implement the minimum fix.
7. Rerun the focused test.
8. Rerun `rtk go test ./protocol`.
9. If the change touches delivery semantics, also rerun `rtk go test ./subscription`.
10. Finish with `rtk go test ./...`.
11. Rewrite the handoff docs so the next session starts from the real remaining slice.

Suggested verification commands
- `rtk go test ./protocol -run <focused test>`
- `rtk go test ./protocol`
- `rtk go test ./subscription`
- `rtk go test ./...`

Expected deliverable
- one real Phase 1 protocol-parity slice landed
- passing focused protocol tests
- passing broad suite
- handoff docs updated to the next unresolved Phase 1 or Phase 1.5 slice

What not to do in this session
- do not rebuild the Phase 0 harness
- do not start a fresh broad parity audit
- do not pivot into SQL/query-surface implementation
- do not pivot into scheduler/recovery implementation yet
- do not use generic TECH-DEBT cleanup as the main objective
- do not make broad refactors without a named parity behavior target

Current best next slice after this session if only one narrow fix lands
- continue Phase 1 until the wire-level protocol boundary stops being obviously Shunter-specific
- after that, move to Phase 1.5:
  - canonical end-to-end delivery parity
  - caller/non-caller result model decision
  - confirmed-read ordinary-flow semantics
  - no-subscription / empty-changeset edge cases

Stop rule
- Stop when one narrow Phase 1 slice is truly landed, verified, and documented.
- Do not widen into broad parity implementation in the same session.