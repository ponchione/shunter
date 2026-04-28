# Hosted Runtime Planning Handoff

Use this file as the primary cross-agent handoff for hosted-runtime planning or
implementation work.

Use `NEXT_SESSION_HANDOFF.md` instead for TECH-DEBT / correctness work.

## Current Target

The active hosted-runtime slice is V1.5-D permissions/read-model metadata.

Next agent run: complete V1.5-D end to end, from prerequisites through tests,
implementation, contract/export metadata, validation, and handoff upkeep.

Start from:
- `docs/hosted-runtime-planning/V1.5/README.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-D/00-current-execution-plan.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-D/01-stack-prerequisites.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-D/02-permission-metadata-tests.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-D/03-metadata-implementation.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-D/04-format-and-validate.md`
- `module.go`
- `module_declarations.go`
- `runtime_contract.go`
- `runtime_contract_test.go`
- `codegen/codegen.go`
- `codegen/typescript.go`
- `codegen/codegen_test.go`

## Execution Granularity

V1.5 work should proceed one lettered slice at a time, not one numbered task at
a time. A normal agent run should complete the whole active `V1.5-*` slice:
1. read the slice execution plan and task docs
2. run prerequisite inspection commands
3. add the planned failing tests
4. implement the slice
5. expose any required metadata/output surface
6. run the slice validation gates
7. update the slice plan and this handoff to the next `V1.5-*` slice

Do not stop after a prerequisite or failing-test task unless there is a real
blocker, an ambiguous contract decision, or a validation failure that cannot be
resolved inside the active slice.

V1.5-A is complete. Its live proof is:
- `QueryDeclaration` and `ViewDeclaration` exist in the root package.
- `Module.Query(...)` and `Module.View(...)` register module-owned query/view
  declarations fluently.
- declaration names are validated during `Build`; blank names and duplicate
  names are rejected, and query/view names share one namespace.
- `Module.Describe` and `Runtime.Describe` expose detached query/view
  declaration summaries through `Queries` and `Views`.
- `Runtime.ExportSchema` remains the lower-level schema/reducer export.

V1.5-A validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test(Module.*Declaration|Runtime.*Declaration|.*Describe.*Declaration)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

V1.5-B is complete. Its live proof is:
- `ModuleContract` exists in the root package as the canonical full module
  contract artifact.
- `Runtime.ExportContract()` combines module identity/version/metadata,
  schema export, reducers, queries, views, reserved permission/read-model and
  migration sections, and codegen/export metadata.
- `Runtime.ExportContractJSON()` and `ModuleContract.MarshalCanonicalJSON()`
  produce deterministic indented JSON with a trailing newline.
- `DefaultContractSnapshotFilename` is `shunter.contract.json`.
- contract export returns detached values and works before `Runtime.Start` and
  after `Runtime.Close`.
- permission, read-model, and migration sections are reserved metadata only;
  no executable behavior was added.

V1.5-B validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*Contract|Test.*Export.*JSON' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./schema -count=1`

V1.5-C is complete. Its live proof is:
- `codegen.Generate(...)` accepts detached `ModuleContract` values.
- `codegen.GenerateFromJSON(...)` accepts canonical `ModuleContract` JSON.
- `LanguageTypeScript` is the only supported generator target.
- generated TypeScript is deterministic and covers table row types, table
  subscription helpers, reducer raw-byte call helpers, declared query helpers,
  and declared view/subscription helpers.
- lifecycle reducers are exposed separately from normal callable reducer
  helpers.
- unsupported language values and unusable contract input fail clearly.
- no CLI command package was added in V1.5-C; the reusable package surface is
  the landed artifact.

