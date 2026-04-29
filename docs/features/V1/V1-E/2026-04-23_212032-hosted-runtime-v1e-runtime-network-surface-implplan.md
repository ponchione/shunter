# Hosted Runtime V1-E Runtime Network Surface Implementation Plan

> For Hermes: use the subagent-driven-development skill to execute this plan task-by-task if the user asks for implementation.

Status: concretely validated against the live repo on 2026-04-23.
Scope: V1-E only; planning artifact, not implementation.

Goal: expose the V1-D lifecycle-owned runtime through a WebSocket-first network surface with both a simple serving call and a composable HTTP handler, while replacing V1-D's private no-op fan-out sender with protocol-backed delivery.

Architecture: V1-E should keep `Runtime.Start(ctx)` / `Runtime.Close()` as lifecycle ownership from V1-D. `Runtime.HTTPHandler()` returns a host-mountable `http.Handler` for the protocol `/subscribe` route. `Runtime.ListenAndServe(ctx)` is the easy default path: it starts the runtime if needed, serves the handler on the configured listen address, and shuts down serving plus runtime ownership on cancellation or close. Protocol wiring should be derived from `protocol.Server`, `executor.ProtocolInboxAdapter`, `protocol.ConnManager`, `protocol.NewClientSender`, and `protocol.NewFanOutSenderAdapter`; do not copy the example binary as an implementation source of truth.

Tech stack: Go, root package `github.com/ponchione/shunter`, existing `auth`, `executor`, `protocol`, `subscription`, `schema`, `store`, and `types` packages, `net/http`, RTK-wrapped Go toolchain commands.

---

## Current grounded context

Read and verified while writing this plan:

- `docs/specs/hosted-runtime-version-phases.md` defines V1-E as runtime network surface:
  - `ListenAndServe(...)` or equivalent clean default
  - `HTTPHandler()` for host-app composition
  - protocol/network options through top-level config
- `docs/specs/hosted-runtime-v1-contract.md` says the primary external client surface is the realtime WebSocket protocol, not REST or MCP.
- `docs/features/V1/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md` deliberately keeps network serving out of V1-D and says V1-E owns protocol-backed fan-out delivery.
- Live repo reality at validation time: the root `shunter` package is still absent in this checkout (`rtk go list .` reports `no Go files in /home/ponchione/source/shunter`). This V1-E plan is therefore stacked after the V1-A, V1-B, V1-C, and V1-D implementation plans.
- The former bundled demo command remains a demo consumer, not an implementation source of truth for runtime architecture.

Go API facts verified with `rtk go doc` and file inspection:

- `protocol.Server` is the HTTP-level WebSocket upgrade entry point. `HandleSubscribe` is routed from `/subscribe` by the host application.
- `protocol.Server.HandleSubscribe(w, r)` authenticates, resolves/generates `connection_id`, negotiates the WebSocket subprotocol, upgrades, and runs the built-in lifecycle path when `Executor` and `Conns` are set and `Upgraded` is nil.
- `protocol.Server` fields needed for the default path are `JWT`, `Mint`, `Options`, `Executor`, `Conns`, `Schema`, and `State`.
- `protocol.ProtocolOptions` has transport/backpressure fields and `protocol.DefaultProtocolOptions()` supplies defaults.
- `protocol.ConnManager` tracks active connections and exposes `CloseAll(ctx, inbox)` for graceful server shutdown.
- `auth.JWTConfig` has `SigningKey`, `Audiences`, and engine-level `AuthMode`.
- `auth.MintConfig` has `Issuer`, `Audience`, `SigningKey`, and `Expiry`; it is required when protocol auth mode is anonymous/dev.
- `executor.NewProtocolInboxAdapter(exec)` creates the production `protocol.ExecutorInbox` bridge for lifecycle, subscribe/unsubscribe, reducer calls, and one-off dispatch admission.
- `protocol.NewClientSender(conns, inbox)` creates a `protocol.ClientSender` backed by `ConnManager` and executor inbox teardown behavior.
- `protocol.NewFanOutSenderAdapter(clientSender)` adapts protocol client sending to `subscription.FanOutSender`.
- `subscription.FanOutWorker` captures its `FanOutSender` at construction time, so V1-E must either create the protocol-backed sender before the worker starts or introduce a private swappable sender wrapper if V1-D already starts the worker with a no-op sender.

