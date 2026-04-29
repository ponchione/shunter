# Hosted Runtime V2 Read Authorization Design

Status: implementation design authority
Created: 2026-04-29
Scope: external read authorization for raw SQL, declared queries, declared
views, generated clients, and row-level visibility.

This document records the target design for bringing Shunter's read
authorization model up to the same class of capability as the system that
inspired it, without copying implementation. It replaces the V2-E "read
permission enforcement is deferred" note as the implementation-facing authority
for read-auth implementation work.

Execution decomposition lives in `docs/features/V2.5/`. Workers should use
that directory for task sequencing and use this document for design authority.

## Context

V2-E completed reducer permission enforcement. Reducer calls now carry caller
permission tags through local and protocol paths, and the executor rejects
missing reducer permissions before user reducer code or transaction creation.

Read permissions are different because Shunter currently has two read surfaces:

- authored declarations: `QueryDeclaration` and `ViewDeclaration`
- raw SQL protocol paths: one-off queries and subscriptions

V2-D made declaration SQL executable through generated helpers, but those
helpers still call raw SQL protocol paths. That means a declaration permission
check alone cannot be complete. A client can bypass a named declaration by
sending equivalent raw SQL unless raw SQL itself is governed by a table/read
policy.

The design below treats that as the central constraint.

## Reference Insight

The reference system does not solve this by checking permissions on named query
helpers. Its relevant behavior is:

- tables have visibility; private tables are not readable by normal clients
- SQL table resolution is auth-aware
- one-off queries and subscriptions compile through the same auth-aware table
  resolution path
- a private table is rejected even if it only appears inside a join and is not
  projected, because joins can leak information
- row-level visibility filters are attached to tables and are expanded into the
  query/read path, not checked after arbitrary rows have already been exposed
- module-owned views are their own exported surfaces and can intentionally
  expose computed data from internal tables

Shunter should adopt the behavior class, not the source design or code.

## Design Goals

1. Raw SQL must never be able to read a table the caller is not allowed to
   read.
2. Unauthorized tables must look unresolved to raw SQL callers. Do not leak
   whether the table exists.
3. Named declared reads must be real runtime surfaces, not just codegen sugar.
4. Generated clients must call named declared read surfaces for declarations,
   not raw SQL strings.
5. Table-level policy must apply before subscription registration and before
   one-off execution.
6. Row-level visibility must be enforced as part of query evaluation for every
   external read path, including joins.
7. Reducer/internal module reads remain unrestricted by external read policy.
8. The implementation must keep protocol error-shape compatibility where
   existing tests pin it.

## Non-Goals

This design does not require:

- copying the reference implementation
- a broad policy language detached from Shunter's existing permission tags,
  SQL compiler, predicates, and schema registry
- dynamic module loading
- out-of-process execution
- changing reducer permission semantics
- making raw SQL privileged just because a matching declaration exists

## Core Model

Shunter should have three different read concepts:

1. Table read policy

   This governs raw external SQL access to base tables.

2. Declared read surface policy

   This governs named query/view declarations exported by the module.

3. Row-level visibility policy

   This governs which rows from a table are visible to an external caller when
   that table participates in a read.

These concepts must not be collapsed into one field.

## Table Read Policy

Add table read policy to schema metadata.

Recommended public types:

```go
type TableAccess int

const (
    TableAccessPrivate TableAccess = iota
    TableAccessPublic
    TableAccessPermissioned
)

type ReadPolicy struct {
    Access      TableAccess
    Permissions []string
}
```

Recommended builder options:

```go
schema.WithPrivateRead()
schema.WithPublicRead()
schema.WithReadPermissions("messages:read")
```

Recommended semantics:

- default table access is private
- `WithPublicRead` allows any external caller to read the table through raw SQL
- `WithReadPermissions(...)` allows raw SQL only when the caller has all
  required permission tags
- `WithPrivateRead` denies raw SQL unless the caller has bypass privileges
- reducer code and lifecycle reducer code are internal and do not go through
  external table read policy
- table access metadata is exported through schema/contract/codegen artifacts

Default-private is a deliberate security default. Tests and examples that
expect raw SQL access must mark tables public or permissioned.

## Caller Read Context

Read authorization should use the same caller facts already used by reducer
authorization:

- identity
- connection ID when available
- permission tags
- `AllowAllPermissions`

Do not duplicate permission semantics. Extract the existing executor permission
matching into a small shared helper package or shared internal function so
protocol and executor use the same rule:

- if `AllowAllPermissions` is true, authorization succeeds
- otherwise every non-empty required permission must be present in the caller's
  granted permission set

