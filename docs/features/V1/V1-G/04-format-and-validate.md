# V1-G Task 04: Format and validate the slice

Parent plan: `docs/features/V1/V1-G/2026-04-24_074206-hosted-runtime-v1g-export-introspection-implplan.md`

Objective: run V1-G verification gates.

Commands:
- `rtk go fmt .`
- `rtk go test . -run 'Test(ModuleDescribe|RuntimeExportSchema|RuntimeDescribe)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go doc . Module.Describe`
- `rtk go doc . Runtime.ExportSchema`
- `rtk go doc . Runtime.Describe`

Validation checklist:
- introspection is detached/copy-safe
- schema export delegates to existing schema engine export
- runtime description reports existing health state only
- no v1.5 contract/codegen/query/view/permission/migration/example work leaked into this slice
