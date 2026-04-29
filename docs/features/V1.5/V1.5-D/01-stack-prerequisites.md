# V1.5-D Task 01: Reconfirm Permission Metadata Prerequisites

Parent plan: `docs/features/V1.5/V1.5-D/00-current-execution-plan.md`

Objective: verify permission/read-model metadata can annotate existing exported
surfaces without changing the runtime shape.

Checks:
- `rtk go doc . Module`
- `rtk go doc . Runtime.ExportContract`
- `rtk go doc ./schema ReducerExport`

Prerequisite conclusions to record in Task 01:
- reducers are already exported through schema/contract metadata
- queries and views are exported through V1.5-A/V1.5-B metadata
- V1.5-D should attach metadata near the declarations it governs
- V1.5-D should not introduce a standalone policy language
- codegen should be able to inspect metadata without enforcing it

