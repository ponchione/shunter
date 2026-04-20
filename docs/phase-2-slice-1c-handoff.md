# Phase 2 Slice 1c handoff

Use this file to kick off the next agent on the narrow parity slice for `OneOffQuery` message-id wire parity.

## Copy-paste prompt

Continue Shunter on the parity track.

This session is an implementation slice, not a broad audit or cleanup pass.

Primary objective
- Land Phase 2 Slice 1c: `OneOffQuery.message_id` wire-shape parity.
- Shunter currently uses `RequestID uint32` on `OneOffQueryMsg` and `OneOffQueryResult`.
- The target is the reference-style opaque byte identifier: `MessageID []byte`.

Why this slice
- It is the next narrow externally visible parity gap.
- It is explicitly tracked in `TECH-DEBT.md` as `TD-141`.
- It does not depend on the resolved subscription-admission blockers (`TD-136`, `TD-137`, `TD-138`, `TD-140`).

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
10. `TECH-DEBT.md` around `TD-141`

Grounded context
- `docs/parity-phase0-ledger.md` says Phases 1 and 1.5 are closed, Phase 2 Slice 2 is closed, and the remaining Phase 2 anchors are SQL `OneOffQuery` / lag policy.
- `TECH-DEBT.md` `TD-141` defines the exact follow-up:
  - rename `RequestID` -> `MessageID`
  - change the type to `[]byte`
  - update encoder/decoder to use a length-prefixed byte string
  - flip `OneOffQueryResult.RequestID` correlation accordingly
  - add positive-shape pin `TestPhase2Slice1COneOffQueryMessageIDBytes`
- `TD-141` is intentionally narrow and independent.

Likely file surface
- `protocol/client_messages.go`
- `protocol/server_messages.go`
- `protocol/wire_types.go`
- `protocol/handle_oneoff.go`
- `protocol/handle_oneoff_test.go`
- `protocol/server_messages_test.go`
- `protocol/parity_message_family_test.go`
- any encoder/decoder helpers used by one-off query messages

TDD requirements
1. Write or update the parity pin first:
   - `TestPhase2Slice1COneOffQueryMessageIDBytes`
2. Run the targeted test and watch it fail.
3. Make the smallest code change set that flips the wire shape.
4. Run focused protocol tests.
5. Run a broad verification pass if the focused pass is clean.

Locked implementation decisions
- Keep the scope to `OneOffQuery` only.
- Do not widen SQL grammar in this slice.
- Do not touch lag / slow-client policy in this slice.
- Do not reopen subscription admission model work; that was resolved by ADR `docs/adr/2026-04-19-subscription-admission-model.md`.
- Do not reintroduce root prompt docs or other retired historical docs.

Expected behavioral change
- Client request envelope for one-off query uses opaque `MessageID []byte` instead of `RequestID uint32`.
- Server response envelope correlates with the same opaque byte identifier.
- Encoding/decoding treats the identifier as bytes, not a numeric scalar.
- Existing one-off query success/error behavior should remain unchanged apart from identifier shape.

Suggested verification commands
- `rtk go test ./protocol -run TestPhase2Slice1COneOffQueryMessageIDBytes -v`
- `rtk go test ./protocol -run 'TestOneOffQuery|TestPhase2Slice1'`
- `rtk go test ./protocol`
- `rtk go test ./...`

Acceptance gate
- `OneOffQueryMsg` and `OneOffQueryResult` use `MessageID []byte` at the wire boundary.
- The new positive-shape parity pin passes.
- Existing one-off query handler tests pass after the identifier migration.
- No unrelated protocol message families change shape.

Stop / escalate if
- The wire codec for `OneOffQuery` is more shared than expected and changing it would also silently alter subscribe/unsubscribe/call-reducer shapes.
- The response correlation path requires a broader protocol-wide request-id abstraction refactor.
- The slice tempts widening SQL grammar; stop and defer that to `TD-142`.

Deliverables
- code + tests for Phase 2 Slice 1c
- any tiny doc update needed in `docs/parity-phase0-ledger.md`, `docs/spacetimedb-parity-roadmap.md`, or `TECH-DEBT.md`
- concise final note stating the slice is closed and what remains next (likely lag policy or `P0-RECOVERY-002`)

## Operator note

If this slice lands cleanly, the next strongest resume point is `P0-RECOVERY-002` (`TxID` / `nextID` / sequence invariants across snapshot + replay), not generic cleanup.
