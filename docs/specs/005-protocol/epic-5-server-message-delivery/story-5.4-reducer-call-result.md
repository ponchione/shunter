# Story 5.4: ReducerCallResult with Caller-Delta Diversion

**Epic:** [Epic 5 — Server Message Delivery](EPIC.md)
**Spec ref:** SPEC-005 §8.7, §13
**Depends on:** Story 5.1, Story 5.3 (integrates with TransactionUpdate pipeline)
**Blocks:** Nothing (terminal story in this epic)

**Cross-spec:** SPEC-003 (`ReducerResponse`, `CallReducerCmd.Request.Caller.ConnectionID`), SPEC-004 (`CommitFanout`)

---

## Summary

When a reducer commits, the calling client receives its subscription deltas embedded in `ReducerCallResult` instead of as a separate `TransactionUpdate`. This guarantees atomicity: the client sees the reducer outcome and its effects in one message.

## Deliverables

- Caller-delta diversion logic:
  1. When `CommitFanout` is produced for a commit originating from `CallReducer`:
     - Identify the caller's `ConnectionID`
     - Extract the caller's `[]SubscriptionUpdate` from the fanout
     - Package it into `ReducerCallResult.TransactionUpdate`
     - Remove the caller's entry from `CommitFanout` before standalone delivery (Story 5.3)
  2. Send `ReducerCallResult` via `ClientSender.SendReducerResult`
  3. Other connections receive standalone `TransactionUpdate` as normal

- `ReducerCallResult` construction:
  ```go
  result := &ReducerCallResult{
      RequestID:         cmd.RequestID,
      Status:            status,    // 0=committed, 1=failed_user, 2=failed_panic, 3=not_found
      TxID:              txID,      // 0 if no commit
      Error:             errMsg,
      Energy:            0,         // reserved v1
      TransactionUpdate: callerUpdates, // empty if status != 0
  }
  ```

- Rules enforcement:
  - `status != 0` → `TransactionUpdate` MUST be empty
  - `status == 0`, no active matching subscriptions → `TransactionUpdate` MUST be empty
  - Caller MUST NOT receive separate `TransactionUpdate` for same `tx_id`

## Acceptance Criteria

- [ ] Committed reducer → caller gets `ReducerCallResult` with embedded updates
- [ ] Caller does NOT receive standalone `TransactionUpdate` for same tx_id
- [ ] Other clients still receive standalone `TransactionUpdate`
- [ ] Failed reducer (status=1) → empty embedded TransactionUpdate
- [ ] Panicked reducer (status=2) → empty embedded TransactionUpdate
- [ ] Not-found reducer (status=3) → empty embedded TransactionUpdate, TxID=0
- [ ] Committed reducer, caller has no matching subscriptions → empty embedded TransactionUpdate, no separate TransactionUpdate
- [ ] `RequestID` echoed from original `CallReducerMsg`
- [ ] `Energy` always 0 in v1
- [ ] Two clients call reducers concurrently: each gets own ReducerCallResult, other gets TransactionUpdate

## Design Notes

- The diversion happens in the fan-out integration layer. The fan-out worker (SPEC-004) produces `CommitFanout` without knowledge of which connection is the caller. The protocol layer identifies the caller via `CallReducerCmd.Request.Caller.ConnectionID` captured at dispatch time and diverts before standalone delivery.
- This is the most complex message delivery path: it coordinates between the executor response channel (for status/error), the fan-out pipeline (for deltas), and the client sender (for wire delivery).
- If the caller disconnects between submitting `CallReducer` and receiving the result, the result is discarded. The reducer still commits (if it succeeded), and other clients still get their `TransactionUpdate`.
