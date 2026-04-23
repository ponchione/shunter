# APP-RUNTIME-LAYER-AND-USAGE-SURFACE

Status: exploratory design note
Scope: additive design document describing the missing coherent layer above the current Shunter engine/kernel so real applications can use Shunter through one hosted runtime model instead of hand-wiring the subsystem graph each time.

This document does not change the existing six core specs. It describes the practical runtime/application surface that would make the current engine actually usable across multiple projects.

Companion framing:
- `BRAIN-EXTENSIONS-LLM-HARNESS.md` describes one important app/product layer that could be built on top of Shunter.
- `GENERAL-PURPOSE-APP-PLATFORM-NOTES.md` describes the broader reusable platform/product surface Shunter may eventually want.
- This document sits between those two and the core engine specs: it describes the missing runtime/application layer that turns the kernel into something applications define against and clients connect to.

Owner-operator framing:
- This document assumes Shunter is primarily being built for the repo owner’s own Go projects.
- It therefore prioritizes a coherent private developer/runtime experience over broad-market productization.
- The goal is not “rebuild every part of SpacetimeDB immediately.”
- The goal is “make Shunter’s engine usable enough that real apps can be built on top of it and thereby harden the runtime with live workloads.”

---

## 1. Core conclusion

The current Shunter code/spec set is far enough along to justify an engine/kernel.
What is still missing is the coherent hosted app-facing runtime layer above that kernel.

Right now, Shunter mostly exists as:
- schema subsystem
- store subsystem
- executor subsystem
- commit-log/recovery subsystem
- subscription subsystem
- protocol subsystem
- one manual bootstrap example that wires them together

That is enough to prove the engine pieces are real.
It is not yet enough to make Shunter feel like a natural runtime/server that multiple apps define against.

So the next missing piece is not another app-specific product document.
It is a document that defines the runtime/application surface that sits above the kernel and below specific products.

In plain terms:
- the kernel already explains how Shunter works internally
- the brain doc explains one thing Shunter could power
- the general-purpose platform doc explains how Shunter could grow broadly
- this doc explains how applications actually use Shunter through one hosted runtime shape

---

## 2. The problem this document is solving

### 2.1 The current gap

Today, using Shunter requires understanding and wiring the engine graph directly:
- schema builder / registry
- recovery / committed state bootstrap
- durability worker
- reducer registry
- subscription manager
- executor
- fan-out worker
- protocol server
- HTTP server
- shutdown ordering

That is too low-level to be the normal app-author or operator experience.

Even if the engine is correct, an app author should not need to manually reason about:
- fan-out inbox channels
- durability adapters
- state adapters
- startup ordering
- recovery bootstrap sequencing
- protocol sender wiring
- lifecycle shutdown choreography

That is runtime ownership work.
That work should be hidden behind one coherent runtime surface.

### 2.2 Why this matters now

Without this layer, two bad things happen:

1. Every app invents its own bootstrap and lifecycle.
2. The engine cannot be stress-tested through real app use because the developer/runtime surface is too awkward.

That means Shunter stays trapped in an “engine pieces exist” state instead of becoming:
- a real runtime
- a real app foundation
- a real thing the owner can use across projects

### 2.3 Why this is not the same as the brain doc or platform doc

This document is narrower than `GENERAL-PURPOSE-APP-PLATFORM-NOTES.md` and more general than `BRAIN-EXTENSIONS-LLM-HARNESS.md`.

It is not about:
- document graphs
- embeddings
- search/indexing
- auth products
- reusable app modules beyond the runtime seam itself
- cloud hosting
- broad-market platform workflows

It is about the minimum hosted runtime/application surface that makes the engine usable in real projects.

---

## 3. The mental model Shunter needs

### 3.1 The kernel is not the same thing as the app-facing runtime

The current kernel is the machinery that makes a SpacetimeDB-like system possible.
It provides the hard internal properties:
- transactional reducer execution
- durable commit history
- recovery
- subscriptions
- push propagation
- protocol delivery

But app authors should not think in terms of those internals first.
They should think in terms of:
- my app schema
- my reducers / business rules
- my runtime config
- my connection surface
- my read/query surface
- my lifecycle hooks

So there needs to be a higher-level runtime that owns the kernel on behalf of applications.

### 3.2 The missing layer in one sentence

Shunter needs a hosted runtime/application layer that lets an app author define an app model and then start/stop/use it without hand-assembling the engine graph.

### 3.3 The simplest intended user experience

The target experience should feel more like:

1. define an app/module
2. configure runtime options
3. start the runtime/server
4. expose or use the network/client surfaces
5. write app code against stable runtime contracts

and less like:

1. manually open recovery state
2. manually start durability worker
3. manually build executor
4. manually wire subscription manager
5. manually build sender adapters
6. manually run protocol server
7. manually coordinate shutdown

