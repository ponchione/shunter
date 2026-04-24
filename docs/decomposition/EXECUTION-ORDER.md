# Shunter — Implementation Execution Order

This document is the working implementation plan for the current decomposition docs. It is meant to be used for real execution, not as a loose architectural summary.

Scope boundary:
- The decomposition docs model the Shunter core engine/runtime that is comparable to the SpacetimeDB engine kernel.
- They do not attempt to specify the full SpacetimeDB product surface such as hosted/cloud control-plane behavior, standalone host/database routing, or multi-language module-runtime hosting.
- Do not silently expand a decomposition story into parity work that is outside this narrowed engine scope.

The spec headers are not the only source of dependency cycles. There are three real implementation-level tangles that matter here:

1. SPEC-006 schema registration is mostly independent of the executor, but reducer registration in SPEC-006 Epic 3 Story 3.2 explicitly consumes `ReducerHandler` / `ReducerContext` from SPEC-003.
2. SPEC-004 fan-out delivery and SPEC-005 outbound protocol delivery are intentionally split across specs: subscriptions produce `CommitFanout`, protocol owns `ClientSender`, and SPEC-004 Epic 6 depends on that protocol sender contract.
3. SPEC-004 Story 5.1 assembles and emits `FanOutMessage`, but that type is owned by SPEC-004 Epic 6 Story 6.1. In practice, the evaluation loop and the fan-out contract have to land together as a small internal milestone even though the full protocol-backed delivery behavior finishes later.

So the correct execution strategy is not “finish each whole spec in numeric order.” It is “land the contract-producing slices first, then build the dependent epics.”

Operational reading rule:
- If a step below names a whole epic, treat that as “finish the whole epic.”
- If a step below names a story or “contract slice,” that is an intentional exception backed by the decomposition docs.
- Do not silently re-inflate sliced exceptions into whole-epic blockers.

---

## Critical Path

```text
Phase 1: Foundation contracts
  SPEC-001 E1 (ValueKind, Value, ProductValue)
  SPEC-006 E2 (struct tag parser)                          ← can start immediately
  SPEC-006 E1 (TableSchema, type mapping)                  ← after 001 E1
  SPEC-003 E1.1 + E1.2 + minimal E1.4 contract slice      ← reducer/runtime contract only

Phase 2: Schema core
  SPEC-006 E3.1 (Builder core, TableDef, EngineOptions)
  SPEC-006 E4 (reflection-path RegisterTable)
  SPEC-006 E3.2 (reducer registration)                     ← after 003 contract slice
  SPEC-006 E5 (validation, Build, SchemaRegistry)
  SPEC-006 E6 (schema export)                              ← not on critical path

Phase 3: Store
  SPEC-001 E2 (schema-backed table storage)                ← after 006 E1
  SPEC-001 E3 (B-tree index engine)                        ← parallel with 001 E2
  SPEC-001 E4 (table indexes & constraints)
  SPEC-001 E5 (transaction layer)                          ← after 006 E5
  SPEC-001 E6 (commit, rollback, changeset)
  SPEC-001 E7 (read-only snapshots)
  SPEC-001 E8 (auto-increment & recovery hooks)

Phase 4: Commit-log core + executor core
  SPEC-002 E1 (BSATN codec)                                ← after 001 E1
  SPEC-002 E2 (record format & segment I/O)                ← independent
  SPEC-003 E2 (reducer registry)
  SPEC-003 E3 (executor core)
  SPEC-003 E4 (reducer transaction lifecycle)              ← after 001 E6
  SPEC-002 E3 (changeset codec)                            ← after 001 E6 + 002 E1
  SPEC-002 E5 (snapshot I/O)                               ← after 001 E8 + 002 E1
  SPEC-002 E4 (durability worker)                          ← after 002 E2 + 002 E3
  SPEC-002 E6 (recovery)                                   ← after 002 E2 + 002 E3 + 002 E5 + 001 E8
  SPEC-002 E7 (log compaction)

Phase 5: Subscription core
  SPEC-004 E1 (predicate types & query hash)
  SPEC-004 E2 (pruning indexes)
  SPEC-004 E3 (delta view & delta computation)             ← after 001 E7
  SPEC-004 E4 (subscription manager)
  SPEC-004 E6.1-enabling slice                             ← FanOutMessage / inbox contract for Story 5.1
  SPEC-004 E5 (evaluation loop)

Phase 6: Executor post-commit + lifecycle features
  SPEC-003 E5 (post-commit pipeline)                       ← after 002 E4 + 004 E4/E5 + 001 E7
  SPEC-003 E6 (scheduled reducers)                         ← after 003 E5 + 006 E5
  SPEC-003 E7 (lifecycle reducers & sys_clients)           ← after 003 E5 + 006 E5

Phase 7: Protocol core
  SPEC-005 E1 (message types & wire encoding)
  SPEC-005 E2 (authentication & identity)
  SPEC-005 E3.1-3.3 + 3.5 (transport core, conn state, keepalive)
  SPEC-005 E4 (client message dispatch)
  SPEC-005 E5 (server message delivery / ClientSender)
  SPEC-005 E6.1 + E6.2 (buffer limits / overflow handling)
  SPEC-005 E3.4 + 3.6 (OnConnect / OnDisconnect wiring)    ← after 003 E7
  SPEC-005 E6.3 + E6.4 (close/network-failure/reconnect)   ← after 005 lifecycle wiring

Phase 8: Fan-out integration
  SPEC-004 E6 remainder (fan-out & delivery)               ← after 004 E5 + 005 E5/6
```

