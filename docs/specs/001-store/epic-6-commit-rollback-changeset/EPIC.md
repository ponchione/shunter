# Epic 6: Commit, Rollback & Changeset

**Parent:** [SPEC-001-store.md](../SPEC-001-store.md) §5.6, §5.7, §6.1–§6.3  
**Blocked by:** Epic 5 (Transaction Layer)  
**Blocks:** Epic 7 (Snapshots), Epic 8 (Recovery)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 6.1 | [story-6.1-changeset-types.md](story-6.1-changeset-types.md) | Changeset, TableChangeset, TxID structs |
| 6.2 | [story-6.2-commit.md](story-6.2-commit.md) | Commit algorithm: lock, apply deletes, apply inserts, build changeset |
| 6.3 | [story-6.3-net-effect-semantics.md](story-6.3-net-effect-semantics.md) | Changeset correctly reflects net effects across all collapse cases |
| 6.4 | [story-6.4-rollback.md](story-6.4-rollback.md) | Rollback: discard TxState, O(1) |

## Implementation Order

```
Story 6.1 (Changeset types)
  └── Story 6.2 (Commit algorithm)
        └── Story 6.3 (Net-effect verification)
Story 6.4 (Rollback) — independent
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 6.1 | `changeset.go` |
| 6.2–6.3 | `commit.go`, `commit_test.go` |
| 6.4 | `transaction.go` (extend), `transaction_test.go` (extend) |
