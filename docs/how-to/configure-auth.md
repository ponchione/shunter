# Configure Auth

This page is a short integration guide. The full current auth contract lives in
[Authentication](../authentication.md).

## Choose A Mode

Shunter currently has two root auth modes:

- `AuthModeDev`: zero-value development mode.
- `AuthModeStrict`: production-oriented JWT validation mode.

Development mode removes setup friction for local apps and tests. Strict mode
is the right starting point for public protocol serving.

## Development Mode

Development mode is the default:

```go
cfg := shunter.Config{
	DataDir: "./data/chat",
}
```

In dev mode:

- protocol clients without a token are allowed
- Shunter can mint anonymous development tokens
- local reducer, procedure, and declared-read calls allow all permissions
  unless the caller explicitly supplies permissions

Do not use dev mode as the production policy for a public service.

## Strict Mode

Strict mode validates bearer tokens for protocol connections.

```go
cfg := shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	AuthMode:       shunter.AuthModeStrict,
	AuthSigningKey: signingKey,
	AuthIssuers:    []string{"https://issuer.example"},
	AuthAudiences:  []string{"chat"},
}
```

The `AuthSigningKey` path validates HS256 JWTs. Tokens must include `iss` and
`sub`. When configured, issuer and audience are checked.

For RS256 or ES256 tokens, configure local verification keys instead of an HMAC
signing key:

```go
cfg := shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	AuthMode:       shunter.AuthModeStrict,
	AuthVerificationKeys: []shunter.AuthVerificationKey{
		{
			Algorithm: shunter.AuthAlgorithmRS256,
			KeyID:     "issuer-key-2026-05",
			Key:       issuerPublicKeyPEM,
		},
	},
	AuthIssuers:   []string{"https://issuer.example"},
	AuthAudiences: []string{"chat"},
}
```

`KeyID` matches the token header `kid` during local key rotation. Shunter does
not need a local public key when the issuer exposes a JWKS endpoint:

```go
cfg := shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	AuthMode:       shunter.AuthModeStrict,
	AuthOIDCIssuers: []shunter.AuthOIDCIssuer{
		{
			Issuer:  "https://issuer.example",
			JWKSURL: "https://issuer.example/.well-known/jwks.json",
		},
	},
	AuthIssuers:   []string{"https://issuer.example"},
	AuthAudiences: []string{"chat"},
}
```

`AuthOIDCIssuers` fetches RS256 and ES256 JWKS keys on demand, caches successful
key sets, and refreshes when a presented token uses a `kid` that is not present
in the cached keyed remote set. Remote JWKS URLs must use HTTPS, except
loopback HTTP URLs used by local tests and development tooling. Keep
`AuthIssuers` and `AuthAudiences` configured; JWKS configuration supplies
signature keys, not claim policy.

### Supabase

Treat Supabase as delegated auth. Your frontend or app code obtains and
refreshes the Supabase access token, then passes it as the existing Shunter
bearer token. Shunter validates the JWT locally; it does not call Supabase Auth
or manage Supabase sessions.

For Supabase projects using asymmetric signing keys, prefer explicit JWKS
configuration:

```go
cfg := shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	AuthMode:       shunter.AuthModeStrict,
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

Allow `anon` as an audience only when the app intentionally admits anonymous
Supabase users. Supabase `role` is not a Shunter permission; the only JWT path
to Shunter permissions is the `permissions` claim.

## Permissions

Declare required permissions on reducers, procedures, and declared reads:

```go
mod.Reducer("send_message", sendMessage, shunter.WithReducerPermissions(
	shunter.PermissionMetadata{Required: []string{"messages:write"}},
))

mod.Procedure("send_system_message", sendSystemMessage,
	shunter.WithProcedurePermissions(
		shunter.PermissionMetadata{Required: []string{"messages:write"}},
	),
)

