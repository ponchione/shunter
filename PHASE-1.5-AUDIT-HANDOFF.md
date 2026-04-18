# Audit handoff — Phase 1.5 `TransactionUpdate` heavy/light split

## Mission

Independently confirm that the Phase 1.5 envelope-split work in commit
`542c438` is sound, internally consistent, and honestly reflected in the
docs. Report findings as a punch list of accepted / rejected / requires-fix
items. Do not rewrite the implementation. Do not re-open any closed parity
slice. If a claim is right, say so; if a claim is wrong, point at the file
and line that proves it.

You are an independent reviewer. Assume nothing about what "the previous
session claimed" beyond the artifacts in the repo. Verify by reading code
and tests, not by trusting prose. Trust but verify: a doc saying "X is
pinned by test Y" is a claim, not a proof — open test Y and check.

## Scope

Commit under audit: `542c438` — `protocol(parity): Phase 1.5 TransactionUpdate heavy/light split — P0-DELIVERY-001/002 closed`.

Previous session's stated intent:
- Adopt the reference heavy `TransactionUpdate` + `TransactionUpdateLight`
  envelope split and drop `ReducerCallResult` from the wire surface.
- Keep `TagReducerCallResult` reserved (byte 7) so a future reintroduction
  cannot silently collide.
- Caller always receives heavy; non-callers with row-touches receive light;
  confirmed-read gating continues to wait on `TxDurable`.
- Caller's outcome envelope must not be dropped on empty changeset /
  no-active-subscription commits (`P0-DELIVERY-002`).
- Numeric metadata (`Timestamp`, `EnergyQuantaUsed`, `TotalHostExecutionDuration`)
  and caller identity / reducer-id / reducer-name / args are stubbed zero,
  explicitly deferred to Phase 3 or a Phase 1.5 follow-up sub-slice.
- Shunter's former `failed_user` / `failed_panic` / `not_found` flat statuses
  collapsed onto `StatusFailed{Error}`.

## Required reading (in order)

1. `RTK.md` — mandatory shell / git wrapper rules. Use RTK for every
   shell and git command you run.
2. `CLAUDE.md` — repo rules and reading order.
3. `docs/project-brief.md` — product intent.
4. `docs/spacetimedb-parity-roadmap.md` — development driver. Phase 1.5
   §4 Slice 3 is the scope under audit.
5. `docs/parity-phase0-ledger.md` — the `P0-*` scenario ledger. Note the
   rows that Phase 1.5 flipped:
   - `P0-DELIVERY-001` → `closed`
   - `P0-DELIVERY-002` → `closed`
   - `P0-PROTOCOL-004` row text updated (no status change).
6. `docs/parity-phase1.5-outcome-model.md` — the written-down decision
   and deferral list. This is the core doc; everything else flows from it.
7. `SPEC-AUDIT.md` — rows `3.4`, `3.9`, `2.10`, `2.12` were flipped from
   GAP / DIVERGE to RESOLVED. Row `3.5` (`CallReducer.flags`) is the next
   Phase 1.5 sub-slice and stays open.
8. `NEXT-SESSION-PROMPT.md` — handoff for the *next* session. Verify it
   matches the actual state of the repo.
9. `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`
   — the reference shape Phase 1.5 is matching. Treat this as read-only
   research input only (clean-room rules in `CLAUDE.md` — do NOT copy
   Rust source into implementation).

## Primary source files to inspect

Protocol layer:
- `protocol/server_messages.go` — type definitions + BSATN encode/decode
- `protocol/tags.go` — tag constants, including reserved `TagReducerCallResult`
- `protocol/sender.go` — `ClientSender` interface + `connManagerSender`
- `protocol/fanout_adapter.go` — `FanOutSenderAdapter` converting
  subscription-domain types to wire format
- `protocol/send_txupdate.go` — `DeliverTransactionUpdateLight` helper
- `protocol/handle_callreducer.go` — pre-acceptance rejection synthesis
- `protocol/async_responses.go` — `connOnlySender` + `watchReducerResponse`
- `protocol/lifecycle.go` — `CallReducerRequest.ResponseCh` type change
- `protocol/parity_message_family_test.go` — Phase 1.5 parity pins

Subscription layer:
- `subscription/fanout.go` — `FanOutMessage`, `PostCommitMeta`,
  `CallerOutcome` (replaces former `ReducerCallResult` forward-decl)
- `subscription/fanout_worker.go` — caller-heavy + non-caller-light dispatch
- `subscription/eval.go` — `EvalAndBroadcast` no-early-return-when-caller-present guard

