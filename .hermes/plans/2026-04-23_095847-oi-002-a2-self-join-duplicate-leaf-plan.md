# OI-002 A2 self-join alias-sensitive join-filter duplicate-leaf idempotence implementation plan

> Planning only. Re-validate against the live handoff, worktree, and code before implementation. This plan supersedes `.hermes/plans/2026-04-23_091858-oi-002-a2-self-join-associative-grouping-plan.md` and `.hermes/plans/2026-04-23_081725-oi-002-a2-join-filter-child-order-canonicalization.md`, because both target seams are now already closed in live code/tests while `NEXT_SESSION_HANDOFF.md` now points at the next residual.

## Goal

Close the next bounded OI-002 A2 query-identity residual by making already-accepted aliased self-join SQL share one canonical query hash and one shared manager query state when only exact duplicate alias-aware self-join filter leaves differ.

Concrete target shapes:
- `a.id = 1`
- `a.id = 1 AND a.id = 1`
- `a.id = 1 OR a.id = 1`

For accepted aliased self-join queries, these should already have the same visible one-off rows and should now also share canonical hash / manager query-state identity.

## Live context and assumptions

Grounded from the current repo state:
- `NEXT_SESSION_HANDOFF.md` names the exact next task: self-join alias-sensitive join-filter duplicate-leaf idempotence.
- `TECH-DEBT.md`, `docs/spacetimedb-parity-roadmap.md`, and `docs/parity-phase0-ledger.md` all agree that child-order and associative-grouping self-join seams are already closed and the next residual is duplicate-leaf idempotence.
- The worktree is already dirty in docs/plans (`NEXT_SESSION_HANDOFF.md`, `README.md`, several docs, and two existing `.hermes/plans/` files), but no live Go worktree evidence currently suggests this duplicate-leaf slice is already implemented.
- `subscription/hash.go` still fences join-containing predicates out of the broader same-table canonicalization path via `containsJoinLikePredicate(...)` and `canonicalGroupTable(...)`. That fence should remain in place for this batch.
- `subscription/hash.go::canonicalizeSelfJoinFilter(...)` now recursively canonicalizes, flattens same-kind `And`/`Or` groups, and sorts children for self-join filters, but it currently does not dedupe exact duplicate children after sorting.
- `subscription/validate.go::validateJoin(...)` still requires distinct aliases for self-joins, and `validateSelfJoinFilterAliases(...)` still guarantees self-join filter leaves use only the enclosing aliases. That makes canonical child bytes alias-sensitive enough to support bounded duplicate-leaf removal safely inside the self-join-local path.
- `protocol/handle_oneoff_test.go` already contains the closed self-join child-order and associative-grouping visible-row pins, so the new semantic pin should sit immediately beside those tests and reuse the same tiny `t(id,u32)` fixture.
- `subscription/hash_test.go` and `subscription/manager_test.go` already contain the closed self-join child-order and associative-grouping canonical-identity/query-state pins, so the new duplicate-leaf tests should mirror that structure.

## Batch boundary

In scope:
- one public one-off semantic pin proving visible rows are already equal for duplicate-leaf accepted aliased self-join SQL
- one canonical-hash pin for self-join duplicate-leaf `And`/`Or` shapes
- one manager dedup/query-state-sharing pin for the same duplicate-leaf shapes
- minimal `subscription/hash.go` change to remove exact duplicate alias-aware self-join filter children after the existing self-join-local flatten/sort step
- same-session doc follow-through if the slice lands

Out of scope:
- self-join absorption-law reductions
- broader same-table dedupe/absorb reuse for join-containing predicates
- parser widening
- distinct-table join work
- rows-shape reopen
- OI-001 / OI-003 / hardening work

## Relevant files already verified

Planning/reference inputs:
- `RTK.md`
- `README.md`
- `CLAUDE.md`
- `NEXT_SESSION_HANDOFF.md`
- `docs/project-brief.md`
- `docs/EXECUTION-ORDER.md`
- `docs/current-status.md`
- `TECH-DEBT.md`
- `docs/spacetimedb-parity-roadmap.md`
- `docs/parity-phase0-ledger.md`

Primary code/test surfaces for this slice:
- `subscription/hash.go`
- `subscription/validate.go`
- `protocol/handle_oneoff_test.go`
- `subscription/hash_test.go`
- `subscription/manager_test.go`

