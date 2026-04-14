# Epic 7: Log Compaction

**Parent:** [SPEC-002-commitlog.md](../SPEC-002-commitlog.md) §7  
**Blocked by:** Story 6.1 (segment metadata), Epic 5 (Snapshot completion)  
**Blocks:** Nothing

---

## Stories

| Story | File | Summary |
|---|---|---|
| 7.1 | [story-7.1-segment-coverage.md](story-7.1-segment-coverage.md) | Compute [min_tx, max_tx] coverage per sealed segment from recovery-produced SegmentInfo |
| 7.2 | [story-7.2-compaction.md](story-7.2-compaction.md) | Delete segments fully covered by snapshot, retain boundary segments |

## Implementation Order

```
Story 7.1 (Coverage analysis)
  └── Story 7.2 (Compaction)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 7.1–7.2 | `commitlog/compaction.go`, `commitlog/compaction_test.go` |
