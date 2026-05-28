# Auth Provider And Claims Hardening

Status: actionable implementation slice
Primary backlog items: `deferred-functionality-backlog.md` items 12 and 13

## Purpose

Improve strict-auth usability for real hosted apps without expanding Shunter
into a full identity provider. The actionable slice is:

- OIDC discovery-document lookup for configured issuers
- app-visible, copy-isolated, bounded extra claim context

Auth remains delegated. Shunter validates externally issued JWTs and exposes
bounded caller context to app code. The external identity provider owns
login/logout, sessions, token refresh, MFA, user lifecycle, token issuance, and
key rotation policy.

Background remote refresh remains deferred. Provider-specific session
management, login flows, refresh tokens, and protocol-specific 401 reshaping
remain out of scope unless a concrete hosted app requires them.

## Delegated Auth Boundary

Supabase is a target delegated-auth provider, not a Shunter runtime dependency
or built-in control plane. Treat Supabase Auth as an external JWT issuer:

- Browser/app code obtains and refreshes the Supabase access token.
- Shunter protocol clients pass that token as the existing bearer token.
- Shunter validates the token signature, issuer, audience, and standard JWT
  time semantics locally.
- Reducers/procedures receive only normalized identity, permissions, and
  explicitly configured extra claims.

For current Supabase asymmetric signing keys, the primary Shunter integration
path is explicit JWKS configuration, not OIDC discovery:

```go
cfg := shunter.Config{
	AuthMode: shunter.AuthModeStrict,
	AuthOIDCIssuers: []shunter.AuthOIDCIssuer{
		{
			Issuer:  "https://<project-ref>.supabase.co/auth/v1",
			JWKSURL: "https://<project-ref>.supabase.co/auth/v1/.well-known/jwks.json",
		},
	},
	AuthIssuers:   []string{"https://<project-ref>.supabase.co/auth/v1"},
	AuthAudiences: []string{"authenticated"},
}
```

Allow `anon` as an audience only for apps that intentionally admit anonymous
Supabase users. Legacy Supabase HS256/JWT-secret verification can still use
`AuthSigningKey`, but docs should steer hosted Supabase deployments toward
asymmetric signing keys plus JWKS verification.

OIDC discovery remains useful generic IdP support. It should not replace or
deprioritize explicit JWKS config, because Supabase's documented verification
path exposes a direct JWKS endpoint.

## Current Boundary

Current auth behavior:

- Root `Config` supports dev and strict auth modes.
- Strict auth validates HS256 through `AuthSigningKey`.
- Strict auth supports local `AuthVerificationKeys` for HS256, RS256, and
  ES256.
- Strict auth supports configured `AuthOIDCIssuers`, currently aliases of
  `auth.JWKSConfig` with explicit `Issuer` and `JWKSURL`.
- JWKS fetch is on demand, cached, HTTPS by default, loopback HTTP allowed for
  tests/dev, and refreshes on unknown `kid`.
- Issuer and audience policy stays in `AuthIssuers` and `AuthAudiences`.
- Validated claims normalize to `auth.Claims`.
- Reducers/procedures see `types.CallerContext`, including
  `types.AuthPrincipal`.
- `AuthPrincipal` currently carries `Issuer`, `Subject`, `Audience`, and
  `Permissions`.

Implementation anchors:

- `Config`, `ConfigFromEnvE`, and `parseOIDCIssuerEnv` in `config.go` expose
  only explicit JWKS issuer pairs through `SHUNTER_AUTH_OIDC_ISSUERS`.
- `buildAuthConfig` in `network.go` copies `AuthOIDCIssuers` into
  `auth.JWTConfig.JWKS`; strict mode still requires one of signing key,
  local verification keys, or explicit JWKS issuer config.
- `auth.JWKSConfig`, `validateJWKSConfig`, `keysForJWKS`, and `fetchJWKS` in
  `auth/jwks.go` own bounded HTTPS-by-default JWKS fetch, process-local cache,
  loopback HTTP exception, and unknown-`kid` refresh behavior.
