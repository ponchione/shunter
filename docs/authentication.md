# Shunter Authentication

Shunter authentication is intentionally small today. The root runtime supports a
development mode that can mint anonymous protocol identities and a strict mode
that requires signed JWTs for protocol connections.

## Modes

### Dev Mode

`AuthModeDev` is the zero-value root auth mode. It is intended for local
development and tests.

Behavior:

- Protocol connections without a token are allowed.
- The server mints an anonymous HS256 JWT for missing-token protocol clients.
- If `AuthSigningKey` is empty, Shunter generates an ephemeral in-process
  signing key for that runtime.
- Presented tokens are validated against `AuthSigningKey` and any configured
  `AuthVerificationKeys`.
- `AnonymousTokenIssuer` defaults to `shunter-dev`.
- `AnonymousTokenAudience` defaults to the first configured `AuthAudiences`
  value, or `shunter-dev` when no audience is configured.
- Local reducer, procedure, and declared-read calls in dev mode allow all
  permissions unless the caller explicitly supplies permissions.

Do not use `AuthModeDev` as the production policy for a public service. Its
purpose is to remove auth setup friction while building an app.

### Strict Mode

`AuthModeStrict` is the production-oriented mode currently implemented by the
root runtime.

Behavior:

- Protocol connections must present a bearer token.
- Either `AuthSigningKey`, at least one `AuthVerificationKeys` entry, at least
  one `AuthOIDCIssuers` entry, or at least one `AuthOIDCDiscoveryIssuers`
  entry is required when protocol serving is enabled.
- `AuthSigningKey` validates legacy HS256 JWTs.
- `AuthVerificationKeys` validates local HS256, RS256, or ES256 keys. RS256 and
  ES256 keys are PEM-encoded public keys or certificates.
- `AuthOIDCIssuers` validates RS256 or ES256 tokens against remote JWKS
  documents for configured trusted issuers. Remote JWKS URLs must use HTTPS,
  except loopback HTTP URLs used by local tests and development tooling.
- `AuthOIDCDiscoveryIssuers` fetches OIDC discovery documents on demand,
  validates their issuer and `jwks_uri`, then uses the discovered JWKS URL for
  the same RS256/ES256 verification path.
- Remote JWKS and discovery sources are key sources only. They are selected by
  the token issuer during verification, but they do not populate `AuthIssuers`
  or `AuthAudiences`; configure those allowlists explicitly for issuer and
  audience policy.
- A verification key's `KeyID` matches the token header `kid` for overlapping
  rotation. If a token supplies `kid`, keyed matches are preferred; unkeyed
  keys remain a fallback for legacy HS256 configurations.
- `sub` and `iss` claims are required.
- `iss` is checked when `AuthIssuers` is non-empty.
- `aud` is checked only when `AuthAudiences` is non-empty.
- `permissions` may be a string or string list and is copied into the caller
  principal and protocol connection.
- Configured extra JWT claims are copied into reducer and procedure caller
  principals as compact JSON values, bounded by configured-claim count, JSON
  value type/depth, per-claim bytes, and total bytes. Blank extra-claim
  configuration preserves the previous narrow principal.
- Missing, expired, future-issued, not-yet-valid, wrong-algorithm,
  bad-signature, audience-mismatched, missing-claim, or otherwise invalid
  tokens are rejected during protocol admission. When the client offers a
  supported Shunter WebSocket subprotocol, Shunter completes the upgrade and
  closes with WebSocket code 1008 and reason
  `auth-token rejected by admission` so browser clients can classify the
  failure. Syntactically malformed `Authorization` headers remain HTTP 401
  before upgrade.
- Local reducer, procedure, and declared-read calls do not receive the dev-mode
  allow-all permission bypass by default.

JWKS keys are fetched on demand, cached, and refreshed when a token presents a
`kid` that is not present in the cached keyed remote set. OIDC discovery
documents are also fetched on demand and cached; Shunter does not run
background remote key or discovery refresh workers.

## Delegated Providers And Supabase

Shunter delegates authentication to external issuers. Browser or app code owns
login, logout, sessions, refresh tokens, MFA, user lifecycle, and provider API
calls. Shunter validates externally issued JWTs and exposes only normalized
caller context to app code.

Supabase Auth fits this model as an external JWT issuer. Hosted Supabase
deployments using asymmetric signing keys should configure explicit JWKS
verification:

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

`AuthOIDCIssuers` supplies explicit JWKS verification keys and remains the
preferred Supabase asymmetric-signing-key path. `AuthOIDCDiscoveryIssuers` is
generic IdP key-discovery support for providers where discovery is desired.
`AuthIssuers` and `AuthAudiences` remain the issuer and audience policy in both
cases. Allow Supabase `anon` as an audience only for apps that intentionally
admit anonymous users.

Supabase `role` is not a Shunter permission. Shunter permission checks use only
the `permissions` JWT claim or local caller permission options.

## Principal And Identity

Protocol identity is derived from the validated `(iss, sub)` pair. If a token
includes `hex_identity`, Shunter checks that it matches the derived identity.

Reducers receive caller metadata through `ReducerContext.Caller`. Local callers
can supply equivalent metadata with options such as `WithIdentity`,
`WithAuthPrincipal`, and `WithPermissions`.

`Module.Version(...)` and Shunter build metadata are unrelated to authentication
claims. They should not be used as issuer, subject, or permission values.

## Extra Claims

