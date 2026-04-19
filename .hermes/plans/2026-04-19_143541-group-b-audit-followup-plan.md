# Group B Admission-Model Audit Follow-Up Plan

> For Hermes: use subagent-driven-development skill to implement this plan task-by-task. Planning only; do not implement from this document without re-reading the cited files.

Goal: resolve the post-audit minor concerns from the Group B admission-model follow-through slice so the docs and test surface precisely match the landed manager-authoritative design, without broadening the scope back into new protocol or subscription behavior work.

Architecture: keep the already-landed production design intact. This follow-up is a narrow docs-and-test-surface reconciliation pass: fix the one remaining SPEC-005 rule that overstates a no-longer-exposed pending-state behavior, rename stale test names that still describe removed async reply delivery, and optionally add a bounded proof note / future task for the missing concrete executor-to-protocol adapter bridge. Do not change production admission behavior unless a read pass proves an actual code bug beyond the current audit.

Tech stack: Go, markdown docs, `rtk` wrapper, `protocol`, `docs/decomposition/005-protocol`, `.hermes/plans`, existing protocol/executor tests.

---

## Current context

Grounded from the audit and live code:

- The production contract is already manager-authoritative and synchronous at the executor seam:
  - `executor/executor.go:247-275`
  - `protocol/handle_subscribe_single.go:33-54`
  - `protocol/handle_subscribe_multi.go:39-60`
  - `protocol/handle_unsubscribe_single.go:21-41`
  - `protocol/handle_unsubscribe_multi.go:22-42`
- The only material docs/code drift found in the audit is SPEC-005 §9.1 rule 2:
  - current prose: `docs/decomposition/005-protocol/SPEC-005-protocol.md:502`
  - live behavior source: `subscription/register_set.go:245-249`
  - registration lifecycle source: `subscription/register_set.go:228-232`, `executor/executor.go:247-275`
- The remaining loose-end code smell is misleading test naming only:
  - `protocol/handle_subscribe_test.go:288` `TestHandleSubscribeSingle_DeliversAsyncSubscribeApplied`
  - `protocol/handle_subscribe_test.go:419` `TestHandleSubscribeMulti_DeliversAsyncMultiApplied`
  - `protocol/handle_unsubscribe_test.go:45` `TestHandleUnsubscribeSingle_DeliversAsyncUnsubscribeApplied`
  - `protocol/handle_unsubscribe_test.go:127` `TestHandleUnsubscribeMulti_DeliversAsyncMultiApplied`
- Repo-wide tests were green at audit time:
  - `rtk go test ./protocol -count=1`
  - `rtk go test ./... -count=1`

Non-goals for this plan:

- do not redesign the admission model
- do not add tracker-like latches, hidden buffering, or new protocol state
- do not introduce new production adapter packages unless a concrete host-adapter location already exists and is clearly in scope
- do not edit unrelated SPEC-005 sections just for wording polish

---

## Proposed approach

Use three narrow tasks:

1. Reconcile SPEC-005 §9.1 rule 2 with the live manager-authoritative unregister path.
2. Rename stale protocol handler tests so their names match the synchronous `Reply`-closure design.
3. Record the still-missing concrete executor/protocol adapter proof as an explicit deferred validation item, only if the repo currently lacks a real adapter implementation to test.

The success bar is “docs and test names no longer mislead a future maintainer.” The bar is not “invent new runtime behavior.”

---

## Task 1: Re-audit the exact unregister state contract before editing docs

Objective: prove precisely what the live code does for unregister of unknown vs. in-flight registrations so the doc fix is mechanism-accurate.

Files:
- Read: `docs/decomposition/005-protocol/SPEC-005-protocol.md`
- Read: `subscription/register_set.go`
- Read: `executor/executor.go`
- Read: `protocol/handle_unsubscribe_single.go`
- Read: `protocol/handle_unsubscribe_multi.go`
- Read: `subscription/manager_test.go`

Step 1: Verify the current rule text
- Read `docs/decomposition/005-protocol/SPEC-005-protocol.md` around §9.1.
- Capture the exact current wording of rule 2.

