# SPEC-006 — Epic Decomposition

Source: [SPEC-006-schema.md](./SPEC-006-schema.md)

---

## Epic 1: Schema Types & Type Mapping

**Spec sections:** §2, §8, §13 (error ownership for type-mapping contracts)

Foundational data structures and Go-to-Shunter type mapping. Everything else builds on these types.

**Scope:**
- `TableSchema`, `ColumnSchema`, `IndexSchema` structs
- `TableID` (`uint32`), `IndexID` (`uint32`) types
- Go type → `ValueKind` mapping table (§2)
- Named type underlying-type resolution via `reflect` (e.g., `type UnixNanos int64` → `Int64`)
- Excluded type set (pointers, interfaces, maps, non-`[]byte` slices, arrays, `int`/`uint`)
- Auto-increment numeric bounds metadata used by SPEC-001 to enforce `ErrSequenceOverflow`
- `ValueKind` → export string mapping (`"bool"`, `"int8"`, … `"bytes"`)
- CamelCase → snake_case conversion utility for table and column names

**Testable outcomes:**
- All 13 Go scalar types map to correct `ValueKind`
- `[]byte` maps to `Bytes`; other slices rejected
- Named type with scalar underlying type accepted
- Unsupported types (pointer, interface, map, `int`, `uint`) rejected
- Integer auto-increment bounds are available for every supported integer `ValueKind`
- `PlayerSession` → `player_session`, `PlayerID` → `player_id`, `ID` → `id`
- `TableSchema`, `ColumnSchema`, `IndexSchema` round-trip through construction

**Dependencies:** None. This is the leaf.

**Cross-spec:** Consumes `ValueKind` from SPEC-001 §2.1. Produces `TableSchema` consumed by SPEC-001 §3.

---

## Epic 2: Struct Tag Parser

**Spec sections:** §3

Parse `shunter:"..."` struct tags into structured directive sets. Pure string parsing — no schema or type dependencies.

**Scope:**
- Directive constants: `primarykey`, `autoincrement`, `unique`, `index`, `index:<name>`, `name:<column-name>`, `-`
- `ParseTag(tag string) (*TagDirectives, error)` — comma-split, extract parametric forms
- Tag-level validation:
  - `-` must appear alone
  - No duplicate directives
  - Unknown directives rejected
  - `primarykey` may not combine with `index` or `index:<name>`
- Default index name generation: `<column>_idx` for plain `index`, `<column>_uniq` for plain `unique`, `pk` for `primarykey`

**Testable outcomes:**
- `"primarykey,autoincrement"` → two directives, correct flags
- `"index:guild_score"` → named index directive with name `guild_score`
- `"name:player_id,primarykey"` → name override + primarykey
- `"-"` → exclude directive
- `"-,index"` → error (dash not alone)
- `"primarykey,index"` → error (contradictory)
- `"foo"` → error (unknown directive)
- `"index,index"` → error (duplicate)
- Empty tag → no directives, no error (plain column)

**Dependencies:** None. This is the leaf.

---

## Epic 3: Builder & Builder-Path Registration

**Spec sections:** §4.2, §4.3, §5 (Builder struct, registration mutators, EngineOptions)

The `Builder` struct and explicit registration methods. Accumulates tables and reducers before validation.

**Scope:**
- `Builder` struct with internal state (table list, reducer map, lifecycle hooks, version)
- `NewBuilder() *Builder`
- `TableDef(def TableDefinition, opts ...TableOption) *Builder` — builder-path registration
- `TableDefinition`, `ColumnDefinition`, `IndexDefinition` types
- `TableOption`, `WithTableName()` option
- `SchemaVersion(v uint32) *Builder`
- `EngineOptions` struct (DataDir, ExecutorQueueCapacity, DurabilityQueueCapacity, EnableProtocol)
- Reducer registration: `Reducer(name, handler)`, `OnConnect(handler)`, `OnDisconnect(handler)`
- `ReducerHandler` type (byte-oriented handler from SPEC-003)

**Testable outcomes:**
- `NewBuilder()` returns non-nil builder
- `TableDef` stores table definition internally
- `WithTableName` overrides stored table name
- `SchemaVersion` stores version
- `Reducer` stores handler by name
- `OnConnect` / `OnDisconnect` store lifecycle hooks
- Multiple `TableDef` calls accumulate tables
- Builder methods return `*Builder` for chaining

