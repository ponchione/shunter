# Story 4.3: Subscription Deregistration

**Epic:** [Epic 4 — Subscription Manager](EPIC.md)
**Spec ref:** SPEC-004 §4.2
**Depends on:** Story 4.2 (Register — creates state that unregister removes)
**Blocks:** Story 4.4 (Disconnect — batch unregister)

---

## Summary

Remove a client from a subscription. Clean up query state and pruning indexes when last subscriber leaves.

## Deliverables

- `Unregister(connID ConnectionID, subscriptionID SubscriptionID) error`

- Steps (per §4.2):
  1. **Remove** client from subscriber set via `queryRegistry.removeSubscriber`
  2. If **lastSubscriber=true**:
     - Remove from pruning indexes via `RemoveSubscription` (Story 2.4)
     - Remove query state via `queryRegistry.removeQueryState`
  3. Return nil (or `ErrSubscriptionNotFound` if subID unknown)

- No final delta sent in v1. SPEC-004 §4.2 makes this configurable per client; that hook is intentionally deferred and must be documented as a v1 narrowing rather than treated as unspecified behavior.

## Acceptance Criteria

- [ ] Unregister one of two subscribers → query state alive, other subscriber unaffected
- [ ] Unregister last subscriber → query state removed, pruning indexes cleaned
- [ ] Unregister unknown subscriptionID → `ErrSubscriptionNotFound`
- [ ] After unregister, subscription no longer appears in candidate collection
- [ ] Ref count decremented correctly
- [ ] byConn and bySub reverse lookups cleaned up
- [ ] v1 does not emit the optional final delta on unsubscribe; behavior is explicitly deferred rather than silently omitted

## Design Notes

- "Optional final delta" (§4.2) is deferred to v2. Simpler to skip for now — clients handle reconnection anyway.
- Unregister is idempotent from the pruning index perspective: if the subscription was already removed (e.g., by `DisconnectClient`), the index removal is a no-op.
- No lock needed — runs on executor goroutine.
