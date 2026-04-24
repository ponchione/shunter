# V1-A Task 03: Implement `Config` and `AuthMode`

Parent plan: `docs/hosted-runtime-planning/V1-A/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`

Objective: add the scalar runtime config surface used by `Build`, without introducing lifecycle or protocol behavior.

Files:
- Create `config.go`

Implementation requirements:
- Add `type AuthMode int`
- Add constants:
  - `AuthModeDev`
  - `AuthModeStrict`
- Add `type Config struct` with fields:
  - `DataDir string`
  - `ExecutorQueueCapacity int`
  - `DurabilityQueueCapacity int`
  - `EnableProtocol bool`
  - `ListenAddr string`
  - `AuthMode AuthMode`
- Keep config scalar and root-runtime focused
- Do not embed lower-level protocol/auth/subsystem structs
- `ListenAddr` and `AuthMode` exist for later slices only; V1-A must not serve or start anything
- Do not add validation helpers unless needed by `Build`

Run:
- `rtk go test .`

Expected result:
- config symbols compile
- runtime/build tests still fail until Task 04 lands

Done when:
- `Config` matches the V1-A plan exactly
- no extra API surface or side effects have been introduced
