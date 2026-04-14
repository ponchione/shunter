# Epic 2: Authentication & Identity

**Parent:** [SPEC-005-protocol.md](../SPEC-005-protocol.md) §4
**Blocked by:** Nothing — leaf epic
**Blocks:** Epic 3 (WebSocket Transport — auth during upgrade)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 2.1 | [story-2.1-identity-type.md](story-2.1-identity-type.md) | Identity 32-byte type, derivation from (iss, sub), canonical string form |
| 2.2 | [story-2.2-jwt-validation.md](story-2.2-jwt-validation.md) | JWT signature verification, claim extraction, expiry check, hex_identity cross-check |
| 2.3 | [story-2.3-anonymous-token-minting.md](story-2.3-anonymous-token-minting.md) | Generate fresh Identity, sign local JWT, configurable issuer/audience/expiry |

## Implementation Order

```
Story 2.1 (Identity type)
  ├── Story 2.2 (JWT validation)
  └── Story 2.3 (Anonymous minting) — parallel with 2.2
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 2.1 | `auth/identity.go`, `auth/identity_test.go` |
| 2.2 | `auth/jwt.go`, `auth/jwt_test.go` |
| 2.3 | `auth/mint.go`, `auth/mint_test.go` |
