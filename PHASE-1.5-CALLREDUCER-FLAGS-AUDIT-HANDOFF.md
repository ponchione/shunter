# Audit handoff — Phase 1.5 `CallReducer.flags` / `NoSuccessNotify` sub-slice

## Mission

Independently confirm that the Phase 1.5 `CallReducer.flags` sub-slice
landed in commits `d286a8c`, `a66e262`, `302ede5`, `6715290` is sound,
internally consistent, and honestly reflected in the docs. Report
findings as a punch list of accepted / rejected / requires-fix items.
Do not rewrite the implementation. Do not re-open any closed parity
slice. If a claim is right, say so; if a claim is wrong, point at the
file and line that proves it.

You are an independent reviewer. Assume nothing about what "the
previous session claimed" beyond the artifacts in the repo. Verify by
reading code and tests, not by trusting prose. Trust but verify: a doc
saying "X is pinned by test Y" is a claim, not a proof — open test Y
and check.

## Scope

Commits under audit (four, in order):

1. `d286a8c` — `protocol(parity): CallReducer.flags wire byte — Phase 1.5 sub-slice begin`
2. `a66e262` — `protocol/executor(parity): thread CallReducer.Flags to CallerOutcome`
3. `302ede5` — `subscription(parity): suppress caller heavy on NoSuccessNotify + committed`
4. `6715290` — `docs(parity): close Phase 1.5 CallReducer.flags sub-slice`

Previous session's stated intent:

- Add a `Flags byte` trailing field to the client `CallReducerMsg` wire
  shape, matching reference `CallReducerFlags` (`FullUpdate = 0`,
  `NoSuccessNotify = 1`). Encoder appends a single u8 after `Args`;
  decoder reads it and rejects out-of-range values with
  `ErrMalformedMessage`.
- Propagate the flag end-to-end: `protocol.CallReducerRequest.Flags` →
  `executor.ReducerRequest.Flags` → `executor.postCommitOptions.callerFlags`
  → `subscription.CallerOutcome.Flags`.
- In `subscription/fanout_worker.go::deliver`, suppress the caller's
  heavy `TransactionUpdate` envelope when
  `CallerOutcome.Kind == CallerOutcomeCommitted` AND
  `CallerOutcome.Flags == CallerOutcomeFlagNoSuccessNotify`. Failure and
  out-of-energy outcomes are never suppressed. Confirmed-read gating
  treats the caller as absent when suppressed.
- Update `SPEC-AUDIT.md §3.6`, `docs/parity-phase0-ledger.md`
  `P0-PROTOCOL-004` row, and `NEXT-SESSION-PROMPT.md` to reflect
  closure. Flip `TestPhase1DeferralCallReducerNoFlagsField` into the
  positive-shape pin `TestPhase15CallReducerFlagsField`.

Broad verification claim: `948 passed in 9 packages`. `rtk go vet ./...`
clean.

## Required reading (in order)

1. `RTK.md` — mandatory shell / git wrapper rules. Use RTK for every
   shell and git command you run.
2. `CLAUDE.md` — repo rules and reading order.
3. `docs/project-brief.md` — product intent.
4. `docs/spacetimedb-parity-roadmap.md` — development driver. Phase 1.5
   §4 Slice 3 is the surrounding scope; this sub-slice closes the
   last client-facing wire deferral in that slice.
5. `docs/parity-phase0-ledger.md` — the `P0-*` scenario ledger. Note
   that the `P0-PROTOCOL-004` row text was updated in this sub-slice.
6. `docs/parity-phase1.5-outcome-model.md` — context for the surrounding
   Phase 1.5 envelope split; referenced by the new worker suppression
   logic.
7. `SPEC-AUDIT.md` — row `3.6` was flipped from `[DIVERGE] TRACKED` to
   `[RESOLVED]`.
8. `NEXT-SESSION-PROMPT.md` — handoff for the *next* session. Verify it
   matches the actual state of the repo.
9. `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`
   — the `CallReducer` struct and `CallReducerFlags` enum (around
   lines 115–153). Treat as read-only research input. Do NOT copy Rust
   source into implementation (clean-room rule in `CLAUDE.md`).
