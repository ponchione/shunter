# Shunter v1 Roadmap Docs

Status: active planning backlog
Scope: focused implementation tracks required before cutting a real `v1.0.0`.

These docs turn the current v1 gap analysis into agent-sized work areas. They
are not a SpacetimeDB parity checklist. Use `reference/SpacetimeDB/` only as a
research source for product and semantic lessons; keep Shunter's Go-native
module model, native protocol, and reducer-owned write path as the baseline.

## How To Use These Docs

Each file is meant to be an entry point for one focused workstream:

1. Read `RTK.md`.
2. Read this index.
3. Read only the roadmap file for the workstream being assigned.
4. Read package docs, code, tests, and narrow specs for the surface being
   changed.
5. Update the roadmap file when a decision becomes settled or a slice lands.

Do not treat any roadmap item as permission to broaden scope into unrelated
features. Each workstream should produce code, tests, docs, or a concrete
decision that can be reviewed independently.

## Roadmap Files

- [01-v1-contract.md](01-v1-contract.md) - freeze the supported v1 API,
  protocol, contract, and SQL/read surfaces.
- [02-reference-application.md](02-reference-application.md) - build and
  maintain a realistic end-to-end Shunter application.
- [03-client-sdk.md](03-client-sdk.md) - ship a production-credible TypeScript
  client experience on top of generated contracts.
- [04-production-auth.md](04-production-auth.md) - turn the current auth base
  into a documented strict production mode.
- [05-operations.md](05-operations.md) - define backup, restore, migration,
  upgrade, and operator workflows.
- [06-runtime-hardening.md](06-runtime-hardening.md) - prove correctness across
  recovery, concurrency, fuzzing, visibility, and protocol scenarios.
- [07-performance-envelopes.md](07-performance-envelopes.md) - publish and
  enforce realistic v1 workload limits.
- [08-process-isolation.md](08-process-isolation.md) - decide and document
  Shunter's in-process trust model and future isolation boundary.
- [09-sql-read-scope.md](09-sql-read-scope.md) - define the SQL/query depth
  Shunter actually needs for v1.
- [10-v1-execution-plan.md](10-v1-execution-plan.md) - ordered execution plan
  for finishing the remaining v1 workstreams.

## Current Reality Check

Several original roadmap gaps now have partial implementation:

- `docs/v1-compatibility.md` is the current v1 support matrix.
- Offline backup/restore helpers, CLI commands, migration hooks, snapshot, and
  compaction helpers exist, but the operator runbook and upgrade policy are not
  complete.
- Runtime gauntlet, recovery, storage-fault, fuzz, and benchmark coverage exist,
  but the release qualification command set and published performance envelopes
  are not complete.
- TypeScript codegen exists. A maintained TypeScript client runtime/package does
  not.
- A release-candidate app workload exists in tests. A maintained in-repo
  reference app for users does not.

## v1 Product Thesis

Shunter v1 should be excellent at one job:

> Self-hosted Go applications with reducer-owned writes, durable state, typed
> clients, permission-aware reads, and reliable live updates.

Features outside that thesis should be deferred unless they directly support
the v1 contract. In particular, v1 should not chase SpacetimeDB wire
compatibility, cloud hosting, multi-language module upload, broad SQL database
compatibility, or a full framework/template ecosystem.
