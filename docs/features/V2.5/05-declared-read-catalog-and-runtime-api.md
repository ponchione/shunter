# V2.5 Task 05: Declared Read Catalog And Runtime API

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Depends on:
- Task 02 schema table read policy
- Task 03 shared permission checker
- Task 04 auth-aware raw SQL admission

Objective: make `QueryDeclaration` and `ViewDeclaration` runtime-owned named
read surfaces with permission checks, instead of metadata that generated
clients execute through raw SQL.

## Required Context

Read:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- `docs/features/V2/V2-D/00-current-execution-plan.md`
- `docs/features/V2.5/04-auth-aware-raw-sql-admission.md`

Inspect:

```sh
rtk go doc . QueryDeclaration
rtk go doc . ViewDeclaration
rtk go doc . Runtime
rtk go doc . Runtime.ExportContract
rtk go doc . LocalReadView
rtk rg -n "moduleSnapshot|moduleDescription|QueryDescription|ViewDescription|queryDeclarations|viewDeclarations|ExportContract|Build\\(" .
rtk rg -n "handleOneOffQuery|compileSQLQueryString|ValidateSQLQueryString" protocol
```

## Target Behavior

At build time, create a runtime-owned read catalog from authored query/view
declarations.

Each catalog entry should store:

- declaration name
- declaration kind: query or view
- original SQL
- required declaration permissions
- read-model and migration metadata already present on the declaration
- compiled SQL metadata or a stable prevalidated plan
- referenced table set, if available from the compiler
- whether the SQL uses caller identity, such as `:sender`

Named declared reads must be executed by name. Do not infer declarations from
raw SQL text.

Add local/runtime APIs for named declared reads. Exact API names can follow
local style, but they must make the declaration name explicit. For example:

```go
rt.CallQuery(ctx, "recent_messages", opts...)
rt.SubscribeView(ctx, "live_messages", opts...)
```

If local subscribe APIs do not exist yet, expose the narrowest runtime/protocol
entry points needed for task 06 to wire generated clients and protocol support.

## Authorization Rules

For named declared reads:

- missing declaration name returns an unknown-declared-read error
- missing declaration permission returns permission denied
- `AllowAllPermissions` bypasses declaration permission checks
- base table raw-read policy does not block the declaration
- row-level visibility is applied by tasks 07-08 and remains mandatory by
  default for declared reads

This allows a module to keep base tables private while exposing safe named
queries/views over them.

## Build-Time Rules

Existing V2-D SQL validation remains required:

- query SQL uses one-off query grammar
- view SQL uses subscription grammar
- metadata-only declarations remain allowed only if existing V2-D behavior still
  requires them

If a named execution API requires executable SQL, it must return a clear error
for metadata-only declarations. Do not fall back to raw SQL.

## Tests To Add First

Add focused failing tests for:

- build creates catalog entries for executable query/view declarations
- catalog entries are defensively copied and cannot be mutated through module
  descriptions/contracts
- named query over private base table succeeds when declaration permission is
  satisfied
- named view over private base table can be admitted when declaration
  permission is satisfied
- missing declaration permission rejects before execution/registration
- `AllowAllPermissions` bypasses declaration permission checks
- unknown declared read name does not fall back to raw SQL
- metadata-only declaration cannot be executed by named runtime API
- raw SQL equivalent to a declaration does not inherit declaration permission

Prefer root runtime tests for local APIs and narrow protocol tests only if this
task wires an internal protocol seam.

## Validation

Run at least:

```sh
rtk go fmt . ./protocol ./executor ./schema
rtk go test . -run 'Test.*(Declaration|Read|Query|View|Permission|Runtime)' -count=1
rtk go test ./protocol ./executor ./schema -count=1
rtk go vet . ./protocol ./executor ./schema
```

Run `rtk go test ./... -count=1` if runtime APIs or exported contracts change.

## Completion Notes

When complete, update this file with:

- read catalog type/location
- local/runtime API names
- named-read authorization behavior
- validation commands run
- any protocol/codegen follow-up required by task 06