`AllowAllPermissions` is Shunter's current admin/dev bypass. For read
authorization it should bypass:

- private table access
- permissioned table checks
- row-level visibility filters
- declared read permission checks

Strict auth mode should not set `AllowAllPermissions` unless explicitly
configured by the caller path.

## Auth-Aware Schema Lookup

Raw SQL compilation already centralizes table resolution in `protocol` through
`SchemaLookup` and `compileSQLQueryString`. This is the right enforcement point.

Add an auth-aware wrapper around `SchemaLookup`:

```go
type ReadAuthorizer interface {
    CanReadTable(caller ReadCaller, table schema.TableID, tableName string) bool
}

func NewAuthorizedSchemaLookup(base protocol.SchemaLookup, auth ReadAuthorizer, caller ReadCaller) protocol.SchemaLookup
```

The wrapper must:

- return `false` from `TableByName` for unauthorized tables
- return `false` from `Table` for unauthorized tables when used for raw SQL
  compilation
- preserve existing case-sensitivity and ambiguity behavior
- preserve the existing diagnostic text produced by the compiler:

```text
no such table: `{name}`. If the table exists, it may be marked private.
```

Do not put this check after SQL compilation. A denied table must be invisible
during compilation so joins cannot leak existence or shape.

## Raw One-Off Query Flow

Target flow:

1. Build a read caller from `Conn`.
2. Wrap the runtime schema lookup with the auth-aware lookup.
3. Compile `OneOffQueryMsg.QueryString` against that wrapped lookup.
4. Validate the compiled predicate against the same wrapped lookup, or against
   a validation surface that has already been checked for all referenced
   tables.
5. Apply row-level visibility expansion before execution.
6. Execute against committed state.

Important details:

- no table access check should be based only on the projected table
- every table referenced by the query must be authorized
- unauthorized tables in `JOIN`, `WHERE`, projection, aggregate, or new SQL
  forms must fail at admission
- error shape should remain the existing one-off error response with the
  compiler message as text

## Raw Subscription Flow

Target flow:

1. Build a read caller from `Conn`.
2. Wrap the runtime schema lookup with the auth-aware lookup.
3. Compile every SQL string in `SubscribeSingle` / `SubscribeMulti` against
   the wrapped lookup.
4. Apply row-level visibility expansion before registration.
5. Register only already-authorized predicates/plans.

Important details:

- `SubscribeMulti` must remain atomic: if any query is unauthorized, none of
  the set is registered
- unauthorized tables must use the existing subscription compile-error shape:

```text
{inner error}, executing: `{sql}`
```

- subscription manager should not become the first line of read authorization;
  it should receive already-authorized read plans
- post-commit fanout must evaluate the same row-level visibility that initial
  subscription delivery used

## Declared Reads

`QueryDeclaration` and `ViewDeclaration` must become runtime-owned read
surfaces.

At `Build`, create a read catalog from authored declarations. Each catalog
entry should contain:

- declaration name
- declaration kind: one-off query or live view
- original SQL
- required declaration permissions
- compiled read plan or compiled SQL metadata
- referenced table set
- whether the plan uses caller identity, such as `:sender`
- migration/read-model metadata already present on declarations

The runtime must expose named execution paths for this catalog. Generated
clients must use those named paths.

Generated client helpers must stop doing this for declarations:

```ts
return runQuery("SELECT * FROM messages");
return subscribeView("SELECT * FROM messages");
```

They should instead do the equivalent of:

```ts
return runDeclaredQuery("recent_messages");
return subscribeDeclaredView("live_messages");
```

The exact TypeScript callback names can follow existing codegen style, but the
contract is strict: generated declaration helpers must not execute by sending
arbitrary raw SQL.

### Declared Read Authorization

Declared reads have their own permission checks.

For a named declared query/view:

1. Look up the declaration by name.
2. If missing, report the named read as unknown.
3. Check the declaration's `PermissionMetadata.Required` against the caller.
4. Execute the declaration's authored SQL.

Base table access policy does not apply as if the caller had issued raw SQL,
because declared reads are module-owned exported surfaces. This allows a module
to keep base tables private while exposing a safe query/view over them.

Row-level visibility policy should apply to declared reads by default because
declared reads are still external data exposure. Trusted declarations that
intentionally bypass row-level filters need an explicit authored metadata flag
and dedicated tests. Do not add an implicit bypass.

### Declared Read Protocol Surface

Implement real protocol/runtime entry points for named reads. Do not simulate
declared reads by matching raw SQL text.

Acceptable implementation shapes include:

- new Shunter protocol message tags for declared one-off query and declared
  subscribe
