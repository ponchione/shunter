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

Completed 2026-04-29.

Read catalog type/location:

- `declared_read_catalog.go`
- `declaredReadCatalog` stored on `Runtime.readCatalog`
- `declaredReadEntry` preserves declaration name, kind, SQL, permission
  metadata, read-model metadata, migration metadata, referenced table IDs,
  `:sender` usage, and prevalidated `protocol.CompiledSQLQuery` metadata for
  executable declarations.
- Metadata-only declarations are present in the catalog with no compiled plan.

Local/runtime API names:

- `Runtime.CallQuery(ctx, declarationName, opts...)`
- `Runtime.SubscribeView(ctx, declarationName, queryID, opts...)`
- Local named-read options added:
  `WithDeclaredReadIdentity`, `WithDeclaredReadConnectionID`,
  `WithDeclaredReadPermissions`, `WithDeclaredReadAllowAllPermissions`, and
  `WithDeclaredReadRequestID`.

Named-read authorization behavior:

- Named query/view APIs look up the explicit declaration name and kind; unknown
  names return `ErrUnknownDeclaredRead` and never fall back to raw SQL text.
- Declaration permissions are checked with `types.MissingRequiredPermission`
  before one-off execution or subscription registration.
- Missing declaration permission returns `ErrPermissionDenied`.
- `AllowAllPermissions` bypasses declaration permission checks.
- Metadata-only declarations return `ErrDeclaredReadNotExecutable`.
- Declared reads compile/execute against the runtime schema, not the
  caller-authorized raw-SQL lookup, so private base-table raw-read policy does
  not block module-owned declared reads.
- Raw SQL equivalent to a declaration still uses raw-SQL admission and does
  not inherit declaration permissions.

Protocol/runtime seam added for Task 05:

- `protocol.CompiledSQLQuery`
- `protocol.CompileSQLQueryString`
- `protocol.ExecuteCompiledSQLQuery`

Validation commands run:

```sh
rtk go fmt . ./protocol ./executor ./schema
rtk go test . -run 'Test.*(Declaration|Read|Query|View|Permission|Runtime)' -count=1
rtk go test ./protocol ./executor ./schema -count=1
rtk go vet . ./protocol ./executor ./schema
rtk go test ./commitlog -count=1
rtk go test ./... -count=1
```

Task 06 protocol/codegen follow-up:

- Add external protocol/server surfaces that receive declared read names for
  named one-off query and named view subscribe.
- Update generated clients so declaration helpers call named declared-read
  callbacks/API paths instead of raw SQL callbacks.
- Keep raw SQL helpers separate from declared-read helpers; do not infer
  declarations from matching SQL text.