Step 2: Trace live unregister behavior
- Inspect `subscription/register_set.go:240-284`.
- Confirm that the only direct `ErrSubscriptionNotFound` return is when `(ConnID, QueryID)` is absent from `m.querySets`.

Step 3: Trace register visibility timing
- Inspect `subscription/register_set.go:228-232` and `executor/executor.go:247-275`.
- Confirm whether there is any externally observable “pending but not found” window in the current synchronous design.

Step 4: Check current tests for the real contract
- Read `subscription/manager_test.go` around `TestUnregisterUnknown`.
- Search for any tests asserting a separate pending-unsubscribe semantic.

Validation:
- `rtk grep -n "ErrSubscriptionNotFound|Unregister" subscription/register_set.go subscription/manager_test.go docs/decomposition/005-protocol/SPEC-005-protocol.md`

Expected outcome:
- a precise replacement sentence for SPEC-005 rule 2, grounded in live code, not in the pre-ADR tracker-era state machine.

---

## Task 2: Patch SPEC-005 §9.1 rule 2 to match the live mechanism

Objective: remove the misleading “pending or unknown” wording and replace it with language that matches the landed manager-authoritative unregister behavior.

Files:
- Modify: `docs/decomposition/005-protocol/SPEC-005-protocol.md`

Preferred edit shape:
- Keep the surrounding state-machine section intact.
- Replace only the rule-2 bullet and, if needed, one adjacent explanatory clause.
- Preserve the higher-level user-visible intent while removing mechanism drift.

Recommended target wording direction:
- replace “pending or unknown” with language like “unknown / not-live on this connection” or “not registered under the manager’s `(ConnID, QueryID)` set registry”
- do not imply a separate protocol-tracker pending bucket still exists
- if you mention pending at all, phrase it in terms of the current executor/manager path, not a tracker state machine

Example acceptable end-state (adapt wording to fit surrounding prose):
- “`Unsubscribe` for a `subscription_id` that is not currently registered under the connection’s live `(ConnID, QueryID)` set registry returns `ErrSubscriptionNotFound`.”

Step 1: Make the narrow doc edit
- Update only the specific bullet and any immediately adjacent prose that would become contradictory.

Step 2: Re-read §9.1 as a whole
- Check that rules 1, 3, and 4 still read coherently after the edit.
- Ensure rule 2 does not reintroduce tracker-era terminology.

Step 3: Search for nearby stale references
- Search SPEC-005 for other prose that explicitly relies on the old pending-tracker mechanism for unsubscribe semantics.
- Only patch additional lines if they are directly contradictory.

Validation:
- `rtk grep -n "pending or unknown|ErrSubscriptionNotFound|tracker|connOnlySender|OutboundCh FIFO" docs/decomposition/005-protocol/SPEC-005-protocol.md`

Expected outcome:
- SPEC-005 §9.1 accurately describes the live manager-authoritative unregister contract.

---

## Task 3: Rename stale protocol handler tests that still claim async delivery

Objective: align test names with the landed synchronous `Reply`-closure design so future audits and grep-based reviews do not get misled.

Files:
- Modify: `protocol/handle_subscribe_test.go`
- Modify: `protocol/handle_unsubscribe_test.go`

Exact renames to apply:
- `TestHandleSubscribeSingle_DeliversAsyncSubscribeApplied`
  -> `TestHandleSubscribeSingle_DeliversSubscribeAppliedViaReplyClosure`
- `TestHandleSubscribeMulti_DeliversAsyncMultiApplied`
  -> `TestHandleSubscribeMulti_DeliversMultiAppliedViaReplyClosure`
- `TestHandleUnsubscribeSingle_DeliversAsyncUnsubscribeApplied`
  -> `TestHandleUnsubscribeSingle_DeliversUnsubscribeAppliedViaReplyClosure`
- `TestHandleUnsubscribeMulti_DeliversAsyncMultiApplied`
  -> `TestHandleUnsubscribeMulti_DeliversMultiAppliedViaReplyClosure`

Step 1: Rename the test functions only
- Do not change assertions unless the read pass reveals hidden async assumptions.

Step 2: Update any nearby comments that still say async
- Keep comments minimal and mechanism-accurate.
- Preferred terminology: “reply closure” or “executor-supplied reply closure,” not “async response.”

