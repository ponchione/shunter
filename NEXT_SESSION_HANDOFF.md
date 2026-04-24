# Next session handoff

Use this file to start the next agent on the current Shunter TECH-DEBT / parity task with no prior chat context.

This file is not the hosted-runtime planning handoff. Hosted-runtime planning now lives in:

- `HOSTED_RUNTIME_PLANNING_HANDOFF.md`

## Current objective

Continue working through `TECH-DEBT.md`, prioritizing externally visible parity gaps first.

Current active TECH-DEBT issue for handoff purposes:

- `OI-002`: query and subscription behavior still diverges from the target runtime model.

`OI-002` remains open after the latest one-off cross-join `WHERE` equality-plus-filter closure. The next bounded A2 batch should be chosen from fresh live evidence, not by reopening a just-closed SQL, fanout-ordering, or QueryID wire-correlation slice.

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

Do not reopen the closed P0-SUBSCRIPTION-001 through P0-SUBSCRIPTION-031 rows without new failing regression evidence.

`P0-SUBSCRIPTION-031` is the latest OI-002 query/subscription slice. A fresh scout found that one-off/ad hoc SQL accepted the query-only cross-join `WHERE` column-equality shape, but still rejected the next reference-backed bounded predicate shape where that equality is combined with one qualified column-literal filter. Reference query SQL treats query predicates as the subscription `WHERE` predicate language and has no subscription join limitations; subscriptions still deliberately reject cross-join `WHERE`.

Latest closed OI-002 slice:

- `P0-SUBSCRIPTION-031`: one-off/ad hoc SQL now accepts `SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32 AND s.enabled = TRUE`-style shapes. The parser preserves a qualified column-vs-column equality plus one qualified column-literal filter, the one-off compile seam lowers it into the existing `subscription.Join` evaluator with `Join.Filter`, and subscribe still rejects cross-join `WHERE` before executor registration.

Behavior now pinned:

- `query/sql/parser_test.go::TestParseJoinWhereColumnEqualityAndLiteralFilter` pins the parser shape as `AndPredicate{ColumnComparisonPredicate, ComparisonPredicate}` without flattening it into legacy filters
- `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_CrossJoinWhereColumnEqualityAndLiteralFilterReturnsProjectedRows` fails if one-off/ad hoc query rejects the shape or ignores the literal filter
- `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_CrossJoinWhereColumnEqualityAndLiteralFilterStillRejected` fails if subscriptions accidentally accept cross-join `WHERE` or reach executor registration

Previous latest runtime/protocol slices:

- `P0-SUBSCRIPTION-030`: subscription updates now retain manager-internal `SubscriptionID` only inside the subscription package, while initial subscribe snapshots, post-commit fanout, final unsubscribe deltas, protocol adapters, and protocol encode/decode stamp/carry the client `QueryID` visible to clients.
- `P0-SUBSCRIPTION-029`: evaluator-produced fanout is stabilized per connection by internal subscription-registration/SubscriptionID order before fanout worker handoff and before caller-update capture.

Primary files touched by the latest OI-002 work:

- `query/sql/parser_test.go`
- `protocol/handle_oneoff_test.go`
- `protocol/handle_subscribe_test.go`
- `protocol/handle_subscribe.go`
- `TECH-DEBT.md`
- `docs/parity-phase0-ledger.md`
- `docs/spacetimedb-parity-roadmap.md`
- `docs/decomposition/005-protocol/SPEC-005-protocol.md`
- `NEXT_SESSION_HANDOFF.md`

Latest validation reported for that slice:

- `rtk go test ./query/sql ./protocol -run 'TestParseJoinWhereColumnEqualityAndLiteralFilter|TestHandleOneOffQuery_CrossJoinWhereColumnEqualityAndLiteralFilterReturnsProjectedRows|TestHandleSubscribeSingle_CrossJoinWhereColumnEqualityAndLiteralFilterStillRejected' -count=1 -v` initially failed as expected on the one-off test with `cross join WHERE only supports qualified column equality`
- `rtk go fmt ./query/sql ./protocol`
- `rtk go test ./query/sql ./protocol -run 'TestParseJoinWhereColumnEquality|TestHandleOneOffQuery_CrossJoinWhereColumnEquality|TestHandleSubscribeSingle_CrossJoinWhereColumnEquality' -count=1 -v`
- `rtk go test ./query/sql ./protocol ./subscription ./executor -count=1`
- `rtk go vet ./query/sql ./protocol`
- `rtk go test ./... -count=1`

Before calling the next slice done, still run the appropriate touched-package tests and prefer `rtk go test ./... -count=1` when time allows.

## Good next OI-002 candidates

Choose from fresh live evidence. The next bounded candidate should be chosen by scouting live code/docs/tests after the `P0-SUBSCRIPTION-031` landing; do not carry forward older candidate notes without re-verification.

Candidates carried forward from prior handoffs:

1. Runtime/fanout lanes.
   - Choose only from fresh evidence; the known QueryID-level fanout/protocol correlation and deterministic per-connection ordering candidates are closed.

2. Row-level security / per-client filtering.
   - This remains real but is too large to mix with a narrow SQL or fanout slice unless the user explicitly requests that broader work.

3. A TBD parser/compile seam continuation, to be chosen from fresh scout.
   - Use this route only when the parity claim is exactly bounded and reference-backed.
   - Do not reopen the now-closed projection-family, unindexed-join, cross-join `WHERE` equality/equality-plus-filter, join-backed count aggregate, JOIN ON filter, SubscriptionError durability-gating, per-connection ordering, or QueryID fanout/protocol targets without new failing regression evidence.

## Recommended next command checklist

```bash
rtk git status --short --branch
rtk go test ./query/sql ./protocol ./subscription -count=1
```

Then inspect touched Go surfaces with `rtk go doc` / `rtk go list -json` before editing.

## Working tree caution

The repo may contain unrelated hosted-runtime planning files and/or broader docs moves. Do not mix those into a TECH-DEBT/OI-002 implementation slice unless the user explicitly asks.
