# Shunter

Shunter is a self-hosted Go runtime for stateful realtime applications. An
application declares its schema, reducers, procedures, lifecycle hooks, reads,
permissions, and protocol behavior in Go; Shunter builds that declaration into
one runtime with embedded relational storage, durable commit logging,
serialized reducer execution, subscription delta evaluation, and WebSocket
delivery.

A Shunter-backed service is a normal Go binary. Applications can link Shunter
as an embedded runtime library, use `shunter.Run` as the backend entrypoint, or
mount the runtime handler inside an app-owned HTTP server. This repository
contains the runtime, CLI tooling, contract export and validation workflow,
TypeScript client generation, an end-to-end hosted example, benchmarks, and
operator documentation for the supported v1 surface.

## At a Glance

- Go module declarations for tables, reducers, procedures, declared queries,
  declared live views, visibility filters, lifecycle hooks, permissions, and
  migration metadata
- runtime-owned durability through snapshots, segmented commit logs, recovery,
  manual compaction, and offline backup/restore helpers
- serialized reducer transactions with separate procedure handlers for
  client-callable workflows that need external I/O before requesting commits
- Shunter-native WebSocket protocol with JWT auth, declared-read parameters,
  subscription deltas, bounded client helpers, and backpressure controls
- contract JSON validation, compatibility review commands, and generated
  TypeScript bindings backed by a local `@shunter/client` runtime package
- operational runbooks, benchmark workflow guidance, and recorded advisory
  performance envelopes

## Project Status

The supported app-facing entrypoint is the root `shunter` package:

- `Module` defines application tables, reducers, procedures, lifecycle hooks,
  queries, views, visibility filters, metadata, and migration metadata.
- `Config` controls runtime startup, persistence, authentication, protocol
  settings, and serving behavior.
- `ConfigFromEnv` reads the small Shunter-scoped hosted-app environment
  surface for data directory, listen address, protocol, and auth
  configuration.
- `Build` validates and constructs a runtime from a module definition.
- `Run` builds a runtime, serves it with the runtime-owned HTTP/protocol
  lifecycle, and shuts down cleanly when the supplied context is canceled.
- `Runtime` owns lifecycle, local reads, reducer and procedure calls, declared
  reads, contract/schema export, HTTP serving, snapshots, compaction, and
  graceful shutdown.

Shunter v1 is a self-hosted embedded runtime, not a managed database service.
The stable v1 surfaces are the root package APIs, Shunter-native protocol,
contract JSON, generated TypeScript, read surfaces, and documented operations
described under `docs/`.

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
- third-party protocol or client compatibility

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
- root procedure support - app-owned service handlers that can perform
  external work before requesting reducer commits
- `subscription` - predicate model, pruning indexes, delta evaluation, fanout,
  and confirmed-read delivery
- `protocol` - wire codecs, WebSocket upgrade/auth path, connection lifecycle,
  dispatch, outbound delivery, and backpressure handling
- `protocolclient` - bounded WebSocket client helpers used by running-app CLI
  commands and maintenance tooling
- `query/sql` - the intentionally narrow SQL surface used by subscription and
  one-off query paths
- `bsatn` - binary value encoding used across runtime boundaries
- `contractworkflow` and `codegen` - contract loading, validation,
  compatibility workflow, and generated TypeScript bindings
- root `shunter` package - hosted-runtime API, lifecycle management, local
  calls, procedures, protocol serving, declared reads, schema export, and
  contract export

## Runtime Entrypoint

The shortest hosted-backend entrypoint is:

```go
cfg := shunter.ConfigFromEnv()
if err := shunter.Run(context.Background(), app.Module(), cfg); err != nil {
	log.Fatal(err)
}
```

Applications that need custom HTTP routing can still use `Build`,
`Runtime.Start`, `Runtime.HTTPHandler`, and `Runtime.Close` directly. The root
package remains the maintained integration surface for application code and
tests.

Important public APIs include:

- `NewModule`
- `Module.TableDef`
- `Module.Reducer`
- `Module.Procedure`
- `Module.Query`
- `Module.View`
- `Module.VisibilityFilter`
- `Build`
- `ConfigFromEnv`
- `ConfigFromEnvE`
- `Run`
- `Runtime.Start`
- `Runtime.Close`
- `Runtime.CallReducer`
- `Runtime.WaitUntilDurable`
- `Runtime.CallProcedure`
- `Runtime.CallQuery`
- `Runtime.SubscribeView`
- `Runtime.CreateSnapshot`
- `Runtime.CompactCommitLog`
- `Runtime.HTTPHandler`
- `Runtime.ListenAndServe`
- `Runtime.ExportSchema`
- `Runtime.ExportContract`

The canonical hosted example is
[`examples/hosted-chat`](examples/hosted-chat/README.md). It demonstrates a Go
module, `shunter.Run`, contract export, TypeScript generation, and a
frontend-shaped client that calls reducers and procedures and subscribes to a
live view.

## Current Limitations

The runtime has meaningful implementation depth, with several areas kept
intentionally narrow:

- the bundled hosted-chat example is small by design; larger applications
  should add workload-specific tests, benchmarks, and operational gates
- generated TypeScript and the local `@shunter/client` runtime package are the
  v1 client path
- SQL support is scoped to the documented v1 read surfaces; Shunter does not
  promise broad SQL database compatibility
- performance rows are advisory unless a release process defines hard
  thresholds for a specific workload
- lower-level runtime packages remain implementation details except for the
  documented stable subsets used by root APIs, contracts, protocol rows, BSATN,
  and generated clients

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
rtk go build -ldflags "-X github.com/ponchione/shunter.Version=v1.0.0 -X github.com/ponchione/shunter.Commit=<git-sha> -X github.com/ponchione/shunter.Date=<utc-rfc3339>" ./cmd/shunter
```

Use `vX.Y.Z` git tags for releases. `Module.Version(...)` is separate: it is
application module metadata exported into `ModuleContract` artifacts, not the
Shunter tool/runtime release version.

## Repository Guide

After this overview, use:

1. [docs/README.md](docs/README.md) - guide to the app-author docs tree
2. [docs/getting-started.md](docs/getting-started.md) - first-pass
   app-author path for embedding Shunter
3. [docs/concepts.md](docs/concepts.md) - vocabulary for modules, runtimes,
   reducers, procedures, reads, contracts, protocol serving, and durable state
4. [docs/how-to/README.md](docs/how-to/README.md) - task-focused integration
   guides
5. [docs/reference/README.md](docs/reference/README.md) - compact API
   decision notes

Contributor planning notes live under `working-docs/`. Use
`working-docs/README.md`, `working-docs/shunter-design-decisions.md`, and the
narrow `working-docs/specs/` section only when implementation work needs that
context.

For implementation work, inspect the active packages directly:

- `schema/`
- `store/`
- `commitlog/`
- `executor/`
- `subscription/`
- `protocol/`
- root package files such as `module.go`, `runtime.go`, `lifecycle.go`,
  `network.go`, and `config.go`

Repository command conventions are in `RTK.md`; task-specific automation
instructions are in `AGENTS.md`.

App-author and operator docs live under `docs/`. Implementation planning,
baseline specs, audits, and backlog trackers live under `working-docs/`. Prefer
live code, tests, and the narrow spec section for the surface being touched.

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

The checked-in read-only GitHub Actions workflow and its exact local RTK command
mapping are documented in [Continuous integration](docs/continuous-integration.md).

## Source Provenance

Shunter is intended to be an independent clean-room implementation. The project
uses public documentation, behavior analysis, original specifications, tests,
and implementation audits as design inputs; it does not rely on source reuse
from external projects.