mod.Query(shunter.QueryDeclaration{
	Name:        "recent_messages",
	SQL:         "SELECT * FROM messages",
	Permissions: shunter.PermissionMetadata{Required: []string{"messages:read"}},
})
```

Protocol strict-mode permissions come from the token's `permissions` claim.
Local reducer callers use `WithPermissions`, local procedure callers use
`WithProcedureCallerPermissions`, and local declared-read callers use
`WithDeclaredReadPermissions`.

In dev mode, local calls allow all permissions when no permission option is
supplied. Once a local caller supplies an explicit permission set, Shunter
checks that set against declared requirements. Strict mode does not provide the
dev allow-all default.

## Extra JWT Claims

Reducers and procedures can inspect an explicit, bounded subset of provider
claims through `ctx.Caller.Principal.Claims`.

```go
cfg.AuthExtraClaims = []string{"email", "role", "session_id", "aal", "is_anonymous"}
cfg.AuthMaxExtraClaimBytes = 4096
cfg.AuthMaxExtraClaimsBytes = 16384
```

Equivalent environment variables:

```text
SHUNTER_AUTH_EXTRA_CLAIMS=email,role,session_id,aal,is_anonymous
SHUNTER_AUTH_MAX_EXTRA_CLAIM_BYTES=4096
SHUNTER_AUTH_MAX_EXTRA_CLAIMS_BYTES=16384
```

Blank `AuthExtraClaims` preserves the narrow principal. Claim names are trimmed,
may use provider or URI-style names, and cannot be empty, duplicated,
control-character-bearing, over 256 bytes, or Shunter-owned (`iss`, `sub`,
`aud`, `exp`, `iat`, `nbf`, `hex_identity`, `permissions`). Missing configured
claims are skipped. Present claims are compact JSON values copied out of the
already parsed JWT claim map.

Extra claims are application context only. They do not expose JWT headers,
`kid`, signatures, bearer tokens, or raw JWT text, and provider claims such as
Supabase `role` are not mapped to Shunter permissions.

## Caller Metadata For Local Calls

Use local call options when an app-owned adapter has already authenticated a
caller outside Shunter's WebSocket protocol.

```go
res, err := rt.CallReducer(
	ctx,
	"send_message",
	payload,
	shunter.WithAuthPrincipal(shunter.AuthPrincipal{
		Issuer:  "https://issuer.example",
		Subject: "user-123",
	}),
	shunter.WithPermissions("messages:write"),
)
```

`WithAuthPrincipal` supplies caller context. It does not bypass permission
checks.

Procedures use procedure-specific caller options:

```go
out, err := rt.CallProcedure(
	ctx,
	"send_system_message",
	payload,
	shunter.WithProcedureAuthPrincipal(shunter.AuthPrincipal{
		Issuer:  "https://issuer.example",
		Subject: "user-123",
	}),
	shunter.WithProcedureCallerPermissions("messages:write"),
)
if err != nil {
	return err
}
_ = out
```

Declared reads use matching options:

```go
result, err := rt.CallQuery(
	ctx,
	"recent_messages",
	shunter.WithDeclaredReadAuthPrincipal(shunter.AuthPrincipal{
		Issuer:  "https://issuer.example",
		Subject: "user-123",
	}),
	shunter.WithDeclaredReadPermissions("messages:read"),
)
if err != nil {
	return err
}
_ = result
```

Use `WithDeclaredReadAllowAllPermissions` only in trusted tests or admin
tooling. For reducer and procedure permission tests, pass explicit permissions
so the runtime exercises the same admission path the app depends on.

## Visibility Filters

Use visibility filters for row-level caller isolation:

```go
mod.VisibilityFilter(shunter.VisibilityFilterDeclaration{
	Name: "own_messages",
	SQL:  "SELECT * FROM messages WHERE owner = :sender",
})
```

The current stable parameter is `:sender`, derived from caller identity.

## Production Checklist

- Set `AuthModeStrict` for public protocol serving.
- Provide a strong `AuthSigningKey` for HS256, configure
  `AuthVerificationKeys` for local HS256/RS256/ES256, or configure
  `AuthOIDCIssuers` for remote RS256/ES256 JWKS verification.
- Configure accepted issuers.
- Configure audiences when tokens are app-scoped.
- Include required permissions in issued tokens.
- Test allowed and denied reducer calls.
- Test allowed and denied declared reads.
- Test visibility-filtered reads.
- Document local key replacement as a restart/deployment event, or document the
  JWKS cache TTL and issuer rotation policy when using remote keys.
