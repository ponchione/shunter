# Runtime Lifecycle Reference

Status: rough draft
Scope: lifecycle order for `Build`, `Start`, serving, reads, writes, and
`Close`.

## Build

`shunter.Build(mod, cfg)` validates the module, opens or initializes durable
state, checks recovery compatibility, builds runtime catalogs, and returns a
runtime in the built state.

`Build` does not start workers or protocol serving.

Safe after `Build`:

- `Runtime.Describe`
- `Runtime.ExportSchema`
- `Runtime.ExportContract`
- `Runtime.ExportContractJSON`
- `Runtime.Config`
- `Runtime.ModuleName`
- `Runtime.Health`
- `Runtime.HTTPHandler` construction
- `Runtime.Close`

Normal app code should still start the runtime before serving traffic or
performing reads and writes.

## Start

`Runtime.Start(ctx)` starts runtime-owned lifecycle, durability, executor,
subscription, migration hook, and protocol resources.

Call `Start` before:

- `Runtime.CallReducer`
- `Runtime.Read`
- `Runtime.CallQuery`
- `Runtime.SubscribeView`
- serving `Runtime.HTTPHandler()` traffic

Startup failure should be treated as app startup failure.

## ListenAndServe

`Runtime.ListenAndServe(ctx)` is the lifecycle-owned serving path. It starts the
runtime if needed, serves `Runtime.HTTPHandler()` on `Config.ListenAddr`, and
closes runtime ownership when the context is canceled.

Use this when Shunter owns the HTTP server lifecycle.

## HTTPHandler

`Runtime.HTTPHandler()` returns an HTTP handler for app-owned servers.

The handler does not start lifecycle. If you mount it yourself, call
`Runtime.Start` before accepting traffic and `Runtime.Close` during shutdown.

## Close

`Runtime.Close()` shuts down runtime-owned resources. It is safe to defer after
a successful `Build`.

For graceful service shutdown:

1. Stop admitting new traffic.
2. Optionally wait for important transactions with `WaitUntilDurable`.
3. Call `Close`.
4. Only then run offline backup, restore, or migration tooling.

## Health And Readiness

Use `Runtime.Ready()` for a boolean readiness check. Use
`InspectRuntimeHealth(rt)` when the app needs structured health status and
reasoning.

HTTP-style status helpers:

```go
inspection := shunter.InspectRuntimeHealth(rt)
readyCode := shunter.ReadyzStatusCode(inspection.Status)
healthCode := shunter.HealthzStatusCode(inspection.Status)
_ = readyCode
_ = healthCode
```

## Host Lifecycle

`Host` coordinates multiple built runtimes. `Host.Start`, `Host.Close`, and
`Host.ListenAndServe` operate over the hosted runtimes without merging their
schemas, transactions, data directories, or contracts.

Use host lifecycle only when the application intentionally serves multiple
independent modules from one process.
