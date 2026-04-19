# Review/fix plan for call-reducer committed-response seam

## Goal

Do a narrow review pass on the recently landed protocol↔executor committed-response seam, fix any grounded issues found, and verify the corrected behavior with focused and broader tests.

## Current grounded findings

After reviewing the changed files and tracing the committed external reducer path, two real issues stand out:

1. Duplicate-delivery ownership risk
   - `executor/executor.go` still exports `CallerConnID` + `CallerOutcome` into the subscription/fanout path for external committed calls.
   - `executor/protocol_inbox_adapter.go` also directly forwards a committed heavy `TransactionUpdate` back to the protocol caller.
   - That creates two possible owners for the same caller-visible committed success envelope.

2. `NoSuccessNotify` bypass on the direct adapter path
   - The fanout worker already suppresses caller success delivery when `CallerOutcome.Flags == CallerOutcomeFlagNoSuccessNotify`.
   - The new direct adapter forwarding path currently sends committed heavy success unconditionally.
   - So the committed success echo can still leak through for protocol-originated calls with `NoSuccessNotify`.

Also missing are tests that pin:
- exactly-one committed caller delivery on the protocol-originated path
- committed `NoSuccessNotify` suppression on that path
- failure delivery still occurring under `NoSuccessNotify`

## Approach

Keep the slice narrow and consistent with the current architecture:
- Let the protocol inbox adapter own protocol-originated `CallReducer` reply delivery.
- Keep shared committed heavy-envelope assembly centralized in `protocol.BuildTransactionUpdateHeavy(...)`.
- Stop exporting caller-heavy success metadata into the fanout path for protocol-originated reducer calls so the caller reply has a single owner.
- Preserve non-caller light fanout behavior.
- Preserve failure-path reply behavior.

## Step-by-step plan

### Step 1 — Lock the intended behavior in tests first

Add/adjust tests for:
- protocol-originated committed success is delivered once
- protocol-originated committed success is suppressed under `NoSuccessNotify`
- failure still returns a heavy failure envelope even when `NoSuccessNotify` is set

Likely files:
- `executor/protocol_inbox_adapter_test.go`
- possibly `protocol/handle_callreducer_test.go`
- possibly `executor/pipeline_test.go`

### Step 2 — Remove duplicate committed-caller ownership

Adjust the executor post-commit path so protocol-originated direct replies do not also export committed caller-heavy delivery through the subscription fanout seam.

Likely shape:
- keep authoritative caller update capture in post-commit for the adapter reply object
- stop setting `meta.CallerConnID` / `meta.CallerOutcome` for the path where the protocol adapter itself owns the caller-visible reply
- keep non-caller subscription fanout intact

Primary file:
- `executor/executor.go`

### Step 3 — Honor `NoSuccessNotify` on the direct reply path

Ensure the adapter does not send committed success when the request flags opt out.

Primary file:
- `executor/protocol_inbox_adapter.go`

### Step 4 — Tighten comments/contracts

Update any comments that still imply the fanout path is the owner for protocol-originated committed caller replies if that is no longer true after Step 2.

Likely files:
- `protocol/lifecycle.go`
- `subscription/fanout.go`
- `executor/executor.go`

### Step 5 — Verification

Run at minimum:
- `rtk go test ./executor -run 'ProtocolInboxAdapter|PostCommit'`
- `rtk go test ./protocol -run 'CallReducer|TransactionUpdate'`
- `rtk go test ./subscription -run 'Fanout|Eval'`
- `rtk go test ./executor ./subscription ./protocol`

## Files likely to change

Most likely:
- `executor/executor.go`
- `executor/protocol_inbox_adapter.go`
- `executor/protocol_inbox_adapter_test.go`

Possibly:
- `executor/pipeline_test.go`
- `protocol/handle_callreducer_test.go`
- `protocol/lifecycle.go`
- `subscription/fanout.go`

## Risks / tradeoffs

- The main risk is changing ownership incorrectly and accidentally dropping the caller-visible committed response entirely.
- The safe fix is to make one path authoritative, then pin it with tests before and after.
- This should remain narrow: no redesign of generic executor response types, no broad protocol lifecycle changes.

## Definition of done

This pass is complete when:
- protocol-originated committed reducer calls have exactly one caller-visible owner
- committed success honors `NoSuccessNotify`
- failure paths still return one heavy failure envelope
- tests pin the ownership/suppression behavior
- focused and broader package tests pass
