# Story 6.1: sys_scheduled Table

**Epic:** [Epic 6 — Scheduled Reducers](EPIC.md)  
**Spec ref:** SPEC-003 §9.2  
**Depends on:** SPEC-001 (Table, TableSchema)  
**Blocks:** Story 6.2

---

## Summary

Built-in system table for durable scheduled reducer storage. Source of truth for all pending schedules.

## Deliverables

- Table schema:
  ```go
  sys_scheduled {
      schedule_id:    uint64   primarykey autoincrement
      reducer_name:   string
      args:           bytes
      next_run_at_ns: int64    // Unix nanoseconds
      repeat_ns:      int64    // 0 = one-shot
  }
  ```

- Schema registration function:
  ```go
  func RegisterSysScheduledTable(schema SchemaRegistry) TableID
  ```
  - Registers the table with the schema registry at startup
  - Returns the assigned TableID for executor use

- `type ScheduleID uint64` (already in Story 1.1, used as PK here)

## Acceptance Criteria

- [ ] Table registered with correct column types
- [ ] schedule_id is autoincrement primary key
- [ ] repeat_ns = 0 means one-shot schedule
- [ ] repeat_ns > 0 means repeating schedule
- [ ] next_run_at_ns stores Unix nanoseconds
- [ ] Table is mutable via normal Transaction operations (insert/delete/update)

## Design Notes

- `sys_scheduled` is a regular table in committed state — it benefits from all SPEC-001 guarantees (transactions, indexes, changesets). The scheduler timer is just an in-memory cache of future wakeups; the table is authoritative.
- Autoincrement for `schedule_id` uses SPEC-001's Sequence mechanism (SPEC-001 §8).
- The table appears in changesets and can trigger subscription deltas. This is by design — applications can subscribe to `sys_scheduled` to observe pending schedules.
