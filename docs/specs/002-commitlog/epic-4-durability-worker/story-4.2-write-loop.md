# Story 4.2: Write Loop (Batch-Then-Sync)

**Epic:** [Epic 4 — Durability Worker](EPIC.md)  
**Spec ref:** SPEC-002 §4.4  
**Depends on:** Story 4.1  
**Blocks:** Stories 4.3, 4.4

---

## Summary

The core goroutine loop: wait for items, drain a batch, encode+write, fsync, update durable TxID.

## Deliverables

- Write loop algorithm:
  1. Block on channel for at least one item (or shutdown signal)
  2. Non-blocking drain up to `DrainBatchSize` additional items
  3. For each item:
     - Encode Changeset to payload bytes (Epic 3)
     - Build Record (tx_id, RecordTypeChangeset, flags=0, payload)
     - Append to active segment via SegmentWriter
  4. Call `seg.Sync()` — single fsync for entire batch
  5. `atomic.Store(&durable, last_written_tx_id)`
  6. Loop

- `EnqueueCommitted` implementation:
  - Check `closing` and `fatalErr` — panic if either set
  - Validate txID > last enqueued txID
  - Send to channel (blocks on backpressure)

- `DurableTxID` implementation:
  - `return TxID(w.durable.Load())`

- `Close` implementation:
  - Set `closing = true`
  - Close channel (signals goroutine to drain remaining and exit)
  - Wait on `done` channel
  - Final fsync
  - Return last durable TxID + fatalErr

## Acceptance Criteria

- [ ] Send 1000 items, Close() → all 1000 on disk
- [ ] DurableTxID advances to last TxID in each fsynced batch
- [ ] Batch size respects DrainBatchSize limit
- [ ] Single fsync per batch (not per record)
- [ ] Channel full → EnqueueCommitted blocks until space available
- [ ] EnqueueCommitted with non-increasing txID → panic
- [ ] Close waits for drain, returns correct final TxID
- [ ] After Close, EnqueueCommitted panics

## Design Notes

- Batch-then-sync: one fsync per batch amortizes ~5ms disk cost. At 1000 TPS with batch=64, ~15 fsyncs/sec.
- Non-blocking drain uses `select { case item := <-ch: ... default: break }` pattern.
- EnqueueCommitted panics (not returns error) because a post-close or post-fatal enqueue is a programming error in the executor, not a recoverable condition.