- `auth.ValidateJWT` in `auth/jwt.go` parses through `jwt.MapClaims`, validates
  issuer/audience policy, extracts `permissions`, and returns normalized
  `auth.Claims`.
- `auth.Claims.Principal` returns `types.AuthPrincipal`; `AuthPrincipal.Copy`
  and `CallerContext.Copy` in `types/reducer.go` currently deep-copy only
  slices.
- `protocol.Server.HandleSubscribe`, `executor.ProtocolInboxAdapter`, and
  `Runtime.HandleCallProcedure` are the current protocol-to-reducer/procedure
  propagation path for principal and permissions.
- Existing tests: `auth/jwt_test.go`, `auth/jwks_test.go`,
  `protocol/upgrade_test.go`, `procedure_test.go`, and
  `internal/gauntlettests/read_auth_gauntlet_test.go`.

Exact gaps:

- No OIDC discovery document type, fetcher, cache, or env parser exists.
- `AuthOIDCIssuers` means explicit JWKS URL today; changing that format would
  be a breaking config change.
- `auth.Claims` does not retain the original `jwt.MapClaims`, so extra-claim
  preservation needs a deliberate decoded-claim copy before the function
  returns.
- `types.AuthPrincipal` has no claim field and no map/deep-copy test for raw
  JSON claim values.
- `ConfigFromEnvE` has no `SHUNTER_AUTH_EXTRA_CLAIMS` or byte-limit env
  parsing.
- No protocol frame carries extra claims; this slice should keep claims
  server-side unless a separate client contract is approved.

## Non-Goals

Do not add:

- Shunter-owned login/logout flows
- refresh-token handling
- Supabase SDK calls or Supabase Auth API calls in the runtime hot path
- background JWKS or OIDC refresh workers
- provider-specific claim mapping beyond explicit configuration
- automatic mapping from provider claims such as Supabase `role` to Shunter
  `Permissions`
- raw token/header/signature exposure to reducers
- unbounded arbitrary claim maps
- auth state persistence outside current runtime/client lifecycle
- a new managed auth control plane

## Actionable Outcomes

1. Let operators configure OIDC issuer discovery without manually supplying a
   JWKS URL.
2. Preserve current explicit JWKS configuration for deployments that do not
   want discovery, including Supabase asymmetric-signing-key deployments.
3. Fetch and validate discovery documents with the same security posture as
   JWKS:
   - HTTPS required except loopback dev/test URLs
   - response byte limit
   - strict JSON decoding
   - issuer in document must match configured issuer
   - `jwks_uri` required and validated
   - supported signing algorithms remain RS256 and ES256 unless explicitly
     expanded later
4. Keep discovery on demand and cached. Do not add background refresh.
5. Add app-visible extra claims only through an explicit, bounded,
   copy-isolated surface.

## OIDC Discovery Design

Discovery is additive generic IdP support. Do not make Supabase depend on this
path, and do not change the existing meaning of `AuthOIDCIssuers`.

Recommended API shape:

```go
type OIDCDiscoveryConfig struct {
	Issuer         string
	DiscoveryURL   string
	Algorithms     []JWTAlgorithm
	CacheTTL       time.Duration
	RefreshTimeout time.Duration
}
```

Root config could expose:

```go
AuthOIDCDiscoveryIssuers []AuthOIDCDiscoveryIssuer
```

Alternatively, `auth.JWKSConfig` could grow a `DiscoveryURL` field, but that
risks overloading a type that currently means "issuer plus explicit JWKS URL."
Prefer a separate type if it keeps validation and docs clearer.

Default discovery URL:

```text
<issuer>/.well-known/openid-configuration
```

Only use that default when `DiscoveryURL` is blank and `Issuer` is a valid URL
base. If the issuer string is not a URL, require `DiscoveryURL`.

Discovery document fields needed for this slice:

```json
{
  "issuer": "https://issuer.example",
  "jwks_uri": "https://issuer.example/.well-known/jwks.json",
  "id_token_signing_alg_values_supported": ["RS256", "ES256"]
}
```