## Validation conclusion for V1-E

V1-E should be the first hosted-runtime slice that exposes HTTP/WebSocket serving through the root runtime API.

V1-E must not add REST-first APIs, MCP surfaces, local reducer/query calls, export/introspection, control-plane/admin APIs, or hello-world replacement work. It should only wire the already-existing protocol server into the runtime owner and replace internal no-op fan-out delivery with protocol-backed delivery.

Because V1-D owns `Start` and `Close`, V1-E should not move lifecycle startup into `Build`. The serving surface should compose with lifecycle as follows:

1. `HTTPHandler()` is composable and safe to obtain after `Build`; it returns an `http.Handler` that only admits protocol traffic after the runtime is ready.
2. Host apps that mount `HTTPHandler()` directly should call `Start(ctx)` themselves before serving traffic.
3. `ListenAndServe(ctx)` is the easy default path and may call `Start(ctx)` automatically if the runtime is built but not ready.
4. `Close()` must close active protocol connections before stopping executor/durability resources, so disconnect cleanup can still route through the executor inbox.

## Scope

In scope:

- add `Runtime.HTTPHandler() http.Handler`
- add `Runtime.ListenAndServe(ctx context.Context) error`
- add private protocol graph wiring owned by `Runtime`
- add/normalize narrow top-level config for listen/auth/protocol options
- create `protocol.ConnManager`
- create `executor.ProtocolInboxAdapter` from the V1-D executor
- create `protocol.ClientSender` and `protocol.FanOutSenderAdapter`
- make V1-D fan-out use protocol-backed delivery once serving/protocol wiring is active
- close active protocol connections during `Runtime.Close()` before executor shutdown
- test handler composition, strict/dev auth behavior, serving lifecycle behavior, and close behavior

Out of scope:

- local reducer/query public APIs
- export/introspection
- hello-world/example replacement
- REST-first API
- MCP-first API
- broad admin/control-plane surface
- v1.5 query/view declarations
- contract snapshots/codegen
- permissions/read-model metadata
- migration metadata
- multi-module hosting
- dynamic plugins or out-of-process modules
- lower-level protocol/auth/executor redesign

## Decisions to lock for V1-E

1. `HTTPHandler()` is the composable surface.
   - Signature target:
     ```go
     func (r *Runtime) HTTPHandler() http.Handler
     ```
   - It returns a handler that routes `/subscribe` to the runtime-owned `protocol.Server.HandleSubscribe`.
   - Before the runtime is ready, requests should receive `503 Service Unavailable` instead of reaching a half-wired protocol server.
   - After `Close`, requests should receive `503 Service Unavailable` or a closed-runtime response; tests should assert the status class/closed behavior without overfitting a response string.

2. `ListenAndServe(ctx)` is the simple default path.
   - Signature target:
     ```go
     func (r *Runtime) ListenAndServe(ctx context.Context) error
     ```
   - It uses the normalized `Config.ListenAddr` and defaults blank listen address to `127.0.0.1:3000` unless implementation review selects a different documented local default.
   - It calls `Start(ctx)` automatically if the runtime is built and not ready.
   - It blocks until the HTTP server exits or `ctx` is canceled.
   - On `ctx` cancellation, it should gracefully shut down the HTTP server and call `Runtime.Close()`.
   - If `Start(ctx)` after prior `Close()` returns `ErrRuntimeClosed`, `ListenAndServe(ctx)` should preserve that sentinel via `errors.Is`.

3. Direct handler composition requires explicit lifecycle.
   - A host app may do:
     ```go
     rt, err := shunter.Build(mod, cfg)
     if err != nil { return err }
     if err := rt.Start(ctx); err != nil { return err }
     mux := http.NewServeMux()
     mux.Handle("/shunter/", http.StripPrefix("/shunter", rt.HTTPHandler()))
     ```
   - `HTTPHandler()` must not itself start lifecycle. It only exposes a handler around current runtime state.