10. `reference/SpacetimeDB/crates/core/src/client/client_connection.rs`
    around lines 842–849 — the reference behavior that translates
    `CallReducerFlags::NoSuccessNotify` into `sender = None`, which
    downstream causes `eval_updates` to skip caller delivery entirely.
    This is the semantic Shunter is matching.
11. `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs`
    around lines 1955–1972 — the v1 send path showing that a `Some(caller)`
    always produces a heavy envelope (possibly with empty updates) and
    `None` skips it. This is the referent for the Shunter
    `effCallerConnID` / `effCallerOutcome` suppression pattern.

## Primary source files to inspect

Protocol layer:

- `protocol/client_messages.go` — `CallReducerMsg.Flags` field,
  `CallReducerFlagsFullUpdate` / `CallReducerFlagsNoSuccessNotify`
  constants, `EncodeClientMessage` trailing byte write,
  `decodeCallReducer` trailing byte read + out-of-range rejection.
- `protocol/client_messages_test.go` — `TestCallReducerRoundTrip`
  (extended), `TestCallReducerFlagsNoSuccessNotifyRoundTrip`,
  `TestCallReducerFlagsInvalidByteRejected`. Also confirm
  `TestCallReducerEmptyArgs` still passes.
- `protocol/parity_message_family_test.go` —
  `TestPhase15CallReducerFlagsField` (renamed from
  `TestPhase1DeferralCallReducerNoFlagsField`).
- `protocol/lifecycle.go` — `CallReducerRequest.Flags` field.
- `protocol/handle_callreducer.go` — `msg.Flags` forwarded into the
  `CallReducerRequest` handed to the executor inbox.
- `protocol/handle_callreducer_test.go` —
  `TestHandleCallReducer_ForwardsFlags_NoSuccessNotify`.

Executor layer:

- `executor/reducer.go` — `ReducerRequest.Flags` field.
- `executor/executor.go` — `postCommitOptions.callerFlags`, assignment
  inside the `CallSourceExternal` branch, propagation onto
  `subscription.CallerOutcome.Flags`.
- `executor/pipeline_test.go` — `TestPostCommitPropagatesCallerFlags`.

Subscription layer:

- `subscription/fanout.go` — `CallerOutcome.Flags` field,
  `CallerOutcomeFlagFullUpdate` / `CallerOutcomeFlagNoSuccessNotify`
  constants.
- `subscription/fanout_worker.go::deliver` — `callerSuppressed`
  computation, `effCallerConnID` / `effCallerOutcome` substitution,
  gating + heavy-dispatch guards.
- `subscription/fanout_worker_test.go` — the four
  `TestFanOutWorker_NoSuccessNotify_*` tests:
  `SuppressesCallerHeavy_OnCommitted`, `EmptyFanout_NoDelivery`,
  `DoesNotSuppressOnFailed`, `DoesNotSuppressOnOutOfEnergy`.

Docs touched:

- `SPEC-AUDIT.md` row `3.6`.
- `docs/parity-phase0-ledger.md` row `P0-PROTOCOL-004` + the
  "Recommended next implementation slice" section.
- `NEXT-SESSION-PROMPT.md` — primary decision, landed-work section,
  pinned-tests list, deferrals list, reading list.

## Verification checklist

Work through each item. For each, state one of: `accepted` (with a
pointer to the evidence), `rejected` (with the file/line that disproves
the claim), or `requires-fix` (with a short description of what's wrong
and where).

### A. Wire shape matches the reference

1. `CallReducerMsg` field order is exactly `{RequestID, ReducerName,
   Args, Flags}`. Pinned by `TestPhase15CallReducerFlagsField`.
2. The encoder writes `Flags` as a single trailing byte after `Args`
   (length-prefixed `writeBytes` for `Args`, then `buf.WriteByte(msg.Flags)`).
   Confirm by reading `EncodeClientMessage` in
   `protocol/client_messages.go`.
3. The decoder reads `Flags` as a single trailing byte and validates
   it against `{CallReducerFlagsFullUpdate, CallReducerFlagsNoSuccessNotify}`,
   returning `ErrMalformedMessage` for any other value (reference
   `impl_deserialize!` behavior: "invalid call reducer flag"). Pinned
   by `TestCallReducerFlagsInvalidByteRejected`.