## Proposed implementation approach

Keep the broader join fence unchanged and extend only the self-join-local canonicalization path.

Why this is the right seam:
- The generic same-table path (`flattenCanonical*` + `dedupeCanonicalPredicates` + `absorbCanonicalPredicates`) is intentionally fenced off from join-containing predicates because its grouping logic is alias-blind at the join boundary.
- The self-join-specific path already owns the accepted alias-aware normalization seam for child-order and associative grouping.
- Duplicate-leaf idempotence is the next smallest safe extension of that same path: flatten same-kind children, sort by canonical bytes that already encode alias identity, then drop byte-identical neighbors before rebuilding.

## Step-by-step plan

### 1. Add the public semantic pin first

Objective:
Prove the live seam is still query identity only, not runtime row semantics.

File:
- `protocol/handle_oneoff_test.go`

Add a focused test immediately after the existing self-join grouping test.

Suggested test name:
- `TestHandleOneOffQuery_AliasedSelfJoinFilterDuplicateLeafVisibleRowsMatch`

Suggested query set:
- `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1`
- `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 AND a.id = 1`
- `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 OR a.id = 1`

Fixture/expectations:
- Reuse the same tiny `t(id,u32)` schema with `idx_t_u32` and the same three rows used by the adjacent self-join tests.
- Assert all queries succeed.
- Assert all three queries return the same visible projected `a.*` rows in the same order.
- Keep the assertion narrow: visible semantics already match; no production edit should be required to make this test pass.

First validation command:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol -run 'TestHandleOneOffQuery_AliasedSelfJoinFilterDuplicateLeafVisibleRowsMatch' -count=1`

Expected before implementation:
- PASS immediately.

Stop condition:
- If this fails, stop and re-scout; the seam is not just canonical identity anymore.

### 2. Add the failing canonical-hash pin

Objective:
Show that duplicate-leaf self-join filters still hash differently today solely because the self-join canonicalizer rebuilds every child without deduping exact duplicates.

File:
- `subscription/hash_test.go`

Add a focused test after the existing self-join grouping test.

Suggested test name:
- `TestQueryHashSelfJoinFilterDuplicateLeafCanonicalized`

Suggested predicate setup:
- base leaf:
  - `a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}`
- duplicate `And` form:
  - `Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: a, Right: a}}`
- duplicate `Or` form:
  - same join, but `Filter: Or{Left: a, Right: a}`
- single-leaf baseline:
  - same join, but `Filter: a`

Primary assertions:
- `ComputeQueryHash(single, nil) == ComputeQueryHash(andDup, nil)`
- `ComputeQueryHash(single, nil) == ComputeQueryHash(orDup, nil)`

Required guardrail:
- Keep an alias-drift negative assertion in the same test: e.g. replace one duplicate with `Alias: 1` and assert the hash changes.
- Do not add absorption expectations here.

First validation command:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestQueryHashSelfJoinFilterDuplicateLeafCanonicalized' -count=1`

Expected before implementation:
- FAIL.

### 3. Add the failing manager dedup/query-state pin

Objective:
Prove registration still forks query state when the only difference is redundant duplicate self-join leaves.

File:
- `subscription/manager_test.go`

Add a focused test after the existing self-join grouping manager test.

Suggested test name:
- `TestRegisterSet_SelfJoinFilterDuplicateLeafSharesQueryState`

Suggested fixture shape:
- Reuse the same tiny fake schema already used in the adjacent self-join manager tests:
  - table `1`
  - columns `id`, `u32`
  - index on join column `u32`
  - three committed rows with repeated `u32` values
- Register the single-leaf self-join under one connection/query ID.
- Register a duplicate-leaf variant under a second connection/query ID.
- Prefer one test covering `And` first; add `Or` in the same test only if it stays tight and readable.

Assertions:
- both registrations succeed
- `len(mgr.registry.byHash) == 1`
- hashes match
- shared query state has `refCount == 2`

First validation command:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestRegisterSet_SelfJoinFilterDuplicateLeafSharesQueryState' -count=1`

Expected before implementation:
- FAIL.

### 4. Implement the minimal production fix in `subscription/hash.go`

