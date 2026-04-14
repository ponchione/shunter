# Story 6.1: Schema Export Types

**Epic:** [Epic 6 — Schema Export & Codegen Interface](EPIC.md)
**Spec ref:** SPEC-006 §12, §12.1
**Depends on:** Epic 1 (ValueKindExportString)
**Blocks:** Story 6.2

---

## Summary

Serializable types that describe the registered schema for client code generation tools. These are JSON-friendly value types with no internal pointers or interface dependencies.

## Deliverables

- `SchemaExport` struct:
  ```go
  type SchemaExport struct {
      Version  uint32          `json:"version"`
      Tables   []TableExport   `json:"tables"`
      Reducers []ReducerExport `json:"reducers"`
  }
  ```

- `TableExport` struct:
  ```go
  type TableExport struct {
      Name    string         `json:"name"`
      Columns []ColumnExport `json:"columns"`
      Indexes []IndexExport  `json:"indexes"`
  }
  ```

- `ColumnExport` struct:
  ```go
  type ColumnExport struct {
      Name string `json:"name"`
      Type string `json:"type"` // "bool", "int8", ... "string", "bytes"
  }
  ```

- `IndexExport` struct:
  ```go
  type IndexExport struct {
      Name    string   `json:"name"`
      Columns []string `json:"columns"`
      Unique  bool     `json:"unique"`
      Primary bool     `json:"primary"`
  }
  ```

- `ReducerExport` struct:
  ```go
  type ReducerExport struct {
      Name      string `json:"name"`
      Lifecycle bool   `json:"lifecycle"`
  }
  ```

## Acceptance Criteria

- [ ] All export types have JSON struct tags
- [ ] `ColumnExport.Type` is a lowercase string: `"bool"`, `"int8"`, `"uint8"`, … `"string"`, `"bytes"`
- [ ] `ReducerExport.Lifecycle` is `true` for `OnConnect`/`OnDisconnect`, `false` otherwise
- [ ] `IndexExport.Columns` uses column names (strings), not indices
- [ ] All types are JSON-serializable: `json.Marshal` → `json.Unmarshal` round-trips correctly
- [ ] No pointer fields, no interface fields — pure value types

## Design Notes

- Column type is a string, not a `ValueKind` integer. Codegen tools should not need to know Shunter's internal enum numbering. The `ValueKindExportString` function (Story 1.4) produces these strings.
- Reducer argument/return types are not introspectable in v1 (byte-oriented handler). The `ReducerExport` type intentionally omits argument schemas. Typed reducer codegen is a v2 design problem per §12.2.
- System tables (`sys_clients`, `sys_scheduled`) are included in the export. Client code may want to subscribe to them.
