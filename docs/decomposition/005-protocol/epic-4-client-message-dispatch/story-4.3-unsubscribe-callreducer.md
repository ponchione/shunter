# Story 4.3: Unsubscribe & CallReducer Handlers

**Epic:** [Epic 4 — Client Message Dispatch](EPIC.md)
**Spec ref:** SPEC-005 §7.2, §7.3
**Depends on:** Story 4.1
**Blocks:** Epic 5 (response delivery)

**Cross-spec:** SPEC-003 (executor inbox: `UnregisterSubscriptionCmd`, `CallReducerCmd`)

---

## Summary

Two handlers grouped together because they share the pattern: validate, route to executor, await response.

## Deliverables

### Unsubscribe

- `func handleUnsubscribe(conn *Conn, msg *UnsubscribeMsg, executor ExecutorInbox)`:
  1. Validate `subscription_id` is active → `ErrSubscriptionNotFound` if pending or not found
  2. Send `UnregisterSubscriptionCmd` to executor using the SPEC-003 fields (`ConnID`, `SubscriptionID`, `ResponseCh`); preserve `send_dropped` in the protocol-layer response path
  3. On executor response: remove subscription from tracker, deliver `UnsubscribeApplied` (Epic 5)

### CallReducer

- `func handleCallReducer(conn *Conn, msg *CallReducerMsg, executor ExecutorInbox)`:
  1. Reject lifecycle reducer names (`"OnConnect"`, `"OnDisconnect"`) → send `ReducerCallResult` with `status = not_found` and `ErrLifecycleReducer` error message
  2. Send `CallReducerCmd` to executor inbox with `ResponseCh` for result delivery
  3. Executor sends `ReducerCallResult` back via `ResponseCh` → delivered to client (Epic 5)

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

- Unsubscribe for a pending subscription is rejected because SPEC-005 §9.1 defines `ErrSubscriptionNotFound` for any subscription_id that is not active. The client should wait for `SubscribeApplied` before unsubscribing. If the subscription fails, `SubscriptionError` will be sent and the ID released.
- Lifecycle reducers are blocked at the protocol layer, not the executor. The executor should never receive a `CallReducerCmd` for `OnConnect`/`OnDisconnect`.
- `CallReducerMsg.Args` is forwarded as raw bytes. Type validation happens in the executor.
