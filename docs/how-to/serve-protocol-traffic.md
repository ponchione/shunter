# Serve Protocol Traffic

Status: current v1 app-author guidance
Scope: serving Shunter's HTTP/WebSocket protocol from an app process.

Enable protocol serving when external clients should talk to Shunter over its
WebSocket protocol. Keep using local runtime APIs for in-process workers,
tests, admin tools, and app-owned HTTP handlers that do not need the protocol.

## Runtime-Owned Server

Use `Runtime.ListenAndServe` when Shunter should own the HTTP server lifecycle.

```go
rt, err := shunter.Build(app.Module(), shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
	ListenAddr:     "127.0.0.1:3000",
})
if err != nil {
	return err
}

if err := rt.ListenAndServe(ctx); err != nil && !errors.Is(err, context.Canceled) {
	return err
}
```

`ListenAndServe` starts the runtime if needed, serves `Runtime.HTTPHandler()`,
and closes runtime ownership when the context is canceled.
Runtime-owned HTTP serving uses defensive read-header and idle timeouts.

## App-Owned HTTP Server

Use `Runtime.HTTPHandler` when your application owns the HTTP server or wants
to mount Shunter beside other routes.

```go
rt, err := shunter.Build(app.Module(), shunter.Config{
	DataDir:        "./data/chat",
	EnableProtocol: true,
})
if err != nil {
	return err
}
defer rt.Close()

if err := rt.Start(ctx); err != nil {
	return err
}

server := &http.Server{
	Addr:              "127.0.0.1:3000",
	Handler:           rt.HTTPHandler(),
	ReadHeaderTimeout: 5 * time.Second,
	IdleTimeout:       60 * time.Second,
}
return server.ListenAndServe()
```

`HTTPHandler` does not start lifecycle by itself. Call `Start` before serving
traffic.

If the app-owned server needs its own middleware, route `rt.HTTPHandler()` under
the desired mount point after the runtime has started. Keep using local
`CallReducer`, `Read`, `CallQuery`, and `SubscribeView` for trusted in-process
work that does not need WebSocket protocol behavior.

For WebSocket write backpressure tuning, use `Config.Protocol.WriteTimeout`.
Zero values use Shunter's protocol defaults.

## Protocol Endpoint

With protocol enabled, the runtime mounts the WebSocket protocol endpoint at
`/subscribe`.

The v1 protocol is Shunter-native. Do not document it as a compatibility layer
for another runtime's wire format.

## Wire Compatibility

Supported WebSocket subprotocol tokens are:

- `v2.bsatn.shunter`: current/default protocol. V2 adds parameterized
  declared-query and declared-view request messages.
- `v1.bsatn.shunter`: minimum supported protocol for existing v1 clients and
  no-parameter declared reads.

Frames are Shunter-native binary frames: one tag byte followed by a BSATN body,
inside the runtime compression envelope when compression is negotiated.
Unknown, unassigned, reserved, malformed, or trailing-byte messages are fatal
protocol errors within the negotiated version. V1 connections reject v2-only
tags instead of widening v1 semantics.

Stable client-to-server message families are `SubscribeSingle`,
`UnsubscribeSingle`, `SubscribeMulti`, `UnsubscribeMulti`, `CallReducer`,
`OneOffQuery`, `DeclaredQuery`, and `SubscribeDeclaredView`. V2 also supports
`DeclaredQueryWithParameters` and `SubscribeDeclaredViewWithParameters`; their
`params` field is a BSATN product row encoded according to the declaration's
parameter schema.

Stable server-to-client message families are `IdentityToken`,
`SubscribeSingleApplied`, `UnsubscribeSingleApplied`,
`SubscribeMultiApplied`, `UnsubscribeMultiApplied`, `SubscriptionError`,
`TransactionUpdate`, `TransactionUpdateLight`, and `OneOffQueryResponse`.

Tag `0` and tags `128` through `255` are reserved in supported protocol
versions. Server tag `7` is reserved for the retired reducer-call result
envelope.

Row batches and subscription updates use Shunter row-list and flat update
payloads. Reference wrapper chains, energy fields, and SpacetimeDB wire
compatibility are out of scope.

## Auth Mode

Development mode is the zero-value `AuthMode` and allows missing-token protocol
connections by minting anonymous development tokens.

Strict mode requires token validation for protocol connections:

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

See [Configure auth](configure-auth.md) for the app-author setup path and
[Authentication](../authentication.md) for the full strict-mode contract and
production checklist.

## Diagnostics

Runtime diagnostics are configured through `Config.Observability.Diagnostics`.
When diagnostics HTTP mounting is enabled, `Runtime.HTTPHandler()` also exposes
runtime diagnostic endpoints such as health, readiness, and optional metrics
handlers.

For in-process checks, use:

```go
inspection := shunter.InspectRuntimeHealth(rt)
statusCode := shunter.ReadyzStatusCode(inspection.Status)
_ = statusCode
```

## Multi-Module Host

`Host` can serve multiple built runtimes under explicit route prefixes:

```go
host, err := shunter.NewHost(
	shunter.HostRuntime{Name: "chat", RoutePrefix: "/chat", Runtime: chatRuntime},
	shunter.HostRuntime{Name: "ops", RoutePrefix: "/ops", Runtime: opsRuntime},
)
if err != nil {
	return err
}

return host.ListenAndServe(ctx, "127.0.0.1:3000")
```

Use this only when you intentionally want multiple independent runtimes in one
process. A host does not merge schemas, transactions, data directories, or
contracts.

Host route prefixes are explicit and must not overlap. Each registered module
name must match the built runtime, and hosted runtimes must not share a
`DataDir`.

Cross-module transactions, cross-module SQL or subscriptions, merged schema or
contract artifacts, global reducer/query namespaces, and dynamic module upload
are out of scope for v1.

## Serving Checklist

- Set `EnableProtocol: true`.
- Choose `ListenAndServe` or app-owned `HTTPHandler` mounting.
- Set HTTP read-header and idle timeouts when using an app-owned server.
- Use a durable `DataDir`.
- Use strict auth for public services.
- Call `Start` before serving when using `HTTPHandler` directly.
- Call `Close` during shutdown.
- Verify readiness before admitting traffic.