---

## 4. Hosted-first: what this layer is and is not

### 4.1 Hosted-first meaning here

Hosted-first means:
- Shunter's primary identity is its own runtime/server
- applications define modules/app packages against that runtime
- clients connect to a canonical Shunter-hosted surface
- tooling, codegen, and operational workflows target one runtime model

It does **not** mean:
- hosted cloud service by default
- multi-tenant control plane
- mandatory WASM or multi-language runtime
- immediate full SpacetimeDB product parity

### 4.2 Why this matters

Hosted-first gives Shunter one canonical shape:
- one runtime bootstrap model
- one protocol/auth surface
- one CLI and codegen story
- one way app definitions plug in

That is a better fit for the real ambition than treating Shunter mainly as a library that each app wraps differently.

### 4.3 What remains out of scope

Hosted-first still does not require:
- cloud control-plane behavior
- multi-tenant server fleet concerns
- cross-language module packaging
- broad-market product completeness before the runtime is coherent

The immediate need is to make Shunter a coherent hosted runtime/server first.

---

## 5. The primary responsibility of the runtime layer

The runtime layer should own these concerns.

### 5.1 Runtime construction

It should take care of:
- schema registry freeze/build
- runtime config normalization
- data directory / persistence path handling
- first-boot bootstrap vs recovery reopen
- reducer registration / lifecycle registration
- wiring store, durability, executor, subscriptions, and protocol pieces together

### 5.2 Runtime lifecycle

It should own:
- startup ordering
- readiness state
- shutdown ordering
- context cancellation behavior
- cleanup/flush on exit
- future scheduler/lifecycle integration points

### 5.3 Runtime-facing app contract

It should present a stable app-author-facing contract for:
- declaring schema
- declaring reducers
- optional lifecycle hooks
- protocol/network enablement
- read/query access
- write/reducer invocation
- connection/subscription hooks

### 5.4 Operational defaults

It should carry sane defaults for:
- local data directory layout
- durability queue sizing
- executor queue sizing
- protocol enablement
- auth mode defaults in local/dev mode
- local listen address defaults when protocol is enabled

### 5.5 Introspection/export

It should provide a stable way to introspect the app/runtime definition for later:
- generated client bindings
- schema export
- reducer export
- admin tooling
- diagnostics and “describe” style commands

---

## 6. What the app author should provide

The app author should provide only app-specific information.

### 6.1 Required inputs

At minimum:
- tables/schema definitions
- reducers / business rules
- runtime configuration
- any app-specific auth/identity policy hooks
- any app-specific external adapters

### 6.2 Optional inputs

Potentially optional:
- on-connect / on-disconnect logic
- custom query/read policies
- metrics/logging sinks
- custom serializer/codegen config

### 6.3 What the app author should not have to provide

They should not normally have to manually provide:
- a durability adapter
- a state adapter
- a fan-out inbox channel
- protocol sender plumbing
- explicit executor/subscription wiring
- bootstrap/recovery choreography

Those are runtime implementation details, not app-definition concerns.

---

## 7. What the runtime should expose back

The runtime should return one coherent handle/object that owns the running system.

### 7.1 Minimum capabilities of that handle

The handle should make these kinds of operations possible:
- start the runtime
- stop the runtime cleanly
- access runtime readiness/health state
- access local query/read helpers
- invoke reducers programmatically
- expose HTTP/WebSocket handler(s)
- expose metadata/schema export

### 7.2 Likely sub-surfaces on that handle

A practical runtime handle may expose logical sub-surfaces such as:
- `Runtime`
- `Runtime.HTTPHandler()`
- `Runtime.DB()` or `Runtime.ReadView()`
- `Runtime.CallReducer(...)`
- `Runtime.ExportSchema()`
- `Runtime.Close()`

The exact API can vary.
The important part is that the app or operator sees one stable owner object.

---

## 8. The app-definition surface Shunter likely needs

This is the part most analogous to SpacetimeDB’s module concept, but adapted for Go-native use.

### 8.1 Why a module-like concept still matters

If Shunter is going to be a hosted runtime, it needs a concept equivalent to:
- “this is the app’s schema and business logic package”

That concept is useful because it gives Shunter one stable thing to:
- build
- run
- inspect
- export
- generate clients from later

### 8.2 What a Go-native app/module definition should contain

At minimum:
- schema definitions
- reducer declarations
- lifecycle hook declarations
- version information
- app/runtime metadata

Potentially later:
- query/view declarations
- binding export metadata
- permissions/read-model declarations
- migration policy metadata

### 8.3 What it should not require

It should not require:
- WASM
- cross-language module packaging
- a separate host product before any app can run

Those are separate product choices.
The app/module concept can exist purely as a Go-native runtime definition.

