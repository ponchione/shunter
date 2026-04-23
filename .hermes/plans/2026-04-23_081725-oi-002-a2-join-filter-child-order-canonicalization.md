# OI-002 A2 distinct-table join-filter child-order canonicalization implementation plan

> For Hermes: use subagent-driven-development only after re-validating this plan against the live handoff and worktree. This is a planning-only artifact.

Goal: close the next bounded OI-002 A2 runtime/model residual by making already-accepted distinct-table join SQL with same-table commutative filter child-order differences share one canonical query hash and one shared query state.

Architecture:
- The live code already aligns subscribe admission and one-off execution for accepted join-filter shapes, but `subscription/hash.go` still treats `Join.Filter` as source-order-sensitive because `canonicalizePredicate(...)` only canonicalizes top-level `And`/`Or` trees and returns `Join` unchanged.
- The narrow fix is to canonicalize only the filter subtree inside `subscription.Join`, while preserving join structure (`Left`, `Right`, `LeftCol`, `RightCol`, aliases, `ProjectRight`) exactly as-is.
- Keep the first slice fenced to distinct-table joins and filter-local same-table commutative reordering only. Do not widen into self-join alias canonicalization, grouping/absorption families inside joins, cross joins, or parser-surface widening in the same batch.

Grounded current context:
- `NEXT_SESSION_HANDOFF.md` says the next batch must stay in OI-002 A2 and prefer an accepted-shape normalization / validation / runtime-identity residual over a new parser widening.
- `protocol/handle_subscribe.go` already accepts and lowers join filters like:
  - `SELECT "users".* FROM "users" JOIN "other" ON "users"."id" = "other"."uid" WHERE (("users"."id" = 1) AND ("users"."id" > 0))`
- `protocol/handle_oneoff_test.go` already proves accepted join-filter execution works for that shape.
- `subscription/hash.go` currently canonicalizes `And`/`Or` only when they are the direct predicate being hashed; `canonicalizePredicate(...)` has no `Join` case, and `encodePredicate(...)` serializes `Join.Filter` in source order.
- `subscription/register_set.go` already validates both the original predicate and the canonicalized predicate. That guardrail should stay intact.
- No existing `.hermes/plans/` file remains for this batch; this plan supersedes no saved plan.

Batch boundary:
- In scope:
  - canonical query identity for distinct-table `Join.Filter` child-order permutations on already-accepted same-table filter terms
  - targeted hash + manager dedup/query-state sharing follow-through
  - one-off semantic pin proving visible rows were already equal
- Out of scope:
  - self-join filter alias canonicalization
  - join-filter associative grouping / duplicate-leaf / absorption reductions
  - cross-join behavior
  - parser widening
  - recovery / hardening / rows-shape / OI-001 work

Likely files to modify:
- Modify: `subscription/hash.go`
- Modify: `subscription/hash_test.go`
- Modify: `subscription/manager_test.go`
- Modify: `protocol/handle_oneoff_test.go`
- Optional only if needed for a stronger public compile pin: `protocol/handle_subscribe_test.go`

## Task 1: Add a one-off semantic pin for reordered distinct-table join filters

Objective: prove the user-visible one-off row result is already the same when only the order of same-table join-filter children changes.

Files:
- Modify: `protocol/handle_oneoff_test.go`

Step 1: Add a focused test next to the existing quoted/parenthesized join-filter coverage.

Suggested shape:
- Build the same `users` / `other` schema already used by the existing accepted join-filter test.
- Execute two one-off queries that differ only by `WHERE` child order:
  - `... WHERE (("users"."id" = 1) AND ("users"."id" > 0))`
  - `... WHERE (("users"."id" > 0) AND ("users"."id" = 1))`
- Assert both succeed and return the same single projected `users` row.

Step 2: Run the targeted one-off test.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol -run 'TestHandleOneOffQuery_.*Join.*ChildOrder' -count=1`

Expected:
- PASS immediately. This is the proof that execution semantics are already aligned and the residual is at the hash/query-state seam.

## Task 2: Add the failing canonical-hash pin for reordered join filters

Objective: prove the canonical identity seam is still open.

Files:
- Modify: `subscription/hash_test.go`

Step 1: Add a failing test with two `subscription.Join` predicates that differ only by filter child order.

Suggested predicate pair:
- Base leaves:
  - `a := ColEq{Table: 1, Column: 0, Value: types.NewUint32(1)}`
  - `b := ColRange{Table: 1, Column: 0, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}}`
- Join A:
  - `Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, Filter: And{Left: a, Right: b}}`
- Join B:
  - same join, but `Filter: And{Left: b, Right: a}`

Assertion:
- `ComputeQueryHash(joinA, nil) == ComputeQueryHash(joinB, nil)`

Guardrail assertion:
- Also keep one negative check nearby showing a real join-structure change still alters the hash, e.g. `ProjectRight` or `Filter=nil` vs `Filter!=nil`, so the slice does not over-collapse join identity.

Step 2: Run only the new hash test.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestQueryHashJoinFilterChildOrderCanonicalized' -count=1`

Expected:
- FAIL before implementation. Current `Join.Filter` encoding is source-order-sensitive.

## Task 3: Add the failing manager dedup/query-state-sharing pin

