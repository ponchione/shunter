# Story 5.2: Response Message Delivery

**Epic:** [Epic 5 — Server Message Delivery](EPIC.md)
**Spec ref:** SPEC-005 §8.2, §8.3, §8.4, §8.6
**Depends on:** Story 3.3, Story 5.1
**Blocks:** Story 5.3

---

## Summary

Deliver direct response messages: SubscribeSingleApplied / SubscribeMultiApplied, UnsubscribeSingleApplied / UnsubscribeMultiApplied, SubscriptionError, and OneOffQueryResult.

## Deliverables

- `func SendSubscribeSingleApplied(sender ClientSender, conn *Conn, msg *SubscribeSingleApplied) error` and `SendSubscribeMultiApplied`:
  - Verify the connection is still open; closed connections return `ErrConnNotFound`
  - Send via `sender.Send`

- `func SendUnsubscribeSingleApplied(sender ClientSender, conn *Conn, msg *UnsubscribeSingleApplied) error` and `SendUnsubscribeMultiApplied`:
  - Verify the connection is still open; closed connections return `ErrConnNotFound`
  - Send via `sender.Send`

- `func SendSubscriptionError(sender ClientSender, conn *Conn, msg *SubscriptionError) error`:
  - Send the wire error keyed by optional `query_id`; no protocol tracker release occurs
  - Send via `sender.Send`

- `func SendOneOffQueryResult(sender ClientSender, connID ConnectionID, msg *OneOffQueryResult) error`:
  - Send via `sender.Send` (no subscription state change)

## Acceptance Criteria

- [ ] SubscribeApplied: rows are the correct committed-state snapshot for that client `query_id` at registration time
- [ ] SubscribeApplied: initial rows encoded as RowList in message
- [ ] Disconnect before response delivery → send fails with `ErrConnNotFound` and does not resurrect protocol state
- [ ] UnsubscribeApplied with `send_dropped=1`: includes current rows
- [ ] UnsubscribeApplied with `send_dropped=0`: no rows
- [ ] UnsubscribeApplied: response enqueued without protocol tracker mutation
- [ ] SubscriptionError: optional `query_id` is encoded when known; manager state owns reuse
- [ ] SubscriptionError: `request_id` echoed when known; `0` only for genuinely uncorrelated spontaneous failures
- [ ] OneOffQueryResult success: rows present, error empty
- [ ] OneOffQueryResult error: error present, rows absent
- [ ] All messages delivered via ClientSender (buffered, non-blocking)

## Design Notes

- These are 1:1 responses (one client message → one server response). No fan-out or broadcast involved.
- `SubscribeApplied` contains a consistent snapshot of matching rows. The executor ensures this by running subscription registration within the commit serialization window (SPEC-003 §2.5).
- The pending-subscription discard rule from SPEC-005 §9.1 is enforced by the manager and by connection-scoped send failure handling; response delivery is a straight transport push and must not resurrect state after disconnect.
- `SubscriptionError` reports the optional client `query_id` when known. Reuse is controlled by the manager-owned `(ConnID, QueryID)` registry, not by protocol tracker cleanup.
