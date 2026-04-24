# Hosted runtime planning handoff

Use this file for Shunter hosted-runtime planning phases. Do not use `NEXT_SESSION_HANDOFF.md` for hosted-runtime planning; that file stays reserved for the next TECH-DEBT / parity execution agent.

## Current hosted-runtime planning objective

The hosted-runtime work is in planning mode unless the user explicitly asks for implementation.

A concrete V1-A implementation plan is ready:

- `.hermes/plans/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`

The previous `.hermes/plans/2026-04-23_190445-hosted-runtime-top-level-api-skeleton-implplan.md` plan was superseded and deleted as stale planning clutter.

## Read before hosted-runtime planning or implementation

1. `RTK.md`
2. `README.md`
3. `docs/project-brief.md`
4. `docs/decomposition/EXECUTION-ORDER.md`
5. `docs/decomposition/APP-RUNTIME-LAYER-AND-USAGE-SURFACE.md`
6. `docs/decomposition/hosted-runtime-version-phases.md`
7. `docs/hosted-runtime-implementation-roadmap.md`
8. `docs/decomposition/hosted-runtime-v1-contract.md`
9. `.hermes/plans/2026-04-23_195510-hosted-runtime-v1a-top-level-api-owner-skeleton-implplan.md`

Use `rtk` for shell commands, including git commands. Do not push unless explicitly asked.

## V1-A decisions now recorded

The V1-A/V1-B boundary decisions are:

- Empty modules do not build successfully in V1-A.
- `NewModule` must not default schema version to 1.
- `Module.SchemaVersion(...)` belongs to V1-B.
- V1-A must not change lower-level schema semantics.
- Do not add `schema.EngineOptions.AllowEmptySchema`, fake tables, hidden default schema versions, or smoke-only empty-schema escape hatches.
- The root package `github.com/ponchione/shunter` is the normal v1 app-facing package.
- Do not add an `engine/` package in V1-A.

`docs/decomposition/hosted-runtime-version-phases.md` records these decisions.

## V1-A implementation target

Implement only the top-level API owner skeleton:

- `shunter.Module`
- `shunter.Config`
- `shunter.Runtime`
- `shunter.Build(module, config)`

Expected new root files:

- `module.go`
- `config.go`
- `runtime.go`
- `module_test.go`
- optionally `runtime_test.go`

The root package currently does not exist; `rtk go list .` fails with `no Go files in /home/gernsback/source/shunter`.

## V1-A scope guardrails

Do not implement in V1-A:

- `Runtime.Start`
- `Runtime.Close`
- `ListenAndServe`
- `HTTPHandler`
- network serving
- goroutines
- local reducer/query APIs
- schema/reducer/lifecycle registration wrappers
- codegen / contract snapshots
- permissions or migration metadata
- v1.5/v2 surfaces
- any OI-002/query/protocol parity cleanup unless it directly blocks compilation of the V1-A root package

Do not edit lower-level schema behavior for V1-A unless a compile error proves a direct need:

- `schema/build.go`
- `schema/builder.go`
- `schema/validate_schema.go`

## TDD-first implementation shape

Follow the plan exactly:

1. Add failing root tests first.
2. Implement `Module` metadata/version/name shell.
3. Implement scalar `Config` and `AuthMode`.
4. Implement `Runtime` and `Build` validation/delegation.
5. Validate with RTK commands.

Important expected behavior:

- Root validation rejects nil module, blank module name, negative queue capacities, and invalid auth mode before schema build.
- Publicly valid empty module input reaches existing schema validation and fails there, likely with `schema.ErrSchemaVersionNotSet` first.
- V1-A should not return a successful runtime for an empty module.
- A successful real build path is deferred until V1-B adds `Module.SchemaVersion(...)` and `Module.TableDef(...)` wrappers.

## Validation commands for implementation

Use targeted gates first:

```bash
rtk go test .
rtk go test ./schema -count=1
rtk go vet . ./schema
```

Then prefer broad validation when the working tree allows it:

```bash
rtk go test ./... -count=1
```

If broad tests fail because of unrelated dirty OI-002/query/protocol state, report the exact unrelated failures and preserve the narrower root/schema passing gates. Do not fix unrelated parity code inside V1-A.

## Working tree caution

The broader working tree may contain unrelated query/protocol/OI-002 changes. They are not part of hosted-runtime V1-A planning. Leave them alone unless the user explicitly pivots back to OI-002 parity work.

## Next slice after V1-A

After V1-A is implemented and accepted, the next slice should be V1-B module registration wrappers:

- `Module.SchemaVersion(...)`
- `Module.TableDef(...)`
- `Module.Reducer(...)`
- `Module.OnConnect(...)`
- `Module.OnDisconnect(...)`

V1-B should be the first slice where a non-empty, explicitly versioned module can build successfully through the top-level API.
