# Production Auth

Status: open, strict HS256 issuer/audience/future-token support landed; broader
auth remains
Owner: unassigned
Scope: production-ready authentication, principal derivation, permission
mapping, and operational auth documentation for Shunter v1.

## Goal

Turn Shunter's current auth foundation into a strict, documented production
mode that app operators can configure safely.

For v1, auth should answer:

- How is a principal derived?
- Which token issuers and audiences are accepted?
- How are permissions mapped?
- How are anonymous identities handled?
- How are signing keys rotated?
- What is the failure behavior for missing, expired, malformed, or unauthorized
  tokens?

## Current State

Shunter has JWT validation, identity derivation, protocol auth handling, table
read policy, reducer permissions, declared read permissions, visibility filters,
and dev/strict auth modes. This is a strong base, but production operation
still needs a clearer strict-mode contract and likely broader token validation
support.

Current code reality:

- Root `Config` exposes `AuthMode`, `AuthSigningKey`, `AuthAudiences`, and dev
  anonymous-token settings.
- `AuthModeStrict` requires a non-empty signing key when protocol auth config is
  built.
- JWT validation currently accepts HS256 signed tokens and optional audience
  and issuer allowlists. It derives identity from `(iss, sub)` and carries
  optional `permissions` claims into the caller principal.
- Dev mode can mint anonymous tokens with an ephemeral or configured signing
  key. This is still the zero-value convenience path.
- There is no asymmetric key support, JWKS/OIDC discovery, key-rotation cache,
  or app-provided claim mapper in the current root config.

The current `AuthModeDev` behavior is useful for local development. It should
remain easy, but v1 must prevent accidental production use of dev auth.

## v1 Decisions To Make

1. Decide required fields for strict auth configuration.
2. Decide supported signing algorithms:
   - keep symmetric signing key only
   - add asymmetric key support
   - add JWKS/OIDC discovery
3. Decide key rotation behavior and cache lifetime.
4. Decide issuer and audience validation rules.
5. Decide claim-to-permission mapping:
   - static claim names
   - app-provided mapper
   - both
6. Decide anonymous-token behavior in strict mode.
7. Decide how auth errors are represented in protocol responses and local APIs.
8. Decide whether table visibility filters may depend only on sender identity
   or also on token claims.

## Implementation Work

Completed or partially complete:

- Audit `auth`, `protocol`, root `Config`, reducer permissions, declared read
  permissions, and visibility filtering as one auth flow.
- Add strict-mode config validation that fails protocol auth setup when signing
  config is incomplete.
- Add root `AuthIssuers` and strict JWT issuer allowlist validation.
- Document current dev/strict auth behavior, principal derivation, permission
  mapping, visibility-filter limits, key replacement, and production checklist
  in `docs/authentication.md`.
- Add coverage for JWT validation, issuer and audience validation, expired,
  future-issued, not-yet-valid, malformed, wrong-algorithm, bad-signature, and
  missing-claim tokens, protocol upgrade auth, local strict permissions,
  declared-read permissions, read authorization, and visibility-filtered reads.

Remaining:

- Decide whether HS256-only strict auth is sufficient for v1 or whether
  asymmetric/JWKS/OIDC support must land before release.
- Decide whether key replacement remains restart-based or gains runtime
  multi-key rotation.
- Decide whether claim-to-permission mapping stays on the `permissions` claim
  or gains an app-provided mapper.
- Confirm any remaining tests needed to prove auth is enforced consistently
  across:
  - WebSocket reducer calls
  - local `CallReducer`
  - one-off raw SQL
  - declared queries
  - raw subscriptions
  - declared views
- Add examples to the reference app using realistic non-dev auth.

## Verification

Run targeted auth/protocol/runtime tests, then:

```bash
rtk go test ./...
rtk go vet ./...
```

If token parsing or crypto dependencies change, also run pinned static analysis:

```bash
rtk go tool staticcheck ./...
```

## Done Criteria

- Strict auth fails closed by default.
- Dev auth is clearly labeled and hard to enable accidentally in production.
- Principal derivation and permission mapping are documented.
- Auth behavior is consistent across local and protocol entry points.
- Key rotation or key replacement behavior is documented and tested.
- The reference app demonstrates the recommended production pattern.

## Non-Goals

- Owning a full identity provider.
- Cloud account management.
- SpacetimeDB auth compatibility.
- Application-specific authorization policy beyond Shunter's permission and
  visibility hooks.
