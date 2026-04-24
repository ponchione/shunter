# V1-E Task 03: Implement private protocol wiring and `HTTPHandler`

Parent plan: `docs/hosted-runtime-planning/V1-E/2026-04-23_212032-hosted-runtime-v1e-runtime-network-surface-implplan.md`

Objective: expose the composable network surface while keeping lower-level protocol objects private.

Files:
- Modify `config.go`
- Modify `runtime.go`
- Create or modify `runtime_network.go`

Implementation requirements:
- add narrow top-level config for auth signing key, audiences, anonymous mint settings, and protocol options
- normalize `ProtocolConfig` into `protocol.ProtocolOptions`
- create `protocol.ConnManager`, `executor.ProtocolInboxAdapter`, `protocol.ClientSender`, and protocol-backed fan-out adapter
- add `Runtime.HTTPHandler() http.Handler`
- route `/subscribe` to runtime-owned `protocol.Server.HandleSubscribe`
- use a private swappable sender wrapper if V1-D already starts the fan-out worker with a no-op sender
