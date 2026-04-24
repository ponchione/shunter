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

Do not reopen the closed P0-SUBSCRIPTION-001 through P0-SUBSCRIPTION-027 rows without new failing regression evidence.

`P0-SUBSCRIPTION-027` is the first OI-002 slice since `P0-SUBSCRIPTION-018` where subscribe's acceptance surface widens alongside one-off. The justification is that the ON-form is syntactically new but semantically identical to the already-accepted WHERE-form — the parser produces indistinguishable output for either. Future slices should default back to the one-off-only pattern; use this precedent only when the new shape has a pinned parser-level parity claim against an already-accepted form.

Latest closed OI-002 query-only slice:

- `P0-SUBSCRIPTION-027`: one-off and subscription SQL now accept bounded `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface.

Representative accepted shape:

```sql
SELECT o.id FROM Orders o JOIN Inventory product ON o.product_id = product.id AND product.quantity < 10
```

Behavior now pinned:

- parser admits ON-equality plus exactly one qualified-column/literal filter; multi-conjunct, OR, column-vs-column, unqualified-column, and third-relation qualifiers remain rejected at the parser gate
- the parser transparently folds the ON-extracted filter into `Statement.Predicate`, producing output structurally identical to the semantically equivalent WHERE-form (verified by `TestParseJoinOnEqualityParityWithWhereForm`)
- one-off/ad hoc returns correctly filtered rows end-to-end (mirrors the existing WHERE-form pin)
- subscribe-side accepts the new shape transparently via the existing WHERE-filter compile path; this is the first OI-002 slice since `P0-SUBSCRIPTION-018` where subscribe widens alongside one-off, justified by the ON↔WHERE parser-level parity
- unindexed-join rejection for subscriptions remains independent of filter presence

Primary files touched by the latest OI-002 work:

- `query/sql/parser.go`
- `query/sql/parser_test.go`
- `protocol/handle_oneoff_test.go`
- `protocol/handle_subscribe_test.go`
- `TECH-DEBT.md`
- `docs/parity-phase0-ledger.md`

Latest validation reported for that slice:

- `rtk go test ./query/sql ./protocol -run 'JoinOnEquality.*Filter|ParseRejectsJoinOnFilter' -count=1 -v`
- `rtk go test ./query/sql ./protocol -count=1`
- `rtk go test ./query/sql ./protocol ./subscription -count=1`

Before calling the next slice done, still run the appropriate touched-package tests and prefer `rtk go test ./... -count=1` when time allows.

## Good next OI-002 candidates

Choose from fresh live evidence. The next bounded candidate should be chosen by scouting live code/docs/tests after the `P0-SUBSCRIPTION-027` landing; do not carry forward older candidate notes without re-verification.

Candidates carried forward from prior handoffs:

1. Runtime/fanout lanes.
   - QueryID-level fanout correlation / SubscriptionID wire cleanup.
   - Confirmed-read durability gating for `SubscriptionError`.
   - Deterministic per-connection update ordering.

2. Row-level security / per-client filtering.
   - This remains real but is too large to mix with a narrow SQL slice unless the user explicitly requests that broader work.

3. A TBD parser/compile seam continuation, to be chosen from fresh scout — for example, additional bounded widenings that can be admitted via transparent parser-level parity against an already-accepted shape (use this route only when the parity claim is exactly as tight as P0-SUBSCRIPTION-027's).

## Recommended next command checklist

```bash
rtk git status --short --branch
rtk go test ./query/sql ./protocol ./subscription -count=1
```

Then inspect touched Go surfaces with `rtk go doc` / `rtk go list -json` before editing.

## Working tree caution

The repo may contain unrelated hosted-runtime planning files and/or broader docs moves. Do not mix those into a TECH-DEBT/OI-002 implementation slice unless the user explicitly asks.
