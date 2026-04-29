# Story 6.4: Rollback

**Epic:** [Epic 6 — Commit, Rollback & Changeset](EPIC.md)  
**Spec ref:** SPEC-001 §5.7  
**Depends on:** Epic 5 (Transaction)  
**Blocks:** Nothing

---

## Summary

Discard a transaction. No committed state change. O(1).

## Deliverables

- `func Rollback(tx *Transaction)`
  - Sets tx.tx to nil (or clears inserts/deletes)
  - No lock acquired on CommittedState
  - No cleanup needed — GC handles TxState memory

- After rollback, the Transaction is no longer usable. Any subsequent Insert/Delete/Update/Commit should panic or return error.

## Acceptance Criteria

- [ ] Rollback after inserts → committed state has no trace of inserted rows
- [ ] Rollback after deletes → committed rows still present
- [ ] Rollback after mixed ops → committed state identical to pre-transaction
- [ ] Rollback is O(1) — no per-row cleanup, no index maintenance
- [ ] Provisional RowIDs consumed during TX are NOT reused after rollback (gaps allowed)
- [ ] Using Transaction after Rollback → panic or error (not silent corruption)

## Design Notes

- Rollback is trivially simple because TxState is a separate buffer that never touches committed state. Drop the reference, let GC collect.
- RowID gaps from rolled-back transactions are explicitly allowed by spec §2.3. The monotonic counter does not reset.