- an internal runtime API used by generated client adapters that routes by
  declaration name
- a compatibility layer that supports existing raw SQL callbacks while generated
  declaration helpers switch to named callbacks

Whichever shape is chosen, the server must receive the declaration name.

Exact-SQL matching is not sufficient. It is fragile, can be bypassed when base
tables are public, and makes declaration permissions depend on client honesty.

## Row-Level Visibility

Row-level visibility is a table policy that limits which rows are visible to an
external caller.

Recommended authored API:

```go
type VisibilityFilterDeclaration struct {
    Name string
    SQL  string
}

func (m *Module) VisibilityFilter(decl VisibilityFilterDeclaration) *Module
```

The SQL must describe rows visible from one return table. For example:

```sql
SELECT * FROM messages WHERE owner = :sender
```

Multiple filters for the same table are ORed together. A row is visible if any
filter for that table admits it. If a table has no visibility filter, row-level
visibility does not restrict it.

Build-time validation must reject invalid filters:

- blank name
- duplicate name
- invalid SQL
- SQL that does not return exactly one table shape
- SQL that cannot be tied to a known return table
- SQL whose return table is unknown
- SQL forms that the read planner cannot enforce correctly; unsupported forms
  must be rejected rather than accepted without enforcement

### Visibility Must Apply Before Data Leaks

Do not implement row-level visibility as a final post-filter over projected
rows only. That is not sufficient.

Example:

```sql
SELECT public_table.*
FROM public_table
JOIN private_or_filtered_table
  ON public_table.owner = private_or_filtered_table.owner
```

Even if the result only contains `public_table` rows, the join can reveal
whether matching rows exist in the private/filtered table. Visibility must
restrict every relation before the join participates in query evaluation.

### Read Plan Refactor

The current `subscription.Predicate` tree is good for many existing queries,
but full row-level visibility needs an explicit read plan layer.

Recommended direction:

1. Parse SQL into an internal relation-aware read plan.
2. Resolve each relation through auth-aware schema lookup.
3. Attach a relation alias to every table occurrence.
4. Expand table visibility filters into the relation source for each table
   occurrence.
5. Lower the authorized/expanded plan into existing subscription predicates
   where possible.
6. Extend subscription evaluation only where the existing predicate model cannot
   express the authorized plan.

This avoids bolting visibility onto the side of a predicate after the compiler
has lost important relation/alias context.

Minimum relation facts the read plan must preserve:

- table ID
- source table name
- table alias
- whether the relation is the projected/returned table
- join edges
- filter predicates
- whether the relation uses caller identity
- table IDs read by nested visibility filters

### Visibility And Self-Joins

Self-joins need alias-aware visibility. If a query reads the same table twice,
the visibility filter must be applied independently to both relation aliases.

The existing predicate alias tags are useful, but they are not enough by
themselves for complex visibility filters. Do not assume table ID alone is
enough to identify a relation occurrence.

## Permission And Policy Interaction

Raw SQL:

- table read policy is mandatory
- row-level visibility is mandatory
- declaration permissions are irrelevant because no declaration was invoked

Declared query/view:

- declaration permissions are mandatory
- row-level visibility is mandatory by default
- base table raw-read policy does not block the declaration

Reducer/internal code:

- external read policy does not apply
- reducer permission enforcement remains exactly as implemented for external
  reducer calls

Admin/dev bypass:

- `AllowAllPermissions` bypasses table read policy, declared read permissions,
  and row-level visibility

## Error Semantics

Raw SQL unauthorized table:

- same as unresolved/private table
- one-off error body should carry the existing compiler text
- subscription error should use existing SQL-wrapped compile error text

Declared read missing permission:

- use a permission-denied error, not an unknown table error
- the caller asked for a named exported surface, so hiding table existence is not
  relevant at that layer

Declared read missing name:

- use an unknown-declared-read error
- do not fall back to raw SQL

Row-level visibility:

- denied rows are omitted
- this is not an error
- invalid filter definitions are build-time errors

## Contract And Codegen

Contracts should include enough metadata for generated clients and external
review tools:

- table read access mode
- table read permission tags
- declared query/view permission tags
- declared query/view SQL metadata
- visibility filter declarations or a safe summary of them

Generated clients should:

- expose raw SQL helpers only as raw SQL helpers
- expose declared query/view helpers through named declared-read callbacks
- continue exporting declaration SQL as metadata if useful for debugging,
  migration review, or docs
- not use declaration SQL metadata as the execution authority

Contract diff should classify:

- table access changes
- table read permission changes
- declaration permission changes
- declaration SQL changes
- visibility filter add/remove/change

