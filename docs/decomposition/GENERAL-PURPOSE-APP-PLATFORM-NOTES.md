# GENERAL-PURPOSE-APP-PLATFORM-NOTES

Status: exploratory design note
Scope: additive product/platform notes describing what Shunter would still need, beyond the current kernel specs, to support broader general-purpose application development in the way SpacetimeDB is marketed.

This document is a companion to `BRAIN-EXTENSIONS-LLM-HARNESS.md`.

Owner-operator framing:
- These notes should be read through the lens of a private, owner-operated platform for the repo owner’s own projects/businesses.
- “General-purpose” here means reusable across the owner's own app ideas, not “public competitor to SpacetimeDB” or “broad-market database product.”
- That makes selective parity, opinionated defaults, and workload-specific narrowing acceptable as long as the kernel/layering stays clean.

---

## 1. Core conclusion

The current Shunter decomposition/spec set describes a strong kernel, not yet a full general-purpose application platform.

That kernel is already meaningful:

- in-memory relational state
- reducer-based transactional writes
- commit log + snapshots
- subscription evaluation and fan-out
- websocket protocol
- schema definition

But SpacetimeDB's marketed generality comes from more than that kernel. It comes from the kernel plus product/platform layers that make many kinds of applications ergonomic to build.

So the practical design choice for Shunter is:

1. make the kernel excellent and stable first,
2. then add reusable platform layers,
3. then build project/app-specific layers on top of those.

---

## 2. What is already good enough in the current kernel

The current specs are already a credible foundation for:

- embedded realtime stateful backends
- reducer-driven application logic
- push-synchronized clients
- single-node personal/internal apps
- higher-level systems built on top of a stable transactional core

The kernel is particularly strong at:

- write serialization and correctness
- state-change capture via changesets
- recovery model
- subscription-driven propagation

These are the hard parts that many app layers want to inherit rather than reinvent.

---

## 3. What is still missing for a broader “general-purpose” app platform

To move from “kernel” to “general-purpose platform,” Shunter likely needs a platform layer similar in role to the extra product surface around SpacetimeDB.

### 3.1 Richer read/query surface

The current specs are strongest on:
- reducers
- subscriptions
- constrained point/standing-query workflows

General-purpose apps need more expressive read behavior:

- filtering
- sorting
- pagination
- ad hoc queries
- better one-off read APIs
- query ergonomics that do not require every app author to compose low-level protocol/state machinery manually

Without this, many ordinary app screens become clumsy:
- admin lists
- filtered tables
- reports
- search views
- timelines
- dashboards

A platform layer should likely provide:
- a richer one-off read API
- consistent filter/order/paging contracts
- clear distinction between standing subscriptions and ad hoc reads

### 3.2 Broader type/model support

The current schema/type model is intentionally narrow. That is good for v1 kernel clarity, but many general apps eventually want:

- nullable/optional values
- richer structured fields
- nested object-ish payloads
- collections/arrays in some controlled form
- blobs/attachments or references to them

General-purpose use does not necessarily require full SpacetimeDB type parity immediately, but it usually needs more than a flat scalar-only model.

### 3.3 Better developer-facing application API

A kernel can be powerful yet still awkward to use. A general-purpose platform needs stronger ergonomics.

That usually means:

- code generation or typed bindings
- stable client/server contracts
- helper APIs for common patterns
- easy startup/config/bootstrap paths
- reusable abstractions around reducers/subscriptions/queries

Without this, every new project has to write too much glue.

### 3.4 Operational product surface

General-purpose application developers need operational behaviors, not just engine behaviors.

Likely required:

- authentication and authorization surface
- migrations/versioning story
- backup/export/import
- observability and debugging support
- lifecycle and deployment ergonomics
- error surfaces that are friendly to app developers, not only engine implementers

For personal projects, these can be lightweight at first, but they still need to exist.

### 3.5 Reusable platform modules

The biggest difference between a kernel and a general-purpose platform is usually the presence of reusable higher-level modules.

Examples of reusable modules Shunter may eventually want:

- auth/identity module
- richer query/read module
- full-text search / indexing module
- background jobs / scheduling module
- codegen/bindings module
- common entity/task/audit/history modules
- file/blob attachment module
- permissions/policy module

