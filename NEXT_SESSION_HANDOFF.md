# Next session handoff

Use this file to start the next agent on the next real Shunter parity step with no prior context.

For provenance of closed slices, use `rtk git log`. This file tracks current state and the next concrete batch only.

## Current state

- OI-001 A1 wire-close slices identified so far are closed and pinned in `protocol/parity_*_test.go`.
- Phase 2 Slice 4 rows-shape wrapper-chain parity remains a documented divergence. Do not reopen it without a new decision doc that also reopens SPEC-005 §3.4 row-list format work.
- `cmd/shunter-example` still provides the working bootstrap path and remains anonymous-auth only.
- OI-002 / Tier A2 remains the next active execution issue.
- The self-join alias-sensitive join-filter child-order slice is now closed across one-off semantics, canonical query hashing, and manager query-state sharing.

## OI-002 A2 slices already closed

Do not reopen these without fresh evidence:
- fan-out durability gating + dropped-client cleanup on eval failure
- join/cross-join multiplicity across compile/hash/bootstrap/one-off/delta
- one-off vs subscribe unindexed-join admission parity via shared `subscription.ValidatePredicate(...)`
- committed join bootstrap/final-delta projected-side ordering
- projected-join delta ordering
- subscribe-side `:sender` hash identity / mixed-batch parameterization provenance
- neutral-`TRUE` normalization across compile/hash/register seams
- same-table canonicalization family:
  - commutative child order
  - associative grouping
  - duplicate-leaf idempotence
  - absorption-law reduction
- overlength SQL admission rejection before recursive compile work
- bare/grouped `FALSE` follow-through across parse/normalize/hash/bootstrap/one-off/eval
- distinct-table join-filter child-order canonicalization
- self-join alias-sensitive join-filter child-order canonicalization

## Next batch: self-join alias-sensitive join-filter associative-grouping canonicalization

Take this exact slice next.

Problem statement:
- accepted aliased self-join SQL now compiles, executes, and shares canonical identity when only immediate filter child order changes
- `subscription/hash.go` still keeps join-containing predicates out of the broader same-table flatten/dedupe/absorb pipeline via `containsJoinLikePredicate(...)` / `canonicalGroupTable(...)`
- the new self-join helper only reorders the two immediate children of self-join `And` / `Or` nodes
- that means grouped self-join filters such as `(a.id = 1 AND a.id > 0) AND a.id < 10` vs `a.id = 1 AND (a.id > 0 AND a.id < 10)` can still hash/register differently even though visible one-off row semantics should match

Why this is the next actionable seam:
- it stays inside OI-002 A2's accepted-shape normalization / runtime-identity family
- it directly follows the now-closed self-join child-order slice without widening into alias-blind reductions
- live code already isolates self-join filter canonicalization in one bounded seam inside `subscription/hash.go`

## Grounded evidence to re-read before editing

Read these first:
- `subscription/hash.go`
- `subscription/validate.go`
- `protocol/handle_oneoff_test.go`
- `subscription/hash_test.go`
- `subscription/manager_test.go`
- `TECH-DEBT.md`
- `docs/spacetimedb-parity-roadmap.md`
- `docs/parity-phase0-ledger.md`

Specific live facts:
- `subscription/hash.go`
  - `containsJoinLikePredicate(...)` still makes `canonicalGroupTable(...)` return false for join-containing predicates, so the broader flatten/sort/dedupe/absorb pipeline still does not apply to self-join filters
  - `canonicalizeSelfJoinFilter(...)` only canonicalizes recursively and reorders the two immediate children of self-join `And` / `Or` nodes by canonical bytes
- `subscription/validate.go`
  - `validateJoin(...)` still enforces distinct aliases for self-joins
  - `validateSelfJoinFilterAliases(...)` still guarantees each self-join filter leaf alias matches `Join.LeftAlias` or `Join.RightAlias`
- `protocol/handle_oneoff_test.go`
  - `TestHandleOneOffQuery_AliasedSelfJoinFilterChildOrderVisibleRowsMatch` now proves visible rows were already equal for the child-order-only accepted shape
- `subscription/hash_test.go`
  - `TestQueryHashSelfJoinFilterChildOrderCanonicalized` now pins the closed immediate-child ordering seam
- `subscription/manager_test.go`
  - `TestRegisterSet_SelfJoinFilterChildOrderSharesQueryState` now pins shared registration/query-state reuse for that closed seam

## Batch boundary

In scope:
- canonical query identity for accepted aliased self-join filters when only associative grouping changes
- one-off semantic pin proving visible rows are already equal for the regrouped accepted shape
- hash + manager query-state-sharing pins for that accepted self-join grouped shape
- minimal `subscription/hash.go` change to flatten/rebuild only same-kind self-join filter groups in an alias-aware way

Out of scope:
- self-join duplicate-leaf idempotence
- self-join absorption-law reductions
- parser widening
- distinct-table join changes
- rows-shape reopen
- OI-001 / OI-003 / hardening work

## TDD shape for the next agent

1. Add one public one-off semantic pin first.
   - File: `protocol/handle_oneoff_test.go`
   - Shape: accepted aliased self-join with three same-side leaves where only grouping changes
   - Suggested query pair:
     - `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE (a.id = 1 AND a.id > 0) AND a.id < 2`
     - `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 AND (a.id > 0 AND a.id < 2)`
   - Expected: both succeed and return the same visible rows

2. Add the failing canonical-hash pin.
   - File: `subscription/hash_test.go`
   - Build a self-join `Join{Left: 1, Right: 1, LeftAlias: 0, RightAlias: 1, ...}`
   - Use three leaves on the same alias side only
   - Assert regrouped self-join `And` / `Or` forms share one hash inside `Join.Filter`
   - Keep a negative guard nearby proving alias identity still matters

3. Add the failing manager dedup/query-state pin.
   - File: `subscription/manager_test.go`
   - Register the same accepted self-join twice with grouping A vs B only
   - Assert one query state, not two

4. Implement the minimum fix in `subscription/hash.go`.
   - Keep it bounded to self-join filter associative grouping only
   - Important guardrail: do not simply enable the existing same-table flatten/dedupe/absorb pipeline for all join-containing predicates; it is alias-blind at the grouping seam and broader than this batch
   - Preferred fix shape:
     - recurse through self-join filter children
     - flatten only same-kind self-join-local `And` or `Or` groups
     - sort children by canonical bytes that already include alias identity
     - rebuild one deterministic grouped shape
     - leave duplicate-leaf and absorption reductions for later bounded scouts

5. Validate in this order.
   - focused new tests first
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol ./subscription -count=1`
   - `PATH=/usr/local/go/bin:$PATH rtk go vet ./protocol ./subscription`
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./... -count=1`

## What to update in the same session

If the slice lands, update:
- `TECH-DEBT.md`
- `docs/spacetimedb-parity-roadmap.md`
- `docs/parity-phase0-ledger.md`
- `NEXT_SESSION_HANDOFF.md`

## Out of scope for the next batch

- OI-001 A3 recovery/store parity
- OI-004 / OI-005 / OI-006 hardening
- rows-shape cluster reopen
- strict-auth wiring in `cmd/shunter-example`

## Startup notes

- Read `CLAUDE.md`, then `RTK.md`, then `docs/project-brief.md`, then `docs/EXECUTION-ORDER.md`
- Use `rtk git log` for slice provenance
- Before changing a file, verify against live code rather than trusting stale notes
