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
	Addr:    "127.0.0.1:3000",
	Handler: rt.HTTPHandler(),
}
return server.ListenAndServe()
```

`HTTPHandler` does not start lifecycle by itself. Call `Start` before serving
traffic.

If the app-owned server needs its own middleware, route `rt.HTTPHandler()` under
the desired mount point after the runtime has started. Keep using local
`CallReducer`, `Read`, `CallQuery`, and `SubscribeView` for trusted in-process
work that does not need WebSocket protocol behavior.

## Protocol Endpoint

With protocol enabled, the runtime mounts the WebSocket protocol endpoint at
`/subscribe`.

The v1 protocol is Shunter-native. Do not document it as a compatibility layer
for another runtime's wire format.

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

## Serving Checklist

- Set `EnableProtocol: true`.
- Choose `ListenAndServe` or app-owned `HTTPHandler` mounting.
- Use a durable `DataDir`.
- Use strict auth for public services.
- Call `Start` before serving when using `HTTPHandler` directly.
- Call `Close` during shutdown.
- Verify readiness before admitting traffic.