4. V1-E protocol wiring is private runtime state.
   - Expected private fields, exact names flexible:
     ```go
     protocolConns *protocol.ConnManager
     protocolInbox *executor.ProtocolInboxAdapter
     protocolSender protocol.ClientSender
     fanOutSender subscription.FanOutSender
     protocolServer *protocol.Server
     httpServer *http.Server // only for ListenAndServe-owned serving, if useful
     ```
   - Do not expose these lower-level handles publicly.

5. V1-E should replace V1-D no-op fan-out safely.
   - Preferred design: V1-D should instantiate the fan-out worker with a private swappable sender wrapper, initially pointing to a no-op sender.
   - V1-E sets that wrapper's target to `protocol.NewFanOutSenderAdapter(protocol.NewClientSender(conns, inbox))` before the runtime admits WebSocket traffic.
   - When this plan was active, a concurrent V1-D implementation would have needed the swappable wrapper from the start rather than starting the worker with a permanently fixed no-op sender.
   - Do not export the swappable sender or make it a public option.

6. Auth mapping stays narrow but real.
   - Root `AuthModeDev` maps to `auth.AuthModeAnonymous` with minting enabled.
   - Root `AuthModeStrict` maps to `auth.AuthModeStrict` and requires a configured signing key before protocol serving can start.
   - Add only the narrow top-level config needed for real protocol auth, for example:
     ```go
     type Config struct {
         // existing V1-A/V1-C fields...
         ListenAddr string
         AuthMode   AuthMode

         AuthSigningKey []byte
         AuthAudiences  []string
         AnonymousTokenIssuer   string
         AnonymousTokenAudience string
         AnonymousTokenTTL      time.Duration
         Protocol               ProtocolConfig
     }
     ```
   - `AuthSigningKey` should be defensively copied into private runtime config.
   - For dev/anonymous mode, if no signing key is provided, generate a private per-runtime dev key during `Build` or protocol wiring. Do not use a package-global hard-coded secret.
   - For strict mode, missing `AuthSigningKey` should fail at `Start`/network setup with a clear sentinel such as `ErrAuthSigningKeyRequired`; do not silently downgrade to anonymous mode.

7. Protocol option mapping should be top-level and optional.
   - Add a root `ProtocolConfig` rather than exposing `protocol.ProtocolOptions` directly as the primary app-facing API:
     ```go
     type ProtocolConfig struct {
         PingInterval           time.Duration
         IdleTimeout            time.Duration
         CloseHandshakeTimeout  time.Duration
         DisconnectTimeout      time.Duration
         OutgoingBufferMessages int
         IncomingQueueMessages int
         MaxMessageSize         int64
     }
     ```
   - Zero values mean use `protocol.DefaultProtocolOptions()`.
   - Negative durations/counts/sizes are invalid and should fail before serving.
   - Mapping helper target:
     ```go
     func buildProtocolOptions(cfg ProtocolConfig) (protocol.ProtocolOptions, error)
     ```

8. Close ordering must account for active connections.
   - V1-D planned shutdown order was scheduler/fan-out, executor, durability.
   - V1-E must prepend protocol connection shutdown before executor shutdown:
     1. stop admitting new HTTP/WebSocket requests
     2. call `ConnManager.CloseAll(ctx, protocolInbox)` with a bounded shutdown context
     3. stop scheduler/fan-out goroutines
     4. call executor shutdown
     5. close durability
   - Reason: connection close/disconnect cleanup needs `protocol.ExecutorInbox`, which routes into the executor.

9. `EnableProtocol` should not be a hidden blocker for explicit network methods.
   - V1-A included `Config.EnableProtocol` before network behavior existed.
   - In V1-E, calling `HTTPHandler()` or `ListenAndServe(ctx)` is explicit network enablement.
   - Do not make zero-value `Config{}` accidentally unusable for network serving solely because `EnableProtocol` defaults to false.
   - If the field is retained for lower-level `schema.EngineOptions`, document that top-level network methods control actual serving.

## Files likely to modify

Assuming V1-A/V1-B/V1-C/V1-D files exist after stacking/landing:

- Modify: `config.go`
- Modify: `runtime.go`
- Modify: `runtime_lifecycle.go`
- Create or modify: `runtime_network.go`
- Create: `runtime_network_test.go`
- Possibly modify: `runtime_build.go` to store normalized listen/auth/protocol config privately

