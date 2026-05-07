# Production Auth

Status: open, strict HS256 issuer/audience/future-token support landed; v1
policy and coverage audit documented; external canary example remains
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
and dev/strict auth modes. The v1 strict-mode contract is intentionally narrow
and documented in `docs/authentication.md`.

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
- v1 strict auth is HS256-only. There is no asymmetric key support, JWKS/OIDC
  discovery, key-rotation cache, anonymous-token minting in strict mode, or
  app-provided claim mapper in the current root config.

The current `AuthModeDev` behavior is useful for local development. It should
remain easy, but v1 must prevent accidental production use of dev auth.

## v1 Policy

- `AuthModeStrict` requires `AuthSigningKey` when protocol serving is enabled.
- Tokens must be HS256 JWTs with `iss` and `sub`.
- `AuthIssuers` and `AuthAudiences` are allowlists when configured.
- Permission mapping uses the `permissions` claim only.
- Strict mode does not mint anonymous tokens.
- Key replacement is restart/deployment based; overlapping key rotation is
  outside the v1 runtime contract.
- Visibility filters may depend on `:sender`, not arbitrary token claims.
- Auth failures on protocol upgrade fail before the WebSocket is accepted;
  local permission failures are returned as reducer/read admission failures.

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
- Document the strict-auth coverage audit in `docs/AUTH-COVERAGE.md`.
- Add coverage for JWT validation, issuer and audience validation, expired,
  future-issued, not-yet-valid, malformed, wrong-algorithm, bad-signature, and
  missing-claim tokens, protocol upgrade auth, local strict permissions,
  declared-read permissions, read authorization, and visibility-filtered reads.
- Decide and document the v1 strict-auth policy: HS256-only, restart-based key
  replacement, `permissions` claim mapping, no strict-mode anonymous tokens, and
  `:sender`-only visibility claim exposure.

Remaining:

- Keep the external canary app demonstrating realistic strict non-dev auth.

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
- The external canary app demonstrates the recommended production pattern.

## Non-Goals

- Owning a full identity provider.
- Cloud account management.
- SpacetimeDB auth compatibility.
- Application-specific authorization policy beyond Shunter's permission and
  visibility hooks.
