# Group B — Subscription admission model (Shape 1) implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close TD-136 (C1) and TD-137 (C2) together by retiring `protocol.SubscriptionTracker`, deleting the wire-id admission gate in fan-out, and reshaping the `ExecutorInbox` subscription-set contract to a synchronous `Reply` closure that enqueues the Applied / Error envelope on the target `Conn.OutboundCh` inside the executor main-loop goroutine. Matches reference `ModuleSubscriptionManager` + `send_worker_queue` discipline.

**Architecture:** `subscription.Manager.querySets` becomes the single admission source of truth. Applied delivery happens synchronously inside the executor's command handler via a protocol-owned `Reply func(SubscriptionSetCommandResponse)` closure; fan-out drops the per-wire-id `IsActive` check; disconnect-discard is handled by `connOnlySender`'s existing `<-c.closed` guard. See `docs/adr/2026-04-19-subscription-admission-model.md` for the decision record.

**Tech Stack:** Go, `rtk` command wrapper, existing `protocol` / `executor` / `subscription` packages. Strict TDD per `superpowers:test-driven-development`.

---

## Pre-read for the executor

Before Task 0, read:

1. `docs/adr/2026-04-19-subscription-admission-model.md` — the decision record this plan executes.
2. `TECH-DEBT.md` entries TD-136, TD-137, TD-138, TD-140.
3. `CLAUDE.md` and `RTK.md`.
4. The four files the ADR's "Contract changes" table lists.
5. Reference call-site: `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:841-1101` — do not port, only confirm semantics.

Scope guardrails from `NEXT-SESSION-PROMPT.md`:

- Do not stage or touch the pre-existing drift files listed at session start (commitlog, schema, store, select executor tests). Stage each file explicitly by path.
- No sweeping style cleanup.
- Commits: one logical step per commit. Follow existing commit-message style (see `rtk git log --oneline HEAD -20`).

---

## File-structure map

Files modified or created by this plan:

**Created:**
- `protocol/td136_regression_test.go` — C1 pin.
- `protocol/td137_regression_test.go` — C2 pin.
- `protocol/admission_ordering_test.go` — §9.4 ordering + §9.1 disconnect-discard pins.

**Modified — production code:**
- `protocol/conn.go` — delete `SubscriptionTracker`, `SubscriptionState`, `ErrDuplicateSubscriptionID`, `ErrSubscriptionNotFound` (the last conflicts with `subscription.ErrSubscriptionNotFound` — that sentinel stays in `subscription`), and the `Conn.Subscriptions` field.
- `protocol/send_responses.go` — `SendSubscribeSingleApplied`, `SendUnsubscribeSingleApplied`, `SendSubscriptionError` become tracker-free straight pushes.
- `protocol/send_txupdate.go` — delete `validateActiveSubscriptionUpdates`, `ErrSubscriptionNotActive`, and the gate branch in `DeliverTransactionUpdateLight`.
- `protocol/async_responses.go` — delete `watchSubscribeSetResponse` and `watchUnsubscribeSetResponse`; retain `connOnlySender` and `watchReducerResponse` (unaffected).
- `protocol/lifecycle.go` — `RegisterSubscriptionSetRequest.Reply` / `UnregisterSubscriptionSetRequest.Reply` replace `ResponseCh`.
- `protocol/handle_subscribe_single.go` / `handle_subscribe_multi.go` / `handle_unsubscribe_single.go` / `handle_unsubscribe_multi.go` — construct `Reply` closure bound to `connOnlySender{conn}`; pass through `ExecutorInbox` call.
- `executor/command.go` — `RegisterSubscriptionSetCmd.Reply` / `UnregisterSubscriptionSetCmd.Reply` replace `ResponseCh`. Deletes `UnregisterSubscriptionSetResponse` (subsumed into `protocol.UnsubscribeSetCommandResponse`).
- `executor/executor.go` — `handleRegisterSubscriptionSet` / `handleUnregisterSubscriptionSet` invoke `cmd.Reply(...)` synchronously.

**Modified — test code:**
- `protocol/send_responses_test.go` — remove tracker seeds; collapse state-machine assertions that referenced the deleted type.
- `protocol/send_txupdate_test.go` — remove tracker seeds; retain the fan-out shape assertions.
- `protocol/handle_subscribe_test.go` / `handle_unsubscribe_test.go` — migrate fake inboxes from `ResponseCh` to `Reply` closures; drop the masking `Reserve(7)` seed.
- `protocol/sender_test.go` — remove tracker seeds; adapt assertions that mutated tracker state.
- `protocol/reconnect_test.go` — remove tracker-based assertions (the reconnect test was asserting that a new `Conn` starts with a fresh tracker; adapt to assert fresh `OutboundCh` and absence of live `querySets[conn]` bucket at the manager).
- `protocol/fanout_adapter_test.go` — remove tracker seeds.
- `protocol/conn_test.go` — delete the tracker unit tests (the tracker no longer exists).
- `executor/contracts_test.go` — migrate from `ResponseCh` to `Reply`.
- `executor/subscription_dispatch_test.go` — migrate from `ResponseCh` to `Reply`.
- `executor/pipeline_test.go` — verify no tracker references; update if any slipped in.

**Modified — docs:**
- `docs/decomposition/005-protocol/SPEC-005-protocol.md` — §9.1 rule 4 prose re-framed per ADR.
- `TECH-DEBT.md` — close TD-136, TD-137, TD-138, TD-140 with PR link + date.
- `NEXT-SESSION-PROMPT.md` — mark Group B complete; remaining = Group C (TD-139 code + TD-138 close note), Task 10 (host adapter).

---

## Task 0: Branch + commit ADR

**Files:**
- Stage only: `docs/adr/2026-04-19-subscription-admission-model.md`

**Steps:**

- [ ] **Step 1: Confirm working-tree drift matches brief's ignore-list**

Run: `rtk git status --porcelain`

Expected modified files (pre-existing drift, do NOT stage): `commitlog/snapshot_select_test.go`, `executor/lifecycle_test.go`, `executor/phase4_acceptance_test.go`, `executor/scheduler_test.go`, `executor/scheduler_worker_test.go`, `protocol/parity_close_codes_test.go`, `schema/errors.go`, `schema/reflect_test.go`, `store/audit_regression_test.go`, `store/transaction.go`.

Expected untracked: `docs/adr/` (our new ADR) and `docs/superpowers/plans/2026-04-19-group-b-admission-model-shape1.md` (this plan).

If the modified set differs, stop and flag.

- [ ] **Step 2: Create branch from `phase-2-slice-2-subscribe-multi`**

Run: `rtk git checkout -b phase-2-slice-2-td140-admission-model`

Expected output: `Switched to a new branch 'phase-2-slice-2-td140-admission-model'`.

- [ ] **Step 3: Stage only the ADR**

Run: `rtk git add docs/adr/2026-04-19-subscription-admission-model.md docs/superpowers/plans/2026-04-19-group-b-admission-model-shape1.md`

Run: `rtk git status --porcelain`

Expected: `A docs/adr/2026-04-19-subscription-admission-model.md` and `A docs/superpowers/plans/2026-04-19-group-b-admission-model-shape1.md`. The pre-existing drift should still be shown as `M` but unstaged.

- [ ] **Step 4: Commit ADR + plan**

