# Hosted Runtime Planning Handoff

Use this file as the primary cross-agent handoff for hosted-runtime planning or
implementation work.

Use `NEXT_SESSION_HANDOFF.md` instead for TECH-DEBT / correctness work.

## Current Target

The active hosted-runtime slice is V1.5-B canonical contract export.

Next agent run: complete V1.5-B end to end, from prerequisites through tests,
implementation, JSON output, validation, and handoff upkeep.

Start from:
- `docs/hosted-runtime-planning/V1.5/README.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-B/00-current-execution-plan.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-B/01-stack-prerequisites.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-B/02-contract-model-tests.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-B/03-contract-export-implementation.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-B/04-json-snapshot-output.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-B/05-format-and-validate.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-A/00-current-execution-plan.md`
- `module_declarations_test.go`

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

V1.5-B goal:
- export a full module contract artifact as deterministic canonical JSON
- combine module identity, schema/reducer export, and V1.5-A query/view
  declarations
- reserve but do not implement permissions/read-model and migration behavior

## Current Hosted-Runtime State

V1-H and V1.5-A are audited as landed.

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
- root/runtime package tests are the live proof for hosted-runtime ownership,
  serving, local calls, describe, export, and lifecycle behavior
- the prior bundled hello-world command was removed because it no longer served
  a maintained product or integration purpose

Do not reopen V1-A through V1-H or V1.5-A unless a new failing regression proves
drift.

## Startup Reading

Required:
1. `RTK.md`
2. this file
3. `docs/hosted-runtime-planning/V1.5/README.md`
4. `docs/hosted-runtime-planning/V1.5/V1.5-B/00-current-execution-plan.md`
5. `docs/hosted-runtime-planning/V1.5/V1.5-B/01-stack-prerequisites.md`
6. `docs/hosted-runtime-planning/V1.5/V1.5-B/02-contract-model-tests.md`
7. `docs/hosted-runtime-planning/V1.5/V1.5-B/03-contract-export-implementation.md`
8. `docs/hosted-runtime-planning/V1.5/V1.5-B/04-json-snapshot-output.md`
9. `docs/hosted-runtime-planning/V1.5/V1.5-B/05-format-and-validate.md`
10. `docs/hosted-runtime-planning/V1.5/V1.5-A/00-current-execution-plan.md` only
   if V1.5-A proof is needed

V1.5-B Task 01 should run and record these checks:
- `rtk go doc . Module`
- `rtk go doc . Module.Describe`
- `rtk go doc . Runtime`
- `rtk go doc . Runtime.ExportSchema`
- `rtk go doc ./schema SchemaExport`
- `rtk go doc ./schema ReducerExport`

Open these only when the active V1.5-B docs or live code leave a contract
question unresolved:
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`
- `docs/decomposition/hosted-runtime-version-phases.md`
- `docs/hosted-runtime-implementation-roadmap.md`
- `docs/decomposition/hosted-runtime-v1-contract.md`

Do not read broad roadmap, ledger, or decomposition docs by default.

## V1.5-B Scope

In scope:
- in-memory full module contract model
- deterministic canonical JSON contract snapshot output
- contract content for module identity/version/metadata
- schema/contract version, tables, reducers, queries, and views
- reserved permissions/read-model and migration fields
- codegen/export metadata enough for V1.5-C

Out of scope:
- client bindings or codegen
- executable permissions behavior
- migration diff tooling or executable migrations
- full SQL/view system
- query engine surface widening
- runtime shape changes
- multi-module hosting, out-of-process module execution, or control-plane work

Preserve WebSocket-first v1 runtime behavior.

## Next Slice Notes

Complete V1.5-B in one run. Work through its numbered tasks in order:
- reconfirm contract export prerequisites
- add failing in-memory contract model tests
- implement contract assembly from module, schema, reducers, queries, and views
- add deterministic canonical JSON snapshot output
- format, test, vet, and update handoffs

Prerequisite proof should verify V1.5-B can assemble its contract from live
surfaces:
- module description provides identity, metadata, queries, and views
- schema export provides schema version, tables, and reducers
- V1.5-B owns the first full module contract artifact
- permissions and migration fields should be reserved, not behaviorally
  implemented

## Coordination Notes

There may be concurrent gauntlet/dependency test work in the same worktree.
For V1.5-B, avoid touching these unless the user explicitly redirects the work:
- `docs/RUNTIME-HARDENING-GAUNTLET.md`
- `go.mod`
- `go.sum`
- `runtime_gauntlet_test.go`
- rapid/fuzz-style test files under `bsatn/`, `commitlog/`, `query/sql/`, and
  `store/`

Do not push unless explicitly asked.

## Validation

Expected V1.5-B validation:
- `rtk go fmt .`
- `rtk go test . -run 'Test.*Contract|Test.*Export.*JSON' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

Expand when contract code touches schema export or shared tooling:
- `rtk go test ./schema -count=1`
- `rtk go test ./... -count=1`

Pinned Staticcheck is available as `rtk go tool staticcheck ./...`. Use it for
static-analysis visibility when relevant, but do not treat a broad green run as
required until OI-008 cleanup clears known findings and any dirty compile
blockers.

Do not claim a Go implementation slice is complete until the relevant Go
commands pass.

## Handoff Upkeep

When V1.5-B completes:
- update task progress in `docs/hosted-runtime-planning/V1.5/V1.5-B/00-current-execution-plan.md`
- update this file to make V1.5-C client bindings/codegen the current target
- keep startup reading minimal
- record only future-relevant state, not closure archaeology
