# OI-002 A2 projected-side join bootstrap ordering parity Implementation Plan

> For Hermes: Use subagent-driven-development skill to implement this plan task-by-task.

Goal: Make subscribe bootstrap and unsubscribe final-delta join rows preserve the same projected-side row ordering that one-off join queries already expose, so accepted join SQL no longer changes user-visible row sequence based only on which join side happens to carry the usable index.

Architecture: Keep the slice at the committed-join enumeration seam. Do not widen SQL grammar, hash identity, fan-out, or join multiplicity. Rework `subscription.Manager.initialQuery(...)` so join evaluation always iterates projected-side rows first, probing the non-projected side by index when possible and falling back to a bounded full scan when the only available index lives on the projected side. Reuse the same helper for unregister final-delta rows because `UnregisterSet(...)` already routes through `initialQuery(...)`.

Tech Stack: Go, existing `subscription`, `protocol`, `schema`, and `store` packages; RTK-wrapped Go test/vet/fmt commands.

Grounded mismatch this plan closes:
- `subscription/register_set.go:68-130` chooses the outer scan side by whichever opposite-side index is available. For `SELECT lhs.* ...` when only `lhs.join_col` is indexed, bootstrap rows are emitted in RHS scan / LHS index-seek order instead of projected-LHS order. The same inversion exists for `SELECT rhs.* ...` when only the RHS join column is indexed.
- `subscription/register_set.go:308-317` reuses `initialQuery(...)` for unsubscribe final-delta rows, so the same order drift leaks into unsubscribe-visible deletes.
- `protocol/handle_oneoff.go:61-73,132-179` already enumerates projected-side rows first for one-off joins, and existing tests pin that public order:
  - `protocol/handle_oneoff_test.go:700-777`
  - `protocol/handle_oneoff_test.go:925-1002`
- Existing subscription bootstrap tests pin row shape and multiplicity but not order:
  - `subscription/manager_test.go:256-283`
  - `subscription/manager_test.go:286-343`
- Reference grounding for the public model: `reference/SpacetimeDB/crates/query-builder/src/join.rs:51-60,83-95,136-139` distinguishes left semijoin (“filters and returns left table rows”) from right semijoin (“returns right table rows”), so projected-side row identity should remain the visible driver instead of silently flipping with index placement.

Scope guardrails:
- Stay inside OI-002 A2.
- Do not widen accepted SQL.
- Do not reopen fan-out delivery, join/cross-join multiplicity, or one-off-vs-subscribe join-index validation.
- Do not change canonical hash behavior.
- Do not redesign delta-eval fragment ordering in `subscription/delta_join.go` / `subscription/delta_dedup.go` as part of this slice; this batch is only bootstrap + final-delta committed-query ordering.

Files likely to change:
- Modify: `subscription/register_set.go`
- Modify: `subscription/manager_test.go`
- Maybe modify: `protocol/handle_oneoff_test.go` only to strengthen the already-correct projected-order baseline if a more explicit pin helps readability
- Modify at end: `TECH-DEBT.md`
- Modify at end: `docs/parity-phase0-ledger.md`
- Modify at end: `docs/spacetimedb-parity-roadmap.md`
- Modify at end: `NEXT_SESSION_HANDOFF.md`

---

## Task 1: Re-scout and write down the exact mismatch

Objective: Confirm the order drift before editing tests or code.

Files:
- Read: `subscription/register_set.go`
- Read: `protocol/handle_oneoff.go`
- Read: `subscription/manager_test.go`
- Read: `protocol/handle_oneoff_test.go`
- Read: `reference/SpacetimeDB/crates/query-builder/src/join.rs`

Step 1: Inspect `initialQuery(...)` join enumeration.
Run: `rtk read subscription/register_set.go`
Expected: the branch at `case Join:` uses `rhsIdx` first, otherwise `lhsIdx`, which means the outer loop follows index placement rather than projected-side semantics.

Step 2: Inspect one-off join enumeration.
Run: `rtk read protocol/handle_oneoff.go`
Expected: `evaluateOneOffJoin(...)` always scans the projected side first and only then walks partner rows.

Step 3: Record the exact mismatch in one bullet before coding.
Expected wording: “subscribe bootstrap/final-delta join row order flips with available index side, while one-off already preserves projected-side order.”

---

## Task 2: Add failing bootstrap order tests first

Objective: Prove the mismatch with focused subscription-level tests.

Files:
- Modify: `subscription/manager_test.go`

Step 1: Add a left-projection bootstrap order test where only the projected side is indexed.
Suggested test name:
- `TestRegisterJoinBootstrapPreservesProjectedLeftOrderWhenOnlyLeftJoinColumnIndexed`

Fixture shape:
- left table rows in visible order: `{1, 7}`, `{2, 7}`
- right table rows in visible order: `{10, 7}`, `{11, 7}`
- only left join column indexed
- predicate: `Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1, ProjectRight: false}`

Expected row sequence:
- `[1,7]`, `[1,7]`, `[2,7]`, `[2,7]`
- not `[1,7]`, `[2,7]`, `[1,7]`, `[2,7]`

