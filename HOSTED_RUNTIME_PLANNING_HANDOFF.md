# Hosted Runtime Planning Handoff

Use this file as the primary cross-agent handoff for hosted-runtime planning or
implementation work.

Use `NEXT_SESSION_HANDOFF.md` instead for TECH-DEBT / correctness work.

## Current Target

The active hosted-runtime slice is V1.5-C client bindings/codegen.

Next agent run: complete V1.5-C end to end, from prerequisites through tests,
implementation, generated output, validation, and handoff upkeep.

Start from:
- `docs/hosted-runtime-planning/V1.5/README.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-C/00-current-execution-plan.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-C/01-stack-prerequisites.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-C/02-generator-contract-tests.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-C/03-typescript-binding-generator.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-C/04-secondary-artifacts.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-C/05-format-and-validate.md`
- `runtime_contract.go`
- `runtime_contract_test.go`

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

V1.5-C goal:
- generate useful client bindings from the canonical contract
- make TypeScript the first client binding target
- consume contract JSON or `ModuleContract` data without requiring a live
  runtime process
- cover tables, reducers, queries, and views at the generator surface

## Current Hosted-Runtime State

V1-H, V1.5-A, and V1.5-B are audited as landed.

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
- root/runtime package tests are the live proof for hosted-runtime ownership,
  serving, local calls, describe, export, and lifecycle behavior
- the prior bundled hello-world command was removed because it no longer served
  a maintained product or integration purpose

Do not reopen V1-A through V1-H, V1.5-A, or V1.5-B unless a new failing
regression proves drift.

## Startup Reading

Required:
1. `RTK.md`
2. this file
3. `docs/hosted-runtime-planning/V1.5/README.md`
4. `docs/hosted-runtime-planning/V1.5/V1.5-C/00-current-execution-plan.md`
5. `docs/hosted-runtime-planning/V1.5/V1.5-C/01-stack-prerequisites.md`
6. `docs/hosted-runtime-planning/V1.5/V1.5-C/02-generator-contract-tests.md`
7. `docs/hosted-runtime-planning/V1.5/V1.5-C/03-typescript-binding-generator.md`
8. `docs/hosted-runtime-planning/V1.5/V1.5-C/04-secondary-artifacts.md`
9. `docs/hosted-runtime-planning/V1.5/V1.5-C/05-format-and-validate.md`
10. `docs/hosted-runtime-planning/V1.5/V1.5-B/00-current-execution-plan.md` only
   if V1.5-B proof is needed

V1.5-C Task 01 should run and record these checks:
- `rtk go doc . Runtime.ExportContract`
- `rtk go doc ./schema SchemaExport`

Open these only when the active V1.5-C docs or live code leave a contract or
generator question unresolved:
- `docs/decomposition/006-schema/SPEC-006-schema.md#12-client-code-generation-interface`
- `docs/decomposition/006-schema/epic-6-schema-export/story-6.3-codegen-tool-contract.md`
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`

Do not read broad roadmap, ledger, or decomposition docs by default.

## V1.5-C Scope

In scope:
- first useful client binding generator from the V1.5-B canonical contract
- TypeScript as the initial language target
- deterministic generated output
- generated surfaces for tables, reducers, queries, and views
- generator inputs from contract JSON or detached `ModuleContract` data
- clear errors for unsupported languages or unusable contract input

Out of scope:
- executable permissions behavior
- migration diff tooling or executable migrations
- full SQL/view system
- query engine surface widening
- runtime shape changes
- multi-module hosting, out-of-process module execution, or control-plane work
- all-language SDK generation
- server/module implementation generation

Preserve WebSocket-first v1 runtime behavior.

## Next Slice Notes

Complete V1.5-C in one run. Work through its numbered tasks in order:
- reconfirm codegen prerequisites
- add failing generator contract tests
- implement the first TypeScript binding generator from the canonical contract
- add secondary artifacts only where cheap and clearly separated
- format, test, vet, and update handoffs

Prerequisite proof should verify V1.5-C consumes the contract artifact rather
than hidden runtime state:
- codegen input is `ModuleContract` or canonical contract JSON
- TypeScript remains the first documented language target
- reducer calls may remain raw-byte until typed reducer argument metadata exists
- query/view bindings should reflect V1.5-A declarations
- the generator must not require a live runtime process

## Coordination Notes

There may be concurrent gauntlet/dependency test work in the same worktree.
For V1.5-C, avoid touching these unless the user explicitly redirects the work:
- `docs/RUNTIME-HARDENING-GAUNTLET.md`
- `go.mod`
- `go.sum`
- `runtime_gauntlet_test.go`
- rapid/fuzz-style test files under `bsatn/`, `commitlog/`, `query/sql/`, and
  `store/`

Do not push unless explicitly asked.

## Validation

Expected V1.5-C validation:
- `rtk go fmt <touched packages>`
- `rtk go test ./... -run 'Test.*Codegen|Test.*Generator|Test.*TypeScript' -count=1`
- `rtk go test ./... -count=1`
- `rtk go vet <touched packages>`

If V1.5-C creates a command package, include that package explicitly in format
and vet commands.

Pinned Staticcheck is available as `rtk go tool staticcheck ./...`. Use it for
static-analysis visibility when relevant, but do not treat a broad green run as
required until OI-008 cleanup clears known findings and any dirty compile
blockers.

Do not claim a Go implementation slice is complete until the relevant Go
commands pass.

## Handoff Upkeep

When V1.5-C completes:
- update task progress in `docs/hosted-runtime-planning/V1.5/V1.5-C/00-current-execution-plan.md`
- update this file to make V1.5-D permissions/read-model metadata the current
  target
- keep startup reading minimal
- record only future-relevant state, not closure archaeology
