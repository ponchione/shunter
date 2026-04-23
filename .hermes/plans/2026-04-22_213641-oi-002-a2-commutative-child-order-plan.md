# OI-002 A2 commutative child-order canonicalization implementation plan

> For Hermes: Use subagent-driven-development skill to implement this plan task-by-task.

Goal: Close the bounded OI-002 A2 slice where already-accepted same-table `AND` / `OR` SQL differs only by child order yet still produces different canonical query hashes and separate query-state entries.

Architecture: Keep the fix as small as possible. The parser and runtime may continue to preserve source order for ordinary evaluation, but canonical identity must stop depending on commutative child order for the narrow accepted same-table shapes in scope. The safest seam is the canonicalization/hash path used by `RegisterSet`, with protocol and one-off tests proving visible behavior was already equivalent before the hash fix.

Tech stack: Go, existing SQL parser in `query/sql`, protocol SQL compilation in `protocol/handle_subscribe.go` + `protocol/handle_oneoff.go`, subscription hash/registry logic in `subscription/hash.go` + `subscription/register_set.go`, focused package tests under `protocol/` and `subscription/`.

---

## Confirmed mismatch from the live scout

- `query/sql/parser.go:701-729` builds left-associative `AndPredicate` / `OrPredicate` trees and preserves source child order.
- `protocol/handle_subscribe.go:74-102` only normalizes neutral `TRUE`; it does not canonicalize commutative child order.
- `protocol/handle_subscribe.go:296-315` compiles SQL `AndPredicate` / `OrPredicate` into runtime `subscription.And` / `subscription.Or` without reordering.
- `subscription/hash.go:87-118` canonicalizes `TRUE`-style identities but preserves `And` / `Or` child order.
- `subscription/hash_test.go:69-77` currently pins that `And` child order changes the hash.
- `subscription/register_set.go:230-312` dedups and query-state sharing are keyed by `ComputeQueryHash(...)`, so accepted commutative SQL can still create separate query states even when user-visible row results match.

That is the exact residual this slice should close.

## Scope boundary

In scope:
- already-accepted same-table SQL only
- commutative `AND` / `OR` child-order drift
- canonical query hash / query-state identity / register dedup behavior
- focused proof that visible one-off row results were already equal

Out of scope:
- SQL widening
- join admission changes
- projected join ordering
n- join/cross-join multiplicity
- `:sender` provenance/hash identity
- neutral-`TRUE` normalization beyond ensuring it still passes
- A3 recovery/store work
- Tier B hardening work

## Likely files to inspect and/or change

Primary code files:
- `subscription/hash.go`
- `subscription/register_set.go`
- `protocol/handle_subscribe.go`
- `protocol/handle_oneoff.go`
- `query/sql/parser.go`
- `subscription/predicate.go`

Primary test files:
- `subscription/hash_test.go`
- `subscription/manager_test.go`
- `protocol/handle_oneoff_test.go`
- `protocol/handle_subscribe_test.go`
- optionally `query/sql/parser_test.go` only if a parser-shape pin is needed for the scout narrative

Docs to update in the same session after the code lands:
- `TECH-DEBT.md`
- `docs/parity-phase0-ledger.md`
- `docs/spacetimedb-parity-roadmap.md`
- `NEXT_SESSION_HANDOFF.md`

## Implementation strategy

Use a proof-first sequence:
1. Prove accepted child-order pairs already parse and return the same visible rows.
2. Prove hash / query-state identity still drifts.
3. Fix only canonical identity, not broad parser behavior.
4. Re-run focused tests, then the broader package set, then full repo tests.
5. Refresh the four status docs in the same session.

Prefer the minimal production fix in `subscription/hash.go` unless a focused failing test proves a second seam must change.

## Task 1: Add scout pins that separate visible semantics from canonical identity

Objective: Lock the pre-fix reality with tests so the implementation cannot accidentally widen scope.

Files:
- Modify: `protocol/handle_oneoff_test.go`
- Modify: `subscription/hash_test.go`
- Modify: `subscription/manager_test.go`
- Optional modify: `protocol/handle_subscribe_test.go`

Step 1: Add a one-off equality pin for same-table `AND`

