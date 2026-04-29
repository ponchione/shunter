# Story 1.2: Go Type → ValueKind Mapping

**Epic:** [Epic 1 — Schema Types & Type Mapping](EPIC.md)
**Spec ref:** SPEC-006 §2, §13 (`ErrUnsupportedFieldType` ownership)
**Depends on:** Story 1.1 (ValueKind re-export)
**Blocks:** Epic 4 (Reflection engine uses this mapping)

**Cross-spec:** Consumes `ValueKind` enum from SPEC-001 §2.1.

---

## Summary

Map supported Go field types to Shunter `ValueKind` values and reject unsupported types deterministically. This is the reflection-path gate for what field shapes may enter the schema system.

## Deliverables

- `GoTypeToValueKind(t reflect.Type) (ValueKind, error)` — maps a Go `reflect.Type` to the corresponding `ValueKind`:

  | Go type | ValueKind |
  |---|---|
  | `bool` | Bool |
  | `int8` | Int8 |
  | `uint8` | Uint8 |
  | `int16` | Int16 |
  | `uint16` | Uint16 |
  | `int32` | Int32 |
  | `uint32` | Uint32 |
  | `int64` | Int64 |
  | `uint64` | Uint64 |
  | `float32` | Float32 |
  | `float64` | Float64 |
  | `string` | String |
  | `[]byte` | Bytes |

  For named types (for example `type UnixNanos int64`), resolve through `reflect.Type.Kind()` to the underlying scalar. `[]byte` remains a special case checked before generic slice rejection.

- Excluded types: `GoTypeToValueKind` returns `ErrUnsupportedFieldType` for:
  - Pointers
  - Interfaces
  - Maps
  - Slices other than `[]byte`
  - Arrays
  - Structs (non-embedded; embedded struct handling is Epic 4's concern)
  - `int` / `uint` (platform-dependent width)
  - Channels, functions, complex types

## Acceptance Criteria

- [ ] All 13 Go scalar types map to the correct `ValueKind`
- [ ] `[]byte` → `Bytes`
- [ ] Named scalar and byte-slice aliases (`type Score int64`, `type ID uint64`, `type Blob []byte`) resolve to their underlying supported `ValueKind`
- [ ] Unsupported slice/map/pointer forms (`[]string`, `map[string]int`, `*int64`) return `ErrUnsupportedFieldType`
- [ ] Platform-width and interface types (`int`, `uint`, `interface{}`) return `ErrUnsupportedFieldType`
- [ ] Error message includes the rejected Go type name

## Design Notes

- `int` and `uint` are excluded because their width varies by platform (32 or 64 bits). Deterministic storage requires explicit widths. The error should suggest `int64` or `uint64` instead.
- `[]byte` must be checked before generic `reflect.Kind()` slice rejection.
- `ErrUnsupportedFieldType` is introduced here even though Epic 4 is where most developers will encounter it through reflection error messages.
