# V1-F Task 05: Format and validate the slice

Parent plan: `docs/hosted-runtime-planning/V1-F/2026-04-23_212927-hosted-runtime-v1f-local-runtime-calls-implplan.md`

Objective: run local-call verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go test ./executor ./store ./query/sql -count=1`
- `rtk go vet . ./executor ./store ./query/sql`

Validation checklist:
- local calls are secondary APIs and do not replace the WebSocket-first external model
- reducer calls use executor command submission
- read snapshots are always closed by the runtime-owned callback wrapper
- no new network/export/example work leaked into this slice