4. Constants are defined as package-level `byte` values with the
   correct numeric assignments: `FullUpdate = 0`,
   `NoSuccessNotify = 1`. Reference:
   `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`
   `pub enum CallReducerFlags` and `impl_serialize!` / `impl_deserialize!`.
5. A short-frame (truncated flags byte) is rejected, not treated as
   flags=0. Confirm the decoder returns `ErrMalformedMessage` when
   `len(body)-off < 1` after `Args`.

### B. Flag propagation across seams

6. `protocol.CallReducerRequest` has a `Flags byte` field between
   `Args` and `ResponseCh`. Confirm via `rtk grep "Flags" protocol/lifecycle.go`.
7. `protocol/handle_callreducer.go::handleCallReducer` forwards
   `msg.Flags` into the `CallReducerRequest` passed to
   `executor.CallReducer`. Pinned by
   `TestHandleCallReducer_ForwardsFlags_NoSuccessNotify`. Confirm no
   clobbering or accidental zeroing.
8. `executor.ReducerRequest` has a `Flags byte` field. Confirm via
   `rtk grep "Flags" executor/reducer.go`.
9. `executor.postCommitOptions.callerFlags` is populated only on the
   `CallSourceExternal` branch (the same branch that sets
   `callerConnID` and `callerRequestID`). Confirm scheduled /
   lifecycle calls do NOT accidentally set the flag via leaked
   defaults.
10. `subscription.CallerOutcome.Flags` is populated from
    `opts.callerFlags` inside `executor.postCommit`, alongside the
    existing `Kind` / `RequestID` fields. Pinned by
    `TestPostCommitPropagatesCallerFlags`.
11. `subscription.CallerOutcomeFlagFullUpdate` /
    `CallerOutcomeFlagNoSuccessNotify` are defined and numerically
    equal to their wire counterparts (0 and 1). Confirm via
    `rtk grep "CallerOutcomeFlag" subscription/fanout.go`.
12. No import cycle was introduced: `subscription` must not import
    `protocol`, and `executor` must not import `protocol`. The flag
    constants are duplicated deliberately in `subscription` to keep
    the fan-out worker independent of the protocol package. Verify
    via `rtk grep "ponchione/shunter/protocol" subscription executor`.

### C. Worker suppression semantics

13. `subscription/fanout_worker.go::deliver` computes
    `callerSuppressed := msg.CallerConnID != nil && msg.CallerOutcome != nil &&
    msg.CallerOutcome.Kind == CallerOutcomeCommitted &&
    msg.CallerOutcome.Flags == CallerOutcomeFlagNoSuccessNotify`.
    Verify the boolean logic is correct — specifically that the `Kind`
    check is `== CallerOutcomeCommitted` (not `!= CallerOutcomeFailed`,
    which would accidentally suppress out-of-energy).
14. When `callerSuppressed` is true, `effCallerConnID` and
    `effCallerOutcome` are set to `nil`. These `eff*` values are used
    for:
    - `anyConfirmedRead` gating — caller is treated as absent so a
      caller-only suppressed batch does not block on `TxDurable` for a
      delivery that will not happen.
    - the final `SendTransactionUpdateHeavy` guard — caller heavy is
      skipped.
15. Non-caller light delivery is NOT affected by the suppression: the
    existing loop uses `msg.CallerConnID` (not `effCallerConnID`) to
    skip the caller's entry, so non-callers with row-touches still
    receive their light envelope. Pinned by
    `TestFanOutWorker_NoSuccessNotify_SuppressesCallerHeavy_OnCommitted`
    where `other` gets a `lightCall`.
16. Failure (`CallerOutcomeFailed`) and out-of-energy
    (`CallerOutcomeOutOfEnergy`) outcomes are never suppressed, even
    with the flag set. Pinned by
    `TestFanOutWorker_NoSuccessNotify_DoesNotSuppressOnFailed` and
    `TestFanOutWorker_NoSuccessNotify_DoesNotSuppressOnOutOfEnergy`.
17. The `msg.Fanout` map is still not mutated by `deliver` — the
    caller entry is skipped via `continue`, not `delete`. Confirm by
    reading the loop and by the existing
    `TestFanOutWorker_DeliverDoesNotMutateFanout` still passing.

