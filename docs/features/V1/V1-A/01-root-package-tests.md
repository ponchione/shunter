# V1-A Task 01: Add failing root package tests

Parent plan: `docs/features/V1/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`

Objective: create the first root-package tests that pin the V1-A public API and validation boundary before production code exists.

Scope:
- Create `module_test.go`
- Optionally create `runtime_test.go` if test separation improves readability
- Do not create production files yet

Tests to add:
- `NewModule("chat")` returns non-nil
- `Name()` returns the exact module name
- `Version("v0.1.0")` is chainable and visible through `VersionString()`
- `Metadata(input)` defensively copies input
- `MetadataMap()` defensively copies output
- `Metadata(nil)` clears metadata
- blank module names are constructible, but `Build` rejects them later
- `Build(nil, Config{})` rejects nil module before schema build
- `Build(NewModule("   "), Config{})` rejects blank/whitespace-only names before schema build
- negative executor queue capacity is rejected before schema build
- negative durability queue capacity is rejected before schema build
- invalid `AuthMode` is rejected before schema build
- `Build(NewModule("chat"), Config{})` reaches schema validation and fails with `schema.ErrSchemaVersionNotSet` or another schema-layer validation error, not a config error
- `EnableProtocol` and `ListenAddr` are retained as config fields, but V1-A exposes no serving API

Assertions guidance:
- Use `errors.Is` for schema-layer failures
- For root validation failures, assert the relevant field/cause without overfitting whole error strings
- Do not assert any successful empty-module build path

Run:
- `rtk go test .`

Expected result:
- failure due to missing root package symbols and/or missing files

Done when:
- the tests clearly encode the intended V1-A contract
- the first test run fails for the expected missing-implementation reasons
