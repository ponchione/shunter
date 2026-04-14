# Story 1.1: ValueKind + Value Struct + Constructors

**Epic:** [Epic 1 — Core Value Types](EPIC.md)  
**Spec ref:** SPEC-001 §2.1, §2.2  
**Depends on:** Nothing  
**Blocks:** Stories 1.2, 1.3, 1.4

---

## Summary

The tagged union that represents a single column value.

## Deliverables

- `ValueKind` — integer enum with 13 variants:
  `Bool, Int8, Uint8, Int16, Uint16, Int32, Uint32, Int64, Uint64, Float32, Float64, String, Bytes`

- `Value` struct:
  ```go
  type Value struct {
      kind ValueKind
      b    bool
      i64  int64
      u64  uint64
      f32  float32
      f64  float64
      str  string
      buf  []byte
  }
  ```
  Fields unexported. Only one payload field populated per kind.

- Constructor per kind:
  `NewBool(bool)`, `NewInt8(int8)` ... `NewString(string)`, `NewBytes([]byte)`
  - Signed integers (Int8–Int64) all stored in `i64`
  - Unsigned integers (Uint8–Uint64) all stored in `u64`
  - `NewFloat32` / `NewFloat64` — reject NaN, return `(Value, error)`
  - `NewBytes` — **copies** input slice (immutability contract)
  - `NewString` — Go strings already immutable, no copy needed

- Accessor per kind:
  `AsBool() bool`, `AsInt8() int8`, etc.
  - Panic on kind mismatch (caller bug, not user error)

- `Kind() ValueKind` method

- `ValueKind.String()` for debug printing

## Acceptance Criteria

- [ ] Construct each of 13 kinds, round-trip through accessor — value matches
- [ ] `NewFloat32(NaN)` returns error
- [ ] `NewFloat64(NaN)` returns error
- [ ] `NewBytes(b)` — mutating original `b` does not affect stored Value
- [ ] Accessor kind mismatch panics

## Design Notes

- Signed ints widen into `i64`, unsigned into `u64`. Narrow-type constructors validate range (e.g., `NewInt8` rejects values outside [-128, 127] — but since Go signatures use typed params like `int8`, this is enforced at compile time).
- `float32` stored in dedicated `f32` field, not widened to `f64`. Preserves bit-exact round-trip.
- The zero-initialized Go struct for `Value` is not part of the store contract. Valid stored values should come from explicit constructors or other code paths that establish a deliberate `ValueKind`.
