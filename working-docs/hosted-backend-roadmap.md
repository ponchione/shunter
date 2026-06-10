# Hosted Backend Direction

Status: current product direction
Scope: Shunter as a Go-first self-hosted backend/database runtime.

Shunter's current product direction is a static Go app server that application
frontends talk to over the Shunter protocol. App state, reducers, declared
reads, live views, subscriptions, procedures, durability, recovery, health, and
diagnostics are Shunter-owned backend responsibilities.

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
- hosted-chat release gate covering the common static hosted-app workflow.

## Non-Goals

Current non-goals:

- managed cloud hosting, billing, organization, or project ownership surfaces.
- dynamic arbitrary code upload as the first hosted model.
- Rust, C#, C++, Unreal, WASM, or other module language support.
- multi-language client SDK parity beyond TypeScript.
- reference-runtime protocol, storage, client, or source compatibility.
- PostgreSQL compatibility, PGWire, SQL DML, or broad SQL database behavior.
- app-facing blob/object storage; keep large bytes in object storage and keep
  transactional metadata in Shunter tables.

## Near-Term Focus

Keep current work tied to real hosted-app pressure:

- product-app validation through Kickbrass, without adding artificial product
  features only to exercise Shunter.
- private/local `@shunter/client` package workflow in release gates, followed
  by a separate public npm promotion slice once package ownership, publish
  authority, access policy, package metadata, and artifact policy are settled.
- scaffolded hosted app template tooling if the documented hosted-chat
  template shape and browser/SSR lifecycle guidance are not enough.
- workload-derived performance and operability evidence from real apps and
  external canaries.
- targeted hardening for hosted surfaces already implemented: procedures,
  event tables, maintained live windows, auth/JWKS, migration reports, and
  generated TypeScript clients.

## Remaining Work Trackers

Use focused trackers instead of growing this direction note:

- `deferred-functionality-backlog.md` owns explicitly deferred product/runtime
  scope such as dynamic serving, broad SQL, online backup orchestration,
  cross-table visibility, richer schema types, and codegen breadth.
- `tech-debt.md` owns non-blocking productization, canary, performance, and
  hardening follow-up.
- `release-qualification.md` owns release gate command sets and evidence.
