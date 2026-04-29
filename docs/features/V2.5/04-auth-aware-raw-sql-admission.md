# V2.5 Task 04: Auth-Aware Raw SQL Admission

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Depends on:
- Task 02 schema table read policy
- Task 03 shared permission checker

Objective: enforce table read policy for raw one-off SQL and raw subscription
SQL before any read executes or subscription registers.

This is the first task that delivers real external read protection.

## Required Context

Read:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- `docs/features/V2.5/02-schema-table-read-policy.md`
- `docs/features/V2.5/03-shared-permission-checker.md`

Inspect:

```sh
rtk go doc ./protocol.ValidateSQLQueryString
rtk go doc ./protocol.SchemaLookup
rtk go doc ./protocol.Conn
rtk go doc ./protocol.OneOffQueryMsg
rtk go doc ./protocol.SubscribeSingleMsg
rtk go doc ./protocol.SubscribeMultiMsg
rtk rg -n "compileSQLQueryString|lookupSQLTableExact|handleOneOffQuery|handleSubscribeSingle|handleSubscribeMulti|ValidateQueryPredicate" protocol
rtk rg -n "TableByName|Table\\(|TableExists|HasIndex|ColumnType|ColumnExists" schema protocol subscription
```

## Target Behavior

Raw SQL admission must resolve tables through a caller-aware schema lookup.

For raw one-off SQL:

1. Build caller context from `Conn.Identity`, `Conn.Permissions`, and
   `Conn.AllowAllPermissions`.
2. Wrap the base `SchemaLookup` with an authorized lookup.
3. Compile SQL against the authorized lookup.
4. Validate the compiled predicate after table authorization has succeeded.
5. Execute only if all referenced tables were authorized.

For raw subscriptions:

1. Build caller context from `Conn`.
2. Wrap the base `SchemaLookup` with an authorized lookup.
3. Compile each query against the authorized lookup.
4. Register only authorized predicates.
5. Preserve `SubscribeMulti` atomicity: one unauthorized query registers none
   of the set.

Unauthorized tables must look unresolved. Preserve existing error text:

```text
no such table: `{name}`. If the table exists, it may be marked private.
```

For subscription compile errors, preserve the existing SQL wrapper:

```text
{inner error}, executing: `{sql}`
```

## Important Rules

- Do not authorize only the projected table.
- Do not compile against the full schema and reject afterward.
- Do not let subscription manager discover unauthorized tables.
- Do not add row-level visibility here. That belongs to tasks 07-08.
- Do not use declaration metadata here. Raw SQL does not inherit declaration
  permissions.
- `AllowAllPermissions` bypasses table access checks.

## Tests To Add First

Add focused failing tests for:

- raw one-off query against default-private table is rejected
- raw subscription against default-private table is rejected
- raw one-off query against public table succeeds
- raw subscription against public table succeeds
- permissioned table rejects caller without required tag
- permissioned table accepts caller with required tag
- `AllowAllPermissions` can read private and permissioned tables
- private table in a join is rejected when projected
- private table in a join is rejected when not projected
- private table in a `WHERE` or join predicate cannot leak existence/shape
- `SubscribeMulti` with one unauthorized query registers none of the queries
- existing unknown-table one-off error text remains unchanged
- existing subscription SQL-wrapped error text remains unchanged

Use both protocol-level handler tests and runtime/protocol integration tests
where necessary. Keep row-level visibility tests out of this task.

## Validation

Run at least:

```sh
rtk go fmt ./protocol ./schema ./subscription ./executor .
rtk go test ./protocol -count=1
rtk go test ./subscription -count=1
rtk go test . -run 'Test.*(Read|Query|Subscribe|Permission|Auth)' -count=1
rtk go vet ./protocol ./schema ./subscription ./executor .
```

Run `rtk go test ./... -count=1` before marking the task complete because raw
SQL admission touches runtime-facing behavior.

## Completion Notes

When complete, update this file with:

- authorized lookup implementation location
- exact raw one-off and subscription behavior
- validation commands run
- any remaining declared-read or row-level work that tasks 05-08 must handle

Completed 2026-04-29.

Authorized lookup implementation location:

- `protocol/read_authorization.go`
- `protocol.NewAuthorizedSchemaLookup`
- raw handler wiring in `protocol/handle_oneoff.go`,
  `protocol/handle_subscribe_single.go`, and
  `protocol/handle_subscribe_multi.go`

Raw one-off behavior:

- `handleOneOffQuery` builds a `types.CallerContext` from `Conn.Identity`,
  `Conn.ID`, `Conn.Permissions`, and `Conn.AllowAllPermissions`.
- One-off SQL now compiles against the authorized lookup, so private or
  permission-missing tables are invisible during table resolution.
- The compiled predicate is validated against the same authorized lookup before
  snapshot execution.
- Unauthorized tables emit the existing one-off compiler text:

  ```text
  no such table: `{name}`. If the table exists, it may be marked private.
  ```

- `AllowAllPermissions` bypasses private and permissioned table access checks.

Raw subscription behavior:

- `SubscribeSingle` and every `SubscribeMulti` SQL string compile against the
  caller-authorized lookup before registration.
- `SubscribeMulti` remains atomic: the first unauthorized query returns a
  `SubscriptionError`, and no predicates are submitted to the executor.
- Subscription compile errors preserve the existing SQL wrapper:

  ```text
  {inner error}, executing: `{sql}`
  ```

- Authorization happens through table resolution, so JOIN sides, projected
  tables, non-projected tables, WHERE predicates, and join predicates are
  covered before registration.

Tests added:

- `protocol/auth_read_admission_test.go` covers default-private rejection,
  public success, permissioned access, `AllowAllPermissions`, private JOIN
  tables projected and non-projected, shape-hiding for private join predicate
  references, `SubscribeMulti` atomicity, and existing error text.
- Existing broad protocol handler fixtures use an explicit allow-all test
  connection so SQL compiler/error-shape tests remain about SQL semantics
  rather than table policy.
- The runtime gauntlet `players` table is explicitly public because that
  strict-auth workload uses raw SQL without read tags.

Validation commands run:

```sh
rtk go fmt ./protocol ./schema ./subscription ./executor .
rtk go test ./protocol -run AuthReadAdmission -count=1
rtk go test ./protocol -count=1
rtk go test ./subscription -count=1
rtk go test . -run 'Test.*(Read|Query|Subscribe|Permission|Auth)' -count=1
rtk go vet ./protocol ./schema ./subscription ./executor .
rtk go test ./... -count=1
```

Remaining work for tasks 05-08:

- Runtime read catalog construction for declared query/view surfaces.
- Named declared read execution and declaration permission enforcement.
- Generated clients must call named declared-read callbacks instead of raw SQL
  for declarations.
- Row-level visibility declarations, validation, read-plan expansion, and
  per-relation enforcement before joins/evaluation.
