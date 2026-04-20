# Shunter

Shunter is a Go-native, clean-room real-time database/runtime inspired by the publicly documented design of SpacetimeDB.

Important reality check: this repo is no longer just a docs/spec exercise. It contains substantial implementation across the core subsystem packages, and the test suite currently passes. But it is also not a polished, production-ready database you can confidently drop into an app today.

Latest broad verification during active audit work:
- `rtk go test ./...`
- Result: `Go test: 1101 passed in 10 packages`
- `rtk go build ./...`
- Result: `Go build: Success`

## What this repo is

Shunter is trying to be an embeddable runtime that combines:
- schema definition
- in-memory relational storage
- append-only commit log + recovery
- single-threaded transaction execution
- subscription-based delta evaluation
- WebSocket protocol delivery

The implementation is intended to be clean-room:
- inspired by public SpacetimeDB docs and architecture discussions
- not copied from SpacetimeDB source
- developed from independent specs, decomposition docs, tests, and audit passes

## What is actually implemented today

There is working code in these packages:
- `types` — core value/identity/connection/reducer types
- `auth` — JWT validation, identity derivation, anonymous minting
- `schema` — builder, reflection path, registry, export, startup schema compatibility checks
- `store` — tables, indexes, transactions, changesets, snapshots, recovery hooks
- `commitlog` — record format, durability worker, snapshot I/O, replay, recovery, compaction
- `executor` — reducer registry, serialized execution, lifecycle/scheduler wiring, subscription dispatch
- `subscription` — predicate model, pruning indexes, delta evaluation, fanout, confirmed-read delivery
- `protocol` — wire codecs, upgrade/auth path, connection lifecycle, dispatch, outbound delivery, backpressure handling
- `query/sql` — the current narrow SQL surface used by subscribe and one-off query paths
- `bsatn` — binary value encoding used across the system

In other words: this is not vaporware anymore. There is a real subsystem implementation here.

## What is not true yet

This repo is not yet a clear, finished product experience.

Specifically:
- there is no simple top-level "start here" app/demo flow
- there is no `main` package or polished runnable example server at the repo root
- there is no stable public embedding story documented end-to-end
- there is no README-driven quickstart that proves "build app, define schema, run server, connect client"
- there is still active debt reconciliation work in `TECH-DEBT.md`

Also important: the current `schema.Engine.Start(...)` is not a full unified runtime bootstrap. It currently performs startup schema compatibility checks, but this repo does not yet present a single polished engine package that wires every subsystem into one obvious developer-facing API.

## So is the clean-room effort functional?

My honest answer:
- As a collection of implemented subsystems with meaningful tests: yes
- As a finished replacement for SpacetimeDB: no
- As a polished thing that clearly justifies unlimited audit/token burn by default: also no

A better framing is:
- there is real technical progress here
- the repo has crossed the line from "spec-only" into "substantial prototype/runtime pieces"
- but the project still lacks a crisp productized narrative and end-to-end developer experience

## When continuing this project makes sense

Continuing makes sense if the goal is one of these:
- finish a clean-room experimental runtime in Go
- validate the architecture and core invariants
- turn the existing subsystem work into one coherent engine API
- decide whether the system can support a real embedded database/product direction

Continuing probably does not make sense if the goal is:
- "I need a production-ready SpacetimeDB alternative right now"
- "I want a short path to a stable OSS release without more integration/product work"
- "I only want to keep doing narrow audit passes forever"

## Why the repo feels confusing right now

Because it has two different realities at once:

1. The codebase is much more real than an early-spec project.
2. The top-level framing is still missing the one document that says, plainly:
   - what exists
   - what works
   - what does not
   - what to build next
   - whether this should become a product, a research prototype, or a stopped experiment

That gap is exactly what this README is trying to close.

## Recommended way to read the repo

If you want orientation instead of another audit spiral, read in this order:
1. `README.md` — this file
2. `docs/current-status.md` — blunt completion/parity snapshot
3. `docs/project-brief.md` — original thesis and architecture intent
4. `docs/EXECUTION-ORDER.md` — implementation sequencing and dependency map
5. `docs/spacetimedb-parity-roadmap.md` — active parity development driver
6. `docs/parity-phase0-ledger.md` — named parity scenarios and pinned tests
7. `TECH-DEBT.md` — live debt and follow-up ledger

Then inspect the main implementation packages:
- `schema/`
- `store/`
- `commitlog/`
- `executor/`
- `subscription/`
- `protocol/`

## Current practical status

If you want the blunt summary:
- The repo is worth keeping if you still care about building your own SpacetimeDB-like runtime.
- The repo is not yet worth pretending it is "done."
- The next high-leverage work is not more tiny audit slices by default.
- The next high-leverage work is parity work against SpacetimeDB outcomes.

## What I would do next

If continuing, the most useful next step is:

1. Use `docs/spacetimedb-parity-roadmap.md` as the active driver
- wire-level protocol parity first
- one end-to-end delivery parity slice second
- query/subscription-surface parity third
- runtime/recovery semantics immediately after that, with scheduling pulled forward when the workload depends on it
- cleanup after the parity target is locked

2. Build parity harnesses before broad refactors
- protocol scenario tests
- subscribe/reducer/update end-to-end tests
- recovery/replay scenario tests

3. Close the biggest externally visible differences before internal cleanup
- wire/protocol behavior
- query/subscription behavior
- reducer/update semantics
- recovery/store behavior

If that sequence is not followed, it is easy to keep improving local correctness while still not ending up with your own operational SpacetimeDB.

## Validation

To run the broad test suite:

```bash
rtk go test ./...
```

## Clean-room note

Shunter is intended to be a clean-room implementation inspired by public documentation and independent analysis of SpacetimeDB's architecture. The goal is architectural learning and independent implementation, not source reuse.
