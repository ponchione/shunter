# SPEC-004 E6 Follow-Through Plan

> For Hermes: planning only. Do not implement from this document without a fresh execution turn.

**Goal:** Fix the confirmed-read shutdown bug found in the audit and close the remaining gaps so SPEC-004 E6 is honestly spec-complete rather than “library code landed, runtime seam deferred.”

**Architecture:** Keep the existing package ownership split: `subscription` owns the fan-out worker + semantic payloads, `protocol` owns wire encoding and connection delivery, and `executor` owns post-commit ordering. The follow-through should harden the worker first, then replace the current deferred metadata handoff with an explicit executor→subscription delivery envelope carrying caller metadata and durability readiness.

**Tech Stack:** Go, goroutines/channels, existing `subscription.FanOutWorker`, `protocol.FanOutSenderAdapter`, executor post-commit pipeline, commitlog durability worker.

---

## Current audited state

1. `subscription/fanout_worker.go` blocks on `<-msg.TxDurable` without observing `ctx.Done()`. That violates Story 6.1 clean-exit acceptance when a confirmed-read delivery is waiting on durability.
2. `subscription`/`protocol` package logic exists, but the live post-commit seam still does not populate:
   - `FanOutMessage.TxDurable`
   - `FanOutMessage.CallerConnID`
   - `FanOutMessage.CallerResult`
3. `REMAINING.md` currently marks E6 complete even though those runtime fields are still deferred.
4. `protocol/send_reducer_result.go` still contains the old mutating caller-diversion helper. Even if it is not the main path anymore, it should not retain behavior the audit just flagged as unsafe.
5. Coverage is decent but still missing direct regressions for:
   - cancel while waiting on durability
   - non-mutation of shared fanout maps
   - dropped-channel-full non-blocking behavior
   - exact row-payload roundtrip in `protocol/fanout_adapter.go`
   - direct success-path tests for `SendReducerResult` / `SendSubscriptionError`

---

## Constraints to preserve

- `subscription` MUST NOT import `protocol`.
- Do not invent a new top-level runtime/engine package just to make this slice look wired.
- Stay inside the existing three-package seam:
  - `executor` produces post-commit metadata
  - `subscription` assembles semantic fan-out messages
  - `protocol` encodes and delivers them
- Prefer explicit typed seams over hidden mutation or “set this field later” side effects.
- Use RTK for shell commands during implementation.

---

## Proposed end state

After the follow-through:

1. `FanOutWorker.Run(ctx)` exits cleanly even if a confirmed-read batch is waiting on durability.
2. The executor passes real caller metadata and a real durability readiness channel into the subscription fan-out path for external reducer commits.
3. Non-reducer commits (scheduled/lifecycle/internal) still deliver normal standalone `TransactionUpdate` fanout without pretending to be caller-originated.
4. The protocol adapter is covered by exact payload-fidelity tests, not just row-count checks.
5. Any stale helper that still mutates a shared fanout map is either fixed or removed.
6. `REMAINING.md` only marks E6 done if the runtime seam is actually wired.

---

## Task 1: Lock the audit findings in with failing tests

**Objective:** Add regression tests first so the bug and incomplete areas are forced into the implementation surface.

**Files:**
- Modify: `subscription/fanout_worker_test.go`
- Modify: `protocol/fanout_adapter_test.go`
- Modify: `executor/pipeline_test.go`
- Modify: `executor/subscription_dispatch_test.go`
- Modify: `protocol/send_reducer_result_test.go` or remove the helper later if fully obsolete

**Steps:**
- Add `TestFanOutWorker_ContextCancelWhileWaitingOnTxDurable` in `subscription/fanout_worker_test.go`.
  - Reproduce the audit failure exactly: confirmed-read conn, never-signaled `TxDurable`, cancel the context, expect `Run` to exit promptly.
- Add a direct non-mutation regression in `subscription/fanout_worker_test.go`.
  - Build one `CommitFanout`, call `deliver`, and assert the caller entry still exists in the original map after delivery.
- Add a `dropped` saturation regression in `subscription/fanout_worker_test.go`.
  - Use a full `dropped` channel and a sender that returns `ErrSendBufferFull`; assert delivery returns promptly instead of blocking.
- Strengthen `protocol/fanout_adapter_test.go`.
  - Decode RowList and compare the raw row bytes against BSATN bytes generated from the original `types.ProductValue` rows.
  - Add direct success-path tests for `SendReducerResult` and `SendSubscriptionError`, not just error mapping on `SendTransactionUpdate`.
