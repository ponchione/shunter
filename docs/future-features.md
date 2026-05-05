# Future Feature Tracks

Status: working list
Scope: future Shunter-native feature tracks to revisit as real applications put
pressure on the runtime.

This document is not a SpacetimeDB parity checklist. SpacetimeDB remains useful
as a reference for runtime/product lessons, but Shunter owns its protocol,
module model, storage contracts, and developer workflow.

## Near-Term Priority Order

1. Richer query and declared-read foundation.
2. App-owned CLI/runtime helpers.
3. Migration preflight and app-owned migration hooks.
4. Type-system vertical slices driven by application schemas.
5. Storage, recovery, and subscription performance hardening.
6. Client SDK ergonomics, including a possible React SDK track.

## Client SDK Ergonomics

Keep this track open.

Current direction:

- Continue generating client-facing artifacts from `ModuleContract`.
- Keep the first client surface small and contract-driven.
- Build SDK layers on the generated TypeScript table, reducer, and executable
  declared-read name unions rather than raw string callback names.
- Consider a React SDK once enough projects repeat the same table subscription,
  reducer-call, connection-state, and cache patterns.
- Avoid owning a broad framework/template ecosystem before the reusable client
  shape is visible from real Shunter apps.

Potential React SDK responsibilities:

- connection lifecycle state
- typed reducer calls using generated reducer-name unions
- typed declared query/view helpers using generated executable-name unions
- table/view cache updates from Shunter protocol messages
- subscription cleanup on component unmount
- stable handling of reconnect and protocol-version mismatch

## Module Hosting

The current Shunter identity is Go-native, statically linked, app-owned
runtime. Wasmtime, V8, dynamic module upload, and multi-language hosted modules
are outside the current product shape.

Keep the root `shunter.Module` and `shunter.Runtime` surfaces as the normal
application boundary. Revisit process or plugin boundaries only as a Shunter
runtime isolation problem, not as a SpacetimeDB compatibility goal.

## CLI And App Workflow

Expand workflow support through app-owned binaries and reusable library helpers.

Current generic CLI and helper boundary:

- contract diff
- contract policy
- contract plan, including backup/restore guidance for blocking or data-rewrite changes
- contract codegen from existing JSON
- offline `DataDir` compatibility preflight through `shunter.CheckDataDirCompatibility`
- offline executable `DataDir` migrations through `shunter.RunDataDirMigrations`
- app-owned startup migration hooks through `Module.MigrationHook`
- offline `DataDir` backup through `shunter.BackupDataDir` and the generic CLI
- offline `DataDir` restore through `shunter.RestoreDataDir` and the generic CLI

The generic `shunter` CLI should not pretend it can load arbitrary app modules
unless Shunter gains a real module loading boundary.

## Query And Declared Reads

This is the next major capability track.

Direction:

- Make one-off reads richer first.
- Keep one-off single-table `ORDER BY <column> ASC/DESC` eligible for matching
  single-column indexes while rechecking filters and visibility before
  `OFFSET`/`LIMIT`/projection.
- Make declared queries richer after the execution model is clear.
- Grow live views/subscriptions more carefully because incremental deltas over
  joins, aggregates, ordering, and limits carry higher correctness risk.

Likely feature slices:

- stronger parser/planner boundary
- richer projections and aliases
- multi-join support
- nullable-value semantics for aggregates once nullable types exist
- `OFFSET` for additional result shapes where snapshot/live-view semantics are
  explicit
- broader index-aware planning for joins, multi-column ordering, reverse-cursor
  descending scans, and live paths
- clear interaction with read policy and visibility filters

Any query expansion must include tests for authorization, visibility filtering,
subscription deltas, and contract/codegen export where applicable.

## Type System

Do not copy a full SATS-style type universe as one large project. Add types as
vertical slices across the runtime.

Each new type should cover:

- `types.Value`
- schema registration and reflection helpers
- BSATN encoding/decoding
- store validation and indexing behavior
- SQL literal coercion where relevant
- schema and contract export
- contract diff behavior
- codegen output
- migration compatibility rules

Likely useful types:

- nullable/optional values with explicit wire semantics
- arrays beyond `[]string` when an app needs them
- app-level enums with a simple exported representation

## Migrations

The current contract diff and metadata tooling gives useful visibility, but
runtime migration execution is a separate feature track.

Current dry-run contract planning emits backup/restore guidance when blocking
or data-rewrite changes should be reviewed before touching a durable `DataDir`.
Startup snapshot selection now reports every detected table/column/index schema
mismatch from the selected snapshot in one strict startup failure.
App-owned binaries can preflight a stopped or missing `DataDir` against a
module schema with `shunter.CheckDataDirCompatibility`. App-owned binaries can
run explicit stopped-DataDir migrations with `shunter.RunDataDirMigrations`.
They can also register `Module.MigrationHook` callbacks that run during
`Runtime.Start` after recovery and durability are available, and before normal
runtime readiness. Hooks must be idempotent because a failed later startup step
or process restart may run them again.

Recommended sequence:

1. Migration-runner ergonomics once real app-owned binaries show repeated
   migration workflows.

Migration behavior should be explicit and reviewable. Normal runtime startup
should not silently rewrite durable state.

## Storage, Recovery, And Performance

Shunter already has real store, commitlog, snapshot, recovery, compaction, and
subscription machinery. As more projects use it, this track becomes operational
hardening work.

Near-term work:

- keep reducer/query/subscription benchmarks current as workloads evolve
- stress large tables, large rows, many clients, many subscriptions, and restart
  recovery
- extend storage fault coverage around runtime shutdown

Storage architecture changes should be driven by measured bottlenecks from
Shunter workloads.
