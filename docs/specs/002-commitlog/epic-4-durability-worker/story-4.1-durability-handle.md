# Story 4.1: DurabilityHandle Interface + Worker Struct

**Epic:** [Epic 4 — Durability Worker](EPIC.md)  
**Spec ref:** SPEC-002 §4.2, §4.3  
**Depends on:** Epic 2 (SegmentWriter)  
**Blocks:** Stories 4.2, 4.3, 4.4

---

## Summary

The public interface and internal struct for the async durability goroutine.

## Deliverables

- `DurabilityHandle` interface:
  ```go
  type DurabilityHandle interface {
      EnqueueCommitted(txID TxID, changeset *Changeset)
      DurableTxID() TxID
      WaitUntilDurable(txID TxID) <-chan TxID
      Close() (TxID, error)
  }
  ```

- `durabilityItem` struct:
  ```go
  type durabilityItem struct {
      txID      TxID
      changeset *Changeset
  }
  ```

- `durabilityWorker` struct:
  ```go
  type durabilityWorker struct {
      ch       chan durabilityItem
      durable  atomic.Uint64
      fatalErr atomic.Pointer[error]
      closing  atomic.Bool
      done     chan struct{}
      opts     CommitLogOptions
      dir      string
      seg      *SegmentWriter
  }
  ```

- `func NewDurabilityWorker(dir string, startTxID TxID, opts CommitLogOptions) (*durabilityWorker, error)`
  - Creates or opens active segment
  - Launches goroutine
  - Returns handle implementing DurabilityHandle

- `CommitLogOptions` struct (from §8):
  ```go
  type CommitLogOptions struct {
      MaxSegmentSize        int64   // default 512 MiB
      MaxRecordPayloadBytes uint32  // default 64 MiB
      MaxRowBytes           uint32  // default 8 MiB
      ChannelCapacity       int     // default 256
      DrainBatchSize        int     // default 64
      SnapshotInterval      uint64  // default 0 (no auto-snapshot)
  }
  ```

- `func DefaultCommitLogOptions() CommitLogOptions`

## Acceptance Criteria

- [ ] NewDurabilityWorker creates segment file in correct directory
- [ ] Returned handle satisfies DurabilityHandle interface
- [ ] Channel created with configured capacity
- [ ] DurableTxID initially returns 0 (or startTxID - 1 if resuming)
- [ ] WaitUntilDurable(0) returns nil; WaitUntilDurable(txID>0) returns a non-nil channel
- [ ] DefaultCommitLogOptions returns spec-documented defaults
- [ ] Worker goroutine is running after construction

## Design Notes

- `atomic.Uint64` for durable TxID: executor reads from its own goroutine, worker writes from durability goroutine. Atomic avoids mutex for this single integer.
- Channel capacity 256 = absorb bursts without unbounded memory growth.
