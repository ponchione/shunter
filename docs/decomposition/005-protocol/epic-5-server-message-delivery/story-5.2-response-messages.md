# Story 5.2: Response Message Delivery

**Epic:** [Epic 5 — Server Message Delivery](EPIC.md)
**Spec ref:** SPEC-005 §8.2, §8.3, §8.4, §8.6
**Depends on:** Story 3.3, Story 5.1
**Blocks:** Story 5.3

---

## Summary

Deliver the four response messages that are direct replies to client requests: SubscribeApplied, UnsubscribeApplied, SubscriptionError, OneOffQueryResult.

## Deliverables

- `func SendSubscribeApplied(sender ClientSender, connID ConnectionID, msg *SubscribeApplied) error`:
  - Verify the connection is still open and the `subscription_id` is still pending; if the connection closed or the subscription was already discarded, drop the result
  - Activate subscription in tracker (pending → active) immediately before successful delivery commitment
  - Send via `sender.Send`

- `func SendUnsubscribeApplied(sender ClientSender, connID ConnectionID, msg *UnsubscribeApplied) error`:
  - Remove subscription from tracker
  - Send via `sender.Send`

- `func SendSubscriptionError(sender ClientSender, connID ConnectionID, msg *SubscriptionError) error`:
  - Release subscription_id from tracker (available for reuse)
  - Send via `sender.Send`

- `func SendOneOffQueryResult(sender ClientSender, connID ConnectionID, msg *OneOffQueryResult) error`:
  - Send via `sender.Send` (no subscription state change)

## Acceptance Criteria

- [ ] SubscribeApplied: subscription transitions pending → active
- [ ] SubscribeApplied: rows are the correct committed-state snapshot for that subscription at registration time
- [ ] SubscribeApplied: initial rows encoded as RowList in message
- [ ] Disconnect while subscription is pending → later SubscribeApplied result is discarded and the subscription never becomes active
- [ ] UnsubscribeApplied with `send_dropped=1`: includes current rows
- [ ] UnsubscribeApplied with `send_dropped=0`: no rows
- [ ] UnsubscribeApplied: subscription removed from tracker
- [ ] SubscriptionError: subscription_id released, immediately reusable
- [ ] SubscriptionError: `request_id` echoed when known; `0` only for genuinely uncorrelated spontaneous failures
- [ ] OneOffQueryResult success: rows present, error empty
- [ ] OneOffQueryResult error: error present, rows absent
- [ ] All messages delivered via ClientSender (buffered, non-blocking)

## Design Notes

- These are 1:1 responses (one client message → one server response). No fan-out or broadcast involved.
- `SubscribeApplied` contains a consistent snapshot of matching rows. The executor ensures this by running subscription registration within the commit serialization window (SPEC-003 §2.5).
- The pending-subscription discard rule from SPEC-005 §9.1 is enforced here at the activation point: a late registration result must not resurrect a subscription after disconnect or explicit unsubscribe while still pending.
- `SubscriptionError` makes the `subscription_id` immediately reusable. The client can send a new `Subscribe` with the same ID right away. Tracker cleanup is idempotent: if disconnect or pending-discard already released the ID, response handling must treat the second removal as a no-op rather than a new state transition.
