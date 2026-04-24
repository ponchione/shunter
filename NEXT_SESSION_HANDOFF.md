# Next session handoff

Use this file to start the next agent on the current Shunter TECH-DEBT / parity task with no prior chat context.

This file is not the hosted-runtime planning handoff. Hosted-runtime planning now lives in:

- `HOSTED_RUNTIME_PLANNING_HANDOFF.md`

## Current objective

Continue working through `TECH-DEBT.md`, prioritizing externally visible parity gaps first.

Current active TECH-DEBT issue for handoff purposes:

- `OI-002`: query and subscription behavior still diverges from the target runtime model.

`OI-002` remains open after the latest one-off cross-join `WHERE` + `COUNT(*)` aggregate combination closure. The next bounded A2 batch should be chosen from fresh live evidence, not by reopening a just-closed SQL, fanout-ordering, or QueryID wire-correlation slice.

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

Do not reopen the closed P0-SUBSCRIPTION-001 through P0-SUBSCRIPTION-032 rows without new failing regression evidence.

`P0-SUBSCRIPTION-032` is the latest OI-002 query/subscription slice. A fresh scout found that after P0-SUBSCRIPTION-024 (cross-join `WHERE` column-equality), P0-SUBSCRIPTION-031 (cross-join `WHERE` equality-plus-literal-filter), and P0-SUBSCRIPTION-025 (join-backed `COUNT(*) [AS] alias`) had all closed, the bounded combination of cross-join `WHERE` + `COUNT(*)` aggregate on the existing two-table join surface was already latently admitted by the parser and compile seam but had no end-to-end pin. The slice adds parser + one-off + subscribe-rejection pins without any production code change; the combination behavior is now locked by explicit tests.

Latest closed OI-002 slice:

- `P0-SUBSCRIPTION-032`: one-off/ad hoc SQL now accepts the bounded combination of cross-join `WHERE` column-equality (with optional one qualified column-literal filter) and join-backed `COUNT(*) [AS] alias` aggregate projection, for example `SELECT COUNT(*) AS n FROM t JOIN s WHERE t.id = s.t_id AND s.active = TRUE`. The parser preserves aggregate metadata on the cross-join WHERE shape, one-off routes the query through the existing `subscription.Join` evaluator with matched-pair counting into one uint64 aggregate row using the requested alias, and subscribe rejects with "aggregate projections not supported for subscriptions" (the aggregate guard fires before the cross-join `WHERE` guard).

Behavior now pinned:

- `query/sql/parser_test.go::TestParseJoinCountStarAliasProjectionOnCrossJoinWhereEquality` pins the parser aggregate metadata + `ColumnComparisonPredicate` structure on the equality-only cross-join WHERE shape
- `query/sql/parser_test.go::TestParseJoinCountStarBareAliasProjectionOnCrossJoinWhereEqualityAndFilter` pins the parser aggregate metadata + `AndPredicate{ColumnComparisonPredicate, ComparisonPredicate}` structure on the equality-plus-filter shape with bare alias
- `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_ParityJoinCountAliasOnCrossJoinWhereEqualityReturnsAggregate` fails if one-off rejects the shape or miscounts matched pairs
- `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_ParityJoinCountBareAliasOnCrossJoinWhereEqualityAndFilterReturnsAggregate` fails if one-off ignores the literal filter or mis-projects the aggregate
- `protocol/handle_subscribe_test.go::TestHandleSubscribeSingle_JoinCountAggregateOnCrossJoinWhereStillRejected` fails if subscribe accepts the combination or surfaces an unexpected rejection message

Previous latest query/subscription slices:

