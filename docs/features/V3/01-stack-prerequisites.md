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

## Recorded Baseline 2026-05-01

Inspection commands run:

```sh
rtk go list -json . ./commitlog ./executor ./protocol ./subscription
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
rtk grep -n 'log\.Printf|log\.|RuntimeHealth|HostHealth|RecoveryReport|HTTPHandler' *.go commitlog executor protocol subscription store
rtk rg -n 'log\.Printf' -g '!**/*_test.go' .
```

`rtk go test -list` summarized list-only runs as `Go test: No tests
found`, so test inventory below is from `rtk rg` over `*_test.go` names.

### Public API And Config

- `Config` currently has no SPEC-007 observability fields. Its exported
  fields are runtime, queue, protocol, and auth settings ending with
  `Protocol ProtocolConfig`.
- No public root package types exist yet for `ObservabilityConfig`,
  `RedactionConfig`, `MetricsConfig`, `DiagnosticsConfig`, `TracingConfig`,
  `MetricName`, `MetricLabels`, `MetricsRecorder`, `Tracer`, or `Span`.
- `Runtime.Config()` returns `copyConfig(r.config)`. `copyConfig` currently
  deep-copies only `AuthSigningKey` and `AuthAudiences`; V3 task 02 must
  decide and pin detached-copy behavior for pointer/interface observability
  fields.

### Health And Description Shapes

- `RuntimeHealth` is currently:

  ```go
  type RuntimeHealth struct {
      State     RuntimeState
      Ready     bool
      LastError error
  }
  ```

- `HostHealth` is currently `Modules []HostModuleHealth`; there is no
  host-level `Ready` or `Degraded` boolean yet.
- `RuntimeDescription` is currently `{Module ModuleDescription; Health
  RuntimeHealth}`. `HostDescription` is currently `Modules
  []HostModuleDescription`.
- `Runtime.Health()` locks `r.mu`, returns state, readiness, and `lastErr`,
  and does not inspect subsystem depth, fatal flags, durability state,
  protocol state, subscription state, or recovery facts.
- `Runtime.Describe()` returns module metadata plus `Runtime.Health()`.
  `Host.Health()` and `Host.Describe()` snapshot host module mounts and return
  empty non-nil slices for nil/empty hosts.

### Build And Recovery

- `Build` validates module identity, normalizes config, validates module
  declarations and metadata through preview registry checks, builds the schema
  engine, constructs declared-read catalog, opens or bootstraps committed
  state, and builds a frozen executor reducer registry.
- `openOrBootstrapState` calls `commitlog.OpenAndRecoverDetailed`. On
  `commitlog.ErrNoData`, it creates a fresh committed state, registers schema
  tables, writes snapshot `0`, then calls `OpenAndRecoverDetailed` again.
- `Runtime` stores `recoveredTxID` and `commitlog.RecoveryResumePlan`, but it
  does not currently store a full `commitlog.RecoveryReport`.
- `commitlog.RecoveryReport` is available from
  `commitlog.OpenAndRecoverWithReport` and includes selected snapshot, durable
  log horizon, replayed tx range, recovered tx, resume plan, skipped snapshots,
  damaged tail segments, and segment coverage. Task 03 should switch the root
  build path to retain this report rather than recreating recovery facts.

### Current Process-Global Logging

Production `log.Printf` call sites remain in:

- root declared-read protocol delivery: `declared_read.go`
- commitlog offset-index advisory and replay fallback paths:
  `commitlog/durability.go`, `commitlog/replay.go`, `commitlog/segment.go`
- executor dispatch, subscription registration failure, post-commit fatal,
  dropped-client cleanup, and lifecycle reducer paths:
  `executor/executor.go`, `executor/lifecycle.go`
- protocol error delivery, protocol close, async reducer delivery,
  subscription response delivery, outbound writer, and disconnect paths:
  `protocol/*.go`
- subscription eval and fan-out delivery/drop paths:
  `subscription/eval.go`, `subscription/fanout_worker.go`
- store leaked snapshot finalizer warning: `store/snapshot.go`

These are the task 05 replacement or isolation targets.

### Natural Observation Points

- Build/recovery: `normalizeConfig`, declaration validation failures,
  `openOrBootstrapState`, and the `OpenAndRecoverWithReport` return path.
- Runtime lifecycle: `Start` state transitions, `recordStartFailure`, schema
  engine start, auth/protocol option validation, durability worker creation,
  executor startup, protocol graph creation, ready publication, `Close`
  closing/closed transitions, protocol graph shutdown, executor shutdown, and
  durability close.
- Host lifecycle: `Host.Start` per-module start, partial-start cleanup,
  `Host.Close`, `Host.HTTPHandler` prefix routing, `Host.Health`, and
  `Host.Describe`.
