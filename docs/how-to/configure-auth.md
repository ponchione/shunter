# Configure Auth

Status: current v1 app-author guidance
Scope: choosing dev or strict auth mode for local calls and protocol serving.

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
- local reducer and declared-read calls allow all permissions unless the caller
  explicitly supplies permissions

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
not fetch JWKS/OIDC keys; load the accepted public keys from app configuration.

## Permissions

Declare required permissions on reducers and declared reads:

```go
mod.Reducer("send_message", sendMessage, shunter.WithReducerPermissions(
	shunter.PermissionMetadata{Required: []string{"messages:write"}},
))

mod.Query(shunter.QueryDeclaration{
	Name:        "recent_messages",
	SQL:         "SELECT * FROM messages",
	Permissions: shunter.PermissionMetadata{Required: []string{"messages:read"}},
})
```

Protocol strict-mode permissions come from the token's `permissions` claim.
Local callers supply permissions with `WithPermissions` or
`WithDeclaredReadPermissions`.

In dev mode, local calls allow all permissions when no permission option is
supplied. Once a local caller supplies an explicit permission set, Shunter
checks that set against declared requirements. Strict mode does not provide the
dev allow-all default.

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
tooling. For reducer permission tests, pass explicit permissions so the runtime
exercises the same admission path the app depends on.

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
- Provide a strong `AuthSigningKey` for HS256 or configure
  `AuthVerificationKeys` for HS256, RS256, or ES256.
- Configure accepted issuers.
- Configure audiences when tokens are app-scoped.
- Include required permissions in issued tokens.
- Test allowed and denied reducer calls.
- Test allowed and denied declared reads.
- Test visibility-filtered reads.
- Document local key replacement as a restart/deployment event.
