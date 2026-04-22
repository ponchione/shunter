# OI-002 A2 one-off join index-validation parity Implementation Plan

> For Hermes: Use subagent-driven-development skill to implement this plan task-by-task.

Goal: Make one-off SQL join admission reject the same unindexed join shapes that subscribe registration already rejects, so accepted SQL -> compiled predicate -> validation -> runtime behavior stays aligned across one-off and subscribe.

Architecture: Keep the slice narrow at the shared predicate-validation seam. Do not redesign join execution, hash normalization, or SQL grammar. Reuse `subscription.ValidatePredicate(...)` after one-off SQL compilation and before any snapshot evaluation, then return the existing one-off error envelope when validation fails.

Tech Stack: Go, existing `protocol`, `subscription`, `schema`, and `executor` packages; RTK-wrapped Go test/vet/fmt commands.

Grounded mismatch this plan closes:
- Subscribe registration rejects `subscription.Join` when neither join column is indexed (`subscription.ValidatePredicate(...)` -> `ErrUnindexedJoin`), but one-off SQL currently compiles the same join shape and executes it via nested scans without running that validation.
- Grounding surfaces already confirmed:
  - `subscription/validate.go:126-149`
  - `subscription/register_set.go:218-223`
  - `subscription/validate_test.go:134-143`
  - `subscription/manager_test.go:159-170`
  - `protocol/handle_oneoff.go:43-69,128-175`
  - `protocol/handle_subscribe.go:67-152`
  - reference: `reference/SpacetimeDB/crates/subscription/src/lib.rs:515-520`
  - reference: `reference/SpacetimeDB/crates/core/src/subscription/query.rs:27-36`

Scope guardrails:
- Stay inside OI-002 A2.
- Do not widen SQL support.
- Do not reopen fan-out delivery or join/cross-join multiplicity work.
- Do not change subscribe behavior except where needed to share validation plumbing.
- Do not change hash semantics unless a failing test proves it is required for this exact slice.

Files likely to change:
- Modify: `protocol/handle_oneoff.go`
- Modify: `protocol/handle_subscribe.go` or another protocol-local schema/validation seam if interface widening is needed
- Modify: `protocol/handle_oneoff_test.go`
- Maybe modify: `protocol/handle_subscribe_test.go` only if the shared protocol schema seam changes and needs a compile-time/runtime pin
- Maybe modify: `executor/protocol_inbox_adapter_test.go` only if protocol-facing schema interfaces or adapters need new compile guarantees
- Modify at end: `TECH-DEBT.md`
- Modify at end: `docs/parity-phase0-ledger.md`
- Modify at end: `docs/spacetimedb-parity-roadmap.md`
- Modify at end: `NEXT_SESSION_HANDOFF.md`

---

## Task 1: Re-scout the exact one-off validation seam

Objective: Confirm the implementation seam before changing tests or code.

Files:
- Read: `protocol/handle_oneoff.go`
- Read: `protocol/handle_subscribe.go`
- Read: `subscription/validate.go`
- Read: `subscription/register_set.go`

Step 1: Inspect the one-off flow around compile -> evaluate.
Run: `rtk go doc ./protocol && rtk read protocol/handle_oneoff.go`
Expected: `handleOneOffQuery(...)` compiles SQL and goes straight to evaluation without `subscription.ValidatePredicate(...)`.

Step 2: Inspect the subscribe validation flow.
Run: `rtk read subscription/register_set.go && rtk read subscription/validate.go`
Expected: `RegisterSet(...)` pre-validates each predicate and `validateJoin(...)` rejects unindexed joins.

Step 3: Record the exact seam in working notes or commit message draft.
Expected: “one-off join admission bypasses shared predicate validation” is the only target.

---

## Task 2: Add the failing one-off regression pin first

Objective: Prove the mismatch with one focused protocol-level test.

Files:
- Modify: `protocol/handle_oneoff_test.go`

Step 1: Add a new test for an unindexed equi-join one-off query.
Test shape:
- two tables with matching join-column types
- neither join column indexed
- one-off query such as `SELECT o.* FROM Orders o JOIN Inventory product ON o.product_id = product.id`
- expect error response, not rows

Suggested test name:
- `TestHandleOneOffQuery_UnindexedJoinRejected`

Assertions:
- `OneOffQueryResponse.Error != nil`
- error string is non-empty
- rows/result tables are empty
- if the implementation returns the wrapped validation error text, prefer `strings.Contains(*resp.Error, "join column has no index on either side")` or `strings.Contains(*resp.Error, "Subscriptions require indexes on join columns")` only after checking the actual chosen wording

Step 2: Run only the new failing test.
Run: `rtk go test ./protocol -run TestHandleOneOffQuery_UnindexedJoinRejected -count=1 -v`
Expected: FAIL — current code should return rows or no error because one-off bypasses join-index validation.

---

## Task 3: Choose the narrowest shared-validation plumbing

Objective: Decide how `protocol.handleOneOffQuery` can call `subscription.ValidatePredicate(...)` without broad interface churn.

Files:
- Modify: `protocol/handle_oneoff.go`
- Maybe modify: `protocol/handle_subscribe.go`

Decision rule:
- Prefer the smallest protocol-side interface widening or adapter that lets one-off validate compiled predicates using the same schema facts subscribe already relies on.
- Avoid touching executor or runtime model unless the compiler forces it.

Two acceptable implementation shapes:
1. Widen `protocol.SchemaLookup` so the concrete schema object used by protocol also satisfies `subscription.SchemaLookup`.
2. Keep `protocol.SchemaLookup` narrow for table lookup and add a tiny local adapter/helper only for one-off validation if the concrete call sites already expose enough information.