**Dependencies:** Epic 1 (schema types, ValueKind used in ColumnDefinition)

**Cross-spec:** `ReducerHandler` type from SPEC-003 §10.

---

## Epic 4: Reflection-Path Registration

**Spec sections:** §4.1, §11

Derive `TableDefinition` from Go struct types using `reflect` and struct tags. The primary developer-facing API.

**Scope:**
- `RegisterTable[T any](b *Builder, opts ...TableOption) error` — generic function
- Field discovery: exported fields in declaration order
- Embedded non-pointer struct flattening (recursive, declaration order)
- Embedded pointer-to-struct → registration error
- Unexported fields silently ignored
- Go type → `ValueKind` mapping per field (Epic 1)
- Tag parsing per field (Epic 2)
- Column naming: snake_case default, `name:<n>` override
- Table naming: snake_case of Go type name, `WithTableName` override
- Composite index assembly: multiple fields with same `index:<name>` → one multi-column index, column order = struct field order
- Error reporting: `"schema error: Player.CachedAt: field type *time.Time is not supported"`

**Testable outcomes:**
- Struct with all 13 supported types → valid `TableDefinition`
- `*time.Time` field → `ErrUnsupportedFieldType`
- Embedded non-pointer struct → fields flattened in order
- Embedded pointer-to-struct → error
- Unexported field → silently skipped
- `shunter:"-"` field → excluded from columns
- `name:player_id` → column name overridden
- Default column name: `PlayerID` → `player_id`
- Default table name: `PlayerSession` → `player_session`
- `WithTableName("players")` → overrides table name
- Two fields with `index:guild_score` → one composite index, struct field order
- Error message includes struct name and field name
- Builder path and reflection path produce equivalent schemas for same table

**Dependencies:** Epic 1 (type mapping, snake_case), Epic 2 (tag parser), Epic 3 (Builder.TableDef)

---

## Epic 5: Validation, Build & SchemaRegistry

**Spec sections:** §5 (Build / Start boundary), §6, §7, §9, §10, §13 (validation/version errors)

Validate all registrations, assign stable IDs, auto-register system tables, construct immutable `SchemaRegistry`, and enforce schema version compatibility at startup.

**Scope:**
- **Validation rules (§9, error ownership in §13):**
  - Table-level: non-empty name, valid pattern `[A-Za-z][A-Za-z0-9_]*`, unique across tables, ≥1 column, ≤1 PK, autoincrement on integer only, autoincrement requires PK or unique
  - Column-level: non-empty name, valid pattern `[a-z][a-z0-9_]*`, unique within table, supported type, no nullable (v1)
  - Index-level: non-empty name, unique within table, ≥1 column, valid column refs, declaration-order columns, PK single-column (v1), no mixed unique on composite, primarykey+index combo rejected
  - Reducer-level: non-empty name, unique, reserved lifecycle names
  - Schema-level: SchemaVersion set and >0, no user tables named `sys_*`
- **System tables (§10):** auto-register `sys_clients` and `sys_scheduled` during Build
- **Build orchestration (§5):** validate → register system tables → assign TableIDs (user tables in registration order, then `sys_clients`, then `sys_scheduled`) → assign IndexIDs → construct SchemaRegistry → return Engine; repeated `Build()` on the same builder returns a deterministic error
- **SchemaRegistry (§7):** read-only interface: `Table(id)`, `TableByName(name)`, `Tables()`, `Reducer(name)`, `Reducers()`, `OnConnect()`, `OnDisconnect()`, `Version()`; immutable, concurrent-safe
- **Schema versioning (§6):** at startup, compare registered version + full schema structure against snapshot; mismatch → `ErrSchemaMismatch` with diff details

