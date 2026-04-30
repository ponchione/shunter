# Hosted Runtime V1.5-B Current Execution Plan

Goal: export a full module contract artifact as deterministic canonical JSON.

Task sequence:
1. Reconfirm V1.5-A declaration metadata and the existing schema export.
2. Add failing tests for the in-memory contract model.
3. Implement module contract assembly from module, schema, reducers, queries,
   and views.
4. Add deterministic canonical JSON snapshot output.
5. Format and validate V1.5-B gates.

Task progress:
- Task 01 complete: prerequisite `go doc` checks confirmed `Module`,
  `Module.Describe`, `Runtime`, `Runtime.ExportSchema`,
  `schema.SchemaExport`, and `schema.ReducerExport` expose the needed live
  surfaces. Module descriptions provide identity, metadata, queries, and views;
  schema export provides versioned tables and reducers.
- Task 02 complete: `runtime_contract_test.go` pins the full in-memory
  contract model, reserved permission/read-model/migration sections, detached
  values, and lifecycle-safe export before start and after close.
- Task 03 complete: `Runtime.ExportContract()` returns a detached
  `ModuleContract` combining module identity, schema export, reducers, queries,
  views, reserved sections, and codegen/export metadata.
- Task 04 complete: `Runtime.ExportContractJSON()` and
  `ModuleContract.MarshalCanonicalJSON()` provide deterministic indented JSON
  with `DefaultContractSnapshotFilename` set to `shunter.contract.json`.
- Task 05 complete: validation passed with `rtk go fmt .`,
  `rtk go test . -run 'Test.*Contract|Test.*Export.*JSON' -count=1`,
  `rtk go test . -count=1`, `rtk go vet .`, and
  `rtk go test ./schema -count=1`.

V1.5-B target artifact:
- module identity and module version
- schema/contract version
- tables
- reducers
- queries
- views
- reserved fields for permissions/read-model declarations
- reserved fields for migration metadata
- codegen/export metadata

Default repo snapshot name:
- `shunter.contract.json`

Historical sequencing note: later hosted-runtime slices have since landed. Do
not treat this completed V1.5-B plan as a live handoff; use
`docs/internal/HOSTED_RUNTIME_PLANNING_HANDOFF.md` for current hosted-runtime status.