Do not edit unless implementation proves a direct compile necessity:

- `protocol/upgrade.go`
- `protocol/sender.go`
- `protocol/fanout_adapter.go`
- `protocol/conn.go`
- `executor/protocol_inbox_adapter.go`
- `subscription/fanout_worker.go`
- `auth/jwt.go`
- `auth/mint.go`

## Public API target for V1-E

Add these root-package methods/types:

```go
func (r *Runtime) HTTPHandler() http.Handler
func (r *Runtime) ListenAndServe(ctx context.Context) error

type ProtocolConfig struct {
    PingInterval           time.Duration
    IdleTimeout            time.Duration
    CloseHandshakeTimeout  time.Duration
    DisconnectTimeout      time.Duration
    OutgoingBufferMessages int
    IncomingQueueMessages int
    MaxMessageSize         int64
}
```

Add narrow auth fields to `Config` if not already present after prior slices:

```go
type Config struct {
    DataDir                 string
    ExecutorQueueCapacity   int
    DurabilityQueueCapacity int
    EnableProtocol          bool
    ListenAddr              string
    AuthMode                AuthMode

    AuthSigningKey []byte
    AuthAudiences  []string

    AnonymousTokenIssuer   string
    AnonymousTokenAudience string
    AnonymousTokenTTL      time.Duration

    Protocol ProtocolConfig
}
```

Expected sentinel errors, exact names flexible:

```go
var ErrAuthSigningKeyRequired = errors.New("shunter: auth signing key required")
var ErrRuntimeNotReady = errors.New("shunter: runtime is not ready")
var ErrRuntimeServing = errors.New("shunter: runtime is already serving")
```

Do not add:

```go
// V1-F:
// func (r *Runtime) CallReducer(...)
// func (r *Runtime) Query(...)
// func (r *Runtime) ReadView(...)

// V1-G:
// func (r *Runtime) ExportSchema(...)
// func (r *Runtime) Describe(...)
```

## Task 1: Reconfirm stack prerequisites before coding

Objective: ensure V1-E is implemented only after V1-D lifecycle ownership exists.

Files:
- Read: V1-A through V1-D implplans under `docs/features/V1/`
- Inspect: `module.go`, `config.go`, `runtime.go`, `runtime_build.go`, `runtime_lifecycle.go`, root tests once prior slices exist

Run:

```bash
rtk go list .
rtk go doc ./protocol.Server
rtk go doc ./protocol.Server.HandleSubscribe
rtk go doc ./protocol.ProtocolOptions
rtk go doc ./protocol.DefaultProtocolOptions
rtk go doc ./protocol.ConnManager
rtk go doc ./auth.JWTConfig
rtk go doc ./auth.MintConfig
rtk go doc ./executor.NewProtocolInboxAdapter
rtk go doc ./protocol.NewClientSender
rtk go doc ./protocol.NewFanOutSenderAdapter
rtk go doc ./subscription.FanOutSender
```

Expected:
- `rtk go list .` succeeds after V1-A exists.
- V1-D lifecycle tests pass before V1-E starts.

Stop condition:
- If `Runtime.Start`, `Runtime.Close`, and private V1-D executor/subscription/fan-out fields are missing, stop and apply/land prior slices first. Do not mix V1-E with creating lifecycle ownership from scratch.

## Task 2: Add protocol option normalization tests

Objective: pin top-level protocol config mapping before wiring a server.

Files:
- Modify or create: `runtime_network_test.go`

Test cases:

```go
func TestBuildProtocolOptionsUsesDefaultsForZeroConfig(t *testing.T) {
    opts, err := buildProtocolOptions(ProtocolConfig{})
    if err != nil {
        t.Fatalf("buildProtocolOptions returned error: %v", err)
    }
    want := protocol.DefaultProtocolOptions()
    if opts != want {
        t.Fatalf("options = %+v, want %+v", opts, want)
    }
}

func TestBuildProtocolOptionsAppliesOverrides(t *testing.T) {
    opts, err := buildProtocolOptions(ProtocolConfig{
        PingInterval:           time.Second,
        IdleTimeout:            2 * time.Second,
        CloseHandshakeTimeout:  3 * time.Second,
        DisconnectTimeout:      4 * time.Second,
        OutgoingBufferMessages: 17,
        IncomingQueueMessages:  18,
        MaxMessageSize:         19,
    })
    if err != nil {
        t.Fatalf("buildProtocolOptions returned error: %v", err)
    }
    if opts.PingInterval != time.Second || opts.OutgoingBufferMessages != 17 || opts.MaxMessageSize != 19 {
        t.Fatalf("override mapping failed: %+v", opts)
    }
}

func TestBuildProtocolOptionsRejectsNegativeValues(t *testing.T) {
    _, err := buildProtocolOptions(ProtocolConfig{OutgoingBufferMessages: -1})
    if err == nil {
        t.Fatal("expected error for negative outgoing buffer")
    }
}
```

Run:

```bash
rtk go test . -run 'TestBuildProtocolOptions' -count=1
```

Expected:
- fail until `ProtocolConfig` and helper exist.

## Task 3: Implement `ProtocolConfig` and option mapping

Objective: expose narrow top-level protocol tuning while reusing protocol defaults.

Files:
- Modify: `config.go`
- Create or modify: `runtime_network.go`

Implementation notes:
- Start from `protocol.DefaultProtocolOptions()`.
- Override each field only when the top-level value is non-zero.
- Reject negative durations/counts/sizes with clear errors.
- Keep this helper private unless a later public diagnostics/export slice needs it.

Run:

```bash
rtk go test . -run 'TestBuildProtocolOptions' -count=1
```

Expected:
- pass.

## Task 4: Add auth config mapping tests

Objective: pin strict/dev auth behavior before exposing network traffic.

Files:
- Modify: `runtime_network_test.go`

Test cases:

```go
func TestBuildAuthConfigDevGeneratesAnonymousMintConfig(t *testing.T) {
    jwtCfg, mintCfg, err := buildAuthConfig(Config{AuthMode: AuthModeDev})
    if err != nil {
        t.Fatalf("buildAuthConfig returned error: %v", err)
    }
    if jwtCfg.AuthMode != auth.AuthModeAnonymous {
        t.Fatalf("auth mode = %v, want anonymous", jwtCfg.AuthMode)
    }
    if len(jwtCfg.SigningKey) == 0 || mintCfg == nil || len(mintCfg.SigningKey) == 0 {
        t.Fatal("dev auth did not configure signing/minting")
    }
}

func TestBuildAuthConfigStrictRequiresSigningKey(t *testing.T) {
    _, _, err := buildAuthConfig(Config{AuthMode: AuthModeStrict})
    if !errors.Is(err, ErrAuthSigningKeyRequired) {
        t.Fatalf("error = %v, want ErrAuthSigningKeyRequired", err)
    }
}

func TestBuildAuthConfigStrictMapsAudiencesAndCopiesKey(t *testing.T) {
    key := []byte("test-secret")
    cfg := Config{AuthMode: AuthModeStrict, AuthSigningKey: key, AuthAudiences: []string{"app"}}
    jwtCfg, mintCfg, err := buildAuthConfig(cfg)
    if err != nil {
        t.Fatalf("buildAuthConfig returned error: %v", err)
    }
    if jwtCfg.AuthMode != auth.AuthModeStrict || mintCfg != nil {
        t.Fatalf("unexpected strict config: jwt=%+v mint=%+v", jwtCfg, mintCfg)
    }
    key[0] = 'X'
    if string(jwtCfg.SigningKey) == string(key) {
        t.Fatal("signing key was not defensively copied")
    }
}
```

Run:

```bash
rtk go test . -run 'TestBuildAuthConfig' -count=1
```

Expected:
- fail until auth config fields/helper exist.

## Task 5: Implement auth config mapping

Objective: make runtime config produce valid `auth.JWTConfig` / `auth.MintConfig` for protocol serving.

Files:
- Modify: `config.go`
- Modify: `runtime_network.go`

Implementation notes:
- Use a private helper such as:
  ```go
  func buildAuthConfig(cfg Config) (*auth.JWTConfig, *auth.MintConfig, error)
  ```