---

## Why naive whole-spec ordering is wrong

### 1. SPEC-006 cannot be treated as entirely before SPEC-003

`docs/decomposition/006-schema/epic-3-builder-registration/story-3.2-reducer-registration.md`
explicitly depends on `ReducerHandler` and `ReducerContext` from SPEC-003.

That means:
- schema table-registration work can start early
- schema reducer-registration work cannot be considered done until the executor’s reducer type contract exists

So the safe split is:
- do SPEC-006 E3 Story 3.1 early
- land the SPEC-003 reducer/runtime contract slice
- then do SPEC-006 E3 Story 3.2

### 2. SPEC-004 cannot be finished entirely before SPEC-005

`docs/decomposition/004-subscriptions/epic-6-fanout-delivery/EPIC.md`
and Story 6.1 explicitly say fan-out delivery depends on SPEC-005’s `ClientSender` and outbound buffering contract.

At the same time,
`docs/decomposition/005-protocol/epic-5-server-message-delivery/EPIC.md`
consumes `CommitFanout` / `SubscriptionUpdate` from SPEC-004.

So the correct split is:
- finish SPEC-004 E1-E4 first
- land SPEC-004 E5 together with the minimal E6.1 fan-out contract it emits into
- build SPEC-005 E5/E6 (`ClientSender`, outbound delivery, backpressure)
- only then finish the rest of SPEC-004 E6 fan-out delivery

### 3. SPEC-005 transport core and lifecycle wiring should be split

`docs/decomposition/005-protocol/epic-3-websocket-transport-lifecycle/EPIC.md`
shows that only Stories 3.4 and 3.6 are lifecycle-hook integration work. Stories 3.1, 3.2, 3.3, and 3.5 are transport/config/connection-state work.

That means:
- protocol transport scaffolding can start before executor lifecycle support is complete
- only `OnConnect` / `OnDisconnect` wiring should wait for SPEC-003 E7

So “SPEC-005 E3 after SPEC-003 E7” is too strict if interpreted as the whole epic.

### 4. Executor post-commit work is not on the same critical path as executor core

SPEC-003 E2-E4 can be built before the commit log and subscription subsystems are complete.
Only SPEC-003 E5 needs:
- durability handoff from SPEC-002 E4
- subscription manager/eval contract from SPEC-004
- committed snapshots from SPEC-001 E7

So treating “all of SPEC-003” as blocked on “all of SPEC-002” is too strict.

### 5. Store table storage does not need the full schema build pipeline

