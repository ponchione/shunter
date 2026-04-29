# V2.5 Task 01: Reconfirm Read Authorization Stack

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Objective: establish the exact live-code surfaces that subsequent V2.5 workers will
edit. This task is read-only unless the worker finds stale documentation that
directly blocks V2.5 implementation.

## Required Reading

Read:
- `RTK.md`
- `NEXT_SESSION_HANDOFF.md`
- `HOSTED_RUNTIME_PLANNING_HANDOFF.md`
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- `docs/features/V2.5/README.md`
- `docs/features/V2.5/00-current-execution-plan.md`

Use V2-D and V2-E only for concrete background:
- `docs/features/V2/V2-D/00-current-execution-plan.md`
- `docs/features/V2/V2-E/00-current-execution-plan.md`
- `docs/features/V2/V2-E/04-read-permission-extension.md`

Do not read broad roadmap/decomposition docs unless a dependency question
cannot be answered from the files above or live code.

## Required Inspection Commands

Run these before planning code edits:

```sh
rtk go doc . QueryDeclaration
rtk go doc . ViewDeclaration
rtk go doc . PermissionMetadata
rtk go doc ./schema.TableSchema
rtk go doc ./schema.TableDefinition
rtk go doc ./schema.TableOption
rtk go doc ./schema.SchemaLookup
rtk go doc ./protocol.ValidateSQLQueryString
rtk go doc ./protocol.SubscribeSingleMsg
rtk go doc ./protocol.OneOffQueryMsg
rtk go doc ./types.CallerContext
rtk go doc ./executor.ErrPermissionDenied
rtk go list -json ./protocol
rtk go list -json ./schema
rtk go list -json ./subscription
```

Useful targeted searches:

```sh
rtk rg -n "compileSQLQueryString|ValidateSQLQueryString|handleOneOffQuery|handleSubscribe" protocol
rtk rg -n "PermissionMetadata|WithReducerPermissions|WithPermissions|AllowAllPermissions" .
rtk rg -n "TableByName|TableExists|SchemaLookup|TableSchema|TableDefinition|TableOption" schema protocol
rtk rg -n "QueryDeclaration|ViewDeclaration|querySQL|viewSQL|runQuery|subscribeView" . codegen
```

## Context To Confirm

Confirm these live facts before subsequent implementation:

- `protocol.compileSQLQueryString` is the shared raw SQL compiler used by
  one-off query, subscribe admission, and declaration SQL validation.
- `protocol.handleOneOffQuery` compiles SQL, validates the predicate, then
  reads committed state.
- `protocol.handleSubscribeSingle` and `protocol.handleSubscribeMulti` compile
  SQL before forwarding predicates to the executor/subscription manager.
- `schema.SchemaLookup` is the table/column resolution surface consumed by the
  protocol SQL compiler.
- `schema.TableSchema` currently has no table access metadata.
- `PermissionMetadata` currently describes passive tags for reducers, queries,
  and views.
- reducer permission enforcement already uses `types.CallerContext` plus
  `AllowAllPermissions`.
- generated declaration helpers currently execute declaration SQL through raw
  query/subscription callbacks.

## Output

The worker should report:

- any drift from the assumptions above
- packages and files likely touched by tasks 02-10
- any existing tests that already pin needed behavior
- any risky compatibility points, especially protocol error text

No implementation is expected from this task.
