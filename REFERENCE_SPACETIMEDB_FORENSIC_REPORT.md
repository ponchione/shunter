# SpacetimeDB reference forensic report

Scope note: this report was produced by inspecting only `reference/SpacetimeDB` and not the live implementation outside `/reference`.

## Executive summary

SpacetimeDB appears to be a full application platform, not just a database, SDK, or demo project.

Its central architectural bet is:

- put state, application logic, and realtime synchronization inside one runtime,
- let clients connect directly to that runtime,
- and generate typed client surfaces around the server-side schema and logic.

At a high level, it looks like a vertically integrated realtime backend platform for stateful networked applications, especially:

- multiplayer games
- chat systems
- collaboration tools
- shared-state applications
- latency-sensitive apps where many clients need a coherent view of changing state

The repo contains evidence of all the layers needed to support that claim:

- a core runtime/database engine
- a standalone host
- client API/protocol handling
- subscriptions and incremental update machinery
- durability, commitlog, and snapshot systems
- codegen and bindings
- multi-language SDKs
- templates and demos
- CI/smoketests/benchmarks
- AI/LLM-oriented docs and benchmark tooling

My strongest conclusion is:

SpacetimeDB is trying to replace not just a database, but a whole class of backend architecture made of app server + database + websocket sync layer + hand-written client glue.

## Primary textual evidence

The clearest framing appears in `reference/SpacetimeDB/README.md`:

- `README.md:68-76` describes SpacetimeDB as “a relational database that is also a server” where application logic is uploaded into the database and clients connect without a server in between.
- `README.md:72-76` says server modules can be written in Rust, C#, TypeScript, or C++.
- `README.md:85` says application state is held in memory while a commit log on disk provides durability and crash recovery.
- `README.md:117-147` describes the basic programming model as tables + reducers + subscriptions, with clients receiving live updates automatically.
- `README.md:166-171` lists TypeScript, Rust, C#/Unity, and C++/Unreal as supported client surfaces.

The docs deepen that model:

- `docs/docs/00100-intro/00100-getting-started/00400-key-architecture.md`
- `docs/docs/00200-core-concepts/00400-subscriptions/00200-subscription-semantics.md`
- `docs/docs/00200-core-concepts/00600-clients/00200-codegen.md`

These files make clear that the intended mental model is:

- a host runs databases
- a database is an application
- application logic lives in a module
- tables store state
- reducers are transactional requests
- clients subscribe to state and maintain a local cache

## What the repo shape reveals

From `reference/SpacetimeDB/Cargo.toml`, the workspace includes substantial engine/product crates:

- `crates/core`
- `crates/standalone`
- `crates/client-api`
- `crates/commitlog`
- `crates/durability`
- `crates/execution`
- `crates/query`
- `crates/sql-parser`
- `crates/subscription`
- `crates/table`
- `crates/schema`
- `crates/cli`
- `crates/update`
- bindings/codegen crates and tools

The workspace default members are:

- `crates/cli`
- `crates/standalone`
- `crates/update`

That strongly suggests the main user-facing artifacts are:

- the CLI
- the standalone host/server
- the updater/installer path

A rough codebase inspection over `reference/SpacetimeDB` found about:

- 5,294 files
- ~297k LOC
- dominant languages: Rust, TypeScript, C#, TSX, C/C++

The top-level layout is also revealing:

- `crates/` — large engine/runtime surface
- `sdks/` — language/client integrations
- `templates/` — starter apps and framework-specific onboarding
- `tools/` — codegen, CI helpers, benchmark tooling, LLM tooling
- `docs/` — large versioned docs surface
- `demo/` — flagship samples
- `modules/` — benchmark/test/demo server modules

This is clearly a product repository, not a reference snippet dump.

## Runtime architecture: forensic read

### 1. Standalone is a local product host with a minimal control plane

From `reference/SpacetimeDB/crates/standalone/src/lib.rs` and `control_db.rs`, standalone appears to maintain local metadata/control state and launch module hosts.

