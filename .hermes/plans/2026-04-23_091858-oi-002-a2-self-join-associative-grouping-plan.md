# OI-002 A2 self-join alias-sensitive join-filter associative-grouping canonicalization implementation plan

> For Hermes: planning only. Re-validate against the live handoff, worktree, and code before implementation. Supersedes `.hermes/plans/2026-04-23_081725-oi-002-a2-join-filter-child-order-canonicalization.md`, whose target seam is already closed and no longer matches `NEXT_SESSION_HANDOFF.md`.

Goal: close the next bounded OI-002 A2 query-identity residual by making already-accepted aliased self-join SQL share one canonical query hash and one shared manager query state when only self-join filter associative grouping changes.

Architecture:
- The live accepted-shape seam is already narrowed to self-join `Join.Filter` canonicalization. `canonicalGroupTable(...)` still fences join-containing predicates out of the broader same-table flatten/dedupe/absorb pipeline, and that fence should stay in place for this batch.
- `subscription/hash.go` already has a dedicated self-join path: `canonicalizePredicate(Join)` detects `p.Left == p.Right` and routes `p.Filter` through `canonicalizeSelfJoinFilter(...)`.
- Today that helper only recursively canonicalizes and reorders immediate `And` / `Or` children by canonical bytes. It does not flatten same-kind self-join-local groups, so `(a AND b) AND c` and `a AND (b AND c)` still encode differently.
- The minimal safe fix is to extend the self-join helper with alias-aware flatten/sort/rebuild for same-kind `And` / `Or` groups only, using canonical child bytes that already include alias identity. Do not enable the broader alias-blind same-table dedupe/absorb machinery for join-containing predicates in this slice.

Grounded current context:
- `NEXT_SESSION_HANDOFF.md` explicitly names “Self-join alias-sensitive join-filter associative-grouping canonicalization” as the next task.
- `TECH-DEBT.md`, `docs/spacetimedb-parity-roadmap.md`, and `docs/parity-phase0-ledger.md` all agree that the child-order seam is closed and the next residual is grouping only.
- `subscription/hash.go` currently contains:
  - `containsJoinLikePredicate(...)` / `canonicalGroupTable(...)` fencing join-containing predicates out of the broader same-table canonicalization path.
  - `canonicalizeSelfJoinFilter(...)` with immediate-child-only ordering for `And` / `Or`.
  - existing generic helpers `sortCanonicalPredicates(...)`, `rebuildCanonicalAnd(...)`, and `rebuildCanonicalOr(...)` that can likely be reused.
- `subscription/validate.go` still guarantees self-join alias discipline via:
  - `validateJoin(...)` requiring distinct aliases when `Left == Right`
  - `validateSelfJoinFilterAliases(...)` ensuring every self-join filter leaf alias matches `Join.LeftAlias` or `Join.RightAlias`
- `protocol/handle_oneoff_test.go::TestHandleOneOffQuery_AliasedSelfJoinFilterChildOrderVisibleRowsMatch` already proves the previous residual was identity-only for child-order changes.
- Existing saved plan `.hermes/plans/2026-04-23_081725-oi-002-a2-join-filter-child-order-canonicalization.md` is stale relative to the handoff and should not guide the next slice.

Batch boundary:
- In scope:
  - one-off semantic pin proving visible rows are already equal for regrouped accepted aliased self-join SQL
  - canonical query-hash pin for self-join `And` and/or `Or` regrouping only
  - manager dedup/query-state-sharing pin for the same regrouped accepted shape
  - minimal `subscription/hash.go` change to flatten/rebuild same-kind self-join-local groups while preserving alias identity
- Out of scope:
  - self-join duplicate-leaf idempotence
  - self-join absorption-law reductions
  - enabling the generic same-table flatten/dedupe/absorb pipeline for join-containing predicates
  - distinct-table join changes
  - parser widening
  - rows-shape, OI-001/OI-003, or hardening work

Likely files to modify:
- Modify: `protocol/handle_oneoff_test.go`
- Modify: `subscription/hash_test.go`
- Modify: `subscription/manager_test.go`
- Modify: `subscription/hash.go`
- Later in same implementation session if the slice lands:
  - `TECH-DEBT.md`
  - `docs/spacetimedb-parity-roadmap.md`
  - `docs/parity-phase0-ledger.md`
  - `NEXT_SESSION_HANDOFF.md`

## Task 1: Add the public one-off semantic pin first

Objective: prove the visible one-off row result is already equal when only associative grouping changes inside an accepted aliased self-join filter.

Files:
- Modify: `protocol/handle_oneoff_test.go`

Step 1: Add a focused test immediately after the existing self-join child-order test.

Suggested test name:
- `TestHandleOneOffQuery_AliasedSelfJoinFilterAssociativeGroupingVisibleRowsMatch`

