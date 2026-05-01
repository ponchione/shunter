# V1-D Current Execution Plan

Historical note: V1-D and later hosted-runtime slices have landed. Do not treat
this file as an active handoff; use the relevant feature plan for
current hosted-runtime status.

Parent plan: `docs/features/V1/V1-D/2026-04-23_210537-hosted-runtime-v1d-runtime-lifecycle-ownership-implplan.md`

Goal: complete hosted runtime V1-D by adding lifecycle ownership to the V1-C-built runtime: `Start(ctx)`, `Close()`, readiness, health, and private ownership of started kernel resources.

Scope boundaries:
- In scope: lifecycle state machine, readiness/health, durability worker creation/closure, subscription manager and internal fan-out worker, executor/scheduler construction, executor startup sequencing, safe shutdown order, startup cancellation/retry, partial-start cleanup.
- Out of scope: HTTP handlers, WebSocket serving, listeners/sockets, protocol server construction, protocol-backed fan-out delivery, local reducer/query APIs, export/introspection, auth surface expansion.

Grounded kernel contracts:
- `schema.Engine.Start(ctx)` performs schema startup compatibility checks.
- `commitlog.NewDurabilityWorkerWithResumePlan(...)` starts a durability worker immediately, so V1-D creates it only inside `Runtime.Start` and closes it on failure/close.
- `executor.Executor.Startup(ctx, scheduler)` must run before `Scheduler.Run`, `Executor.Run`, and protocol admission.
- `executor.Executor.SchedulerFor()` documents scheduler must stop before `Executor.Shutdown()` closes the inbox.
- `subscription.NewManager(...)` is construction-only; `subscription.NewFanOutWorker(...).Run(ctx)` is the goroutine.
- `subscription.FanOutSender` remains protocol-backed in V1-E; V1-D uses an unexported no-op sender.

Execution sequence:
1. Confirm V1-D follows V1-C and re-check kernel contracts with `rtk go doc`.
2. Add RED tests in `runtime_lifecycle_test.go` for:
   - built runtime initial health/not-ready state
   - `Start(ctx)` readiness and non-blocking ownership
   - `Close()` readiness clearing and closed state
   - repeated `Start` after ready is idempotent
   - repeated `Close` is idempotent
   - `Close()` before `Start()` marks closed and `Start` after close returns `ErrRuntimeClosed`
   - canceled startup returns `context.Canceled`, records `LastError`, remains retryable
   - injected partial-start failure cleans durability/resources and remains retryable
3. Verify RED with focused `rtk go test . -run ... -count=1`; expected failure is missing lifecycle public methods/types/fields.
4. Implement V1-D minimally:
   - add `RuntimeState`, `RuntimeHealth`, `ErrRuntimeStarting`, `ErrRuntimeClosed`
   - add lifecycle fields to `Runtime`: mutex-protected state/last error, atomic ready flag, lifecycle context/cancel, started subsystem handles
   - initialize `RuntimeStateBuilt` in `Build`
   - add `Ready()` and `Health()`
   - add private no-op fan-out sender
   - add `Start(ctx)` with startup context semantics and private runtime-owned lifecycle context
   - add `Close()` and shared cleanup helper with scheduler/fan-out stop before executor shutdown, durability close last
   - add a narrow unexported test seam for injected partial-start failure only
5. Verify GREEN with focused lifecycle tests, then root/touched package tests and vet, then broad `rtk go test ./... -count=1`.

Guardrails:
- Do not inspect or copy the former bundled demo command as implementation source of truth.
- Do not add public network or local-call methods.
- Do not instantiate protocol server or listeners.
- Do not leave long-lived goroutines after tests; every started runtime is closed.
