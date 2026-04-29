# V1-A Task 04: Implement `Runtime` and `Build`

Parent plan: `docs/features/V1/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`

Objective: add the minimal root runtime owner object and the V1-A `Build` validation/mapping path, while preserving existing schema-layer failure behavior.

Files:
- Create `runtime.go`

Implementation requirements:
- Add `type Runtime struct { ... }` with private fields for:
  - `moduleName string`
  - `config Config`
  - `engine *schema.Engine`
- Add:
  - `Build(mod *Module, cfg Config) (*Runtime, error)`
  - `ModuleName() string`
  - `Config() Config`
- `Config()` must return a value copy

Build validation order must be:
1. reject nil module
2. reject blank module name after `strings.TrimSpace`
3. reject negative executor queue capacity
4. reject negative durability queue capacity
5. reject unknown auth mode
6. call `mod.builder.Build(schema.EngineOptions{...})`
7. if schema build fails, return nil runtime and wrap with hosted-runtime context
8. if schema build succeeds in a later V1-B-capable world, return a runtime holding module name, config, and engine

Exact schema option mapping:
- `DataDir: cfg.DataDir`
- `ExecutorQueueCapacity: cfg.ExecutorQueueCapacity`
- `DurabilityQueueCapacity: cfg.DurabilityQueueCapacity`
- `EnableProtocol: cfg.EnableProtocol`

Guardrails:
- Do not set `StartupSnapshotSchema`
- Do not call `engine.Start(...)`
- Do not start goroutines
- Do not open sockets
- Do not create HTTP handlers
- Do not initialize protocol serving
- Do not add lifecycle methods yet
- Do not change `schema.EngineOptions`, `schema.Builder.Build`, or schema validation behavior

Run:
- `rtk go test .`

Expected result:
- root package tests pass
- the normal empty-module build path still fails via schema validation rather than succeeding

Done when:
- all V1-A root tests pass
- schema-layer failure behavior is preserved
- the root package exposes only the V1-A surface