Run:
```bash
rtk git commit -m "$(cat <<'EOF'
docs(adr): subscription admission model (TD-140) — manager-authoritative Shape 1

ADR selecting Shape 1 from the admission-model options: retire
protocol.SubscriptionTracker; subscription.Manager.querySets becomes the
single source of truth; ExecutorInbox register/unregister gain a
synchronous Reply closure that enqueues the Applied/Error envelope on
Conn.OutboundCh inside the executor main-loop goroutine. Fan-out's
per-wire-id admission gate is deleted; SPEC-005 §9.4 ordering is
preserved by per-connection OutboundCh FIFO + executor-goroutine
serialization — reference-parity analog of send_worker_queue.

Unblocks Group B (TD-136 + TD-137 fixes) and the host-adapter slice
(Task 10 of the Phase 2 Slice 2 plan). Group B plan:
docs/superpowers/plans/2026-04-19-group-b-admission-model-shape1.md.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

Expected: commit succeeds, hooks (if any) pass.

---

## Task 1: Fix C1 (SendSubscribeSingleApplied gate removal) + regression pin

**Files:**
- Create: `protocol/td136_regression_test.go`
- Modify: `protocol/send_responses.go`

**Rationale:** `SendSubscribeSingleApplied` currently early-returns when `conn.Subscriptions.IsPending(msg.QueryID)` is false. Post-split, `handleSubscribeSingle` no longer calls `Reserve`, so production delivery is silently dropped. The minimal fix (pre-full-reshape) is to collapse `SendSubscribeSingleApplied` to a straight push identical in shape to `SendSubscribeMultiApplied`. This is safe today because `SubscribeMultiApplied` delivery already goes through the same path without gating.

**Steps:**

- [ ] **Step 1: Write the failing regression pin**

Create `protocol/td136_regression_test.go`:

```go
package protocol

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestTD136_SubscribeSingleAppliedReachesWireWithoutTrackerSeed pins the
// C1 regression from TD-140/TD-136: SendSubscribeSingleApplied must deliver
// the Applied envelope on a fresh Conn with no prior tracker Reserve seed.
// Pre-fix, the IsPending guard silently dropped the message in production
// because handleSubscribeSingle no longer Reserves the QueryID.
func TestTD136_SubscribeSingleAppliedReachesWireWithoutTrackerSeed(t *testing.T) {
	t.Parallel()
	conn := newTestConn(t, types.ConnectionID{0x01})
	sender := &recordingSender{}
	msg := &SubscribeSingleApplied{
		RequestID: 42,
		QueryID:   7,
		TableName: "users",
		Rows:      []byte{0x01, 0x02},
	}

	if err := SendSubscribeSingleApplied(sender, conn, msg); err != nil {
		t.Fatalf("SendSubscribeSingleApplied returned error: %v", err)
	}

	if len(sender.sent) != 1 {
		t.Fatalf("expected one Send call, got %d", len(sender.sent))
	}
	got, ok := sender.sent[0].msg.(SubscribeSingleApplied)
	if !ok {
		t.Fatalf("sender.sent[0].msg type = %T, want SubscribeSingleApplied", sender.sent[0].msg)
	}
	if got.QueryID != msg.QueryID {
		t.Fatalf("QueryID mismatch: got %d, want %d", got.QueryID, msg.QueryID)
	}
}
```

If `newTestConn` and `recordingSender` do not already exist in the `protocol` test package, grep for existing equivalents; the codebase has helpers in `sender_test.go` and `handle_subscribe_test.go`. Use whichever helper is closest. If none suits, add a minimal `recordingSender` that captures `(connID, msg)` pairs and returns `nil`.

- [ ] **Step 2: Run the new test and confirm it fails**

Run: `rtk go test ./protocol -run TestTD136_SubscribeSingleAppliedReachesWireWithoutTrackerSeed -count=1 -v`

Expected: FAIL. The failure reason is `len(sender.sent) == 0` because `SendSubscribeSingleApplied` returns early when `IsPending` is false on a fresh tracker.

- [ ] **Step 3: Apply the minimal fix — collapse `SendSubscribeSingleApplied` to a straight push**

Modify `protocol/send_responses.go`. Replace the body of `SendSubscribeSingleApplied` with the same shape as `SendSubscribeMultiApplied`:

```go
// SendSubscribeSingleApplied delivers a SubscribeSingleApplied message.
// Phase 2 Slice 2 admission-model slice (TD-140): wire-id admission
// bookkeeping is no longer maintained on the protocol connection —
// subscription.Manager.querySets is the single source of truth, and
// §9.4 ordering is preserved by the synchronous Reply closure invoked
// inside the executor main-loop goroutine plus per-connection
// OutboundCh FIFO. See docs/adr/2026-04-19-subscription-admission-model.md.
func SendSubscribeSingleApplied(sender ClientSender, conn *Conn, msg *SubscribeSingleApplied) error {
	return sender.Send(conn.ID, *msg)
}
```

- [ ] **Step 4: Run the new test plus the file's existing tests; confirm pass**

Run: `rtk go test ./protocol -run TestTD136 -count=1 -v`

Expected: PASS.

Run: `rtk go test ./protocol -run TestSendSubscribeSingle -count=1 -v`

Existing tests that asserted `IsActive` state after delivery will now fail; that is expected — they will be adjusted in Task 5 along with the tracker deletion. For now, if any same-file tests fail due to the gate removal, note them and proceed; do NOT attempt to fix them in Task 1.

- [ ] **Step 5: Commit**

Run:
```bash
rtk git add protocol/td136_regression_test.go protocol/send_responses.go
rtk git commit -m "$(cat <<'EOF'
protocol(parity): collapse SendSubscribeSingleApplied gate (TD-136 / C1)

Removes the tracker IsPending guard that silently dropped Applied
envelopes in production because handleSubscribeSingle no longer
Reserves the QueryID post-variant-split. Matches SendSubscribeMultiApplied
which was already a straight push. Single admission source is
subscription.Manager.querySets (see ADR 2026-04-19).

Regression pin: TestTD136_SubscribeSingleAppliedReachesWireWithoutTrackerSeed.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Fix C2 (delete `validateActiveSubscriptionUpdates`) + regression pin

**Files:**
- Create: `protocol/td137_regression_test.go`
- Modify: `protocol/send_txupdate.go`

**Rationale:** `validateActiveSubscriptionUpdates` compares manager-allocated internal `SubscriptionID` (in `SubscriptionUpdate.SubscriptionID`) against the tracker's wire-`QueryID`-keyed map. Post-split the two namespaces never coincide, so every fan-out delivery would fail admission in production. Delete the gate; rely on `connOnlySender.Send`'s existing `<-c.closed` / `ErrConnNotFound` / `ErrClientBufferFull` returns.

**Steps:**

- [ ] **Step 1: Write the failing regression pin**

Create `protocol/td137_regression_test.go`:

```go
package protocol

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestTD137_DeliverTransactionUpdateLightNoAdmissionGate pins the C2
// regression from TD-140/TD-137: DeliverTransactionUpdateLight must NOT
// reject a SubscriptionUpdate whose SubscriptionID is a manager-allocated
// internal id. Pre-fix, validateActiveSubscriptionUpdates compared this
// against the wire-QueryID tracker map and returned ErrSubscriptionNotActive
// for every fan-out delivery.
func TestTD137_DeliverTransactionUpdateLightNoAdmissionGate(t *testing.T) {
	t.Parallel()
	connID := types.ConnectionID{0x02}
	conn := newTestConn(t, connID)
	mgr := NewConnManager()
	mgr.Add(conn)

	sender := &recordingSender{}

	internalSubID := types.SubscriptionID(9001) // arbitrary non-zero, != any possible wire QueryID
	fanout := map[types.ConnectionID][]SubscriptionUpdate{
		connID: {
			{
				SubscriptionID: internalSubID,
				TableID:        1,
				TableName:      "users",
				Inserts: []types.ProductValue{
					{types.NewU64(1)},
				},
			},
		},
	}

	errs := DeliverTransactionUpdateLight(sender, mgr, 0, fanout)

	if len(errs) != 0 {
		t.Fatalf("DeliverTransactionUpdateLight returned %d errors, want 0: %+v", len(errs), errs)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected one Send call, got %d", len(sender.sent))
	}
}
```

