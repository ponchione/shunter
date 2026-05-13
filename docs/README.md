# Shunter Documentation

This directory holds current user-facing documentation for application authors
and operators. Implementation plans, subsystem specs, audits, source-reading
notes, and future-work trackers live under `../working-docs/`.

## App-Author Learning Path

1. [Getting started](getting-started.md) gives the shortest path for embedding
   Shunter in a Go application.
2. [Concepts](concepts.md) defines the vocabulary used by the guides.
3. [How-to guides](how-to/README.md) cover specific integration tasks such as
   declaring modules, writing reducers, serving protocol traffic, configuring
   auth, persistence, contracts, generated TypeScript clients, and tests.
4. [Reference notes](reference/README.md) summarize config, lifecycle, and read
   surface choices that Go doc alone does not explain.

Use [authentication](authentication.md) for the full current auth contract,
[operations](operations.md) for backup/restore and release runbooks, and
[performance envelopes](performance-envelopes.md) for the current advisory
benchmark snapshot.

## Compatibility And Support

Shunter v1 is a Go-native hosted runtime with reducer-owned writes,
Shunter-native protocol frames, and contract-driven clients. It is not a
SpacetimeDB compatibility layer.

Stable v1 compatibility applies to the app-facing root package APIs for module
declaration, runtime lifecycle, local reducer calls, local and declared reads,
contract export and validation, build metadata, and health status
classification. It also applies to v1 `ModuleContract` JSON, the v1 WebSocket
wire contract, BSATN value/product-row encoding at runtime boundaries, and
generated TypeScript for valid v1 contracts.

Runtime diagnostics, observability hooks, offline operation helpers, migration
hooks, multi-module hosting, contract workflow helpers, and lower-level
protocol package helpers are advanced surfaces. They are usable, but app code
that needs normal v1 compatibility should prefer the root APIs, contract JSON,
generated clients, or documented protocol behavior.

Runtime implementation packages such as `store`, `subscription`, `executor`,
`commitlog`, `query/sql`, and `internal/*` are not app compatibility surfaces.
`reference/SpacetimeDB/` is research-only material.

## Current Docs

- [Getting started](getting-started.md) - app-author onboarding flow.
- [Concepts](concepts.md) - modules, runtimes, reducers, reads, contracts,
  protocol serving, durable state, and trust boundaries.
- [How-to guides](how-to/README.md) - task-focused integration guides.
- [Use generated TypeScript clients](how-to/typescript-client.md) - local
  `@shunter/client` installs, generated bindings, reducer/query/view helpers,
  managed subscriptions, and reconnect.
- [Reference notes](reference/README.md) - compact decision guides for exported
  app-facing surfaces.
- [Authentication](authentication.md) - dev/strict auth behavior, principals,
  permissions, visibility, key replacement, and production checklist.
- [Operations](operations.md) - `DataDir` lifecycle, backup/restore,
  migrations, upgrades, and release checklist.
- [Performance envelopes](performance-envelopes.md) - current advisory
  benchmark snapshot, workload fixtures, and known measurement gaps.

## Working Docs

Working docs are repository-internal implementation material. Consult them only
when an active task needs their contracts or when live code and Go doc do not
answer a dependency question.

- `../working-docs/specs/*/SPEC-*.md` - numbered subsystem implementation
  contracts.
- `../working-docs/specs/README.md` - scope note for the numbered subsystem
  contracts.
- `../working-docs/tech-debt.md` - non-blocking future work retired from stale
  release roadmaps.
- `../working-docs/shunter-design-decisions.md` - consolidated implementation
  decisions that code and tests still cite.

## Maintenance Notes

Prefer live code, tests, and Go doc over stale prose. If a user-facing doc stops
being current, fold the current contract into the smallest active doc or delete
the obsolete page. Do not keep history-only files in `docs/`.