These modules are what make the same kernel feel “general-purpose” across many kinds of apps.

---

## 4. Why kernel-first is still the better approach

The current docs are strongest at the kernel level. Trying to bake every application concern into the kernel too early would likely make the core system worse.

### 4.1 Why kernels generalize better

A good kernel provides:

- strong invariants
- stable state and transaction semantics
- composable primitives
- low-level correctness that multiple products can trust

That is reusable across:

- agent brains
- task/project backends
- internal tools
- collaborative apps
- game-adjacent realtime systems

### 4.2 Why product layers should sit above the kernel

Many higher-level concerns are highly app-dependent:

- what “search” means
- what “documents” mean
- what “permissions” mean
- what “sessions/history” mean
- how much structure vs freeform content is needed
- what indexing/ranking rules matter

If these are forced into the kernel too soon, the kernel stops being a clean foundation and becomes a half-opinionated app platform that is hard to reuse.

### 4.3 The practical layering model

A clean layering approach would be:

#### Layer 1: Shunter kernel

- schema
- store
- executor
- commit log / recovery
- subscriptions
- base protocol

#### Layer 2: reusable platform services

- auth / identity
- richer read/query APIs
- search/indexing
- jobs/background work
- codegen/client bindings
- attachment/blob support
- common audit/history patterns

#### Layer 3: app-specific products

- LLM brain
- task/project system
- collaboration app
- internal realtime dashboards
- domain-specific tools

This is likely the healthiest architecture.

---

## 5. What “general-purpose” really means in a SpacetimeDB-like system

This is the key conceptual point.

A system like SpacetimeDB is not “general-purpose” in the same sense as:

- PostgreSQL
- SQLite
- MySQL

Those are broad-purpose databases where the database itself is the main reusable product.

A SpacetimeDB-like system is “general-purpose” in a different sense:

- it provides a general runtime model for many classes of stateful applications
- it gives you a reusable pattern for logic + data + realtime sync
- it is general across app categories, not general across every database workload shape

That is a different kind of generality.

### 5.1 What kind of generality is being offered

The real general-purpose claim is something like:

“Use the same runtime architecture to build many different realtime/stateful applications.”

Examples:
- multiplayer games
- chat systems
- collaborative tools
- dashboards with live state
- social/activity systems
- agent memory backends

The kernel plus platform layers make that possible.

### 5.2 What that does NOT mean

It does not necessarily mean:

- universal SQL breadth
- analytics/warehouse excellence
- maximum flexibility for every storage model
- zero need for higher-level app/framework layers

So there is no contradiction in saying:

- the system is general-purpose for many app types,
- while still needing platform layers on top of the kernel.

That is how many real platforms work.

---

## 6. Why SpacetimeDB can market itself as broadly as it does

Based on the reference audit, SpacetimeDB’s breadth comes from multiple stacked capabilities, not just from the bare engine.

Those stacked capabilities include:

- engine kernel
- module runtime
- client protocol
- SDKs
- codegen
- templates/examples
- docs/tutorials
- demos/benchmarks
- productized workflows for web, game, and other client ecosystems

That combined stack lets them say “games to web apps to LLM stuff,” because they are not only selling a storage engine; they are selling a reusable application model plus surrounding tooling.

So if Shunter wants a similar breadth of practical applicability, it likely needs:

- at least the kernel,
- plus a meaningful portion of reusable Layer 2,
- plus targeted Layer 3 solutions for specific app classes.

---

## 7. Direct answer to the architectural question

### Question

If we add layers on top of this thing like SpacetimeDB has, how is that general-purpose?

### Answer

Because “general-purpose” in this model means:

- one shared engine/runtime foundation,
- plus reusable platform layers,
- used to build many different app types.

It does **not** mean “the bare kernel alone should directly satisfy every application need.”

That is normal.

A good platform is often:
- narrow at the kernel layer,
- broader at the platform-services layer,
- highly specialized at the product layer.

That still counts as general-purpose if many different applications can be built cleanly from the same shared core and platform services.

---

## 8. Do we need to get through Layer 2 to become meaningfully general-purpose?

Probably yes.

Direct answer:

- Layer 1 alone gives you a strong kernel.
- Layer 1 alone does **not** yet give you a broad, comfortable general-purpose app platform.
- Layer 2 is the point where the system starts to feel general-purpose for real projects.