Use the existing `newTestConn` and `recordingSender` helpers from Task 1 (or the same call-sites already present in `protocol/` tests). The import of `types` and the types package's public `SubscriptionID` + `NewU64` helpers are already used extensively in the package (`protocol/send_txupdate_test.go:22-23`, `protocol/fanout_adapter_test.go` etc.) — mirror those.

- [ ] **Step 2: Run the test and confirm it fails**

Run: `rtk go test ./protocol -run TestTD137_DeliverTransactionUpdateLightNoAdmissionGate -count=1 -v`

Expected: FAIL. Failure reason: `errs` is length 1, containing `ErrSubscriptionNotActive` for the fresh-tracker conn.

- [ ] **Step 3: Delete the gate**

Modify `protocol/send_txupdate.go` — remove `validateActiveSubscriptionUpdates` and the per-fanout-entry check in `DeliverTransactionUpdateLight`. Also remove the now-unused `ErrSubscriptionNotActive` sentinel; callers in-tree reference it only via the deleted gate.

New `send_txupdate.go` body:

```go
package protocol

import (
	"github.com/ponchione/shunter/types"
)

// DeliveryError pairs a connection ID with the error encountered during
// delivery. Used by callers to trigger disconnect for buffer-full
// connections.
type DeliveryError struct {
	ConnID types.ConnectionID
	Err    error
}

// DeliverTransactionUpdateLight sends a non-caller TransactionUpdateLight
// to every connection in fanout (Phase 1.5). Connections not found in
// the ConnManager are skipped (disconnected since evaluation). Empty
// update slices are skipped. Buffer-full errors are collected so the
// caller can trigger disconnects.
//
// Phase 2 Slice 2 admission-model slice (TD-140): the former per-update
// IsActive(SubscriptionID) gate is gone — admission is owned by
// subscription.Manager.querySets, and fan-out enumerates only live
// subs. Transport-level guards in connOnlySender.Send (<-c.closed,
// ErrConnNotFound, ErrClientBufferFull) handle disconnect races.
// See docs/adr/2026-04-19-subscription-admission-model.md.
func DeliverTransactionUpdateLight(
	sender ClientSender,
	mgr *ConnManager,
	requestID uint32,
	fanout map[types.ConnectionID][]SubscriptionUpdate,
) []DeliveryError {
	var errs []DeliveryError
	for connID, updates := range fanout {
		if len(updates) == 0 {
			continue
		}
		if mgr.Get(connID) == nil {
			continue
		}
		msg := &TransactionUpdateLight{RequestID: requestID, Update: updates}
		if err := sender.SendTransactionUpdateLight(connID, msg); err != nil {
			errs = append(errs, DeliveryError{ConnID: connID, Err: err})
		}
	}
	return errs
}
```

- [ ] **Step 4: Run regression pin + sibling tests**

Run: `rtk go test ./protocol -run TestTD137 -count=1 -v`

Expected: PASS.

Run: `rtk go test ./protocol -run TestDeliverTransactionUpdateLight -count=1 -v`

Existing sibling tests may reference `ErrSubscriptionNotActive` or expect its error path — catalog failures but do NOT fix them in this task. They will be migrated in Task 5 along with the tracker deletion.

- [ ] **Step 5: Commit**

Run:
```bash
rtk git add protocol/td137_regression_test.go protocol/send_txupdate.go
rtk git commit -m "$(cat <<'EOF'
protocol(parity): drop fan-out wire-id admission gate (TD-137 / C2)

validateActiveSubscriptionUpdates compared manager-allocated internal
SubscriptionID against a tracker map keyed by wire QueryID; post-split
the two namespaces never coincide, so every TransactionUpdateLight
delivery would have failed admission in production. Gate removed;
admission is owned by subscription.Manager.querySets (fan-out enumerates
only live subs) and transport-level guards in connOnlySender.Send
handle disconnect races. See ADR 2026-04-19.

Regression pin: TestTD137_DeliverTransactionUpdateLightNoAdmissionGate.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Reshape `ExecutorInbox` register/unregister to synchronous `Reply` closure

**Files:**
- Modify: `protocol/lifecycle.go`
- Modify: `protocol/handle_subscribe_single.go`
- Modify: `protocol/handle_subscribe_multi.go`
- Modify: `protocol/handle_unsubscribe_single.go`
- Modify: `protocol/handle_unsubscribe_multi.go`
- Modify: `protocol/async_responses.go`
- Modify: `executor/command.go`
- Modify: `executor/executor.go`
- Modify (migrate fakes): `protocol/handle_subscribe_test.go`, `protocol/handle_unsubscribe_test.go`, `protocol/fanout_adapter_test.go`, `executor/contracts_test.go`, `executor/subscription_dispatch_test.go`

**Rationale:** This is the architectural core of the slice. It closes the watcher-goroutine vs fan-out ordering race by making `Reply` synchronous on the executor main-loop goroutine. After this task, `watchSubscribeSetResponse` / `watchUnsubscribeSetResponse` are gone; the Applied / UnsubscribeApplied / Error frame is enqueued on `OutboundCh` inside `handleRegisterSubscriptionSet` / `handleUnregisterSubscriptionSet` before the executor returns to its command dispatch loop. This task is mechanical but wide; compile breaks until every caller is migrated.

**Steps:**

- [ ] **Step 1: Write the §9.4 ordering regression pin**

Create `protocol/admission_ordering_test.go`:

```go
package protocol

import (
	"sync"
	"testing"

	"github.com/ponchione/shunter/types"
)

// TestAdmissionOrdering_AppliedPrecedesFanoutOnOutboundCh pins SPEC-005
// §9.4: SubscribeApplied MUST be delivered before any TransactionUpdate
// that references that subscription_id. Post-ADR, this is guaranteed by
// the synchronous Reply closure — the closure enqueues Applied on
// OutboundCh from the same goroutine that later drives fan-out, so
// OutboundCh FIFO preserves order. Pre-ADR (with watch goroutines), the
// watcher could lose the race against fan-out.
//
// The test simulates the executor's main-loop sequencing by invoking
// Reply synchronously then enqueuing a fake fan-out update, and asserts
// the observed OutboundCh frame order.
func TestAdmissionOrdering_AppliedPrecedesFanoutOnOutboundCh(t *testing.T) {
	t.Parallel()
	connID := types.ConnectionID{0x03}
	conn := newTestConn(t, connID)
	mgr := NewConnManager()
	mgr.Add(conn)

	sender := connOnlySender{conn: conn}

	// Phase A: executor calls Reply synchronously.
	applied := &SubscribeSingleApplied{RequestID: 1, QueryID: 7, TableName: "users"}
	if err := sender.Send(connID, *applied); err != nil {
		t.Fatalf("sender.Send(applied) returned error: %v", err)
	}

	// Phase B: subsequent commit fans out an update on the same goroutine.
	update := []SubscriptionUpdate{{SubscriptionID: 9001, TableName: "users"}}
	errs := DeliverTransactionUpdateLight(sender, mgr, 42, map[types.ConnectionID][]SubscriptionUpdate{
		connID: update,
	})
	if len(errs) != 0 {
		t.Fatalf("DeliverTransactionUpdateLight returned errors: %+v", errs)
	}

	// Drain OutboundCh and confirm order.
	var drained [][]byte
	done := make(chan struct{})
	go func() {
		for frame := range conn.OutboundCh {
			drained = append(drained, frame)
			if len(drained) == 2 {
				close(done)
				return
			}
		}
	}()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-done
	}()
	wg.Wait()

	if len(drained) != 2 {
		t.Fatalf("expected 2 frames on OutboundCh, got %d", len(drained))
	}
	// First frame tag byte: TagSubscribeSingleApplied (2). Second: TagTransactionUpdateLight (8).
	// Compression prefix may sit at drained[i][0]; payload starts at drained[i][1] or drained[i][0]
	// depending on conn.Compression. In newTestConn compression is off, so payload is raw.
	if drained[0][0] != TagSubscribeSingleApplied {
		t.Fatalf("first frame tag = %d, want TagSubscribeSingleApplied (%d)", drained[0][0], TagSubscribeSingleApplied)
	}
	if drained[1][0] != TagTransactionUpdateLight {
		t.Fatalf("second frame tag = %d, want TagTransactionUpdateLight (%d)", drained[1][0], TagTransactionUpdateLight)
	}
}

