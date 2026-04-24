# V1-E Task 05: Format and validate the slice

Parent plan: `docs/hosted-runtime-planning/V1-E/2026-04-23_212032-hosted-runtime-v1e-runtime-network-surface-implplan.md`

Objective: run network-surface verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go test ./protocol ./auth ./executor ./subscription -count=1`
- `rtk go vet . ./protocol ./auth ./executor ./subscription`

Validation checklist:
- `HTTPHandler()` is composable and does not start lifecycle by itself
- `ListenAndServe(ctx)` is the simple default path
- protocol-backed fan-out replaces the V1-D no-op sender safely
- close ordering shuts connections before executor shutdown
