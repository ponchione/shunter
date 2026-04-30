# Hosted Runtime Planning Handoff

Use this file as the primary cross-agent handoff only for hosted-runtime
planning or implementation work. Use `NEXT_SESSION_HANDOFF.md` for TECH-DEBT /
correctness work.

## Current Target

No hosted-runtime implementation slice is queued.

Hosted-runtime V1-H, V1.5-A through V1.5-E, V2-A through V2-G, and the V2.5
read authorization follow-on are complete. Do not reopen those slices unless a
new Shunter-visible regression proves drift or the user assigns a new target.

V2-G deliberately ended as an out-of-process execution gate, not a production
runner. The supported runtime model remains statically linked in-process Go
modules built with `shunter.Build(...)`.

## Startup Reading

Required:

1. `RTK.md`
2. this file
3. any explicitly assigned hosted-runtime slice, regression, or design document

Do not read broad roadmap, ledger, or decomposition docs by default.

If new V2 implementation starts from an explicit user target, begin with:

- `docs/features/V2/README.md`
- the selected slice `00-current-execution-plan.md`
- that slice's `01-stack-prerequisites.md`
- live code/package docs named by the slice

For V2-G audit only, start from:

- `docs/features/V2/V2-G/00-current-execution-plan.md`
- `docs/features/V2/V2-G/04-decision-record.md`
- `internal/processboundary/`
- `executor/`
- `runtime_lifecycle.go`
- `subscription/`

For V1.5-E audit only, start from:

- `docs/hosted-runtime-planning/V1.5/V1.5-E/00-current-execution-plan.md`
- `runtime_migration_test.go`
- `runtime_contract.go`
- `contractdiff/`

## Current Hosted-Runtime State

Live surface:

- root package imports as `github.com/ponchione/shunter`
- app authors define code-first modules through `Module`, `Config`, and
  `Build(...)`
- `Runtime` owns startup/close, local reducer calls, local reads, HTTP protocol
  serving, lifecycle handling, health/description, and contract export
- `Module.Query(...)` and `Module.View(...)` register named read/view
  declarations
- `Runtime.ExportContract` and `Runtime.ExportContractJSON` expose canonical
  module contracts for review, codegen, migration planning, and policy checks
- `codegen` generates deterministic TypeScript bindings from canonical
  contracts without starting a runtime
- `contractworkflow` and `cmd/shunter contract ...` operate on existing
  canonical JSON files; app-owned export still comes from
  `Runtime.ExportContractJSON`
- `Host` can bind already-built runtimes under explicit module names and route
  prefixes for multi-module hosting

Authorization and policy state:

- reducer permission metadata is exported and enforced for local/protocol
  external calls
- raw SQL external reads use table read policy
- named declared query/view execution enforces declaration permissions
- row-level visibility filters are validated at build, exported in contracts,
  and expanded into raw and declared external read plans
- anonymous/dev `AllowAllPermissions` remains the trusted bypass for dev paths

Migration state:

- migration metadata is descriptive and contract-level only
- migration planning, contract diffs, and policy checks are review/CI tools
- there is no executable migration runner, automatic stored-state rewrite,
  rollback system, or startup-blocking migration enforcement

Out-of-process state:

- `internal/processboundary` records the experimental reducer/lifecycle
  invocation contract and validation gate
- production process isolation remains deferred until there is a design for
  serializable reducer transaction mutation, scheduler access, lifecycle
  rollback/cleanup, and committed-state-driven subscription fanout across a
  process boundary
- do not add a runner, supervisor, dynamic module loader, cross-language SDK, or
  executor routing change without an explicit target

## Guardrails

- Do not invent V2-H or V2.5 follow-on slices without an explicit user target.
- Preserve WebSocket-first V1 runtime behavior unless a new target changes it.
- Keep canonical contract JSON stable and app-owned; generic tooling should
  consume existing contract files, not load modules dynamically.
- Keep SpacetimeDB reference usage scoped to design evidence. Shunter owns the
  Go API, protocol, runtime behavior, and operator contract.
- Do not push unless explicitly asked.

## Validation

For a hosted-runtime implementation slice:

1. inspect touched packages with Go tools first
2. add focused failing tests for the slice or regression
3. run `rtk go fmt` on touched packages
4. run targeted `rtk go test <touched packages> -count=1`
5. run `rtk go vet <touched packages>` when behavior, exported APIs, or
   interfaces changed
6. broaden to `rtk go test ./... -count=1` when the touched surface warrants it
7. run `rtk go tool staticcheck ./...` when static analysis is relevant

Pinned Staticcheck is expected to be green after OI-008 cleanup. Treat failures
as real cleanup findings unless a task explicitly narrows verification.

Do not claim a Go implementation slice is complete until the relevant Go
commands pass.

## Handoff Upkeep

Keep this file future-facing:

- make the active target explicit
- keep startup reading minimal
- link only the docs/code needed for the active slice
- record current contracts, guardrails, and validation expectations
- leave completed-slice proof, validation transcripts, and closure detail in
  tests and git history
