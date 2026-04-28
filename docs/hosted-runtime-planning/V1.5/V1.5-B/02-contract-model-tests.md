# V1.5-B Task 02: Add Failing Contract Model Tests

Parent plan: `docs/hosted-runtime-planning/V1.5/V1.5-B/00-current-execution-plan.md`

Objective: pin the in-memory full module contract before JSON output is added.

Likely files:
- create `runtime_contract_test.go`
- create a small focused test helper if existing module fixtures become noisy

Tests to add:
- contract export includes module name, module version, and metadata
- contract export includes schema/contract version
- contract export includes tables from `Runtime.ExportSchema`
- contract export includes reducers from `Runtime.ExportSchema`
- contract export includes V1.5-A queries and views
- permission/read-model fields are present as empty or reserved values
- migration metadata fields are present as empty or reserved values
- codegen/export metadata is present enough for V1.5-C to consume
- contract export returns detached values
- contract export works before `Runtime.Start`
- contract export works after `Runtime.Close`

Test boundaries:
- do not generate client bindings
- do not implement permissions behavior
- do not implement migration diff behavior
- do not add executable migrations

