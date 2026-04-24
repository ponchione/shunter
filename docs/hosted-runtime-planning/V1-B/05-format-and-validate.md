# V1-B Task 05: Format and validate the slice

Parent plan: `docs/hosted-runtime-planning/V1-B/2026-04-23_204414-hosted-runtime-v1b-module-registration-wrappers-implplan.md`

Objective: run the V1-B verification gates without widening scope into lifecycle or networking work.

Commands:
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go test ./schema -count=1`
- `rtk go vet . ./schema`

Validation checklist:
- explicit, versioned, non-empty modules build successfully through `shunter.Build`
- wrapper methods remain thin delegations to `schema.Builder`
- schema validation errors are preserved via `errors.Is`
- no `Runtime.Start`, `Runtime.Close`, `ListenAndServe`, or `HTTPHandler` work has been introduced
