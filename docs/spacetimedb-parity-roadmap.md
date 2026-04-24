# Shunter SpacetimeDB Operational-Parity Roadmap

This is the parity direction document, not a changelog. Closed slice detail belongs in tests and git history.

## Mission

Build a clean-room Go implementation that is operationally equivalent to SpacetimeDB where it matters:
- clients can use comparable protocol flows and observe comparable outcomes
- reducers, subscriptions, delivery, durability, reconnect, and recovery are close enough for real private use
- internal mechanisms may differ when the externally visible result is equivalent or consciously deferred

Parity is judged by named client-visible scenarios, not helper-level resemblance.

## Current Grounded Status

- The repo contains substantial live implementation across protocol, subscription, executor, commitlog, store, schema, types, and bsatn.
- `TECH-DEBT.md` is the open backlog.
- `NEXT_SESSION_HANDOFF.md` is the immediate parity startup document.
- `docs/parity-phase0-ledger.md` is now a compact current-truth ledger, not a historical list of every closed row.

## Priority Rule

Use this order when choosing work:
1. externally visible parity gaps
2. correctness / concurrency bugs that can invalidate parity claims
3. capability gaps that prevent comparable workloads from running
4. internal cleanup / duplication / ergonomics

## Tier A - Required For Serious Parity Claims

### A1. Protocol surface is not wire-close enough

Open theme:
- legacy `v1.bsatn.shunter` admission, brotli deferral, and envelope/message-family divergence remain tracked under `TECH-DEBT.md::OI-001`

Main code surfaces:
- `protocol/options.go`
- `protocol/tags.go`
- `protocol/wire_types.go`
- `protocol/client_messages.go`
- `protocol/server_messages.go`
- `protocol/compression.go`
- `protocol/dispatch.go`
- `protocol/send_responses.go`
- `protocol/send_txupdate.go`
- `protocol/fanout_adapter.go`
- `protocol/upgrade.go`

### A2. Subscription/query model still diverges too much

No queued active item. Choose the next bounded A2 residual from a fresh scout, not from historical handoff notes.

Already-closed SQL/query and fanout slices are not listed here. If an agent needs historical detail, use the tests named by git history, not this roadmap.

Remaining broad risks:
- supported SQL remains narrower than the reference path
- row-level security / per-client filtering is absent
- subscription behavior still spans parser, protocol, manager, evaluator, executor, and fanout seams

Main code surfaces:
- `query/sql/parser.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_oneoff.go`
- `subscription/predicate.go`
- `subscription/validate.go`
- `subscription/hash.go`
- `subscription/register_set.go`
- `subscription/manager.go`
- `subscription/eval.go`
- `subscription/fanout.go`
- `executor/executor.go`

### A3. Recovery/store behavior still differs in ways users can feel

Open theme:
- value model, changeset semantics, commitlog format, replay edges, snapshots, and recovery behavior remain tracked under `TECH-DEBT.md::OI-003` and `TECH-DEBT.md::OI-007`

Main code surfaces:
- `types/`
- `bsatn/`
- `store/`
- `commitlog/`
- `executor/executor.go`

## Tier B - Required For Trustworthy Private Use

Current themes:
- lower-level raw read-view / snapshot lifetime discipline remains an expert-API contract (`OI-005`)
- hosted-runtime v1 normal read path is already narrowed and should not be reopened without a concrete regression

## Tier C - Can Wait Until Parity Decisions Are Locked

Examples:
- broad cleanup
- structural refactors with no externally visible parity impact
- ergonomics work after parity-critical seams settle

## Development Principles

- Parity first, elegance second.
- Same outcome beats same mechanism.
- Every parity change needs an observable test.
- Do not leave divergences implicit.
- Do not carry closed slice history into every new agent run.

## Immediate Execution Guidance

For a new parity session:
1. read `RTK.md`
2. read `NEXT_SESSION_HANDOFF.md`
3. inspect the live code with Go tools before editing
4. open this roadmap, `TECH-DEBT.md`, or the ledger only for specific prioritization or contract questions

Current best next direction is the active item in `NEXT_SESSION_HANDOFF.md`.

## Reading Rule

Use this file for priority and phase framing only. Do not use it as implementation context for a narrow slice unless the slice actually changes roadmap-level direction.
