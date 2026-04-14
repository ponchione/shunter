# Epic 4: Durability Worker

**Parent:** [SPEC-002-commitlog.md](../SPEC-002-commitlog.md) §4  
**Blocked by:** Epic 2 (Segment Writer) for Story 4.1; Epic 3 (Changeset Encoder) joins at Story 4.2  
**Blocks:** Nothing (consumed by SPEC-003 executor)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 4.1 | [story-4.1-durability-handle.md](story-4.1-durability-handle.md) | DurabilityHandle interface and durabilityWorker struct |
| 4.2 | [story-4.2-write-loop.md](story-4.2-write-loop.md) | Batch drain, encode, write, fsync, update durable TxID |
| 4.3 | [story-4.3-segment-rotation.md](story-4.3-segment-rotation.md) | Rotate to new segment when size exceeds max or when recovery requires fresh-tail resume |
| 4.4 | [story-4.4-failure-handling.md](story-4.4-failure-handling.md) | Fatal error latch, panic on post-failure enqueue, Close semantics |

## Implementation Order

```
Story 4.1 (Interface + struct)
  └── Story 4.2 (Write loop)
        ├── Story 4.3 (Rotation)
        └── Story 4.4 (Failure handling)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 4.1 | `commitlog/durability.go` |
| 4.2–4.4 | `commitlog/durability_worker.go`, `commitlog/durability_worker_test.go` |
