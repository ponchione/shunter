# V1-B Task 04: Pin error preservation and builder freeze behavior

Parent plan: `docs/hosted-runtime-planning/V1-B/2026-04-23_204414-hosted-runtime-v1b-module-registration-wrappers-implplan.md`

Objective: prove the root wrappers preserve existing schema-layer semantics instead of inventing new ones.

Files:
- Modify `module_test.go`
- Modify `runtime_test.go` if clearer
- Optionally create `test_helpers_test.go`

Tests to add:
- missing `SchemaVersion` still surfaces `schema.ErrSchemaVersionNotSet`
- no tables still surfaces `schema.ErrNoTables`
- duplicate table names preserve `schema.ErrDuplicateTableName`
- duplicate reducer names preserve `schema.ErrDuplicateReducerName`
- reserved reducer names preserve `schema.ErrReservedReducerName`
- nil reducer handlers preserve `schema.ErrNilReducerHandler`
- duplicate lifecycle reducers preserve `schema.ErrDuplicateLifecycleReducer`
- successful first `Build` freezes the module so later mutation or rebuild preserves `schema.ErrAlreadyBuilt`

Assertion guidance:
- prefer `errors.Is` over whole-string matching
- wrap schema errors with root context, but preserve sentinels

Run:
- `rtk go test .`
