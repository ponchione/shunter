# Next session handoff

Use this file to start the next agent on the next real Shunter parity step with no prior context.

For provenance of closed slices, use `rtk git log`. The latest landed slice is:
- `e83216b fix: canonicalize self-join filter grouping`

This file tracks current state and the next concrete batch only.

## Current state

- OI-001 A1 wire-close slices identified so far are closed and pinned in `protocol/parity_*_test.go`.
- Phase 2 Slice 4 rows-shape wrapper-chain parity remains a documented divergence. Do not reopen it without a new decision doc that also reopens SPEC-005 §3.4 row-list format work.
- `cmd/shunter-example` still provides the working bootstrap path and remains anonymous-auth only.
- OI-002 / Tier A2 remains the next active execution issue.
- The self-join alias-sensitive join-filter child-order, associative-grouping, and duplicate-leaf slices are now closed across one-off semantics, canonical query hashing, and manager query-state sharing.

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
- self-join alias-sensitive join-filter associative-grouping canonicalization
- self-join alias-sensitive join-filter duplicate-leaf idempotence canonicalization

## Selected next task

Self-join alias-sensitive join-filter absorption-law reduction.

Take this exact slice next.

## Problem statement

Accepted aliased self-join SQL now compiles, executes, and shares canonical identity when only same-side filter child order, associative grouping, or exact duplicate leaves change.

But `subscription/hash.go` still has the next bounded gap:
- `containsJoinLikePredicate(...)` still keeps join-containing predicates out of the broader same-table flatten/dedupe/absorb pipeline via `canonicalGroupTable(...)`
- `canonicalizeSelfJoinFilter(...)` now flattens, sorts, and dedupes same-kind self-join `And` / `Or` groups, but it still does not remove bounded absorption-equivalent alias-aware shapes
- so accepted self-join filters such as:
  - `a.id = 1`
  - `a.id = 1 OR (a.id = 1 AND a.id > 0)`
  - `a.id = 1 AND (a.id = 1 OR a.id > 0)`
  can still hash/register differently even though visible one-off row semantics should already match

This is the strongest next residual because it stays inside the same accepted-shape normalization/query-identity seam without widening into alias-blind reductions or parser/runtime work.

## Grounded evidence to read first

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
  - `containsJoinLikePredicate(...)` still makes `canonicalGroupTable(...)` return false for join-containing predicates, so the broader same-table canonicalization path still does not apply to self-join filters
  - `canonicalizeSelfJoinFilter(...)` now canonicalizes recursively, flattens same-kind self-join-local groups, sorts children by canonical bytes, and dedupes exact duplicate alias-aware leaves, but it still does not apply absorption reductions
- `subscription/validate.go`
  - `validateJoin(...)` still enforces distinct aliases for self-joins
  - `validateSelfJoinFilterAliases(...)` still guarantees each self-join filter leaf alias matches `Join.LeftAlias` or `Join.RightAlias`
- `protocol/handle_oneoff_test.go`
  - `TestHandleOneOffQuery_AliasedSelfJoinFilterDuplicateLeafVisibleRowsMatch` now proves visible rows are already equal for duplicate-leaf accepted shapes
- `subscription/hash_test.go`
  - `TestQueryHashSelfJoinFilterDuplicateLeafCanonicalized` pins the now-closed duplicate-leaf seam
- `subscription/manager_test.go`
  - `TestRegisterSet_SelfJoinFilterDuplicateLeafSharesQueryState` pins shared registration/query-state reuse for that closed seam

## Batch boundary

In scope:
- canonical query identity for accepted aliased self-join filters when only bounded absorption-equivalent shapes differ
- one-off semantic pin proving visible rows are already equal for those accepted self-join absorption shapes
- hash + manager query-state-sharing pins for those accepted self-join absorption shapes
- minimal `subscription/hash.go` change to remove bounded absorption-equivalent alias-aware self-join filter children after the existing self-join-local flatten/sort/dedupe step

