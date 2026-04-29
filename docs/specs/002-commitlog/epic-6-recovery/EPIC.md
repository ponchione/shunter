# Epic 6: Recovery

**Parent:** [SPEC-002-commitlog.md](../SPEC-002-commitlog.md) §6  
**Blocked by:** Epic 2 (Segment Reader), Epic 3 (Changeset Decoder), Epic 5 (Snapshot Reader), SPEC-001 state export hooks for restore  
**Blocks:** Nothing (entry point for engine startup)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 6.1 | [story-6.1-segment-scanning.md](story-6.1-segment-scanning.md) | List segments, validate contiguity, find durable replay horizon, and classify safe resume mode |
| 6.2 | [story-6.2-snapshot-selection.md](story-6.2-snapshot-selection.md) | Pick best snapshot, fallback chain on corruption |
| 6.3 | [story-6.3-log-replay.md](story-6.3-log-replay.md) | Decode changesets, call ApplyChangeset, rebuild state |
| 6.4 | [story-6.4-open-and-recover.md](story-6.4-open-and-recover.md) | Full OpenAndRecover orchestration with deterministic ErrNoData and fresh-next-segment resume handling |
| 6.5 | [story-6.5-recovery-error-types.md](story-6.5-recovery-error-types.md) | Recovery-specific error types |

## Implementation Order

```
Story 6.5 (Error types) — parallel
Story 6.1 (Segment scanning)
Story 6.2 (Snapshot selection)
Story 6.3 (Log replay)
  └── Story 6.4 (OpenAndRecover) ← depends on 6.1 + 6.2 + 6.3
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 6.1 | `commitlog/segment_scan.go`, `commitlog/segment_scan_test.go` |
| 6.2 | `commitlog/snapshot_select.go`, `commitlog/snapshot_select_test.go` |
| 6.3 | `commitlog/replay.go`, `commitlog/replay_test.go` |
| 6.4 | `commitlog/recovery.go`, `commitlog/recovery_test.go` |
| 6.5 | `commitlog/errors.go` (extend) |