### D. Pre-acceptance rejection path unaffected

18. `protocol/handle_callreducer.go::sendSyntheticFailure` does NOT go
    through the fan-out seam — it sends a heavy `TransactionUpdate`
    with `StatusFailed` directly via `connOnlySender`. Therefore the
    `NoSuccessNotify` flag cannot suppress pre-acceptance rejections
    (lifecycle-reducer-name collision, executor-unavailable). This is
    correct per the "failure always delivered" invariant. Confirm
    there is no flag-check branch inside `handleCallReducer` that
    would skip the synthetic failure.

### E. Tests really test what they claim

19. `TestPhase15CallReducerFlagsField` uses `reflect.DeepEqual` on the
    field name list and the expected list is exactly
    `{"RequestID", "ReducerName", "Args", "Flags"}`. The previous
    deferral test name `TestPhase1DeferralCallReducerNoFlagsField` is
    gone (replaced, not just commented out). Confirm via
    `rtk grep "TestPhase1DeferralCallReducerNoFlagsField"` — should
    only hit the `.hermes/plans/` historical copy, not live code.
20. `TestCallReducerFlagsNoSuccessNotifyRoundTrip` asserts the
    trailing byte of the encoded frame equals `NoSuccessNotify` AND
    the decoded message has `Flags == NoSuccessNotify`. Verify it
    does not tautologically round-trip `0`.
21. `TestCallReducerFlagsInvalidByteRejected` mutates the trailing
    byte of an already-valid frame to a value outside `{0, 1}` and
    asserts `errors.Is(err, ErrMalformedMessage)`. Confirm the test
    really mutates the flags byte (trailing byte of the frame) and
    not some other byte that happens to trip a different decode
    error.
22. `TestHandleCallReducer_ForwardsFlags_NoSuccessNotify` asserts
    `exec.callReducerReq.Flags == CallReducerFlagsNoSuccessNotify`.
    Verify the mock captures the `CallReducerRequest` by value-copy,
    so a later mutation in production code would not pollute the
    observation.
23. `TestPostCommitPropagatesCallerFlags` submits a `CallReducerCmd`
    with `ReducerRequest.Flags = 1` and asserts
    `meta.CallerOutcome.Flags == 1`. Verify the harness's subs mock
    captures the `PostCommitMeta` by value, not by reference.
24. `TestFanOutWorker_NoSuccessNotify_SuppressesCallerHeavy_OnCommitted`
    verifies `heavyCalls == 0` AND `lightCalls == 1` (non-caller
    gets light). Verify both assertions.
25. `TestFanOutWorker_NoSuccessNotify_EmptyFanout_NoDelivery` verifies
    no delivery at all when caller suppressed and fanout is empty.
    Verify it runs long enough for the worker to have processed the
    message (50ms sleep is the existing pattern).
26. `TestFanOutWorker_NoSuccessNotify_DoesNotSuppressOnFailed` and
    `TestFanOutWorker_NoSuccessNotify_DoesNotSuppressOnOutOfEnergy`
    each construct a `CallerOutcome` with `Flags = NoSuccessNotify`
    but a non-`Committed` `Kind`, and assert the heavy envelope IS
    delivered.

### F. Doc honesty

27. `SPEC-AUDIT.md §3.6` is now `[RESOLVED]` with a pointer to
    `TestPhase15CallReducerFlagsField` and the full pin list. The
    prior `[DIVERGE]` / `TRACKED` text is gone. No stale claim of
    divergence remains.
28. `docs/parity-phase0-ledger.md` `P0-PROTOCOL-004` row's "Next slice
    note" reflects the closure: `CallReducer.flags` is listed as the
    closed sub-slice; the remaining deferrals are `SubscribeMulti` /
    `QueryId` (Phase 2 Slice 2) and SQL `OneOffQuery` (Phase 2 Slice 1).
29. `docs/parity-phase0-ledger.md` "Recommended next implementation
    slice" no longer recommends `CallReducer.flags`; instead it lists
    the caller-metadata follow-ups. Verify no leftover "next sub-slice
    is `CallReducer.flags`" language exists anywhere in the repo.
    Candidate grep: `rtk grep "next.*CallReducer\.flags\|next.*NoSuccessNotify"`.