The signing-alg list should be treated as advisory capability metadata:

- intersect it with configured allowed algorithms when present
- default to existing RS256/ES256 behavior if the field is absent
- reject documents that only advertise unsupported algorithms

Cache key should include:

- issuer
- discovery URL
- algorithm allowlist
- cache TTL

The resolved discovery result should feed the same JWKS verification path as
explicit `JWKSConfig`.

Supabase-specific docs should show explicit JWKS first. Discovery examples
should use a generic OIDC issuer unless a verified Supabase discovery-document
path is intentionally added later.

## Env Configuration

Current env:

```text
SHUNTER_AUTH_OIDC_ISSUERS=issuer,jwks-url;issuer,jwks-url
```

Supabase asymmetric-signing-key deployments should continue to use this direct
JWKS form:

```text
SHUNTER_AUTH_OIDC_ISSUERS=https://<project-ref>.supabase.co/auth/v1,https://<project-ref>.supabase.co/auth/v1/.well-known/jwks.json
SHUNTER_AUTH_ISSUERS=https://<project-ref>.supabase.co/auth/v1
SHUNTER_AUTH_AUDIENCES=authenticated
```

Add a separate env var to avoid breaking this format:

```text
SHUNTER_AUTH_OIDC_DISCOVERY_ISSUERS=issuer;issuer,discovery-url
```

Rules:

- semicolon separates issuers
- each entry is either `issuer` or `issuer,discovery-url`
- issuer must be non-empty
- discovery URL, when present, must be non-empty
- explicit `SHUNTER_AUTH_OIDC_ISSUERS` continues to mean issuer plus JWKS URL

Docs should call out that issuer/audience allowlists are still configured with:

- `SHUNTER_AUTH_ISSUERS`
- `SHUNTER_AUTH_AUDIENCES`

Do not silently add discovered issuers to the issuer allowlist unless the
runtime deliberately documents that behavior. Safer default: discovery config
adds keys; issuer policy still comes from `AuthIssuers`.

For extra claims, prefer separate env variables so strict-auth key discovery
and reducer-visible context are independently reviewable:

```text
SHUNTER_AUTH_EXTRA_CLAIMS=email,role,session_id,aal,is_anonymous
SHUNTER_AUTH_MAX_EXTRA_CLAIM_BYTES=4096
SHUNTER_AUTH_MAX_EXTRA_CLAIMS_BYTES=16384
```

Blank claim lists preserve today's behavior. Invalid names or byte limits
should fail `ConfigFromEnvE`/startup before serving traffic.

## Extra Claim Context

Current `AuthPrincipal` is narrow and safe:

```go
type AuthPrincipal struct {
	Issuer      string
	Subject     string
	Audience    []string
	Permissions []string
}
```

The richer claim surface should not become a raw token dump. Preferred shape:

```go
type AuthClaims struct {
	Values map[string]json.RawMessage
}

type AuthPrincipal struct {
	Issuer      string
	Subject     string
	Audience    []string
	Permissions []string
	Claims      AuthClaims
}
```

Guardrails:

- extra claims are opt-in by name through config
- standard claims used by Shunter are rejected as extra-claim names in this
  slice: `iss`, `sub`, `aud`, `exp`, `iat`, `nbf`, `hex_identity`, and
  `permissions`
- claim names are trimmed, non-empty, unique, and size-limited
- claim names may use provider/URI-style names; do not restrict them to Go or
  SQL identifier syntax
- each preserved raw JSON claim has a byte limit
- total preserved claim bytes have a limit
- `Copy()` deep-copies the map and raw-message byte slices
- reducers/procedures cannot mutate shared claim state
- malformed or too-large configured claims fail validation before reducer
  execution
- provider claims do not grant Shunter permissions unless the existing
  `permissions` claim or local caller options supply permissions

Possible config:

```go
AuthExtraClaims []string
AuthMaxExtraClaimBytes int
AuthMaxExtraClaimsBytes int
```

Defaults:

