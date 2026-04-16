# Story 2.1: Identity Derivation & Canonical String Form

**Epic:** [Epic 2 — Authentication & Identity](EPIC.md)
**Spec ref:** SPEC-005 §4.1; canonical `Identity` type declaration in SPEC-001 §2.4 (`types/types.go`)
**Depends on:** SPEC-001 Story 1.6 (named ID types — provides `Identity`)
**Blocks:** Stories 2.2, 2.3

---

## Summary

`Identity` is the engine-wide 32-byte opaque client identifier. The type itself is declared in `types/types.go` (SPEC-001 §2.4). This story owns the derivation-and-string-form helpers consumed by the protocol layer; it does **not** redeclare the type.

## Deliverables

Helpers declared in `types/identity.go`, operating on the `types.Identity` declared by SPEC-001:

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
- The type itself lives in `types/types.go` (SPEC-001 §2.4). This story only defines the derivation and canonical-string helpers alongside it in `types/identity.go`. SPEC-005 §15 OQ#4 closes on that split.
- Zero Identity is not explicitly rejected by the spec (unlike zero ConnectionID), but `IsZero()` is useful for defensive checks.
