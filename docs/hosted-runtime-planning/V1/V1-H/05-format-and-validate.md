# V1-H Task 05: Format and validate the slice

Parent plan: `docs/hosted-runtime-planning/V1-H/2026-04-23_214356-hosted-runtime-v1h-hello-world-replacement-v1-proof-implplan.md`

Objective: run end-to-end example verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test ./cmd/... -count=1`
- `rtk go test . -count=1`
- `rtk go vet ./cmd/... .`

Validation checklist:
- the normal example uses the top-level `shunter` API
- the end-to-end proof covers build, start/serve, subscribe, reducer call, update delivery, recovery, and shutdown
- V1-H does not backfill missing V1-A through V1-G APIs
