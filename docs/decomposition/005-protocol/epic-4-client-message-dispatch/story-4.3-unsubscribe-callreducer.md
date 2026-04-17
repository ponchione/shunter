# Story 4.3: Unsubscribe & CallReducer Handlers

**Epic:** [Epic 4 — Client Message Dispatch](EPIC.md)
**Spec ref:** SPEC-005 §7.2, §7.3
**Depends on:** Story 4.1
**Blocks:** Epic 5 (response delivery)

**Cross-spec:** SPEC-003 (executor inbox: `UnregisterSubscriptionCmd`, `CallReducerCmd`)

---

## Summary

Two handlers grouped together because they share the same E4 shape: validate locally, submit to the executor, and return immediately. E5 owns the accepted-command response watchers.

## Deliverables

### Unsubscribe

- `func handleUnsubscribe(conn *Conn, msg *UnsubscribeMsg, executor ExecutorInbox)`:
  1. Validate `subscription_id` is active → `ErrSubscriptionNotFound` if pending or not found
  2. Send `UnregisterSubscriptionRequest` to the executor using the SPEC-003 fields (`ConnID`, `SubscriptionID`, `RequestID`, `SendDropped`, `ResponseCh`)
  3. On executor submission success: remove subscription from tracker immediately; E5 watches the response channel and delivers `UnsubscribeApplied` / `SubscriptionError`

### CallReducer

- `func handleCallReducer(conn *Conn, msg *CallReducerMsg, executor ExecutorInbox)`:
  1. Reject lifecycle reducer names (`"OnConnect"`, `"OnDisconnect"`) → send `ReducerCallResult` with `status = not_found` and `ErrLifecycleReducer` error message
  2. Send `CallReducerRequest` to executor inbox with `RequestID`, raw args, and `ResponseCh` for later result delivery
  3. On submission failure: send `ReducerCallResult` with `status=3` and error message

- Reuse/surface `ErrLifecycleReducer` from SPEC-003 when the client attempts to invoke a lifecycle reducer directly

## Acceptance Criteria

- [ ] Unsubscribe active subscription → `UnregisterSubscriptionCmd` sent, subscription removed from tracker
- [ ] Unsubscribe pending subscription → `ErrSubscriptionNotFound` (cannot unsubscribe what has not become active)
- [ ] Unsubscribe unknown subscription_id → `ErrSubscriptionNotFound`
- [ ] CallReducer valid name → `CallReducerCmd` sent to executor
- [ ] CallReducer `"OnConnect"` → `ReducerCallResult` with `status=3` (not_found)
- [ ] CallReducer `"OnDisconnect"` → `ReducerCallResult` with `status=3` (not_found)
- [ ] CallReducer unknown reducer → executor returns `ErrReducerNotFound`, result delivered with `status=3`

## Design Notes

- Unsubscribe for a pending subscription is rejected because SPEC-005 §9.1 defines `ErrSubscriptionNotFound` for any subscription_id that is not active. The client should wait for `SubscribeApplied` before unsubscribing. If the subscription fails, `SubscriptionError` will be sent and the ID released. Tracker removal is single-owner: unsubscribe removes active entries, while pending-path cleanup is handled by error/disconnect discard logic rather than a second blind delete.
- Lifecycle reducers are blocked at the protocol layer, not the executor. The executor should never receive a `CallReducerCmd` for `OnConnect`/`OnDisconnect`.
- `CallReducerMsg.Args` is forwarded as raw bytes. Type validation happens in the executor.