Suggested query pair:
- `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE (a.id = 1 AND a.id > 0) AND a.id < 2`
- `SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1 AND (a.id > 0 AND a.id < 2)`

Suggested fixture shape:
- Reuse the tiny `t(id,u32)` schema with `idx_t_u32` and three rows already used by `TestHandleOneOffQuery_AliasedSelfJoinFilterChildOrderVisibleRowsMatch`.
- Assert both queries succeed.
- Assert both produce the same multiplicity-expanded `a.*` rows in the same order.
- Keep the assertion narrow: this is proving execution semantics are already aligned, not changing runtime behavior.

Step 2: Run only the new one-off test.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol -run 'TestHandleOneOffQuery_AliasedSelfJoinFilterAssociativeGroupingVisibleRowsMatch' -count=1`

Expected before implementation:
- PASS immediately.

If this fails:
- Stop and re-scout. The seam is no longer pure query identity, so do not proceed with hash-only planning.

## Task 2: Add the failing canonical-hash pin

Objective: prove grouped self-join filters still hash differently solely because of tree shape.

Files:
- Modify: `subscription/hash_test.go`

Step 1: Add a focused test beside `TestQueryHashSelfJoinFilterChildOrderCanonicalized`.

Suggested test name:
- `TestQueryHashSelfJoinFilterAssociativeGroupingCanonicalized`

Suggested predicate setup:
- Three same-side alias-aware leaves on alias `0` only:
  - `a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}`
  - `b := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}, Upper: Bound{Unbounded: true}}`
  - `c := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Unbounded: true}, Upper: Bound{Value: types.NewUint32(2), Inclusive: false}}`
- Join A:
  - `Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: And{Left: a, Right: b}, Right: c}}`
- Join B:
  - same join, but `Filter: And{Left: a, Right: And{Left: b, Right: c}}}`

Primary assertion:
- `ComputeQueryHash(joinA, nil) == ComputeQueryHash(joinB, nil)`

Required guardrail in the same test or an adjacent one:
- Alias identity must still matter. For example, rebuild one child with `Alias: 1` and assert the hash changes.
- Do not add duplicate-leaf or absorption expectations here; keep the failure isolated to associative grouping.

Optional follow-through after `And` is pinned:
- Mirror the same shape for `Or` if live code closes both cheaply, but only if it stays bounded and does not obscure the main failing seam.

Step 2: Run only the new hash test.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestQueryHashSelfJoinFilterAssociativeGroupingCanonicalized' -count=1`

Expected before implementation:
- FAIL.

## Task 3: Add the failing manager dedup/query-state-sharing pin

Objective: prove subscription registration still forks query state when only self-join filter grouping differs.

Files:
- Modify: `subscription/manager_test.go`

Step 1: Add a focused registration test after the existing self-join child-order manager test.

Suggested test name:
- `TestRegisterSet_SelfJoinFilterAssociativeGroupingSharesQueryState`

Suggested fixture shape:
- Reuse the same tiny fake schema already used by `TestRegisterSet_SelfJoinFilterChildOrderSharesQueryState`:
  - table `1` with `id` and indexed `u32`
  - three committed rows with shared `u32` values so the join is valid
- Register grouping A under `(ConnID=1, QueryID=156)`.
- Register grouping B under `(ConnID=2, QueryID=157)`.

Assertions:
- both registrations succeed
- `len(mgr.registry.byHash) == 1`
- both hashes match
- the shared query state has `refCount == 2`

Step 2: Run only the new manager test.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestRegisterSet_SelfJoinFilterAssociativeGroupingSharesQueryState' -count=1`

Expected before implementation:
- FAIL.

## Task 4: Implement the minimal self-join grouping fix in `subscription/hash.go`

Objective: canonicalize associative grouping for accepted self-join filter trees without broadening the canonicalization seam.

Files:
- Modify: `subscription/hash.go`

Implementation outline:
1. Keep `containsJoinLikePredicate(...)` and `canonicalGroupTable(...)` unchanged.
   - They are the fence preventing alias-blind same-table canonicalization from touching join-containing predicates.
2. Extend the self-join-specific path instead of reusing `flattenCanonicalAnd/Or(...)` directly.
   - Add tiny self-join-local helpers, e.g.:
     - `flattenSelfJoinCanonicalAnd(pred Predicate, out []Predicate) []Predicate`
     - `flattenSelfJoinCanonicalOr(pred Predicate, out []Predicate) []Predicate`
   - These helpers should recurse only through same-kind nodes (`And` for the `And` flattener, `Or` for the `Or` flattener).
   - They should not attempt same-table detection through `canonicalGroupTable(...)`; the enclosing `Join` self-join branch is the fence for this batch.