- For `AuthModeDev`, map to `auth.AuthModeAnonymous` and configure minting.
- For dev signing key defaulting, generate per-runtime random bytes if `AuthSigningKey` is empty.
- Default anonymous issuer/audience to stable local strings such as `shunter-dev` / module name if module name is available; if helper has only `Config`, default both to `shunter-dev`.
- For `AuthModeStrict`, require a non-empty signing key and do not configure `Mint`.
- Defensively copy all byte slices and string slices.

Run:

```bash
rtk go test . -run 'TestBuildAuthConfig' -count=1
```

Expected:
- pass.

## Task 6: Add a failing test for `HTTPHandler` before and after `Start`

Objective: prove handler composition exists without starting lifecycle implicitly.

Files:
- Modify: `runtime_network_test.go`

Test shape:

```go
func TestHTTPHandlerReturnsServiceUnavailableBeforeStart(t *testing.T) {
    rt := buildValidTestRuntime(t)
    req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
    rec := httptest.NewRecorder()

    rt.HTTPHandler().ServeHTTP(rec, req)

    if rec.Code != http.StatusServiceUnavailable {
        t.Fatalf("status = %d, want 503", rec.Code)
    }
}

func TestHTTPHandlerRoutesSubscribeAfterStart(t *testing.T) {
    rt := buildValidTestRuntime(t)
    if err := rt.Start(context.Background()); err != nil {
        t.Fatalf("Start: %v", err)
    }
    t.Cleanup(func() { _ = rt.Close() })

    req := httptest.NewRequest(http.MethodGet, "/subscribe", nil)
    rec := httptest.NewRecorder()
    rt.HTTPHandler().ServeHTTP(rec, req)

    // No WebSocket upgrade headers means protocol.HandleSubscribe should reject
    // at the HTTP layer. This proves routing reached protocol rather than the
    // runtime readiness gate.
    if rec.Code == http.StatusServiceUnavailable {
        t.Fatal("handler still gated after Start")
    }
}
```

Run:

```bash
rtk go test . -run 'TestHTTPHandler' -count=1
```

Expected:
- fail until `HTTPHandler` and protocol server wiring exist.

## Task 7: Implement private protocol graph builder

Objective: create the protocol server/sender state from V1-D runtime-owned fields.

Files:
- Modify: `runtime_network.go`
- Modify: `runtime.go` for private fields if needed
- Modify: `runtime_lifecycle.go` if Start should invoke the protocol graph builder

Implementation shape:

```go
func (r *Runtime) ensureProtocolGraphLocked() error {
    if r.protocolServer != nil {
        return nil
    }
    jwtCfg, mintCfg, err := buildAuthConfig(r.config)
    if err != nil {
        return err
    }
    opts, err := buildProtocolOptions(r.config.Protocol)
    if err != nil {
        return err
    }
    conns := protocol.NewConnManager()
    inbox := executor.NewProtocolInboxAdapter(r.executor)
    clientSender := protocol.NewClientSender(conns, inbox)
    fanOutSender := protocol.NewFanOutSenderAdapter(clientSender)

    r.protocolConns = conns
    r.protocolInbox = inbox
    r.protocolSender = clientSender
    r.setFanOutSender(fanOutSender) // private swappable sender target
    r.protocolServer = &protocol.Server{
        JWT:      jwtCfg,
        Mint:     mintCfg,
        Options:  opts,
        Executor: inbox,
        Conns:    conns,
        Schema:   r.registry,
        State:    r.state,
    }
    return nil
}
```

Guardrails:
- Call this only after V1-D `Start` has created `r.executor`.
- If the runtime is not ready, `HTTPHandler` should gate rather than trying to build the protocol server with nil lifecycle fields.
- If V1-D currently constructs `FanOutWorker` with a fixed no-op sender, first change V1-D private wiring to use a swappable sender wrapper.

Run:

```bash
rtk go test . -run 'TestHTTPHandler' -count=1
```

Expected:
- `HTTPHandler` tests pass.

## Task 8: Add WebSocket auth smoke tests through `HTTPHandler`

Objective: prove top-level auth mapping reaches `protocol.Server.HandleSubscribe`.

