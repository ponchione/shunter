# V3 Task 01: Reconfirm Observability Stack Prerequisites

Parent plan: `docs/features/V3/00-current-execution-plan.md`

Depends on:
- `RTK.md`
- `docs/specs/007-observability/SPEC-007-observability.md`

Objective: verify V3 is stacked on the live hosted runtime, recovery,
protocol, executor, subscription, and host code before implementation begins.

## Required Context

Read:
- `docs/features/V3/README.md`
- `docs/features/V3/00-current-execution-plan.md`
- `docs/specs/007-observability/SPEC-007-observability.md`

Inspect:

```sh
rtk go list -json .
rtk go list -json ./commitlog
rtk go list -json ./executor
rtk go list -json ./protocol
rtk go list -json ./subscription
rtk go doc . Config
rtk go doc . Runtime
rtk go doc . Runtime.Health
rtk go doc . Runtime.Describe
rtk go doc . Runtime.HTTPHandler
rtk go doc . Host
rtk go doc . Host.Health
rtk go doc . Host.HTTPHandler
rtk go doc ./commitlog RecoveryReport
rtk go doc ./commitlog DurabilityWorker
rtk go doc ./executor Executor
rtk go doc ./protocol Server
rtk go doc ./subscription Manager
rtk grep -n 'log\\.Printf|log\\.|RuntimeHealth|HostHealth|RecoveryReport|HTTPHandler' *.go commitlog executor protocol subscription store
```

Read only if needed:
- `config.go`
- `build.go`
- `runtime.go`
- `lifecycle.go`
- `describe.go`
- `host.go`
- `network.go`
- `commitlog/recovery.go`
- `commitlog/durability.go`
- `executor/executor.go`
- `executor/lifecycle.go`
- `protocol/*.go`
- `subscription/*.go`
- `store/snapshot.go`

## Prerequisite Conclusions To Record

Update this file with the observed facts before starting task 02:

- whether `Config` already has any observability fields
- current `RuntimeHealth`, `HostHealth`, `RuntimeDescription`, and
  `HostDescription` shapes
- how `Build` opens or bootstraps committed state and whether it keeps a full
  `commitlog.RecoveryReport`
- current production process-global logging call sites
- where runtime lifecycle, protocol connection/message handling, executor
  command handling, reducer invocation, durability queueing, subscription
  evaluation, and fan-out expose natural observation points
- current HTTP handler routing behavior for `/subscribe`
- any tests that already pin health, host, network, recovery, or logging
  behavior and will need adjustment

## Stop If

Stop and update the V3 plan before implementation if:

- live code has already landed part of SPEC-007 under different exported names
- recovery facts are no longer available from `commitlog.RecoveryReport`
- root package APIs have moved enough that the task sequence would cause
  broad unrelated rewrites
- existing tests reveal a correctness regression that should be fixed before
  observability work starts

