# Next session handoff

Use this file to start the next agent on the current Shunter TECH-DEBT / parity task with no prior chat context.

This file is not the hosted-runtime planning handoff. Hosted-runtime planning now lives in:

- `HOSTED_RUNTIME_PLANNING_HANDOFF.md`

## Current objective

Continue working through `TECH-DEBT.md`, prioritizing externally visible parity gaps first.

Current active TECH-DEBT issue for handoff purposes:

- `OI-002`: query and subscription behavior still diverges from the target runtime model.

`OI-002` remains open after the latest query-only closures. The next bounded A2 batch should be chosen from fresh live evidence, not by reopening a just-closed SQL slice.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Required startup reading

Read in this order before editing:

1. `RTK.md`
2. `README.md`
3. `TECH-DEBT.md`
4. `docs/spacetimedb-parity-roadmap.md`
5. `docs/parity-phase0-ledger.md`
6. relevant spec/decomposition files for the chosen slice

If the task is hosted-runtime planning or implementation instead, stop and read `HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead of using this handoff.

## Latest OI-002 state to preserve

Do not reopen the closed P0-SUBSCRIPTION-001 through P0-SUBSCRIPTION-026 rows without new failing regression evidence.

Latest closed OI-002 query-only slice:

- `P0-SUBSCRIPTION-026`: one-off/ad hoc SQL now accepts bounded mixed-relation explicit column lists on the existing two-table join surface.

Representative accepted shape:

```sql
SELECT o.id, product.quantity
FROM Orders o JOIN Inventory product ON o.product_id = product.id
```

Behavior now pinned:

- parser preserves per-column source qualifiers across left/right join relations
- compile metadata carries table/alias identity for each selected column
- one-off row shaping can project from both sides of the matched join pair
- the response envelope remains the first projected relation's table name for this narrow slice
- SubscribeSingle still rejects column-list projections before executor registration with `column-list projections not supported for subscriptions`

Primary files touched by the latest OI-002 work:

- `query/sql/parser.go`
- `query/sql/parser_test.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_oneoff.go`
- `protocol/handle_oneoff_test.go`
- `TECH-DEBT.md`
- `docs/spacetimedb-parity-roadmap.md`
- `docs/parity-phase0-ledger.md`

Latest validation reported for that slice:

- `rtk go test ./query/sql ./protocol -run 'Join.*Projection|ProjectionColumnsOutside|Subscribe.*Projection' -count=1 -v`
- `rtk go test ./query/sql ./protocol -count=1`
- `rtk go test ./query/sql ./protocol ./subscription -count=1`

Before calling the next slice done, still run the appropriate touched-package tests and prefer `rtk go test ./... -count=1` when time allows.

## Good next OI-002 candidates

Choose from fresh live evidence. Remaining candidates from prior OI-002 handoff material were:

1. One-off JOIN ON predicate widening for two-table joins.
   - First bounded shape: `SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10`.
   - Keep subscribe rejection for anything outside subscription join constraints.

2. Runtime/fanout lanes.
   - QueryID-level fanout correlation / SubscriptionID wire cleanup.
   - Confirmed-read durability gating for `SubscriptionError`.
   - Deterministic per-connection update ordering.

3. Row-level security / per-client filtering.
   - This remains real but is too large to mix with a narrow SQL slice unless the user explicitly requests that broader work.

These are candidates only. Re-scout live code/docs/tests before choosing a batch.

## Recommended next command checklist

```bash
rtk git status --short --branch
rtk go test ./query/sql ./protocol ./subscription -count=1
```

Then inspect touched Go surfaces with `rtk go doc` / `rtk go list -json` before editing.

## Working tree caution

The repo may contain unrelated hosted-runtime planning files and/or broader docs moves. Do not mix those into a TECH-DEBT/OI-002 implementation slice unless the user explicitly asks.