30. `NEXT-SESSION-PROMPT.md`:
    - The "Primary decision" section reflects `CallReducer.flags`
      closure.
    - The "Pinned parity tests" list shows `TestPhase15CallReducerFlagsField`
      (not the old deferral name).
    - The "Phase 1.5 deferrals still open" list no longer contains
      `CallReducer.flags` as item 1; subsequent items are renumbered.
    - The "Recently landed" subsection accurately describes the wire,
      threading, and suppression behavior.
    - The "Primary source files to inspect when wiring
      `CallReducer.flags`" section has been removed (the work is done).
31. `PHASE-1.5-AUDIT-HANDOFF.md` (the prior slice's handoff) remains
    untracked / unchanged. The new handoff for THIS sub-slice is
    `PHASE-1.5-CALLREDUCER-FLAGS-AUDIT-HANDOFF.md` (this file).

### G. Broad verification

32. Run `rtk go test ./... -count=1` — expect all tests to pass and
    the pass count to be at least `948 passed in 9 packages`. Record
    any deviation. A higher count is fine if new adjacent tests
    landed incidentally; a lower count is a finding.
33. Run `rtk go vet ./...` — expect no issues. Record any new
    warnings introduced by this sub-slice.
34. Run the focused flag suite:

    ```bash
    rtk go test ./protocol -run 'TestPhase15CallReducerFlagsField|TestCallReducerFlagsNoSuccessNotifyRoundTrip|TestCallReducerFlagsInvalidByteRejected|TestHandleCallReducer_ForwardsFlags_NoSuccessNotify' -v
    rtk go test ./executor -run 'TestPostCommitPropagatesCallerFlags' -v
    rtk go test ./subscription -run 'TestFanOutWorker_NoSuccessNotify' -v
    ```

    Expect 4 + 1 + 4 = 9 tests to pass.

### H. Clean-room boundary

35. Confirm no new imports or references to `reference/SpacetimeDB/`
    in production code. The reference was read for spec derivation
    only (flag values, semantics). This is a repo rule in `CLAUDE.md`.
    Candidate grep: `rtk grep "reference/SpacetimeDB" -- '*.go'`.

## Areas where the work is most likely to have a defect

Use these as focused stress-areas — if there's a bug, it's probably here:

- The `callerSuppressed` boolean in `fanout_worker.go::deliver`. An
  inversion (`!= CallerOutcomeCommitted` instead of `==`) would silently
  suppress the wrong outcomes. Likewise, using `Flags != 0` instead of
  `Flags == CallerOutcomeFlagNoSuccessNotify` would leak future flag
  values into suppression.
- The `effCallerConnID` / `effCallerOutcome` substitution feeding
  `anyConfirmedRead`. If gating still reads `msg.CallerConnID` directly,
  a caller-only suppressed batch could hang on `TxDurable` forever
  waiting for a delivery that never happens. Confirm by reading the
  `if msg.TxDurable != nil && w.anyConfirmedRead(msg.Fanout, effCallerConnID, effCallerOutcome)`
  line.
- The non-caller light loop uses `msg.CallerConnID` (not
  `effCallerConnID`) to skip the caller's fanout entry. This is
  intentional: the caller's row-touches are still embedded in
  `msg.Fanout` and must not be delivered as a non-caller light to the
  caller. Confirm the loop still uses `msg.CallerConnID`, not the
  suppressed `effCallerConnID`, for the skip check.
- The decoder's out-of-range rejection path must not consume more bytes
  than it read. `len(body) - off < 1` guards the read; the final
  `switch` only validates the read byte. Confirm the error message
  carries the actual invalid byte value so debugging is possible.
- Struct field addition ordering in `CallReducerMsg`. The positive-shape
  pin `TestPhase15CallReducerFlagsField` uses `reflect.DeepEqual` on
  an ordered field list — swapping `Flags` ahead of `Args` would break
  the pin and diverge from the expected trailing-byte encoding.
- `executor.postCommitOptions.callerFlags` is only set in the
  `CallSourceExternal` branch, but it is a field on the shared struct.
  A future bug where someone copies the struct without zeroing could
  leak flags from a prior call. Confirm `opts` is declared locally
  per-call (`var opts postCommitOptions`) and not reused across calls.
- `CallReducerFlagsFullUpdate = 0` is the zero value. Any struct that
  zero-initializes `Flags` (scheduled calls, lifecycle calls, test
  fixtures) will carry `FullUpdate` semantics. Confirm this is
  compatible with the "always-full-update" default and that no code
  path treats a `Flags == 0` caller specially.

## What NOT to do

- Do not re-open `P0-PROTOCOL-00*` or `P0-DELIVERY-00*` slices. If you
  think one is wrong, report it — don't rewrite it.
- Do not re-litigate the Phase 1.5 outcome-model decision or the
  surrounding envelope split. The decision is recorded in
  `docs/parity-phase1.5-outcome-model.md`.
- Do not copy Rust code from `reference/SpacetimeDB/` into any file.
  The clean-room boundary is a hard rule.
- Do not widen this audit into the remaining Phase 1.5 caller-metadata
  follow-ups (`CallerIdentity`, `ReducerID`, `ReducerName`, `Args`,
  `Timestamp`, `EnergyQuantaUsed`, `TotalHostExecutionDuration`).
  Those are separate, independently landable slices.
- Do not pivot into Phase 2 (SQL `OneOffQuery`, `SubscribeMulti`),
  Phase 3 (runtime parity, energy model), or recovery work.
- Do not run destructive git commands. If a rollback is warranted,
  propose it and stop.
- Do not modify `SPEC-AUDIT.md`, the decision doc, or the ledger unless
  the user accepts a finding that requires a doc edit — then make the
  minimum edit.

## Output format

Produce a single report with this structure:

```
## Phase 1.5 CallReducer.flags audit — findings

### Verdict
One of: ship-it / ship-with-fixes / block.

### Accepted (n)
- [short statement] — evidence: file:line or test name

### Requires fix (n)
- [short statement] — location: file:line — suggested fix

### Rejected (n)
- [short statement] — contradicting evidence: file:line

### Observations outside scope (optional)
Things that look off but are not this sub-slice's scope.
```

Keep the report tight — one-liner per finding when possible. The user
wants an audit, not a rewrite.

## Verification commands (copy-paste)

```bash
rtk git log --stat d286a8c..6715290
rtk go test ./... -count=1
rtk go test ./protocol -run 'TestPhase15CallReducerFlagsField|TestCallReducerFlagsNoSuccessNotifyRoundTrip|TestCallReducerFlagsInvalidByteRejected|TestHandleCallReducer_ForwardsFlags_NoSuccessNotify' -v
rtk go test ./executor -run 'TestPostCommitPropagatesCallerFlags' -v
rtk go test ./subscription -run 'TestFanOutWorker_NoSuccessNotify' -v
rtk go vet ./...
rtk grep "TestPhase1DeferralCallReducerNoFlagsField"
rtk grep "CallerOutcomeFlag" subscription executor protocol
rtk grep "callerSuppressed\|effCallerConnID\|effCallerOutcome" subscription
rtk grep "NoSuccessfulUpdate" .
```

The last grep is a honesty check — the old pre-sub-slice docs
occasionally used the spelling `NoSuccessfulUpdate` (descriptive name)
alongside `NoSuccessNotify` (reference enum name). After closure, the
repo should prefer the reference spelling `NoSuccessNotify`. Stale
`NoSuccessfulUpdate` mentions in doc prose are not a blocker but are
worth flagging.

## Starting pointers

- Commits under audit: `d286a8c` (wire), `a66e262` (threading),
  `302ede5` (suppression), `6715290` (docs).
- Reference flag enum:
  `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`
  around lines 131–153.
- Reference sender-None semantics:
  `reference/SpacetimeDB/crates/core/src/client/client_connection.rs`
  around lines 842–849.
- Reference caller-send invariant:
  `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs`
  around lines 1955–1972.
- Prior audit handoff (envelope split) for comparison tone:
  `PHASE-1.5-AUDIT-HANDOFF.md`.
- Handoff for the next session (which this audit gates):
  `NEXT-SESSION-PROMPT.md`.
- If a finding says "the decision doc is wrong", prefer fixing the doc
  over reopening the implementation. The implementation was shipped
  intentionally; the docs support it.