**Testable outcomes:**
- Two tables same name → `ErrDuplicateTableName`
- Table named `sys_clients` → `ErrReservedTableName`
- `autoincrement` on string → `ErrAutoIncrementType`
- `autoincrement` without PK or unique → `ErrAutoIncrementRequiresKey`
- Two `primarykey` columns → `ErrDuplicatePrimaryKey`
- Build without `SchemaVersion()` → `ErrSchemaVersionNotSet`
- Build with no tables → `ErrNoTables`
- `sys_clients` and `sys_scheduled` present in registry after Build
- `SchemaRegistry.Table(id)` returns correct schema
- `SchemaRegistry.Tables()` returns user IDs then system IDs, stable order
- `SchemaRegistry.Reducer(name)` returns registered handler
- Same registration inputs → same TableIDs across runs
- Schema version mismatch at startup → `ErrSchemaMismatch`
- Matching schema + version → recovery proceeds

**Dependencies:** Epic 3 (Builder state to validate). Epic 4 feeds additional reflection-path inputs through the same validation/build pipeline but is not a hard blocker for builder-path implementation.

**Cross-spec:** `SchemaRegistry` consumed by SPEC-001, SPEC-002, SPEC-003. Schema version stored in snapshots per SPEC-002 §5.3.

---

## Epic 6: Schema Export & Codegen Interface

**Spec sections:** §12

Export registered schema for client code generation tools. Build-time concern, not runtime.

**Scope:**
- Export types: `SchemaExport`, `TableExport`, `ColumnExport`, `IndexExport`, `ReducerExport`
- `Engine.ExportSchema() *SchemaExport` — walks SchemaRegistry, builds export structs
- Column type as string (`"bool"`, `"int8"`, … `"bytes"`)
- `ReducerExport.Lifecycle` flag for OnConnect/OnDisconnect
- JSON serialization of `SchemaExport`
- `shunter-codegen` CLI contract (§12.2): consume `schema.json`, validate `--lang/--schema/--out`, and generate build-time client artifacts

**Testable outcomes:**
- `ExportSchema()` includes all user tables and system tables
- Column types rendered as lowercase strings
- Index export includes columns, uniqueness, primary flag
- Lifecycle reducers marked with `Lifecycle: true`
- Non-lifecycle reducers have `Lifecycle: false`
- JSON round-trip: marshal → unmarshal → equals original
- Export includes schema version
- `shunter-codegen --lang typescript --schema schema.json --out ./generated/` validates its contract inputs and reads `SchemaExport` JSON successfully

**Dependencies:** Epic 5 (SchemaRegistry to export from)

---

## Dependency Graph

```
Epic 1: Schema Types & Type Mapping
  │
  ├── Epic 3: Builder & Builder-Path Registration
  │     │
  │     ├── Epic 5: Validation, Build & SchemaRegistry
  │     │     │
  │     │     └── Epic 6: Schema Export & Codegen Interface
  │     │
  │     └── Epic 4: Reflection-Path Registration ← Epic 1, Epic 2, Epic 3
  │
Epic 2: Struct Tag Parser
  └── Epic 4 (also depends on Epic 2)
```

Linearized build order: 1 → 2 → 3 → 4 → 5 → 6 (Epics 1 and 2 may be built in parallel; Epic 5 can start on the builder path before Epic 4 lands, then absorb reflection-path coverage once Epic 4 is complete)

## Error Types

Errors introduced where first needed:

| Error | Introduced in |
|---|---|
| `ErrUnsupportedFieldType` | Epic 1 (Go type mapping; surfaced by reflection in Epic 4) |
| `ErrSequenceOverflow` | Epic 1 (integer bounds contract exported to SPEC-001 auto-increment logic) |
| `ErrDuplicateTableName` | Epic 5 (table-level validation) |
| `ErrDuplicateReducerName` | Epic 5 (reducer-level validation) |
| `ErrReservedTableName` | Epic 5 (schema-level validation) |
| `ErrDuplicatePrimaryKey` | Epic 5 (table-level validation) |
| `ErrAutoIncrementRequiresKey` | Epic 5 (table-level validation) |
| `ErrAutoIncrementType` | Epic 5 (table-level validation) |
| `ErrEmptyTableName` | Epic 5 (table-level validation) |
| `ErrInvalidColumnName` | Epic 5 (column-level validation) |
| `ErrSchemaMismatch` | Epic 5 (schema version check) |
| `ErrSchemaVersionNotSet` | Epic 5 (schema-level validation) |
| `ErrNoTables` | Epic 5 (schema-level validation) |