Files:
- Modify: `runtime_network_test.go`

Suggested tests:

- Dev/anonymous mode with no token reaches WebSocket upgrade path when request includes required subprotocol.
- Strict mode with no token returns `401 Unauthorized`.
- Strict mode with invalid token returns `401 Unauthorized`.

Use `httptest.NewServer(rt.HTTPHandler())` and `coder/websocket.Dial` if existing protocol tests already use that dependency directly; otherwise use HTTP requests for strict/no-token 401 cases and one WebSocket dial smoke only if straightforward.

Run:

```bash
rtk go test . -run 'TestHTTPHandler.*Auth|TestRuntimeNetworkAuth' -count=1
```

Expected:
- pass after auth/protocol wiring exists.

## Task 9: Add a failing `ListenAndServe` lifecycle test using a private listener helper

Objective: pin easy default serving behavior without binding a hard-coded public port in tests.

Files:
- Modify: `runtime_network.go`
- Modify: `runtime_network_test.go`

Implementation/test seam:
- Keep public `ListenAndServe(ctx)` using `net.Listen("tcp", normalizedListenAddr)`.
- Add an unexported helper for tests:
  ```go
  func (r *Runtime) serve(ctx context.Context, ln net.Listener) error
  ```

Test shape:

```go
func TestListenAndServeStartsRuntimeAndStopsOnContextCancel(t *testing.T) {
    rt := buildValidTestRuntime(t)
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    errCh := make(chan error, 1)
    go func() { errCh <- rt.serve(ctx, ln) }()

    eventually(t, func() bool { return rt.Ready() })
    cancel()

    select {
    case err := <-errCh:
        if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, http.ErrServerClosed) {
            t.Fatalf("serve returned %v", err)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("serve did not exit after context cancellation")
    }
    if rt.Ready() {
        t.Fatal("runtime still ready after serve cancellation")
    }
}
```

Run:

```bash
rtk go test . -run TestListenAndServeStartsRuntimeAndStopsOnContextCancel -count=1
```

Expected:
- fail until serving helper and `ListenAndServe` logic exist.

## Task 10: Implement `ListenAndServe` / private `serve`

Objective: make the easy default path start lifecycle, serve HTTP, and cleanly shut down.

Files:
- Modify: `runtime_network.go`
- Modify: `runtime_lifecycle.go` if `Close` needs serving-awareness

Implementation notes:
- Normalize blank listen address to `127.0.0.1:3000` or another documented default.
- Prevent duplicate active serving with a mutex/state guard and `ErrRuntimeServing`.
- `ListenAndServe(ctx)` should create the listener and call the private `serve(ctx, ln)` helper.
- `serve(ctx, ln)` should:
  1. call `Start(ctx)` if not ready
  2. construct `http.Server{Handler: r.HTTPHandler()}`
  3. run `server.Serve(ln)` in a goroutine
  4. on `ctx.Done()`, call `server.Shutdown(shutdownCtx)` and `r.Close()`
  5. return nil or a wrapped non-`http.ErrServerClosed` serving error
- Do not leave `http.Server` as a long-lived public handle.

Run:

```bash
rtk go test . -run TestListenAndServeStartsRuntimeAndStopsOnContextCancel -count=1
```

Expected:
- pass.

## Task 11: Add close-order test for active protocol connections

Objective: pin that V1-E closes protocol connections before executor shutdown.

Files:
- Modify: `runtime_network_test.go`
- Possibly modify: `runtime_lifecycle_test.go`

Preferred approach:
- Use a narrow private test seam around connection shutdown if a real WebSocket connection test is too slow/flaky.
- The test should force/observe that `protocolConns.CloseAll(ctx, protocolInbox)` is called before executor shutdown resources are nilled/closed.
- If using a real WebSocket, connect through `httptest.Server`, then call `rt.Close()` and assert the client observes close without leaking/hanging.

Run:

```bash
rtk go test . -run 'TestRuntimeCloseClosesProtocolConnections|TestRuntimeCloseActiveWebSocket' -count=1
```

Expected:
- pass.

Guardrail:
- Do not add exported testing hooks or public connection-management APIs for this. Use same-package tests and private seams if needed.

## Task 12: Add fan-out sender replacement test