---

## 9. Recommended layering model

The healthiest architecture still looks like this.

### Layer 1: Shunter kernel

Owns:
- schema primitives
- store
- executor
- commit log / recovery
- subscriptions
- base protocol

### Layer 2: hosted runtime layer

Owns:
- runtime construction
- lifecycle
- config
- bootstrap/recovery orchestration
- stable runtime handle
- network/protocol mounting
- introspection/export surface

### Layer 3: app definitions

Owns:
- app schema
- reducers
- lifecycle logic
- app-specific policies and external integrations

### Layer 4: adapters/products

Examples:
- Kickbrass web/mobile surface
- LLM brain MCP surface
- dashboards/admin tools
- CLI/admin commands
- generated clients

This is the key architectural move.
The missing piece is mostly Layer 2.

---

## 10. How this helps a public product app like Kickbrass

### 10.1 Current shape of the problem

A conventional backend tends to split:
- API routes
- service layer
- repository layer
- database
- realtime syncing layer

A Shunter-powered hosted runtime can collapse a large part of that glue by making reducers and subscriptions the primary state/update model.

### 10.2 What Shunter should replace

For a public product app, Shunter should be able to replace much of the glue around:
- core transactional state changes
- state synchronization
- live state propagation
- manual cache invalidation
- custom websocket fan-out logic

### 10.3 What still remains outside the runtime

Even with Shunter, a public app still needs:
- auth/identity integration
- billing/payments
- external APIs/webhooks
- file/blob handling
- email/notifications
- deployment and ops
- abuse/rate-limit controls

That is normal.
The runtime does not need to erase all backend code to be valuable.
It only needs to erase the awkward data/API/sync glue that currently exists by accident.

---

## 11. How this helps a brain/backend replacement case

### 11.1 Current shape of the problem

A brain system built around files + vault conventions + MCP tends to split:
- document storage
- retrieval/indexing
- provenance rules
- agent access layer
- state synchronization

### 11.2 What the runtime layer enables

A coherent hosted runtime lets a brain package be built as:
- a Shunter app definition
- plus a brain-specific schema/reducer layer
- plus an MCP adapter or direct tool adapter on top

### 11.3 Why this matters

That means Shunter becomes the durable, transactional substrate.
MCP becomes an access surface, not the underlying data model.

This is a cleaner long-term architecture than treating the vault itself as the main durable system forever.

---

## 12. The minimum deliverables this document should drive

If this document is useful, it should eventually lead to a concrete runtime/app usage design with at least these outputs.

### 12.1 A real runtime package

There should be a clear package and/or top-level runtime surface for operators and app authors.

It should not remain a repo where the main usable entrypoint is only a hand-wired example binary.

### 12.2 A stable runtime config surface

There should be one config object that actually controls runtime behavior.

It should cover at least:
- persistence path
- queue sizing
- protocol enablement
- auth mode
- listen settings
- logging/metrics hooks

### 12.3 A stable app-definition surface

There should be one obvious way to define:
- schema
- reducers
- lifecycle hooks
- version metadata

### 12.4 A stable runtime handle

There should be one obvious handle for:
- start/stop
- reducer calls
- read/query access
- network handlers
- export/introspection

### 12.5 A better hello-world story

A future hello-world should read like:
- define a table
- define a reducer
- start runtime
- connect client
- observe live state

It should not primarily read like:
- open recovery plan
- construct worker
- adapt tx ids
- build sender adapters
- hand-wire protocol graph

---

## 13. What should probably wait until after this layer exists

The following are valuable, but should not lead this work:
- fully productized CLI workflows
- broad codegen/client SDKs
- broad optional modules
- heavy auth/policy systems
- rich search/indexing products
- cloud/control-plane behavior
- multi-language module runtimes

Those become much easier to design correctly once the hosted runtime layer exists.
Without that layer, they risk being built on top of unstable seams.

---

## 14. Practical bottom line

The current Shunter engine is not the wrong idea.
It is just incomplete as a developer-facing/runtime-facing system.

The missing piece is not another app-specific feature layer first.
The missing piece is the coherent hosted runtime/application layer that lets real apps use the engine naturally.

So the right mental model is:
- Shunter kernel = the engine internals
- hosted runtime layer = the thing that makes the engine usable
- app definition = the schema/reducer package for a specific project
- app adapters = HTTP, WebSocket, MCP, CLI, generated clients, dashboards, and so on

If that layer exists, then Shunter can start making sense as:
- the state/runtime core for a public product like Kickbrass
- the durable memory substrate for a brain system
- a reusable runtime across multiple owner-operated apps

Without that layer, Shunter remains mostly a promising engine with awkward manual bring-up.

So this “final piece” is not optional polish.
It is the bridge between “kernel exists” and “apps can actually live on top of it.”
