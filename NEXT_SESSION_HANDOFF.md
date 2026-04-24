# Next session handoff

Use this file to start the next agent on the current Shunter TECH-DEBT / parity task with no prior chat context.

This file is not the hosted-runtime planning handoff. Hosted-runtime planning now lives in:

- `HOSTED_RUNTIME_PLANNING_HANDOFF.md`

## Current objective

Continue working through `TECH-DEBT.md`, prioritizing externally visible parity gaps first.

Current active TECH-DEBT issue for handoff purposes:

- `OI-002`: query and subscription behavior still diverges from the target runtime model.

`OI-002` remains open after the latest QueryID fanout/protocol closure. The next bounded A2 batch should be chosen from fresh live evidence, not by reopening a just-closed SQL, fanout-ordering, or QueryID wire-correlation slice.

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

Do not reopen the closed P0-SUBSCRIPTION-001 through P0-SUBSCRIPTION-030 rows without new failing regression evidence.

`P0-SUBSCRIPTION-030` is the latest OI-002 runtime/protocol slice. A fresh scout found that `protocol.SubscriptionUpdate` still exposed the subscription manager's internal `SubscriptionID` on the wire, while the client-visible correlator should be the `QueryID` chosen in `SubscribeSingle` / `SubscribeMulti`.

Latest closed OI-002 slice:

- `P0-SUBSCRIPTION-030`: subscription updates now retain manager-internal `SubscriptionID` only inside the subscription package, while initial subscribe snapshots, post-commit fanout, final unsubscribe deltas, protocol adapters, and protocol encode/decode stamp/carry the client `QueryID` visible to clients.

Behavior now pinned:

- `subscription/eval_test.go::TestEvalFanoutCarriesClientQueryIDForEachSubscription` fails if eval fanout stamps internal `SubscriptionID` instead of the client `QueryID`
- `protocol/fanout_adapter_test.go::TestEncodeSubscriptionUpdate_CarriesClientQueryID` fails if protocol projection exposes `SubscriptionID` or ignores `QueryID`
- protocol server-message round trips and applied/light/heavy row-shape pins now serialize the first `SubscriptionUpdate` field as `QueryID`
- this closes the carried runtime/fanout candidate "QueryID-level fanout correlation / SubscriptionID wire cleanup"

Previous latest runtime/fanout slice:

- `P0-SUBSCRIPTION-029`: evaluator-produced fanout is stabilized per connection by internal subscription-registration/SubscriptionID order before fanout worker handoff and before caller-update capture.

Primary files touched by the latest OI-002 work:

- `subscription/manager.go`
- `subscription/query_state.go`
- `subscription/register_set.go`
- `subscription/eval.go`
- `subscription/eval_test.go`
- `protocol/wire_types.go`
- `protocol/server_messages.go`
- `protocol/fanout_adapter.go`
- `protocol/fanout_adapter_test.go`
- `executor/protocol_inbox_adapter.go`
- `TECH-DEBT.md`
- `docs/parity-phase0-ledger.md`
- `docs/spacetimedb-parity-roadmap.md`
- `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md`
- `docs/decomposition/005-protocol/SPEC-005-protocol.md`
- `docs/parity-phase2-slice4-rows-shape.md`
- `NEXT_SESSION_HANDOFF.md`

Latest validation reported for that slice:

- `rtk go test ./subscription -run TestEvalFanoutCarriesClientQueryIDForEachSubscription -count=1 -v`
- `rtk go test ./protocol -run TestEncodeSubscriptionUpdate_CarriesClientQueryID -count=1 -v`
- `rtk go test ./protocol -count=1`
- `rtk go test ./subscription -count=1`
- `rtk go test ./executor -count=1`
- `rtk go test ./query/sql ./protocol ./subscription ./executor -count=1`
- `rtk go vet ./subscription ./protocol ./executor`
- `rtk go test ./... -count=1`

Before calling the next slice done, still run the appropriate touched-package tests and prefer `rtk go test ./... -count=1` when time allows.

## Good next OI-002 candidates

Choose from fresh live evidence. The next bounded candidate should be chosen by scouting live code/docs/tests after the `P0-SUBSCRIPTION-030` landing; do not carry forward older candidate notes without re-verification.

Candidates carried forward from prior handoffs:

1. Runtime/fanout lanes.
   - Choose only from fresh evidence; the known QueryID-level fanout/protocol correlation and deterministic per-connection ordering candidates are closed.

2. Row-level security / per-client filtering.
   - This remains real but is too large to mix with a narrow SQL or fanout slice unless the user explicitly requests that broader work.

3. A TBD parser/compile seam continuation, to be chosen from fresh scout — for example, additional bounded widenings that can be admitted via transparent parser-level parity against an already-accepted shape (use this route only when the parity claim is exactly as tight as P0-SUBSCRIPTION-027's).

## Recommended next command checklist

```bash
rtk git status --short --branch
rtk go test ./query/sql ./protocol ./subscription -count=1
```

Then inspect touched Go surfaces with `rtk go doc` / `rtk go list -json` before editing.

## Working tree caution

The repo may contain unrelated hosted-runtime planning files and/or broader docs moves. Do not mix those into a TECH-DEBT/OI-002 implementation slice unless the user explicitly asks.
