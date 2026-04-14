# Story 5.3: TransactionUpdate Delivery

**Epic:** [Epic 5 — Server Message Delivery](EPIC.md)
**Spec ref:** SPEC-005 §8.5, §9.2–§9.4, §13
**Depends on:** Story 3.3, Story 5.1, Story 5.2 (subscription must be active before updates sent)
**Blocks:** Story 5.4 (caller-delta diversion integrates with this pipeline)

**Cross-spec:** SPEC-004 §7 (`CommitFanout`), §8 (`FanOutWorker`, `FanOutMessage`, `TransactionUpdate`), §10.2 (`SubscriptionUpdate`)

---

## Summary

After each committed transaction, deliver per-connection `TransactionUpdate` messages assembled from the subscription evaluator's grouped per-connection updates.

## Deliverables

- Integration with fan-out worker (SPEC-004 §8):
  1. Receive per-connection transaction deltas from the fan-out worker after it groups `CommitFanout` entries and associates them with the committed `TxID`
  2. For each connection: build or accept `TransactionUpdate{TxID, Updates}` using the grouped `[]SubscriptionUpdate`
  3. Send via `ClientSender.SendTransactionUpdate`

- Per-connection ordering guarantee enforcement:
  - `TransactionUpdate` referencing a `subscription_id` MUST NOT be sent before `SubscribeApplied` for that ID
  - Implementation: check subscription state is `SubActive` before including that subscription's update; if the state is not active, treat that as a pipeline invariant violation and do not silently drop the delta

- Delta semantics (informational — produced by SPEC-004, not constructed here):
  - `inserts`: rows newly entering the subscription result set
  - `deletes`: rows leaving the subscription result set
  - Row update where old+new both match → `delete(old)` + `insert(new)`
  - Row entering predicate → `insert` only
  - Row leaving predicate → `delete` only
  - Same-row insert+delete in one tx (net-zero) → omitted entirely

## Acceptance Criteria

- [ ] CommitFanout with one connection → TransactionUpdate delivered to that connection
- [ ] CommitFanout with N connections → N TransactionUpdates delivered
- [ ] Connection not in ConnManager (disconnected since evaluation) → skipped, no error
- [ ] TransactionUpdate contains correct TxID
- [ ] Multiple SubscriptionUpdates per connection preserved in single TransactionUpdate
- [ ] Insert rows via committed reducer → matching rows appear in `TransactionUpdate.inserts`
- [ ] Delete rows via committed reducer → matching rows appear in `TransactionUpdate.deletes`
- [ ] Row update where old and new both match predicate → `delete(old)` and `insert(new)` both appear
- [ ] Row update where the row enters the predicate → `insert` only
- [ ] Row update where the row leaves the predicate → `delete` only
- [ ] Insert+delete of the same row in one transaction → no row appears in delivered updates
- [ ] SubscribeApplied for subscription X always delivered before any TransactionUpdate containing X
- [ ] Empty update for a connection (no matching subscriptions in this commit) → no message sent
- [ ] `ErrClientBufferFull` from send → trigger client disconnect

## Design Notes

- The protocol layer does not compute deltas. It receives pre-computed `CommitFanout` from SPEC-004's fan-out worker and translates entries into wire messages.
- The `TxDurable` channel in `FanOutMessage` supports confirmed-reads clients (SPEC-004 §8.4). For v1, the fan-out worker handles the wait. The protocol layer just sends what it receives.
- The protocol layer still verifies delivered wire semantics even though delta computation originates in SPEC-004. These acceptance tests are end-to-end protocol guarantees, not a second implementation of the evaluator.
- The ordering guarantee (SubscribeApplied before TransactionUpdate) is naturally satisfied if the executor serializes subscription registration with commits. The check here is defensive and must not silently drop required deltas.
