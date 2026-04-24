# V1-B Task 02: Add the first successful explicit-build test

Parent plan: `docs/hosted-runtime-planning/V1-B/2026-04-23_204414-hosted-runtime-v1b-module-registration-wrappers-implplan.md`

Objective: pin the first root-package build path that V1-B is meant to unlock.

Files:
- Modify `module_test.go`
- Keep tests in package `shunter` if private runtime inspection is needed

Test coverage to add:
- explicitly versioned module with at least one table builds successfully through `Build`
- returned runtime is non-nil
- `ModuleName()` matches authored module name
- built schema registry version matches the declared schema version
- built registry contains the declared table

Test shape guidance:
- use `SchemaVersion(1)` and one explicit `schema.TableDefinition`
- use existing schema/types definitions directly; do not invent root-level table wrappers in V1-B
- if private runtime fields must be inspected, keep the test same-package rather than widening public API

Run:
- `rtk go test .`

Expected result:
- this new success-path test fails until the wrapper methods exist and delegate correctly