// TestAdmissionOrdering_DisconnectBetweenRegisterAndReplyDropsApplied pins
// SPEC-005 §9.1 rule 3: if the client disconnects while a subscription is
// pending, the registration result is discarded. Post-ADR, this is served
// by connOnlySender.Send's <-c.closed guard: a Reply invoked after the
// Conn has been closed finds the closed channel and returns
// ErrConnNotFound without enqueuing on OutboundCh.
func TestAdmissionOrdering_DisconnectBetweenRegisterAndReplyDropsApplied(t *testing.T) {
	t.Parallel()
	connID := types.ConnectionID{0x04}
	conn := newTestConn(t, connID)

	// Simulate disconnect: close the conn.closed channel.
	close(conn.closed)

	sender := connOnlySender{conn: conn}
	applied := &SubscribeSingleApplied{RequestID: 2, QueryID: 8}

	err := sender.Send(connID, *applied)
	if err == nil {
		t.Fatalf("expected Send error after close, got nil")
	}
	// The existing connOnlySender.Send contract returns ErrConnNotFound
	// (wrapped) on closed conn. Accept either wrapped or not.
	if len(conn.OutboundCh) != 0 {
		t.Fatalf("Applied should not have been enqueued on closed conn; OutboundCh len=%d", len(conn.OutboundCh))
	}
}
```

Verify `newTestConn` creates a Conn with an open `closed` channel and no compression; reuse or adapt the existing helper used by `sender_test.go` / `send_responses_test.go`.

- [ ] **Step 2: Run the new tests to see current state**

Run: `rtk go test ./protocol -run TestAdmissionOrdering -count=1 -v`

Expected for the first test (`...AppliedPrecedesFanoutOnOutboundCh`): PASS if drained outside of compression. The test as written exercises synchronous usage of `connOnlySender` and therefore already passes — it is a forward-looking pin for the mechanism, not a regression. (The race the ADR describes is between the watcher goroutine and fan-out; after Task 3 deletes the watcher, ordering is structurally guaranteed. The pin ensures the new mechanism preserves the invariant.)

Expected for the second test (`...DisconnectBetweenRegisterAndReplyDropsApplied`): PASS already — `connOnlySender.Send`'s existing guard handles this. Pin exists to prevent regression.

If either fails, stop and investigate — the pins encode assumptions that should hold today.

- [ ] **Step 3: Reshape `protocol/lifecycle.go` to introduce `Reply` closures**

Modify `protocol/lifecycle.go`. Replace the `RegisterSubscriptionSetRequest` and `UnregisterSubscriptionSetRequest` struct definitions:

```go
// RegisterSubscriptionSetRequest carries the fields the executor needs to
// register a set of predicates under one QueryID. Predicates is []any (not
// []subscription.Predicate) because the host-owned executor adapter — the
// concrete ExecutorInbox implementation — may live in a package that should
// not depend on the subscription package. The adapter casts each element to
// subscription.Predicate on the way through. A Single-path submission
// forwards len==1; a Multi-path submission forwards len==N.
//
// Reply is invoked synchronously by the executor on its main-loop goroutine
// after subscription.Manager.RegisterSet returns. Exactly one of the
// response arms (SingleApplied, MultiApplied, Error) is populated. The
// closure encodes and enqueues the wire envelope on the target
// Conn.OutboundCh. See docs/adr/2026-04-19-subscription-admission-model.md.
type RegisterSubscriptionSetRequest struct {
	ConnID     types.ConnectionID
	QueryID    uint32
	RequestID  uint32
	Predicates []any // []subscription.Predicate
	Reply      func(SubscriptionSetCommandResponse)
}

// UnregisterSubscriptionSetRequest drops every internal subscription
// registered under (ConnID, QueryID) atomically. Used by both Single
// and Multi unsubscribe paths. Reply is invoked synchronously by the
// executor on its main-loop goroutine.
type UnregisterSubscriptionSetRequest struct {
	ConnID    types.ConnectionID
	QueryID   uint32
	RequestID uint32
	Reply     func(UnsubscribeSetCommandResponse)
}
```

`SubscriptionSetCommandResponse` and `UnsubscribeSetCommandResponse` stay as-is — they already carry exactly-one-of arms.

- [ ] **Step 4: Migrate the four handlers**

Modify `protocol/handle_subscribe_single.go`:

```go
package protocol

import (
	"context"
)

