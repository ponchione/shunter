# Story 2.2: JWT Validation

**Epic:** [Epic 2 — Authentication & Identity](EPIC.md)
**Spec ref:** SPEC-005 §4.1, §4.3
**Depends on:** Story 2.1
**Blocks:** Epic 3 (auth during WebSocket upgrade)

---

## Summary

Validate incoming JWT tokens. Extract claims, verify signature, check expiry, cross-check `hex_identity` if present.

## Deliverables

- `JWTConfig` struct:
  ```go
  type JWTConfig struct {
      SigningKey  []byte   // symmetric key or public key bytes
      Audiences  []string // accepted audience values; empty = skip audience validation
      AuthMode   AuthMode // Strict or Anonymous
  }
  ```

- `AuthMode` enum:
  ```go
  type AuthMode uint8
  const (
      AuthModeStrict    AuthMode = iota
      AuthModeAnonymous
  )
  ```

- `Claims` struct:
  ```go
  type Claims struct {
      Subject     string
      Issuer      string
      Audience    []string
      ExpiresAt   *time.Time // nil if not set
      IssuedAt    time.Time
      HexIdentity string     // optional; empty if not present
  }
  ```

- `func ValidateJWT(tokenString string, config *JWTConfig) (*Claims, error)` — validates signature, extracts claims, checks expiry, validates audience (if configured), cross-checks `hex_identity`

- `func (c *Claims) DeriveIdentity() Identity` — calls `DeriveIdentity(c.Issuer, c.Subject)`

- Validation errors (returned before WebSocket upgrade as HTTP 401):
  - Invalid signature
  - Expired token (`exp` in the past)
  - `hex_identity` present but does not match `DeriveIdentity(iss, sub).Hex()`
  - Missing required claims (`sub`, `iss`)

## Acceptance Criteria

- [ ] Valid token with all claims → `Claims` populated correctly
- [ ] Valid token without `exp` → accepted, `ExpiresAt` is nil
- [ ] Expired token → error
- [ ] Bad signature → error
- [ ] Missing `sub` → error
- [ ] Missing `iss` → error
- [ ] `hex_identity` present, matches → accepted
- [ ] `hex_identity` present, mismatched → error
- [ ] `hex_identity` absent → accepted (no cross-check)
- [ ] Audience validation: token `aud` matches configured → accepted
- [ ] Audience validation: token `aud` does not match → error
- [ ] Audience validation disabled (empty config) → any `aud` accepted

## Design Notes

- Use a standard JWT library (e.g., `golang-jwt/jwt/v5`). Don't hand-roll JWT parsing.
- `hex_identity` is a redundant safety claim. When present it must match, but it's not required.
- Audience validation is optional per spec: "v1 servers MAY validate this against configured accepted audiences." Controlled by `JWTConfig.Audiences`.
