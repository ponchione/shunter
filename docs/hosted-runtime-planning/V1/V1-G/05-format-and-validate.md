# V1-G Task 05: Format and validate the slice

Parent plan: `docs/hosted-runtime-planning/V1-G/2026-04-23_213651-hosted-runtime-v1g-export-introspection-foundation-implplan.md`

Objective: run export/introspection verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test . -count=1`
- `rtk go test ./schema -count=1`
- `rtk go vet . ./schema`

Validation checklist:
- module/runtime/schema descriptions are detached snapshots
- reducer metadata stays narrow and honest
- no canonical contract, codegen, permissions, or migration work leaked into V1-G
