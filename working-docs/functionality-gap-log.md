# Functionality Gap Log

This log tracks clean-room functionality gaps found while comparing Shunter
packages against the read-only ignored reference tree.

Scope rules:
- Do not copy source, structure, comments, tests, or identifiers from the
  reference tree.
- Do not treat reference-runtime wire compatibility, byte-for-byte format
  compatibility, client interoperability, or source compatibility as goals.
- Log product/runtime capabilities Shunter may need to clone independently.
- Prefer live Shunter code over specs when classifying current behavior.

## 2026-05-13: `bsatn` vs `reference sats crate`

Status: no immediate Shunter-native correctness gap found.

Relevant findings:
- Shunter deliberately uses a Shunter-native tagged row encoding, and the specs
  say it is not byte-compatible with the reference runtime's BSATN. This is not a gap.
- The reference runtime's SATS supports recursive sum/product/array values, refs/typespace,
  option/result forms, raw BSATN equality, raw BSATN hashing, and static-layout
  optimized row serialization.
- Shunter currently supports flat `ProductValue` rows over Shunter `ValueKind`
  values, plus widened scalar conveniences such as `ArrayString`, `UUID`,
  `Duration`, and canonical `JSON`.

Potential follow-up:
- If Shunter needs richer application value shapes, design a Shunter-native
  recursive value system and codec. This should not be framed as reference runtime
  BSATN compatibility.
- If profiling shows decode/copy costs in row fanout, consider independently
  designed raw-row equality/hash helpers.

## 2026-05-13: `auth` vs `reference auth crate` / auth support code

Status: Shunter covers the current documented v1 auth surface, but the
comparison found production-auth capability gaps worth tracking.

Current Shunter behavior:
- `auth.ValidateJWT` validates HS256 JWTs with required `sub` and `iss`,
  optional issuer/audience allowlists, optional `hex_identity` consistency,
  and optional string/string-list `permissions`.
- `auth.MintAnonymousToken` mints anonymous HS256 tokens for dev-mode protocol
  clients.
- `types.AuthPrincipal` exposes normalized issuer, subject, audience, and
  permissions to reducers/local-call surfaces.

Relevant findings:
- P1: Bound accepted `iss` and `sub` sizes. The reference auth path rejects
  overlong issuer/subject strings before constructing identity claims. Shunter
  currently requires non-empty strings but does not cap their length. Add a
  Shunter-owned limit and tests to avoid unbounded claim data flowing into
  identity derivation, logs, metrics, and app callbacks.
- P2: Preserve optional claim context for app code. The reference carries a
  structured claims object plus serialized JWT payload and preserves extra
  claims. Shunter intentionally exposes only normalized principal fields today.
  If apps need richer auth context, design a Shunter-native claim metadata
  surface rather than passing raw token internals everywhere.
- P2: Production key validation remains narrow. The reference runtime has local
  asymmetric signing/validation plus OIDC/JWKS lookup and caching support.
  Shunter docs already call out asymmetric keys, JWKS/OIDC discovery, multiple
  active verification keys, and rotation caches as unsupported. Keep this as a
  tracked product gap for public deployments.
- P3: Identity string ergonomics. The reference runtime's identities include a recognizable
  prefix/checksum scheme and abbreviation helpers. Shunter identities are raw
  32-byte values with fixed lowercase hex parsing/formatting. This is not a
  compatibility issue, but Shunter may eventually want its own typo-resistant
  display/URL identity format.

Potential follow-up:
- Add `MaxIssuerBytes` / `MaxSubjectBytes` constants or config defaults to
  `auth.ValidateJWT`, then add boundary tests.
- Sketch a narrow `AuthClaims` or extended `AuthPrincipal` design before adding
  arbitrary claim propagation.
- Treat asymmetric keys/JWKS/OIDC as a separate auth roadmap slice, not as an
  incremental change to the current HS256-only validator.