Objective: prove subscribe registration still forks query state for the same accepted distinct-table join when only filter child order differs.

Files:
- Modify: `subscription/manager_test.go`

Step 1: Add a focused registration test.

Suggested structure:
- Reuse a minimal two-table schema with an index on one join side so the join is valid.
- Register one join predicate with filter order A under `(ConnID=1, QueryID=10)`.
- Register the same join predicate with filter order B under `(ConnID=2, QueryID=11)`.
- Assert:
  - both registrations succeed
  - registry query-state count is `1`, not `2`
  - both subscriptions point at the same `QueryHash`

Step 2: Run only the new manager test.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestRegisterSet_DistinctTableJoinFilterChildOrderSharesQueryState' -count=1`

Expected:
- FAIL before implementation because the reordered filter currently hashes differently.

## Task 4: Implement the minimal canonicalization in `subscription/hash.go`

Objective: canonicalize the filter subtree inside `Join` without broadening the seam.

Files:
- Modify: `subscription/hash.go`

Implementation outline:
1. Add an explicit `case Join:` branch to `canonicalizePredicate(...)`.
2. Leave all join identity fields unchanged:
   - `Left`
   - `Right`
   - `LeftCol`
   - `RightCol`
   - `LeftAlias`
   - `RightAlias`
   - `ProjectRight`
3. If `p.Filter == nil`, return the join unchanged.
4. For the first slice, only recurse/canonicalize `p.Filter` when `p.Left != p.Right`.
   - This keeps self-join alias semantics out of scope for now.
5. Canonicalize the filter subtree by calling `canonicalizePredicate(p.Filter)` and reattach it.
6. Do not add any new canonicalization rule for `CrossJoin`.
7. Do not canonicalize across the join boundary itself; only the nested filter subtree may change.

Important guardrails:
- The canonicalization must not rewrite `Join` into a non-join predicate.
- Do not collapse mixed-table filter trees beyond what `canonicalizePredicate(...)` already allows.
- Keep `register_set.go`’s double-validation behavior untouched.
- If recursion unexpectedly affects self-join filters, stop and narrow further rather than widening the batch.

## Task 5: Re-run focused tests, then package tests, then the broad suite

Objective: verify the slice end-to-end without widening scope.

Files:
- No new files beyond the touched tests/code above.

Step 1: Run the three focused tests.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol -run 'TestHandleOneOffQuery_.*Join.*ChildOrder' -count=1`
- `PATH=/usr/local/go/bin:$PATH rtk go test ./subscription -run 'TestQueryHashJoinFilterChildOrderCanonicalized|TestRegisterSet_DistinctTableJoinFilterChildOrderSharesQueryState' -count=1`

Expected:
- PASS

Step 2: Run touched-package suites.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./protocol ./subscription -count=1`

Step 3: Run vet on touched Go packages.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go vet ./protocol ./subscription`

Step 4: Run the broad suite.

Run:
- `PATH=/usr/local/go/bin:$PATH rtk go test ./... -count=1`

Expected:
- full suite green

## Task 6: Documentation follow-through in the same session

Objective: keep the handoff/docs aligned so the next agent does not reopen this seam.

Files:
- Modify: `TECH-DEBT.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `NEXT_SESSION_HANDOFF.md`

Step 1: Update `docs/parity-phase0-ledger.md`.
- Add or extend the `P0-SUBSCRIPTION-*` row for this new closed seam.
- Record the authoritative tests added in `protocol/handle_oneoff_test.go`, `subscription/hash_test.go`, and `subscription/manager_test.go`.

Step 2: Update `TECH-DEBT.md` and `docs/spacetimedb-parity-roadmap.md`.
- Mark distinct-table join-filter child-order canonicalization as closed.
- Keep the broader A2 backlog open.
- Explicitly name what still remains out of scope after this close, especially self-join alias-sensitive join-filter normalization and any deeper join-filter grouping/absorption family if still unscouted.

Step 3: Update `NEXT_SESSION_HANDOFF.md`.
- Move this seam into the closed list.
- Point the next batch at the next fresh bounded A2 residual, not another reopen of join-filter child order.

## Risks and stop conditions

Risks:
- Self-join filters use alias tags on leaves. A naive recursive join-filter canonicalization could accidentally reopen the guardrail that earlier same-table work intentionally fenced off.
- If the hash drift turns out to affect grouped/duplicate/absorption variants immediately too, do not silently widen this batch. Land child-order only unless the extra work is required for the minimal fix to compile or keep tests coherent.
- If a focused test reveals one-off semantics are not actually equal for the reordered filter, stop and re-scout; the residual would no longer be a pure identity seam.

Stop conditions:
- Stop if fixing the hash/query-state seam requires self-join alias canonicalization.
- Stop if the change would require parser or protocol request-shape widening.
- Stop if the next failing case after child-order is a different seam family; document it for the next batch rather than widening mid-session.

## Deliverable for the implementation session

A good implementation session following this plan should end with:
- one bounded OI-002 A2 slice closed
- new pins proving accepted distinct-table join-filter child-order permutations share one canonical hash and one query state
- one public one-off test proving visible rows were already equal
- docs + handoff updated in the same session
- no push unless the user explicitly says `push`
