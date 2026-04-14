# Story 4.4: Fatal Error Handling

**Epic:** [Epic 4 — Durability Worker](EPIC.md)  
**Spec ref:** SPEC-002 §4.6  
**Depends on:** Story 4.2  
**Blocks:** Nothing

---

## Summary

On any fatal encode/write/sync/rotate error: latch the error, stop accepting work, exit deterministically.

## Deliverables

- On fatal error in write loop:
  1. Store first error in `fatalErr` (atomic, only first error latched)
  2. Stop draining channel
  3. Close `done` channel (signal shutdown)
  4. Goroutine exits

- `EnqueueCommitted` post-fatal behavior:
  - Check `fatalErr` before sending — panic with `ErrDurabilityFailed` wrapping the latched error
  - MUST NOT silently drop items

- `DurableTxID` post-fatal behavior:
  - Continues returning last successfully fsynced TxID
  - Does NOT return 0 or error — it's the honest last-known-good point

- `Close` post-fatal behavior:
  - Returns last durable TxID + latched error
  - Does not attempt further writes or syncs

- `ErrDurabilityFailed` — sentinel error for the latched state

## Acceptance Criteria

- [ ] Inject write error → fatalErr latched
- [ ] Next EnqueueCommitted after fatal → panics
- [ ] DurableTxID after fatal → returns last good TxID (not 0)
- [ ] Close after fatal → returns (lastDurableTxID, latchedError)
- [ ] Only first fatal error stored — subsequent errors ignored
- [ ] Goroutine exits cleanly after fatal (no leak)
- [ ] Items queued but unwritten at fatal time are lost (acceptable — not yet durable)

## Design Notes

- Panic on post-fatal enqueue is intentional. The executor (SPEC-003) must treat this as terminal engine failure. Returning an error would require every callsite to handle it, and the engine can't recover anyway.
- "Items in channel at fatal time are lost" — these items were enqueued but not yet fsynced, so they were never durable. The executor's DurableTxID tells it exactly what survived.
