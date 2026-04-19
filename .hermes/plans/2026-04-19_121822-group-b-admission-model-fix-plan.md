# Group B Admission-Model Follow-Through Fix Plan

> For Hermes: use subagent-driven-development or execute task-by-task manually. Planning only; do not implement from this document without re-reading the cited files.

Goal: finish the Group B admission-model slice so the live repo actually matches the ADR/plan, closes TD-136 and TD-137 at the root-cause level, removes remaining production-path dependence on `protocol.SubscriptionTracker`, and provides grounded proof for SPEC-005 §9.1/§9.4 behavior.

Architecture: land the ADR's Shape 1 end-state rather than patching around the remaining split. `subscription.Manager.querySets` becomes the only admission authority. Register/unregister responses stop flowing through per-request channels + watcher goroutines and instead use protocol-owned synchronous `Reply` closures invoked inside the executor goroutine, which enqueue Applied/Error frames onto the target connection's `OutboundCh` before subsequent fan-out for that connection can be enqueued.

Tech stack: Go, `rtk` wrapper, `protocol`, `executor`, `subscription` packages, existing repo test helpers.

---

## Current audited state

The repo has only completed the narrow TD-136 / TD-137 gate removals:
- `protocol/send_responses.go` removed the `SendSubscribeSingleApplied` pending gate.
- `protocol/send_txupdate.go` removed `validateActiveSubscriptionUpdates` from `DeliverTransactionUpdateLight`.
- `protocol/sender.go` removed the same gate from typed heavy/light send paths.
- Narrow regression pins were added in `protocol/td136_regression_test.go` and `protocol/td137_regression_test.go`.

The repo has not yet landed the ADR's required end-state:
- `SubscriptionTracker` still exists in production: `protocol/conn.go`
- register/unregister still use `ResponseCh`: `protocol/lifecycle.go`, `executor/command.go`
- handlers still spawn async watchers: `protocol/handle_subscribe_*.go`, `protocol/handle_unsubscribe_*.go`, `protocol/async_responses.go`
- executor still replies by channel send, not synchronous callback: `executor/executor.go`
- stale tracker/gate tests still fail under `rtk go test ./protocol`
- SPEC/docs/comments are ahead of reality in some files and behind reality in others.

Observed failing protocol tests after the narrow hotfixes:
- `TestFanOutSenderAdapter_SendTransactionUpdateLightRejectsPendingSubscription`
- `TestFanOutSenderAdapter_SendTransactionUpdateHeavyRejectsPendingSubscription`
- `TestSendSubscribeSingleAppliedActivatesSubscription`
- `TestSendSubscribeSingleAppliedActivatesBeforeSend`
- `TestSendSubscribeSingleAppliedDiscardsAfterDisconnect`
- `TestSendSubscribeSingleAppliedSendFailureDoesNotLeaveSubscriptionActive`
- `TestDeliverTransactionUpdateLightRejectsPendingSubscription`

Pass target:
- `rtk go test ./protocol -count=1` passes
- no production-path tracker dependence remains for this slice
- no async watcher path remains for subscribe/unsubscribe applied delivery
- ordering/disconnect regression pins exist and pass
- docs/comments describe the live implementation accurately

---

## Scope guardrails

In scope:
- Group B admission-model follow-through only
- files directly named in the ADR/plan and the currently failing protocol tests
- doc updates strictly required to describe the landed Shape 1 state

Out of scope:
- unrelated dirty-worktree files shown by `rtk git status`
- TD-139 compile-time predicate typing cleanup
- broad protocol refactors unrelated to admission model
- new host-adapter work beyond the protocol/executor seam shape required by Group B

---

## Proposed implementation sequence

Use one coherent follow-through branch of small commits. The steps below are ordered to keep the repo buildable and to turn today's partial bridge state into the intended end-state.

### Task 1: Add end-state regression pins before broader refactor

Objective: add the two missing behavior tests that prove the ADR's actual value, not just the two deleted gates.