- Add executor seam tests that fail until caller/durability metadata is wired.
  - One external reducer-commit test should assert the subscription side receives caller metadata.
  - One non-external commit test (scheduled or lifecycle) should assert caller metadata stays nil.
  - One test should assert a non-nil durability readiness channel is propagated when the durability handle supports it.

**Verification target after implementation:**
- `rtk go test ./subscription/... ./protocol/... ./executor/... -run 'FanOutWorker|FanOutSenderAdapter|postCommit|CallReducer'`

---

## Task 2: Fix the shutdown bug in `FanOutWorker`

**Objective:** Make confirmed-read waiting cancellation-safe without changing the existing fast-read semantics.

**Files:**
- Modify: `subscription/fanout_worker.go`
- Modify: `subscription/fanout_worker_test.go`

**Implementation notes:**
- Change `Run` to call `deliver(ctx, msg)` rather than `deliver(msg)`.
- Replace the raw blocking receive with a cancellation-aware wait:
  - if no durable wait is needed, continue immediately
  - if durable wait is needed, `select` on `msg.TxDurable` and `ctx.Done()`
- Preserve the existing nil-`TxDurable` fast path.
- Do not change the “wait for all clients if any client requires confirmed reads” semantics.

**Key checks:**
- cancel while waiting exits cleanly
- already-ready durable channel still delivers immediately
- nil durable channel still behaves as “already durable”

**Verification:**
- `rtk go test ./subscription/... -run 'ContextCancelWhileWaiting|ConfirmedRead|NilTxDurable|FastRead'`
- `rtk go test -race ./subscription/...`

---

## Task 3: Replace the deferred metadata handoff with an explicit executor→subscription seam

**Objective:** Stop relying on “executor will populate these fields later somehow” comments and make caller/durability metadata part of the real post-commit contract.

**Files:**
- Modify: `subscription/fanout.go`
- Modify: `subscription/manager.go`
- Modify: `subscription/eval.go`
- Modify: `executor/interfaces.go`
- Modify: `executor/executor.go`
- Modify: `executor/lifecycle.go`
- Modify: `executor/reducer.go` if a typed post-commit input/result struct is needed
- Modify: `executor/pipeline_test.go`
- Modify: `executor/subscription_dispatch_test.go`

**Recommended shape:**
Introduce one explicit post-commit request/envelope type owned by `subscription`, for example:
- `type PostCommitFanOut struct { TxID, TxDurable, CallerConnID, CallerResult, Changeset/View inputs as needed }`
or
- keep `EvalAndBroadcast(...)` internal and add a new top-level method that takes metadata plus the changeset/view.

Do not keep a design where:
- `EvalAndBroadcast` emits a partial `FanOutMessage`
- then some other layer mutates the emitted message later

The seam should make it impossible to forget caller/durability metadata.

**Behavior to implement:**
- External reducer commit:
  - attach `CallerConnID`
  - attach `CallerResult` seeded with request ID / status / tx ID / error text / energy=0
- Scheduled commit and lifecycle commit:
  - leave caller fields nil
- Every commit:
  - attach `TxDurable` if the durability handle can provide readiness
  - otherwise attach nil and preserve fast-read semantics

**Why this task matters:**
This is the actual path from “tests prove worker logic” to “E6 behavior is live in the post-commit pipeline.”

**Verification:**
- `rtk go test ./executor/... ./subscription/... -run 'postCommit|Lifecycle|CallReducer|EvalAndBroadcast'`
- `rtk go test -race ./executor/... ./subscription/...`

---

## Task 4: Extend durability with an explicit readiness/wait signal

**Objective:** Make confirmed-read delivery operational by giving the executor a real readiness channel to hand to the worker.

**Files:**
- Modify: `executor/interfaces.go`
- Modify: `executor/executor.go`
- Modify: `commitlog/durability.go`
- Modify: `commitlog/phase4_acceptance_test.go`
- Modify: `executor/pipeline_test.go`

**Recommended shape:**
Extend the executor-side durability seam with one readiness API instead of forcing subscription code to poll:
- `DurabilityHandle.EnqueueCommitted(txID, changeset)` stays
- add something like `WaitUntilDurable(txID types.TxID) <-chan types.TxID`

For `commitlog.DurabilityWorker`, implement this by:
- tracking waiter channels keyed by txID, or by a monotonic durable-tx notifier that can satisfy all waiters up to the latest durable tx
- closing/signaling the waiter when the synced durable tx reaches the requested txID
- making already-durable txIDs return an already-ready channel

**Notes:**
- Keep this executor-facing only; the fan-out worker should still just consume a readiness channel.
- Preserve existing enqueue/fatal semantics.

**Verification:**
- dedicated unit tests for already-durable, later-durable, and batched-durable cases
- `rtk go test ./commitlog/... ./executor/... -run 'Durable|postCommit|confirmed'`

