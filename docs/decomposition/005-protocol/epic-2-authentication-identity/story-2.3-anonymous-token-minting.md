# Story 2.3: Anonymous Token Minting

**Epic:** [Epic 2 — Authentication & Identity](EPIC.md)
**Spec ref:** SPEC-005 §4.2
**Depends on:** Story 2.1
**Blocks:** Epic 3 (anonymous mode in upgrade handler)

---

## Summary

When no token is presented and the engine is in anonymous mode, mint a fresh Identity and sign a local JWT for it.

## Deliverables

- `MintConfig` struct:
  ```go
  type MintConfig struct {
      Issuer     string        // local issuer string for minted tokens
      Audience   string        // audience value placed in minted tokens
      SigningKey []byte        // key used to sign minted tokens
      Expiry     time.Duration // 0 = no expiry (omit exp claim)
  }
  ```

- `func MintAnonymousToken(config *MintConfig) (token string, identity Identity, err error)`:
  1. Generate a random `subject` (e.g., UUID or 16 random bytes hex-encoded)
  2. Derive Identity from `(config.Issuer, subject)`
  3. Build JWT with claims: `iss`, `sub`, `aud`, `iat`, optionally `exp`
  4. Sign with `config.SigningKey`
  5. Return signed token string + derived Identity

- Minted tokens must pass `ValidateJWT` (Story 2.2) when presented back on reconnect

## Acceptance Criteria

- [ ] Mint → token is valid JWT that passes `ValidateJWT`
- [ ] Mint → returned Identity matches `DeriveIdentity(issuer, subject)` from token claims
- [ ] Two mints → different subjects, different Identities
- [ ] Minted token with `Expiry > 0` → `exp` claim present
- [ ] Minted token with `Expiry = 0` → no `exp` claim
- [ ] Reconnect with minted token → `ValidateJWT` succeeds, same Identity derived

## Design Notes

- Subject randomness: 16 bytes from `crypto/rand`, hex-encoded = 32-char string. Sufficient entropy to avoid collision.
- The spec requires the server to define issuer, audience, and expiry policy for minted tokens. `MintConfig` exposes these as explicit configuration.
- For production use, minted tokens may have finite lifetimes. For development/testing, no-expiry tokens reduce friction.
