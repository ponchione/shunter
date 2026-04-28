# Hosted Runtime Planning Handoff

Use this file as the primary cross-agent handoff for hosted-runtime planning or
implementation work.

Use `NEXT_SESSION_HANDOFF.md` instead for TECH-DEBT / correctness work.

## Current Target

The active hosted-runtime slice is V1.5-E migration metadata, contract diffs,
and warning policy checks.

Next agent run: complete V1.5-E end to end, from prerequisites through tests,
implementation, contract-diff tooling, warning policy checks, validation, and
handoff upkeep.

Start from:
- `docs/hosted-runtime-planning/V1.5/README.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-E/00-current-execution-plan.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-E/01-stack-prerequisites.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-E/02-migration-metadata-tests.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-E/03-metadata-implementation.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-E/04-contract-diff-tooling.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-E/05-warning-policy-checks.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-E/06-format-and-validate.md`
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

V1.5-D is complete. Its live proof is:
- `PermissionMetadata` and `ReadModelMetadata` exist in the root package.
- `Module.Reducer(...)` accepts `WithReducerPermissions(...)` without breaking
  existing two-argument reducer registrations.
- `QueryDeclaration` and `ViewDeclaration` carry passive `Permissions` and
  `ReadModel` metadata.
- `Runtime.ExportContract()` exports permission metadata for reducers,
  queries, and views plus read-model metadata for queries and views.
- absent permission/read-model metadata serializes deterministically as empty
  contract arrays.
- TypeScript codegen exposes passive `permissions` and `readModels` constants.
- no runtime access-control enforcement, policy engine, migration metadata, or
  runtime shape change was added.

V1.5-D validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*Permission|Test.*ReadModel|Test.*Contract' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./codegen -count=1`
- `rtk go vet ./codegen`

Known non-slice validation blocker:
- `rtk go test ./... -count=1` currently fails in
  `store.TestRapidStoreCommitMatchesModel`.
- The same failure reproduces with
  `rtk go test ./store -run TestRapidStoreCommitMatchesModel -count=1`.
- V1.5-D did not touch `store/`, and the V1.5-D handoff explicitly avoided
  rapid/fuzz-style store work.

V1.5-E goal:
- make schema/module evolution visible and reviewable without executing
  migrations
- export descriptive module-level and declaration-level migration metadata
- add deterministic contract-diff tooling against `shunter.contract.json`
- add warning/CI-oriented policy checks
- keep runtime startup non-blocking for missing or risky migration metadata

## Current Hosted-Runtime State

V1-H, V1.5-A, V1.5-B, V1.5-C, and V1.5-D are audited as landed.

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
- permission/read-model metadata is passive, exported in the canonical
  contract, and visible to generated TypeScript clients
- root/runtime package tests are the live proof for hosted-runtime ownership,
  serving, local calls, describe, export, and lifecycle behavior
- the prior bundled hello-world command was removed because it no longer served
  a maintained product or integration purpose

Do not reopen V1-A through V1-H, V1.5-A, V1.5-B, V1.5-C, or V1.5-D unless a
new failing regression proves drift.

## Startup Reading

Required:
1. `RTK.md`
2. this file
3. `docs/hosted-runtime-planning/V1.5/README.md`
4. `docs/hosted-runtime-planning/V1.5/V1.5-E/00-current-execution-plan.md`
5. `docs/hosted-runtime-planning/V1.5/V1.5-E/01-stack-prerequisites.md`
6. `docs/hosted-runtime-planning/V1.5/V1.5-E/02-migration-metadata-tests.md`
7. `docs/hosted-runtime-planning/V1.5/V1.5-E/03-metadata-implementation.md`
8. `docs/hosted-runtime-planning/V1.5/V1.5-E/04-contract-diff-tooling.md`
9. `docs/hosted-runtime-planning/V1.5/V1.5-E/05-warning-policy-checks.md`
10. `docs/hosted-runtime-planning/V1.5/V1.5-E/06-format-and-validate.md`
11. `docs/hosted-runtime-planning/V1.5/V1.5-D/00-current-execution-plan.md`
    only if V1.5-D proof is needed

V1.5-E Task 01 should run and record these checks:
- `rtk go doc . Runtime.ExportContract`
- inspect the V1.5-B contract JSON tests
- inspect the V1.5-D metadata attachment patterns

Open these only when the active V1.5-E docs or live code leave a contract,
metadata, or diff-tooling question unresolved:
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`

Do not read broad roadmap, ledger, or decomposition docs by default.

## V1.5-E Scope

In scope:
- descriptive migration metadata types
- module-level migration/version compatibility summary
- declaration-level migration metadata where the V1.5-E task docs require it
- canonical contract export of migration metadata
- deterministic contract-diff tooling comparing current exports against a
  previous `shunter.contract.json`
- warning/CI policy checks for missing metadata, risky changes, and
  declared-vs-inferred mismatches

Out of scope:
- executable migration runners
- runtime-blocking migration metadata enforcement
- automatic stored-state rewrites, rollback, or deployment orchestration
- full SQL/view system
- query engine surface widening
- runtime shape changes
- multi-module hosting, out-of-process module execution, or control-plane work

Preserve WebSocket-first v1 runtime behavior.

## Next Slice Notes

Complete V1.5-E in one run. Work through its numbered tasks in order:
- reconfirm contract, metadata, and snapshot surfaces
- add failing tests for descriptive migration metadata
- implement module-level and declaration-level migration metadata
- add deterministic contract-diff tooling
- add warning/CI-oriented policy checks
- format, test, vet, and update handoffs

Prerequisite proof should verify V1.5-E builds on the canonical contract
without changing runtime startup semantics:
- canonical contract JSON is the source of truth for diffs
- `shunter.contract.json` remains the recommended snapshot path
- migration metadata is descriptive and exported
- runtime startup must not fail solely because migration metadata is missing or
  risky
- tooling/CI may warn or fail based on project policy

## Coordination Notes

There may be concurrent gauntlet/dependency test work in the same worktree.
For V1.5-E, avoid touching these unless the user explicitly redirects the work:
- `docs/RUNTIME-HARDENING-GAUNTLET.md`
- `go.mod`
- `go.sum`
- `runtime_gauntlet_test.go`
- rapid/fuzz-style test files under `bsatn/`, `commitlog/`, `query/sql/`, and
  `store/`

Do not push unless explicitly asked.

## Validation

Expected V1.5-E validation:
- `rtk go fmt <touched packages>`
- `rtk go test ./... -run 'Test.*Migration|Test.*ContractDiff|Test.*Policy' -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet <touched packages>`

If V1.5-E creates a command package, include that package explicitly in format
and vet commands.

Current broad-suite caveat:
- `rtk go test ./... -count=1` is blocked by
  `store.TestRapidStoreCommitMatchesModel` until that non-slice rapid test is
  fixed or quarantined.

Pinned Staticcheck is available as `rtk go tool staticcheck ./...`. Use it for
static-analysis visibility when relevant, but do not treat a broad green run as
required until OI-008 cleanup clears known findings and any dirty compile
blockers.

Do not claim a Go implementation slice is complete until the relevant Go
commands pass.

## Handoff Upkeep

When V1.5-E completes:
- update task progress in `docs/hosted-runtime-planning/V1.5/V1.5-E/00-current-execution-plan.md`
- update this file to make the next hosted-runtime target explicit
- keep startup reading minimal
- record only future-relevant state, not closure archaeology
