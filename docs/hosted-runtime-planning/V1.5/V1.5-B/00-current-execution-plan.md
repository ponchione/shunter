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
- Task 01 pending.
- Task 02 pending.
- Task 03 pending.
- Task 04 pending.
- Task 05 pending.

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

Immediate next V1.5 slice after V1.5-B: V1.5-C client bindings and codegen.

