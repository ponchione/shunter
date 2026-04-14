# Epic 8: Auto-Increment & Recovery

**Parent:** [SPEC-001-store.md](../SPEC-001-store.md) §8, §5.8  
**Blocked by:** Epic 6 (Commit/Changeset), Epic 4 (Indexes)  
**Blocks:** Nothing (consumed by SPEC-002 commit log recovery)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 8.1 | [story-8.1-sequence.md](story-8.1-sequence.md) | Sequence type, auto-increment on insert, monotonic guarantee |
| 8.2 | [story-8.2-apply-changeset.md](story-8.2-apply-changeset.md) | ApplyChangeset for crash recovery replay |
| 8.3 | [story-8.3-state-export.md](story-8.3-state-export.md) | Export/restore nextID + sequence state for snapshot persistence |

## Implementation Order

```
Story 8.1 (Sequence)
Story 8.2 (ApplyChangeset) — independent of 8.1
Story 8.3 (State export) — depends on 8.1
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 8.1 | `sequence.go`, `sequence_test.go` |
| 8.2 | `recovery.go`, `recovery_test.go` |
| 8.3 | `state_export.go`, `state_export_test.go` |
