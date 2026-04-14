# Story 6.3: Net-Effect Semantics Verification

**Epic:** [Epic 6 — Commit, Rollback & Changeset](EPIC.md)  
**Spec ref:** SPEC-001 §6.2  
**Depends on:** Story 6.2  
**Blocks:** Nothing (verification story)

---

## Summary

Verify that changesets produced by Commit correctly capture net effects, not raw operation logs. This is primarily a **test story** — the net-effect behavior is a consequence of TxState collapse logic (Epic 5) plus Commit reading what survives to commit time.

## Net-Effect Rules

| Scenario | Inserts | Deletes |
|---|---|---|
| Row inserted, never deleted | row in Inserts | — |
| Committed row deleted, not reinserted | — | row in Deletes |
| Row inserted then deleted in same TX | — | — |
| Committed row deleted, identical reinserted (undelete) | — | — |
| Committed row deleted, different row inserted | new row in Inserts | old row in Deletes |
| Update (delete old + insert new) | new row in Inserts | old row in Deletes |
| Update to identical value (no-op) | — | — |

## Acceptance Criteria

- [ ] Pure insert → changeset Inserts only
- [ ] Pure delete → changeset Deletes only
- [ ] Insert + delete same row in TX → empty changeset for that table
- [ ] Delete committed + reinsert identical → empty changeset (undelete collapsed in TX layer)
- [ ] Delete committed + insert different → old in Deletes, new in Inserts
- [ ] Update (different value) → old in Deletes, new in Inserts
- [ ] Update to identical value → empty changeset (undelete)
- [ ] Multiple tables in one TX → each TableChangeset independent
- [ ] Changeset Inserts/Deletes contain the actual ProductValue rows (not RowIDs)
- [ ] Changeset is read-only after creation — verify no mutation methods exist

## Design Notes

- Most net-effect collapse happens in TxState during the transaction (insert-then-delete removes from inserts, reinsert-identical cancels delete). By commit time, tx.inserts contains only surviving inserts and tx.deletes contains only surviving deletes. Commit just materializes what's left.
- This story exists as a focused test suite rather than new code. The implementation is spread across Epic 5 (TxState collapse) and Story 6.2 (Commit reads surviving state).
