# Hosted Backend Direction

Status: current active-development direction
Scope: Shunter as an experimental Go-first self-hosted backend/database
runtime.

No real application is currently selected. The canonical hosted-chat example
is the repository-local integration fixture, not a product choice.

Shunter is an actively developed experimental backend/runtime. Its current
architectural direction is a static Go app server that application frontends
talk to over the Shunter protocol. App state, reducers, declared reads, live
views, subscriptions, procedures, durability, recovery, health, and diagnostics
are Shunter-owned backend responsibilities.

```text
Frontend / TypeScript client
  -> @shunter/client runtime
  -> generated app bindings from a ModuleContract
  -> static Go Shunter app server
      -> Go module tables, reducers, procedures, reads, views, subscriptions
      -> durable state, recovery, snapshots, compaction
      -> optional app-owned service integrations
```

## Product Boundary

The supported hosted-app shape is a normal Go binary that links the app module
and Shunter runtime:

```go
cfg := shunter.ConfigFromEnv()
if err := shunter.Run(ctx, app.Module(), cfg); err != nil {
	log.Fatal(err)
}
```

This static server model is intentional. It keeps module authoring idiomatic
Go while hardening the user-facing backend contract first: protocol serving,
TypeScript bindings, auth, migrations, durability, backup, observability, and
deployment.

Dynamic module publish/load, managed control-plane behavior, and arbitrary
code upload remain deferred until static app servers plus a public TypeScript
SDK prove insufficient.

## Current Baseline

Implemented hosted-app surfaces:

- `shunter.Run`, `ConfigFromEnv`, `Runtime.HTTPHandler`, graceful shutdown, and
  mounted health/readiness/diagnostics handlers.
- app-owned contract export through `Runtime.ExportContract()` /
  `Runtime.ExportContractJSON()`.
- `shunter contract codegen --language typescript`.
- running-app `shunter call`, `shunter procedure`, `shunter query`,
  `shunter query --sql`, `shunter describe`, and `shunter health`.
- offline `shunter backup` and `shunter restore`.
- procedures for external-service workflows outside the serialized reducer
  executor.
- event tables for transient subscription-visible facts.
- declared queries, declared views, maintained single-table ordered/windowed
  live views, and bounded multi-way join guardrails.
- strict auth with local HS256/RS256/ES256 keys and configured JWKS/OIDC issuer
  verification.
- hosted-app DataDir compatibility reports, safe additive recovery, and
  app-owned migration hooks.
- private package-shaped `@shunter/client` runtime and generated module-client
  helpers for reducers, procedures, declared reads/views, table subscriptions,
  and event-table streams.
- hosted-chat integration gate covering the common static hosted-app workflow.
- app-owned offline maintenance preparation and a deterministic hosted-chat
  snapshot, covered-compaction, backup, restore, preflight, and recovered-state
  drill.
- bounded TypeScript reconnect with explicit connection epochs, subscription
  replay completion, non-authoritative resynchronizing handles, and
  unknown-outcome errors for interrupted reducer/procedure calls.

## Non-Goals

Current non-goals:

- managed cloud hosting, billing, organization, or project ownership surfaces.
- dynamic arbitrary code upload as the first hosted model.
- Rust, C#, C++, Unreal, WASM, or other module language support.
- multi-language client SDK parity beyond TypeScript.
- reference-runtime protocol, storage, client, or source compatibility.
- PostgreSQL compatibility, PGWire, SQL DML, or broad SQL database behavior.
- distributed transactions or multi-region operation.
- app-facing blob/object storage; keep large bytes in object storage and keep
  transactional metadata in Shunter tables.

## Active Development Priorities

Select work from concrete evidence or an explicit goal, not from a release or
productization sequence. Current priorities are:

- explicit feature requests tied to an application or user goal
- correctness bugs and reproducible failures
- simplification and maintainability of the existing implementation
- focused hardening of behavior touched by an active change
- targeted performance work supported by profiles or benchmarks

Validate each change proportionally: start with the touched packages and
expand only when risk, dependencies, or the affected external canary surface
justify it. No specific feature is selected as the next implementation task by
this direction note.

## Dormant Until Triggered

Keep production operating envelopes, public npm distribution, extensive
release qualification, canary-wide release gates, and release preparation
dormant until a concrete product, integration, distribution, or explicitly
authorized release decision requires them. Their presence in supporting
trackers is not a promise of production readiness or an instruction to begin
productization.

## Remaining Work Trackers

Use focused trackers instead of growing this direction note:

- `recommendations/` owns optional, trigger-driven proposals until a concrete
  user goal, failure, code evidence, integration pressure, or authorized
  distribution/release decision promotes one into active work.
- `deferred-functionality-backlog.md` owns explicitly deferred product/runtime
  scope such as dynamic serving, broad SQL, online backup orchestration,
  cross-table visibility, richer schema types, and codegen breadth.
- `tech-debt.md` owns non-blocking productization, canary, performance, and
  hardening follow-up.
- `release-qualification.md` preserves historical records and owns on-demand
  release gate command sets and evidence for explicitly authorized release
  work.