Suggested test shape in `protocol/handle_oneoff_test.go`:
- register a simple `users` table with indexed `id` and string `name`
- seed rows including `{1, "alice"}` and non-matching distractors
- execute both:
  - `SELECT * FROM users WHERE id = 1 AND name = 'alice'`
  - `SELECT * FROM users WHERE name = 'alice' AND id = 1`
- decode both `OneOffQueryResponse` payloads and assert identical row lists

Expected result before code changes: PASS

Step 2: Add the analogous one-off equality pin for same-table `OR`

Suggested pair:
- `SELECT * FROM users WHERE id = 1 OR id = 2`
- `SELECT * FROM users WHERE id = 2 OR id = 1`

Expected result before code changes: PASS

Step 3: Add failing hash pins for commutative same-table order

Replace or narrow the current order-sensitive hash pin in `subscription/hash_test.go`.

Recommended split:
- keep order-sensitive coverage only for out-of-scope shapes if still needed later
- add new in-scope failing tests:
  - `TestQueryHashSameTableAndChildOrderCanonicalized`
  - `TestQueryHashSameTableOrChildOrderCanonicalized`

Suggested predicate values:
- `And{Left: ColEq{Table: 1, Column: 0, Value: 1}, Right: ColEq{Table: 1, Column: 1, Value: "alice"}}`
- reversed child order
- same pattern for `Or`

Expected result before code changes: FAIL, because hashes still differ.

Step 4: Add failing query-state sharing pins in `subscription/manager_test.go`

Add focused tests that register the two orderings on separate connections and assert one shared query state.

Suggested names:
- `TestRegisterSet_SameTableAndChildOrderSharesQueryState`
- `TestRegisterSet_SameTableOrChildOrderSharesQueryState`

Expected result before code changes: FAIL, because `len(mgr.registry.byHash)` becomes 2 instead of 1.

Step 5: Optional protocol subscribe proof

Only if needed for clarity, add a small `protocol/handle_subscribe_test.go` case proving both SQL strings are accepted and forwarded with literal `PredicateHashIdentities == nil`. Do not try to assert dedup here; that belongs in `subscription/manager_test.go`.

## Task 2: Implement the minimal canonicalization seam

Objective: Make canonical query identity insensitive to commutative child order for the accepted same-table shapes in scope.

Files:
- Modify: `subscription/hash.go`
- Maybe modify: `subscription/register_set.go` only if tests show canonicalized predicate storage needs explicit tightening
- Avoid touching parser/protocol unless strictly necessary

Step 1: Add a helper that decides when `And` / `Or` children are safely reorderable

Recommended rule for this slice:
- both children are non-nil
- both children resolve to the same single table via `Tables()`
- no join/cross-join widening logic
- retain existing `TRUE` simplifications first

This keeps the change bounded to the handoff’s accepted same-table target.

Step 2: Add a stable child-order comparison based on canonical child encoding

Recommended approach:
- canonicalize each child recursively first
- encode each child into canonical bytes using the existing encoder machinery
- compare byte slices lexicographically
- for reorderable commutative nodes, place the lexicographically smaller child on the left

Why this approach:
- uses the repo’s existing canonical encoder rather than inventing a new ad hoc rank system
- works for `ColEq`, `ColNe`, `ColRange`, and nested same-table commutative subtrees
- keeps behavior deterministic and easy to explain in tests

Step 3: Apply the helper inside `canonicalizePredicate`

Target cases:
- `And`
- `Or`

Order of operations:
1. recursively canonicalize children
2. apply existing `AllRows` simplifications
3. if the node is a reorderable commutative same-table shape, reorder children deterministically
4. return the canonical node

Step 4: Keep the production change minimal

Do not:
- change parser associativity
- broaden SQL acceptance
- alter `MatchRow` / `MatchRowSide`
- alter join hashing unless a focused test proves the same bug exists there and the user asked to widen scope

## Task 3: Re-run and tighten the tests

Objective: Turn the new pins green and ensure closed slices did not regress.

Files:
- Modify tests only as needed for stable assertions

