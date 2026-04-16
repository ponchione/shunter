# Story 6.1: Changeset Types

**Epic:** [Epic 6 — Commit, Rollback & Changeset](EPIC.md)  
**Spec ref:** SPEC-001 §6.1  
**Depends on:** Epic 1 (ProductValue), Epic 2 (TableID)  
**Blocks:** Stories 6.2, 6.3

---

## Summary

Data structures for the net-effect output of a committed transaction, using the shared `TxID` type defined by SPEC-003 and declared in the `types/` Go package (SPEC-001 §2.4).

## Deliverables

- Shared `TxID` type imported from SPEC-003 §6; lives in the `types/` Go package
  - Do not define a new store-local `type TxID uint64` in this story; use the shared engine type so SPEC-001 and SPEC-002 point at one authoritative home
  - Monotonically increasing per-database counter, owned and advanced by the executor (Model A — see SPEC-001 §5.6, SPEC-003 §13.2). `Changeset.TxID` is stamped by the executor after `Commit` returns; the store never assigns it.

- `Changeset` struct:
  ```go
  type Changeset struct {
      TxID   TxID
      Tables map[TableID]*TableChangeset
  }
  ```

- `TableChangeset` struct:
  ```go
  type TableChangeset struct {
      TableID   TableID
      TableName string
      Inserts   []ProductValue   // rows whose net effect is "now present"
      Deletes   []ProductValue   // rows whose net effect is "now absent"
  }
  ```

- `func (cs *Changeset) IsEmpty() bool` — true if no table has any inserts or deletes

- `func (cs *Changeset) TableChangeset(id TableID) *TableChangeset` — lookup, nil if no changes for table

## Acceptance Criteria

- [ ] Construct Changeset with inserts and deletes for two tables — accessible by TableID
- [ ] IsEmpty on changeset with no inserts/deletes → true
- [ ] IsEmpty on changeset with one insert → false
- [ ] TableChangeset for unknown table → nil
- [ ] Imported shared TxID type is usable as map key, comparable, printable

## Design Notes

- Changeset is immutable after creation. Consumers (SPEC-002 commit log, SPEC-004 subscription evaluator) receive the same value. No defensive copying needed — just don't mutate.
- No separate `Updates` list in v1. SPEC-004 derives update semantics by comparing insert/delete rows against subscription predicates.
