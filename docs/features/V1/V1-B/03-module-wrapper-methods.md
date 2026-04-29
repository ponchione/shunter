# V1-B Task 03: Implement `Module` registration wrappers

Parent plan: `docs/features/V1/V1-B/2026-04-23_204414-hosted-runtime-v1b-module-registration-wrappers-implplan.md`

Objective: add the explicit top-level module authoring methods as a thin layer over `schema.Builder`.

Files:
- Modify `module.go`

Add these methods:
- `SchemaVersion(v uint32) *Module`
- `TableDef(def schema.TableDefinition, opts ...schema.TableOption) *Module`
- `Reducer(name string, h schema.ReducerHandler) *Module`
- `OnConnect(h func(*schema.ReducerContext) error) *Module`
- `OnDisconnect(h func(*schema.ReducerContext) error) *Module`

Implementation requirements:
- each method is chainable and returns `m`
- each method delegates directly to `m.builder`
- do not add a parallel schema DSL or duplicate root-level definition structs
- do not eagerly panic on nil handlers; preserve schema-layer validation timing
- do not introduce root-level mutable/frozen state separate from the underlying builder

Run:
- `rtk go test .`

Expected result:
- the explicit-build success-path test now passes or gets close enough to expose remaining error-preservation tests