Why Layer 2 matters:

It is where you add the reusable app-building conveniences and subsystems that many projects share.

Without Layer 2:
- every new app rebuilds the same glue
- ergonomics remain too low
- “general-purpose” remains mostly theoretical

With Layer 2:
- multiple app types can share the same higher-level services
- app development gets much faster
- the platform starts to look genuinely reusable rather than merely technically flexible

So the likely milestone progression is:

1. Kernel complete and stable
2. Layer 2 platform services added
3. Shunter becomes meaningfully general-purpose for personal/internal app work
4. Layer 3 product packages emerge for specific domains

---

## 9. Recommended product framing for Shunter

If this direction is pursued, the most accurate framing would be:

### Near-term framing

“Shunter is an embeddable realtime application kernel for stateful Go projects.”

This is honest and achievable based on the current specs.

### Mid-term framing

“Shunter is a reusable application platform for realtime/stateful apps, with shared services for auth, search, jobs, bindings, and live state sync.”

This becomes true once Layer 2 exists.

### Long-term framing

“Shunter powers multiple higher-level products and app stacks, such as brains, collaboration systems, internal tools, and domain-specific realtime apps.”

This becomes true once Layer 3 products exist.

---

## 10. Practical design takeaway

If the real goal is:

- build a better agent brain,
- and also use the same foundation for broader personal/internal apps,

then the best path is likely:

1. finish and stabilize the kernel,
2. add reusable Layer 2 platform services,
3. build both the brain and other app ideas on top of that.

This keeps the core clean while still allowing broad eventual applicability.

---

## 11. What Layer 2 should look like as a Go library

If Shunter is meant to become something you `go get` and embed into many projects, Layer 2 is the real public library surface.

The critical rule is:

- Layer 1 should remain mostly internal engine machinery.
- Layer 2 should be the stable app-facing API.
- Layer 3 apps should compose Layer 2, not reach deep into Layer 1 internals.

### 11.1 Layer 2 responsibilities

Layer 2 should provide the public, reusable platform API for application authors.

That API should cover at least:

- engine bootstrap / lifecycle
- schema registration
- reducer registration
- query/read helpers
- subscription registration helpers
- auth and identity integration hooks
- optional service modules (search, jobs, blobs, embeddings, etc.)
- client/codegen-facing schema export hooks
- configuration and observability hooks

In other words, Layer 2 is where Shunter stops feeling like a set of engine subsystems and starts feeling like an embeddable application platform.

### 11.2 Internal vs public package boundary

For extensibility, the public API boundary should be deliberate.

Recommended shape:

- internal / low-level engine packages should not be the normal import path for apps
- public packages should expose stable builders, interfaces, and service registration points
- app code should not need direct awareness of executor/store/commitlog internals

Possible package split:

```text
github.com/ponchione/shunter/
  shunter/           // top-level app-facing package, or root package itself
  schema/            // public schema/builder surface
  client/            // optional client helpers / generated bindings support
  auth/              // public auth provider interfaces
  search/            // optional search/query module APIs
  jobs/              // optional background job APIs
  blobs/             // optional attachment/blob APIs

  internal/...       // engine-only implementation details
```

Exact package names are flexible; the important point is that Layer 2 should look intentional and stable to application developers.

### 11.3 Primary extension seam: registration and composition

The main way apps should extend the platform is by registration/composition, not by editing engine internals.

An app should be able to supply:

- tables
- reducers
- lifecycle hooks
- optional modules/services
- policies/configuration

That means Layer 2 should revolve around a public builder/bootstrap object.

Conceptually:

```go
type AppBuilder struct {
    // public registration/config surface only
}

func NewApp(opts ...Option) *AppBuilder

func (b *AppBuilder) RegisterTable[T any](opts ...schema.TableOption) error
func (b *AppBuilder) Reducer(name string, h ReducerHandler) *AppBuilder
func (b *AppBuilder) OnConnect(h ReducerHandler) *AppBuilder
func (b *AppBuilder) OnDisconnect(h ReducerHandler) *AppBuilder

func (b *AppBuilder) WithAuthProvider(p auth.Provider) *AppBuilder
func (b *AppBuilder) WithSearchModule(m search.Module) *AppBuilder
func (b *AppBuilder) WithJobRunner(j jobs.Runner) *AppBuilder
func (b *AppBuilder) WithBlobStore(bs blobs.Store) *AppBuilder

func (b *AppBuilder) Build() (*App, error)
```

