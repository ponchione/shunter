# Hosted Runtime V1-E Current Execution Plan

Goal: implement the V1-E runtime network surface on top of the completed V1-D lifecycle owner, exposing `Runtime.HTTPHandler()` and `Runtime.ListenAndServe(ctx)` while wiring existing protocol/auth/executor/subscription packages privately.

Grounded facts:
- V1-D `Runtime.Start(ctx)`, `Runtime.Close()`, `Runtime.Ready()`, and `Runtime.Health()` exist and pass lifecycle tests.
- `protocol.Server.HandleSubscribe` is the HTTP WebSocket upgrade handler for `/subscribe`.
- `protocol.Server` needs JWT, optional Mint, ProtocolOptions, Executor, Conns, Schema, and State for the default lifecycle/dispatch path.
- `executor.NewProtocolInboxAdapter(exec)` is the protocol executor bridge.
- `protocol.NewClientSender(conns, inbox)` plus `protocol.NewFanOutSenderAdapter(clientSender)` is the protocol-backed subscription fan-out delivery path.
- `protocol.ConnManager.CloseAll(ctx, inbox)` must run before executor shutdown so disconnect cleanup can still use the executor inbox.

Scope:
- Add root `ProtocolConfig` and narrow network auth config fields.
- Add private protocol option/auth mapping helpers.
- Add private protocol graph construction for a started runtime.
- Replace V1-D fixed no-op fan-out delivery with a private swappable sender wrapper whose target is updated to protocol-backed delivery once protocol wiring exists.
- Add `Runtime.HTTPHandler() http.Handler` with readiness gating.
- Add `Runtime.ListenAndServe(ctx) error` and private `serve(ctx, ln)` test seam.
- Update `Runtime.Close()` to close active protocol connections before stopping scheduler/fan-out/executor/durability.

Non-goals:
- No local reducer/query API.
- No export/introspection API.
- No REST/MCP surface.
- No hello-world/example replacement.
- No v1.5 contract/codegen/permissions/migration/query-view surface.
- No public exposure of protocol connection manager, protocol inbox, client sender, fan-out sender, executor, or committed state.

TDD sequence:
1. Add `runtime_network_test.go` with RED tests for protocol option defaulting/overrides/negative validation.
2. Add RED tests for dev and strict auth mapping, including strict missing-key sentinel and defensive copies.
3. Add RED tests for `HTTPHandler` readiness gating and post-start route reachability.
4. Add RED tests for `ListenAndServe`/private `serve` auto-start and context-cancel shutdown.
5. Add RED tests for close-order/protocol connection cleanup and fan-out sender replacement using same-package private state.
6. Implement minimal config fields and helpers.
7. Implement private swappable fan-out sender and protocol graph builder.
8. Implement `HTTPHandler`, `ListenAndServe`, and connection-aware `Close()` updates.
9. Run focused V1-E tests, then root/touched-package/full validation.

Validation commands:
- `rtk go test . -run 'TestBuildProtocolOptions|TestBuildAuthConfig|TestHTTPHandler|TestListenAndServe|TestRuntimeNetwork|TestRuntimeClose' -count=1`
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go test ./protocol ./auth ./executor ./subscription -count=1`
- `rtk go vet . ./protocol ./auth ./executor ./subscription`
- `rtk go test ./... -count=1`

Historical sequencing note: the later hosted-runtime slices have since landed.
Use the relevant feature plan for current hosted-runtime status.
