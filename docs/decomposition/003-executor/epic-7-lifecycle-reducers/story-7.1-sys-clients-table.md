# Story 7.1: sys_clients Table

**Epic:** [Epic 7 — Lifecycle Reducers & Client Management](EPIC.md)  
**Spec ref:** SPEC-003 §10.2  
**Depends on:** SPEC-001 (Table, TableSchema)  
**Blocks:** Stories 7.2, 7.3

---

## Summary

Built-in system table tracking active client connections. Readable by reducer code; changes appear in changesets.

## Deliverables

- Table schema:
  ```go
  sys_clients {
      connection_id: bytes(16)  primarykey   // ConnectionID
      identity:      bytes(32)               // Identity
      connected_at:  int64                   // Unix nanoseconds
  }
  ```

- Schema registration function:
  ```go
  func RegisterSysClientsTable(schema SchemaRegistry) TableID
  ```

## Acceptance Criteria

- [ ] Table registered with correct column types and sizes
- [ ] connection_id is primary key (bytes(16))
- [ ] identity is 32 bytes
- [ ] connected_at stores Unix nanoseconds
- [ ] Reducer code can read sys_clients via Transaction
- [ ] sys_clients changes appear in changesets and trigger subscription deltas

## Design Notes

- `sys_clients` is a normal table in committed state. It benefits from all SPEC-001 guarantees: transactional inserts/deletes, index lookups by connection_id, changeset inclusion.
- Applications can subscribe to `sys_clients` to receive connect/disconnect notifications as subscription deltas.
- Connection ID is 16 bytes (UUID-sized). Identity is 32 bytes per SPEC-001 §2.4.
