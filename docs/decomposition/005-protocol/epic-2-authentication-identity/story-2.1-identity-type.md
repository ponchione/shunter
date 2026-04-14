# Story 2.1: Identity Type & Derivation

**Epic:** [Epic 2 — Authentication & Identity](EPIC.md)
**Spec ref:** SPEC-005 §4.1
**Depends on:** Nothing
**Blocks:** Stories 2.2, 2.3

---

## Summary

`Identity` is a 32-byte opaque identifier for a client. Derived deterministically from `(iss, sub)` JWT claims. Same pair always maps to same Identity.

## Deliverables

- `Identity` type:
  ```go
  type Identity [32]byte
  ```

- `func DeriveIdentity(issuer, subject string) Identity` — deterministic derivation. The same `(iss, sub)` pair always produces the same 32-byte Identity.

- `func (id Identity) IsZero() bool` — checks if all bytes are zero

- `func (id Identity) Hex() string` — canonical hex string form (64 lowercase hex chars)

- `func ParseIdentityHex(s string) (Identity, error)` — parse from hex string

- Derivation algorithm: hash `(issuer, subject)` with a collision-resistant hash (e.g., SHA-256 or BLAKE3) to produce 32 bytes. The exact algorithm is an implementation choice, but must be deterministic and stable.

## Acceptance Criteria

- [ ] Same `(iss, sub)` → same Identity across calls
- [ ] Different `(iss, sub)` → different Identity
- [ ] `("a", "b")` ≠ `("ab", "")` — issuer/subject boundary not ambiguous
- [ ] `Hex()` round-trips through `ParseIdentityHex`
- [ ] `IsZero()` returns true for zero Identity, false for non-zero
- [ ] `ParseIdentityHex` rejects wrong-length strings
- [ ] `ParseIdentityHex` rejects non-hex characters

## Design Notes

- Derivation must include a separator or length-prefix between issuer and subject to prevent `("a", "bc")` colliding with `("ab", "c")`. A simple approach: `SHA-256(len(issuer) || issuer || subject)` where `len` is encoded as a fixed-width integer.
- The spec notes: "Its exact derivation and canonical string form MUST be defined in the shared identity type spec." For now, this is the definition. If a shared identity spec is written later, this type moves there and the protocol layer imports it.
- Zero Identity is not explicitly rejected by the spec (unlike zero ConnectionID), but `IsZero()` is useful for defensive checks.
