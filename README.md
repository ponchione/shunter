# Shunter

Shunter is a Go-native hosted runtime for stateful realtime applications. It
combines module definition, embedded relational storage, durable commit logging,
serialized reducer execution, subscription delta evaluation, and WebSocket
delivery behind a single runtime-owned API.

The project is under active development. Core subsystems are implemented and
covered by meaningful tests, while the top-level developer experience is still
being refined.

## Project Status

The supported app-facing entrypoint is the root `shunter` package:

- `Module` defines application tables, reducers, lifecycle hooks, queries,
  views, visibility filters, metadata, and migration metadata.
- `Config` controls runtime startup, persistence, authentication, protocol
  settings, and serving behavior.
- `Build` validates and constructs a runtime from a module definition.
- `Runtime` owns lifecycle, local reads, reducer calls, declared reads,
  contract/schema export, HTTP serving, snapshots, compaction, and graceful
  shutdown.

Shunter is not currently positioned as a production-ready database or managed
service. It is best understood as an implementation-focused runtime project
with substantial subsystem coverage and an emerging hosted-runtime API.

## Goals

Shunter is designed to:

- let applications define their state model and business logic in Go
- execute reducers against runtime-owned state with serialized transaction
  semantics
- persist committed changes through an append-only durability path
- evaluate subscriptions at commit time and deliver precise client deltas
- expose authentication, lifecycle, protocol, and serving primitives from one
  self-hosted runtime
- keep the implementation independent, auditable, and testable

Current non-goals:

- managed cloud hosting
- distributed database behavior
- broad SQL compatibility
- multi-language module hosting
- protocol or client compatibility with another runtime

## Implemented Components

The repository contains working implementations across the main runtime
subsystems:

- `types` - core value, identity, connection, and reducer types
- `auth` - JWT validation, identity derivation, and anonymous token minting
- `schema` - schema builder, reflection path, registry, export, and startup
  compatibility checks
- `store` - tables, indexes, transactions, changesets, snapshots, and recovery
  hooks
- `commitlog` - durable record format, worker, snapshot I/O, replay, recovery,
  and compaction
- `executor` - reducer registry, serialized execution, lifecycle and scheduler
  wiring, and subscription dispatch
- `subscription` - predicate model, pruning indexes, delta evaluation, fanout,
  and confirmed-read delivery
- `protocol` - wire codecs, WebSocket upgrade/auth path, connection lifecycle,
  dispatch, outbound delivery, and backpressure handling
- `query/sql` - the intentionally narrow SQL surface used by subscription and
  one-off query paths
- `bsatn` - binary value encoding used across runtime boundaries
- root `shunter` package - hosted-runtime API, lifecycle management, local
  calls, protocol serving, declared reads, schema export, and contract export

## Runtime Entrypoint

There is no maintained bundled demo command at the moment. The root package is
the maintained integration surface for application code and tests.

Important public APIs include:

- `NewModule`
- `Module.TableDef`
- `Module.Reducer`
- `Module.Query`
- `Module.View`
- `Module.VisibilityFilter`
- `Build`
- `Runtime.Start`
- `Runtime.Close`
- `Runtime.CallReducer`
- `Runtime.CallQuery`
- `Runtime.SubscribeView`
- `Runtime.CreateSnapshot`
- `Runtime.CompactCommitLog`
- `Runtime.HTTPHandler`
- `Runtime.ListenAndServe`
- `Runtime.ExportSchema`
- `Runtime.ExportContract`

A runnable example should be added when it can demonstrate the intended product
workflow end to end rather than serve as a temporary smoke test.

## Current Limitations

The runtime has meaningful implementation depth, but several areas are still
early or intentionally narrow:

- developer onboarding material is limited
- there is no maintained hello-world command or tutorial flow
- client bindings and code generation exist, but onboarding material around
  them is still limited
- SQL support is scoped to the v1 read-surface matrix; Shunter does not promise
  broad SQL database compatibility
- protocol, recovery, subscription, and reducer semantics are still being
  hardened through focused tests and debt reconciliation
- public API stability should be expected to evolve while the hosted-runtime
  surface settles

## Versioning

Shunter's source version lives in `VERSION` and uses v-prefixed SemVer. The
root package exposes that value through `shunter.CurrentBuildInfo()`, and the
CLI reports it with:

```bash
rtk go run ./cmd/shunter --version
rtk go run ./cmd/shunter version
```

Release builds can stamp exact build metadata without changing source files:

```bash
rtk go build -ldflags "-X github.com/ponchione/shunter.Version=v0.1.0 -X github.com/ponchione/shunter.Commit=<git-sha> -X github.com/ponchione/shunter.Date=<utc-rfc3339>" ./cmd/shunter
```

Use `vX.Y.Z` git tags for releases. `Module.Version(...)` is separate: it is
application module metadata exported into `ModuleContract` artifacts, not the
Shunter tool/runtime release version.

## Repository Guide

For human orientation, start with:

1. `README.md` - project overview and current status
2. `docs/README.md` - guide to the app-author and codebase docs tree
3. `docs/getting-started.md` - first-pass app-author path for embedding
   Shunter

For implementation planning context, use `working-docs/README.md`,
`working-docs/shunter-design-decisions.md`, and the narrow
`working-docs/specs/` section for the surface being touched.

For implementation work, inspect the active packages directly:

- `schema/`
- `store/`
- `commitlog/`
- `executor/`
- `subscription/`
- `protocol/`
- root package files such as `module.go`, `runtime.go`, `lifecycle.go`,
  `network.go`, and `config.go`

For automation or agent-driven work, follow `AGENTS.md` and `RTK.md` before
running commands or editing files.

App-author and codebase docs live under `docs/`. Implementation planning,
baseline specs, audits, and roadmaps live under `working-docs/`. Prefer live
code, tests, and the narrow spec section for the surface being touched.

## Validation

Run the full Go test suite:

```bash
rtk go test ./...
```

Run Go vet:

```bash
rtk go vet ./...
```

Run pinned static analysis:

```bash
rtk go tool staticcheck ./...
```

Staticcheck is expected to pass. Treat failures as real cleanup findings unless
a task explicitly narrows the verification scope.

## Source Provenance

Shunter is intended to be an independent clean-room implementation. The project
uses public documentation, behavior analysis, original specifications, tests,
and implementation audits as design inputs; it does not rely on source reuse
from external projects.
