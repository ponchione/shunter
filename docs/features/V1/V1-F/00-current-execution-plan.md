# Hosted Runtime V1-F Current Execution Plan

Goal: continue hosted-runtime V1-F one task at a time, starting with prerequisite reconfirmation and then moving to local reducer-call tests/implementation only after the stack is proven ready.

Grounded facts from this audit:
- V1-A through V1-E root/runtime surfaces exist in this checkout.
- `rtk go list .` succeeds for `github.com/ponchione/shunter`.
- Root V1-A/V1-D/V1-E focused tests and touched kernel-package tests passed during the audit.
- `Runtime.Start`, `Runtime.Close`, `Runtime.Ready`, `Runtime.HTTPHandler`, and `Runtime.ListenAndServe` are exported and documented.
- `executor.Executor.Submit` is the lower-level in-process command seam.
- The landed `Runtime.CallReducer` implementation uses `executor.Executor.SubmitWithContext`
  with `CallSourceExternal` so local calls preserve the same startup admission and
  permission semantics as the protocol path.
- `executor.CallReducerCmd` supplies a buffered local response path through `executor.ReducerResponse`.
- `store.CommittedState.Snapshot` returns a committed read snapshot that must be closed promptly.
- `query/sql` remains a narrow parser/coercion package, not a broad public query engine.

Audit fixes made before V1-F execution:
- Refreshed stale root comments in `module.go`, `config.go`, and `runtime.go` that still described earlier V1-A/V1-C-only scope even though later slices have expanded the live API.

Scope for V1-F:
- Add local reducer invocation through the runtime-owned executor.
- Add explicit local caller options and deterministic dev/test identity defaults.
- Add a minimal callback-owned read API over committed snapshots.
- Keep local calls secondary to the WebSocket-first external client model.

Non-goals:
- No new network serving APIs.
- No REST/MCP/admin/control-plane surface.
- No export/introspection work.
- No hello-world/example replacement.
- No v1.5 query/view declarations, contract export, codegen, permissions, or migration metadata.
- No broad SQL helper unless it can reuse a small shared one-off evaluator without protocol duplication; default plan is to defer SQL and ship the read callback surface.

Task sequence, one task at a time:
1. Complete `01-stack-prerequisites.md` by reconfirming the prior slices and local-call kernel seams. Stop and report.
2. Add RED tests in `runtime_local_test.go` for local reducer readiness gates and reducer result behavior.
3. Implement local reducer result/options plus `Runtime.CallReducer` through `executor.CallReducerCmd`.
4. Add and implement the minimal `Read(ctx, func(LocalReadView) error)` callback-owned snapshot API.
5. Format and validate with the V1-F gates.

Task progress:
- Task 01 complete: prerequisite seams and prior runtime slices reconfirmed.
- Task 02 complete: RED local reducer-call tests added in `runtime_local_test.go` and confirmed failing on the missing public API before implementation.
- Task 03 complete: `runtime_local.go` now exposes executor-aligned local reducer result/status aliases, caller options, deterministic local identity defaults, and `Runtime.CallReducer` using `executor.CallReducerCmd` plus `executor.Executor.SubmitWithContext`.
- Task 04 complete: `runtime_local.go` now exposes `ErrLocalReadNilCallback`, callback-scoped `LocalReadView`, and `Runtime.Read(ctx, func(LocalReadView) error)` over committed snapshots; `runtime_local_test.go` verifies nil callback ordering, readiness/starting/closed sentinels, committed row access, and snapshot closure via a post-read reducer call.
- Task 05 complete: V1-F format/test/vet gates passed for the root package and touched executor/store/query seams.

Latest Task 05 validation:
- `rtk go fmt .` -> passed.
- `rtk go test . -count=1` -> passed, 59 tests.
- `rtk go test ./executor ./store ./query/sql -count=1` -> passed, 420 tests across 3 packages.
- `rtk go vet . ./executor ./store ./query/sql` -> passed.

Historical sequencing note: later hosted-runtime slices have since landed. Do
not treat this completed V1-F plan as a live handoff; use
`docs/internal/HOSTED_RUNTIME_PLANNING_HANDOFF.md` for current hosted-runtime status.