- no extra claims preserved
- zero byte-limit config means defaults
- default per-claim limit: 4 KiB
- default total preserved-claims limit: 16 KiB
- negative byte limits are invalid
- byte limits apply to compact JSON values after marshaling from validated
  `jwt.MapClaims`

If adding config fields feels too broad for the first slice, start with the
`AuthClaims` type and preserve no extras until config is added in the same
series.

Useful Supabase extra-claim examples:

- `role`
- `email`
- `session_id`
- `aal`
- `is_anonymous`
- `app_metadata` or `user_metadata` only when the app deliberately accepts
  larger, user-controlled JSON and the byte limits are set accordingly

The claim value type should be immutable-by-convention but copied by API:

```go
func (c AuthClaims) Copy() AuthClaims
func (c AuthClaims) Get(name string) (json.RawMessage, bool)
```

`Get` should return a copied `json.RawMessage` so reducers/procedures cannot
mutate principal state shared across calls.

## Claim Decoding Rules

Use the already parsed JWT map, not token bytes.

Recommended behavior:

- preserve the configured claim values after validation as compact JSON
  re-marshaled from `jwt.MapClaims`; do not promise byte-for-byte preservation
  of the original JWT payload
- skip missing claims
- reject preserved values whose compact JSON exceeds limits
- reject unsupported claim-name configuration at startup
- keep `permissions` extraction as today for permission checks
- keep `aud` normalization as today for audience validation
- do not expose JWT header fields such as `kid`
- do not expose the signature, original bearer token, or raw compact JWT

## Protocol Mapping

This slice does not require a protocol change if claims remain server-side in
reducer/procedure contexts. If the TypeScript client or protocol identity frame
later needs claim metadata, that is a separate client contract decision.

Any auth failure introduced by discovery or claim validation should continue
to map through existing strict-auth failure paths.

## Staging

Stage A: Supabase-aligned reducer/procedure extra claims.

- Add `types.AuthClaims`, copy semantics, and `AuthPrincipal.Claims`.
- Extend `auth.JWTConfig`/root `Config` with explicit claim allowlist and
  limits.
- Preserve configured compact JSON claim values from the already parsed claims
  map.
- Thread copies through protocol admission, local reducer/declared-read/
  procedure options, executor commands, reducers, and procedures.
- Add env parsing and docs for `SHUNTER_AUTH_EXTRA_CLAIMS`,
  `SHUNTER_AUTH_MAX_EXTRA_CLAIM_BYTES`, and
  `SHUNTER_AUTH_MAX_EXTRA_CLAIMS_BYTES`.
- Document Supabase as delegated auth using explicit JWKS plus optional extra
  claims.

Stage B: OIDC discovery.

- Add `auth.OIDCDiscoveryConfig` and root `AuthOIDCDiscoveryIssuers` without
  changing `AuthOIDCIssuers`.
- Resolve discovery documents into existing `auth.JWKSConfig` values.
- Reuse JWKS validation, cache TTL, refresh timeout, and algorithm allowlist
  semantics where possible.
- Add env parsing and docs for `SHUNTER_AUTH_OIDC_DISCOVERY_ISSUERS`.

Stage C: hosted evidence.

- Add one strict-auth hosted runtime test that uses discovery and one reducer
  or procedure assertion that observes a copied extra claim.
- Do not add login/session UX or client-visible claim frames in this slice.

## Risks

- Issuer discovery can be mistaken for issuer authorization; docs and config
  validation must keep key discovery separate from `AuthIssuers` policy.
- Supabase delegated auth can be mistaken for a Shunter-managed auth feature;
  keep runtime docs clear that Shunter validates tokens and never manages
  Supabase sessions.
- Supabase `role` can be mistaken for Shunter permissions; do not auto-map it.
- Discovery/JWKS caches are package-global today, so tests need unique issuer
  and URL values or explicit cache-aware assertions.
- Extra claims can accidentally become a raw token dump; keep allowlists and
  byte limits mandatory for preservation.
- Adding fields to `ModuleContract` or protocol identity frames would expand
  this slice beyond server-side reducer/procedure context.

## Implementation Sequence

