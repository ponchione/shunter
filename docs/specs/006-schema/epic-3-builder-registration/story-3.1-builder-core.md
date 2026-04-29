# Story 3.1: Builder Core & Builder-Path Registration

**Epic:** [Epic 3 — Builder & Builder-Path Registration](EPIC.md)
**Spec ref:** SPEC-006 §4, §4.2, §5
**Depends on:** Epic 1 (schema types)
**Blocks:** Story 3.2, Epic 4, Epic 5

**Cross-spec:** Stores reducer/lifecycle registrations consumed by SPEC-003 and engine options consumed when SPEC-002/SPEC-003 runtime startup is wired in.

---

## Summary

The `Builder` struct accumulates table definitions, reducers, and engine configuration before `Build()` validates and freezes everything. This story covers the builder itself and the explicit (non-reflection) registration path.

## Deliverables

- `Builder` struct (fields unexported):
  ```go
  type Builder struct {
      tables     []tableEntry     // accumulated table definitions
      reducers   map[string]ReducerHandler
      onConnect  func(*ReducerContext) error
      onDisconnect func(*ReducerContext) error
      onConnectRegistrations    int
      onDisconnectRegistrations int
      version    uint32
      versionSet bool
  }
  ```

- `func NewBuilder() *Builder`

- `TableDefinition` struct:
  ```go
  type TableDefinition struct {
      Name    string
      Columns []ColumnDefinition
      Indexes []IndexDefinition
  }
  ```

- `ColumnDefinition` struct:
  ```go
  type ColumnDefinition struct {
      Name          string
      Type          ValueKind
      PrimaryKey    bool
      AutoIncrement bool
  }
  ```

- `IndexDefinition` struct:
  ```go
  type IndexDefinition struct {
      Name    string
      Columns []string  // column names, in key order
      Unique  bool
  }
  ```

- `TableOption` and `WithTableName(name string) TableOption`

- `func (b *Builder) TableDef(def TableDefinition, opts ...TableOption) *Builder`
  — stores the definition internally. `WithTableName` overrides `def.Name` if provided.

- `func (b *Builder) SchemaVersion(v uint32) *Builder`
  — stores the version. Must be called before `Build()`.

- `EngineOptions` struct:
  ```go
  type EngineOptions struct {
      DataDir                 string
      ExecutorQueueCapacity   int
      DurabilityQueueCapacity int
      EnableProtocol          bool
  }
  ```

## Acceptance Criteria

- [ ] `NewBuilder()` returns non-nil `*Builder`
- [ ] `TableDef` stores definition; multiple calls accumulate
- [ ] `WithTableName("players")` overrides `TableDefinition.Name`
- [ ] `TableDef` without `WithTableName` uses `def.Name`
- [ ] `SchemaVersion(3)` stores version 3
- [ ] All builder methods return `*Builder` for chaining
- [ ] `EngineOptions` fields have documented zero-value defaults

## Design Notes

- The builder does no validation on individual `TableDef` calls. All validation is deferred to `Build()` (Epic 5). This keeps the builder simple and gives better aggregate error reporting.
- `TableDefinition.Columns` uses `PrimaryKey` and `AutoIncrement` on individual columns rather than separate PK declarations. This mirrors the struct tag model where these are per-field.
- `IndexDefinition` is only for secondary indexes. The primary index is derived from the `PrimaryKey` column during `Build()`.
