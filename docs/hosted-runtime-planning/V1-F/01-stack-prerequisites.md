# V1-F Task 01: Reconfirm prerequisites and local-call contracts

Parent plan: `docs/hosted-runtime-planning/V1-F/2026-04-23_212927-hosted-runtime-v1f-local-runtime-calls-implplan.md`

Objective: verify V1-F is stacked on V1-D/V1-E and grounded in the executor and committed-state seams for local calls.

Read first:
- `docs/hosted-runtime-planning/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md`
- `docs/hosted-runtime-planning/V1-E/2026-04-23_212032-hosted-runtime-v1e-runtime-network-surface-implplan.md`

Checks:
- `rtk go doc ./executor.Executor.Submit`
- `rtk go doc ./executor.Executor.SubmitWithContext`
- `rtk go doc ./executor.CallReducerCmd`
- `rtk go doc ./store.CommittedState.Snapshot`
- `rtk go doc ./query/sql`