Objective: prove V1-E no longer leaves fan-out delivery pointed at V1-D's no-op sender after protocol wiring.

Files:
- Modify: `runtime_network_test.go`

Test target:
- Build and start a runtime.
- Call `HTTPHandler()` or private protocol graph builder.
- Assert the private swappable fan-out sender target is a protocol-backed sender or a test-observable non-noop target.

If direct type assertion is brittle, test through a same-package fake sender by setting the swappable target and injecting a synthetic `subscription.FanOutMessage` into the fan-out inbox. Keep this focused on the runtime wiring seam, not full reducer/subscription end-to-end behavior.

Run:

```bash
rtk go test . -run TestRuntimeNetworkReplacesNoopFanOutSender -count=1
```

Expected:
- pass.

## Task 13: Focused validation

Objective: prove V1-E did not regress V1-A through V1-D or lower-level protocol/auth packages.

Run:

```bash
rtk go fmt .
rtk go test . -count=1
rtk go test ./auth ./protocol ./executor ./subscription -count=1
rtk go vet . ./auth ./protocol ./executor ./subscription
```

Then, if the working tree allows it:

```bash
rtk go test ./... -count=1
```

Expected:
- root and touched-package gates pass
- broad tests pass, or unrelated dirty-state failures are reported without fixing unrelated correctness/audit code inside V1-E

---

## Verification checklist

V1-E is complete when all of the following are true:

- `Runtime.HTTPHandler()` exists and returns a composable `http.Handler`.
- `HTTPHandler()` gates requests before readiness and routes `/subscribe` to `protocol.Server.HandleSubscribe` after `Start`.
- `Runtime.ListenAndServe(ctx)` exists, starts lifecycle automatically when needed, serves on normalized `Config.ListenAddr`, and stops on context cancellation.
- Runtime protocol graph creates and owns `protocol.ConnManager`.
- Runtime protocol graph creates `executor.ProtocolInboxAdapter` from the V1-D executor.
- Runtime protocol graph creates `protocol.ClientSender` and `protocol.FanOutSenderAdapter`.
- V1-D's no-op fan-out target is replaced/wrapped with protocol-backed delivery before clients can be admitted.
- Root dev auth maps to anonymous protocol auth with mint config.
- Root strict auth requires a signing key and maps audiences/signing key into `auth.JWTConfig`.
- Top-level protocol config maps onto `protocol.ProtocolOptions` with defaulting and validation.
- `Runtime.Close()` closes active protocol connections before executor shutdown.
- No local reducer/query APIs, export/introspection, REST-first API, MCP-first API, admin/control-plane API, v1.5, or v2 surface is added.
- Focused RTK validation passes.

## Risks and guardrails

1. Fan-out sender replacement can be impossible if V1-D hard-wired a no-op sender into a running worker.
   - Guardrail: use a private swappable sender wrapper in V1-D/V1-E. Do not export it.

2. Serving can race runtime shutdown.
   - Guardrail: `HTTPHandler` should check readiness/state per request and `Close` should stop admission/connection delivery before executor shutdown.

3. Strict auth can silently become dev auth if defaults are too permissive.
   - Guardrail: strict mode must require `AuthSigningKey`; preserve `ErrAuthSigningKeyRequired` via `errors.Is`.

4. `ListenAndServe` can leak an HTTP server goroutine on cancellation.
   - Guardrail: test context cancellation with a private listener helper and assert serve exits.

5. Connection shutdown can need the executor inbox.
   - Guardrail: `ConnManager.CloseAll(ctx, inbox)` must happen before executor shutdown.

6. The existing `EnableProtocol` bool can confuse the API because zero-value bool defaults false.
   - Guardrail: explicit network method calls are network enablement; do not make zero-value config unusable solely because `EnableProtocol` is false.

7. Scope creep into local APIs is tempting once protocol reducer calls work.
   - Guardrail: V1-E remains network-only. V1-F owns local reducer/query calls.

## Historical sequencing note

The later hosted-runtime slices have since landed. Do not treat this completed
V1-E plan as a live handoff; use `HOSTED_RUNTIME_PLANNING_HANDOFF.md` for
current hosted-runtime status.