Policy/migration planning should treat these as compatibility-sensitive because
they can add or remove client-visible data.

## Testing Requirements

Do not mark this feature complete without tests for these cases.

Table access:

- default table is private
- raw one-off query against private table is rejected
- raw subscription against private table is rejected
- public table is readable by raw one-off and raw subscription
- permissioned table rejects missing tags
- permissioned table accepts matching tags
- `AllowAllPermissions` bypasses private and permissioned table checks
- private table inside a join is rejected even when not projected
- `SubscribeMulti` with one unauthorized query registers none of the queries

Declared reads:

- declared query over private base table succeeds when declaration permission is
  satisfied
- declared view over private base table subscribes when declaration permission
  is satisfied
- declared query/view rejects missing declaration permission
- generated declaration helper uses named read callback, not raw SQL callback
- raw SQL equivalent to a declaration does not inherit declaration permissions
- declaration SQL is prevalidated at build
- missing declared read name does not fall back to raw SQL

Row-level visibility:

- one-off query returns only caller-visible rows
- subscription initial state returns only caller-visible rows
- subscription deltas include only caller-visible inserted/deleted rows
- two clients subscribed to the same table receive different rows when the
  filter uses `:sender`
- multiple filters for one table OR together
- self-join applies visibility per alias
- join with filtered non-projected table does not leak rows through join
  participation
- admin/dev bypass sees unfiltered rows
- invalid visibility filter SQL fails build

Regression/error shape:

- existing unknown-table one-off error text is preserved
- existing subscription SQL-wrapped error text is preserved
- projection/aggregate/join validation ordering remains pinned
- reducer permission tests remain unchanged

Recommended validation gates:

```sh
rtk go fmt ./...
rtk go test ./protocol ./schema ./subscription ./executor ./codegen -count=1
rtk go test . -run 'Test.*(Permission|Auth|Read|Query|View|Subscribe|Contract|Codegen)' -count=1
rtk go vet ./protocol ./schema ./subscription ./executor ./codegen .
rtk go test ./... -count=1
rtk go tool staticcheck ./...
```

Run narrower package tests first while developing. Expand to full tests before
calling the feature complete.

## Suggested Implementation Campaign

This can be split among workers, but the overall campaign is not complete until
all items below are implemented and tested.

1. Schema table read policy

   Add table access metadata, builder options, schema export, contract export,
   validation, and contract diff classification.

2. Shared permission checker

   Move the "required permissions are all present unless AllowAllPermissions"
   rule into a reusable package/function used by executor and protocol.

3. Auth-aware raw SQL admission

   Add authorized schema lookup and wire it into one-off and subscription SQL
   compilation. Preserve existing error text.

4. Runtime read catalog

   Build a declaration catalog at runtime build time. Store compiled declaration
   metadata, referenced tables, permissions, and SQL.

5. Named declared read execution

   Add local/protocol/server paths that receive declaration names, check
   declaration permissions, and execute the catalog entry. Do not use raw SQL
   text matching.

6. Codegen update

   Generated declaration helpers must call named declared-read callbacks.
   Keep raw SQL helpers separate.

7. Visibility filter declarations

   Add authored filter metadata, build validation, contract export, and
   contract diff classification.

8. Read plan and visibility expansion

   Add or refactor the SQL compiler output so table visibility filters can be
   applied per relation before joins/evaluation. Lower to existing predicates
   where safe and extend evaluation where necessary.

9. End-to-end gauntlet coverage

   Add runtime/protocol tests proving raw reads, declared reads, subscriptions,
   generated clients, and row-level visibility compose correctly.

## Implementation Warnings

- Do not authorize only the projected table. Every table referenced by the SQL
  matters.
- Do not authorize after compiling against the full schema. Unauthorized tables
  must be invisible during compilation.
- Do not post-filter only projected rows for row-level visibility.
- Do not use exact SQL text matching to infer that a raw read is a declared
  read.
- Do not let generated declaration helpers continue using raw SQL execution
  callbacks.
- Do not make subscription manager responsible for discovering unauthorized
  SQL. Admission should reject before registration.
- Do not silently make strict-auth callers admin-equivalent.
- Do not weaken existing reducer permission behavior.

## Completion Definition

Read authorization is complete when:

- raw SQL cannot read unauthorized tables
- raw SQL cannot leak through joins with unauthorized or filtered tables
- declarations are invokable by name and enforce declaration permissions
- generated clients use named declaration execution for declared reads
- row-level visibility applies consistently to one-off reads and subscriptions
- contracts and diffs expose policy changes
- all required tests above pass
