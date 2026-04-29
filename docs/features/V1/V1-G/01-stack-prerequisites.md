# V1-G Task 01: Reconfirm export/introspection prerequisites

Parent plan: `docs/features/V1/V1-G/2026-04-24_074206-hosted-runtime-v1g-export-introspection-implplan.md`

Objective: verify V1-G is stacked on V1-F and grounded in existing root/runtime and schema export seams.

Checks:
- `rtk go doc . Module`
- `rtk go doc . Runtime`
- `rtk go doc . Runtime.Health`
- `rtk go doc ./schema.Engine.ExportSchema`
- `rtk go doc ./schema.SchemaExport`
- `rtk go doc ./schema.ReducerExport`