Executor layer:
- `executor/executor.go` — `postCommit` populates `PostCommitMeta.CallerOutcome`

Test anchors:
- `protocol/fanout_adapter_test.go` — adapter integration tests
- `protocol/send_txupdate_test.go` — non-caller light delivery tests
- `protocol/handle_callreducer_test.go` — synthesized heavy failure envelopes
- `protocol/server_messages_test.go` — round-trip encoding tests
- `protocol/sender_test.go` — typed `SendTransactionUpdate` /
  `SendTransactionUpdateLight` paths
- `subscription/fanout_worker_test.go` — dispatch + confirmed-read +
  empty-fanout caller-outcome tests
- `subscription/phase0_parity_test.go` — `P0-DELIVERY-001` scenario
- `subscription/fanout_test.go` — `CallerOutcome` shape pin
- `executor/pipeline_test.go` — `postCommit` caller-metadata wiring

## Verification checklist

Work through each item. For each, state one of: `accepted` (with a
pointer to the evidence), `rejected` (with the file/line that disproves
the claim), or `requires-fix` (with a short description of what's wrong
and where).

### A. Wire-shape pins match the reference

1. `TransactionUpdate` (heavy) fields are exactly `Status`,
   `CallerIdentity`, `CallerConnectionID`, `ReducerCall`, `Timestamp`,
   `EnergyQuantaUsed`, `TotalHostExecutionDuration` — in that order.
   Pinned by `TestPhase15TransactionUpdateHeavyShape`. Compare against
   `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`
   `pub struct TransactionUpdate<F: WebsocketFormat>`.
2. `TransactionUpdateLight` fields are exactly `RequestID`, `Update`.
   Pinned by `TestPhase15TransactionUpdateLightShape`. Compare against
   reference `pub struct TransactionUpdateLight<F>`.
3. `ReducerCallInfo` fields are exactly `ReducerName`, `ReducerID`, `Args`,
   `RequestID`. Pinned by `TestPhase15ReducerCallInfoShape`. Compare
   against reference `pub struct ReducerCallInfo<F>`.
4. `UpdateStatus` is a three-arm tagged union: `StatusCommitted{Update}`,
   `StatusFailed{Error}`, `StatusOutOfEnergy{}`. Pinned by
   `TestPhase15UpdateStatusVariants`. Compare against reference
   `pub enum UpdateStatus<F>`.

### B. `ReducerCallResult` really is gone from the wire surface

5. `protocol.ReducerCallResult` type no longer exists. Confirm via
   `rtk grep "^type ReducerCallResult" protocol`.
6. `TagReducerCallResult` constant is still defined (byte 7), is NOT
   emitted by `EncodeServerMessage`, and is rejected by
   `DecodeServerMessage` with `ErrUnknownMessageTag`. Pinned by
   `TestPhase15TagReducerCallResultReserved`.
7. `subscription.ReducerCallResult` forward-declaration is gone.
   Replacement is `subscription.CallerOutcome` in `subscription/fanout.go`.
   Confirm via `rtk grep "ReducerCallResult" subscription` — should return
   nothing.
8. Repo-wide sanity check: `rtk grep "ReducerCallResult" -- **/*.go`
   should only surface the reserved-tag reference or the audit handoff
   itself, not live code.

### C. Dispatch rules

9. `subscription/fanout_worker.go::deliver`:
   - caller always receives heavy `TransactionUpdate` via
     `SendTransactionUpdateHeavy` when `CallerConnID` is set, regardless
     of whether `Fanout[CallerConnID]` is populated.
   - non-callers with row-touches receive light `TransactionUpdateLight`
     via `SendTransactionUpdateLight`. Caller is skipped (not deleted)
     during iteration.
   - light-envelope `RequestID` is sourced from
     `msg.CallerOutcome.RequestID` when present (non-caller updates
     inherit the originating request id).
   - confirmed-read gating still waits on `TxDurable` when any recipient
     requires it. Caller-only batches with no non-caller recipients still
     participate in gating.
10. `subscription/eval.go::EvalAndBroadcast`:
    - the `hasActive + empty-changeset` early-return is NOT taken when
      `CallerConnID` + `CallerOutcome` are both set.
    - when early-return is skipped, a `FanOutMessage` with empty `Fanout`
      + populated caller metadata is still pushed to the worker inbox.

### D. Pre-acceptance rejection synthesis