Key evidence:

- `crates/standalone/src/lib.rs:48-57` defines a `StandaloneEnv` containing `ControlDb`, `DiskStorage`, `HostController`, and a client index.
- `crates/standalone/src/lib.rs:60-107` initializes:
  - a sled-backed control DB
  - disk-backed program storage
  - a local persistence provider
  - a `HostController`
- `crates/standalone/src/control_db.rs:19-27, 68-76` show `ControlDb` is sled-backed.
- `crates/standalone/src/control_db.rs:361-477` stores and manages database and replica metadata.

Important inference:

Standalone is not a distributed cluster control plane in the Kubernetes sense. It is a local metadata catalog + launcher with abstractions for databases/replicas/leaders.

The deeper code read suggests standalone forces single-replica semantics for publish/reset paths, so the “replica” abstraction is present but collapses to one local leader in standalone mode.

### 2. HostController is the runtime orchestrator

The real runtime center seems to be `crates/core/src/host/host_controller.rs`.

Evidence:

- `crates/core/src/host/host_controller.rs:90-120` shows `HostController` managing:
  - running hosts keyed by replica id
  - shared page pools
  - host runtimes
  - persistence provider
  - row-list builder pool
- the deeper forensic read found lazy launch/reuse behavior and watch channels for host swaps/exits.

Architectural inference:

This is not a simplistic “open DB and run handlers” server. It is a runtime manager that can:

- launch module hosts
- keep them keyed by replica identity
- swap/restart them
- separate persistent state from execution instances

### 3. Module instances appear disposable; persistent state is not

A key inference from the code pass:

- module instances are runtime shells around persistent DB state
- persistent truth seems to live in relational state + commitlog + snapshots
- module processes/instances are recreated on launch/update/recovery

This is a major sign of architectural maturity. It means execution containers are replaceable while the state model remains authoritative.

## Execution/runtime strategy

### 4. Multi-runtime module hosting

The forensic pass found evidence that the host supports more than one execution substrate.

From the deeper read:

- `crates/core/src/host/module_host.rs` wraps either Wasm or JS execution paths
- `crates/core/src/host/host_controller.rs` includes runtime structures for Wasmtime and V8

Architectural inference:

SpacetimeDB is trying to preserve one logical module model while allowing multiple authoring/runtime paths.

This is consistent with the public language support story:

- Rust
- C#
- TypeScript
- C++

In practice, the docs describe modules as WebAssembly modules or JavaScript bundles behind a standard ABI:

- `docs/docs/00100-intro/00100-getting-started/00400-key-architecture.md:20-22`

That suggests the repo is implementing a unified platform contract over heterogeneous execution backends.

## Transaction model

### 5. Reducers are the transactional write boundary

The docs are explicit:

- `key-architecture.md:200-205` says reducers run as atomic database transactions
- successful reducers commit changes
- failed reducers revert all changes

This is important because it means the application model is centered on:

- exported transactional functions over relational state

not on:

- a generic HTTP controller layer talking to a separate DB

Architectural implication:

SpacetimeDB treats “application logic” as database-native transactional logic that clients call directly.

That is one of the repo’s core differentiators.

## Durability and recovery

### 6. Commit and durability are distinct phases

The README gives the broad claim, but the deeper code pass shows more nuance.

The runtime appears to distinguish between:

- in-memory committed state
- durably confirmed state

The deeper forensic pass found evidence in `crates/core/src/db/relational_db.rs` and related durability code that:

- datastore commit happens first
- durability work is enqueued asynchronously
- snapshots are also backgrounded

Architectural inference:

This is a write-behind durability model, not a naive synchronous fsync-on-everything path.

That in turn explains why the platform exposes stronger “confirmed reads” semantics for some client-visible paths.

### 7. Recovery is bounded by durable frontier, not just latest snapshot

