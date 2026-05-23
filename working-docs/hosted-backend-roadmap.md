# Hosted Backend Roadmap

Status: planning direction
Scope: Shunter's path from embedded Go runtime toward a Go-first
backend/database product.

This document captures the current product direction and capability gap
analysis after comparing Shunter's implemented scope with the reference
SpaceTimeDB product shape under Shunter's constraints:

- Shunter is Go-first.
- Shunter module logic is written in Go.
- Frontend clients use generated TypeScript and the Shunter TypeScript SDK.
- Multi-language module hosting and multi-language client parity are not goals.
- Reference-runtime wire compatibility, byte-for-byte storage compatibility,
  and source compatibility are not goals.
- The reference tree is research-only. Do not copy source, comments, tests,
  identifiers, or structure from `reference/`.

## Direction

The target product direction is:

```text
Frontend / TypeScript client
  -> generated Shunter TypeScript SDK
  -> Shunter backend runtime
      -> Go module tables, reducers, reads, views, subscriptions
      -> durable state, recovery, snapshots, compaction
      -> optional app-owned service integrations
```

Shunter should become the backend/database runtime for an application, not just
a lower-level embedded library used by a traditional backend.

This does not require cloning the reference managed-cloud business model. It
does require making Shunter feel like the app's primary backend boundary:

- app state lives in Shunter tables
- writes happen through reducers or a future procedure-like surface
- frontend reads use generated TypeScript helpers and live subscriptions
- durability, recovery, backup, health, and operational flows are runtime
  responsibilities
- the surrounding Go code becomes a thin application shell and service-adapter
  layer

## Product Boundary

The immediate target is a static Go app server powered by Shunter.

```go
package main

import (
	"context"
	"log"

	"github.com/ponchione/shunter"
	"example.com/myapp/app"
)

func main() {
	cfg := shunter.ConfigFromEnv()
	if err := shunter.Run(context.Background(), app.Module(), cfg); err != nil {
		log.Fatal(err)
	}
}
```

The exact API names above are illustrative. The important boundary is that
each Shunter app builds a normal Go binary that links:

- the app's Go module declaration
- Shunter runtime/server support
- optional service adapters such as geocoding, email, payments, search, or
  object storage

This is hosted from the developer and frontend perspective: the frontend talks
to Shunter, not to hand-written CRUD handlers. Internally, Go still uses static
linking because that is the most reliable Go-native module-loading model.

## Non-Goals

These reference capabilities are not gaps for Shunter's current direction:

- Rust, C#, C++, Unreal, or other module language support.
- Rust, C#, C++, Unreal, or other client SDK parity.
- Managed cloud account, billing, organization, team, and project ownership
  surfaces.
- Public package/template ecosystem breadth beyond Go apps and TypeScript
  frontend clients.
- Reference protocol or storage compatibility.
- PostgreSQL compatibility as a product goal.
- Dynamic arbitrary code upload as the first hosted model.

## Hosted Does Not Mean Dynamic Publish First

The hardest part of a fully SpaceTimeDB-like host is dynamic module loading and
publishing. For Go, the options are all expensive:

| Model | Benefits | Costs |
| --- | --- | --- |
| Static Go binary per app | Idiomatic, simple, portable, easy to deploy | No in-process dynamic publish |
| Go plugins | Dynamic loading without separate process | Fragile across platforms/toolchains |
| Out-of-process Go module runner | Better isolation, possible dynamic replacement | Protocol, lifecycle, and transaction complexity |
| WASM module hosting | Closer to reference host model | Large scope and weakens Go-only simplicity |

Shunter should start with static Go binaries. A `publish`-like workflow can
come later if static rebuild/redeploy proves too limiting.

## What SpaceTimeDB Provides And Why It Exists

The reference product provides more than a runtime. Its major surfaces exist
for specific reasons. Shunter should selectively adopt only the parts that
serve the Go-first backend/database goal.

### Standalone Server

Reference purpose:
- Run a database host as its own process.
- Accept client WebSocket/API traffic without a separate app backend.
- Own database lifecycle, logs, schema inspection, publish/update, and
  operational routes.

Shunter decision:
- Pursue a hosted server boundary, but begin with static app binaries.
- Add a standard server entrypoint and app layout before attempting a generic
  daemon.

Needed Shunter work:
- `shunter.Run` or equivalent convenience API.
- environment/config loading helpers.
- production server template.
- health, readiness, diagnostics, metrics, and graceful shutdown conventions.
- route layout for protocol, diagnostics, generated-contract access, and
  optional management endpoints.

Example shape:

