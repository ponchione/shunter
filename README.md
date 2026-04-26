# Shunter

Shunter is a Go-native, clean-room real-time database/runtime inspired by the publicly documented design of SpacetimeDB.

SpacetimeDB is an architectural inspiration, not a product-compatibility target.
Shunter is for local/self-hosted apps built against Shunter-owned Go APIs and
Shunter-owned clients. It is not trying to be wire-compatible with SpacetimeDB
clients, mirror SpacetimeDB's hosted business model, or implement usage
metering as a billing primitive.

Important reality check: this repo is no longer just a docs/spec exercise. It contains substantial implementation across the core subsystem packages, and the test suite currently passes. But it is also not a polished, production-ready database you can confidently drop into an app today.

Latest broad verification during active audit work:
- `rtk go test ./... -count=1`
- Result: `Go test: 2006 passed in 11 packages`
- `rtk go vet ./...`
- Result: `Go vet: No issues found`
- `rtk go build ./...`
- Result: `Go build: Success`

## What this repo is

Shunter is trying to be a hosted runtime that combines:
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

## Core thesis and boundaries

Traditional app architecture separates the database, application server, and
client-side state layer. Shunter's thesis is that a single runtime can own the
data, run app logic next to it, evaluate subscriptions at commit time, and push
precise deltas to clients without a separate cache/API/WebSocket glue layer.

The intended developer surface is Go-native: applications define modules,
tables, reducers, and runtime configuration in Go. Shunter is not a managed
cloud service, not a multi-language module host, not a distributed database,
not a full SQL database, and not a SpacetimeDB client-compatibility layer.

SpacetimeDB remains useful as a reference design for hard runtime questions:
transaction ordering, subscription delta semantics, reducer outcomes, binary
encoding tradeoffs, and hosted-runtime ergonomics. When those ideas conflict
with Shunter's simpler self-hosted product goals, Shunter's goals win.

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
- root `shunter` package — hosted-runtime API (`Module`, `Config`, `Runtime`, `Build`, lifecycle/serving/local/describe helpers)

In other words: this is not vaporware anymore. There is a real subsystem implementation here.

## Hosted-runtime entrypoint

The prior bundled demo command has been removed. It no longer served a useful
role as a maintained app surface, and the root `shunter` package is the
supported hosted-runtime entrypoint.

Current app-facing proof lives in the root runtime API and package tests:
`Module`, `Config`, `Runtime`, `Build`, lifecycle/serving helpers, local calls,
and schema/export helpers. A new runnable example should be added only when it
proves a real product or integration path rather than another throwaway demo.

## What is not true yet

This repo is not yet a clear, finished product experience.

Specifically:
- the top-level hosted-runtime path exists, but it is still early and intentionally narrow
- there is no maintained bundled runnable hello-world command
- there is no full tutorial site, generated frontend, or client-binding/codegen workflow yet
- v1.5 query/view declarations, contract export, permissions metadata, and migration metadata are not implemented yet
- there is still active debt reconciliation work in `TECH-DEBT.md`

Also important: `schema.Engine.Start(...)` is not the app-facing runtime owner. The root `shunter.Runtime` is now the hosted-runtime owner that wires the subsystem graph behind a top-level API.

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
- turn the existing subsystem work into one coherent hosted runtime / app-definition model


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

For agent work, do not use this list as startup context. Read `RTK.md`, then the active root handoff (`NEXT_SESSION_HANDOFF.md` or `HOSTED_RUNTIME_PLANNING_HANDOFF.md`), then inspect live code.

For human orientation instead of another audit spiral, read in this order:
1. `README.md` — this file
2. `docs/decomposition/hosted-runtime-version-phases.md` — hosted-runtime phase map
3. `TECH-DEBT.md` — live debt and Shunter correctness priority framing
4. `docs/parity-decisions.md` — historical name; current Shunter design decisions informed by SpacetimeDB

Then inspect the main implementation packages:
- `schema/`
- `store/`
- `commitlog/`
- `executor/`
- `subscription/`
- `protocol/`
- root `shunter` package files (`module.go`, `runtime*.go`, `config.go`)

## Current practical status

If you want the blunt summary:
- The repo is worth keeping if you still care about building your own SpacetimeDB-like runtime.
- The repo is not yet worth pretending it is "done."
- The next high-leverage work is not more tiny audit slices by default.
- If continuing hosted-runtime usability, plan V1.5-A query/view declarations from `HOSTED_RUNTIME_PLANNING_HANDOFF.md`.
- If continuing correctness/completeness, use `NEXT_SESSION_HANDOFF.md` as the active driver and open the roadmap only for priority questions.

## What I would do next

If continuing correctness/completeness work, the most useful next step is:

1. Use `NEXT_SESSION_HANDOFF.md` as the active driver
- Shunter protocol correctness first
- one end-to-end delivery correctness slice second
- query/subscription-surface correctness third
- runtime/recovery semantics immediately after that, with scheduling pulled forward when the workload depends on it
- cleanup after the product contract is locked

2. Build scenario harnesses before broad refactors
- protocol scenario tests
- subscribe/reducer/update end-to-end tests
- recovery/replay scenario tests

3. Close the biggest externally visible differences before internal cleanup
- wire/protocol behavior
- query/subscription behavior
- reducer/update semantics
- recovery/store behavior

If that sequence is not followed, it is easy to keep improving isolated
internals without ending up with one coherent app runtime.

## Validation

To run the broad test suite:

```bash
rtk go test ./...
```

## Clean-room note

Shunter is intended to be a clean-room implementation inspired by public documentation and independent analysis of SpacetimeDB's architecture. The goal is architectural learning and independent implementation, not source reuse.
