# V2.5 Task 02: Schema Table Read Policy

Parent plan: `docs/features/V2.5/00-current-execution-plan.md`

Depends on:
- Task 01 stack reconfirmation

Objective: add table read policy metadata to the schema layer and make it
available to runtime/protocol/contract/codegen consumers without yet enforcing
raw SQL reads.

## Required Context

Read:
- `docs/features/V2/READ-AUTHORIZATION-DESIGN.md`
- `docs/features/V2.5/01-stack-prerequisites.md`

Inspect:

```sh
rtk go doc ./schema.TableSchema
rtk go doc ./schema.TableDefinition
rtk go doc ./schema.TableOption
rtk go doc ./schema.SchemaExport
rtk go doc . ModuleContract
rtk go doc . Runtime.ExportContract
rtk rg -n "TableSchema|TableDefinition|TableOption|SchemaExport|ExportContract|TableMigration" schema *.go codegen contractdiff contractworkflow
```

## Target Behavior

Add schema-level table read policy:

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

Exact names may follow local style if necessary, but keep the semantics.

Default table access must be private.

Add builder/module authoring options:

```go
schema.WithPrivateRead()
schema.WithPublicRead()
schema.WithReadPermissions("messages:read")
```

Rules:

- `WithPrivateRead` denies external raw SQL unless the caller has bypass
  privileges.
- `WithPublicRead` allows external raw SQL without permission tags.
- `WithReadPermissions(...)` allows external raw SQL only when all required
  tags are present.
- empty permission strings are invalid.
- duplicate permission strings should be normalized or rejected consistently
  with existing permission metadata validation.
- read policy values must be defensively copied when they cross module/runtime
  boundaries.

## Contract And Export Requirements

Expose table policy through:

- schema export
- runtime contract JSON
- any contract validation surface that rebuilds schema lookup from contract
  data
- generated-client metadata if table metadata is already emitted there

Do not enforce raw SQL yet in this task. Enforcement belongs to task 04.

## Tests To Add First

Add focused failing tests for:

- default table policy is private
- `WithPublicRead` records public access
- `WithPrivateRead` records private access
- `WithReadPermissions` records permissioned access and copies permission
  slices defensively
- invalid/blank read permission tags fail build
- schema export and `Runtime.ExportContract` include table read policy
- contract validation rejects invalid table read policy metadata

Prefer targeted tests in `schema`, root runtime contract tests, and codegen or
contract tests only where the metadata is consumed.

## Validation

Run at least:

```sh
rtk go fmt ./schema . ./codegen ./contractdiff ./contractworkflow
rtk go test ./schema -count=1
rtk go test . -run 'Test.*(Table|Schema|Contract|Read|Permission)' -count=1
rtk go test ./codegen ./contractdiff ./contractworkflow -count=1
rtk go vet ./schema . ./codegen ./contractdiff ./contractworkflow
```

Expand to `rtk go test ./... -count=1` if exported contract or schema behavior
changes broadly.

## Completion Notes

When complete, update this file with:

- exported API names
- behavior changes visible in contracts/schema JSON
- validation commands run
- any compatibility concern that task 04 must account for

Completed 2026-04-29.

Exported API names:

- `schema.TableAccess`
- `schema.TableAccessPrivate`
- `schema.TableAccessPublic`
- `schema.TableAccessPermissioned`
- `schema.ReadPolicy`
- `schema.ErrInvalidTableReadPolicy`
- `schema.ValidateReadPolicy`
- `schema.WithPrivateRead`
- `schema.WithPublicRead`
- `schema.WithReadPermissions`
- `contractdiff.SurfaceTableReadPolicy`

Schema/contract JSON behavior changes:

- `schema.TableSchema` and `schema.TableExport` now include
  `read_policy`.
- Table read policy JSON uses string access values: `private`, `public`, and
  `permissioned`.
- `read_policy.permissions` is emitted as an array in schema export and
  runtime contract JSON; default/private/public policies emit an empty array.
- Absent legacy `read_policy` fields decode as zero-value private policy.
- `Runtime.ExportContract` copies and normalizes table read policies into the
  schema section.
- `ValidateModuleContract` rejects invalid access modes, private/public
  policies with permissions, permissioned policies without permissions, and
  blank or duplicate permission tags.
- TypeScript codegen emits `tableReadPolicies` beside existing table metadata.
- `contractdiff` reports table read policy changes on
  `table_read_policy`, using table migration metadata for policy warnings.

Validation commands run:

- `rtk go fmt ./schema . ./codegen ./contractdiff ./contractworkflow`
- `rtk go test ./schema -count=1`
- `rtk go test . -run 'Test.*(Table|Schema|Contract|Read|Permission)' -count=1`
- `rtk go test ./codegen ./contractdiff ./contractworkflow -count=1`
- `rtk go vet ./schema . ./codegen ./contractdiff ./contractworkflow`
- `rtk go test ./... -count=1`
- `rtk go tool staticcheck ./...`

Task 04 compatibility concerns:

- Raw SQL admission is still unchanged; private/default-private metadata is
  visible but not enforced in Task 02.
- Existing contracts that omit `read_policy` now mean private. Task 04 must
  account for this default when enabling enforcement so tests/examples that
  need raw SQL reads mark tables public or permissioned first.
- Snapshot schema encoding does not persist read policy metadata, so read
  policy changes do not affect durable data-shape compatibility checks.