The deeper pass found evidence that on recovery the system:

- restores from a snapshot no newer than the durable transaction offset
- then replays commitlog history from there

That is a good sign. It suggests snapshots are acceleration artifacts, while durability truth is anchored by durable offset + commitlog replay.

This is much more sophisticated than just “dump memory to disk occasionally.”

### 8. There is serious storage engineering here

The presence of:

- `crates/commitlog`
- `crates/durability`
- `crates/snapshot`
- background durability workers
- snapshot workers
- replay logic
- snapshot-driven compaction/compression behavior

strongly suggests SpacetimeDB is implementing real database durability and recovery concerns, not merely wrapping another engine.

## Subscription engine: likely the deepest differentiator

### 9. Subscriptions are not naive pubsub

The docs describe subscriptions as maintaining a consistent client cache, but the code-level read makes the mechanism more interesting.

The deeper pass found evidence of:

- subscription/query deduplication across subscribers
- indexing subscriptions by tables/join/search characteristics
- delta evaluation against changed rows
- incremental update logic
- send workers preserving ordering
- careful lock ordering to avoid duplicate/racy updates

Architectural inference:

This is closer to an incremental query maintenance engine than to ordinary “broadcast events when something changes.”

### 10. Client cache consistency is a first-class goal

`reference/SpacetimeDB/docs/docs/00200-core-concepts/00400-subscriptions/00200-subscription-semantics.md` is one of the most revealing documents in the repo.

It says the system guarantees:

- sequential response ordering (`18-24`)
- atomic transaction updates (`25-27`)
- atomic subscription initialization from a consistent snapshot (`28-31`)
- client cache updates happen atomically before callbacks observe them (`69-92`)

This is exactly the kind of semantics application developers struggle to build manually.

My strongest inference is:

The true “killer feature” of this platform is not merely reducers or embedded business logic. It is coherent transactional shared-state replication to clients.

That matters enormously for:

- games
- chat
- collaborative apps
- presence systems
- shared-world/stateful UI applications

## Client protocol and transport

### 11. There is a real versioned protocol stack

The repo contains both HTTP and WebSocket transport layers.

The deeper forensic read found:

- HTTP endpoints for database interactions, reducer calls, SQL, schema, etc.
- WebSocket subscription endpoints for long-lived realtime connections
- code paths for multiple websocket protocol variants, including binary forms

One notable forensic finding:

- the docs mention older/v1 websocket protocol framing
- code apparently negotiates additional/versioned binary protocols beyond what the docs emphasize

Inference:

The wire protocol is important enough to version explicitly, and docs are trailing implementation somewhat.

That is a sign of an actively evolving platform.

### 12. Identity is built into the connection model

The deeper read found that the websocket connection bootstrap sends identity/token context up front.

That fits the broader platform model:

- clients are durable actors with identities
- reducers see caller identity in context
- connection lifecycle is part of the server model

This is again a strong fit for multiplayer/collaboration systems rather than plain request/response CRUD.

## Code generation and bindings

### 13. Codegen is central, not optional

The docs for codegen are explicit:

- `docs/docs/00200-core-concepts/00600-clients/00200-codegen.md:10-21`
- bindings generate:
  - types matching tables and schema
  - callable reducers/procedures
  - query interfaces
  - callbacks

The generation commands support:

- TypeScript
- C#
- Rust
- Unreal C++

Evidence:

- `codegen.md:23-86`

Architectural inference:

The intended source of truth is the server module schema and API surface.
Clients are supposed to consume generated typed projections of that surface.

That gives the platform:

- strong type safety
- less handwritten API glue
- fewer drift bugs
- consistent client ergonomics across ecosystems

### 14. The TypeScript SDK is especially broad

`reference/SpacetimeDB/sdks/typescript/package.json` is very revealing.

It exports multiple framework-specific entrypoints:

- `.`
- `./sdk`
- `./react`
- `./server`
- `./vue`
- `./tanstack`
- `./svelte`
- `./angular`

