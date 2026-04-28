# Hosted Runtime Planning Handoff

Use this file as the primary cross-agent handoff for hosted-runtime planning or
implementation work.

Use `NEXT_SESSION_HANDOFF.md` instead for TECH-DEBT / correctness work.

## Current Target

The active hosted-runtime slice is V1.5-A query/view declarations.

Next task: V1.5-A Task 02, add failing module-owned query/view declaration
metadata tests.

Start from:
- `docs/hosted-runtime-planning/V1.5/README.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-A/00-current-execution-plan.md`
- `docs/hosted-runtime-planning/V1.5/V1.5-A/02-declaration-metadata-tests.md`

Task 01 is complete. Its live-code conclusions are recorded in:
- `docs/hosted-runtime-planning/V1.5/V1.5-A/01-stack-prerequisites.md`

V1.5-A goal:
- add the smallest code-first query/view declaration surface to the module model
- make declarations inspectable/exportable enough for V1.5-B
- keep the existing v1 hosted-runtime shape intact

Remaining V1.5-A task order:
1. Add failing tests for module-owned query/view declaration metadata.
2. Implement declaration types and module registration methods.
3. Expose declaration metadata through narrow descriptions.
4. Format and validate the slice.

After V1.5-A lands, update this handoff to point at V1.5-B canonical contract
export.

## Current Hosted-Runtime State

V1-H is audited as landed.

Live proof points:
- root package imports as `github.com/ponchione/shunter`
- `Module`, `Config`, `Runtime`, and `Build(...)` exist
- `Runtime.Start`, `Close`, `HTTPHandler`, `ListenAndServe`, local calls,
  describe, and schema export exist
- `Module` registration remains fluent and code-first
- `Module.Describe` currently exposes detached identity metadata only:
  `Name`, `Version`, and `Metadata`
- `Runtime.Describe` currently exposes module identity plus runtime health
- `Runtime.ExportSchema` currently exposes lower-level schema/reducer metadata:
  `Version`, `Tables`, and `Reducers`
- root/runtime package tests are the live proof for hosted-runtime ownership,
  serving, local calls, describe, export, and lifecycle behavior
- the prior bundled hello-world command was removed because it no longer served
  a maintained product or integration purpose

Do not reopen V1-A through V1-H unless a new failing regression proves drift.

## Startup Reading

Required:
1. `RTK.md`
2. this file
3. `docs/hosted-runtime-planning/V1.5/README.md`
4. `docs/hosted-runtime-planning/V1.5/V1.5-A/00-current-execution-plan.md`
5. `docs/hosted-runtime-planning/V1.5/V1.5-A/02-declaration-metadata-tests.md`
6. `docs/hosted-runtime-planning/V1.5/V1.5-A/01-stack-prerequisites.md` only
   if the recorded Task 01 proof is needed

Task 01 already ran these checks. Rerun them before editing Go code if the
current working tree has moved or if the next task needs fresh API confirmation:
- `rtk go doc . Module`
- `rtk go doc . Module.Describe`
- `rtk go doc . Runtime`
- `rtk go doc . Runtime.Describe`
- `rtk go doc . Runtime.ExportSchema`
- `rtk go doc ./schema SchemaExport`

Open these only when the active V1.5-A docs or live code leave a contract
question unresolved:
- `docs/decomposition/hosted-runtime-v1.5-follow-ons.md`
- `docs/decomposition/hosted-runtime-version-phases.md`
- `docs/hosted-runtime-implementation-roadmap.md`
- `docs/decomposition/hosted-runtime-v1-contract.md`

Do not read broad roadmap, ledger, or decomposition docs by default.

## V1.5-A Scope

In scope:
- named read query declarations
- named live view/subscription declarations
- fluent module registration methods
- detached declaration metadata
- narrow `Describe` or equivalent exposure for declaration metadata
- tests proving declarations are module-owned and do not disturb existing v1
  module/build behavior

Out of scope:
- canonical `shunter.contract.json`
- client bindings or codegen
- permissions/read-model metadata
- migration metadata or contract diff tooling
- full SQL/view system
- query engine surface widening
- runtime shape changes
- multi-module hosting, out-of-process module execution, or control-plane work

Preserve WebSocket-first v1 runtime behavior.

## Next Task Notes

Task 02 should add tests before implementation.

Likely files:
- create `module_declarations_test.go`
- extend `runtime_describe_test.go` only if the selected narrow description API
  requires runtime-level assertions

Pin behavior for:
- registering a named read query declaration
- registering a named live view/subscription declaration
- rejecting or surfacing empty declaration names
- rejecting or surfacing duplicate query names
- rejecting or surfacing duplicate view names
- treating query and view names as one shared namespace unless the implementation
  explicitly documents separate namespaces
- returning detached declaration descriptions
- keeping declarations visible before and after `Build`
- preserving existing table/reducer registration behavior

Do not implement the declaration API in Task 02 unless explicitly asked. The
focused declaration tests are expected to fail until Tasks 03 and 04 implement
the code.

## Coordination Notes

There may be concurrent gauntlet/dependency test work in the same worktree.
For V1.5-A, avoid touching these unless the user explicitly redirects the work:
- `docs/RUNTIME-HARDENING-GAUNTLET.md`
- `go.mod`
- `go.sum`
- `runtime_gauntlet_test.go`
- rapid/fuzz-style test files under `bsatn/`, `commitlog/`, `query/sql/`, and
  `store/`

Do not push unless explicitly asked.

## Validation

For Task 02, run the focused declaration test command and record the failing
result:
- `rtk go test . -run 'Test(Module.*Declaration|Runtime.*Declaration|.*Describe.*Declaration)' -count=1`

Expected full V1.5-A validation after implementation:
- `rtk go fmt .`
- `rtk go test . -run 'Test(Module.*Declaration|Runtime.*Declaration|.*Describe.*Declaration)' -count=1`
- `rtk go test . -count=1`
- `rtk go vet .`

Expand when root/runtime behavior changed beyond declaration metadata:
- `rtk go test ./... -count=1`

Pinned Staticcheck is available as `rtk go tool staticcheck ./...`. Use it for
static-analysis visibility when relevant, but do not treat a broad green run as
required until OI-008 cleanup clears known findings and any dirty compile
blockers.

Do not claim a Go implementation slice is complete until the relevant Go
commands pass.

## Handoff Upkeep

When V1.5-A completes:
- update task progress in `docs/hosted-runtime-planning/V1.5/V1.5-A/00-current-execution-plan.md`
- update this file to make V1.5-B the current target
- keep startup reading minimal
- record only future-relevant state, not closure archaeology
