# Story 6.4: Confirmed Reads

**Epic:** [Epic 6 — Fan-Out & Delivery](EPIC.md)
**Spec ref:** SPEC-004 §8.2 (step 1), §12.3
**Depends on:** Stories 6.1, 6.2 (fan-out loop and caller/non-caller routing)
**Blocks:** Nothing

---

## Summary

Optional per-client durability wait. Confirmed-read clients see data only after fsync. Fast-read clients see data immediately.

## Deliverables

- `TxDurable` handling in fan-out loop:
  ```go
  if anyClientRequiresConfirmedReads(msg.Fanout) {
      <-msg.TxDurable  // block until transaction is durable
  }
  ```

- Per-connection confirmed-read policy tracked by fan-out worker metadata

- Optimization: only wait for `TxDurable` if at least one client in this fanout batch requires confirmed reads. If all clients are fast-read, skip the wait entirely.

- `TxDurable` is executor-supplied post-commit metadata backed by the durability subsystem. The fan-out worker waits on readiness but does not depend directly on the exported SPEC-002 `DurabilityHandle` API.

## Acceptance Criteria

- [ ] All fast-read clients → no TxDurable wait, immediate delivery
- [ ] One confirmed-read client in batch → wait for TxDurable before delivering to all
- [ ] TxDurable channel becomes ready → delivery proceeds
- [ ] TxDurable already ready (fast fsync) → no blocking
- [ ] Mix of confirmed and fast clients → both receive after durability wait

## Design Notes

- Current design waits for durability before delivering to *any* client in the batch if *any* client requires confirmed reads. This is simpler than splitting delivery into two phases (fast clients first, confirmed clients after durability).
- v2 optimization: split delivery — send to fast-read clients immediately, queue confirmed-read clients until TxDurable fires. Adds complexity for marginal latency benefit.
- Default recommendation (§12.3): fast reads. Confirmed reads are opt-in per client.
- If the durability worker is behind (batching fsyncs), this wait time includes that latency. The fan-out goroutine is blocked but the executor is not — the executor already sent the FanOutMessage and moved on.