11. `protocol/handle_callreducer.go`:
    - lifecycle-reducer-name collision emits a heavy `TransactionUpdate`
      with `StatusFailed{Error: "lifecycle reducer cannot be called externally"}`.
    - executor-unavailable (`executor.CallReducer` returns an error) emits
      a heavy `TransactionUpdate` with
      `StatusFailed{Error: "executor unavailable: <wrapped>"}`.
    - both paths set `ReducerCall.ReducerName` / `Args` / `RequestID`
      from the incoming `CallReducerMsg`.
    - both paths send via `connOnlySender.SendTransactionUpdate` (direct,
      not through the fanout seam).

### E. Executor seam

12. `executor/executor.go::postCommit`:
    - `PostCommitMeta.CallerOutcome` is populated for
      `CallSourceExternal` commits.
    - `CallerOutcome.Kind` is `CallerOutcomeCommitted`.
    - `CallerOutcome.RequestID` carries `opts.callerRequestID`.
    - `CallerIdentity`, `ReducerName`, `Args`, `ReducerID`, `Timestamp`,
      `EnergyQuantaUsed`, `TotalHostExecutionDuration` are all zero —
      this is the *stated* deferral. Confirm the zero values are
      explicit and commented, not accidental.
13. `protocol.CallReducerRequest.ResponseCh` type is `chan<- TransactionUpdate`
    (not the old `chan<- ReducerCallResult`). Confirm via
    `rtk grep "ResponseCh" protocol/lifecycle.go`.

### F. Doc honesty

14. `docs/parity-phase1.5-outcome-model.md` accurately describes what
    actually landed. In particular:
    - the "Adopted now" section matches real type definitions in
      `protocol/server_messages.go`
    - the "Deferred explicitly" section lists fields that are actually
      zeroed in `executor/executor.go::postCommit`, not claimed to be
      wired
    - the "Why Option 2" rationale is internally consistent
15. `docs/parity-phase0-ledger.md`:
    - `P0-DELIVERY-001` and `P0-DELIVERY-002` rows point at tests that
      actually exist and actually test the claimed behavior
    - `P0-PROTOCOL-004` row points at the flipped Phase 1.5 pin tests
16. `SPEC-AUDIT.md` rows `3.4`, `3.9`, `2.10`, `2.12`:
    - text no longer claims divergence that has actually been closed
    - cross-references point at tests that exist
    - rows that were *not* in scope for Phase 1.5 (e.g., `3.5`
      `CallReducer.flags`) are still open and still cite a pinned
      deferral test
17. `NEXT-SESSION-PROMPT.md`:
    - the "What landed last session" section matches what's in the code
    - the "Phase 1.5 deferrals still open" list is accurate — each named
      deferral corresponds to a zero/stub somewhere observable
    - the "Pinned parity tests" list cites tests that actually exist

### G. Tests really test what they claim

18. Open each test named in the pins and confirm it actually exercises
    the claimed contract, not a tautology:
    - `TestPhase15TransactionUpdateHeavyShape` — asserts field names
      exactly, in order
    - `TestPhase15TransactionUpdateLightShape` — same
    - `TestPhase15ReducerCallInfoShape` — same
    - `TestPhase15UpdateStatusVariants` — confirms all three variants
      satisfy the interface and have the expected field shapes
    - `TestPhase15TagReducerCallResultReserved` — confirms the decoder
      rejects the tag byte
    - `TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout` — confirms
      caller heavy delivery on empty `Fanout` map (this is the
      `P0-DELIVERY-002` lock)
    - `TestPhase0ParityCanonicalReducerDeliveryFlow` — confirms the full
      connect → subscribe → reducer → caller-heavy → non-caller-light
      flow under confirmed-read gating
19. The test `TestFanOutWorker_CallerDiversion_FailedStatus` verifies
    that a failed reducer still delivers a heavy envelope to the caller
    (not a silent drop).
20. Round-trip tests in `protocol/server_messages_test.go` cover all
    three `UpdateStatus` variants (Committed, Failed, OutOfEnergy).

### H. Broad verification

21. Run `rtk go test ./...` — expect all tests to pass. Record the
    actual passing count. The previous session's stated baseline was
    `940 passed in 9 packages`. Report any deviation.
22. Run `rtk go vet ./...` — record any new warnings introduced by the
    Phase 1.5 diff. Stale warnings are fine; new ones are not.
23. Run the focused Phase 1.5 suite:
    `rtk go test ./protocol -run TestPhase15` — expect 5 tests pass.
