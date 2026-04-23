# Shunter — Project Brief

## What Shunter Is

Shunter is a Go-native hosted real-time database/runtime that colocates application logic with data and synchronizes state to connected clients via a subscription-driven push model.

It is architecturally inspired by the publicly documented design of SpacetimeDB (by Clockwork Labs), but is an independent, clean-room implementation. No SpacetimeDB source code is used during implementation. Design specs are derived from public documentation, published architecture descriptions, and independent engineering analysis. Implementation is performed by agents with no exposure to the original codebase.

Shunter is not a hosted cloud service. It is intended to be its own runtime/server that applications define against and connect to.

## The Core Thesis

Traditional app architecture separates the database, the application server, and the client-side state layer into independent systems. Developers spend enormous effort synchronizing them: writing API endpoints, managing caches, invalidating stale data, building WebSocket layers for real-time updates, and maintaining client-side state stores (Redux, TanStack Query, etc.) that duplicate server state.

Shunter collapses these layers. Application logic runs in-process with the data. The database knows what changed at commit time, so it is the natural place to evaluate client subscriptions and push deltas. The client-side state is a live mirror of the server-side subscription result set, maintained automatically.

This is not a new idea in its individual components. In-memory databases, commit logs, event sourcing, and pub/sub are all well-established. The value is in their composition into a single coherent runtime that eliminates the synchronization glue code.

## Why Go

SpacetimeDB is written in Rust for reasons specific to their product: zero GC pauses for MMORPG-scale latency requirements, WASM as a native compilation target for their multi-language module system, and compile-time memory safety guarantees for shared mutable state across threads.

Shunter makes different tradeoffs for different reasons:

- **No WASM runtime.** App logic should be defined in Go-native module/app-definition surfaces. Shunter does not need a WASM or multi-language server-module runtime to get the core Spacetime-like architecture.
- **Single-goroutine executor.** All mutable state is owned by one goroutine processing transactions sequentially off a channel. No shared mutable state, so Rust's borrow checker provides no structural advantage over Go's simple ownership-by-convention.
- **GC pauses are manageable.** Go's GC achieves sub-millisecond pauses. CockroachDB, TiDB, InfluxDB, etcd, and Badger are all production database systems in Go. The requirement is allocation discipline on the hot path (buffer pooling, context reuse), not a different language.
- **The audience is Go developers.** The point is a Go-native runtime and Go-native app-definition story, not FFI into Rust and not a multi-language module host.
- **Throughput ceiling is sufficient.** Rust will reach higher raw transaction throughput on a single node. But tens of thousands of transactions per second — which covers the vast majority of real-time app workloads — is well within Go's capability.

## Architectural Overview

Shunter consists of four core subsystems and two supporting systems:

```
┌─────────────────────────────────────────────────────────┐
│                     Shunter Engine                       │
│                                                         │
│  ┌─────────────┐    ┌──────────────┐    ┌────────────┐  │
│  │  In-Memory   │◄──│  Transaction  │──►│  Commit     │  │
│  │  Relational  │   │  Executor     │   │  Log        │  │
│  │  Store       │   │  (single      │   │  (WAL +     │  │
│  │              │   │   goroutine)  │   │  snapshots) │  │
│  └──────┬───────┘   └──────┬───────┘   └────────────┘  │
│         │                  │                            │
│         │           ┌──────▼───────┐                    │
│         │           │ Subscription │                    │
│         └──────────►│ Evaluator    │                    │
│                     │              │                    │
│                     └──────┬───────┘                    │
│                            │                            │
│  ┌─────────────┐    ┌──────▼───────┐                    │
│  │   Schema     │   │   Client     │                    │
│  │   Definition │   │   Protocol   │                    │
│  │   System     │   │   (WebSocket)│                    │
│  └─────────────┘   └──────────────┘                    │
└─────────────────────────────────────────────────────────┘
```

### 1. In-Memory Relational Store

Holds the entire working dataset in RAM. Supports typed columns, primary keys with uniqueness enforcement, and secondary indexes. The critical design requirement is cheap changeset production — every committed transaction must produce a precise diff (inserts, updates with before/after, deletes) that the subscription evaluator can process.

**Design approach:** Explicit mutation journaling. The transaction context maintains a journal of every insert, update, and delete as it happens. On commit, the journal *is* the changeset. This avoids the cost of diffing immutable data structures and is natural for imperative Go code.

Tables are defined as Go structs with struct tags. Indexes are declared via tags or registration functions. The store uses B-tree or radix-tree indexes internally.

### 2. Commit Log

Append-only durable log of committed transactions. Each entry contains the full changeset (inserts, updates with before/after values, deletes) in a compact binary format. The in-memory store is a materialized view of this log — on crash recovery, the log is replayed to reconstruct state.

