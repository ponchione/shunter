# Story 1.1: ValueKind + Value Struct + Constructors

**Epic:** [Epic 1 — Core Value Types](EPIC.md)  
**Spec ref:** SPEC-001 §2.1, §2.2  
**Depends on:** Nothing  
**Blocks:** Stories 1.2, 1.3, 1.4

---

## Summary

The tagged union that represents a single column value.

## Deliverables

- `ValueKind` — integer enum with 14 variants:
  `Invalid (= 0), Bool, Int8, Uint8, Int16, Uint16, Int32, Uint32, Int64, Uint64, Float32, Float64, String, Bytes`
  - `ValueKind(0) = Invalid`. A zero-initialized `Value` (`var v Value`) has `kind = Invalid`; Equal, Compare, Hash, and every `As*` accessor panic on an Invalid-kind Value. Valid variants start at 1 so the zero-value Go struct is unambiguously not a stored value (closes SPEC-AUDIT §4.5).

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
  - `NewFloat32` / `NewFloat64` — reject NaN, return `(Value, error)`. The error sentinel is `ErrInvalidFloat` (SPEC-001 §9); these constructors are its only producer.
  - `NewBytes` — **copies** input slice (immutability contract)
  - `NewString` — Go strings already immutable, no copy needed

- Accessor per kind:
  `AsBool() bool`, `AsInt8() int8`, etc.
  - Panic on kind mismatch (caller bug, not user error)
  - `AsBytes() []byte` returns a slice aliasing the Value's internal `buf`. Callers MUST NOT mutate the returned slice. The immutability invariant in SPEC-001 §2.2 depends on the Value having been constructed through `NewBytes` (which copies input) and never handed a mutable view afterwards. If a caller needs a mutable copy, use `append([]byte(nil), v.AsBytes()...)`.
  - `AsString() string` returns the stored string directly (Go strings are already immutable; no aliasing concern).

- `Kind() ValueKind` method

- `ValueKind.String()` for debug printing

## Acceptance Criteria

- [ ] Construct each of 13 kinds, round-trip through accessor — value matches
- [ ] `NewFloat32(NaN)` returns `ErrInvalidFloat`
- [ ] `NewFloat64(NaN)` returns `ErrInvalidFloat`
- [ ] `NewBytes(b)` — mutating original `b` does not affect stored Value
- [ ] Accessor kind mismatch panics
- [ ] `AsBytes` returns a non-nil slice whose length and content match the NewBytes input; its mutation is undefined behavior (contract: read-only)
- [ ] `var v Value` then `v.Kind() == Invalid`
- [ ] Any accessor (`AsBool`, `AsInt8`, …, `AsBytes`) on an Invalid-kind Value panics
- [ ] Equal / Compare / Hash on an Invalid-kind Value panics

## Design Notes

- Signed ints widen into `i64`, unsigned into `u64`. Narrow-type constructors validate range (e.g., `NewInt8` rejects values outside [-128, 127] — but since Go signatures use typed params like `int8`, this is enforced at compile time).
- `float32` stored in dedicated `f32` field, not widened to `f64`. Preserves bit-exact round-trip.
- The zero-initialized Go struct for `Value` has `kind = Invalid (= 0)`. Equal / Compare / Hash / As* on an Invalid-kind Value panic, making accidental use of `Value{}` a loud error rather than a silent "bool false".
