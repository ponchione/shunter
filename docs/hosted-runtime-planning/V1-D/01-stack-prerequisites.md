# V1-D Task 01: Reconfirm prerequisites and lifecycle contracts

Parent plan: `docs/hosted-runtime-planning/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md`

Objective: verify V1-D is stacked on V1-C and grounded in the kernel lifecycle contracts that determine startup and shutdown order.

Read first:
- `docs/hosted-runtime-planning/V1-C/2026-04-23_205158-hosted-runtime-v1c-runtime-build-pipeline-implplan.md`

Checks:
- `rtk go list .`
- `rtk go doc ./schema.Engine.Start`
- `rtk go doc ./commitlog.NewDurabilityWorkerWithResumePlan`
- `rtk go doc ./executor.Executor.Startup`
- `rtk go doc ./executor.Executor.SchedulerFor`
- `rtk go doc ./subscription.NewManager`
- `rtk go doc ./subscription.NewFanOutWorker`

Stop if:
- V1-C build/recovery state is not implemented yet
