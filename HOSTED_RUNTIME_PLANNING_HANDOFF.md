# Hosted Runtime Planning Handoff

Use this file as the primary cross-agent handoff for hosted-runtime planning or
implementation work.

Use `NEXT_SESSION_HANDOFF.md` instead for TECH-DEBT / correctness work.

## Current Target

The active hosted-runtime implementation target is `V2-C`: migration planning
and validation.

V2-B contract artifact admin and CLI workflows are complete.
V2-A runtime/module boundary hardening is complete.

V1.5-E migration metadata, contract diffs, and warning policy checks are
complete, which completes the initial V1.5 hosted-runtime follow-on plan.

V2 planning is now decomposed under `docs/hosted-runtime-planning/V2/`,
starting from the code-grounded source direction in
`docs/decomposition/hosted-runtime-v2-directions.md`.

Next hosted-runtime work should start from `V2-C` unless a newer explicit user
target supersedes this handoff. Do not reopen V1-H or V1.5-A through V1.5-E
unless a new failing regression proves drift.

## Execution Granularity

V2 work should proceed one lettered slice at a time, not one numbered task at a
time. A normal hosted-runtime handoff should complete the whole active `V2-*`
slice:
1. read the slice execution plan and task docs
2. run prerequisite inspection commands
3. add the planned failing tests
4. implement the slice
5. expose any required metadata/output surface
6. run the slice validation gates
7. update the slice plan and this handoff with live proof, validation results,
   and the next `V2-*` slice

Do not stop after a prerequisite or failing-test task unless there is a real
blocker, an ambiguous contract decision, or a validation failure that cannot be
resolved inside the active slice.

Do not hand off a V2 slice as complete until its focused tests, format command,
and required vet/broader validation gates have passed and are recorded here.

V2-A is complete. Its live proof is:
- `Runtime` now groups app-authored module identity, metadata, reducer
  declaration metadata, query/view declarations, migration metadata, and table
  migrations behind the private `moduleSnapshot`.
- `Build` creates that snapshot after module/config/schema/state/reducer
  validation and before returning the runtime.
- mutating the original `Module`, registration input slices, or returned
  `Runtime.Describe` values does not affect later runtime descriptions or
  contract exports.
- runtime-owned schema engine, registry, committed state, reducer registry,
  subscriptions, executor, scheduler, protocol graph, and lifecycle resources
  remain owned by `Runtime` fields separate from the module snapshot.
- `Runtime.Describe`, `Runtime.ExportSchema`, `Runtime.ExportContract`, and
  `Runtime.ExportContractJSON` remain detached public outputs.
- no new public structural API, multi-module behavior, process isolation,
  migration execution, or policy enforcement was added.

