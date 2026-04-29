# Story 1.4: ValueKind Export Strings & Integer Bounds

**Epic:** [Epic 1 — Schema Types & Type Mapping](EPIC.md)
**Spec ref:** SPEC-006 §2, §12.1, §13 (`ErrSequenceOverflow` contract)
**Depends on:** Story 1.1 (ValueKind re-export)
**Blocks:** Epic 5 (validation/build), Epic 6 (schema export)

**Cross-spec:** Exports lowercase type strings consumed by Epic 6 and integer bounds consumed by SPEC-001 auto-increment logic.

---

## Summary

Provide the two schema-side contracts layered on top of `ValueKind`: the lowercase type strings used in schema export and the integer bounds metadata used to enforce auto-increment overflow.

## Deliverables

- `ValueKindExportString(k ValueKind) string` — returns the lowercase export string for every supported kind: `"bool"`, `"int8"`, `"uint8"`, … `"string"`, `"bytes"`.

- `AutoIncrementBounds(k ValueKind) (min int64, max uint64, ok bool)` — reports whether a `ValueKind` is eligible for auto-increment and the representable integer bound that SPEC-001 must enforce for `ErrSequenceOverflow`.
  - Signed integer kinds return both a signed minimum and unsigned maximum representation of the positive bound
  - Unsigned integer kinds return `min=0`
  - Non-integer kinds return `ok=false`

## Acceptance Criteria

- [ ] `ValueKindExportString` returns the correct lowercase string for all 13 `ValueKind` values
- [ ] `AutoIncrementBounds(Int8)` returns the correct signed range metadata
- [ ] `AutoIncrementBounds(Uint64)` returns the correct unsigned range metadata
- [ ] `AutoIncrementBounds(String)` returns `ok=false`
- [ ] All integer `ValueKind`s report `ok=true`; all non-integer kinds report `ok=false`
- [ ] Design/docs state explicitly that SPEC-001 raises `ErrSequenceOverflow` using this bounds contract

## Design Notes

- `ErrSequenceOverflow` is a runtime insert/sequence failure owned operationally by SPEC-001, but the schema layer owns the authoritative notion of which `ValueKind`s participate in auto-increment and what their representable bounds are.
- Keeping export-string and bounds lookup together is deliberate: both are small pure functions over `ValueKind`, and both are reused outside the reflection path.