SPEC-001 E2 needs schema types; SPEC-001 E5 is the point where `SchemaRegistry` becomes a real dependency.
So:
- SPEC-001 E2 can begin once schema types are stable
- SPEC-001 E5 should wait for SPEC-006 E5

That gives real parallelism without violating any stated contract.

---

## Phase Details

### Phase 1: Foundation contracts

Goal: land the shared value, schema-type, tag-parsing, and reducer-type contracts that many later epics talk about.

| Step | Spec | Epic / slice | What it produces | Parallel? |
|---|---|---|---|---|
| 1a | SPEC-001 | E1: Core Value Types | `ValueKind`, `Value`, `ProductValue`, `RowID`, `Identity`, `ColID` | — |
| 1b | SPEC-006 | E2: Struct Tag Parser | `ParseTag`, `TagDirectives`, directive validation | Parallel with 1a |
| 1c | SPEC-006 | E1: Schema Types & Type Mapping | `TableSchema`, `ColumnSchema`, `IndexSchema`, `TableID`, `IndexID`, Go→ValueKind map | After 1a |
| 1d | SPEC-003 | E1.1 + E1.2 + minimal E1.4 contract slice | `ReducerHandler`, `ReducerContext`, request/response shells, and the minimal referenced interface/type shells needed to make the reducer contract compile | Parallel with 1c once shared primitive types exist |

Gate: Phase 2 starts when 1b, 1c, and the reducer-type contract from 1d are in place.

Practical note: Story 1.2 references `SchedulerHandle`, while Story 1.4 is where that interface is formally declared. For real execution, treat 1d as a narrow “make the reducer/runtime contract compile” milestone, not as “finish all cross-subsystem executor interfaces.”

---

### Phase 2: Schema core

Goal: finish the builder / reflection / validation path and produce the immutable `SchemaRegistry`.

| Step | Spec | Epic / slice | What it produces | Parallel? |
|---|---|---|---|---|
| 2a | SPEC-006 | E3.1: Builder core | `Builder`, `TableDef`, `SchemaVersion`, `EngineOptions` | — |
| 2b | SPEC-006 | E4: Reflection path | `RegisterTable[T]`, field discovery, composite index assembly | After 2a + Phase 1 tag/type work |
| 2c | SPEC-006 | E3.2: Reducer registration | `Reducer`, `OnConnect`, `OnDisconnect` registration | After 1d + 2a |
| 2d | SPEC-006 | E5: Validation, Build & SchemaRegistry | `Build()`, `SchemaRegistry`, system tables, schema-version check | After 2b + 2c |
| 2e | SPEC-006 | E6: Schema export | `ExportSchema()` and codegen export structs | After 2d; parallel with later phases |

Gate: store transaction work should not start until 2d is done; plain table storage can start earlier once schema types are stable.

---

### Phase 3: Store

Goal: finish the in-memory store, commit semantics, snapshots, and recovery hooks.

| Step | Spec | Epic | What it produces | Parallel? |
|---|---|---|---|---|
| 3a | SPEC-001 | E2: Schema & Table Storage | `Table`, row insert/delete/scan, type validation | — |
| 3b | SPEC-001 | E3: B-Tree Index Engine | `BTreeIndex`, `IndexKey`, seek/range/scan | Parallel with 3a |
| 3c | SPEC-001 | E4: Table Indexes & Constraints | `Index` wrapper, constraint enforcement, set semantics | After 3a + 3b |
| 3d | SPEC-001 | E5: Transaction Layer | `CommittedState`, `TxState`, `StateView`, `Transaction` | After 3c + 2d |
| 3e | SPEC-001 | E6: Commit, Rollback & Changeset | `Commit()`, `Rollback()`, `Changeset`, net-effect semantics | After 3d |
| 3f | SPEC-001 | E7: Read-Only Snapshots | `CommittedReadView`, `CommittedSnapshot` | After 3e |
| 3g | SPEC-001 | E8: Auto-Increment & Recovery | `Sequence`, `ApplyChangeset()`, state export / restore hooks | After 3e |

