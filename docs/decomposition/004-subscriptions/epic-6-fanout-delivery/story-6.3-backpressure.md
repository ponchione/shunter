# Story 6.3: Client Backpressure

**Epic:** [Epic 6 — Fan-Out & Delivery](EPIC.md)
**Spec ref:** SPEC-004 §8.4, §8.5
**Depends on:** Stories 6.1, 6.2 (worker + per-connection delivery), SPEC-005 (buffer-full error contract)
**Blocks:** Nothing

---

## Summary

Protocol-layer send remains non-blocking from the subscription subsystem's perspective. Buffer full → disconnect client (v1 policy). Signal dropped clients to executor for subscription cleanup.

## Deliverables

- Protocol send through SPEC-005 sender contract:
  ```go
  err := sender.SendTransactionUpdate(connID, &txUpdate)
  if errors.Is(err, ErrClientBufferFull) {
      markDropped(connID)
  } else if err != nil {
      return err
  }
  ```

- `markDropped(connID ConnectionID)`:
  - Remove client delivery metadata from fan-out worker state
  - Close/disable further delivery for that connection via protocol layer
  - Send connID to `dropped` channel (non-blocking; if channel full, log warning)

- Dropped client flow:
  1. Fan-out marks client as dropped, sends to `dropped` channel
  2. Executor drains `DroppedClients()` channel after each post-commit pipeline step
  3. Executor calls `SubscriptionManager.DisconnectClient(connID)` (Story 4.4)

- Client outbound buffer size: bounded, configurable via SPEC-005. Default: defined by protocol layer.

## Acceptance Criteria

- [ ] Client with space in buffer → TransactionUpdate delivered
- [ ] Client with full buffer → disconnected, not blocked
- [ ] Disconnected client → connID on DroppedClients channel
- [ ] Executor drains DroppedClients → subscriptions cleaned up
- [ ] Fan-out goroutine never blocks on a slow client
- [ ] Multiple slow clients → each disconnected independently
- [ ] Dropped channel full → log warning, don't block fan-out

## Design Notes

- v1 policy: disconnect-on-lag. Simpler than drop-with-resync. Clients must handle reconnection anyway (network failures). The client reconnects, re-subscribes, and gets fresh initial state.
- v2 optimization: drop updates + “resync required” flag on next successful send. Not implemented in v1.
- The `dropped` channel has its own bound. If the executor is slow to drain, the fan-out worker logs a warning but does not block. In pathological cases, clients may be disconnected but their subscriptions not cleaned up until the executor catches up. This is safe — stale subscriptions just produce unused deltas until cleaned.
- Two-phase cleanup avoids the fan-out goroutine needing write access to subscription manager state (§8.5).
- SPEC-005 owns the actual outbound queue and the `ErrClientBufferFull` signal. This story owns the subscription-side response to that signal.