Step 3: Search for other stale “async” wording in the protocol tests
- Search the protocol package for `AsyncSubscribeApplied`, `AsyncMultiApplied`, `async unsubscribe`, and similar phrases.
- Patch only clearly stale references related to this slice.

Validation:
- `rtk go test ./protocol -run 'TestHandleSubscribe|TestHandleUnsubscribe' -count=1`
- `rtk grep -rn "DeliversAsync|async .*Applied|async .*reply" protocol/ --include="*_test.go"`

Expected outcome:
- test names and comments accurately describe the synchronous reply model.

---

## Task 4: Re-run focused and package-level verification

Objective: prove the doc/test-surface cleanup did not accidentally disturb the green protocol package.

Files:
- No code changes expected beyond Task 2 and Task 3

Step 1: Focused protocol handler tests
- Run: `rtk go test ./protocol -run 'TestHandleSubscribe|TestHandleUnsubscribe' -count=1`

Step 2: Full protocol package
- Run: `rtk go test ./protocol -count=1`
- Expected: package remains fully green

Step 3: No new skip debt
- Run: `rtk grep -rn "t\.Skip" protocol/ --include="*.go"`
- Expected: no hits

Step 4: Optional repo-level confidence
- Run: `rtk go test ./... -count=1`
- Use if the branch already expects a clean repo-wide run and time budget allows

Expected outcome:
- the cleanup is proven non-disruptive

---

## Task 5: Decide how to record the still-missing adapter-level proof

Objective: handle the audit’s “should consider” item honestly without turning it into speculative implementation work.

Files:
- Read: any live host adapter / engine wiring package if one exists
- Possibly modify: `TECH-DEBT.md` only if there is no existing debt item and the repo wants this tracked explicitly
- Possibly create: a new `.hermes/plans/...` follow-up plan only if the work is clearly larger than a note

Decision tree:

1. Search for a concrete non-test adapter implementation bridging:
   - `protocol.RegisterSubscriptionSetRequest.Reply(func(SubscriptionSetCommandResponse))`
   - to executor `RegisterSubscriptionSetCmd.Reply(func(subscription.SubscriptionSetRegisterResult, error))`
2. If no production adapter exists:
   - do not invent one in this follow-up
   - optionally add a narrow note to TECH-DEBT or an execution plan that this proof remains deferred until a real host adapter lands
3. If a production adapter already exists:
   - write a small follow-up plan to add an adapter-level error-path test
   - keep it separate from this docs/test-name cleanup pass unless the implementation is truly trivial and already in scope

Validation search examples:
- `rtk grep -rn "RegisterSubscriptionSet(ctx|UnregisterSubscriptionSet(ctx|RegisterSubscriptionSetCmd|UnregisterSubscriptionSetCmd" --include="*.go"`

Expected outcome:
- future work is either explicitly deferred or grounded in a real adapter location, not hand-wavy

---

## Risks and pitfalls

- Biggest risk: accidentally “fixing” the spec by reintroducing tracker-era language in new words. Keep the doc grounded in `querySets`, synchronous `Reply`, and transport discard via `connOnlySender`.
- Do not broaden Task 2 into a rewrite of the whole §9.1 state-machine section unless the reread proves another concrete contradiction.
- Do not change test behavior while renaming tests unless a read proves the names were masking a real semantic mismatch.
- If you touch `TECH-DEBT.md`, keep it grounded and minimal; do not close or open debt speculatively.

---

## Definition of done

This follow-up is complete when all of the following are true:

- SPEC-005 §9.1 rule 2 no longer overstates a tracker-era “pending or unknown” unregister mechanism
- the four protocol handler tests no longer claim async applied delivery
- focused protocol handler tests pass
- `rtk go test ./protocol -count=1` still passes
- any remaining adapter-level proof gap is either explicitly deferred or grounded to a concrete production adapter location

---

## Suggested execution order

1. Re-read unregister/register timing and confirm exact live contract
2. Patch SPEC-005 §9.1 rule 2 narrowly
3. Rename stale async test names in protocol handler tests
4. Re-run focused and package-level protocol tests
5. Decide whether to record the adapter-level proof gap as deferred follow-up
