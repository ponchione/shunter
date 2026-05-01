# V1-C Current Execution Plan

Historical note: V1-C and later hosted-runtime slices have landed. Do not treat
this file as an active handoff; use the relevant feature plan for
current hosted-runtime status.

Parent plan: `docs/features/V1/V1-C/2026-04-23_205158-hosted-runtime-v1c-runtime-build-pipeline-implplan.md`

Goal: complete the hosted runtime V1-C build pipeline so `Build(module, config)` owns non-started durable-state bootstrap/recovery and reducer-registry assembly.

Scope boundaries:
- In scope: config normalization, data-dir creation/defaulting, schema registry capture, committed-state bootstrap/reopen, recovery resume-plan storage, reducer-registry assembly from schema registrations.
- Out of scope: `Runtime.Start`, `Runtime.Close`, durability workers, executor/scheduler/fan-out goroutines, HTTP/WebSocket serving, local calls, protocol handlers, export/introspection, auth config expansion.

Execution sequence:
1. Confirm V1-A/V1-B prerequisites and kernel contracts with `rtk go doc` and focused V1-B tests.
2. Add root-package tests in `runtime_build_test.go` that fail before V1-C implementation:
   - `Build` bootstraps committed state for module tables in a temp data dir.
   - a fresh equivalent module can reopen previously bootstrapped state from the same dir.
   - blank public `Config.DataDir` remains valid and privately normalizes to `./shunter-data`.
   - `Build` creates a frozen private executor reducer registry containing normal reducers and lifecycle hooks.
3. Update existing success-path root tests to pass temp data dirs so V1-C default-data-dir behavior does not pollute the repository except in the explicit chdir-isolated default test.
4. Verify RED with focused `rtk go test . -run ... -count=1`; expected failure is missing V1-C private runtime fields/helpers.
5. Implement V1-C minimally:
   - add runtime-private fields: registry, dataDir, state, recoveredTxID, resumePlan, reducers.
   - add config normalization constants/helper.
   - add `openOrBootstrapState` using `commitlog.OpenAndRecoverDetailed`, `store.NewCommittedState`, `store.NewTable`, and `commitlog.NewSnapshotWriter`.
   - add `buildExecutorReducerRegistry` using `executor.NewReducerRegistry`, schema registry reducers, and `OnConnect`/`OnDisconnect` lifecycle wrappers.
   - wire helpers into `Build` after schema build succeeds and before returning `Runtime`.
6. Verify GREEN with focused V1-C tests, then root/schema/commitlog/executor vet/test gates and broad `rtk go test ./... -count=1` if feasible.

Guardrails:
- Do not inspect or copy the former bundled demo command as source of truth.
- Preserve schema-layer error wrapping via `errors.Is`.
- Preserve V1-A validation ordering before schema build.
- Do not create any started resource in `Build`.
