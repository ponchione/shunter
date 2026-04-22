# OI-002 A2 projected-join delta ordering implementation plan

> For Hermes: use subagent-driven-development if executing later. This is a planning-only artifact; do not implement from this file directly without re-checking live code.

Goal: close the next bounded OI-002 A2 runtime/model residual by making post-commit projected-join delta emission preserve the same projected-side row order already pinned for one-off queries and committed bootstrap/final-delta paths.

Architecture:
- Keep scope inside the existing accepted join surface. Do not widen SQL, admission, validation, fan-out, or rows-shape behavior.
- Fix the delta-path ordering seam at join-fragment reconciliation, not by adding ad hoc sorting after projection.
- Preserve bag semantics and multiplicity exactly; only stabilize survivor emission order.

Tech stack: Go, existing subscription delta evaluator, RTK-based test workflow.

---

## Confirmed live mismatch

Grounded scout result:
- `subscription/register_set.go` now emits committed bootstrap/final-delta join rows by scanning the projected side first (`appendProjectedJoinRows`), so those paths preserve projected-side order.
- `protocol/handle_oneoff.go` also scans the projected side first in `evaluateOneOffJoin`, so one-off already has the same projected-side ordering baseline.
- `subscription/eval.go` still routes join delta output through `EvalJoinDeltaFragments(...)` -> `ReconcileJoinDelta(...)` -> `projectJoinedRows(...)`.
- `subscription/delta_dedup.go` rebuilds final insert/delete slices by ranging over `insertCounts` and `deleteCounts` maps. That loses fragment/row encounter order, so delta output order is currently detached from the aligned projected-side baseline.

That makes the next bounded slice concrete:
- preserve stable survivor order in join delta reconciliation so post-commit projected rows no longer come back in map-order instead of projected-side order.

Scope boundary:
- In scope: projected join delta insert/delete ordering for already-accepted join shapes.
- Out of scope: SQL widening, join validation, multiplicity semantics, cross-join runtime work, fan-out delivery, wire rows-shape, hardening OIs.

---

## Files likely to change

Code:
- Modify: `subscription/delta_dedup.go`
- Possibly modify: `subscription/eval.go` only if a tiny helper seam is needed for clearer tests; avoid behavior changes here if `ReconcileJoinDelta` alone is enough.

Tests:
- Modify: `subscription/delta_dedup_test.go`
- Modify: `subscription/eval_test.go`
- Optionally modify: `subscription/delta_join_test.go` only if a fragment-level order pin is needed to explain expected survivor order.