3. In `canonicalizeSelfJoinFilter(...)`:
   - recurse into children first, as today
   - for `And`:
     - build `combined := And{Left: left, Right: right}`
     - flatten same-kind `And` children from `combined`
     - sort children by `canonicalPredicateBytes(...)`
     - rebuild deterministically with `rebuildCanonicalAnd(...)`
   - for `Or`:
     - do the analogous flatten/sort/rebuild with `rebuildCanonicalOr(...)`
4. Preserve nil-child behavior exactly.
   - If either side is nil, return the partially rebuilt `And`/`Or` unchanged, just as the current helper does.
5. Do not add dedupe or absorption in the self-join path in this slice.
   - Grouping only.
6. Do not change the distinct-table join branch.
   - `canonicalizePredicate(Join)` should keep routing `p.Left != p.Right` filters through the existing generic `canonicalizePredicate(...)` path.
7. Leave `CrossJoin` unchanged.

Why this seam is safe:
- alias identity is already part of canonical child bytes for `ColEq` / `ColRange` leaves
- `validateSelfJoinFilterAliases(...)` already constrains leaves to the enclosing aliases
- flattening same-kind `And` or `Or` groups preserves the visible boolean meaning for the accepted shapes under test
- not enabling dedupe/absorb keeps this batch bounded to grouping only

Things not to do:
- do not call `canonicalGroupTable(...)` from the self-join grouping helper; it will reject join-containing trees and collapse the purpose of this slice
- do not route self-join filters through the broader same-table dedupe/absorb helpers just because they already exist
- do not widen to mixed alias-side normalization beyond ordering/grouping for the exact accepted shapes under test

## Task 5: Validate in the exact narrow-first order

Objective: prove the slice closes cleanly and stays bounded.

Step 1: Run the focused tests first.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol -run 'TestHandleOneOffQuery_AliasedSelfJoinFilterAssociativeGroupingVisibleRowsMatch' -count=1`
- `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestQueryHashSelfJoinFilterAssociativeGroupingCanonicalized|TestRegisterSet_SelfJoinFilterAssociativeGroupingSharesQueryState' -count=1`

Expected after implementation:
- PASS

Step 2: Format touched Go packages.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go fmt ./protocol ./subscription`

Step 3: Re-run touched-package suites.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol ./subscription -count=1`

Step 4: Run vet on touched packages.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go vet ./protocol ./subscription`

Step 5: Run the broad suite.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./... -count=1`

Expected:
- full suite green

## Task 6: Documentation follow-through if the slice lands

Objective: keep the parity docs and next handoff aligned with the closed seam.

Files:
- Modify: `TECH-DEBT.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `NEXT_SESSION_HANDOFF.md`

Step 1: Update `docs/parity-phase0-ledger.md`.
- Add a new closed row or extend the self-join canonicalization record so it explicitly says associative grouping is now closed and pinned by:
  - `protocol/handle_oneoff_test.go`
  - `subscription/hash_test.go`
  - `subscription/manager_test.go`

Step 2: Update `TECH-DEBT.md` and `docs/spacetimedb-parity-roadmap.md`.
- Mark self-join alias-sensitive join-filter associative grouping as closed.
- Keep broader OI-002 open.
- Name the next residual narrowly, not “more self-join work” in the abstract.
- If the next grounded seam is self-join duplicate-leaf idempotence, say that explicitly; if not, only name what the new scout actually proves.

Step 3: Rewrite `NEXT_SESSION_HANDOFF.md`.
- Move this slice into the closed list.
- Replace the current “Selected next task” with the next bounded residual backed by live code/tests.
- Preserve the explicit “do not reopen” guidance for already-closed child-order and grouping seams.

## Risks and stop conditions

Risks:
- A naive self-join flattener could accidentally normalize across a seam where alias identity should still matter.
- Reusing generic same-table helpers too aggressively could silently pull in duplicate-leaf or absorption reductions that this batch is not supposed to close.
- If grouped `Or` shapes force additional behavior not needed for the `And` seam, the first implementation should land the smallest coherent grouping fix rather than broadening the scope reflexively.

Stop conditions:
- Stop if the one-off semantic pin fails; that means the seam is not hash-only.
- Stop if the minimal fix requires duplicate-leaf collapse or absorption to make the grouping tests coherent.
- Stop if the next failure clearly belongs to a different seam family after grouping is fixed.

## Deliverable for the implementation session

A good implementation session following this plan should end with:
- one bounded OI-002 A2 self-join grouping slice closed
- one public one-off test proving visible rows were already equal for regrouped accepted self-join SQL
- new hash and manager pins proving regrouped accepted self-join filters share one canonical hash and one shared query state
- `subscription/hash.go` updated with a self-join-local flatten/sort/rebuild path only
- parity docs and `NEXT_SESSION_HANDOFF.md` updated in the same session
- no push unless the user explicitly says `push`
