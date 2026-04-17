# Story 6.4: Confirmed Reads

**Epic:** [Epic 6 — Fan-Out & Delivery](EPIC.md)
**Spec ref:** SPEC-004 §8.2 (step 1), §12.3
**Depends on:** Stories 6.1, 6.2 (fan-out loop and caller/non-caller routing)
**Blocks:** Nothing

---

## Summary

Durability wait for public protocol delivery. In protocol v1, all WebSocket clients observe confirmed-read behavior because the wire protocol exposes no opt-in/opt-out flag.

## Deliverables

- `TxDurable` handling in fan-out loop:
  ```go
  if anyRecipientRequiresConfirmedReads(msg.Fanout, msg.CallerConnID, msg.CallerResult) {
      <-msg.TxDurable  // block until transaction is durable
  }
  ```

- Public protocol v1 rule: WebSocket clients always require confirmed reads. Per-connection policy tracking remains an internal extension point, not a negotiated wire feature.

- Optimization hook: internal/non-wire callers may still skip the wait when no recipient requires confirmed reads.

- `TxDurable` is executor-supplied post-commit metadata backed by the durability subsystem. The fan-out worker waits on readiness but does not depend directly on the exported SPEC-002 `DurabilityHandle` API.

## Acceptance Criteria

- [ ] Public protocol batch → wait for TxDurable before delivery
- [ ] TxDurable channel becomes ready → delivery proceeds
- [ ] TxDurable already ready (fast fsync) → no blocking
- [ ] Caller-only batch with empty fanout still waits when protocol delivery requires confirmed reads

## Design Notes

- Current design waits for durability before delivering to *any* protocol-visible recipient in the batch. This is simpler than splitting delivery into two phases.
- v2 optimization: add a wire-level confirmed-read flag (or a server policy knob) and split delivery for fast-read clients if the latency trade-off proves worthwhile.
- If the durability worker is behind (batching fsyncs), this wait time includes that latency. The fan-out goroutine is blocked but the executor is not — the executor already sent the FanOutMessage and moved on.
