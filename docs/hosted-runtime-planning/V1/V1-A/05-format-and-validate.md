# V1-A Task 05: Format and validate the slice

Parent plan: `docs/hosted-runtime-planning/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`

Objective: run the required Go formatting and verification gates for the completed V1-A slice and isolate unrelated failures if broader repo state is dirty.

Commands:
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go test ./schema -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet . ./schema`

Validation notes:
- The root package must now exist, so `rtk go list .` should succeed
- Prefer reporting narrow passing gates first
- If broad `./...` coverage fails because of unrelated existing work, report the exact unrelated failures instead of widening V1-A scope
- Do not fix unrelated query/protocol/v1.5/v2 work under this task

Completion checklist:
- root package exists
- public `Module`, `Config`, `Runtime`, and `Build` symbols exist
- defensive metadata/version behavior is covered
- public `Build` validation is pinned for nil module, blank name, negative queues, and invalid auth mode
- config mapping to `schema.EngineOptions` is complete
- empty-module build still fails through existing schema validation
- no services, sockets, goroutines, local calls, registration wrappers, or later-slice APIs were added
