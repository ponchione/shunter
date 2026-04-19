# Call-reducer seam handoff

## Objective

The protocol-originated call-reducer committed-response seam is now corrected and verified. The next work agent should treat this slice as implementation-complete and only use it as a stable base for adjacent protocol/executor work, not reopen its core design.

## Reading order

1. `executor/protocol_inbox_adapter.go`
2. `executor/executor.go` (post-commit path)
3. `protocol/fanout_adapter.go`
4. `subscription/eval.go`
5. `subscription/fanout.go`
6. `executor/protocol_inbox_adapter_test.go`
7. `executor/pipeline_test.go`
8. `protocol/lifecycle.go`

## What is locked in

- Protocol-originated committed reducer replies have one caller-visible owner: the protocol inbox adapter.
- Committed heavy envelopes are assembled through the shared helper:
  - `protocol.BuildTransactionUpdateHeavy(...)`
- The authoritative caller-visible update slice comes from subscription evaluation fanout, not raw changeset reconstruction.
- `NoSuccessNotify` suppresses committed success delivery on the protocol-originated path.
- When suppression happens, the adapter closes the response channel so the protocol watcher exits cleanly.
- The fanout path still owns heavy caller delivery for non-protocol external callers.

## Do not change

- Do not widen generic `ReducerResponse` with protocol-specific payloads.
- Do not move protocol wire structs deeper into executor internals.
- Do not reintroduce dual ownership of committed caller-heavy delivery.
- Do not reconstruct caller-visible committed updates from changesets instead of evaluator fanout.

## Current file surface changed in this stream

Primary implementation/test files:
- `executor/command.go`
- `executor/executor.go`
- `executor/lifecycle.go`
- `executor/pipeline_test.go`
- `executor/protocol_inbox_adapter.go`
- `executor/protocol_inbox_adapter_test.go`
- `protocol/fanout_adapter.go`
- `protocol/lifecycle.go`
- `subscription/eval.go`
- `subscription/eval_test.go`
- `subscription/fanout.go`

Also note unrelated dirty files already in worktree before/alongside this slice:
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_unsubscribe_multi.go`
- `protocol/handle_unsubscribe_single.go`
- `protocol/variant_dispatch_test.go`

## Verification already completed

Focused:
- `rtk go test ./executor -run 'ProtocolInboxAdapter|PostCommit'`
- `rtk go test ./protocol -run 'CallReducer|TransactionUpdate'`
- `rtk go test ./subscription -run 'Eval|Fanout'`

Broader:
- `rtk go test ./executor ./subscription ./protocol`

Last known result:
- all passing
- `623` tests passed across executor/subscription/protocol

## Acceptance summary for this slice

Confirmed true now:
- committed protocol-originated reducer call returns a heavy `TransactionUpdate`
- `StatusCommitted.Update` carries the real caller-visible delta
- committed path uses shared heavy-envelope assembly helper
- duplicate committed caller-heavy delivery is prevented
- `NoSuccessNotify` suppresses committed success correctly
- suppression does not leak the watcher goroutine/channel path
- failure still returns a heavy failure envelope
- empty/no-active-subscription committed path still works

## If the next agent touches adjacent work

Safe adjacent areas:
- further protocol message-family parity work
- subscription/fanout delivery audits outside this seam
- lifecycle/protocol contract tightening

Required proof before changing this seam again:
- show a failing targeted test first
- explain why the current single-owner model is insufficient
- preserve shared heavy-envelope assembly
- preserve evaluator-derived caller update source

## Stop/escalate criteria

Stop and escalate if a proposed change would:
- require widening `ReducerResponse`
- reintroduce caller-heavy delivery from both adapter and fanout paths
- bypass evaluator-derived caller updates
- alter `NoSuccessNotify` semantics without new parity/spec evidence

## Bottom line

This stream of work is ready to hand off. I would not spend another agent cycle reworking the seam itself unless a new failing parity/spec test appears. The right next use of an implementation agent is adjacent work, with this seam treated as a fixed base.