Focused commands to run:
- `rtk go test ./subscription -run 'QueryHash|RegisterSet.*ChildOrder|TrueAndComparison' -count=1`
- `rtk go test ./protocol -run 'HandleOneOffQuery_.*(And|Or)|HandleSubscribe.*sender|TrueAnd|TrueOr' -count=1`
- `rtk go test ./protocol ./subscription ./executor -count=1`
- `rtk go test ./... -count=1`

Environment note from this planning session:
- the current CLI environment does not have `go` on PATH, so I could not execute these here (`which go` failed, `go version` failed). The implementation session should still attempt the commands above in a Go-capable environment and treat them as required verification gates.

## Task 4: Update the live docs in the same session

Objective: Record closure of the new narrow A2 slice without reopening old ones.

Files:
- Modify: `TECH-DEBT.md`
- Modify: `docs/parity-phase0-ledger.md`
- Modify: `docs/spacetimedb-parity-roadmap.md`
- Modify: `NEXT_SESSION_HANDOFF.md`

Step 1: `TECH-DEBT.md`
- remove or narrow the remaining OI-002 summary so it no longer lists commutative child-order drift as open
- keep the remaining broader A2 theme open
- explicitly preserve the already-closed slices

Step 2: `docs/parity-phase0-ledger.md`
- add a new closed `P0-SUBSCRIPTION-*` row for accepted same-table commutative `AND` / `OR` child-order canonicalization
- authoritative tests should include at least `subscription/hash_test.go`, `subscription/manager_test.go`, and `protocol/handle_oneoff_test.go`

Step 3: `docs/spacetimedb-parity-roadmap.md`
- mark the “best current narrow-ready direction” as closed
- name the next strongest residual only if a fresh scout after the fix identifies one

Step 4: `NEXT_SESSION_HANDOFF.md`
- replace the current “next session” item with the next bounded residual discovered after this slice
- do not leave the handoff pointing at already-closed commutative child-order work

## Task 5: Final verification and commit framing

Objective: End the batch in a reviewable state with one commit for this slice.

Suggested verification checklist:
- one-off `AND` reordered pair returns identical rows
- one-off `OR` reordered pair returns identical rows
- same-table `AND` reordered pair hashes identically
- same-table `OR` reordered pair hashes identically
- reversed-order subscribe registrations now share one query state
- neutral-`TRUE` tests still pass
- `:sender` hash-identity tests still pass
- broader package tests pass in a Go-capable environment

Suggested commit message:
- `fix: canonicalize commutative same-table predicate child order`

## Risks and guardrails

Risk 1: Accidentally widening into joins or multi-table predicate normalization
- Guardrail: restrict reorderability to same-table shapes only for this slice

Risk 2: Replacing too much existing hash behavior
- Guardrail: preserve existing non-commutative distinctions and join projection/alias identity tests

Risk 3: Hiding a bug by testing only hashes
- Guardrail: prove visible one-off row equality separately from hash/query-state equality

Risk 4: Regressing closed neutral-`TRUE` behavior
- Guardrail: keep those tests in the focused run list

## Open questions to resolve during implementation

1. Should the old `TestQueryHashAndOrderMatters` be deleted entirely or narrowed to an out-of-scope multi-table/join shape?
   - Recommended: replace it with a narrower out-of-scope pin only if it still protects something real.

2. Is `subscription/register_set.go` code change needed beyond `ComputeQueryHash(...)` using the new canonicalization?
   - Likely no, because `RegisterSet` already canonicalizes and keys state by the hash.

3. Does any protocol-level test need to assert canonicalized predicate structure, or is row-equality proof + manager/hash proof enough?
   - Recommended default: row-equality proof + manager/hash proof is enough.

## Ready-to-execute summary

The cleanest plan is:
- prove same visible rows first in `protocol/handle_oneoff_test.go`
- prove hash/query-state drift next in `subscription/hash_test.go` and `subscription/manager_test.go`
- fix only `subscription/hash.go` unless a focused failing test proves another seam must change
- rerun focused tests, then `./protocol ./subscription ./executor`, then `./...`
- update `TECH-DEBT.md`, `docs/parity-phase0-ledger.md`, `docs/spacetimedb-parity-roadmap.md`, and `NEXT_SESSION_HANDOFF.md` in the same session