```text
GET  /healthz
GET  /readyz
GET  /diagnostics/runtime
GET  /contract.json
GET  /subscribe or /v1/subscribe
POST /call/<reducer>        optional management/dev API
POST /query/<declared-read> optional management/dev API
```

### CLI Developer Workflow

Reference purpose:
- Create projects.
- Build modules.
- Publish/update databases.
- Generate client code.
- Call reducers.
- Run SQL.
- View logs.
- Start local servers.

Shunter decision:
- Keep CLI scope focused on static Go apps and TypeScript frontends.
- Do not implement reference-like dynamic publish until hosted app binaries are
  mature.

Needed Shunter work:
- `shunter init` or documented project scaffold.
- `shunter run` / `shunter start` for generated app servers or local dev.
- `shunter contract export`.
- `shunter contract codegen --language typescript`.
- `shunter call` against a running Shunter app for dev/admin.
- `shunter query` for declared queries or narrow SQL reads.
- `shunter describe`.
- `shunter backup` and `shunter restore`.
- `shunter health`.

Example app workflow:

```bash
shunter init --template nuxt-go my-app
cd my-app
shunter dev
shunter contract codegen --language typescript --out frontend/lib/shunter.ts
shunter call create_user '{"name":"Ada"}'
shunter query active_users '{}'
```

The command names are illustrative. The priority is a repeatable workflow, not
exact command parity.

### Database Publish And Update

Reference purpose:
- Upload a compiled module to a host.
- Create or update a named database.
- Run migration compatibility checks.
- Maintain or break active clients according to update policy.

Shunter decision:
- Do not start with dynamic upload.
- Treat rebuild/redeploy of the static Go app binary as the update mechanism.
- Still implement strong data-dir compatibility and migration workflows.

Needed Shunter work:
- clearer DataDir/module identity checks.
- preflight migration command for app binaries.
- machine-readable migration/compatibility reports.
- documented deploy sequence:
  1. stop admitting traffic
  2. wait for important transactions to become durable
  3. close runtime
  4. backup
  5. run migration hooks/preflight
  6. start new binary
- optional app-owned data rewrite hooks.
- explicit client compatibility policy for contract changes.

Avoid for now:
- hot module replacement.
- dynamic schema mutation while the runtime is serving.
- generic database create/delete/list control plane.

### Reducers

Reference purpose:
- Transactional write API.
- Server-side app logic.
- Rollback on failure.
- Client-callable mutation surface.

Shunter status:
- This is already a core Shunter strength.

Needed Shunter work:
- continue hardening reducer error classes and generated TypeScript call
  ergonomics.
- keep reducers as the primary mutation path.
- avoid SQL DML as a competing write model unless a strong operator use case
  appears.

Example intended use:

```go
mod.Reducer("create_place", func(ctx shunter.ReducerContext, args []byte) ([]byte, error) {
	req := decodeCreatePlace(args)
	ctx.DB.Insert("place", shunter.ProductValue{
		shunter.String(req.ID),
		shunter.String(req.Name),
		shunter.Float64(req.Lat),
		shunter.Float64(req.Lng),
	})
	return nil, nil
})
```

### Procedures Or Service Adapters

Reference purpose:
- Allow external I/O such as HTTP calls.
- Keep reducers deterministic and transaction-focused.
- Let procedure code manually enter transactions when it needs state changes.

Shunter decision:
- This is relevant if Shunter is the backend.
- Because Shunter is Go-hosted, the app shell can already call external
  services. A first-class procedure-like surface should be added only when the
  frontend needs to call external-service workflows through Shunter's auth,
  permission, and generated SDK surfaces.

Needed Shunter work:
- define whether procedures are client-callable named functions.
- decide transaction model:
  - no automatic transaction
  - optional `ctx.Read` and `ctx.CallReducer`
  - explicit `ctx.Transaction` if direct writes are allowed
- define scheduler interaction.
- define protocol and TypeScript result shape.
- define timeout, cancellation, and observability behavior.

Preferred initial design:
- procedures may call external services
- procedures do not hold the reducer executor while doing external I/O
- procedures write state by calling reducers or by opening a short explicit
  transaction
- procedure failures return only to the caller, unless they explicitly write
  state that triggers subscriptions

Example geocoding workflow:

```text
Frontend calls geocode_place
  -> procedure checks auth and validates request
  -> procedure calls MapBox outside a Shunter transaction
  -> procedure calls reducer store_geocode_result
  -> reducer commits place coordinates
  -> subscriptions update frontend
```

Illustrative API:

```go
mod.Procedure("geocode_place", func(ctx shunter.ProcedureContext, args []byte) ([]byte, error) {
	req := decodeGeocodeRequest(args)
	result, err := ctx.Services().Mapbox.Geocode(ctx.Context(), req.Address)
	if err != nil {
		return nil, err
	}
	return ctx.CallReducer("store_geocode_result", encode(result))
})
```

### Event Tables

Reference purpose:
- Broadcast transient transaction-scoped facts without storing them as durable
  table state.
- Let clients observe "something happened" without keeping a permanent row.
- Useful for notifications, combat effects, one-shot UI events, diagnostics,
  and ephemeral messages.

Shunter decision:
- Strong candidate for Shunter's backend/database direction.
- This fits TypeScript frontends and realtime apps better than forcing every
  transient event into a persistent table plus cleanup reducer.

Needed Shunter work:
- schema support for event table declarations.
- store changeset representation for event inserts that do not merge into
  committed state.
- commitlog decision:
  - record event rows as transaction history, or
  - treat them as non-replayable delivery-only rows.
- subscription semantics:
  - event rows fire insert callbacks
  - no persistent cache membership
  - no update/delete callbacks
  - visibility filters apply before delivery
- TypeScript generated event callbacks.
- recovery behavior and tests.

Example:

```go
mod.EventTable("notification", schema.TableDefinition{
	Columns: []schema.ColumnDefinition{
		{Name: "recipient", Kind: types.ValueKindIdentity},
		{Name: "message", Kind: types.ValueKindString},
		{Name: "severity", Kind: types.ValueKindString},
	},
})

mod.Reducer("send_notification", func(ctx shunter.ReducerContext, args []byte) ([]byte, error) {
	ctx.DB.InsertEvent("notification", row)
	return nil, nil
})
```

Frontend shape:

```ts
client.db.notification.onInsert((_ctx, row) => {
  toast(row.message)
})
```

### Views And Live Reads

Reference purpose:
- Expose read-only derived data.
- Provide access-control-friendly read surfaces.
- Let clients subscribe to computed data, not only base tables.

Shunter status:
- Declared queries and declared views exist.
- Single-table `ORDER BY`, `LIMIT`, and `OFFSET` currently shape initial
  snapshots only for non-aggregate live views.

Needed Shunter work:
- maintained ordered/windowed live views for common frontend use cases:
  - top N leaderboard
  - newest activity feed
  - nearest or highest-priority tasks
  - paged dashboards
- delta semantics for membership changes:
  - row leaves top N: delete old emitted row
  - row enters top N: insert new emitted row
  - row changes order but remains in window: update or delete+insert according
    to current wire shape
- admission limits and index requirements.
- TypeScript SDK local cache behavior for ordered/windowed views.

Example:

```sql
SELECT id, name, score
FROM player
ORDER BY score DESC
LIMIT 10
```

Expected live behavior:
- if player 11 gains enough score to become rank 8, the previous rank 10 row
  is removed and player 11 is inserted.
- if a top-10 player's score changes but remains in top 10, frontend receives
  a coherent replacement.

### SQL And Admin Queries

Reference purpose:
- Developer/operator inspection.
- CLI `sql`.
- HTTP SQL endpoint.
- PGWire tooling compatibility.
- DML/admin workflows.

Shunter decision:
- Narrow SQL reads are useful.
- SQL DML should remain out of scope until there is a specific operator need.
- Reducers should remain the write boundary.
- PGWire is not a priority.

Needed Shunter work:
- improve declared-query and one-off read ergonomics.
- allow owner/admin local query bypasses where explicit.
- add CLI/dev HTTP support for:
  - declared query calls
  - narrow read SQL
  - schema/contract inspection
- keep DML rejected unless the mutation model is revisited.

Rationale:
- SQL writes bypass reducer-owned invariants.
- SQL DML complicates subscription, durability, permissions, and app logic.
- Most frontend workflows should use generated declared reads/views.

### Auth And Identity

Reference purpose:
- Public internet database access.
- OIDC-compatible identity.
- Managed or third-party auth.
- Identity available to reducers, views, procedures, RLS.

Shunter status:
- strict JWT verification exists for configured local keys.
- dev anonymous auth exists.

Needed Shunter work:
- JWKS/OIDC key discovery.
- key rotation cache.
- issuer and audience validation hardening.
- richer `AuthPrincipal` if real apps need claims beyond the normalized
  identity/permissions surface.
- generated TypeScript token-provider ergonomics for browser/Nuxt apps.
- clear production auth guide.

Example config direction:

```go
cfg.AuthMode = shunter.AuthModeStrict
cfg.AuthOIDCIssuers = []shunter.OIDCIssuer{
	{
		Issuer:   "https://issuer.example",
		JWKSURL:  "https://issuer.example/.well-known/jwks.json",
		Audience: "my-app",
	},
}
```