24. Run `rtk go test ./subscription -run 'TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout|TestFanOutWorker_CallerDiversion|TestPhase0ParityCanonicalReducerDeliveryFlow'`
    — expect 4 tests pass.

### I. Clean-room boundary

25. Confirm no new imports or references to
    `reference/SpacetimeDB/` in code. Confirm the reference was read for
    spec-derivation only. This is a repo rule in `CLAUDE.md`.

## Areas where the work is most likely to have a defect

Use these as focused stress-areas — if there's a bug, it's probably here:

- The `StatusCommitted` / `StatusFailed` / `StatusOutOfEnergy` interface
  satisfaction. Go interface satisfaction is nominal and silent — a
  struct that accidentally doesn't implement `isUpdateStatus()` will
  compile-fail but only where used. Grep for every place `UpdateStatus`
  is constructed.
- The BSATN encode/decode of `UpdateStatus` in
  `protocol/server_messages.go::writeUpdateStatus` / `readUpdateStatus`.
  Tag bytes are `0=Committed`, `1=Failed`, `2=OutOfEnergy`. Confirm the
  decoder rejects unknown tag bytes with `ErrMalformedMessage`.
- The eval-loop guard in `subscription/eval.go` uses `hasCaller` as a
  short-circuit. Confirm the boolean logic is `nothingToEvaluate &&
  !hasCaller` — inverting that would silently drop caller outcomes on
  empty-changeset.
- The `RequestID` propagation to non-caller light envelopes. The worker
  reads `msg.CallerOutcome.RequestID`. If `CallerOutcome` is nil
  (non-reducer commit), `lightRequestID` is zero. Confirm this is
  intentional and not a latent bug for non-reducer commits (e.g.,
  scheduled reducers / lifecycle reducers that also produce fanout).
- Caller-fanout-entry preservation: the worker must skip (not delete)
  the caller entry in `msg.Fanout` during iteration. Deletion would
  mutate shared state.
- `TxDurable` confirmed-read gating: the check uses
  `w.anyConfirmedRead(msg.Fanout, msg.CallerConnID, msg.CallerOutcome)`.
  Confirm the check still returns true for caller-only batches when the
  caller is configured as a confirmed-read recipient.

## What NOT to do

- Do not re-open `P0-PROTOCOL-00*` or `P0-DELIVERY-00*` slices. If you
  think one is wrong, report it — don't rewrite it.
- Do not re-litigate the Phase 1.5 outcome-model decision (Option 1 vs 2
  vs 3). The user chose Option 2 explicitly. If the implementation
  deviates from Option 2 as described in the decision doc, that's a
  finding. If you disagree with Option 2 itself, that's out of scope for
  this audit.
- Do not copy Rust code from `reference/SpacetimeDB/` into any file. The
  clean-room boundary is a hard rule.
- Do not pivot into SQL / query / scheduler / recovery work. Those are
  later phases.
- Do not run destructive git commands. If a rollback is warranted,
  propose it and stop.
- Do not modify `SPEC-AUDIT.md`, the decision doc, or the ledger unless
  the user accepts a finding that requires a doc edit — then make the
  minimum edit.

## Output format

Produce a single report with this structure:

```
## Phase 1.5 audit — findings

### Verdict
One of: ship-it / ship-with-fixes / block.

### Accepted (n)
- [short statement] — evidence: file:line or test name

### Requires fix (n)
- [short statement] — location: file:line — suggested fix

### Rejected (n)
- [short statement] — contradicting evidence: file:line

### Observations outside scope (optional)
Things that look off but are not Phase 1.5 deferrals or scope.
```

Keep the report tight — one-liner per finding when possible. The user
wants an audit, not a rewrite.

## Verification commands (copy-paste)

```bash
rtk go test ./...
rtk go test ./protocol -run TestPhase15 -v
rtk go test ./subscription -run 'TestFanOutWorker_CallerAlwaysReceivesHeavy_EmptyFanout|TestFanOutWorker_CallerDiversion|TestPhase0ParityCanonicalReducerDeliveryFlow' -v
rtk go vet ./...
rtk grep "ReducerCallResult" --type=go
rtk git log --stat -1 542c438
```

## Starting pointers

- Commit under audit: `542c438`.
- Decision doc: `docs/parity-phase1.5-outcome-model.md`.
- Handoff for the next session (which this audit gates): `NEXT-SESSION-PROMPT.md`.
- If a finding says "the decision doc is wrong", prefer fixing the doc
  over reopening the implementation. The implementation was shipped
  intentionally; the docs support it.