Docs:
- Modify: `TECH-DEBT.md`
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`
- Modify: `NEXT_SESSION_HANDOFF.md`

---

## Task 1: Add reconciliation-level order pins

Objective: prove the current reconciliation seam can lose survivor order even while preserving multiplicity.

Files:
- Modify: `subscription/delta_dedup_test.go`

Step 1: Add a failing insert-order test.

Test shape:
- Build two distinct joined rows `rowA`, `rowB`.
- Feed fragments so both survive as inserts in encounter order `rowA`, then `rowB`.
- Assert `ReconcileJoinDelta(...)` returns inserts in exactly that order.

Suggested test name:
- `TestReconcileJoinDeltaPreservesInsertEncounterOrder`

Step 2: Add a failing delete-order test.

Test shape:
- Feed delete fragments whose survivors should appear in encounter order `rowA`, then `rowB`.
- Assert deletes preserve that order.

Suggested test name:
- `TestReconcileJoinDeltaPreservesDeleteEncounterOrder`

Step 3: Run only those tests.

Run:
- `rtk go test ./subscription -run 'TestReconcileJoinDeltaPreserves(Insert|Delete)EncounterOrder' -count=1`

Expected now:
- FAIL or flake risk consistent with unordered map-backed emission.

---

## Task 2: Add end-to-end delta ordering pin for projected-left joins

Objective: prove post-commit delta inserts do not currently follow projected-side order for a left-projected accepted join shape.

Files:
- Modify: `subscription/eval_test.go`

Step 1: Add a failing projected-left delta ordering test.

Suggested test name:
- `TestEvalJoinSubscriptionPreservesProjectedLeftDeltaOrder`

Test setup:
- Reuse the same style as the existing bootstrap order tests in `subscription/manager_test.go`.
- Use two projected-side rows on the left table and one newly inserted matching row on the right side.
- Register `Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1}`.
- Drive a changeset that inserts one RHS row matching both projected LHS rows.
- Assert emitted inserts are exactly the projected left rows in projected-side scan order.

Expected target order:
- `[lhsRow1, lhsRow2]`, not map order, not row-key sort, not grouped by fragment identity.

Step 2: Run only the new eval test plus the reconciliation tests.

Run:
- `rtk go test ./subscription -run 'Test(ReconcileJoinDeltaPreserves(Insert|Delete)EncounterOrder|EvalJoinSubscriptionPreservesProjectedLeftDeltaOrder)' -count=1`

Expected now:
- FAIL before implementation.

---

## Task 3: Add end-to-end delta ordering pin for projected-right joins

Objective: prove the same seam affects right-projected accepted join shapes.

Files:
- Modify: `subscription/eval_test.go`

Step 1: Add a failing projected-right delta ordering test.

Suggested test name:
- `TestEvalJoinSubscriptionPreservesProjectedRightDeltaOrder`

Test setup:
- Mirror Task 2 but flip projection.
- Use two projected-side RHS rows already committed and insert one matching LHS row.
- Register `Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1, ProjectRight: true}`.
- Assert emitted inserts are the two RHS rows in RHS projected order.

Step 2: If the same seam obviously affects deletes too, add one delete regression before implementation rather than after.

Suggested optional names:
- `TestEvalJoinSubscriptionPreservesProjectedLeftDeltaDeleteOrder`
- `TestEvalJoinSubscriptionPreservesProjectedRightDeltaDeleteOrder`

Keep this optional if insert pins already fully expose the seam and delete coverage would just duplicate the same failure mode.

Step 3: Run the new focused tests.

Run:
- `rtk go test ./subscription -run 'Test(EvalJoinSubscriptionPreservesProjected(Left|Right)DeltaOrder|ReconcileJoinDeltaPreserves(Insert|Delete)EncounterOrder)' -count=1`

---

## Task 4: Implement stable join-delta reconciliation

Objective: preserve survivor order without changing bag semantics.

Files:
- Modify: `subscription/delta_dedup.go`

Implementation strategy:
- Keep the existing row-key encoding and multiplicity-count model.
- Add stable emission-order tracking keyed by first surviving encounter, rather than ranging over maps at the end.
- A minimal safe pattern is:
  - keep `insertCounts` / `deleteCounts` as today for bag math
  - additionally track ordered key lists for first-seen insert and delete survivors
  - when visiting insert fragments, append the key to insert order only on first observation
  - when visiting delete fragments, cancel against positive insert count first; otherwise append the key to delete order only on first delete-survivor observation
  - emit inserts/deletes by walking the recorded key order slices and repeating rows according to remaining counts
- Do not sort rows by encoded key or row content. The desired contract is encounter order from the fragment stream, which reflects projected-side enumeration.
- Preserve the existing cancellation semantics exactly: identical rows still cancel one-for-one across insert/delete fragments and multiplicity must remain intact.

Non-goals:
- no changes to join fragment generation
- no projection-side special casing in `eval.go`
- no protocol-layer sorting

---

## Task 5: Validate the narrow slice

Objective: prove the ordering fix closes the intended seam and does not regress existing join semantics.

Files:
- none

Step 1: Run the focused subscription tests.

Run:
- `rtk go test ./subscription -run 'Test(ReconcileJoinDeltaPreserves(Insert|Delete)EncounterOrder|EvalJoinSubscriptionPreservesProjected(Left|Right)DeltaOrder)' -count=1`

Step 2: Run the broader subscription package.

Run:
- `rtk go test ./subscription -count=1`

Step 3: Run the handoff-required nearby packages.

Run:
- `rtk go test ./protocol/... ./subscription/... ./executor/... -count=1`

Step 4: Run the full suite.

Run:
- `rtk go test ./... -count=1`

Optional if exported behavior/signatures changed:
- `rtk go vet ./subscription ./executor ./protocol`

---

## Task 6: Update planning and handoff docs in the same session

Objective: close the stale planning gap so the next session does not reopen this seam.

Files:
- Modify: `TECH-DEBT.md`
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`
- Modify: `NEXT_SESSION_HANDOFF.md`

Required doc updates:
- Mark this projected-join delta-ordering seam closed under OI-002 A2 if tests confirm it.
- Move the next-hand-off focus to the next real A2 residual instead of repeating “projected-join delta ordering/runtime shape” generically.
- Keep wording explicit that multiplicity, join-index validation, and committed bootstrap/final-delta ordering were already closed before this batch.
- If implementation reveals a deeper runtime-shape issue beyond ordering, record that as the next bounded residual instead of hand-waving it.

---

## Risks and watch-outs

- The fix must preserve bag semantics. A stable order patch that accidentally collapses multiplicity is wrong.
- Do not confuse deterministic test harness order with production sorting requirements. The contract here is “preserve encounter order from fragment generation,” not “globally sort rows.”
- If focused tests show `EvalJoinDeltaFragments(...)` itself already emits fragment rows in the wrong order, stop widening `delta_dedup.go` and re-scope the slice around fragment generation. Current live code strongly suggests reconciliation is the first culprit.
- Keep cross joins out of this batch unless new evidence proves they share the exact same seam and the fix is literally identical.

---

## Expected completion criteria

This slice is done when all of the following are true:
- join delta reconciliation preserves stable survivor order for both inserts and deletes
- post-commit projected-left and projected-right join delta tests pass with explicit row-order assertions
- `rtk go test ./protocol/... ./subscription/... ./executor/... -count=1` passes
- `rtk go test ./... -count=1` passes
- `TECH-DEBT.md`, `docs/parity-phase0-ledger.md`, `docs/spacetimedb-parity-roadmap.md`, and `NEXT_SESSION_HANDOFF.md` all reflect the new reality

---

## Suggested commit boundary

One commit is sufficient for this batch if the slice stays as narrow as expected:
- `subscription: preserve projected join delta order during reconciliation`

If the scout during execution discovers deletes require a clearly separable follow-up after inserts, split into two commits:
1. insert-order closure
2. delete-order follow-through

Do not widen beyond that without a new plan or decision doc.
