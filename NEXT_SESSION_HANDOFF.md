# Next session handoff

Use this file to start the next agent on the current Shunter task with no prior context.

## Latest OI-002 parity slice closed

Closed slice: OI-002 A2 one-off join-backed `COUNT(*) [AS] alias` aggregate projection.

What changed:
- `query/sql/parser.go` now accepts `COUNT(*) AS n` and `COUNT(*) n` on the existing two-table join surface while keeping missing-alias, mixed aggregate/projection, aggregate+`LIMIT`, `GROUP BY`, and multi-way join rejections.
- `protocol/handle_subscribe.go` now includes compiled aggregate metadata in join compile return paths reachable by one-off queries.
- `protocol/handle_oneoff.go` reuses the existing aggregate response path, counting matched join rows with multiplicity and returning one uint64 row under the requested alias.
- Subscribe registration still rejects parsed aggregate projections before executor registration.

Authoritative pins:
- `query/sql/parser_test.go::TestParseJoinCountStarAliasProjection`
- `query/sql/parser_test.go::TestParseJoinCountStarBareAliasProjectionWithWhere`
- `query/sql/parser_test.go::TestParseRejectsUnsupported`
- `query/sql/parser_test.go::TestParseRejectsMultiWayJoinChain`
- `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_ParityJoinCountAliasReturnsSingleAggregateRow`
- `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_ParityJoinCountBareAliasWithWhereReturnsSingleAggregateRow`
- `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_ParityJoinCountWithLimitRejected`
- `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_JoinCountAggregateStillRejected`

Next OI-002 instruction: run a fresh bounded scout before selecting another query/subscription residual; do not reopen count/join aggregate work without new regression evidence.

## Current focus

The latest user-directed work was planning-only, not implementation.

Planning completed for the first hosted-runtime v1 implementation marker:

- Implplan file: `.hermes/plans/2026-04-23_190445-hosted-runtime-top-level-api-skeleton-implplan.md`
- Marker: hosted runtime top-level API skeleton
- Goal: create the root package surface `shunter.Module`, `shunter.Config`, `shunter.Runtime`, and `shunter.Build(...)` with validation-only behavior
- No production Go code was changed for this planning step
- Stale prior `.hermes/plans/` planning docs were deleted so this implplan is the active next-slice contract
- Do not start v1.5/v2 work in this slice

If the next session is asked to implement, implement that implplan exactly and keep the slice narrow.

## Repo startup rules

Read in this order before editing:

1. `CLAUDE.md` if present / relevant to the active agent
2. `RTK.md`
3. `README.md`
4. `docs/project-brief.md`
5. `docs/EXECUTION-ORDER.md`
6. `docs/hosted-runtime-implementation-roadmap.md`
7. `docs/hosted-runtime-v1-contract.md`
8. `.hermes/plans/2026-04-23_190445-hosted-runtime-top-level-api-skeleton-implplan.md`

Use `rtk` for shell commands, including git commands.
Do not push unless explicitly asked.
Ignore unrelated dirty state unless it overlaps the implementation files.

## Current repo state relevant to the hosted-runtime skeleton

Grounded facts checked during planning:

- `go.mod` module path is `github.com/ponchione/shunter`.
- `rtk go list .` currently fails with `no Go files in /home/gernsback/source/shunter`, so adding root Go files creates the top-level import package.
- `schema.NewBuilder()` exists.
- `(*schema.Builder).Build(schema.EngineOptions)` exists.
- `(*schema.Builder).TableDef(...)`, `Reducer(...)`, `OnConnect(...)`, `OnDisconnect(...)`, and `SchemaVersion(...)` exist.
- `schema.EngineOptions` currently exposes `DataDir`, `ExecutorQueueCapacity`, `DurabilityQueueCapacity`, `EnableProtocol`, and `StartupSnapshotSchema`.
- `schema.Engine` exposes `Registry()`, `ExportSchema()`, and `Start(context.Context)`.

Current planning boundary:

- Create only the root package skeleton and validation-only `Build`.
- Do not move subsystem wiring out of `cmd/shunter-example/main.go` yet.
- Do not add network serving, lifecycle methods, local calls, codegen, contract snapshots, permissions, migrations, or v1.5/v2 declarations.
- Do not include module schema/reducer registration wrappers in the first skeleton slice; they are the immediate follow-up slice.

## Intended first implementation slice

Files likely to create:

- `module.go`
- `config.go`
- `runtime.go`
- `module_test.go` and/or `runtime_test.go`

API shape:

- `NewModule(name string) *Module`
- `(*Module).Name() string`
- `(*Module).Version(v string) *Module`
- `(*Module).VersionString() string`
- `(*Module).Metadata(values map[string]string) *Module`
- `(*Module).MetadataMap() map[string]string`
- `Config` with scalar runtime fields only
- `AuthModeDev` / `AuthModeStrict` if auth mode is included in the skeleton config
- `Build(mod *Module, cfg Config) (*Runtime, error)`
- `(*Runtime).ModuleName() string`
- `(*Runtime).Config() Config`

Behavior to pin:

- metadata input/output maps are defensively copied
- `Build` rejects nil module
- `Build` rejects blank module name
- `Build` rejects negative queue sizes
- blank `DataDir` remains allowed
- `Build` calls the private schema builder and stores the resulting `*schema.Engine` privately
- `Build` does not call `Start`, start goroutines, or open sockets

TDD/validation sequence:

1. Add failing root tests first.
2. Run `rtk go test .` and confirm the expected failures.
3. Implement the minimum root package code.
4. Run:
   - `rtk go fmt ./...`
   - `rtk go test .`
   - `rtk go test ./schema ./executor ./protocol ./subscription ./store ./commitlog`
   - `rtk go test ./...`

## Existing parity worktree context

The repo already has a broad dirty working tree from previous uncommitted OI-002 / Tier A2 query-only closures. Do not assume those changes belong to the hosted-runtime skeleton.

The current handoff before this planning update said these working-tree closures were present:

- one-off `COUNT(*) [AS] alias`
- one-off explicit projection-column aliases on the existing single-table column-list projection surface
- one-off join-backed explicit column-list projection on the existing two-table join surface
- one-off/ad hoc unindexed two-table join admission while subscribe-side unindexed join rejection is retained
- one-off cross-join `WHERE` column-equality admission while subscribe-side cross-join `WHERE` rejection is retained
- one-off join-backed `COUNT(*) [AS] alias` aggregate projection while subscribe-side aggregate rejection is retained

Do not reopen those query/parity slices while implementing the hosted-runtime skeleton unless a direct compile/test failure requires a narrow compatibility adjustment.

## Follow-up after the skeleton lands

After the root validation-only skeleton is implemented and verified, the next hosted-runtime slices should be:

1. Module schema/reducer registration wrappers:
   - `Module.TableDef(...)`
   - `Module.SchemaVersion(...)`
   - `Module.Reducer(...)`
   - `Module.OnConnect(...)`
   - `Module.OnDisconnect(...)`

2. Minimal runtime introspection:
   - likely `Runtime.ExportSchema()` delegating to the private `schema.Engine`

3. Real runtime build pipeline:
   - begin lifting the working subsystem assembly pattern from `cmd/shunter-example/main.go`

4. Lifecycle/network surface:
   - `Start(ctx)` / `Close()`
   - `ListenAndServe(...)`
   - `HTTPHandler()`

Keep the next session focused on the first skeleton unless the user explicitly asks to widen scope.
