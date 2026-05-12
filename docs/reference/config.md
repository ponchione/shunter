# Config Reference

Status: current v1 reference note
Scope: app-facing `shunter.Config` fields and when to set them.

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

Zero queue capacities are normalized to conservative non-zero defaults by the
runtime.

## Auth Fields

| Field | Purpose |
| --- | --- |
| `AuthSigningKey` | HS256 token signing/verification key. Required for strict protocol serving. |
| `AuthIssuers` | Accepted JWT issuer values when non-empty. |
| `AuthAudiences` | Accepted JWT audience values when non-empty. |
| `AnonymousTokenIssuer` | Development anonymous-token issuer override. |
| `AnonymousTokenAudience` | Development anonymous-token audience override. |
| `AnonymousTokenTTL` | Development anonymous-token lifetime override. |

See [Authentication](../authentication.md) and
[Configure auth](../how-to/configure-auth.md) before using strict mode in a
public service.

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

Strict protocol runtime:

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