---

## Task 5: Make confirmed-read policy safe to use from real callers

**Objective:** Avoid shipping a public mutator API that only works if callers somehow invoke it from the worker goroutine.

**Files:**
- Modify: `subscription/fanout_worker.go`
- Modify: `subscription/fanout_worker_test.go`
- Optionally modify: `subscription/manager.go` if policy changes should flow through manager-owned channels instead of direct worker mutation

**Decision point:**
Pick one of these and do it consistently:

Option A — make the API explicitly internal-only
- unexport the mutators or document them as test/host boot-time only
- use them only before the worker goroutine starts

Option B — make the API concurrency-safe
- guard `confirmedReads` with a mutex, or
- move policy updates through a control channel consumed by `Run`

**Recommendation:** Option B is safer if confirmed reads are meant to be a real runtime feature.
A small control-channel approach is preferable to sprinkling locks because it preserves the worker’s single-owner model.

**Verification:**
- add race-enabled tests that toggle confirmed-read policy while delivery is running
- `rtk go test -race ./subscription/...`

---

## Task 6: Clean up stale mutating helper behavior in `protocol`

**Objective:** Remove or fix old protocol-side fanout logic that still mutates its input map so the codebase has one consistent caller-diversion rule.

**Files:**
- Modify: `protocol/send_reducer_result.go`
- Modify: `protocol/send_reducer_result_test.go`

**Preferred resolution:**
- If this helper is still needed, change it to skip the caller without `delete(fanout, *callerConnID)`.
- If the new `subscription.FanOutWorker` path fully supersedes it, delete the helper and its tests rather than carrying two divergent implementations.

**Reason:**
Leaving the old mutating helper around makes it easy for a future refactor to reintroduce exactly the class of bug that commit `33619e7` fixed.

**Verification:**
- targeted test proving the input fanout map is unchanged after caller diversion

---

## Task 7: Reconcile docs and completion markers with reality

**Objective:** Only mark E6 done once the runtime seam is truly wired.

**Files:**
- Modify: `REMAINING.md`
- Modify: `docs/superpowers/plans/2026-04-16-spec004-e6-fanout-delivery.md` if it needs a follow-up note
- Optionally modify: `TECH-DEBT.md` if any part is intentionally deferred after this slice

**Steps:**
- Remove the current “done but deferred caller/durability wiring” contradiction.
- If all tasks above land, mark E6 done without caveat.
- If any portion remains intentionally deferred after implementation, record it explicitly and stop calling the epic complete.

---

## Suggested implementation order

1. Task 1 — failing tests
2. Task 2 — worker cancellation fix
3. Task 4 — durability readiness channel
4. Task 3 — explicit executor→subscription metadata seam
5. Task 5 — confirmed-read policy safety
6. Task 6 — stale helper cleanup
7. Task 7 — docs/completion markers

This order keeps the real bug fix first, then unlocks the runtime seam work without mixing several moving parts at once.

---

## Validation checklist for the final implementation

Run all of these before calling E6 complete:

- `rtk go test ./subscription/... ./protocol/... ./executor/... ./commitlog/...`
- `rtk go test -race ./subscription/... ./protocol/... ./executor/...`
- `rtk go vet ./subscription/... ./protocol/... ./executor/... ./commitlog/...`

And verify these behaviors explicitly:

- confirmed-read wait is cancel-safe
- caller receives `ReducerCallResult` with embedded `transaction_update` only on successful external reducer commits
- non-caller clients receive standalone `TransactionUpdate`
- lifecycle/scheduled commits do not fabricate caller metadata
- exact row bytes survive `ProductValue -> BSATN -> RowList -> protocol message`
- dropped-channel saturation does not block fan-out
- no code path mutates shared fanout maps during caller diversion

---

## Open questions to resolve during implementation

1. Should confirmed-read policy remain host-configurable only, or is there already an intended protocol/user-facing knob elsewhere in the spec chain?
   - Do not invent a new wire message in this slice unless another spec already requires it.
2. What is the narrowest durability readiness API that avoids leaking commitlog internals into the subscription layer?
   - Prefer a single readiness method on the executor-facing durability seam.
3. Is `protocol/send_reducer_result.go` still part of any live path, or can it be removed entirely once the worker path is the sole implementation?

---

## Bottom line

The minimum honest definition of “spec-complete” for this slice is:
- the cancellation bug is fixed,
- caller/durability metadata flows through the real post-commit pipeline,
- confirmed-read waits can actually happen in live code,
- tests prove those behaviors end-to-end across `executor`, `subscription`, and `protocol`,
- and the docs stop claiming completion while key runtime fields are still deferred.
