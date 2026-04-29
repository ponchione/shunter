# V1-C Task 01: Reconfirm stack prerequisites and kernel contracts

Parent plan: `docs/features/V1/V1-C/2026-04-23_205158-hosted-runtime-v1c-runtime-build-pipeline-implplan.md`

Objective: verify V1-C is stacked on V1-A and V1-B and re-ground the build/recovery design against kernel contracts.

Read first:
- `docs/features/V1/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`
- `docs/features/V1/V1-B/2026-04-23_204414-hosted-runtime-v1b-module-registration-wrappers-implplan.md`

Checks:
- `rtk go list .`
- `rtk go doc ./schema.SchemaRegistry`
- `rtk go doc ./commitlog.OpenAndRecoverDetailed`
- `rtk go doc ./commitlog.NewSnapshotWriter`
- `rtk go doc ./executor.ReducerRegistry`

Stop if:
- the root package is still absent
- V1-B success-path tests fail
