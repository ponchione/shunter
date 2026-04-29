# Story 4.4: OneOffQuery Handler

**Epic:** [Epic 4 — Client Message Dispatch](EPIC.md)
**Spec ref:** SPEC-005 §7.4, §8.6
**Depends on:** Story 4.1
**Blocks:** Nothing (terminal story in this epic)

**Cross-spec:** SPEC-001 (`CommittedState.Snapshot()`)

---

## Summary

Execute a read-only point-in-time query against committed state. Returns matching rows without establishing an ongoing subscription.

## Deliverables

- `func handleOneOffQuery(conn *Conn, msg *OneOffQueryMsg, store CommittedStateAccess)`:
  1. Validate `table_name` exists
  2. Validate predicate columns exist
  3. Acquire read snapshot: `CommittedState.Snapshot()`
  4. Apply predicates to filter rows (same predicate logic as Subscribe, but one-shot)
  5. Encode matching rows as RowList
  6. Close snapshot
  7. Send `OneOffQueryResult` with status=0 and rows
  8. On any error: send `OneOffQueryResult` with status=1 and error message

- `CommittedStateAccess` interface:
  ```go
  type CommittedStateAccess interface {
      Snapshot() CommittedReadView
  }
  ```

## Acceptance Criteria

- [ ] Valid query → `OneOffQueryResult` with status=0 and matching rows
- [ ] No matching rows → `OneOffQueryResult` with status=0 and empty RowList
- [ ] Invalid table → `OneOffQueryResult` with status=1 and error message
- [ ] Invalid column → `OneOffQueryResult` with status=1 and error message
- [ ] Snapshot is released (closed) after query, even on error
- [ ] Query sees committed state only (not in-flight transactions)
- [ ] Multiple concurrent OneOffQueries allowed (read-only snapshots)

## Design Notes

- Unlike Subscribe, OneOffQuery does NOT go through the executor inbox. It reads directly from `CommittedState.Snapshot()`. This is safe because it creates no persistent state.
- The snapshot must be closed promptly. Hold it only for the duration of the scan. Do not hold it across network writes — encode rows into a buffer, close snapshot, then send.
- Predicate filtering reuses the same normalization logic from Story 4.2, but applied as a one-shot scan rather than a subscription registration.