Files:
- Create: `protocol/admission_ordering_test.go`
- Possibly reuse helpers in: `protocol/test_helpers_test.go`, `protocol/handle_subscribe_test.go`, `protocol/sender_test.go`

Step 1: add `TestAdmissionOrdering_AppliedPrecedesFanoutOnOutboundCh`
- Simulate register success and first fan-out for the same connection/subscription.
- Assert outbound frame order is:
  1. `SubscribeSingleApplied` or `SubscribeMultiApplied`
  2. `TransactionUpdate` / `TransactionUpdateLight` for that subscription
- This test should fail on the current async-watcher model if written against the intended synchronous path, or at minimum be blocked until Task 3-4 wiring lands.

Step 2: add `TestDisconnectBetweenRegisterAndReplyDoesNotSend`
- Close the conn before reply delivery.
- Invoke the eventual reply path.
- Assert no applied frame reaches `OutboundCh` and the path returns/logs a connection-gone outcome rather than resurrecting stale state.

Step 3: run targeted tests
- `rtk go test ./protocol -run 'TestAdmissionOrdering|TestDisconnectBetweenRegisterAndReply' -count=1`

Commit suggestion:
- `protocol(parity): add ordering and disconnect admission pins`

Why first:
- These tests prove the ADR's real contract: §9.4 ordering and §9.1 disconnect-discard via transport-level closure, not tracker state.

---

### Task 2: Reshape protocol/executor register-unregister contracts from ResponseCh to Reply

Objective: change the seam so subscribe/unsubscribe completion is delivered synchronously on the executor goroutine.

Files:
- Modify: `protocol/lifecycle.go`
- Modify: `executor/command.go`
- Modify: `executor/executor.go`
- Modify tests that inspect these request structs:
  - `protocol/handle_subscribe_test.go`
  - `protocol/handle_unsubscribe_test.go`
  - `executor/contracts_test.go`
  - `executor/subscription_dispatch_test.go`

Required code shape:
- `protocol.RegisterSubscriptionSetRequest`:
  - remove `ResponseCh chan<- SubscriptionSetCommandResponse`
  - add `Reply func(SubscriptionSetCommandResponse)`
- `protocol.UnregisterSubscriptionSetRequest`:
  - remove `ResponseCh chan<- UnsubscribeSetCommandResponse`
  - add `Reply func(UnsubscribeSetCommandResponse)`
- `executor.RegisterSubscriptionSetCmd`:
  - remove `ResponseCh chan<- subscription.SubscriptionSetRegisterResult`
  - add `Reply func(protocol.SubscriptionSetCommandResponse)` or equivalent executor-local callback shape that preserves full result+error semantics without channel waiting
- `executor.UnregisterSubscriptionSetCmd`:
  - replace `ResponseCh` similarly
  - delete `UnregisterSubscriptionSetResponse` if it becomes redundant

Executor behavior target:
- `handleRegisterSubscriptionSet` must invoke `cmd.Reply(...)` synchronously before returning to dispatch loop.
- `handleUnregisterSubscriptionSet` must do the same.
- Do not launch goroutines in executor reply handling.
- Preserve current register/unregister result semantics:
  - success => Applied payload populated
  - duplicate/not-found/validation error => `SubscriptionError`

Tests to update:
- replace `ResponseCh != nil` assertions with `Reply != nil`
- invoke captured callback directly instead of sending into a channel
- verify callback payload shape matches the expected Single/Multi/Error arm

Validation:
- `rtk go test ./executor -run 'Test.*Subscription|Test.*Dispatch' -count=1`
- `rtk go test ./protocol -run 'TestHandleSubscribe|TestHandleUnsubscribe' -count=1`

Commit suggestion:
- `protocol(executor): convert subscription-set responses to synchronous Reply closures`

---

### Task 3: Migrate handlers to build conn-bound Reply closures and delete async watchers