V2-A validation passed:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Boundary|Describe|Export|Contract|Lifecycle|Build)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -count=1`

V2-B is complete. Its live proof is:
- `contractworkflow.CompareFiles` diffs previous/current canonical
  `ModuleContract` JSON files through `contractdiff.CompareJSON`.
- `contractworkflow.CheckPolicyFiles` runs deterministic migration/contract
  policy checks through `contractdiff.CheckPolicy`, preserving default
  non-fatal warnings and strict failure status.
- `contractworkflow.GenerateFromFile` and `GenerateFile` generate TypeScript
  bindings from contract JSON through `codegen.GenerateFromJSON`.
- `contractworkflow.FormatDiff` and `FormatPolicy` render deterministic text
  and JSON workflow outputs.
- `cmd/shunter` exposes `contract diff`, `contract policy`, and
  `contract codegen` over existing JSON files only.
- generic CLI help documents that module export belongs in app-owned binaries
  via `Runtime.ExportContractJSON`.
- no dynamic module loading, generic module export, runtime startup,
  reducer/query admin commands, cloud control plane, or multi-module host
  commands were added.

V2-B validation passed:
- `rtk go fmt ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`
- `rtk go test ./codegen ./contractdiff ./contractworkflow ./cmd/shunter -count=1`
- `rtk go test ./... -run 'Test.*(Contract|Codegen|Diff|Policy)' -count=1`
- `rtk go vet ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`

The completed V1.5 proof below is historical context and should not be treated
as an active target.

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
- permission and read-model sections were reserved metadata only; migration
  metadata was later populated in V1.5-E.

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

V1.5-E is complete. Its live proof is:
- `MigrationMetadata`, compatibility constants, and classification constants
  exist in the root package.
- `Module.Migration(...)` exports descriptive module-level migration metadata.
- `Module.TableMigration(...)` plus `QueryDeclaration.Migration` and
  `ViewDeclaration.Migration` export declaration-level migration metadata.
- `Runtime.ExportContract()` and `Runtime.ExportContractJSON()` include
  deterministic migration metadata without changing runtime startup behavior.
- `contractdiff.Compare(...)` and `contractdiff.CompareJSON(...)` report
  deterministic additive, breaking, and metadata-only contract changes.
- `contractdiff.CheckPolicy(...)` reports warning/CI-oriented migration
  metadata findings, with warnings non-fatal by default and strict mode
  explicitly opt-in.
- no executable migration runner, state rewrite, or startup-blocking migration
  enforcement was added.

V1.5-E validation passed:
- `rtk go fmt . ./store ./contractdiff`
- `rtk go test ./... -run 'Test.*Migration|Test.*ContractDiff|Test.*Policy' -count=1`
- `rtk go test . -count=1`
- `rtk go test ./contractdiff -count=1`
- `rtk go test ./codegen -count=1`
- `rtk go test ./store -run TestRapidStoreCommitMatchesModel -count=50`
- `rtk go test ./... -count=1`
- `rtk go vet . ./store ./contractdiff ./codegen`

Former non-slice validation blocker resolved:
- `store.TestRapidStoreCommitMatchesModel` exposed a transaction undelete
  constraint bug. A failed undelete now checks transaction-local unique/set
  conflicts before canceling the pending delete.

## Current Hosted-Runtime State

V1-H, V1.5-A through V1.5-E, V2-A, and V2-B are audited as landed.

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
  codegen, reviewable JSON snapshots, migration metadata, and diff tooling
- `Runtime.ExportContractJSON` exposes deterministic canonical contract JSON
- `codegen.Generate` and `codegen.GenerateFromJSON` generate deterministic
  TypeScript client bindings from the canonical contract without starting a
  runtime
- permission/read-model metadata is passive, exported in the canonical
  contract, and visible to generated TypeScript clients
- migration metadata is passive, exported in the canonical contract, and
  consumed by contract-diff/policy tooling only
- V2-A hardened the runtime/module boundary with a private `moduleSnapshot`
  and boundary tests without changing public app-author APIs
- V2-B added reusable JSON-file contract workflows in `contractworkflow` and a
  generic `cmd/shunter` CLI for `contract diff`, `contract policy`, and
  `contract codegen`
- generic contract workflows operate only on existing canonical JSON files;
  app-owned export remains based on `Runtime.ExportContractJSON`
- root/runtime package tests are the live proof for hosted-runtime ownership,
  serving, local calls, describe, export, and lifecycle behavior
- the prior bundled hello-world command was removed because it no longer served
  a maintained product or integration purpose

Do not reopen V1-A through V1-H or V1.5-A through V1.5-E unless a new failing
regression proves drift.

## Startup Reading

Required:
1. `RTK.md`
2. this file
3. `docs/hosted-runtime-planning/V2/README.md`
4. the active V2 slice execution plan and task docs

For the current V2-C target, start from:
- `docs/hosted-runtime-planning/V2/V2-C/00-current-execution-plan.md`
- `docs/hosted-runtime-planning/V2/V2-C/01-stack-prerequisites.md`
- `docs/hosted-runtime-planning/V2/V2-C/02-plan-report-tests.md`
- `docs/hosted-runtime-planning/V2/V2-C/03-migration-plan-model.md`
- `docs/hosted-runtime-planning/V2/V2-C/04-read-only-validation-hooks.md`
- `docs/hosted-runtime-planning/V2/V2-C/05-format-and-validate.md`

For V1.5-E audit only, start from:
- `docs/hosted-runtime-planning/V1.5/README.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-E/00-current-execution-plan.md`
- `runtime_migration_test.go`
- `runtime_contract.go`
- `contractdiff/`

Open this only when V1.5-E audit docs or live code leave a contract, metadata,
or diff-tooling question unresolved:
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`