- Protocol: `Runtime.HTTPHandler`/`handleSubscribe` not-ready rejection,
  `protocol.Server.HandleSubscribe` auth, connection ID, compression,
  subprotocol, and upgrade outcomes; `Conn.RunLifecycle`; `runDispatchLoop`
  decode, malformed, unsupported, and inbound-backpressure paths; subscribe,
  unsubscribe, reducer-call, one-off query, and declared-read handlers;
  `ClientSender` enqueue/outbound backpressure; outbound writer; disconnect and
  `ConnManager.CloseAll`.
- Executor/reducer/scheduler: `Submit`, `SubmitWithContext`, `Run`,
  `dispatchSafely`, every `dispatch` command case, `handleCallReducer` lookup,
  permission, reducer user error, reducer panic, commit error, and commit
  success branches; `postCommit` durability enqueue, subscription evaluation,
  caller response, dropped-client drain, and fatal latch; `Startup` scheduler
  replay and dangling-client sweep.
- Durability: `NewDurabilityWorkerWithResumePlan`, `EnqueueCommitted`,
  `processBatch` encode/append/sync/index/rollover branches,
  `WaitUntilDurable`, `DurableTxID`, and `Close`.
- Subscription/fan-out: `RegisterSet`, `UnregisterSet`,
  `EvalAndBroadcast`, `handleEvalError`, `FanOutWorker.Run`,
  `FanOutWorker.deliver`, `handleSendError`, and `markDropped`.

### HTTP Routing Baseline

- `Runtime.HTTPHandler()` creates a new `http.ServeMux` and mounts only
  `/subscribe`.
- `handleSubscribe` returns `503` with `ErrRuntimeNotReady` unless the runtime
  is ready and `protocolServer` is non-nil, then delegates to
  `protocol.Server.HandleSubscribe`.
- No `/healthz`, `/readyz`, `/debug/shunter/runtime`,
  `/debug/shunter/host`, or `/metrics` endpoints exist yet.
- `Host.HTTPHandler()` snapshots module mounts, matches each module prefix,
  strips that prefix, and delegates to the runtime handler; unmatched paths
  return `404`.

### Existing Tests To Preserve Or Extend

- Root build/config/lifecycle/health/description:
  `build_test.go`, `root_validation_test.go`, `lifecycle_test.go`,
  `describe_test.go`, `network_test.go`, `host_test.go`.
- Root public runtime and recovery gauntlets:
  `gauntlet_test.go`, `recovery_gauntlet_test.go`,
  `runtime_crash_gauntlet_test.go`, `runtime_storage_fault_gauntlet_test.go`,
  `rc_app_workload_test.go`.
- Commitlog recovery/durability/report coverage:
  `commitlog/recovery_test.go`,
  `commitlog/recovery_fault_test.go`,
  `commitlog/rapid_replay_test.go`,
  `commitlog/durability_test.go`,
  `commitlog/commitlog_contract_test.go`.
- Executor lifecycle/reducer/scheduler coverage:
  `executor/executor_test.go`, `executor/pipeline_test.go`,
  `executor/lifecycle_test.go`, `executor/startup_test.go`,
  `executor/scheduler_replay_test.go`,
  `executor/scheduler_worker_test.go`,
  `executor/protocol_inbox_adapter_test.go`.
- Protocol connection/message/backpressure/read coverage:
  `protocol/upgrade_test.go`, `protocol/dispatch_test.go`,
  `protocol/disconnect_test.go`, `protocol/backpressure_in_test.go`,
  `protocol/backpressure_out_test.go`, `protocol/handle_callreducer_test.go`,
  `protocol/handle_oneoff_test.go`, `protocol/handle_subscribe_test.go`,
  `protocol/handle_unsubscribe_test.go`.
- Subscription and fan-out coverage:
  `subscription/manager_test.go`, `subscription/register_set_test.go`,
  `subscription/eval_test.go`, `subscription/eval_view_lifetime_test.go`,
  `subscription/fanout_worker_test.go`, `subscription/hash_test.go`,
  `subscription/hash_soak_test.go`.
- Tests currently capturing process-global logs and likely to need adjustment
  when task 05 lands include `protocol/dispatch_test.go`,
  `protocol/disconnect_test.go`, and `subscription/eval_test.go`.

### Stop-Condition Result

No stop condition is tripped for task 02. SPEC-007 has not already landed under
alternate exported names, `commitlog.RecoveryReport` is available, root package
APIs still match the V3 task sequence, and this baseline pass did not expose a
new correctness regression. The main sequencing note is that root `Build`
currently does not retain the full recovery report, so task 03 must make that
change intentionally before health, logging, and metrics rely on recovery
facts.
