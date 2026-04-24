# Story 4.3: Unsubscribe & CallReducer Handlers

**Epic:** [Epic 4 — Client Message Dispatch](EPIC.md)
**Spec ref:** SPEC-005 §7.2, §7.3
**Depends on:** Story 4.1
**Blocks:** Epic 5 (response delivery)

**Cross-spec:** SPEC-003 (executor inbox: `UnregisterSubscriptionSetCmd`, `CallReducerCmd`)

> **Updated 2026-04-24 (OI-002 QueryID cleanup).** Unsubscribe is keyed by
> client `QueryID` / wire `query_id`. The protocol layer no longer owns a
> subscription tracker; manager-internal `SubscriptionID` values stay below
> the protocol boundary.

---

## Summary

Two handlers grouped together because they share the same E4 shape: validate locally, submit to the executor, and return immediately. E5 owns the accepted-command response watchers.

## Deliverables

### Unsubscribe

- `func handleUnsubscribeSingle(conn *Conn, msg *UnsubscribeSingleMsg, executor ExecutorInbox)` and the matching multi-query path:
  1. Forward `ConnID`, client `QueryID`, and `RequestID` to `UnregisterSubscriptionSetCmd`
  2. Let the subscription manager validate that the `(ConnID, QueryID)` set is live, returning `ErrSubscriptionNotFound` when absent
  3. Deliver `UnsubscribeSingleApplied` / `UnsubscribeMultiApplied` or `SubscriptionError` through the response callback; no protocol tracker is mutated

### CallReducer

- `func handleCallReducer(conn *Conn, msg *CallReducerMsg, executor ExecutorInbox)`:
  1. Reject lifecycle reducer names (`"OnConnect"`, `"OnDisconnect"`) → send `ReducerCallResult` with `status = not_found` and `ErrLifecycleReducer` error message
  2. Send `CallReducerRequest` to executor inbox with `RequestID`, raw args, and `ResponseCh` for later result delivery
  3. On submission failure: send `ReducerCallResult` with `status=3` and error message

- Reuse/surface `ErrLifecycleReducer` from SPEC-003 when the client attempts to invoke a lifecycle reducer directly

## Acceptance Criteria

- [ ] Unsubscribe active `query_id` → `UnregisterSubscriptionSetCmd` sent
- [ ] Unsubscribe pending or not-yet-registered `query_id` → `ErrSubscriptionNotFound`
- [ ] Unsubscribe unknown `query_id` → `ErrSubscriptionNotFound`
- [ ] CallReducer valid name → `CallReducerCmd` sent to executor
- [ ] CallReducer `"OnConnect"` → `ReducerCallResult` with `status=3` (not_found)
- [ ] CallReducer `"OnDisconnect"` → `ReducerCallResult` with `status=3` (not_found)
- [ ] CallReducer unknown reducer → executor returns `ErrReducerNotFound`, result delivered with `status=3`

## Design Notes

- Unsubscribe for a missing/pending `query_id` returns `ErrSubscriptionNotFound`; the manager-owned `(ConnID, QueryID)` registry is the single source of truth, so there is no duplicate protocol-side tracker removal.
- Lifecycle reducers are blocked at the protocol layer, not the executor. The executor should never receive a `CallReducerCmd` for `OnConnect`/`OnDisconnect`.
- `CallReducerMsg.Args` is forwarded as raw bytes. Type validation happens in the executor.
