# Epic 5: Transaction Layer

**Parent:** [SPEC-001-store.md](../SPEC-001-store.md) §5.1–§5.5  
**Blocked by:** Epic 2 (Table), Epic 3 (BTreeIndex), Epic 4 (Constraints)  
**Blocks:** Epic 6 (Commit, Rollback & Changeset)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 5.1 | [story-5.1-committed-state.md](story-5.1-committed-state.md) | CommittedState struct, table registry, RWMutex |
| 5.2 | [story-5.2-tx-state.md](story-5.2-tx-state.md) | TxState struct, insert/delete buffers |
| 5.3 | [story-5.3-state-view.md](story-5.3-state-view.md) | StateView unified read: GetRow, ScanTable, SeekIndex, SeekIndexRange |
| 5.4 | [story-5.4-transaction-insert.md](story-5.4-transaction-insert.md) | Transaction.Insert with two-layer constraint checking, undelete optimization |
| 5.5 | [story-5.5-transaction-delete.md](story-5.5-transaction-delete.md) | Transaction.Delete with tx-local vs committed branching |
| 5.6 | [story-5.6-transaction-update.md](story-5.6-transaction-update.md) | Transaction.Update as delete+insert with collapse semantics |

## Implementation Order

```
Story 5.1 (CommittedState)
  └── Story 5.2 (TxState)
        └── Story 5.3 (StateView)
              ├── Story 5.4 (Insert)
              ├── Story 5.5 (Delete)
              └── Story 5.6 (Update) ← depends on 5.4 + 5.5
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 5.1 | `committed_state.go` |
| 5.2 | `tx_state.go` |
| 5.3 | `state_view.go`, `state_view_test.go` |
| 5.4–5.6 | `transaction.go`, `transaction_test.go` |