Objective: make applied/error enqueue happen directly from executor-triggered synchronous callbacks, not from watcher goroutines.

Files:
- Modify: `protocol/handle_subscribe_single.go`
- Modify: `protocol/handle_subscribe_multi.go`
- Modify: `protocol/handle_unsubscribe_single.go`
- Modify: `protocol/handle_unsubscribe_multi.go`
- Modify: `protocol/async_responses.go`
- Possibly adjust: `protocol/test_helpers_test.go`

Required code shape:
- For each handler, construct `sender := connOnlySender{conn: conn}`.
- Build a closure that switches on the returned response arm and immediately calls the matching send helper.
- Pass that closure through the `RegisterSubscriptionSetRequest` / `UnregisterSubscriptionSetRequest`.
- Remove local `respCh := make(chan ...)` and all `watch*Response(...)` calls.

Delete from `protocol/async_responses.go`:
- `watchSubscribeSetResponse`
- `watchUnsubscribeSetResponse`

Retain:
- `connOnlySender`
- `watchReducerResponse` (not part of this slice)

Critical invariant:
- no subscribe/unsubscribe result delivery goroutine remains after this task
- reply closure execution must stay synchronous with executor register/unregister handling

Validation:
- `rtk go test ./protocol -run 'TestHandleSubscribe|TestHandleUnsubscribe|TestAdmissionOrdering|TestDisconnectBetweenRegisterAndReply' -count=1`

Commit suggestion:
- `protocol(parity): inline subscription replies onto conn outbound queue`

---

### Task 4: Remove remaining tracker behavior from send helpers and connection model

Objective: eliminate all production-path `SubscriptionTracker` usage for this slice.

Files:
- Modify: `protocol/send_responses.go`
- Modify: `protocol/conn.go`
- Modify helpers/tests that construct `Conn` values:
  - `protocol/test_helpers_test.go`
  - `protocol/sender_test.go`
  - `protocol/send_txupdate_test.go`
  - `protocol/reconnect_test.go`
  - `protocol/fanout_adapter_test.go`
  - `protocol/conn_test.go`

Required production changes:
- `protocol/send_responses.go`
  - make `SendUnsubscribeSingleApplied` a straight push
  - make `SendSubscriptionError` a straight push
  - remove stale tracker-centric comments
- `protocol/conn.go`
  - delete `SubscriptionState`
  - delete `SubscriptionTracker`
  - delete `ErrDuplicateSubscriptionID` and protocol-local `ErrSubscriptionNotFound` if no longer used
  - remove `Conn.Subscriptions`
  - remove tracker initialization from `NewConn`

Mandatory search cleanup before commit:
- `SubscriptionTracker`
- `Subscriptions.`
- `ErrDuplicateSubscriptionID`
- protocol-local `ErrSubscriptionNotFound`
- `IsPending(` / `IsActive(` / `IsActiveOrPending(` / `RemoveAll(` if only tracker-related

Validation:
- `rtk go test ./protocol -run 'TestSendSubscribe|TestSendUnsubscribe|TestSendSubscriptionError|TestReconnect' -count=1`
- full grep/sanity search for deleted tracker references

Commit suggestion:
- `protocol(parity): remove subscription tracker from connection state`

---

### Task 5: Update stale protocol tests to the new admission model

Objective: replace tests that encoded deleted tracker/gate behavior with tests that assert manager-authoritative / transport-level behavior.

Files:
- Modify: `protocol/send_responses_test.go`
- Modify: `protocol/send_txupdate_test.go`
- Modify: `protocol/fanout_adapter_test.go`
- Modify: `protocol/sender_test.go`
- Modify: `protocol/handle_subscribe_test.go`
- Modify: `protocol/handle_unsubscribe_test.go`
- Modify: `protocol/reconnect_test.go`
- Delete or rewrite: `protocol/conn_test.go`

Specific rewrites required:

1. `protocol/send_responses_test.go`
- Remove all expectations about pending→active tracker transition during `SendSubscribeSingleApplied`.
- Replace with assertions that the correct frame is sent or that transport-level closed-conn/send-failure semantics are preserved.
- For disconnect/discard cases, test against `closed` / `ErrConnNotFound`, not tracker removal.

2. `protocol/send_txupdate_test.go`
- Replace `TestDeliverTransactionUpdateLightRejectsPendingSubscription` with assertions that updates are delivered when connection exists and skipped/errored only for transport-level reasons.
- Remove tracker setup from happy-path tests.

3. `protocol/fanout_adapter_test.go`
- Replace both `RejectsPendingSubscription` tests with non-gated delivery assertions.
- Keep buffer-full and conn-gone mapping tests.

4. `protocol/reconnect_test.go`
- Remove assertions about fresh tracker state on a new `Conn`.
- Replace with assertions that reconnect gets a new `OutboundCh` / fresh connection object and that no manager live-subscription bucket survives after disconnect (using subscription-manager test seam if available, or protocol fake adapter state if already exposed).

5. `protocol/conn_test.go`
- Delete tracker unit tests entirely if the tracker type is gone.
- Replace only with tests that still make sense for `Conn` itself, if needed.

Validation:
- `rtk go test ./protocol -count=1`

Commit suggestion:
- `protocol(test): migrate admission tests to manager-authoritative model`

---

### Task 6: Remove transitional compatibility shims and stale comments

Objective: clean up temporary leftovers that would otherwise misstate the landed design.

Files:
- Modify: `protocol/send_txupdate.go`
- Modify: `protocol/send_responses.go`
- Modify any test comments referring to tracker seeds or pending-state gates

Required cleanup:
- Remove temporary `ErrSubscriptionNotActive` shim if no tests still reference it.
- Ensure comments in `protocol/send_responses.go` and `protocol/send_txupdate.go` describe only behavior that now truly exists in live code.
- Verify no source comment claims synchronous `Reply` until Tasks 2-3 are already landed in the same branch.

Validation:
- search for `ErrSubscriptionNotActive`
- search for `tracker` / `ResponseCh` / `watchSubscribeSetResponse` / `watchUnsubscribeSetResponse`

Commit suggestion:
- `protocol(cleanup): remove stale admission-model compatibility shims`

---

### Task 7: Update spec/docs and closure notes after code is fully green

Objective: align repo docs with the actual landed implementation, not the partial bridge state.

Files:
- Modify: `docs/decomposition/005-protocol/SPEC-005-protocol.md`
- Modify: `TECH-DEBT.md`
- Optionally modify if still stale: `docs/superpowers/specs/2026-04-18-subscribe-multi-single-split-design.md`

Required doc updates:
- SPEC-005 §9.1 rule 4:
  - stop describing tracker activation as the mechanism
  - describe per-connection FIFO / synchronous applied enqueue behavior instead
- Keep observable behavior unchanged:
  - duplicate ID rejection
  - disconnect-discard for pending registration result
  - no stale activation after disconnect
  - §9.4 ordering guarantee
- Mark TD-136 and TD-137 closed only once full root-cause work is landed.
- Mark TD-140 closed only once tracker removal + synchronous reply conversion have actually landed.
- If TD-138 is incidentally resolved by the landed callback shape, note that explicitly and narrowly.

Validation:
- read docs after edit and confirm they match live code, especially:
  - no tracker prose left in the subscription state machine mechanism description
  - no claim that Group B is complete before code/tests prove it

Commit suggestion:
- `docs(protocol): align subscription semantics with manager-authoritative admission`

---

## File checklist by category

Production files that should change before pass:
- `protocol/lifecycle.go`
- `protocol/handle_subscribe_single.go`
- `protocol/handle_subscribe_multi.go`
- `protocol/handle_unsubscribe_single.go`
- `protocol/handle_unsubscribe_multi.go`
- `protocol/async_responses.go`
- `protocol/send_responses.go`
- `protocol/send_txupdate.go`
- `protocol/conn.go`
- `executor/command.go`
- `executor/executor.go`