Peer deps include:

- React
- Vue
- Svelte
- Angular
- TanStack Query

Inference:

TypeScript is clearly the widest top-of-funnel ecosystem for the product, and the team has invested in meeting frontend developers where they already are.

## SDK and ecosystem strategy

### 15. The platform has one server model and many client “skins”

Evidence across:

- `sdks/rust/Cargo.toml`
- `sdks/csharp/README.md`
- `sdks/unreal/README.md`
- `README.md:166-171`

The strategy appears to be:

- preserve one conceptual protocol and schema model
- project it into many client ecosystems using SDKs + codegen

That is much stronger than just “we have ports in many languages.”

### 16. Game support is strategic, not incidental

The Unreal SDK README says the plugin exposes:

- connections
- reducer calls
- synchronized cache
- generated message/type headers
- Blueprint wrappers

Evidence:

- `reference/SpacetimeDB/sdks/unreal/README.md:1-17`

The Blackholio demo reinforces this with:

- Unity client
- Unreal client
- Rust server module
- C# server module

Evidence:

- `reference/SpacetimeDB/demo/Blackholio/README.md:63-72`

This is not the behavior of a project that merely “also supports games.”
It suggests multiplayer/game workloads are part of the platform identity.

## Templates and onboarding

### 17. Templates are a major product surface

The repo contains many templates:

- `basic-ts`
- `basic-rs`
- `basic-cs`
- `basic-cpp`
- `browser-ts`
- `nodejs-ts`
- `bun-ts`
- `deno-ts`
- `react-ts`
- `nextjs-ts`
- `remix-ts`
- `tanstack-ts`
- `vue-ts`
- `nuxt-ts`
- `svelte-ts`
- `angular-ts`
- `chat-react-ts`
- `chat-console-rs`
- `chat-console-cs`
- `keynote-2`

This is unusually broad for a database project.

The template tooling also matters:

- `reference/SpacetimeDB/tools/templates/README.md` describes generation/sync of template docs and metadata from quickstarts.

Inference:

Templates are not just examples. They are part of:

- the onboarding funnel
- the documentation system
- the product packaging/distribution strategy

### 18. The onboarding funnel is tightly choreographed

The intended flow is very clear from the README and `spacetime dev` docs:

- install the CLI
- login
- run `spacetime dev --template ...`
- generate bindings
- edit schema/reducers/client
- auto rebuild/republish on save
- inspect/debug with CLI calls, SQL, logs

Evidence:

- `README.md:89-113`
- `docs/docs/00200-core-concepts/00100-databases/00200-spacetime-dev.md`

Inference:

The product is highly optimized for “time to first working app,” not for making users assemble infrastructure by hand.

## Benchmarks and competitive posture

### 19. The benchmark suite shows what SpacetimeDB thinks it is competing against

The `templates/keynote-2` benchmark suite compares SpacetimeDB against:

- SQLite + RPC server
- Postgres-based stacks
- Supabase
- CockroachDB
- PlanetScale
- Convex

Evidence:

- `reference/SpacetimeDB/templates/keynote-2/README.md`

The argument is explicitly architectural:

- traditional stack: client → server → ORM → DB
- SpacetimeDB: client → integrated platform (compute + storage colocated)

Inference:

SpacetimeDB sees itself as competing against an entire backend architecture pattern, not just against another SQL engine.

### 20. Performance is part of the product story

There is significant benchmark and perf tooling:

- benchmark modules
- keynote benchmark suite
- perf scripts
- workflows for benchmark PR reporting

This suggests the team cares not just about correctness and DX but about proving the integrated architecture yields concrete throughput/latency benefits.

## AI/LLM-facing productization

### 21. This repo is unusually AI-aware

The repo includes:

- `docs/static/llms.md`
- AI rules files
- SpacetimeDB-specific “skills” docs
- `tools/xtask-llm-benchmark`
- `tools/llm-oneshot`
- workflow automation for LLM benchmark updates

