# Next Session Handoff

Use this file to start the next Shunter correctness / TECH-DEBT agent with no prior chat context.

Hosted-runtime planning uses `HOSTED_RUNTIME_PLANNING_HANDOFF.md` instead.

## Startup

Required reading before editing:

1. `RTK.md`
2. This file

Then inspect live code with Go tools:

```bash
rtk go list -json ./query/sql ./protocol ./schema ./subscription
rtk go doc ./query/sql
rtk go doc ./query/sql.Statement
rtk go doc ./schema.TableSchema.Column
rtk go doc ./schema.SchemaLookup
```

Open `TECH-DEBT.md` only if you need the broader backlog. Open `docs/decomposition/004-subscriptions/SPEC-004-subscriptions.md` or `docs/decomposition/005-protocol/SPEC-005-protocol.md` only for a specific contract question.

Use `rtk` for every shell command, including git. Do not push unless explicitly asked.

## Current Objective

Project framing was clarified on 2026-04-26:

- SpacetimeDB is an architectural reference, not a wire/client/business compatibility target.
- Shunter is for self-hosted / personally operated apps with Shunter-owned Go APIs and clients.
- Energy accounting is not a Shunter product goal; the protocol no longer carries energy fields or an `OutOfEnergy` outcome arm.
- OI-001 Shunter-native protocol cleanup is narrowed to conditional follow-ups: strict decoder body consumption is pinned, subscribe/unsubscribe response shaping is consolidated, and only `v1.bsatn.shunter` is accepted.
- Use reference behavior as evidence for runtime semantics, but prefer Shunter's own simpler contract when compatibility-only details add cost without value.

No fixed implementation slice is queued. OI-002 has no current open runtime-model work after the latest evidence-driven scout and fixture dedup pass.

Reopen OI-002 only if a future user or failing test provides a fresh Shunter-visible regression:

- wrong accepted/rejected query
- wrong one-off rows or subscription rows
- misleading user-visible validation error
- one-off/subscription drift on shared syntax or type semantics

Completed OI-002 history belongs in tests and git history, not this handoff. Do not reopen exact identifier lookup, join-WHERE policy, structured-query protocol cleanup, or one-off cross-join explicit mixed projection without a fresh failing example.

## Confirmed Work Queue

For any future OI-002 regression, add the failing Shunter-visible test first, then implement. Batch only when the locus overlaps; commit per slice if the working tree is clean enough to do so without sweeping unrelated user changes into the commit. If proof is needed, use `rtk git log`, `rtk git show`, and the relevant tests instead of expanding this handoff with closure archaeology.

## Out Of Scope

- SQL surface widening beyond what the parser already admits
- Fanout/QueryID correlation redesign
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

After any future implementation is green:

- update `TECH-DEBT.md::OI-002` only if new evidence reopens or closes runtime-model work
- rewrite this handoff to the next live target, keeping startup reading minimal and only future-relevant state
