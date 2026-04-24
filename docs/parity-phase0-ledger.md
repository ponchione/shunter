# Current Parity Ledger

This file records current parity truth only. Closed slice-by-slice history was intentionally removed to keep agent startup small; use tests and git history when an old closure matters.

Status values:
- `open`: parity gap still requiring work
- `closed`: current target is explicit and sufficiently covered
- `deferred`: intentionally not being closed now

## Open Or Active Themes

| Theme | Status | Authority | Current truth |
|---|---|---|---|
| `OI-001` protocol wire-close follow-through | `open` | `TECH-DEBT.md`, protocol tests | Core protocol behavior is substantial, but legacy admission, brotli, and envelope/message-family divergences remain explicit. |
| `OI-002` subscription/query runtime parity | `open` | `NEXT_SESSION_HANDOFF.md` | No queued active slice; next batch is chosen by fresh scout. Same-connection reused-hash initial-snapshot elision is closed. `SubscriptionError.table_id` on request-origin error paths now always emits `None` to match reference v1. Closed `P0-SUBSCRIPTION-001` through `P0-SUBSCRIPTION-033`, reused-hash elision, and the `table_id: None` request-origin closure are not startup context. |
| `OI-003` recovery/store parity | `open` | `TECH-DEBT.md` | Value model, changeset, commitlog/recovery, replay, and snapshot differences remain user-visible risk areas. |
| `OI-005` raw read-view/snapshot lifetime discipline | `open` | `TECH-DEBT.md` | Normal hosted-runtime read path is narrowed; lower-level raw snapshot APIs remain expert APIs. |
| `OI-007` recovery format/scheduler deferrals | `open` | `TECH-DEBT.md` | Reader-side zero-header EOS is closed; format and scheduler deferrals remain explicit. |
| `DI-001` energy accounting | `deferred` | `TECH-DEBT.md` | `EnergyQuantaUsed` remains zero because Shunter has no energy/quota subsystem. |

## Closed Baselines

The following broad buckets are closed for the current phase framing and should not be reread or reopened without a fresh failing regression:
- protocol subprotocol/compression/lifecycle/message-family baselines
- canonical reducer delivery and empty-fanout caller outcomes
- subscription rows through `P0-SUBSCRIPTION-033`
- same-connection reused subscription-hash initial-snapshot elision
- scheduler startup/firing narrow slice
- recovery replay horizon and snapshot/replay invariant slices
- Phase 4 Slice 2 offset-index, typed error category, and record/log documented-divergence slices

## Reading Rule

Use this ledger to answer "is this theme open?" Do not use it as implementation context. The active root handoff owns the next slice.