That is a lot of AI-facing infrastructure for a database/runtime project.

Inference:

The team appears to believe that:

- AI-assisted development is strategically important,
- docs/templates/rules should be machine-usable,
- and “can an LLM generate a correct app on this platform?” is a measurable product quality.

This is a notably modern and somewhat unusual product posture.

## What kind of applications this is best for

Based on the docs, code, demos, and protocol/subscription design, SpacetimeDB seems best suited for systems where the hard problem is:

not merely storing rows,
but maintaining a coherent shared world across many clients.

That shared world could be:

- a game world
- a chat room system
- a collaborative canvas/editor
- a live social/presence graph
- a stateful operational UI
- a realtime coordination app

The repo’s architecture makes much more sense in that domain than it would for:

- analytics
- warehousing
- offline batch jobs
- plain line-of-business CRUD with minimal realtime needs

## Why this architecture is useful

Traditional systems often require a developer to assemble and maintain:

- a database
- an application server
- websocket/pubsub infrastructure
- client sync logic
- cache invalidation/refetch logic
- authentication glue
- generated or hand-written client types

SpacetimeDB appears to unify a large part of that into one platform.

Main benefits implied by the repo:

### A. Less backend glue

By colocating compute and storage, it can remove a major source of complexity and latency.

### B. Realtime as a first-class primitive

Subscriptions and synchronized client cache semantics are designed into the platform, not bolted on later.

### C. Stronger client consistency model

The subscription semantics aim to prevent clients from seeing inconsistent intermediate state.

### D. Faster onboarding

Templates, codegen, SDKs, and CLI workflows are all aimed at shrinking setup time.

### E. Better fit for games/collab/shared-state apps

This is where the integrated state-sync approach has the clearest advantage.

## Strongest architectural inference

The strongest refined conclusion from this forensic pass is:

SpacetimeDB’s deepest differentiator is not merely “database + server-side functions.”
It is transactional shared-state replication with generated client APIs and strong client-cache consistency semantics.

That is what ties together:

- reducers
- subscriptions
- codegen
- SDKs
- protocol design
- demos
- benchmark positioning

## Product priorities inferred

My best ranking of the repo’s priorities is:

1. eliminate backend glue between app logic and data
2. make realtime shared state feel native and coherent
3. reduce time-to-first-working-app
4. preserve transactional correctness and crash recovery
5. support many client ecosystems without fragmenting the model
6. prove performance through architectural benchmarks
7. establish credibility for multiplayer/game workloads
8. become easy for AI-assisted coding systems to use correctly

## Notable inconsistencies and seams

A few useful forensic seams showed up:

### 1. Docs appear to lag runtime/protocol evolution in places

The deeper pass found places where docs emphasize older/v1 protocol or runtime framing while code suggests newer/expanded support.

Interpretation:

- this is a living platform under active evolution
- docs are substantial but not a perfect mirror of latest implementation

### 2. `spacetime dev` messaging appears somewhat mixed

Some docs/readme language blend local-dev and hosted/maincloud behavior in a way that suggests either product evolution or documentation drift.

### 3. Some template docs look fresher than others

The newer autogenerated quickstart-oriented template docs appear more consistent than some older template READMEs.

None of this undermines the central reading, but it does suggest an actively changing product surface.

## Bottom line

This reference codebase is the source tree of a serious integrated realtime backend platform.

Its architectural center is:

- transactional relational state
- server-side application modules
- direct client connections
- live subscriptions with coherent cache semantics
- generated client bindings across many ecosystems

It is trying to replace a whole backend stack pattern, not just supply a database or SDK.

If I had to summarize the whole thing in one sentence:

SpacetimeDB is a vertically integrated runtime for stateful networked applications whose most distinctive idea is coherent transactional state replication to clients, wrapped in a productized CLI/template/SDK/codegen workflow.