### Visibility And Row-Level Security

Reference purpose:
- Restrict which rows a connected client can read or subscribe to.
- Let visibility depend on caller identity and related tables.
- Prevent leaks through joins and subscriptions.

Shunter status:
- row-level visibility filters exist.
- current stable scope is single-table and row-local.

Needed Shunter work:
- keep view-based access control as the preferred first answer.
- consider cross-table visibility only when real app workflows cannot be
  expressed with declared views and single-table filters.
- if adding planner-level cross-table visibility:
  - cycle detection
  - alias handling
  - recursive filter composition
  - join leak prevention
  - subscription hash identity with caller-specific visibility
  - benchmark and admission limits

Example use case:

```text
client may see project rows where project_member.project_id = project.id
and project_member.identity = :sender
```

If this becomes common, the Shunter implementation should first ask whether a
declared view can expose the safe result:

```sql
SELECT p.*
FROM project p
JOIN project_member m ON m.project_id = p.id
WHERE m.identity = :sender
```

Only add recursive RLS if table-level raw subscriptions must support the same
policy automatically.

### Schema Types

Reference purpose:
- Represent rich module types across languages and clients.
- Encode nested products, sums, arrays, options, identities, connection IDs,
  timestamps, durations, and wide integers.

Shunter decision:
- Shunter does not need the full reference algebraic type system for
  multi-language portability.
- Shunter does need enough schema richness for Go app state and TypeScript
  frontends.

Priority order:

1. identity and connection ID column kinds if app schemas need them.
2. timestamp and duration column kinds with clear encoding and codegen.
3. byte arrays / homogeneous arrays for bounded frontend payloads.
4. simple nested products if flat rows become awkward.
5. simple enums/sums if TypeScript discriminated unions materially improve app
   contracts.
6. recursive/general algebraic type system only with strong evidence.

Avoid:
- type-system expansion that is not reflected cleanly in contracts, BSATN,
  store/index behavior, SQL coercion, subscription hashing, and TypeScript
  codegen.

### Migrations

Reference purpose:
- Let developers update module schema while preserving data.
- Classify safe, breaking, and forbidden changes.
- Support incremental app-managed migrations for hard changes.

Shunter status:
- data-dir compatibility checks and migration hooks exist.
- contract diff/workflow helpers exist.
- current durable schema policy is exact/fail-fast in many places.

Needed Shunter work:
- explicit compatibility matrix for hosted Shunter apps.
- additive migration support where safe:
  - add table
  - add index
  - add nullable/defaulted column, if supported
  - add reducer/query/view
- blocking diagnostics for unsafe changes:
  - drop table
  - reorder column
  - change column type
  - add unique/primary constraint over existing data without validation
- app-owned rewrite hooks with backup/restore guidance.
- generated client compatibility notes.

Example staged migration pattern:

```text
v1: table place(id, address)
v2: add table place_v2(id, address, lat, lng)
v2 reducers dual-write to place and place_v2
client migrates to place_v2 declared views
v3 removes old place only after explicit backup and compatibility break
```

### Operations

Reference purpose:
- Make the database host operable in production.
- Provide backup, restore, logs, metrics, health, config, and crash recovery.

Shunter status:
- backup/restore helpers exist.
- snapshots/compaction exist.
- health, diagnostics, metrics, and observability hooks exist.

Needed Shunter work:
- hosted app runbook.
- online/coordinated backup story:
  - stop or drain writes
  - create snapshot
  - wait durable
  - copy DataDir
  - resume writes
- CLI/server endpoints for:
  - health
  - ready
  - backup status
  - runtime diagnostics
  - version/build info
- log policy for reducer/procedure/server events.
- production config reference for hosted Shunter app servers.

### TypeScript SDK

Reference purpose:
- Let the frontend treat the database as the backend.
- Maintain a local cache of subscribed rows.
- Expose typed reducers, reads, subscriptions, callbacks, reconnect, and auth.

Shunter status:
- TypeScript runtime and generated helpers exist.

Needed Shunter work:
- Nuxt/browser-focused integration guide.
- local cache ergonomics for table and view subscriptions.
- generated reducer helpers with typed args/results.
- generated declared query/view helpers with typed params/results.
- generated event table callbacks if event tables are added.
- reconnect and token-provider behavior hardened for real frontend apps.
- SSR guidance:
  - do not open browser WebSockets during server render
  - support explicit client-only connection lifecycle
  - document server-side admin/query use separately if needed

Example frontend target:

