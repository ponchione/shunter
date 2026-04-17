# Story 4.1: Query State & Subscriber Tracking

**Epic:** [Epic 4 — Subscription Manager](EPIC.md)
**Spec ref:** SPEC-004 §4.1 (steps 3–4), §7.4
**Depends on:** Epic 1 (QueryHash, Predicate)
**Blocks:** Story 4.2

---

## Summary

Internal bookkeeping for active subscriptions. Maps query hashes to executable query state and subscriber sets. Enables deduplication: identical predicates share one evaluation.

## Deliverables

- `queryState` struct:
  ```go
  type queryState struct {
      hash        QueryHash
      predicate   Predicate   // v1 executable plan: the validated predicate itself
      subscribers map[ConnectionID]map[SubscriptionID]struct{}
      refCount    int
  }
  ```

- `queryRegistry` struct:
  ```go
  type queryRegistry struct {
      byHash   map[QueryHash]*queryState
      bySub    map[SubscriptionID]QueryHash       // reverse lookup: sub → query
      byConn   map[ConnectionID][]SubscriptionID  // all subs for a connection
  }
  ```

- `newQueryRegistry() *queryRegistry`

- `addSubscriber(hash QueryHash, connID ConnectionID, subID SubscriptionID)` — add client to existing query state, increment ref count

- `removeSubscriber(connID ConnectionID, subID SubscriptionID) (hash QueryHash, lastSubscriber bool)` — remove client, decrement ref count, return whether this was the last subscriber

- `createQueryState(hash QueryHash, pred Predicate) *queryState` — create new query state entry

- `removeQueryState(hash QueryHash)` — remove query state and all index entries

- `getQuery(hash QueryHash) *queryState` — lookup

- `subscriptionsForConn(connID ConnectionID) []SubscriptionID` — all active subs for a connection

## Acceptance Criteria

- [ ] Create query state, add subscriber → ref count 1
- [ ] Add second subscriber to same hash → ref count 2, same queryState
- [ ] Remove one subscriber → ref count 1, lastSubscriber=false
- [ ] Remove last subscriber → ref count 0, lastSubscriber=true
- [ ] Reverse lookup: subID → queryHash works after add
- [ ] Reverse lookup cleaned up after remove
- [ ] byConn: lists all subs for a connection
- [ ] byConn cleaned up when all subs for connection removed
- [ ] getQuery for nonexistent hash → nil

## Design Notes

- Three maps maintain O(1) lookups in all directions: hash→state, sub→hash, conn→subs. The cost is keeping them in sync on every add/remove.
- A connection may own multiple subscriptions that deduplicate to the same `QueryHash`, so `subscribers` is a nested set rather than a single `ConnectionID -> SubscriptionID` map.
- `refCount` counts total subscriptions, not distinct connections. It is redundant with the total cardinality of the nested subscriber sets, but kept explicit for clarity and to avoid map iteration just to check emptiness.
- No separate compiled-plan type in v1 — “compiled plan” is just the validated predicate itself. Story 4.2 still owns the registration-time compile step/order contract, but that compile step currently records the predicate rather than producing a distinct plan object.
- Thread safety: the manager runs on the executor goroutine (single-threaded). No mutex needed on these structures.