Gate:
- commit-log changeset work needs 3e
- snapshot/recovery work needs 3f + 3g
- executor transaction lifecycle needs 3e

---

### Phase 4: Commit-log core + executor core

These can overlap. The executor’s core loop and reducer lifecycle do not require the full durability pipeline yet, and the commit log can build its file/codec layers independently.

| Step | Spec | Epic | What it produces | Parallel? |
|---|---|---|---|---|
| 4a | SPEC-002 | E1: BSATN Codec | value / row binary codec | Parallel with 4b-4d |
| 4b | SPEC-002 | E2: Record Format & Segment I/O | segment header, reader/writer, framing | Parallel with 4a |
| 4c | SPEC-003 | E2: Reducer Registry | reducer lookup / freeze | Parallel with 4a |
| 4d | SPEC-003 | E3: Executor Core | inbox, run loop, dispatch, shutdown | After 4c |
| 4e | SPEC-003 | E4: Reducer Transaction Lifecycle | begin/execute/commit/rollback path | After 3e + 4d |
| 4f | SPEC-002 | E3: Changeset Codec | changeset payload encoder/decoder | After 3e + 4a |
| 4g | SPEC-002 | E5: Snapshot I/O | snapshot writer/reader, integrity | After 3g + 4a |
| 4h | SPEC-002 | E4: Durability Worker | `DurabilityHandle` impl, write loop, rotation | After 4b + 4f |
| 4i | SPEC-002 | E6: Recovery | `OpenAndRecover`, replay, gap detection | After 4b + 4f + 4g + 3g |
| 4j | SPEC-002 | E7: Log Compaction | segment coverage / deletion | After 4g + recovery-side segment metadata is available |

Gate: SPEC-003 E5 must wait for 4h and the subscription manager/eval path from Phase 5.

---

### Phase 5: Subscription core

Goal: everything needed for registration, pruning, delta computation, and `CommitFanout` production — but not yet final delivery.

| Step | Spec | Epic / slice | What it produces |
|---|---|---|---|
| 5a | SPEC-004 | E1: Predicate Types & Query Hash | structured predicate model |
| 5b | SPEC-004 | E2: Pruning Indexes | value / join / table pruning tiers |
| 5c | SPEC-004 | E3: DeltaView & Delta Computation | delta engine over committed snapshots |
| 5d | SPEC-004 | E4: Subscription Manager | registration, dedup, dropped-client contract |
| 5e | SPEC-004 | E6.1-enabling contract slice | `FanOutMessage` / inbox contract needed by Story 5.1 emission path |
| 5f | SPEC-004 | E5: Evaluation Loop | `CommitFanout`, synchronous post-commit eval |

Gate: executor post-commit integration can start once 5d/5f exist.

Practical note: this is the one place where the decomposition has a real ownership split across adjacent epics. Story 5.1 emits `FanOutMessage`, while Story 6.1 defines it. For implementation, land those two pieces together even though the full fan-out worker behavior is deferred.

---

### Phase 6: Executor post-commit + lifecycle features

Goal: wire durability + subscriptions into the executor, then add scheduled and lifecycle behavior.

| Step | Spec | Epic | What it produces |
|---|---|---|---|
| 6a | SPEC-003 | E5: Post-Commit Pipeline | durability handoff, snapshot acquisition, eval call, fatal-state rules |
| 6b | SPEC-003 | E6: Scheduled Reducers | `sys_scheduled`, timer wakeups, replay |
| 6c | SPEC-003 | E7: Lifecycle Reducers & Client Management | `sys_clients`, OnConnect / OnDisconnect flow |

Dependencies:
- 6a after Phase 4 durability worker, Phase 5 subscription manager/eval, and store snapshots
- 6b / 6c after 6a and schema system-table ownership from SPEC-006 E5

---

### Phase 7: Protocol core

Goal: finish the WebSocket/API side, including the `ClientSender` contract used by subscription fan-out.