```ts
const client = createMyAppClient({
  url: runtimeConfig.public.shunterUrl,
  tokenProvider: () => auth.getToken(),
})

await client.connect()

const places = client.tables.place.subscribe({
  where: { owner: currentIdentity },
})

await client.reducers.createPlace({
  name: 'Library',
  address: '10 Main St',
})
```

## Implementation Phases

### Phase 1: Hosted App Shape

Goal: make Shunter feel like a backend runtime without changing module loading.

Deliverables:
- `shunter.Run` or equivalent app-server convenience API.
- config-from-env helper or documented pattern.
- canonical app layout.
- in-repo example app with Go module and Nuxt/TypeScript client path.
- app-author docs for "Shunter as your backend".
- contract/codegen workflow documented end-to-end.
- release gate that exercises the example app.

Acceptance criteria:
- a new app can start from the template/example and run without bespoke runtime
  wiring.
- frontend can call reducers and subscribe through generated TypeScript.
- app can shut down cleanly and recover durable state.

### Phase 2: Developer CLI And Admin Surface

Goal: replace hand-written app/debug endpoints with common Shunter tooling.

Deliverables:
- CLI `describe` against local contract or running app.
- CLI `call` against running app.
- CLI `query` for declared queries and narrow read SQL.
- CLI `health`.
- CLI `backup` and `restore` retained and documented for hosted apps.
- optional HTTP management endpoints behind explicit enablement.

Acceptance criteria:
- normal development does not require custom admin handlers.
- CLI commands work against the example app.

### Phase 3: Backend Completeness

Goal: fill the largest product-functionality gaps for "Shunter is the
backend/db".

Deliverables:
- event tables or equivalent transient event surface.
- procedure/service-adapter design and first implementation.
- maintained ordered/windowed live views.
- production auth/JWKS support.
- improved migration compatibility reports.
- TypeScript SDK improvements driven by the example app.

Acceptance criteria:
- apps can model persistent state, transient events, external-service
  workflows, and common frontend live views without falling back to custom API
  handlers for core state.

### Phase 4: Hosted Runtime Expansion

Goal: decide whether static app binaries are enough or whether a stronger
hosted platform is needed.

Possible deliverables:
- static multi-module hosting improvements.
- dev watcher that rebuilds/restarts app server and regenerates TypeScript.
- database naming inside a single app server.
- optional publish-like workflow for static deploys.
- out-of-process module boundary if isolation or dynamic replacement becomes
  necessary.

Explicit decision point:
- Do not implement dynamic arbitrary Go module loading until static hosted
  apps have proven insufficient.

## Replacement Strategy For Go Backend Plus Postgres Apps

Shunter should be able to replace most of a conventional Go backend plus
Postgres/Supabase stack for interactive app state.

Good Shunter-owned data:
- user-visible app state
- collaborative state
- live map objects and pins
- notifications and feeds
- tasks and queues
- game/session state
- frontend-synchronized state
- reducer-owned business invariants

Keep outside Shunter unless a concrete app says otherwise:
- large object/blob storage
- analytics and BI history
- search indexing
- billing/payment ledger integrations
- external audit exports
- third-party sync logs
- workloads needing arbitrary ad hoc SQL or mature relational tooling

Example architecture:

```text
Nuxt frontend
  -> Shunter TypeScript SDK for app state
  -> optional upload endpoint for object storage

Go Shunter app server
  -> Shunter runtime for tables/reducers/views/subscriptions
  -> MapBox service adapter for geocoding
  -> email/payment/search adapters as needed
  -> optional Postgres/warehouse only for non-realtime support data
```

## Capability Priority

High priority:
- hosted app server shape
- TypeScript frontend ergonomics
- event tables
- procedure/service-adapter surface
- maintained ordered/windowed views
- auth/JWKS
- migration reports and app-owned migration hooks

Medium priority:
- richer schema types
- cross-table visibility through declared views first
- CLI dev/admin commands
- online/coordinated backup
- read-only system catalog for diagnostics

Low priority:
- PGWire
- SQL DML
- dynamic publish/update
- transactional catalog and online DDL
- out-of-process modules
- managed cloud control-plane concepts

## Design Guardrails

- Reducers remain the primary mutation boundary.
- External I/O must not run while holding the serialized reducer executor.
- Hosted Shunter apps should be deployable as normal Go binaries.
- TypeScript SDK and contract behavior should drive frontend ergonomics.
- Every new app-facing capability needs:
  - root API or generated-client surface
  - contract representation if clients need it
  - protocol representation if external clients need it
  - durability/recovery semantics if it touches state
  - tests at package and hosted-runtime levels
- Do not add a SpaceTimeDB feature just because the reference has it. Add it
  only when it strengthens Shunter as a Go-first backend/database runtime.
