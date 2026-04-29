# Story 6.2: Per-Connection TransactionUpdate Assembly

**Epic:** [Epic 6 — Fan-Out & Delivery](EPIC.md)
**Spec ref:** SPEC-004 §8.2, §8.3
**Depends on:** Story 6.1 (FanOutWorker loop), SPEC-005 (TransactionUpdate / ReducerCallResult delivery contract)
**Blocks:** Stories 6.3, 6.4

---

## Summary

Build one `TransactionUpdate` per non-caller connection from the pre-grouped `CommitFanout`. Preserve subscription boundaries — don't merge entries across SubscriptionIDs.

## Deliverables

- Assembly within fan-out loop:
  ```go
  for connID, updates := range msg.Fanout {
      txUpdate := TransactionUpdate{
          TxID:    txID,
          Updates: updates,  // []SubscriptionUpdate, preserved as-is
      }
      send(connID, txUpdate)
  }
  ```

- Subscription boundaries preserved:
  - Multiple `SubscriptionUpdate` entries for same table but different subscriptions → kept separate
  - Client receives one `TransactionUpdate` per transaction, containing all affected subscriptions

- Caller client special case (§8.2 step 4):
  - The client that invoked the reducer receives its update slice via `ReducerCallResult.transaction_update`
  - The caller MUST NOT also receive a standalone `TransactionUpdate` for the same `tx_id`

## Acceptance Criteria

- [ ] Single subscription affected → TransactionUpdate with 1 entry
- [ ] Two subscriptions on same table affected → 2 entries, not merged
- [ ] Three subscriptions on different tables → 3 entries in one TransactionUpdate
- [ ] Caller client receives reducer result with embedded `transaction_update` instead of standalone TransactionUpdate
- [ ] Non-caller clients receive ordinary standalone `TransactionUpdate` delivery
- [ ] Empty fanout for a connection → no TransactionUpdate sent (shouldn't be in CommitFanout)

## Design Notes

- The evaluation loop (Epic 5) pre-groups by ConnectionID. The fan-out worker iterates that grouping directly — no re-sorting needed.
- `ReducerCallResult` delivery shape is defined by SPEC-005. This story follows that contract by routing the caller's update slice into the reducer result rather than inventing an optional `ReducerResult` field on `TransactionUpdate`.
- Wire encoding and enqueueing happen through the protocol sender contract defined by SPEC-005.