V1.5-C validation passed:
- `rtk go fmt ./codegen`
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet ./codegen`

V1.5-D goal:
- attach narrow permission/read-model metadata to reducers, queries, and views
- export metadata in the canonical contract
- keep metadata passive and inspectable by tooling/codegen
- avoid adding runtime access-control enforcement or a broad policy language

## Current Hosted-Runtime State

V1-H, V1.5-A, V1.5-B, and V1.5-C are audited as landed.

Live proof points:
- root package imports as `github.com/ponchione/shunter`
- `Module`, `Config`, `Runtime`, and `Build(...)` exist
- `Runtime.Start`, `Close`, `HTTPHandler`, `ListenAndServe`, local calls,
  describe, and schema export exist
- `Module` registration remains fluent and code-first
- `Module.Query(...)` and `Module.View(...)` register named read/view
  declarations
- `Module.Describe` exposes detached module identity plus `Queries` and `Views`
- `Runtime.Describe` exposes module identity/declarations plus runtime health
- `Runtime.ExportSchema` currently exposes lower-level schema/reducer metadata:
  `Version`, `Tables`, and `Reducers`
- `Runtime.ExportContract` exposes the full canonical module contract for
  codegen and reviewable JSON snapshots
- `Runtime.ExportContractJSON` exposes deterministic canonical contract JSON
- `codegen.Generate` and `codegen.GenerateFromJSON` generate deterministic
  TypeScript client bindings from the canonical contract without starting a
  runtime
- root/runtime package tests are the live proof for hosted-runtime ownership,
  serving, local calls, describe, export, and lifecycle behavior
- the prior bundled hello-world command was removed because it no longer served
  a maintained product or integration purpose

Do not reopen V1-A through V1-H, V1.5-A, V1.5-B, or V1.5-C unless a new failing
regression proves drift.

## Startup Reading

Required:
1. `RTK.md`
2. this file
3. `docs/hosted-runtime-planning/V1.5/README.md`
4. `docs/hosted-runtime-planning/V1.5/V1.5-D/00-current-execution-plan.md`
5. `docs/hosted-runtime-planning/V1.5/V1.5-D/01-stack-prerequisites.md`
6. `docs/hosted-runtime-planning/V1.5/V1.5-D/02-permission-metadata-tests.md`
7. `docs/hosted-runtime-planning/V1.5/V1.5-D/03-metadata-implementation.md`
8. `docs/hosted-runtime-planning/V1.5/V1.5-D/04-format-and-validate.md`
9. `docs/hosted-runtime-planning/V1.5/V1.5-C/00-current-execution-plan.md` only
   if V1.5-C proof is needed

V1.5-D Task 01 should run and record these checks:
- `rtk go doc . Module`
- `rtk go doc . Runtime.ExportContract`
- `rtk go doc ./schema ReducerExport`

Open these only when the active V1.5-D docs or live code leave a contract or
metadata question unresolved:
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`

Do not read broad roadmap, ledger, or decomposition docs by default.

## V1.5-D Scope

In scope:
- small permission/read-model metadata types
- reducer metadata declarations
- query metadata declarations
- view/subscription metadata declarations
- canonical contract export of that metadata
- deterministic absent metadata representation
- codegen visibility if metadata becomes part of generated output

Out of scope:
- runtime-blocking authorization behavior
- broad standalone policy/auth framework
- complex multi-tenant policy language
- migration metadata, contract diff tooling, or executable migrations
- full SQL/view system
- query engine surface widening
- runtime shape changes
- multi-module hosting, out-of-process module execution, or control-plane work

Preserve WebSocket-first v1 runtime behavior.

## Next Slice Notes

Complete V1.5-D in one run. Work through its numbered tasks in order:
- reconfirm declaration, contract, and codegen surfaces
- add failing permission/read-model metadata tests
- implement narrow metadata on reducers, queries, and views
- format, test, vet, and update handoffs

Prerequisite proof should verify V1.5-D annotates existing exported surfaces
without changing runtime behavior:
- reducers are already exported through schema/contract metadata
- queries and views are exported through V1.5-A/V1.5-B metadata
- metadata should live near the declaration it governs
- metadata should remain passive and inspectable
- codegen may expose metadata but must not enforce it

## Coordination Notes

There may be concurrent gauntlet/dependency test work in the same worktree.
For V1.5-D, avoid touching these unless the user explicitly redirects the work:
- `docs/RUNTIME-HARDENING-GAUNTLET.md`
- `go.mod`
- `go.sum`
- `runtime_gauntlet_test.go`
- rapid/fuzz-style test files under `bsatn/`, `commitlog/`, `query/sql/`, and
  `store/`

Do not push unless explicitly asked.

## Validation

Expected V1.5-D validation:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*Permission|Test.*ReadModel|Test.*Contract' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

Expand when codegen output changes:
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./... -count=1`

Pinned Staticcheck is available as `rtk go tool staticcheck ./...`. Use it for
static-analysis visibility when relevant, but do not treat a broad green run as
required until OI-008 cleanup clears known findings and any dirty compile
blockers.

Do not claim a Go implementation slice is complete until the relevant Go
commands pass.

## Handoff Upkeep

When V1.5-D completes:
- update task progress in `docs/hosted-runtime-planning/V1.5/V1.5-D/00-current-execution-plan.md`
- update this file to make V1.5-E migration metadata / contract diffs the
  current target
- keep startup reading minimal
- record only future-relevant state, not closure archaeology
