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
3. Type-system vertical slices driven by application schemas.
4. Storage, recovery, and subscription performance hardening.
5. Client SDK ergonomics, including a possible React SDK track.

## Client SDK Ergonomics

Keep this track open.

Current direction:

- Continue generating client-facing artifacts from `ModuleContract`.
- Keep the first client surface small and contract-driven.
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

The generic `shunter` CLI should not pretend it can load arbitrary app modules
unless Shunter gains a real module loading boundary.

Open direction:

- add reusable helper APIs only when repeated app-owned workflows make the
  stable boundary clear
- keep module-specific operations in app-owned binaries until Shunter has a
  real module loading boundary

## Query And Declared Reads

This is the next major capability track.

Direction:

- Make one-off reads richer first.
- Keep declared query support aligned with one-off reads once the execution path
  is proven.
- Grow live views/subscriptions more carefully because incremental deltas over
  joins, aggregates, ordering, and limits carry higher correctness risk.

Likely feature slices:

- broader index-aware planning for remaining live subscription candidate
  pruning and complex live join paths
- live-view expansion for currently query-only shapes after delta semantics are
  explicit

Completed slices:

- unfiltered live equi-join candidate pruning uses indexed join-existence
  edges, including same-transaction opposite-side deletes
- live join delta committed-probe fallbacks use per-transaction delta indexes
  when the committed join side is unindexed
- nullable-aware aggregate semantics for one-off and declared queries

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

- arrays beyond `[]string` when an app needs them
- app-level enums with a simple exported representation

## Migrations

Migration behavior should remain explicit and reviewable. Normal runtime
startup should not silently rewrite durable state.

Recommended sequence:

1. Continue refining the existing migration-runner ergonomics once real
   app-owned binaries show repeated migration workflows.

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