Step 2: Add a right-projection bootstrap order test where only the projected side is indexed.
Suggested test name:
- `TestRegisterJoinBootstrapPreservesProjectedRightOrderWhenOnlyRightJoinColumnIndexed`

Fixture shape:
- same logical rows, but only the right join column indexed
- predicate uses `ProjectRight: true`

Expected row sequence:
- right-side projected rows grouped in right scan order, e.g. `[10,7]`, `[10,7]`, `[11,7]`, `[11,7]`

Step 3: Run only the new failing tests.
Run: `rtk go test ./subscription -run 'TestRegisterJoinBootstrapPreservesProjected(Left|Right)OrderWhenOnly.*Indexed' -count=1 -v`
Expected: FAIL — current code should interleave projected rows according to the non-projected outer scan.

---

## Task 3: Add one unsubscribe final-delta order pin

Objective: Prove the same helper leak affects unsubscribe-visible deletes.

Files:
- Modify: `subscription/manager_test.go`

Step 1: Add a final-delta order test that unregisters a join subscription under the same “only projected side indexed” setup.
Suggested test name:
- `TestUnregisterJoinFinalDeltaPreservesProjectedLeftOrderWhenOnlyLeftJoinColumnIndexed`

Assertions:
- `UnregisterSet(...).Update[0].Deletes` follows the same projected-side order as the bootstrap test
- no need to add both left and right variants here if one variant proves the shared helper path

Step 2: Run the narrow failing set.
Run: `rtk go test ./subscription -run 'Test(RegisterJoinBootstrapPreservesProjected(Left|Right)OrderWhenOnly.*Indexed|UnregisterJoinFinalDeltaPreservesProjectedLeftOrderWhenOnlyLeftJoinColumnIndexed)' -count=1 -v`
Expected: FAIL

---

## Task 4: Keep the one-off order baseline visible

Objective: Make the public baseline explicit without broadening the slice.

Files:
- Read or maybe modify: `protocol/handle_oneoff_test.go`

Step 1: Re-run the existing one-off order pins that already show projected-side order.
Run: `rtk go test ./protocol -run 'TestHandleOneOffQuery_(JoinProjectionOnLeftTable|JoinProjectionOnRightTable)' -count=1 -v`
Expected: PASS

Step 2: Only if the existing assertions are too indirect, tighten them with a short comment or helper assertion.
Constraint:
- do not change production protocol code in this task
- do not add new one-off semantics beyond making the projected-order baseline obvious

---

## Task 5: Implement projected-side-first committed join enumeration

Objective: Make subscription bootstrap and final-delta join rows follow projected-side order regardless of which side holds the usable index.

Files:
- Modify: `subscription/register_set.go`

Step 1: Replace the join branch in `initialQuery(...)` with projected-side-first enumeration.
Target behavior:
- determine `projectedTable`, `otherTable`, `projectedJoinCol`, `otherJoinCol`
- iterate `view.TableScan(projectedTable)` as the outer loop in all join cases
- if `m.resolver.IndexIDForColumn(otherTable, otherJoinCol)` exists, probe the other side by index for each projected row
- otherwise fall back to scanning `view.TableScan(otherTable)` for each projected row and comparing join keys directly
- preserve multiplicity and existing `tryJoinFilter(...)` behavior

Step 2: Keep self-join and `ProjectRight` handling correct.
Expected:
- `SELECT a.* FROM t AS a JOIN t AS b ...` still emits a-rows in a-side order
- `SELECT b.* ...` still emits b-rows in b-side order
- filter alias handling remains delegated to `tryJoinFilter(...)`

Step 3: Do not widen this into a shared protocol/subscription helper unless the code becomes obviously clearer.
Guideline:
- a small local helper inside `subscription/register_set.go` is preferred over a new abstraction layer
- if extraction becomes necessary, keep it in `subscription/` and do not touch executor/protocol plumbing

Suggested implementation sketch:

```go
func (m *Manager) appendProjectedJoinRows(out []types.ProductValue, view store.CommittedReadView, p Join) ([]types.ProductValue, error) {
    projectedTable := p.Left
    otherTable := p.Right
    projectedJoinCol := p.LeftCol
    otherJoinCol := p.RightCol
    project := func(projectedRow, otherRow types.ProductValue) (types.ProductValue, types.ProductValue) {
        if p.ProjectRight {
            return otherRow, projectedRow
        }
        return projectedRow, otherRow
    }
    if p.ProjectRight {
        projectedTable, otherTable = p.Right, p.Left
        projectedJoinCol, otherJoinCol = p.RightCol, p.LeftCol
    }

    otherIdx, hasOtherIdx := m.resolver.IndexIDForColumn(otherTable, otherJoinCol)
    for _, projectedRow := range view.TableScan(projectedTable) {
        if int(projectedJoinCol) >= len(projectedRow) {
            continue
        }
        if hasOtherIdx {
            key := store.NewIndexKey(projectedRow[projectedJoinCol])
            for _, rid := range view.IndexSeek(otherTable, otherIdx, key) {
                otherRow, ok := view.GetRow(otherTable, rid)
                if !ok {
                    continue
                }
                leftRow, rightRow := project(projectedRow, otherRow)
                if tryJoinFilter(leftRow, p.Left, rightRow, p.Right, &p) != nil {
                    out = append(out, projectedRow)
                }
            }
            continue
        }
        for _, otherRow := range view.TableScan(otherTable) {
            if int(otherJoinCol) >= len(otherRow) || !projectedRow[projectedJoinCol].Equal(otherRow[otherJoinCol]) {
                continue
            }
            leftRow, rightRow := project(projectedRow, otherRow)
            if tryJoinFilter(leftRow, p.Left, rightRow, p.Right, &p) != nil {
                out = append(out, projectedRow)
            }
        }
    }
    return out, nil
}
```