Prefer option 1 only if it does not cause broad fallout in tests and constructors.
Prefer option 2 if it keeps the slice confined to `protocol`.

Verification for this task:
- The chosen path must allow `handleOneOffQuery(...)` to validate the compiled predicate before snapshot evaluation.
- No production join-evaluation logic should change yet.

---

## Task 4: Implement minimal one-off predicate validation

Objective: Reject invalid compiled predicates in one-off before evaluation.

Files:
- Modify: `protocol/handle_oneoff.go`
- Maybe modify: `protocol/handle_subscribe.go`

Step 1: Insert validation immediately after SQL compilation and before snapshot acquisition/evaluation.
Target behavior:
- compile SQL
- resolve projected table as today
- call `subscription.ValidatePredicate(compiled.Predicate, <schema adapter>)`
- on error, send the normal one-off error envelope and return
- otherwise continue with the existing one-off execution path unchanged

Step 2: Preserve current behavior for non-join accepted shapes.
Expected:
- bare filters, OR, TRUE, join/cross-join multiplicity, and projected-row behavior remain unchanged unless they already violate shared validation

Step 3: Keep error handling protocol-native.
Expected:
- one-off still returns `OneOffQueryResponse{Error: ..., Tables: nil/empty}`
- do not invent a new error envelope or special-case join failures

---

## Task 5: Expand with one positive guard pin if needed

Objective: Ensure the new validation does not over-reject valid indexed joins.

Files:
- Modify: `protocol/handle_oneoff_test.go`

Step 1: If the new one-off rejection test passes after the fix but there is no nearby positive indexed-join pin, add one.
Suggested candidate:
- reuse or slightly tighten an existing one-off join success test where one join side is indexed

Step 2: Run the minimal focused set.
Run: `rtk go test ./protocol -run 'TestHandleOneOffQuery_(UnindexedJoinRejected|.*Join.*)' -count=1 -v`
Expected: PASS

Note:
- Only add this test if coverage is missing or the new validation plumbing risks over-rejection. Do not broaden the slice unnecessarily.

---

## Task 6: Run package-level verification

Objective: Prove the narrow slice is stable at the protocol/subscription/executor seam.

Files:
- No new files beyond touched tests/code

Step 1: Run focused package tests.
Run: `rtk go test ./protocol/... ./subscription/... ./executor/... -count=1`
Expected: PASS

Step 2: Run full suite.
Run: `rtk go test ./... -count=1`
Expected: PASS

Step 3: Run vet for touched behavior/interface surfaces.
Run: `rtk go vet ./protocol ./subscription ./executor`
Expected: no issues

Step 4: Run fmt on touched packages.
Run: `rtk go fmt ./protocol ./subscription ./executor`
Expected: clean

---

## Task 7: Update parity docs and handoff in the same session

Objective: Shrink the open A2 backlog and point the next session at the next real residual.

Files:
- Modify: `TECH-DEBT.md`
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`
- Modify: `NEXT_SESSION_HANDOFF.md`

Step 1: Update `TECH-DEBT.md`.
Expected edit:
- note that one-off vs subscribe join-index validation drift is closed if the code/tests land cleanly
- keep OI-002 open with the next remaining runtime/model residual only

Step 2: Update `docs/parity-phase0-ledger.md`.
Expected edit:
- extend the current OI-002 wording so this specific runtime-model mismatch is no longer implied open
- do not create a noisy historical changelog entry

Step 3: Update `docs/spacetimedb-parity-roadmap.md`.
Expected edit:
- remove or narrow any wording that still treats this one-off-vs-subscribe validation seam as open
- point at the next bounded A2 candidate, likely another predicate/runtime residual

Step 4: Refresh `NEXT_SESSION_HANDOFF.md`.
Expected edit:
- mark this join-index validation slice closed
- name the next best bounded A2 runtime/model candidate
- explicitly say not to reopen this slice unless fresh evidence appears

---

## Likely code sketch

Minimal intended production shape:

```go
compiled, err := compileSQLQueryString(msg.QueryString, sl, &conn.Identity)
if err != nil {
    sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
    return
}
if err := subscription.ValidatePredicate(compiled.Predicate, validationSchema); err != nil {
    sendOneOffError(conn, msg.MessageID, err.Error(), receipt)
    return
}
```

Important constraint:
- `validationSchema` must expose the full `subscription.SchemaLookup` facts, not just `TableByName(...)`.

---

## Risks and tradeoffs

1. Protocol schema interface widening may ripple through tests.
- Mitigation: keep the new requirement as local as possible; update only the test doubles that already back one-off/subscribe compile paths.

2. Error wording may differ between subscribe and one-off.
- Mitigation: pin user-visible failure semantically, not via over-specific exact strings unless the repo already standardizes the message.

3. Validation may reject other one-off join shapes that were previously accepted accidentally.
- Mitigation: this is acceptable only when the shared subscribe/reference model already rejects them; do not broaden beyond validation parity.

4. `NEXT_SESSION_HANDOFF.md` is already dirty in the worktree.
- Mitigation: read before editing at implementation time and preserve any user-authored changes while updating only the relevant handoff section.

---

## Stop condition

Stop after this one batch if all of the following are true:
- the new one-off unindexed-join rejection pin passes
- focused protocol/subscription/executor tests pass
- full `rtk go test ./...` passes
- docs and handoff are updated

If implementation reveals that protocol cannot access a full validation schema without broad unrelated churn, stop after documenting that blocker in `NEXT_SESSION_HANDOFF.md` and do not silently widen into OI-013 or a larger engine-surface refactor.
