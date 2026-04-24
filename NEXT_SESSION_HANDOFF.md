# Next Session Handoff

Use this file to start the next parity / TECH-DEBT agent with no prior chat context.

Hosted-runtime planning uses `HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead.

## Startup

Required reading before editing:

1. `RTK.md`
2. This file

Then inspect live code with Go tools:

```bash
rtk go doc ./subscription.Manager
rtk go list -json ./subscription ./protocol ./query/sql
```

Open `TECH-DEBT.md` only if you need the broader backlog. Open `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` or `docs/decomposition/005-protocol/SPEC-005-protocol.md` only for a specific contract question.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Current Objective

Scout for the next bounded OI-002 / Tier A2 residual. There is no queued target. Recent closures removed the previously queued items:

- `P0-SUBSCRIPTION-033` (one-off `COUNT(*) [AS] alias` + `LIMIT` aggregate composition) closed in `700af6c`.
- Same-connection reused subscription-hash initial-snapshot elision closed in the follow-up commit; pinned by `subscription/register_set_test.go::TestRegisterSetSameConnectionReusedHashEmitsEmptyUpdate` and `TestRegisterSetCrossConnectionReusedHashStillEmitsInitialSnapshot`.

Do not reopen `P0-SUBSCRIPTION-001` through `P0-SUBSCRIPTION-033` or the reused-hash elision without fresh failing regression evidence.

## Scout Prompts

Before starting implementation, answer one of these with live-code evidence:

- Is there a reference subscribe/unsubscribe error-path behavior that Shunter still diverges on (e.g., unsubscribe of unknown hash vs unknown client QueryID, mid-batch failure rollback visibility to clients, shape of `SubscriptionError` on cross-connection fan-out)?
- Is there a bounded SQL surface shape that is latently admitted by parser+compile but has no end-to-end pin (check for parser/predicate combinations the repo already lowers but has no one-off or subscribe pin for)?
- Is there a post-commit fanout ordering, QueryID stamping, or durability-gate edge case that is not yet pinned for a specific combined scenario (multi-subscription-per-connection with partial overlap, self-join aliased delta under concurrent update, reused-hash attachment receiving first delta)?

Pick the smallest bounded slice with a reference anchor and a concrete failing test you can write first.

## Out Of Scope

- SQL surface widening beyond what the parser already admits
- Fanout/QueryID correlation redesign (closed under `P0-SUBSCRIPTION-030`)
- Reopening closed parity rows without fresh failing evidence
- Non-OI-002 tech-debt

## Validation

```bash
rtk go test <touched packages> -count=1 -v
rtk go fmt <touched packages>
rtk go vet <touched packages>
rtk go test ./... -count=1
```

## Doc Follow-Through

After the implementation is green:
- update `docs/parity-phase0-ledger.md` only if the new closure changes current truth
- update `TECH-DEBT.md::OI-002` summary only if the closure removes a risk listed there
- rewrite this handoff to the next live target, keeping startup reading minimal