Apps can opt into bounded extra JWT claims with `Config.AuthExtraClaims` or
`SHUNTER_AUTH_EXTRA_CLAIMS`. Values appear at
`ctx.Caller.Principal.Claims` for reducers and procedures, and can be read with
`AuthClaims.Get(name)`.

```go
cfg.AuthExtraClaims = []string{"email", "role", "session_id", "aal", "is_anonymous"}
cfg.AuthMaxExtraClaimBytes = 4096
cfg.AuthMaxExtraClaimsBytes = 16384
```

At most 32 claim names can be configured. Claim names are trimmed and must be
non-empty, unique, no more than 256 bytes, free of control characters, and not
Shunter-owned claims such as `iss`, `sub`, `aud`, `exp`, `iat`, `nbf`,
`hex_identity`, or `permissions`. Missing configured claims are skipped. Values
must be JSON scalar, object, or array values with nesting no deeper than 16
levels. Preserved values are re-marshaled from the parsed JWT claim map into
compact JSON and copied on access. The byte-limit env vars are decimal integers;
unset or zero values use the 4096-byte per-claim and 16384-byte total defaults,
and negative values fail configuration validation.

Extra claims do not expose JWT headers, `kid`, signatures, bearer tokens, or raw
JWT text. They also do not affect permission checks.

## Permissions

Permissions are simple string tags. They can be declared on:

- reducers with `WithReducerPermissions`
- procedures with `WithProcedurePermissions`
- declared queries and views with `PermissionMetadata`
- table read policies with schema table options such as
  `schema.WithReadPermissions`

Protocol strict-mode permissions come from the token's `permissions` claim.
Local callers must supply permissions explicitly with `WithPermissions`,
`WithProcedureCallerPermissions`, or `WithDeclaredReadPermissions`, unless they
intentionally use a dev/admin allow-all option in tests or trusted local
tooling.

Principal permissions are copied into `AuthPrincipal` for app code to inspect,
but Shunter's admission checks use the caller permission set propagated through
the runtime path.

## Visibility

Visibility filters are SQL filters that narrow rows before read evaluation or
live delivery. The current stable parameter is `:sender`, derived from the
caller identity. Visibility filters should not depend on arbitrary token claims
unless an auth extension explicitly adds that surface.

Example:

```go
mod.VisibilityFilter(shunter.VisibilityFilterDeclaration{
	Name: "own_tasks",
	SQL:  "SELECT * FROM tasks WHERE owner_identity = :sender",
})
```

## Key Replacement

`AuthVerificationKeys` supports overlapping local key rotation. Configure both
old and new public keys or HMAC secrets during the overlap window, give keyed
tokens a stable `kid`, then remove retired keys in a later deployment.

For remote key rotation, configure `AuthOIDCIssuers` with each trusted issuer
and explicit JWKS URL, or configure `AuthOIDCDiscoveryIssuers` for a generic
OIDC provider when you want Shunter to discover the JWKS URL. Shunter fetches
JWKS documents when validating matching RS256 or ES256 tokens, caches
successful key sets for the configured TTL, and refreshes a source immediately
when a token's `kid` is not present in the cached keyed set. Cached keys remain
usable until their TTL expires.

Discovery uses `<issuer>/.well-known/openid-configuration` when
`DiscoveryURL` is blank and the issuer is a URL. Non-URL issuer strings require
an explicit discovery URL. Discovery and JWKS URLs must use HTTPS except for
loopback HTTP used in local development and tests.

Local `AuthVerificationKeys` remain useful when deployments want key material
fully controlled by app configuration. `AuthSigningKey` remains the legacy
HS256 path and should not be used for third-party OIDC providers.

## Failure Behavior

- Missing token in strict mode with a supported Shunter subprotocol: WebSocket
  close 1008 with reason `auth-token rejected by admission`.
- Invalid token with a supported Shunter subprotocol: WebSocket close 1008 with
  reason `auth-token rejected by admission`.
- Syntactically malformed `Authorization` header: HTTP 401 before WebSocket
  upgrade.
- Bad protocol auth configuration: startup or protocol graph construction
  fails with an actionable error.
- Missing reducer permission: reducer call is rejected as a permission failure.
- Missing declared-read or table-read permission: query/subscription admission
  fails before returning rows or registering a subscription.
- Visibility-filtered reads return only rows visible to the caller identity.

## Production Checklist

Before deploying strict auth:

1. Set `AuthMode: shunter.AuthModeStrict`.
2. Provide a strong `AuthSigningKey` for HS256 tokens, configure
   `AuthVerificationKeys` for local HS256, RS256, or ES256 verification, or
   configure `AuthOIDCIssuers` for explicit remote RS256/ES256 JWKS
   verification. Use `AuthOIDCDiscoveryIssuers` only when a generic IdP
   discovery document should supply the JWKS URL.
3. Configure `AuthIssuers` to the accepted token issuer values.
4. Configure `AuthAudiences` when tokens should be scoped to this app.
5. Ensure issued tokens contain `iss`, `sub`, and any required `permissions`.
6. Keep token TTL and refresh policy in the application or identity provider.
7. Test reducer, declared-read, raw subscription, raw query, and visibility
   behavior with allowed and denied callers.
8. Document local key replacement as a restart/deployment event, or document
   the JWKS cache TTL and issuer rotation policy when using remote keys.

## Unsupported In Current Strict Mode

- background remote key refresh
- app-provided claim-to-permission mappers
- anonymous-token minting in strict mode
- arbitrary token claims in visibility-filter SQL
