# V2.5 Task 07: Visibility Filter Declarations

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Depends on:
- Task 02 schema table read policy
- Task 05 declared read catalog and runtime API

Objective: add authored row-level visibility filter declarations and validate
them at build time. This task records filters and proves they are well-formed;
task 08 applies them to read execution.

## Required Context

Read:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- `docs/features/V2.5/05-declared-read-catalog-and-runtime-api.md`

Inspect:

```sh
rtk go doc . Module
rtk go doc ./protocol.ValidateSQLQueryString
rtk rg -n "QueryDeclaration|ViewDeclaration|validateModuleDeclarationSQL|ErrInvalidDeclarationSQL|moduleSnapshot|ExportContract" .
rtk rg -n "compileSQLQueryString|SQLQueryValidationOptions|Projection|Limit|Join" protocol query/sql
```

## Target Behavior

Add authored visibility filters:

```go
type VisibilityFilterDeclaration struct {
    Name string
    SQL  string
}

func (m *Module) VisibilityFilter(decl VisibilityFilterDeclaration) *Module
```

Exact names may follow local style, but the concept must be clear.

A visibility filter describes rows from one return table that are visible to an
external caller. Example:

```sql
SELECT * FROM messages WHERE owner = :sender
```

Multiple filters for the same return table OR together.

If a table has no filter, row-level visibility does not restrict rows for that
table.

## Build-Time Validation

Reject:

- blank filter name
- duplicate filter name
- blank SQL
- invalid SQL
- SQL that cannot be tied to exactly one return table
- SQL whose return table is unknown
- SQL forms the read planner cannot enforce correctly

The validation should use the same SQL compiler/parser family as one-off and
subscription admission where possible. Do not invent a separate string parser.

Store validated filter metadata in the runtime/module snapshot with defensive
copies.

## Contract Metadata

Export visibility filter metadata or a safe summary through the contract so
diff/migration tooling can classify add/remove/change events in task 09.

At minimum, consumers need:

- filter name
- filter SQL or stable hash plus human-readable metadata
- return table name/ID
- whether the filter uses caller identity

Prefer exporting SQL if existing contract conventions already expose authored
SQL for query/view declarations.

## Tests To Add First

Add focused failing tests for:

- filter declaration appears in module description/runtime snapshot
- blank name is rejected
- duplicate name is rejected
- invalid SQL is rejected
- unknown return table is rejected
- filter return table is recorded
- `:sender` usage is recorded when present
- multiple filters for one table are preserved in declaration order
- exported contract includes visibility filter metadata
- returned metadata is defensively copied

Do not add execution filtering tests in this task. Those belong to task 08.

## Validation

Run at least:

```sh
rtk go fmt . ./protocol ./query/sql ./codegen ./contractdiff
rtk go test . -run 'Test.*(Visibility|Declaration|Contract|Read|Query|View)' -count=1
rtk go test ./protocol ./query/sql ./codegen ./contractdiff -count=1
rtk go vet . ./protocol ./query/sql ./codegen ./contractdiff
```

Run full tests if contract JSON shape changes broadly.

## Completion Notes

When complete, update this file with:

- public declaration API
- validation rules actually enforced
- contract metadata shape
- validation commands run
- task 08 assumptions about stored filter metadata