| Step | Spec | Epic / slice | What it produces |
|---|---|---|---|
| 7a | SPEC-005 | E1: Message Types & Wire Encoding | protocol message codecs |
| 7b | SPEC-005 | E2: Authentication & Identity | JWT / anonymous identity flow |
| 7c | SPEC-005 | E3.1-3.3 + 3.5: Transport core | options, upgrade, connection state, keepalive |
| 7d | SPEC-005 | E4: Client Message Dispatch | subscribe/unsubscribe/call/query dispatch |
| 7e | SPEC-005 | E5: Server Message Delivery | `ClientSender`, response delivery, tx-update delivery |
| 7f | SPEC-005 | E6.1 + E6.2: Overflow handling | outgoing/incoming buffer limits and immediate overflow behavior |
| 7g | SPEC-005 | E3.4 + 3.6: Lifecycle wiring | OnConnect / OnDisconnect executor integration |
| 7h | SPEC-005 | E6.3 + E6.4: Close / reconnect semantics | clean close, network failure cleanup, reconnection verification |

Dependencies:
- 7c after 7a + 7b
- 7d after 7c and the relevant executor/subscription command contracts exist
- 7e after 7c + 7d
- 7f after 7c + 7e
- 7g after 7c and SPEC-003 E7
- 7h after 7f + 7g

Gate: Phase 8 starts when 7e/7f/7h are done.

---

### Phase 8: Fan-out integration

Goal: finish the last cross-spec delivery layer.

| Step | Spec | Epic | What it produces |
|---|---|---|---|
| 8a | SPEC-004 | E6 remainder: Fan-Out & Delivery | `FanOutWorker`, protocol-backed delivery, confirmed-read gating, dropped-client signaling |

This is terminal because it depends on both:
- subscription-side `CommitFanout`
- protocol-side `ClientSender` / outbound buffering contract

---

## Parallelism Summary

```text
Timeline →

Foundation:   [001-E1] ──┬──> [006-E1] ───────────────────┐
              [006-E2] ──┘                                 │
              [003-E1 contract slice incl. min 1.4] ──────┤
                                                            │
Schema:                          [006-E3.1] ─┬─> [006-E4] ─┐
                                             └─> [006-E3.2] ├─> [006-E5] ─> [006-E6]
                                                             │
Store:                               [001-E2] ───────┐      │
                                     [001-E3] ──┐    │      │
                                                 └─> [001-E4] ─> [001-E5] ─> [001-E6] ─┬─> [001-E7]
                                                                                         └─> [001-E8]

Commitlog/core executor:          [002-E1] ─┬─> [002-E3] ─┐
                                     [002-E2] ────────────┼─> [002-E4] ─> [002-E6] ─> [002-E7]
                                     [003-E2] ─> [003-E3] ─> [003-E4]                         
                                     [002-E5] <────────────┘

Subscriptions core:                                           [004-E1] ─> [004-E2] ─┐
                                                              [004-E3] ───────────────┼─> [004-E4] ─> [004-E6.1*] ─> [004-E5]
                                                                                       │
Executor post-commit:                                                               [003-E5] ─> [003-E6]
                                                                                       └──────> [003-E7]

Protocol core:                                                                     [005-E1] ─> [005-E3 core] ─> [005-E4] ─> [005-E5] ─> [005-E6.1/6.2]
                                                                                    [005-E2] ───────────────────┘
                                                                                                           └────> [005-E3 lifecycle] ─> [005-E6.3/6.4]

Final integration:                                                                                                 [004-E6 remainder]
```

`[004-E6.1*]` means “land only the minimal fan-out contract needed by SPEC-004 Story 5.1”; it is not the full delivery implementation.

---

## Cross-Phase Interface Contracts

These are the interfaces and contract slices that actually determine the gates.

