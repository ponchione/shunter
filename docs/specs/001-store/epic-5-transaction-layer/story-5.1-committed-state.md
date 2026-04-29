# Story 5.1: CommittedState

**Epic:** [Epic 5 — Transaction Layer](EPIC.md)  
**Spec ref:** SPEC-001 §5.2  
**Depends on:** Epic 2 (Table)  
**Blocks:** Stories 5.2, 5.3

---

## Summary

The authoritative post-commit state. Holds all tables, guarded by RWMutex.

## Deliverables

- `CommittedState` struct:
  ```go
  type CommittedState struct {
      tables map[TableID]*Table
      mu     sync.RWMutex
  }
  ```

- `func NewCommittedState() *CommittedState`

- `func (cs *CommittedState) RegisterTable(schema *TableSchema) error`
  - Creates Table from schema, inserts into tables map
  - Error if TableID already registered

- `func (cs *CommittedState) Table(id TableID) (*Table, bool)`
  - Lookup by TableID

- `func (cs *CommittedState) TableIDs() []TableID`
  - All registered table IDs

- Lock methods (used by Commit in Epic 6 and Snapshot in Epic 7):
  - `func (cs *CommittedState) RLock()` / `RUnlock()`
  - `func (cs *CommittedState) Lock()` / `Unlock()`

## Acceptance Criteria

- [ ] Register table, look up by ID — found
- [ ] Look up unregistered ID — not found
- [ ] Register same ID twice → error
- [ ] TableIDs returns all registered IDs
- [ ] Concurrent RLock from multiple goroutines — no contention
- [ ] Lock blocks until all RLocks released

## Design Notes

- CommittedState is mutated ONLY at commit time (Epic 6) by the single writer goroutine. `mu` write-lock during commit, read-lock during snapshot access.
- The store does not enforce single-writer access. That's the executor's job (SPEC-003).
- Table registration happens at startup before any transactions. No runtime schema changes in v1.