**Key design decisions:**
- Binary encoding: Protocol Buffers or a custom binary format. JSON is excluded for performance.
- Periodic snapshots: Full serialization of in-memory state at intervals. Recovery replays snapshot + log tail.
- Log compaction: Truncate entries before the latest snapshot. Full log retention is optional for audit/time-travel.
- fsync policy: Configurable per-transaction or batched. Tunable durability vs. throughput tradeoff.

### 3. Transaction Executor

Single goroutine that owns all mutable state. Receives transaction requests (reducer calls) via a channel. Processes them sequentially — one at a time, fully serialized.

**Execution flow:**
1. Dequeue reducer call (function reference + arguments + caller identity)
2. Create transaction context with mutable access to in-memory store and an empty mutation journal
3. Execute the reducer function
4. On success: append journal to commit log, apply mutations to in-memory store, pass changeset to subscription evaluator
5. On failure (returned error or panic recovery): discard transaction context entirely, no mutations applied

The single-goroutine model eliminates all concurrency control for writes. No locks, no MVCC versioning, no deadlock detection. This is the Redis model — when data is in memory and operations are fast, a single thread saturates useful throughput before contention matters.

Read-only operations (initial subscription state, ad-hoc queries) may be served concurrently from a read-only snapshot, but all writes are serialized.

### 4. Subscription Evaluator

After every committed transaction, evaluates the changeset against all active client subscriptions to determine which clients receive which deltas.

**A subscription is a standing query.** The client declares "I care about rows in table X where column Y = Z." When a transaction commits changes that affect that result set, the client receives a delta: rows added to the result set, rows removed from it, rows with changed values.

**Optimization strategy:**
- Index subscriptions by the tables they reference. When a changeset arrives, look up only subscriptions that reference modified tables.
- For column-equality predicates (the common case), further index by column + value. This turns fan-out into a lookup rather than a scan.
- Group structurally identical subscriptions (same table, same predicate shape, different parameter values) and evaluate them as a batch.

This is the most architecturally complex subsystem and the one where SpacetimeDB's public documentation is thinnest. Spec derivation for this component will require the most independent design work.

### 5. Client Protocol

WebSocket-based persistent connections between clients and the Shunter engine.

**Operations:**
- **Connect/authenticate:** Client establishes connection, provides identity credentials
- **Subscribe:** Client registers a subscription query, receives initial result set
- **Unsubscribe:** Client removes a subscription
- **Call reducer:** Client sends an RPC (reducer name + arguments), receives success/failure response
- **Receive delta:** Server pushes subscription deltas after relevant commits

**Message format:** Binary-encoded (protobuf or custom). Each delta message contains the subscription ID, the transaction ID that caused it, and the set of row-level changes (insert/update/delete with full row data).

### 6. Schema Definition System

Go structs become table definitions. This is the developer-facing API surface.

```go
type Player struct {
    ID      uint64 `shunter:"primarykey,autoincrement"`
    Name    string `shunter:"index"`
    GuildID uint64 `shunter:"index"`
    Score   int64
}
```

At engine startup, the schema is registered — either via reflection on tagged structs or via explicit builder API. The engine creates the corresponding in-memory tables and indexes.

Client-side type generation (e.g., TypeScript types from Go struct definitions) is a build-time tool, not a runtime concern. This is analogous to protoc or openapi-generator.

## What Shunter Is NOT

- **Not a hosted cloud service.** No cloud control plane or managed multi-tenant infrastructure is implied by default.
- **Not a multi-language module system.** Modules are Go functions. Period.
- **Not an MMORPG engine.** It can power real-time apps including games, but it's not optimized for spatial simulation or physics-tick workloads.
- **Not a distributed database.** Single-node, in-memory. Horizontal scaling is out of scope for v1.
- **Not a SQL database.** Subscriptions may use SQL-like syntax for familiarity, but the query surface is constrained to what the subscription evaluator can efficiently process. Full SQL is a non-goal.

## Spec Deliverables

Each subsystem gets its own standalone specification document. Each spec must be implementable by an engineer (or agent) who has never seen SpacetimeDB source code. Specs define interfaces, data structures, algorithms, error handling, and performance constraints.

| Spec | Filename | Description |
|------|----------|-------------|
| 1 | `SPEC-001-store.md` | In-memory relational store: table representation, column types, indexes, mutation journaling, snapshot serialization |
| 2 | `SPEC-002-commitlog.md` | Commit log: entry format, binary encoding, append/read operations, snapshot lifecycle, log compaction, recovery procedure |
| 3 | `SPEC-003-executor.md` | Transaction executor: goroutine model, reducer lifecycle, transaction context API, commit/abort flow, error handling, panic recovery |
| 4 | `SPEC-004-subscriptions.md` | Subscription evaluator: subscription registration, predicate representation, changeset evaluation algorithm, delta computation, fan-out, optimization strategies |
| 5 | `SPEC-005-protocol.md` | Client protocol: WebSocket lifecycle, message types, binary framing, initial sync, delta delivery, RPC call/response, authentication handshake |
| 6 | `SPEC-006-schema.md` | Schema definition: struct tag grammar, supported column types, index declaration, registration API, reflection-based and builder-based paths, client codegen interface |