Tests that should change before pass:
- `protocol/admission_ordering_test.go` (new)
- `protocol/td136_regression_test.go` (keep)
- `protocol/td137_regression_test.go` (keep)
- `protocol/send_responses_test.go`
- `protocol/send_txupdate_test.go`
- `protocol/fanout_adapter_test.go`
- `protocol/handle_subscribe_test.go`
- `protocol/handle_unsubscribe_test.go`
- `protocol/sender_test.go`
- `protocol/reconnect_test.go`
- `protocol/conn_test.go` (likely delete/replace)
- `executor/contracts_test.go`
- `executor/subscription_dispatch_test.go`

Docs that should change before final merge/pass:
- `docs/decomposition/005-protocol/SPEC-005-protocol.md`
- `TECH-DEBT.md`
- possibly `docs/superpowers/specs/2026-04-18-subscribe-multi-single-split-design.md`

---

## Verification plan

Run in this order after each task cluster:

1. Narrow new pins
- `rtk go test ./protocol -run 'TestTD136|TestTD137|TestAdmissionOrdering|TestDisconnectBetweenRegisterAndReply' -count=1`

2. Protocol handler seam
- `rtk go test ./protocol -run 'TestHandleSubscribe|TestHandleUnsubscribe' -count=1`

3. Executor subscription seam
- `rtk go test ./executor -run 'Test.*Subscription|Test.*Dispatch' -count=1`

4. Subscription manager invariants
- `rtk go test ./subscription -run 'TestRegisterSet|TestDisconnectClient|TestEval' -count=1`

5. Full protocol package
- `rtk go test ./protocol -count=1`

6. Repo-level confidence if desired after protocol green
- `rtk go test ./executor ./subscription ./protocol -count=1`

Search-based exit criteria:
- no production references to `SubscriptionTracker`
- no production references to `Subscriptions.`
- no `watchSubscribeSetResponse` / `watchUnsubscribeSetResponse`
- no `ResponseCh` on register/unregister request structs or executor commands
- no `ErrSubscriptionNotActive` unless intentionally kept in a test-only compatibility surface (prefer removal)

---

## Risks and pitfalls

- Biggest risk: partially converting to `Reply` while leaving watcher-based fallback code around. Do not keep both.
- Do not widen into reducer-response watcher code; `watchReducerResponse` is outside this slice.
- Be careful with import cycles when moving callback types between `protocol` and `executor`; prefer protocol-owned callback payloads, with the executor adapter translating as needed.
- Avoid replacing tracker semantics with a new hidden latch/buffer. The ADR explicitly rejected that shape.
- Do not mark TD-140 closed or rewrite SPEC prose until code/tests prove the full end-state is real.

---

## Definition of done

This plan is complete when all of the following are true:
- `protocol.SubscriptionTracker` and `Conn.Subscriptions` are gone from production code
- subscribe/unsubscribe register paths use synchronous `Reply` closures instead of response channels
- async subscribe/unsubscribe watcher goroutines are gone
- `SendSubscribeSingleApplied`, fan-out light delivery, and typed heavy/light send paths are all tracker-free
- ordering and disconnect regression pins exist and pass
- `rtk go test ./protocol -count=1` passes cleanly
- SPEC/docs/comments match the live manager-authoritative design

---

## Suggested commit sequence

1. `protocol(parity): add ordering and disconnect admission pins`
2. `protocol(executor): convert subscription-set responses to synchronous Reply closures`
3. `protocol(parity): inline subscription replies onto conn outbound queue`
4. `protocol(parity): remove subscription tracker from connection state`
5. `protocol(test): migrate admission tests to manager-authoritative model`
6. `protocol(cleanup): remove stale admission-model compatibility shims`
7. `docs(protocol): align subscription semantics with manager-authoritative admission`
