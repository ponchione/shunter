# V1.5-B Task 01: Reconfirm Contract Export Prerequisites

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-B/00-current-execution-plan.md`

Objective: verify the full contract export can be assembled from live v1 and
V1.5-A surfaces.

Checks:
- `rtk go doc . Module`
- `rtk go doc . Module.Describe`
- `rtk go doc . Runtime`
- `rtk go doc . Runtime.ExportSchema`
- `rtk go doc ./schema SchemaExport`
- `rtk go doc ./schema ReducerExport`

Also inspect the V1.5-A declaration docs and tests before implementation.

Prerequisite conclusions to record in Task 01:
- schema export already provides tables, indexes, columns, and reducers
- module description provides identity/version/metadata
- query/view declarations are available from V1.5-A
- V1.5-B owns the first full module contract artifact
- V1.5-B should reserve but not fully populate permission and migration fields