// handleSubscribeSingle processes an incoming SubscribeSingleMsg. It
// resolves and validates the wire query against the schema, normalizes
// predicates, and submits the subscription to the executor via the
// set-based seam (len(Predicates)==1). The executor invokes the Reply
// closure synchronously inside its command handler, which encodes and
// enqueues a SubscribeSingleApplied or SubscriptionError envelope on
// the connection's OutboundCh. See
// docs/adr/2026-04-19-subscription-admission-model.md.
func handleSubscribeSingle(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeSingleMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	pred, err := compileQuery(msg.Query, sl)
	if err != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     err.Error(),
		})
		return
	}

	sender := connOnlySender{conn: conn}
	reply := singleAppliedReply(sender, conn, msg.RequestID, msg.QueryID)

	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    msg.QueryID,
		RequestID:  msg.RequestID,
		Predicates: []any{pred},
		Reply:      reply,
	}); submitErr != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     "executor unavailable: " + submitErr.Error(),
		})
		return
	}
}
```

Introduce `singleAppliedReply` / `multiAppliedReply` / `singleUnsubReply` / `multiUnsubReply` closure builders in a new helper file or inside `protocol/async_responses.go` (replacing the deleted watchers). Example `singleAppliedReply` builder:

```go
// singleAppliedReply returns a Reply closure that the executor invokes
// synchronously after the manager's RegisterSet returns. The closure
// enqueues SubscribeSingleApplied (happy path) or SubscriptionError
// (error path) on the connection's OutboundCh via connOnlySender.Send.
// Delivery errors are logged — they are non-fatal and do not roll back
// any manager-side state.
func singleAppliedReply(sender connOnlySender, conn *Conn, requestID, queryID uint32) func(SubscriptionSetCommandResponse) {
	return func(resp SubscriptionSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: SubscriptionError delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.Error.QueryID, err)
			}
		case resp.SingleApplied != nil:
			if err := SendSubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				log.Printf("protocol: SubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.SingleApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed SubscriptionSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}
}
```

Mirror for Multi:

```go
func multiAppliedReply(sender connOnlySender, conn *Conn, requestID, queryID uint32) func(SubscriptionSetCommandResponse) {
	return func(resp SubscriptionSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: SubscriptionError delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.Error.QueryID, err)
			}
		case resp.MultiApplied != nil:
			if err := SendSubscribeMultiApplied(sender, conn, resp.MultiApplied); err != nil {
				log.Printf("protocol: SubscribeMultiApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.MultiApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed SubscriptionSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}
}
```

And for Unsubscribe (Single and Multi):

```go
func singleUnsubReply(sender connOnlySender, conn *Conn, requestID, queryID uint32) func(UnsubscribeSetCommandResponse) {
	return func(resp UnsubscribeSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: unsubscribe SubscriptionError delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.Error.QueryID, err)
			}
		case resp.SingleApplied != nil:
			if err := SendUnsubscribeSingleApplied(sender, conn, resp.SingleApplied); err != nil {
				log.Printf("protocol: UnsubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.SingleApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed UnsubscribeSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}
}

func multiUnsubReply(sender connOnlySender, conn *Conn, requestID, queryID uint32) func(UnsubscribeSetCommandResponse) {
	return func(resp UnsubscribeSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: unsubscribe SubscriptionError delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.Error.QueryID, err)
			}
		case resp.MultiApplied != nil:
			if err := SendUnsubscribeMultiApplied(sender, conn, resp.MultiApplied); err != nil {
				log.Printf("protocol: UnsubscribeMultiApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.MultiApplied.QueryID, err)
			}
		default:
			log.Printf("protocol: malformed UnsubscribeSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}
}
```

Place these four builders in `protocol/async_responses.go` in place of the deleted `watchSubscribeSetResponse` / `watchUnsubscribeSetResponse`. The file's remaining content (`connOnlySender` type + methods, `watchReducerResponse`) is unchanged.

Now migrate `protocol/handle_subscribe_multi.go`:

```go
package protocol

import (
	"context"
)

func handleSubscribeMulti(
	ctx context.Context,
	conn *Conn,
	msg *SubscribeMultiMsg,
	executor ExecutorInbox,
	sl SchemaLookup,
) {
	preds := make([]any, 0, len(msg.Queries))
	for _, q := range msg.Queries {
		pred, err := compileQuery(q, sl)
		if err != nil {
			sendError(conn, SubscriptionError{
				RequestID: msg.RequestID,
				QueryID:   msg.QueryID,
				Error:     err.Error(),
			})
			return
		}
		preds = append(preds, pred)
	}

	sender := connOnlySender{conn: conn}
	reply := multiAppliedReply(sender, conn, msg.RequestID, msg.QueryID)

	if submitErr := executor.RegisterSubscriptionSet(ctx, RegisterSubscriptionSetRequest{
		ConnID:     conn.ID,
		QueryID:    msg.QueryID,
		RequestID:  msg.RequestID,
		Predicates: preds,
		Reply:      reply,
	}); submitErr != nil {
		sendError(conn, SubscriptionError{
			RequestID: msg.RequestID,
			QueryID:   msg.QueryID,
			Error:     "executor unavailable: " + submitErr.Error(),
		})
	}
}
```

Confirm the existing `handle_subscribe_multi.go` signature matches; adjust only the parts shown above. Unused imports (e.g. `context` if not needed elsewhere) are pruned.

Mirror the pattern for `handle_unsubscribe_single.go` (uses `singleUnsubReply`) and `handle_unsubscribe_multi.go` (uses `multiUnsubReply`). Read the existing files first to preserve any additional arguments or helpers not shown in this plan — the Reply-closure swap is the only semantic change.

- [ ] **Step 5: Reshape `executor/command.go`**

Modify `executor/command.go`:

```go
// RegisterSubscriptionSetCmd requests atomic set-scoped subscription
// registration. Part of the Phase 2 Slice 2 variant split; the
// admission-model slice (TD-140) introduces the synchronous Reply
// closure, replacing the ResponseCh channel. The executor invokes
// Reply on its main-loop goroutine after subs.RegisterSet returns;
// the protocol-owned closure enqueues the Applied or Error envelope
// on the target Conn.OutboundCh before the command dispatcher moves
// on. See docs/adr/2026-04-19-subscription-admission-model.md.
type RegisterSubscriptionSetCmd struct {
	Request subscription.SubscriptionSetRegisterRequest
	Reply   func(protocol.SubscriptionSetCommandResponse)
}

func (RegisterSubscriptionSetCmd) isExecutorCommand() {}

// UnregisterSubscriptionSetCmd removes every subscription registered
// under one (ConnID, QueryID) key. Reply is invoked synchronously by
// the executor after subs.UnregisterSet returns.
type UnregisterSubscriptionSetCmd struct {
	ConnID  types.ConnectionID
	QueryID uint32
	Reply   func(protocol.UnsubscribeSetCommandResponse)
}

func (UnregisterSubscriptionSetCmd) isExecutorCommand() {}
```

**Import cycle check.** `executor` imports `subscription` today; the `Reply` closure type now requires `executor` to also see `protocol.SubscriptionSetCommandResponse` / `protocol.UnsubscribeSetCommandResponse`. Run `rtk go build ./executor/...` after this edit to confirm no cycle.

If a cycle exists (`protocol` imports `executor` anywhere), resolve by hoisting the response types into a new shared package (e.g. `protocol/subsresp` or `types/subsresp`) — but the current tree has `executor` importing `protocol` only via the `ExecutorInbox` interface definition, which lives in `protocol/lifecycle.go`. The inbox interface is implemented in `executor`'s adapter (out of tree today; Task 10 of the variant-split plan is the host-adapter slice that was skipped). Inside `executor/`, the only `protocol` reference is in tests. If build succeeds, proceed; if not, stop and flag — a late-binding response type via `any` plus a type assertion is acceptable as a fallback, but verify the cycle first.

Also delete the `UnregisterSubscriptionSetResponse` struct in `executor/command.go` — its `{Result, Err}` shape is now subsumed into `protocol.UnsubscribeSetCommandResponse`. Caller adjustments follow in Step 6.

- [ ] **Step 6: Update `executor/executor.go` handlers**

Replace `handleRegisterSubscriptionSet` and `handleUnregisterSubscriptionSet`:

```go
func (e *Executor) handleRegisterSubscriptionSet(cmd RegisterSubscriptionSetCmd) {
	view := e.snapshotFn()
	defer view.Close()
	res, err := e.subs.RegisterSet(cmd.Request, view)
	if err != nil {
		cmd.Reply(protocol.SubscriptionSetCommandResponse{
			Error: &protocol.SubscriptionError{
				RequestID: cmd.Request.RequestID,
				QueryID:   cmd.Request.QueryID,
				Error:     err.Error(),
			},
		})
		return
	}
	// Translate the manager's result into the appropriate Applied arm.
	// Single vs Multi is distinguishable by len(cmd.Request.Predicates):
	// Single forwards len==1; Multi forwards len>=1. The protocol's
	// Reply closure knows which arm to enqueue based on which handler
	// built it — so the executor populates both arms only for the one
	// the closure expects. The existing response type encodes exactly
	// one arm; here we populate MultiApplied unconditionally, and the
	// protocol's singleAppliedReply closure reinterprets it by picking
	// the single-row shape out of res.Update. HOWEVER, to keep the
	// executor oblivious to wire shape, the cleanest mapping is to
	// populate the response envelope from the manager result and let
	// the protocol-side Reply closures pattern-match on the single-vs-multi
	// handler context. Since SubscribeSingleApplied has a different shape
	// (TableName + Rows) than SubscribeMultiApplied (Update slice), the
	// executor cannot build both without knowing which was requested.
	//
	// Resolution: the protocol handler builds the Reply closure already
	// knowing Single vs Multi (Step 4). The closure receives
	// SubscriptionSetCommandResponse with MultiApplied populated in all
	// cases; the singleAppliedReply closure translates MultiApplied ->
	// SubscribeSingleApplied internally by collapsing the len-1 Update
	// slice into {TableName, Rows}. Multi closure passes MultiApplied
	// through unchanged. Executor stays wire-shape-oblivious.
	cmd.Reply(protocol.SubscriptionSetCommandResponse{
		MultiApplied: &protocol.SubscribeMultiApplied{
			RequestID: cmd.Request.RequestID,
			QueryID:   res.QueryID,
			Update:    res.Update,
		},
	})
}

func (e *Executor) handleUnregisterSubscriptionSet(cmd UnregisterSubscriptionSetCmd) {
	view := e.snapshotFn()
	defer view.Close()
	res, err := e.subs.UnregisterSet(cmd.ConnID, cmd.QueryID, view)
	if err != nil {
		cmd.Reply(protocol.UnsubscribeSetCommandResponse{
			Error: &protocol.SubscriptionError{
				QueryID: cmd.QueryID,
				Error:   err.Error(),
			},
		})
		return
	}
	cmd.Reply(protocol.UnsubscribeSetCommandResponse{
		MultiApplied: &protocol.UnsubscribeMultiApplied{
			QueryID: res.QueryID,
			Update:  res.Update,
		},
	})
}
```

**Collapsing Multi → Single in the protocol closure:** update `singleAppliedReply` (from Step 4) to accept `MultiApplied` and flatten. Replace its body:

```go
func singleAppliedReply(sender connOnlySender, conn *Conn, requestID, queryID uint32) func(SubscriptionSetCommandResponse) {
	return func(resp SubscriptionSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: SubscriptionError delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.Error.QueryID, err)
			}
		case resp.MultiApplied != nil:
			// Single path: collapse len-1 Update into the {TableName, Rows}
			// shape of SubscribeSingleApplied. If the executor returns a
			// zero-row result (no initial rows), TableName falls back to
			// empty and Rows to nil — matching pre-slice behavior.
			var tableName string
			var rows []byte
			if len(resp.MultiApplied.Update) == 1 {
				u := resp.MultiApplied.Update[0]
				tableName = u.TableName
				rows = encodeRowsForSingle(u) // existing helper in this package; if missing, inline the encode
			}
			applied := &SubscribeSingleApplied{
				RequestID: requestID,
				QueryID:   queryID,
				TableName: tableName,
				Rows:      rows,
			}
			if err := SendSubscribeSingleApplied(sender, conn, applied); err != nil {
				log.Printf("protocol: SubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], queryID, err)
			}
		default:
			log.Printf("protocol: malformed SubscriptionSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}
}
```

`encodeRowsForSingle` may not exist in the current tree; the existing watcher path uses whatever encoder `watchSubscribeSetResponse` employed for the Single case. Read `protocol/async_responses.go` pre-deletion and `protocol/server_messages.go` for the row-encoding helper. If there is no existing helper, encode the `[]ProductValue` list using the same BSATN row-list writer that `SubscribeMultiApplied` already relies on — find the writer via `grep RowList protocol/`.

Mirror the corresponding transformation for `singleUnsubReply`:

```go
func singleUnsubReply(sender connOnlySender, conn *Conn, requestID, queryID uint32) func(UnsubscribeSetCommandResponse) {
	return func(resp UnsubscribeSetCommandResponse) {
		switch {
		case resp.Error != nil:
			if err := SendSubscriptionError(sender, conn, resp.Error); err != nil {
				log.Printf("protocol: unsubscribe SubscriptionError delivery failed for conn %x query_id=%d: %v", conn.ID[:], resp.Error.QueryID, err)
			}
		case resp.MultiApplied != nil:
			var hasRows bool
			var rows []byte
			if len(resp.MultiApplied.Update) == 1 {
				u := resp.MultiApplied.Update[0]
				if len(u.Deletes) > 0 {
					hasRows = true
					rows = encodeRowsForSingleUnsub(u)
				}
			}
			applied := &UnsubscribeSingleApplied{
				RequestID: requestID,
				QueryID:   queryID,
				HasRows:   hasRows,
				Rows:      rows,
			}
			if err := SendUnsubscribeSingleApplied(sender, conn, applied); err != nil {
				log.Printf("protocol: UnsubscribeSingleApplied delivery failed for conn %x query_id=%d: %v", conn.ID[:], queryID, err)
			}
		default:
			log.Printf("protocol: malformed UnsubscribeSetCommandResponse (req=%d query=%d)", requestID, queryID)
		}
	}
}
```

Note: the existing watcher used a direct `UnsubscribeSingleApplied` arm on the response; under the new symmetric design, only `MultiApplied` is populated by the executor and the closure collapses. If the existing `UnsubscribeSetCommandResponse` struct has a `SingleApplied *UnsubscribeSingleApplied` field that is now unused, delete it along with the parallel `SingleApplied *SubscribeSingleApplied` field on `SubscriptionSetCommandResponse` — the Single shape is synthesized by the closure, not carried on the wire of the internal envelope. Update `protocol/lifecycle.go`:

```go
type SubscriptionSetCommandResponse struct {
	MultiApplied *SubscribeMultiApplied
	Error        *SubscriptionError
}

type UnsubscribeSetCommandResponse struct {
	MultiApplied *UnsubscribeMultiApplied
	Error        *SubscriptionError
}
```

`multiAppliedReply` and `multiUnsubReply` from Step 4 already expect these shapes; no further changes there.

- [ ] **Step 7: Migrate test fakes**

Grep for every `ResponseCh` reference in `protocol/` and `executor/`:

Run: `rtk rg "ResponseCh" protocol executor`

For each hit:
- In test inboxes / dispatch fakes, replace the `chan` field with a captured `func(...)` and invoke it inline.
- In callers constructing `RegisterSubscriptionSetCmd` / `UnregisterSubscriptionSetCmd`, build a `Reply` closure that writes into a test-owned channel so assertions stay channel-based.

Example rewrite for `executor/subscription_dispatch_test.go:96`:

Before:
```go
respCh := make(chan subscription.SubscriptionSetRegisterResult, 1)
exec.dispatch(RegisterSubscriptionSetCmd{Request: req, ResponseCh: respCh})
got := <-respCh
```

After:
```go
respCh := make(chan protocol.SubscriptionSetCommandResponse, 1)
reply := func(r protocol.SubscriptionSetCommandResponse) { respCh <- r }
exec.dispatch(RegisterSubscriptionSetCmd{Request: req, Reply: reply})
got := <-respCh
// assertions now read got.MultiApplied / got.Error rather than a bare Result.
```

Apply equivalent transformations across every listed test file. The `ResponseCh` → `Reply` translation is mechanical.

- [ ] **Step 8: Run the full build + vet + test sweep**

Run: `rtk go build ./...`

Expected: build succeeds. If a cycle appears, return to Step 5 and apply the `any`-typed fallback or hoist to a shared package.

Run: `rtk go vet ./...`

Expected: clean.

Run: `rtk go test ./protocol ./executor ./subscription -count=1`

Expected: PASS. Residual failures in tests that mutated tracker state (`reconnect_test.go`, `conn_test.go`, `send_responses_test.go`, etc.) are expected and will be addressed in Task 5. Catalog them.

- [ ] **Step 9: Commit the reshape**

Stage every file listed at the top of this task. Use explicit paths — do not use `rtk git add -A` or `rtk git add .`.

```bash
rtk git add protocol/lifecycle.go protocol/handle_subscribe_single.go \
  protocol/handle_subscribe_multi.go protocol/handle_unsubscribe_single.go \
  protocol/handle_unsubscribe_multi.go protocol/async_responses.go \
  protocol/admission_ordering_test.go executor/command.go executor/executor.go \
  protocol/handle_subscribe_test.go protocol/handle_unsubscribe_test.go \
  protocol/fanout_adapter_test.go executor/contracts_test.go \
  executor/subscription_dispatch_test.go
```

```bash
rtk git commit -m "$(cat <<'EOF'
protocol+executor(parity): synchronous Reply closure for subscription admission (TD-140)

Replaces the watchSubscribeSetResponse / watchUnsubscribeSetResponse
goroutine pattern on ExecutorInbox.RegisterSubscriptionSet /
UnregisterSubscriptionSet with a protocol-owned Reply closure that the
executor invokes synchronously on its main-loop goroutine after
subs.RegisterSet / UnregisterSet returns. The closure encodes and
enqueues the Applied or Error envelope on Conn.OutboundCh before the
executor's command dispatcher moves on, so per-connection OutboundCh
FIFO guarantees SPEC-005 §9.4 ordering (Applied before any fan-out
update for the new sub). Matches reference send_worker_queue
discipline (module_subscription_manager.rs:841-1101); see ADR
2026-04-19.

Parallels: SubscriptionSetCommandResponse and UnsubscribeSetCommandResponse
collapse to {MultiApplied, Error} — the Single-path closure flattens
len-1 MultiApplied.Update into the {TableName, Rows} shape of
SubscribeSingleApplied at the wire. Matches reference's
add_subscription-wraps-add_subscription_multi pattern.

Regression pins: TestAdmissionOrdering_AppliedPrecedesFanoutOnOutboundCh,
TestAdmissionOrdering_DisconnectBetweenRegisterAndReplyDropsApplied.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Delete `protocol.SubscriptionTracker` + `Conn.Subscriptions`

**Files:**
- Modify: `protocol/conn.go`
- Modify: `protocol/conn_test.go`
- Modify: `protocol/send_responses.go` (remove now-dead `Remove` calls)
- Modify: `protocol/send_responses_test.go`
- Modify: `protocol/send_txupdate_test.go`
- Modify: `protocol/handle_subscribe_test.go`
- Modify: `protocol/handle_unsubscribe_test.go`
- Modify: `protocol/sender_test.go`
- Modify: `protocol/reconnect_test.go`
- Modify: `protocol/fanout_adapter_test.go`

**Rationale:** After Tasks 1-3, no production code path reads or writes the tracker. This task removes the type and field, and purges every test seed that referenced it. Tests that exercised tracker state-machine behavior are deleted outright; tests that merely seeded the tracker to get past gates have those seeds removed.

**Steps:**

- [ ] **Step 1: Verify no production caller remains**

Run: `rtk rg -n "SubscriptionTracker|Subscriptions\.(Reserve|Activate|IsPending|IsActive|IsActiveOrPending|Remove|RemoveAll|state)" protocol executor subscription`

Every hit should be in a `_test.go` file. If any non-test file appears, return to Tasks 1-3 and close the gap before proceeding.

- [ ] **Step 2: Remove `Conn.Subscriptions` field and the tracker type**

Modify `protocol/conn.go`. Delete:
- The `SubscriptionState` type and its constants (`SubPending`, `SubActive`).
- The `SubscriptionTracker` struct and every method (`NewSubscriptionTracker`, `Reserve`, `Activate`, `Remove`, `IsActive`, `IsPending`, `IsActiveOrPending`, `RemoveAll`, `state`).
- The `ErrDuplicateSubscriptionID` and protocol-package-local `ErrSubscriptionNotFound` sentinels. Note: `subscription.ErrSubscriptionNotFound` remains — it is a different sentinel in a different package.
- The `Subscriptions *SubscriptionTracker` field on `Conn`.
- The `Subscriptions: NewSubscriptionTracker(),` initializer in `NewConn`.

Keep: every other field and method on `Conn`, `ConnManager`, and `NewConn`. The `closed` channel, `OutboundCh`, `inflightSem`, and `lastActivity` atomic remain unchanged.

- [ ] **Step 3: Remove tracker calls from `protocol/send_responses.go`**

Confirm only the following three functions remain (already simplified in Task 1 for SendSubscribeSingleApplied; now also simplify the other two):

```go
// SendUnsubscribeSingleApplied delivers an UnsubscribeSingleApplied
// message. Admission bookkeeping is owned by subscription.Manager;
// transport-level delivery is a straight push.
func SendUnsubscribeSingleApplied(sender ClientSender, conn *Conn, msg *UnsubscribeSingleApplied) error {
	return sender.Send(conn.ID, *msg)
}

// SendSubscriptionError delivers a SubscriptionError. Tracker-side
// release is no longer needed — subscription.Manager.RegisterSet's
// error path never inserts into querySets, so there is nothing to
// roll back on this side. See ADR 2026-04-19.
func SendSubscriptionError(sender ClientSender, conn *Conn, msg *SubscriptionError) error {
	return sender.Send(conn.ID, *msg)
}
```

Keep `SendSubscribeMultiApplied`, `SendUnsubscribeMultiApplied`, and `SendOneOffQueryResult` unchanged — they already never touched the tracker.

- [ ] **Step 4: Delete the tracker unit tests**

Modify `protocol/conn_test.go`. Remove tests that exercise `SubscriptionTracker` methods in isolation (Reserve / Activate / IsActive / RemoveAll / state). Keep tests that exercise `Conn` and `ConnManager` behavior orthogonal to the tracker (construction, MarkActivity, CloseAll, Add/Get/Remove).

- [ ] **Step 5: Remove masking seeds from the remaining affected test files**

For each of `send_responses_test.go`, `send_txupdate_test.go`, `handle_subscribe_test.go`, `handle_unsubscribe_test.go`, `sender_test.go`, `fanout_adapter_test.go`:

- Delete every `c.Subscriptions.Reserve(N)` / `Activate(N)` / `Remove(N)` / `IsActive(N)` / `IsActiveOrPending(N)` call.
- Adjust the surrounding assertion: where the test used to check `c.Subscriptions.IsActive(N) == true` after a delivery, replace with `len(sender.sent) == expected` or equivalent outbound-side assertion.
- If a test existed purely to pin "after SendSubscribeSingleApplied, IsActive flips to true" — delete the test; the invariant no longer exists.

`reconnect_test.go` deserves specific attention:

- The test seeds the OLD connection's tracker to simulate active subs, then reconnects and asserts the NEW connection's tracker is empty. Post-ADR the assertion becomes: reconnection creates a fresh `Conn` with a fresh `OutboundCh`; there is no per-conn subscription state left on the protocol side. If the test's intent was to pin "subscription state does not leak across reconnects," keep the skeleton and redirect the assertion to `subscription.Manager.querySets[newConnID]` (should be absent). If that requires wiring a real manager, and the existing test doesn't already, the cleanest migration is to delete the tracker-level assertion entirely and rely on the existing manager-level cleanup tests in `subscription/manager_test.go::TestDisconnectClientClearsQuerySets`.

- [ ] **Step 6: Build + vet + test sweep**

Run: `rtk go build ./...`

Expected: PASS. Any remaining compile errors trace to a `Conn.Subscriptions` reference this task missed — grep again and fix.

Run: `rtk go vet ./...`

Expected: clean.

Run: `rtk go test ./protocol ./executor ./subscription -count=1`

Expected: PASS. If any test still fails, investigate and fix; do NOT proceed until this is green.

- [ ] **Step 7: Commit**

Stage every file listed at the top of this task by explicit path.

```bash
rtk git commit -m "$(cat <<'EOF'
protocol(parity): delete SubscriptionTracker and per-conn admission state (TD-140)

Removes protocol.SubscriptionTracker, its sentinels, and the
Conn.Subscriptions field. subscription.Manager.querySets is now the
single source of truth for admission, with disconnect-discard served
transport-level by connOnlySender.Send's <-c.closed guard and
duplicate-QueryID rejection served by subscription.ErrQueryIDAlreadyLive.
SPEC-005 §9.4 ordering is preserved by the Reply closure + OutboundCh
FIFO introduced in the previous commit. See ADR 2026-04-19.

Purges masking Subscriptions.Reserve / Activate / IsActive seeds from
seven test files and deletes the tracker-state-machine unit tests from
conn_test.go.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: SPEC-005 prose update + TECH-DEBT close-out + NEXT-SESSION-PROMPT refresh

**Files:**
- Modify: `docs/decomposition/005-protocol/SPEC-005-protocol.md`
- Modify: `TECH-DEBT.md`
- Modify: `NEXT-SESSION-PROMPT.md`

**Steps:**

- [ ] **Step 1: Re-frame SPEC-005 §9.1 rule 4**

Modify `docs/decomposition/005-protocol/SPEC-005-protocol.md:504`. Replace:

> - once `SubscribeApplied` is emitted, the subscription transitions from pending → active atomically with tracker activation; a later disconnect cannot resurrect it

with:

> - once the executor's synchronous `Reply` closure has enqueued `SubscribeApplied` on the connection's outbound queue, any subsequent `TransactionUpdate` for that `subscription_id` is guaranteed (by per-connection outbound queue FIFO) to be delivered after it. A disconnect between registration and the flush of `SubscribeApplied` causes the outbound queue to close; the registration result is discarded by the transport layer. See `docs/adr/2026-04-19-subscription-admission-model.md`.

Verify §9.4 (`SubscribeApplied … MUST be delivered before any TransactionUpdate`) remains unchanged — it is the observable contract the reworded rule supports.

- [ ] **Step 2: Close TD-136, TD-137, TD-138, TD-140 in `TECH-DEBT.md`**

For each of TD-136, TD-137, TD-140: append a "Closed:" note with today's date and the branch / PR name.

Example for TD-136:
> Closed: 2026-04-19 by `phase-2-slice-2-td140-admission-model` (Group B). `SendSubscribeSingleApplied` no longer gates on `IsPending`; delivery is a straight push. Regression pin: `TestTD136_SubscribeSingleAppliedReachesWireWithoutTrackerSeed`.

For TD-138: append a "Closed:" note observing that the Reply closure signature resolves the error-symmetry concern:
> Closed: 2026-04-19 by `phase-2-slice-2-td140-admission-model` (Group B). The `Reply func(SubscriptionSetCommandResponse)` signature unifies result and error in one populated envelope; the zero-value-on-error footgun no longer exists. Group C accordingly drops TD-138 from its scope; only TD-139 remains.

For TD-140: append closure referencing the ADR:
> Closed: 2026-04-19 by `phase-2-slice-2-td140-admission-model` (Group B) and `docs/adr/2026-04-19-subscription-admission-model.md` (Group A). Shape 1 adopted: manager-authoritative admission, synchronous Reply closure, tracker retired.

TD-139 stays open — Group C scope.

- [ ] **Step 3: Refresh `NEXT-SESSION-PROMPT.md`**

Update the "Latent blockers" section to show TD-136, TD-137, TD-138, TD-140 closed; TD-139 remains for Group C. Update the suggested next slice to Group C (TD-139 only) followed by Task 10 host-adapter slice.

- [ ] **Step 4: Full build + vet + test sweep**

Run: `rtk go build ./...`
Run: `rtk go vet ./...`
Run: `rtk go test ./... -count=1`

All green. If the pre-existing drift files surface any failure, DO NOT fix — that drift is out of scope for this slice.

- [ ] **Step 5: Commit docs**

```bash
rtk git add docs/decomposition/005-protocol/SPEC-005-protocol.md TECH-DEBT.md NEXT-SESSION-PROMPT.md
rtk git commit -m "$(cat <<'EOF'
docs(parity): close TD-136/137/138/140 — admission model (Group B)

Re-frames SPEC-005 §9.1 rule 4 to reflect the transport-level
ordering guarantee rather than the retired tracker state machine.
Closes TD-136, TD-137, TD-138 (resolved incidentally by the Reply
reshape), and TD-140 (decision record). TD-139 remains for Group C.

Refreshes NEXT-SESSION-PROMPT.md: Group B closed, Group C narrowed
to TD-139, Task 10 host-adapter slice now unblocked.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Open PR

**Files:** none modified.

**Steps:**

- [ ] **Step 1: Final verification sweep**

Run: `rtk go build ./...`
Run: `rtk go vet ./...`
Run: `rtk go test ./... -count=1`
Run: `rtk go test ./protocol -run "TestPhase2" -count=1 -v`

All green. Phase 2 pins unchanged.

Run: `rtk rg "Subscriptions\.(Reserve|Activate|IsPending|IsActive|IsActiveOrPending|Remove|RemoveAll)" .`

Expected output: empty.

Run: `rtk rg "validateActiveSubscriptionUpdates|ErrSubscriptionNotActive|watchSubscribeSetResponse|watchUnsubscribeSetResponse|SubscriptionTracker|ErrDuplicateSubscriptionID" protocol executor`

Expected output: empty. Dead symbols gone.

- [ ] **Step 2: Review branch state**

Run: `rtk git log --oneline main..HEAD`

Expected: five commits — Task 0 ADR, Task 1 C1 fix, Task 2 C2 fix, Task 3 Reply reshape, Task 4 tracker deletion, Task 5 docs.

Run: `rtk git diff --stat main..HEAD`

Check file-count and LOC delta are roughly within the ADR's migration-scope estimate (~200–300 LOC production, ~300–500 LOC tests).

- [ ] **Step 3: Push branch and open PR**

Run: `rtk git push -u origin phase-2-slice-2-td140-admission-model`

Run:
```bash
gh pr create --title "parity: subscription admission model — Shape 1 (TD-140)" --body "$(cat <<'EOF'
## Summary

- Closes TD-136 (C1) and TD-137 (C2) at root by reshaping
  `ExecutorInbox.RegisterSubscriptionSet` / `UnregisterSubscriptionSet`
  to a synchronous `Reply` closure invoked on the executor main-loop
  goroutine, then retiring `protocol.SubscriptionTracker` entirely.
- Closes TD-140 by adopting Shape 1 from the ADR:
  `docs/adr/2026-04-19-subscription-admission-model.md`.
- Closes TD-138 incidentally — the Reply signature subsumes the
  error-symmetry concern.
- Matches reference `ModuleSubscriptionManager` + `send_worker_queue`
  discipline (`reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:841-1101`).

## Test plan

- [x] `rtk go build ./...`
- [x] `rtk go vet ./...`
- [x] `rtk go test ./... -count=1`
- [x] `rtk go test ./protocol -run TestPhase2 -count=1 -v`
- [x] `rtk go test ./protocol -run TestTD136 -count=1 -v`
- [x] `rtk go test ./protocol -run TestTD137 -count=1 -v`
- [x] `rtk go test ./protocol -run TestAdmissionOrdering -count=1 -v`
- [x] Grep sweep: no surviving `Subscriptions.Reserve` / `SubscriptionTracker` / `validateActiveSubscriptionUpdates` / `watchSubscribeSetResponse` references.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Capture the PR URL in the return-to-user message.

---

## Self-review

Spec coverage:

- ADR "Decision" items 1-7 → Tasks 1-4 collectively. ✓
- ADR "Contract changes" table → Task 3 covers Reply signature + watcher deletion; Task 4 covers tracker deletion and `send_responses.go` simplification. ✓
- ADR "SPEC-005 updates implied" → Task 5 Step 1. ✓
- ADR "Test changes" → regression pins in Tasks 1, 2, 3; masking-seed removal in Task 4. ✓
- ADR "Out-of-scope for Group B" (TD-138 close as note, TD-139 stays) → Task 5 Step 2. ✓
- Migration plan steps 1-13 in ADR → mapped 1:1 onto Tasks 1-5. ✓

Placeholder scan: no `TBD` / `TODO` / "fill in" remain in this plan.

Type consistency:

- `Reply func(SubscriptionSetCommandResponse)` used consistently in `RegisterSubscriptionSetRequest` and `RegisterSubscriptionSetCmd`. ✓
- `Reply func(UnsubscribeSetCommandResponse)` used consistently in `UnregisterSubscriptionSetRequest` and `UnregisterSubscriptionSetCmd`. ✓
- `SubscriptionSetCommandResponse{MultiApplied, Error}` and `UnsubscribeSetCommandResponse{MultiApplied, Error}` collapsed shapes used consistently in Tasks 3 + 4. ✓
- `singleAppliedReply` / `multiAppliedReply` / `singleUnsubReply` / `multiUnsubReply` builder names used consistently across handler files. ✓

Edge cases explicitly not covered (by design):

- Pre-existing drift files (listed at session start) are untouched per brief; if any drift-file test fails during a sweep, catalog and punt — not this slice's concern.
- Import-cycle fallback (Task 3 Step 5): the plan describes a fallback but confirms the expected path (no cycle) first. If the cycle appears, the executor pauses and flags rather than unilaterally hoisting types.
