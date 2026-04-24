# V1-D Task 05: Format and validate the slice

Parent plan: `docs/hosted-runtime-planning/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md`

Objective: run lifecycle-focused verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go test ./executor ./commitlog ./subscription -count=1`
- `rtk go vet . ./executor ./commitlog ./subscription`

Validation checklist:
- V1-D is the first slice that starts long-lived runtime goroutines
- `Close()` is idempotent and safe before `Start`
- startup cancellation cleans up partial resources
- no HTTP serving or protocol-backed delivery exists yet
