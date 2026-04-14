# Story 5.2: System Table Auto-Registration

**Epic:** [Epic 5 — Validation, Build & SchemaRegistry](EPIC.md)
**Spec ref:** SPEC-006 §10
**Depends on:** Epic 3 (Builder.TableDef)
**Blocks:** Story 5.3 (Build inserts system tables)

**Cross-spec:** `sys_clients.connection_id` uses `ConnectionID` from SPEC-005 §2 (16 raw bytes). `sys_scheduled` supports the scheduler from SPEC-003.

---

## Summary

Two system tables are auto-registered during `Build()`, after user tables. User code does not declare them.

## Deliverables

- `func registerSystemTables(b *Builder)` — internally registers the two system tables via `TableDef`:

- **`sys_clients`** definition:
  | Column | Type | Directives |
  |---|---|---|
  | `connection_id` | Bytes | primarykey |
  | `identity` | Bytes | — |
  | `connected_at` | Int64 | — |

  Primary key on `connection_id` (16 raw bytes, lexicographic byte ordering). Inserted on connect, deleted on disconnect. Readable by reducer code.

- **`sys_scheduled`** definition:
  | Column | Type | Directives |
  |---|---|---|
  | `schedule_id` | Uint64 | primarykey, autoincrement |
  | `reducer_name` | String | — |
  | `args` | Bytes | — |
  | `next_run_at_ns` | Int64 | — |
  | `repeat_ns` | Int64 | — |

  Primary key on `schedule_id` with auto-increment. Managed by the scheduler subsystem.

## Acceptance Criteria

- [ ] After Build, `sys_clients` exists in SchemaRegistry
- [ ] After Build, `sys_scheduled` exists in SchemaRegistry
- [ ] `sys_clients` has 3 columns: `connection_id` (Bytes, PK), `identity` (Bytes), `connected_at` (Int64)
- [ ] `sys_scheduled` has 5 columns: `schedule_id` (Uint64, PK, autoincrement), `reducer_name` (String), `args` (Bytes), `next_run_at_ns` (Int64), `repeat_ns` (Int64)
- [ ] System tables receive TableIDs after all user tables: `sys_clients` first, then `sys_scheduled`
- [ ] System table schemas pass all validation rules (dogfood)
- [ ] System tables produce subscription deltas like any user table (no special casing)

## Design Notes

- System tables are registered via the same `TableDef` path as user tables. This ensures they pass the same validation rules. No special-case code paths for system tables at the storage or subscription layer.
- `connection_id` stores 16 raw bytes as `Bytes` (`[]byte`). The primary key index uses lexicographic byte ordering from SPEC-001 §2.2.
- `sys_scheduled.repeat_ns` of `0` means one-shot; `>0` means repeating at that interval. The scheduler (SPEC-003) interprets this, not the schema layer.