Out of scope:
- parser widening
- distinct-table join changes
- rows-shape reopen
- OI-001 / OI-003 / hardening work

## TDD shape for the next agent

1. Add one public one-off semantic pin first.
   - File: `protocol/handle_oneoff_test.go`
   - Shape: accepted aliased self-join with one bounded absorption-equivalent same-side filter only
   - Suggested query pairs:
     - `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1`
     - `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 OR (a.id = 1 AND a.id > 0)`
     - `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 AND (a.id = 1 OR a.id > 0)`
   - Expected: all succeed and return the same visible rows
   - Run first:
     - `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol -run 'TestHandleOneOffQuery_AliasedSelfJoinFilterAbsorptionVisibleRowsMatch' -count=1`
   - Expected before implementation:
     - PASS immediately

2. Add the failing canonical-hash pin.
   - File: `subscription/hash_test.go`
   - Build a self-join `Join{Left: 1, Right: 1, LeftAlias: 0, RightAlias: 1, ...}`
   - Use one base alias-aware leaf `a` plus one same-side alias-aware leaf `b`
   - Assert the single-leaf form shares a hash with:
     - `Or{Left: a, Right: And{Left: a, Right: b}}`
     - `And{Left: a, Right: Or{Left: a, Right: b}}`
   - Keep a negative guard nearby proving alias identity still matters
   - Run next:
     - `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestQueryHashSelfJoinFilterAbsorptionCanonicalized' -count=1`
   - Expected before implementation:
     - FAIL

3. Add the failing manager dedup/query-state pin.
   - File: `subscription/manager_test.go`
   - Register the same accepted self-join twice with single-leaf vs absorption-equivalent filter only
   - Assert one query state, not two
   - Run next:
     - `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestRegisterSet_SelfJoinFilterAbsorptionSharesQueryState' -count=1`
   - Expected before implementation:
     - FAIL

4. Implement the minimum fix in `subscription/hash.go`.
   - Keep it bounded to self-join filter absorption-law reduction only
   - Important guardrail: do not enable the broader alias-blind same-table absorb pipeline for all join-containing predicates
   - Preferred fix shape:
     - keep the existing self-join-local recurse + flatten + sort + duplicate-dedupe path
     - add a narrow post-dedupe absorption step that removes `And` children absorbed by a sibling leaf in an `Or` group and `Or` children absorbed by a sibling leaf in an `And` group when canonical child bytes already prove alias-aware equality
     - leave any broader boolean/contradiction work for later

5. Validate in this order.
   - focused new tests first
   - `PATH=/usr/local/go/bin:$PATH rtk go fmt ./protocol ./subscription`
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol -run 'TestHandleOneOffQuery_AliasedSelfJoinFilterAbsorptionVisibleRowsMatch' -count=1`
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestQueryHashSelfJoinFilterAbsorptionCanonicalized|TestRegisterSet_SelfJoinFilterAbsorptionSharesQueryState' -count=1`
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol ./subscription -count=1`
   - `PATH=/usr/local/go/bin:$PATH rtk go vet ./protocol ./subscription`
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./... -count=1`

## What to update in the same session

If the slice lands, update:
- `TECH-DEBT.md`
- `docs/spacetimedb-parity-roadmap.md`
- `docs/parity-phase0-ledger.md`
- `NEXT_SESSION_HANDOFF.md`

## Stop conditions

Stop and report instead of widening scope if:
- the one-off semantic pin fails, meaning the seam is not just query identity
- the minimal fix requires generic join-containing canonicalization rather than the self-join-local absorption seam
- the next failure is clearly a different seam family instead of self-join absorption-law reduction

## Startup notes

- Read `CLAUDE.md`, then `RTK.md`, then `docs/project-brief.md`, then `docs/EXECUTION-ORDER.md`
- Use `rtk git log` for slice provenance
- Before changing a file, verify against live code rather than trusting stale notes
