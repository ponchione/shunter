# Story 4.4: Client Disconnect (Batch Unsubscribe)

**Epic:** [Epic 4 — Subscription Manager](EPIC.md)
**Spec ref:** SPEC-004 §4.3
**Depends on:** Story 4.3 (Unregister)
**Blocks:** Epic 6 (Fan-Out — signals dropped clients)

---

## Summary

When a client connection drops, remove all its subscriptions in one batch. Equivalent to calling Unregister for each, but avoids redundant index lookups.

## Deliverables

- `DisconnectClient(connID ConnectionID) error`

- Steps:
  1. Look up all subscriptions for `connID` via `queryRegistry.subscriptionsForConn`
  2. For each subscription: call `Unregister` logic (Story 4.3)
  3. Clean up `byConn[connID]` entry
  4. Return nil (or error if connID unknown — non-fatal, just log)

## Acceptance Criteria

- [ ] Client with 3 subscriptions → all 3 removed after disconnect
- [ ] Shared query (2 clients): disconnect one → query state alive for other
- [ ] Shared query (2 clients): disconnect both → query state fully removed
- [ ] Unknown connID → no error (idempotent), log warning
- [ ] After disconnect, no residual entries in byConn or `(connID, subID)` reverse lookups for that connection
- [ ] Pruning indexes cleaned for queries that lost their last subscriber

## Design Notes

- Batching optimization: `subscriptionsForConn` returns the full list, then each unregister runs. For clients with many subscriptions, this could be optimized to batch pruning index removals. Deferred — correctness first.
- DisconnectClient is called from two paths: (1) executor draining `DroppedClients()` channel, (2) explicit client disconnect command. Both converge here.