Objective:
Deduplicate exact duplicate self-join filter children without widening into absorption or generic join-containing canonicalization.

File:
- `subscription/hash.go`

Implementation shape:
1. Leave `containsJoinLikePredicate(...)` and `canonicalGroupTable(...)` unchanged.
   - They are still the safety fence preventing the broader alias-blind same-table path from touching join-containing predicates.
2. Keep `canonicalizePredicate(Join)` behavior unchanged except for the fact that self-joins still route `p.Filter` through `canonicalizeSelfJoinFilter(...)`.
3. In `canonicalizeSelfJoinFilter(...)`:
   - recurse into children first, exactly as today
   - flatten same-kind `And` or `Or` children with the existing self-join-local helpers
   - sort children via `sortCanonicalPredicates(...)`
   - add a new self-join-local dedupe step by reusing `dedupeCanonicalPredicates(...)` or an equivalent helper immediately after sorting
   - rebuild deterministically with `rebuildCanonicalAnd(...)` / `rebuildCanonicalOr(...)`
4. Preserve nil-child behavior exactly as it works today.
5. Do not add absorption reductions in the self-join path in this batch.
6. Do not change distinct-table join behavior.

Why this should be safe:
- canonical child bytes already include alias identity (`Alias` is encoded in `ColEq`, `ColNe`, `ColRange` canonical bytes)
- `validateSelfJoinFilterAliases(...)` keeps self-join leaves constrained to the two enclosing aliases
- exact duplicate removal after flatten+sort is strictly narrower than absorption and preserves the intended accepted-shape semantics under test

Things explicitly not to do:
- do not route self-join filters through `canonicalGroupTable(...)`
- do not enable generic same-table absorb logic for self-joins
- do not widen into alias-blind reductions or parser/runtime behavior changes

### 5. Validate in narrow-first order

Run in this order:
1. Focused one-off semantic pin:
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol -run 'TestHandleOneOffQuery_AliasedSelfJoinFilterDuplicateLeafVisibleRowsMatch' -count=1`
2. Focused hash + manager pins:
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestQueryHashSelfJoinFilterDuplicateLeafCanonicalized|TestRegisterSet_SelfJoinFilterDuplicateLeafSharesQueryState' -count=1`
3. Format touched packages:
   - `PATH=/usr/local/go/bin:$PATH rtk go fmt ./protocol ./subscription`
4. Re-run touched package suites:
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol ./subscription -count=1`
5. Vet touched packages:
   - `PATH=/usr/local/go/bin:$PATH rtk go vet ./protocol ./subscription`
6. Broad suite:
   - `PATH=/usr/local/go/bin:$PATH rtk go test ./... -count=1`

Expected after implementation:
- all targeted tests pass
- touched packages pass
- full suite passes

## Documentation follow-through if the slice lands

Update in the same implementation session:
- `TECH-DEBT.md`
- `docs/spacetimedb-parity-roadmap.md`
- `docs/parity-phase0-ledger.md`
- `NEXT_SESSION_HANDOFF.md`

Doc changes to make:
- mark self-join alias-sensitive duplicate-leaf idempotence as closed and pinned
- list the new authoritative tests in protocol/hash/manager surfaces
- keep OI-002 open overall
- point the next handoff at the next bounded residual only after a fresh scout
- likely next residual, if nothing else appears during implementation, is self-join alias-sensitive absorption-law reduction; but do not commit to that in docs unless the live repo still supports it after this slice lands

## Risks and watchpoints

Primary risk:
- accidentally widening into absorption or the generic join-containing same-table pipeline

Secondary risks:
- a semantic one-off mismatch appears, meaning the issue is not limited to query identity
- dedup helper use changes nil/degenerate tree behavior unexpectedly
- adding both `And` and `Or` in one test obscures which shape actually failed

Mitigations:
- keep the first semantic pin public and isolated
- keep hash/manager tests tiny and alias-aware
- make the production change only at the self-join-local flatten/sort/rebuild seam

## Deliverable summary

If execution follows this plan cleanly, the slice should land as:
- one new public one-off visible-row parity pin
- one new hash canonicalization pin
- one new manager query-state-sharing pin
- one bounded `subscription/hash.go` self-join dedupe change
- same-session parity doc + handoff updates