## Research Plan — SpacetimeDB Source Reading

Before writing specs, specific areas of the SpacetimeDB Rust codebase should be studied for algorithmic understanding. This reading informs the spec, but no Rust code is carried into implementation. The implementation agents receive only the finished specs.

**Priority reads:**

1. **Subscription evaluation logic** — How subscriptions are registered, how changesets are matched against subscription predicates, how deltas are computed. This is the least-documented and most novel subsystem.
   - Likely location: `crates/core/src/subscription/` or similar
   - What to extract: The algorithm, not the code. How do they index predicates? How do they handle joins in subscriptions? What's the computational complexity?

2. **Commit log format** — How transaction entries are serialized, what metadata is included, how snapshots interact with the log tail.
   - Likely location: `crates/core/src/db/` or `crates/commitlog/`
   - What to extract: Entry structure, encoding choices, snapshot trigger conditions.

3. **Transaction isolation model** — How the mutation journal works within a reducer execution, how rollback is implemented, how the committed state is atomically updated.
   - Likely location: `crates/core/src/db/` or `crates/vm/`
   - What to extract: Journal data structure, commit vs. abort paths, interaction with the in-memory store.

4. **Client protocol / wire format** — How WebSocket messages are framed, what a delta message looks like on the wire, how initial state sync works.
   - Likely location: `crates/client-api/` or `crates/core/src/client/`
   - What to extract: Message types, serialization format, subscription lifecycle messages.

**Deprioritized:**
- WASM/V8 module runtime — Not relevant. Shunter modules are native Go.
- Multi-language codegen — Not relevant for the core engine. Client codegen is a separate tool concern.
- Cloud/hosting infrastructure — Not relevant. Shunter is a library.
- Auth/identity system — Design independently. SpacetimeDB's OpenID Connect approach is well-documented publicly and doesn't require source reading.

## Implementation Plan (Post-Spec)

Implementation is performed via SodorYard — a multi-agent framework. Agents receive specs from the `.brain/` vault. They have no access to SpacetimeDB source code. The spec is the sole interface.

**Suggested implementation order:**

1. **Schema definition** (SPEC-006) — Foundation. Everything else depends on how tables are defined.
2. **In-memory store** (SPEC-001) — Core data layer. Needs schema system to define tables.
3. **Transaction executor** (SPEC-003) — Needs the store to execute against.
4. **Commit log** (SPEC-002) — Needs the executor to produce committed transactions.
5. **Subscription evaluator** (SPEC-004) — Needs the store (for changeset format) and executor (for commit events).
6. **Client protocol** (SPEC-005) — Needs all of the above to be functional.

Each spec should include a verification section defining how correctness can be tested independently of other subsystems.

## Go Module Structure (Preliminary)

```
github.com/ponchione/shunter/
├── engine/          # Top-level engine initialization and lifecycle
├── store/           # In-memory relational store
├── commitlog/       # WAL, snapshots, recovery
├── executor/        # Transaction executor goroutine
├── subscription/    # Subscription registration and evaluation
├── protocol/        # WebSocket server, message framing
├── schema/          # Struct tag parsing, table registration
├── types/           # Shared types: column types, row representations, changesets
└── cmd/
    └── shunter-codegen/  # Client code generation tool (TypeScript, etc.)
```

## Open Questions

These should be resolved during spec writing:

1. **Subscription query language.** SQL subset? Custom DSL? Pure Go predicate functions? Each has tradeoffs for expressiveness vs. evaluator optimization.
2. **Update granularity.** Are deltas row-level or column-level? Row-level is simpler. Column-level reduces bandwidth for wide tables with frequent partial updates.
3. **Read-only concurrent access.** Should the store support concurrent read-only snapshots for serving initial subscription state while the executor processes writes? This adds complexity but improves latency for new subscribers.
4. **Backpressure.** What happens when a client can't consume deltas fast enough? Buffer and drop? Disconnect? Pause the subscription?
5. **Schema evolution.** How do table definitions change over time? Column additions, removals, type changes? This is hard in any database and should be scoped carefully for v1.
6. **Reducer scheduling.** SpacetimeDB supports scheduled reducers (run at a time or interval). Is this in scope for v1?

## Naming

**Shunter** — A shunter engine's job is sorting, routing, and delivering railway cars to exactly where they need to go. This is what the subscription evaluator does with data: every committed change is evaluated, sorted by relevance, and delivered to the right client.

Go module: `github.com/ponchione/shunter`