1. Add `types.AuthClaims`, copy semantics, and `AuthPrincipal.Claims`.
2. Extend `auth.Claims.Principal` and existing principal copy tests for claim
   deep-copy behavior.
3. Add `auth.JWTConfig` extra-claim allowlist and limit fields, plus validation
   for claim names and byte limits.
4. Preserve configured extra claims in `claimsFromValidatedToken` after JWT
   validation succeeds.
5. Thread root `Config`, `copyConfig`, `ConfigFromEnvE`, `buildAuthConfig`,
   `local.go`, `declared_read.go`, `procedure.go`, `protocol/upgrade.go`, and
   `executor/protocol_inbox_adapter.go`.
6. Add tests proving reducer/procedure contexts receive copies and caller
   mutation cannot corrupt stored state.
7. Add env parsing tests for extra claim names and claim byte limits.
8. Add docs for delegated Supabase auth, explicit Supabase JWKS config, and
   optional extra claims.
9. Add discovery config type and validation in `auth`.
10. Implement discovery fetch:
   - URL construction
   - HTTPS/loopback validation
   - timeout
   - response byte limit
   - strict JSON parse with trailing-value rejection
   - issuer exact-match validation
   - `jwks_uri` validation
11. Cache resolved discovery documents with a key that includes issuer,
   discovery URL, algorithm allowlist, and cache TTL.
12. Convert resolved discovery into JWKS verification sources before
   `resolveJWKSVerificationKeys` runs.
13. Thread discovery root `Config`, `copyConfig`, `ConfigFromEnvE`, and
   `buildAuthConfig`.
14. Add discovery tests with `httptest.Server`:
   - loopback HTTP allowed
   - non-loopback HTTP rejected
   - issuer mismatch rejected
   - missing `jwks_uri` rejected
   - trailing JSON rejected
   - unsupported algorithm rejected
   - discovery result reused from cache
15. Add env parsing tests for discovery issuers.
16. Update docs:
   - `docs/authentication.md`
   - `docs/how-to/configure-auth.md`
   - `docs/reference/config.md`
   - `docs/how-to/host-shunter-backend.md`
   - `CHANGELOG.md`

## Likely Touched Files

- `auth/jwt.go`
- `auth/jwks.go`
- new `auth/oidc_discovery.go`
- `auth/jwt_test.go`
- `auth/jwks_test.go`
- new `auth/oidc_discovery_test.go`
- `types/reducer.go` or a new `types/auth_claims.go`
- `types/reducer_test.go` or a new `types/auth_claims_test.go`
- `config.go`
- `build.go`
- `network.go`
- `local.go`
- `declared_read.go`
- `protocol/upgrade.go`
- `executor/protocol_inbox_adapter.go`
- `procedure.go`
- `docs/authentication.md`
- `docs/how-to/configure-auth.md`
- `docs/reference/config.md`
- `docs/how-to/host-shunter-backend.md`
- `CHANGELOG.md`

## Validation

Targeted:

```bash
rtk go test ./auth ./types ./protocol ./executor .
rtk go vet ./auth ./types ./protocol ./executor .
```

If env/config docs or hosted examples change:

```bash
rtk go test ./cmd/shunter ./examples/hosted-chat/...
rtk bash scripts/hosted-chat-gate.sh
```

Before finishing a claim-context API change:

```bash
rtk go test ./...
rtk go vet ./...
rtk go tool staticcheck ./...
```

## Completion Criteria

This slice is complete when:

- strict auth can verify tokens from a configured OIDC discovery issuer without
  a manually configured JWKS URL
- explicit JWKS configuration still works unchanged
- discovery fetches are bounded, cached, and HTTPS-by-default
- no background refresh worker was added
- reducers and procedures can access configured extra claims through a bounded,
  copy-isolated surface
- Supabase is documented as delegated auth through externally issued JWTs,
  explicit JWKS verification, issuer/audience policy, and optional bounded
  extra claims
- provider claims such as Supabase `role` are not automatically mapped to
  Shunter permissions
- docs explain issuer/audience policy separately from key discovery