| Producer | Interface | Consumer(s) | Notes |
|---|---|---|---|
| SPEC-001 E1 | `ValueKind`, `Value`, `ProductValue`, `Identity`, `ColID` | SPEC-006, SPEC-002, SPEC-004, SPEC-005 | Earliest shared type foundation |
| SPEC-003 E1 contract slice | `ReducerHandler`, `ReducerContext`, minimal referenced interface shells | SPEC-006 E3.2 | Needed before schema reducer registration is complete |
| SPEC-006 E1 | `TableSchema`, `ColumnSchema`, `IndexSchema`, `TableID`, `IndexID` | SPEC-001 E2+, SPEC-002 snapshot schema work, SPEC-004 predicates | Schema-type gate |
| SPEC-006 E5 | `SchemaRegistry` | SPEC-001 E5+, SPEC-003 lifecycle/scheduled work | Full schema build gate |
| SPEC-001 E6 | `Changeset` | SPEC-002 E3/E4/E6, SPEC-004 E5, SPEC-003 E5 | Commit-log and subscription hot path |
| SPEC-001 E7 | `CommittedReadView` | SPEC-004 E3/E4/E5, SPEC-003 E5, SPEC-005 one-off query | Snapshot gate |
| SPEC-001 E8 | `ApplyChangeset()`, state export / restore hooks | SPEC-002 E5/E6 | Recovery gate |
| SPEC-002 E4 | `DurabilityHandle` implementation | SPEC-003 E5 | Post-commit durability handoff |
| SPEC-004 E4 + E6.1 + E5 | `SubscriptionManager`, `FanOutMessage` contract, `CommitFanout` | SPEC-003 E5, SPEC-005 E5, SPEC-004 E6 remainder | Evaluation and final delivery are split by a small internal contract seam |
| SPEC-005 E5/E6 | `ClientSender`, outbound buffer / backpressure contract | SPEC-004 E6 remainder | Required before fan-out delivery can finish |

---

## Readiness checklist before starting implementation from this doc

Use this checklist before assigning actual work:

- If a task touches schema reducer registration, confirm the SPEC-003 reducer/runtime contract slice is already landed.
- If a task claims to implement “protocol lifecycle,” decide whether it is really transport core (Stories 3.1-3.3/3.5) or lifecycle wiring (Stories 3.4/3.6).
- If a task claims to implement SPEC-004 E5, ensure the minimal E6.1 `FanOutMessage` contract is landing with it.
- If a task claims to implement SPEC-003 E5, ensure SPEC-002 E4, SPEC-004 E4/E5, and SPEC-001 E7 are all available.
- If a task claims to implement SPEC-005 Epic 6, split it into overflow handling (Stories 6.1/6.2) vs close/reconnect semantics (Stories 6.3/6.4); the latter needs protocol lifecycle wiring.
- If a task claims to implement final fan-out delivery, ensure SPEC-005 E5/E6 is already complete.

---

## Hosted runtime follow-on track

After the kernel path is ready enough to support a real hosted server, continue with the hosted-runtime roadmap in `docs/hosted-runtime-implementation-roadmap.md`.

That follow-on track starts with v1 hosted-runtime work:
- top-level `shunter` API surface: `Module`, `Config`, `Runtime`, `Build(...)`
- module definition and registration surface
- runtime build/lifecycle/network ownership
- local reducer/query calls as secondary APIs
- export/introspection hooks
- hello-world replacement that no longer hand-wires the subsystem graph

Then it moves to v1.5 usability/platform work:
- code-first query/view declarations
- canonical JSON module contract export
- client codegen/binding export
- narrow permissions/read-model metadata
- descriptive migration metadata and contract-diff tooling

Keep v2+ structural ambitions out of the v1/v1.5 track unless a later audit intentionally moves them earlier:
- multi-module hosting
- out-of-process module execution
- broad control plane
- executable migration systems
- full SQL/view system

---

## Bottom line

The safe implementation order is:
- schema/table/value foundations first
- schema build and store next
- commit-log core and executor core in parallel
- subscription core before executor post-commit wiring
- protocol core before final fan-out delivery
- final subscription fan-out delivery last
- top-level hosted-runtime work after the kernel is ready enough to replace manual bootstrap as the normal app path (initial V1-H proof is now landed)

That ordering matches the current decomposition docs and hosted-runtime docs closely enough to drive real implementation work without reopening the same dependency mistakes. Future hosted-runtime work should now start from V1.5 planning rather than reopening the v1 bootstrap sequence.