The exact signatures are illustrative, not normative. The design point is that app composition should happen at a clean public boundary.

### 11.4 Optional modules instead of kernel bloat

To keep Shunter extensible, higher-level capabilities should usually be optional platform modules, not hard-coded kernel concerns.

Good candidates for optional Layer 2 modules:

- auth / identity providers
- search / full-text / retrieval services
- embeddings / vector services
- background jobs / scheduled work helpers
- blob / attachment stores
- audit/provenance helpers
- common entity/history/task packages

This lets different apps choose different stacks while reusing the same engine kernel.

### 11.5 Interface-driven platform seams

Extensibility depends on public seams being interface-oriented.

Likely public interfaces include:

- `AuthProvider`
- `IdentityDeriver`
- `SearchProvider`
- `EmbeddingProvider`
- `BlobStore`
- `JobRunner`
- `Logger`
- `Clock`
- `IDGenerator`

These are not all kernel concerns. They are platform seams that let apps swap implementations without forking Shunter.

### 11.6 The app should own domain abstractions

Even after Layer 2 exists, individual apps will still need app-specific abstractions.

That is expected.

The right pattern is:

- Layer 2 provides reusable platform services
- each app defines its own domain packages on top

Example app layout:

```text
myapp/
  main.go
  app/
    bootstrap.go
  domain/
    notes/
      schema.go
      reducers.go
      service.go
    tasks/
      schema.go
      reducers.go
      service.go
    agents/
      schema.go
      reducers.go
      service.go
```

Those domain packages should expose registration helpers, for example:

- `notes.Register(builder)`
- `tasks.Register(builder)`
- `agents.Register(builder)`

Then app bootstrap simply composes them.

### 11.7 What app bootstrap should feel like

The target developer experience is something like:

1. import Shunter Layer 2
2. create a builder/app object
3. register domain modules
4. attach optional services
5. build and start the runtime

Conceptually:

```go
func main() {
    b := shunter.NewApp()

    notes.Register(b)
    tasks.Register(b)
    agents.Register(b)

    b.WithAuthProvider(myAuth)
    b.WithSearchModule(mySearch)

    app, err := b.Build()
    if err != nil {
        panic(err)
    }

    if err := app.Start(); err != nil {
        panic(err)
    }
}
```

Again, the exact API is illustrative. The design goal is that application authors work at this level, not at the executor/store/commitlog layer.

### 11.8 What should remain out of the public surface

For long-term stability, app code should generally not depend directly on:

- executor inbox details
- commitlog record framing
- internal recovery choreography
- internal fan-out worker mechanics
- low-level store mutation internals

Those are kernel implementation concerns. Layer 2 should wrap them in stable contracts.

### 11.9 How this enables both reuse and specialization

This design lets Shunter be both:

- a reusable platform across many projects
- and a foundation for highly specific app abstractions

That works because:

- the kernel provides correctness and realtime behavior
- Layer 2 provides reusable app-building services
- Layer 3 remains free to define domain-specific language and workflows

So the existence of app-specific abstractions on top does not weaken generality. It is what makes the platform practical.

### 11.10 Practical milestone

Layer 2 should be considered real when a new project can:

- add Shunter as a dependency
- register its schema/reducers without touching internals
- attach optional services through public interfaces
- start the runtime through a stable bootstrap API
- expose app-specific domain packages on top with minimal glue

---

## 12. Final bottom line

The current Shunter specs are not too narrow to be valuable.
They are narrow in the right place: the kernel.

What is still needed for broader general-purpose use is not “make the kernel do everything,” but:

- preserve the kernel as the stable engine,
- then add reusable platform services above it.

That is likely the right path if Shunter is meant to support:

- an LLM brain
- general internal/personal apps
- future domain-specific realtime systems

In that sense:

- Layer 1 makes Shunter real,
- Layer 2 makes Shunter broadly reusable,
- Layer 3 makes Shunter concretely useful for individual products.
