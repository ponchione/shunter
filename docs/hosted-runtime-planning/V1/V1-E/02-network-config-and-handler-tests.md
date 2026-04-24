# V1-E Task 02: Add failing tests for handler composition and network config

Parent plan: `docs/hosted-runtime-planning/V1-E/2026-04-23_212032-hosted-runtime-v1e-runtime-network-surface-implplan.md`

Objective: pin the public network surface before implementation.

Files:
- Create `runtime_network_test.go`
- Modify `config.go` tests as needed

Tests to add:
- `HTTPHandler()` returns a composable handler and gates non-ready/closed runtime requests with service-unavailable behavior
- `ListenAndServe(ctx)` auto-starts built-but-not-ready runtime
- blank listen address normalizes to the documented default
- strict auth without signing key fails clearly
- dev/anonymous mode can mint with a runtime-owned generated key
- explicit network methods are not blocked solely by `EnableProtocol == false`
