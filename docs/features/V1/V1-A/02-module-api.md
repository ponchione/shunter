# V1-A Task 02: Implement `Module`

Parent plan: `docs/features/V1/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`

Objective: add the root `Module` owner type with defensive metadata behavior and a private schema-builder seam.

Files:
- Create `module.go`
- Update tests only if Task 01 left obvious compile-only gaps

Implementation requirements:
- Add `type Module struct { ... }` with private fields for:
  - `name string`
  - `version string`
  - `metadata map[string]string`
  - `builder *schema.Builder`
- Add:
  - `NewModule(name string) *Module`
  - `Name() string`
  - `Version(v string) *Module`
  - `VersionString() string`
  - `Metadata(values map[string]string) *Module`
  - `MetadataMap() map[string]string`
- `NewModule(name)` must initialize `builder` with `schema.NewBuilder()`
- Do not set schema version here
- Do not register tables/reducers here
- `Version` must be chainable and preserve the exact supplied string
- `Metadata(values)` must defensively copy input
- `Metadata(nil)` must clear metadata to an empty state
- `MetadataMap()` must return a defensive copy
- Blank names may still be constructed here; validation belongs to `Build`

Run:
- `rtk go test .`

Expected result:
- module tests compile and pass
- config/runtime/build-related tests still fail until later tasks land

Done when:
- the `Module` API matches the V1-A plan
- metadata copy semantics are pinned by tests
- validation has not been prematurely moved into `NewModule`