Do not read broad roadmap, ledger, or decomposition docs by default.

## Completed V1.5-E Scope

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

## V2 Planning State

Current active V2 slice:
- `V2-C`: migration planning and validation

Completed V2 slices:
- `V2-A`: runtime/module boundary hardening
- `V2-B`: contract artifact admin and CLI workflows

V2 planning slices are:
1. `V2-A`: runtime/module boundary hardening
2. `V2-B`: contract artifact admin and CLI workflows
3. `V2-C`: migration planning and validation
4. `V2-D`: declared read and SQL protocol convergence
5. `V2-E`: policy/auth enforcement foundation
6. `V2-F`: multi-module hosting exploration
7. `V2-G`: out-of-process module execution gate

If V2 implementation starts, begin with:
- `docs/hosted-runtime-planning/V2/README.md`
- the selected slice `00-current-execution-plan.md`
- that slice's `01-stack-prerequisites.md`
- live code/package docs named by the slice

Do not start V2-D or later until V2-C is complete or explicitly deferred with
the reason recorded here.

## Next Slice Notes

No next V1.5 slice is queued. The active hosted-runtime implementation slice is
V2-C.

After V2-C completes, update this handoff to:
- mark V2-C complete with live proof and validation commands
- set `V2-D` as the active target
- preserve the rule that each handoff completes one full lettered slice,
  including tests and validations

## Coordination Notes

There may be concurrent gauntlet/dependency test work in the same worktree.
Avoid touching these unless the user explicitly redirects the work:
- `docs/RUNTIME-HARDENING-GAUNTLET.md`
- `go.mod`
- `go.sum`
- `runtime_gauntlet_test.go`
- rapid/fuzz-style test files under `bsatn/`, `commitlog/`, `query/sql/`, and
  `store/`

Do not push unless explicitly asked.

## Validation

For the active V2 slice, run and record the validation gates from that slice's
`05-format-and-validate.md`. Do not claim a V2 slice is complete until the
focused tests, required format command, required vet command, and any required
broader test run pass.

Completed V2-A validation:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*(Boundary|Describe|Export|Contract|Lifecycle|Build)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`
- `rtk go test ./... -count=1`

Completed V2-B validation:
- `rtk go fmt ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`
- `rtk go test ./codegen ./contractdiff ./contractworkflow ./cmd/shunter -count=1`
- `rtk go test ./... -run 'Test.*(Contract|Codegen|Diff|Policy)' -count=1`
- `rtk go vet ./codegen ./contractdiff ./contractworkflow ./cmd/shunter`

Completed V1.5-E validation:
- `rtk go fmt <touched packages>`
- `rtk go test ./... -run 'Test.*Migration|Test.*ContractDiff|Test.*Policy' -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet <touched packages>`

Pinned Staticcheck is available as `rtk go tool staticcheck ./...`. Use it for
static-analysis visibility when relevant, but do not treat a broad green run as
required until OI-008 cleanup clears known findings and any dirty compile
blockers.

Do not claim a Go implementation slice is complete until the relevant Go
commands pass.

## Handoff Upkeep

For future hosted-runtime handoff updates:
- make the active target explicit
- keep startup reading minimal
- advance the active target only after the whole lettered slice, its tests, and
  its validation gates are complete
- record only future-relevant state, not closure archaeology
