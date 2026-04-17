# Story 5.1: EvalTransaction Algorithm

**Epic:** [Epic 5 — Evaluation Loop](EPIC.md)
**Spec ref:** SPEC-004 §7.1, §7.2, §10.1
**Depends on:** Epic 2 (PruningIndexes), Epic 3 (DeltaView, delta computation), Epic 4 (queryRegistry), Story 4.5 (manager contract), SPEC-001 (CommittedReadView), SPEC-003/SPEC-001 (`*Changeset`)
**Blocks:** Stories 5.2, 5.3, 5.4, Epic 6

---

## Summary

The main algorithm called after every committed transaction. Orchestrates DeltaView construction, candidate collection, per-query evaluation, and fanout assembly.

## Deliverables

- `EvalAndBroadcast(txID TxID, changeset *Changeset, view CommittedReadView, meta PostCommitMeta)`
  - Called synchronously on executor goroutine (§7.1)
  - Changeset is read-only — must not mutate
  - `meta` carries `TxDurable`, `CallerConnID`, `CallerResult` (§10.1)

- Algorithm (per §7.2):
  ```
  1. If no active subscriptions → return immediately
  2. Build DeltaView from changeset + committed state
     Build delta indexes for columns referenced by active subscriptions
  3. Collect candidate query hashes (delegate to Story 5.2)
  4. For each candidate query hash:
     a. Look up queryState
     b. Determine if single-table or join
     c. Call appropriate delta evaluator (Epic 3)
     d. If delta is empty → skip
     e. For each subscriber: append SubscriptionUpdate to CommitFanout[connID]
  5. Send FanOutMessage{TxDurable, Fanout} to fan-out worker inbox
  ```

- Early exit: `len(queryRegistry.byHash) == 0` → return immediately

- Fanout assembly: `CommitFanout map[ConnectionID][]SubscriptionUpdate`

- Error handling per subscription (§11.1): if delta evaluation fails, log error, send `SubscriptionError` to affected clients, unregister the affected subscription(s), and continue. Do not abort loop — other subscriptions unaffected.

## Acceptance Criteria

- [ ] No active subscriptions → returns immediately, no DeltaView built
- [ ] Single-table subscription with matching changeset → correct delta in fanout
- [ ] Join subscription → 8-fragment evaluation, dedup, correct delta
- [ ] Empty delta (no matching rows) → subscription not in fanout
- [ ] Multiple subscriptions affected → all appear in fanout
- [ ] Same query, two clients → delta computed once, both clients in fanout
- [ ] Evaluation error for one subscription → others still evaluated
- [ ] Evaluation error logs query hash plus predicate/query representation
- [ ] Evaluation error sends `SubscriptionError` to all clients subscribed to that query
- [ ] Evaluation error unregisters the affected subscription(s) without disconnecting unrelated subscriptions on the same connection
- [ ] FanOutMessage sent to fan-out worker inbox
- [ ] Changeset not mutated

## Design Notes

- Runs on executor goroutine — no concurrency concerns for manager state access.
- The DeltaView is built once and shared across all candidate evaluations.
- Step 5 sends to a channel. If the fan-out worker is slow, this blocks the executor. That's by design (§8.1) — the executor waits only for channel admission, not for actual delivery.
- `activeIndexes` for DeltaView construction: scan all active queries, collect the set of (table, index) pairs they reference. This is O(active queries) but runs once per transaction.
- Respect SPEC-001 snapshot discipline: the supplied `CommittedReadView` must be used for in-process evaluation only and must not be retained into fan-out or durability waits.
