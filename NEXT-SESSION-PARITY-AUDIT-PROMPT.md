Continue Shunter in a fresh agent session.

This is a parity-audit pass, not an implementation slice.
The goal is to audit the new parity roadmap against the live codebase, the existing audit/debt ledgers, and the current docs so we start execution from a trustworthy plan.

Primary objective
- Audit `docs/spacetimedb-parity-roadmap.md` against:
  - live code
  - `SPEC-AUDIT.md`
  - `TECH-DEBT.md`
  - `REMAINING.md`
  - `docs/current-status.md`
  - `README.md`
- Tighten the roadmap where it is too vague, overclaims, misses important blockers, or sequences work poorly.
- Do not start implementing parity fixes yet unless a tiny doc-only correction to support the audit is needed.

What success looks like
- We end the session with a roadmap that is credible enough to drive the next real parity implementation slice.
- We know whether Phase 0 in the roadmap is correct as written.
- We know the best first actual implementation slice after the audit.

Required reading order
1. `AGENTS.md`
2. `RTK.md`
3. `docs/project-brief.md`
4. `docs/EXECUTION-ORDER.md`
5. `README.md`
6. `docs/current-status.md`
7. `docs/spacetimedb-parity-roadmap.md`
8. `REMAINING.md`
9. `TECH-DEBT.md`
10. `SPEC-AUDIT.md`

Then inspect the main live implementation packages:
- `protocol/`
- `subscription/`
- `executor/`
- `commitlog/`
- `store/`
- `schema/`
- `types/`
- `bsatn/`

Audit questions to answer
1. Is the roadmap’s definition of parity sharp enough?
- Does it distinguish outcome-equivalence from internal implementation similarity clearly enough?
- Are there important externally visible behaviors missing from the parity definition?

2. Is the gap inventory honest and complete enough?
- Does Tier A really capture the biggest reasons Shunter is not yet operationally equivalent to SpacetimeDB?
- Are there major live-code blockers that the roadmap underweights or omits?
- Are any listed gaps actually lower-value than the roadmap claims?

3. Is the sequencing right?
- Is `Phase 0 — parity harness` the correct first move?
- Is protocol really the best first parity surface, or is there a stronger case for subscription/runtime first?
- Are any phases too broad and in need of subdivision?

4. Are the file touchpoints grounded?
- Do the listed code surfaces really correspond to the stated gaps?
- Are important files missing?
- Are any listed files not actually central?

5. Is the roadmap implementable by a follow-on agent?
- Can someone pick Phase 0 and know what to do?
- Are the acceptance gates concrete enough?
- Does the roadmap need a tighter appendix/checklist/ledger to support real execution?

Hard rules
- Use RTK for every shell/git command.
- This is an audit/revision session, not a code-implementation session.
- Prefer tightening the roadmap and surrounding docs over making code changes.
- Do not soften the parity goal into generic cleanup/productization.
- Do not start a fresh broad architectural essay; keep the work grounded in current docs and code.
- Do not rewrite the project brief unless a contradiction absolutely forces it.

Expected workflow
1. Read the roadmap in full.
2. Read the supporting status/debt/audit docs in full or in the relevant sections.
3. Inspect live code in the packages named by the roadmap.
4. Check whether the roadmap’s claimed priority areas are actually the highest-leverage ones.
5. Patch `docs/spacetimedb-parity-roadmap.md` wherever it is vague, overstated, incomplete, or poorly sequenced.
6. Patch `README.md` / `docs/current-status.md` only if the roadmap audit reveals they now misframe the next work.
7. End with a concise note naming:
   - whether Phase 0 is still the right first move
   - the best first actual implementation slice after the audit
   - any newly discovered blocker or reframing

Specific things to verify in code
- Protocol:
  - subprotocol negotiation
  - message tags / message families
  - compression behavior
  - reducer-result vs tx-update split
  - close/lifecycle behavior
- Subscription:
  - query representation and registration model
  - fanout path
  - lag/backpressure handling
  - confirmed-read / durable delivery path
- Executor/runtime:
  - reducer result surfaces
  - lifecycle and scheduling seams
  - post-commit routing into subscriptions/protocol
- Durability/store:
  - replay / recovery / snapshot behavior
  - tx numbering and sequencing
  - row/value capability boundaries

Likely files worth reading early
- `protocol/upgrade.go`
- `protocol/client_messages.go`
- `protocol/server_messages.go`
- `protocol/compression.go`
- `protocol/send_txupdate.go`
- `protocol/send_reducer_result.go`
- `protocol/lifecycle.go`
- `subscription/register.go`
- `subscription/eval.go`
- `subscription/fanout.go`
- `subscription/fanout_worker.go`
- `subscription/predicate.go`
- `executor/executor.go`
- `executor/scheduler.go`
- `commitlog/recovery.go`
- `commitlog/replay.go`
- `commitlog/segment.go`
- `store/recovery.go`
- `store/snapshot.go`
- `types/value.go`
- `bsatn/encode.go`
- `bsatn/decode.go`

Suggested verification commands
- `rtk go test ./...`
- targeted package runs while reasoning:
  - `rtk go test ./protocol`
  - `rtk go test ./subscription`
  - `rtk go test ./executor`
  - `rtk go test ./commitlog`
  - `rtk go test ./store`

Expected deliverable
- a tightened `docs/spacetimedb-parity-roadmap.md`
- any necessary supporting doc updates in `README.md` and/or `docs/current-status.md`
- a concise final audit summary with:
  - roadmap verdict: usable / needs more work
  - strongest corrections made
  - best first implementation slice after the audit

Stop rule
- Stop when the roadmap has been audited and tightened enough that the next session can use it as the active development driver.
- Do not pivot into implementing parity fixes in this session.