- `P0-SUBSCRIPTION-031`: one-off/ad hoc SQL accepts `SELECT t.* FROM t JOIN s WHERE t.u32 = s.u32 AND s.enabled = TRUE` shapes with cross-join `WHERE` equality plus one literal filter; subscribe still rejects cross-join `WHERE`.
- `P0-SUBSCRIPTION-030`: subscription updates retain manager-internal `SubscriptionID` only inside the subscription package; fanout / protocol carry the client `QueryID`.
- `P0-SUBSCRIPTION-029`: evaluator-produced fanout is stabilized per connection by internal subscription-registration/SubscriptionID order before fanout worker handoff.

Primary files touched by the latest OI-002 work:

- `query/sql/parser_test.go`
- `protocol/handle_oneoff_test.go`
- `protocol/handle_subscribe_test.go`
- `TECH-DEBT.md`
- `docs/parity-phase0-ledger.md`
- `docs/spacetimedb-parity-roadmap.md`
- `NEXT_SESSION_HANDOFF.md`

Latest validation reported for that slice:

- `rtk go test ./query/sql -run 'TestParseJoinCountStarAliasProjectionOnCrossJoinWhereEquality|TestParseJoinCountStarBareAliasProjectionOnCrossJoinWhereEqualityAndFilter' -count=1 -v`
- `rtk go test ./protocol -run 'TestHandleOneOffQuery_ParityJoinCountAliasOnCrossJoinWhereEqualityReturnsAggregate|TestHandleOneOffQuery_ParityJoinCountBareAliasOnCrossJoinWhereEqualityAndFilterReturnsAggregate|TestHandleSubscribeSingle_JoinCountAggregateOnCrossJoinWhereStillRejected' -count=1 -v`
- `rtk go fmt ./query/sql ./protocol`
- `rtk go vet ./query/sql ./protocol`
- `rtk go test ./query/sql ./protocol ./subscription ./executor -count=1`
- `rtk go test ./... -count=1`

Before calling the next slice done, still run the appropriate touched-package tests and prefer `rtk go test ./... -count=1` when time allows.

## Good next OI-002 candidates

Choose from fresh live evidence. The next bounded candidate should be chosen by scouting live code/docs/tests after the `P0-SUBSCRIPTION-032` landing; do not carry forward older candidate notes without re-verification.

Candidates carried forward from prior handoffs:

1. Runtime/fanout lanes.
   - Choose only from fresh evidence; the known QueryID-level fanout/protocol correlation and deterministic per-connection ordering candidates are closed.

2. Row-level security / per-client filtering.
   - This remains real but is too large to mix with a narrow SQL or fanout slice unless the user explicitly requests that broader work.

3. A TBD parser/compile seam continuation, to be chosen from fresh scout.
   - Use this route only when the parity claim is exactly bounded and reference-backed.
   - Do not reopen the now-closed projection-family, unindexed-join, cross-join `WHERE` equality/equality-plus-filter, cross-join `WHERE` + `COUNT(*)` combination, join-backed count aggregate, JOIN ON filter, SubscriptionError durability-gating, per-connection ordering, or QueryID fanout/protocol targets without new failing regression evidence.
   - During the P0-SUBSCRIPTION-032 scout, verified that reference's SQL parser rejects `SELECT DISTINCT ...` (sql.rs `distinct: None` match), supports only `COUNT(*) AS alias` aggregate (rejecting `SUM`, `COUNT(DISTINCT col)`), and errors hard on duplicate-QueryID subscribe + non-existent-QueryID unsubscribe — so those are NOT parity gaps. The parser agent's initial "subscribe-duplicate-idempotent" / "unsubscribe-nonexistent-idempotent" / "sender-caller-filtering" candidates were all wrong against the reference; do not reopen them without fresh counter-evidence.

## Recommended next command checklist

```bash
rtk git status --short --branch
rtk go test ./query/sql ./protocol ./subscription -count=1
```

Then inspect touched Go surfaces with `rtk go doc` / `rtk go list -json` before editing.

## Working tree caution

The repo may contain unrelated hosted-runtime planning files and/or broader docs moves. Do not mix those into a TECH-DEBT/OI-002 implementation slice unless the user explicitly asks.
