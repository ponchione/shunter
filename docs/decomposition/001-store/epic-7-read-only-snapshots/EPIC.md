# Epic 7: Read-Only Snapshots

**Parent:** [SPEC-001-store.md](../SPEC-001-store.md) §7  
**Blocked by:** Epic 4 (Indexes), Epic 6 (Commit)  
**Blocks:** Nothing (consumed by SPEC-004 subscription evaluator)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 7.1 | [story-7.1-committed-read-view.md](story-7.1-committed-read-view.md) | CommittedReadView interface, CommittedSnapshot implementation, and short-lived usage contract |
| 7.2 | [story-7.2-snapshot-concurrency.md](story-7.2-snapshot-concurrency.md) | RLock semantics, commit blocking, multiple coexisting snapshots, and materialize-then-close test coverage |

## Implementation Order

```
Story 7.1 (Interface + implementation)
  └── Story 7.2 (Concurrency verification)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 7.1 | `snapshot.go`, `snapshot_test.go` |
| 7.2 | `snapshot_test.go` (extend with concurrency tests) |
