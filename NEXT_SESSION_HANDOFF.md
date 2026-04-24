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

Do not reopen the closed P0-SUBSCRIPTION-001 through P0-SUBSCRIPTION-028 rows without new failing regression evidence.

`P0-SUBSCRIPTION-028` is the latest OI-002 runtime/fanout slice. A fresh post-V1 scout found that `subscription/fanout_worker.go` delivered evaluation-origin `SubscriptionError` messages before checking `TxDurable`, while normal updates already honored the public/default confirmed-read gate.

Latest closed OI-002 slice:

- `P0-SUBSCRIPTION-028`: post-commit `SubscriptionError` fan-out now waits on `TxDurable` for default/public confirmed-read recipients, using the same durability gate as normal transaction updates.

Behavior now pinned:

- `TestFanOutWorker_SubscriptionError_PublicProtocolDefault_WaitsForDurability` fails if an evaluation-origin `SubscriptionError` is delivered before `TxDurable` is ready
- error-before-update ordering is preserved after durability is ready; the fix only moves error delivery behind the confirmed-read gate
- this closes the carried runtime/fanout candidate "Confirmed-read durability gating for `SubscriptionError`"

Previous latest query-only slice:

- `P0-SUBSCRIPTION-027`: one-off and subscription SQL accept bounded `JOIN ... ON col = col AND <qualified-column op literal>` on the existing two-table join surface by transparently folding the ON-extracted filter into the already-supported WHERE-form. Use this subscribe-widening precedent only when the new parser shape is provably identical to an already-accepted form.

Primary files touched by the latest OI-002 work:

- `subscription/fanout_worker.go`
- `subscription/fanout_worker_test.go`
- `TECH-DEBT.md`
- `docs/parity-phase0-ledger.md`
- `docs/spacetimedb-parity-roadmap.md`
- `NEXT_SESSION_HANDOFF.md`

Latest validation reported for that slice:

- `rtk go test ./subscription -run TestFanOutWorker_SubscriptionError_PublicProtocolDefault_WaitsForDurability -count=1 -v`
- `rtk go test ./subscription -run 'FanOutWorker|SubscriptionError' -count=1`
- `rtk go test ./subscription -count=1`
- `rtk go vet ./subscription`
- `rtk go test ./... -count=1`

Before calling the next slice done, still run the appropriate touched-package tests and prefer `rtk go test ./... -count=1` when time allows.

## Good next OI-002 candidates

Choose from fresh live evidence. The next bounded candidate should be chosen by scouting live code/docs/tests after the `P0-SUBSCRIPTION-028` landing; do not carry forward older candidate notes without re-verification.

Candidates carried forward from prior handoffs:

1. Runtime/fanout lanes.
   - QueryID-level fanout correlation / SubscriptionID wire cleanup.
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
