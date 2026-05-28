# Config Reference

Use Go doc for exact field types. This page explains practical app-author
intent for the root runtime config.

## Core Fields

| Field | Purpose | Normal guidance |
| --- | --- | --- |
| `DataDir` | Runtime-owned durable state directory. | Set explicitly for real apps. Use `t.TempDir()` in tests. |
| `ExecutorQueueCapacity` | Reducer executor queue capacity. | Leave zero unless load testing shows a need. |
| `DurabilityQueueCapacity` | Durability worker queue capacity. | Leave zero unless load testing shows a need. |
| `EnableProtocol` | Enables WebSocket protocol serving. | Set true for external protocol clients. |
| `ListenAddr` | Address used by `Runtime.ListenAndServe`. | Set when Shunter owns the HTTP server lifecycle. |
| `AuthMode` | Development or strict auth behavior. | Use zero-value dev mode for local work, strict mode for public serving. |
| `SubscriptionMaxMultiJoinRelations` | Optional live multi-way join relation-count limit. | Leave zero for compatibility; set before admitting untrusted/high-cardinality live views. |
| `SubscriptionMaxMultiJoinRowsPerRelation` | Optional committed input-row limit for each live multi-way join relation. | Leave zero for compatibility; set from measured production envelopes. |

Zero queue capacities are normalized to conservative non-zero defaults by the
runtime. Zero subscription multi-way join limits mean unlimited. Negative
subscription multi-way join limits are rejected during `Build`.

## Auth Fields

| Field | Purpose |
| --- | --- |
| `AuthSigningKey` | Legacy HS256 token signing/verification key. Required for strict protocol serving unless `AuthVerificationKeys`, `AuthOIDCIssuers`, or `AuthOIDCDiscoveryIssuers` is configured. |
| `AuthVerificationKeys` | Local JWT verification keys for HS256, RS256, or ES256, with optional `KeyID`/`kid` matching for rotation. |
| `AuthOIDCIssuers` | Explicit remote JWKS verification sources for RS256 or ES256 JWTs. This config supplies keys, not issuer/audience policy. |
| `AuthOIDCDiscoveryIssuers` | Generic OIDC discovery-document sources that resolve to JWKS verification sources. This config supplies keys, not issuer/audience policy. |
| `AuthIssuers` | Accepted JWT issuer values when non-empty. |
| `AuthAudiences` | Accepted JWT audience values when non-empty. |
| `AuthExtraClaims` | Optional allowlist of extra JWT claim names copied into reducer/procedure caller principals as compact JSON. |
| `AuthMaxExtraClaimBytes` | Per-extra-claim compact JSON byte limit. Zero defaults to 4096. |
| `AuthMaxExtraClaimsBytes` | Total compact JSON byte limit for all preserved extra claims. Zero defaults to 16384. |
| `AnonymousTokenIssuer` | Development anonymous-token issuer override. |
| `AnonymousTokenAudience` | Development anonymous-token audience override. |
| `AnonymousTokenTTL` | Development anonymous-token lifetime override. |

See [Authentication](../authentication.md) and
[Configure auth](../how-to/configure-auth.md) before using strict mode in a
public service.

Extra claim names are trimmed and validated at startup. They may use provider
or URI-style names, but cannot be empty, duplicated, over 256 bytes, contain
control characters, or name Shunter-owned claims: `iss`, `sub`, `aud`, `exp`,
`iat`, `nbf`, `hex_identity`, or `permissions`. Extra claims are caller context
only; they do not grant permissions.

`ConfigFromEnv` maps these auth variables:

| Environment variable | Config field |
| --- | --- |
| `SHUNTER_AUTH_MODE` | `AuthMode` |
| `SHUNTER_AUTH_SIGNING_KEY` | `AuthSigningKey` |
| `SHUNTER_AUTH_ISSUERS` | `AuthIssuers` |
| `SHUNTER_AUTH_AUDIENCES` | `AuthAudiences` |
| `SHUNTER_AUTH_OIDC_ISSUERS` | `AuthOIDCIssuers` as `issuer,jwks-url;issuer,jwks-url` |
| `SHUNTER_AUTH_OIDC_DISCOVERY_ISSUERS` | `AuthOIDCDiscoveryIssuers` as `issuer;issuer,discovery-url` |
| `SHUNTER_AUTH_EXTRA_CLAIMS` | `AuthExtraClaims` |
| `SHUNTER_AUTH_MAX_EXTRA_CLAIM_BYTES` | `AuthMaxExtraClaimBytes` |
| `SHUNTER_AUTH_MAX_EXTRA_CLAIMS_BYTES` | `AuthMaxExtraClaimsBytes` |