Important constraint:
- the emitted row must always be the projected-side row, even when `tryJoinFilter(...)` needs `(leftRow, rightRow)` in canonical join orientation.

---

## Task 6: Verify the narrow slice end-to-end

Objective: Prove the ordering fix without reopening broader A2 work.

Files:
- No new files beyond touched code/tests/docs

Step 1: Run the new focused subscription tests.
Run: `rtk go test ./subscription -run 'Test(RegisterJoinBootstrapPreservesProjected(Left|Right)OrderWhenOnly.*Indexed|UnregisterJoinFinalDeltaPreservesProjectedLeftOrderWhenOnlyLeftJoinColumnIndexed)' -count=1 -v`
Expected: PASS

Step 2: Run the nearby order/multiplicity baselines.
Run: `rtk go test ./subscription ./protocol -run 'Test(RegisterJoinBootstrapProjectsRight|RegisterCrossJoinBootstrapPreservesMultiplicity|HandleOneOffQuery_(JoinProjectionOnLeftTable|JoinProjectionOnRightTable))' -count=1 -v`
Expected: PASS

Step 3: Run package-level verification.
Run: `rtk go test ./protocol/... ./subscription/... ./executor/... -count=1`
Expected: PASS

Step 4: Run full suite.
Run: `rtk go test ./... -count=1`
Expected: PASS

Step 5: Run vet and fmt for touched packages.
Run:
- `rtk go vet ./subscription ./protocol`
- `rtk go fmt ./subscription ./protocol`
Expected: clean

---

## Task 7: Update docs and handoff in the same session

Objective: Shrink the open A2 backlog and point the next agent at the next real residual.

Files:
- Modify: `TECH-DEBT.md`
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`
- Modify: `NEXT_SESSION_HANDOFF.md`

Step 1: Update `TECH-DEBT.md`.
Expected edit:
- note that projected-side join bootstrap/final-delta ordering drift is closed if the tests land
- keep OI-002 open for the next remaining runtime/model residual only

Step 2: Update `docs/parity-phase0-ledger.md`.
Expected edit:
- narrow the “broader query/subscription parity” wording so this ordering seam is no longer implied open
- avoid turning the ledger into a historical changelog

Step 3: Update `docs/spacetimedb-parity-roadmap.md`.
Expected edit:
- remove this bootstrap/final-delta projected-order seam from the live candidate wording
- point the next session at the next bounded A2 residual, likely a delta-eval ordering/runtime-shape follow-on if the scout confirms it

Step 4: Refresh `NEXT_SESSION_HANDOFF.md`.
Expected edit:
- mark the projected-side bootstrap/final-delta ordering slice closed
- explicitly say not to reopen it unless fresh evidence shows a remaining delta-path ordering mismatch or another distinct uncovered shape
- name the next best bounded A2 follow-on

---

## Risks and tradeoffs

1. Preserving projected-side order may require a full scan of the non-projected side when the only usable index lives on the projected side.
- Mitigation: keep the scope limited to committed bootstrap/final-delta enumeration where correctness/parity outranks speculative micro-optimization; do not widen into a broader query planner rewrite.

2. Self-join filter alias wiring could regress if left/right row orientation is mixed up.
- Mitigation: keep `tryJoinFilter(...)` as the canonical left/right filter gate and add at least one self-join regression rerun during verification.

3. There may be a separate delta-eval ordering issue after bootstrap/final-delta alignment lands.
- Mitigation: treat that as a follow-on batch, not as permission to widen this slice into `subscription/delta_join.go` or `subscription/delta_dedup.go` immediately.

4. The worktree is already dirty from the just-closed join-index validation batch.
- Mitigation: preserve the existing closure edits and limit this slice to the remaining open ordering seam.

---

## Stop condition

Stop after this one batch if all of the following are true:
- the new bootstrap and unregister order pins pass
- the existing one-off projected-order baseline still passes
- `rtk go test ./protocol/... ./subscription/... ./executor/...` passes
- `rtk go test ./...` passes
- docs and handoff are updated

If the scout or tests show that bootstrap/final-delta order already matches and the real remaining mismatch is only delta-eval ordering, stop after updating `NEXT_SESSION_HANDOFF.md` with that grounded residual instead of forcing a speculative code change.
