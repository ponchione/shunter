# Shunter Concepts

Status: rough draft
Scope: vocabulary and mental model for app authors.

This page explains the core nouns used by Shunter docs. It is not a subsystem
spec and does not replace Go doc for method signatures.

## Module

A module is the application definition that Shunter builds into a runtime. It
contains the app-owned schema, reducers, lifecycle hooks, declared queries,
declared views, visibility filters, permission metadata, migration metadata,
and module identity.

Application code creates a module with `shunter.NewModule(name)` and registers
declarations through fluent methods such as `TableDef`, `Reducer`, `Query`,
`View`, and `VisibilityFilter`.

`Module.Version(...)` is app module metadata exported into contracts. It is not
the Shunter runtime or CLI version.

## Runtime

A runtime is the built, stateful owner of one module. It owns committed state,
durable logging, recovery, reducer execution, subscriptions, local reads,
protocol serving, health, diagnostics, and contract export.

Use `shunter.Build(mod, cfg)` to construct a runtime, then `Runtime.Start` or
`Runtime.ListenAndServe` to make it active.

After `Build`, mutating the original `Module` value does not change the built
runtime.

## Reducer

A reducer is the synchronous write boundary. Reducers run on Shunter's
serialized executor path against a transaction-scoped reducer database.

Reducers should mutate Shunter state only through `ctx.DB`, avoid retaining the
context after return, and avoid long-running external work while holding the
executor.

Reducer arguments and results are raw byte slices. Encoding is an application
choice.

## Table

A table is declared with `schema.TableDefinition`. Columns use Shunter value
kinds from `types`, and primary-key columns synthesize a unique primary-key
index.

Secondary indexes are part of the schema contract. Add them for access paths
that reducers, reads, subscriptions, joins, and visibility filters depend on.

## Value And Product Row

`types.Value` is Shunter's tagged value type. A row is represented as
`types.ProductValue`, which is a positional row of `types.Value` entries.

Column order in the table declaration determines row value order at the
runtime boundary.

## Declared Query

A declared query is a named request/response read surface registered with
`Module.Query`. It can include SQL, permissions, read-model metadata, and
migration metadata.

Use `Runtime.CallQuery` for local declared-query execution. Protocol clients
can call the same declared read through the protocol path.

Empty SQL makes the declaration metadata-only and not executable.

## Declared View

A declared view is a named live read surface registered with `Module.View`. It
is used for local initial subscription admission through `Runtime.SubscribeView`
and for protocol live views.

Declared views can include SQL, permissions, read-model metadata, and migration
metadata. Empty SQL makes the declaration metadata-only and not executable.

## Visibility Filter

A visibility filter is row-level SQL attached to the module. It narrows rows
before read evaluation or live delivery for caller-specific access.

The current stable visibility parameter is `:sender`, derived from caller
identity.

## DataDir

`Config.DataDir` is the runtime-owned directory for snapshots, commit log
segments, and recovery metadata.

Use one data directory for one module schema line. Do not edit files inside it,
merge backup contents into it, or run two runtimes against the same directory.

## Contract

A module contract is exported JSON describing the app-facing shape of a
runtime: schema, reducers, queries, views, visibility filters, permissions,
read-model metadata, migration metadata, and codegen metadata.

Contract JSON is the right artifact for review, compatibility checks, codegen,
and client handoff.

## Protocol

The protocol path is Shunter's WebSocket interface for external clients. It is
enabled with `Config.EnableProtocol` and mounted by `Runtime.HTTPHandler()` or
served by `Runtime.ListenAndServe`.

The v1 protocol is Shunter-native. It is not a promise of compatibility with
another runtime's wire protocol.

## Host

A `Host` composes multiple already-built runtimes under explicit route prefixes.
It does not merge their schemas, data directories, transactions, reducers, or
contracts.

Most applications should start with one runtime per app module. Multi-module
hosting is an advanced composition surface.

## What Shunter Is Not

Shunter v1 is not a managed cloud service, distributed database, broad SQL
database, process sandbox, dynamic module loader, or multi-language module
host. App code runs inside the same Go process as the runtime.