OIDC discovery env entries configure key discovery only. They do not add
issuer or audience policy; keep `SHUNTER_AUTH_ISSUERS` and
`SHUNTER_AUTH_AUDIENCES` explicit for strict deployments.

Extra-claim byte-limit env vars are decimal integers. Unset or zero values use
the 4096-byte per-claim and 16384-byte total defaults; negative values fail
`ConfigFromEnvE`.

## Protocol Field

`Protocol` contains WebSocket tuning:

- `PingInterval`
- `IdleTimeout`
- `CloseHandshakeTimeout`
- `WriteTimeout`
- `DisconnectTimeout`
- `OutgoingBufferMessages`
- `IncomingQueueMessages`
- `MaxMessageSize`

Zero values use protocol package defaults. Set these only when you are tuning a
measured serving workload or enforcing an application-specific message limit.
`WriteTimeout` bounds each server-to-client WebSocket data write, which keeps a
slow reader from blocking unrelated outbound delivery indefinitely.

## Observability Field

`Observability` configures runtime-scoped logs, metrics, diagnostics, and
tracing. The zero value is a no-op for external observations.

Use this field when the app wants structured runtime logs, custom metrics
recording, diagnostic HTTP endpoints, or tracing integration.

## Common Configs

Local embedded runtime:

```go
cfg := shunter.Config{
	DataDir: "./data/dev",
}
```

Protocol-serving development runtime:

```go
cfg := shunter.Config{
	DataDir:        "./data/dev",
	EnableProtocol: true,
	ListenAddr:     "127.0.0.1:3000",
}
```

Strict protocol runtime with HS256:

```go
cfg := shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	ListenAddr:     "127.0.0.1:3000",
	AuthMode:       shunter.AuthModeStrict,
	AuthSigningKey: signingKey,
	AuthIssuers:    []string{"https://issuer.example"},
	AuthAudiences:  []string{"chat"},
}
```

Strict protocol runtime with RS256:

```go
cfg := shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	ListenAddr:     "127.0.0.1:3000",
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

Strict protocol runtime with Supabase asymmetric signing keys:

```go
cfg := shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	ListenAddr:     "127.0.0.1:3000",
	AuthMode:       shunter.AuthModeStrict,
	AuthOIDCIssuers: []shunter.AuthOIDCIssuer{
		{
			Issuer:  "https://<project-ref>.supabase.co/auth/v1",
			JWKSURL: "https://<project-ref>.supabase.co/auth/v1/.well-known/jwks.json",
		},
	},
	AuthIssuers:             []string{"https://<project-ref>.supabase.co/auth/v1"},
	AuthAudiences:           []string{"authenticated"},
	AuthExtraClaims:         []string{"email", "role", "session_id", "aal", "is_anonymous"},
	AuthMaxExtraClaimBytes:  4096,
	AuthMaxExtraClaimsBytes: 16384,
}
```

Supabase remains the delegated auth provider. Shunter validates the externally
issued JWT, enforces `AuthIssuers` and `AuthAudiences`, and does not map
Supabase `role` into Shunter permissions.

Strict protocol runtime with generic OIDC discovery:

```go
cfg := shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	ListenAddr:     "127.0.0.1:3000",
	AuthMode:       shunter.AuthModeStrict,
	AuthOIDCDiscoveryIssuers: []shunter.AuthOIDCDiscoveryIssuer{
		{
			Issuer: "https://issuer.example",
		},
	},
	AuthIssuers:   []string{"https://issuer.example"},
	AuthAudiences: []string{"chat"},
}
```

When `DiscoveryURL` is blank, the default is
`<issuer>/.well-known/openid-configuration` for URL issuers. Non-URL issuers
require an explicit discovery URL. Explicit JWKS remains supported and is the
preferred Supabase asymmetric-signing-key configuration path. Discovery does
not add provider SDK behavior; Shunter still does not manage login/logout,
sessions, refresh tokens, or provider user lifecycle.
