# ADR: Subscription admission model — manager-authoritative, synchronous Applied enqueue

- Status: Proposed (2026-04-19)
- Tech-debt linkage: TD-140 (decision), TD-136 + TD-137 (fixes unblocked by
  this decision), TD-138 + TD-139 (parallel cleanup, Group C, not
  addressed here)
- Context origin: Phase 2 Slice 2 `SubscribeMulti` / `SubscribeSingle`
  variant split post-merge audit. See
  `docs/superpowers/specs/2026-04-18-subscribe-multi-single-split-design.md`.
- Clean-room note: reference design patterns cited from
  `reference/SpacetimeDB/` inform decisions here; no Rust code is ported
  or copied.

## Context

The Phase 2 Slice 2 variant split introduced a set-based subscription
manager API (`RegisterSet` / `UnregisterSet`) keyed by
`(ConnID, QueryID)` and an internal `SubscriptionID` allocation used by
fan-out. The split left two admission-bookkeeping systems coexisting:

1. `protocol.Conn.Subscriptions` — per-connection `SubscriptionTracker`
   keyed by wire `QueryID`, enforcing a `pending → active` state
   machine via `Reserve` / `Activate` / `Remove` calls. Referenced by
   SPEC-005 §9.1 rule 4 by name.
2. `subscription.Manager.querySets` — keyed by `(ConnID, QueryID)` and
   mapping to an allocated `[]SubscriptionID`. The full registry /
   pruning-index / fan-out path consumes this.

The split produced two latent production bugs (TD-136, TD-137) and an
underlying architectural split (TD-140):

- TD-136 (C1): `handleSubscribeSingle` no longer calls
  `conn.Subscriptions.Reserve(queryID)`, but `SendSubscribeSingleApplied`
  still gates delivery on `IsPending(queryID)`. In production the Single
  Applied envelope is silently dropped. Unit tests mask this with an
  explicit `Reserve(N)` seed before invoking the handler.
- TD-137 (C2): `SubscriptionUpdate.SubscriptionID` is allocated by
  `Manager.nextSubID++`, distinct from the wire `QueryID`.
  `send_txupdate.go:57-64` still checks
  `conn.Subscriptions.IsActive(update.SubscriptionID)`, comparing a
  manager-internal id against a map keyed by wire ids. Every fan-out
  delivery would fail admission in production. Tests mask this by
  seeding the tracker with the manager's internal id.

These two bugs are downstream symptoms of the admission split, not
independently fixable without deciding which system is authoritative
and how the other becomes derived state (or is removed).

## Constraints

- **SPEC-005 §9.4 is load-bearing contract:**
  > "`SubscribeApplied(subscription_id)` MUST be delivered before any
  > `TransactionUpdate` that references that `subscription_id`."

  Any admission model must preserve this ordering guarantee on a single
  connection.
- **SPEC-005 §9.1 rule 3:** disconnect while a subscription is pending
  discards the registration result.
- **SPEC-005 §9.1 rule 1:** a second `Subscribe` with the same id while
  pending or active MUST fail with `SubscriptionError`.
- **Reference parity is the target:**
  `ModuleSubscriptionManager::add_subscription_multi` at
  `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:1023-1101`
  preserves §9.4-equivalent ordering via a per-client
  `send_worker_queue` FIFO: registration synchronously enqueues Applied
  on the send worker queue; fan-out enqueues updates on the same queue;
  FIFO does the rest. There is no per-wire-id admission gate inside
  fan-out delivery.

## Decision

**Manager-authoritative admission. Per-connection
`SubscriptionTracker` is retired. §9.4 ordering is preserved by
synchronously enqueuing the Applied envelope on `Conn.OutboundCh`
inside the executor main-loop goroutine, before the executor returns
from the register command.**

Concretely:

1. `subscription.Manager.querySets` is the single source of truth for
   "which subscriptions does this connection have?" for every
   admission, duplicate-id, fan-out, and disconnect code path.
2. `protocol.SubscriptionTracker` is deleted. `Conn.Subscriptions` and
   its state machine (`SubPending` / `SubActive`) are removed.
3. `protocol.ExecutorInbox.RegisterSubscriptionSet` and
   `UnregisterSubscriptionSet` accept a protocol-owned `Reply` closure
   instead of a response channel. The executor invokes `Reply`
   synchronously inside `handleRegisterSubscriptionSet` /
   `handleUnregisterSubscriptionSet` after the manager call returns,
   on the executor main-loop goroutine, before the handler returns to
   the command dispatcher.
4. The closure encodes the Applied / Error envelope and enqueues it on
   the target connection's `OutboundCh` via `connOnlySender.Send`. The
   dispatcher then loops to the next command. A subsequent commit's
   fan-out, running on the same executor goroutine, reaches the
   `OutboundCh` strictly after the Applied enqueue. `OutboundCh` is a
   per-connection single-writer FIFO, so wire order matches enqueue
   order.
5. Fan-out admission gating on wire or internal id is removed.
   `validateActiveSubscriptionUpdates` in `protocol/send_txupdate.go`
   is deleted. `DeliverTransactionUpdateLight` relies on the
   `connOnlySender.Send` closed-channel guard and `ErrConnNotFound`
   / `ErrClientBufferFull` returns — the same path fan-out already
   uses for live-subscription deliveries today.
6. Disconnect-while-pending behavior (§9.1 rule 3) is preserved
   transport-level: `Manager.DisconnectClient` drops the full
   `querySets[conn]` bucket; a post-disconnect Reply invocation hits a
   closed `<-conn.closed` inside `connOnlySender.Send` and returns
   `ErrConnNotFound`. The protocol layer logs and moves on. There is
   no scenario in which a stale Applied reaches a disconnected client,
   and no tracker-state resurrection is possible.
7. Duplicate-wire-id rejection (§9.1 rule 1) is served by
   `subscription.ErrQueryIDAlreadyLive`, returned from `RegisterSet`
   when `querySets[conn][queryID]` is already populated. The
   `ErrDuplicateSubscriptionID` sentinel on the deleted tracker
   disappears along with it.

## Rationale

- **Collapses C1 and C2 at root, not at symptom.** Both bugs derive
  from the presence of a second admission system that disagrees with
  the first about both keys and lifecycle. Removing that second system
  makes the disagreement unrepresentable — the register path cannot
  forget to `Reserve` a key that no longer exists, and the fan-out
  path cannot compare keys that are no longer in two namespaces.
- **Matches reference without copying.** The pattern is the same
  structurally: one per-client FIFO receives the Applied envelope
  synchronously during register, then receives fan-out updates in
  order. Reference uses `send_worker_queue`; Shunter uses
  `OutboundCh`. No code is ported.
- **Removes the watcher-goroutine race.** The existing
  `watchSubscribeSetResponse` goroutine was a correctness hazard even
  before the variant split: it enqueues the Applied from a scheduled
  goroutine while fan-out enqueues from the executor main-loop, giving
  the scheduler a window in which fan-out can enqueue first. The
  existing tracker's `IsPending → IsActive` gate hid this race because
  wire id == internal id pre-split. The variant split exposed the
  race. Synchronous Reply closes it on the executor side directly,
  rather than reintroducing a gate to paper over it.
- **Leaner.** Net production-code change is subtractive: delete
  `SubscriptionTracker` (~90 LOC), delete
  `validateActiveSubscriptionUpdates` (~10 LOC), delete both watchers
  in `protocol/async_responses.go` (~70 LOC). The
  `Reply` closure signature replaces `ResponseCh` (~50 LOC net
  change across protocol + executor).
- **Consistent with Single-collapses-into-Set semantics.** §4.5 of the
  Phase 2 Slice 2 design spec observes that reference implements
  `add_subscription` as a one-line wrapper around
  `add_subscription_multi`. The admission model should be consistent
  on the delivery path too: Single and Multi applied frames already
  diverge only in envelope shape, not in admission, and Shape 1 makes
  the delivery path uniformly track that.

## Rejected alternatives

### Shape 2 — Tracker-authoritative; manager publishes internal IDs back

Re-key the tracker from wire `QueryID` to the manager-allocated
`SubscriptionID[]`. Register response carries the allocated internal
ids; tracker stores `queryID → []subscriptionID`; fan-out gates via a
reverse lookup.

Rejected because:

- Preserves duplicated bookkeeping indefinitely. Every new subscription
  feature must be kept in sync across manager and tracker.
- Does not fix the watcher-goroutine ordering race on its own. Still
  requires a synchronous Reply or per-conn ordering latch; at that
  point the tracker adds no admission value beyond what the
  synchronous enqueue plus manager state already provide.
- Preserves the §9.1 rule 4 literal-tracker language at the cost of
  architectural simplicity. The spec language can be re-framed to
  describe the invariant ("Applied is enqueued before any update")
  rather than the mechanism.

### Shape 3 — Tracker as ordering-latch only; no admission role

Shrink the tracker to one bit per wire `QueryID` ("Applied
enqueued?"); fan-out buffers updates behind the latch.

Rejected because:

- Introduces new asynchronous buffering inside fan-out delivery.
  Every fan-out delivery gains a latch read; buffered-update
  drain logic is new surface for bugs.
- The synchronous-Reply mechanism already provides the ordering
  guarantee without a latch. Keeping the tracker as a latch is a
  vestigial structure that pays no correctness dividend.

### Shape 4 — Retain tracker, restore `Reserve(queryID)` in Single handler, realign fan-out gate

Minimal-diff fix: add `Reserve` back to `handleSubscribeSingle`; change
`validateActiveSubscriptionUpdates` to check wire `QueryID` against a
new `SubscriptionUpdate.QueryID` field injected at fan-out time.

Rejected because:

- Requires the manager to track a reverse `SubscriptionID → QueryID`
  mapping per subscription — same duplication cost as Shape 2.
- Does not address the watcher-goroutine race.
- The Multi path has no `Reserve` equivalent today and its Applied is
  already a straight push. Realigning Single to bypass the tracker is
  the simpler end-state; realigning both to share the tracker only
  works if the tracker becomes set-aware, which is the start of Shape
  2.

## Consequences

### Contract changes

| Surface | Before | After |
|---|---|---|
| `protocol.Conn.Subscriptions` | `*SubscriptionTracker` | removed |
| `protocol.SubscriptionTracker` | state machine, `Reserve` / `Activate` / `Remove` / `IsPending` / `IsActive` / `IsActiveOrPending` / `RemoveAll` | removed |
| `protocol.RegisterSubscriptionSetRequest.ResponseCh` | `chan<- SubscriptionSetCommandResponse` | field deleted |
| `protocol.RegisterSubscriptionSetRequest.Reply` | — | new: `func(SubscriptionSetCommandResponse)` |
| `protocol.UnregisterSubscriptionSetRequest.ResponseCh` | `chan<- UnsubscribeSetCommandResponse` | field deleted |
| `protocol.UnregisterSubscriptionSetRequest.Reply` | — | new: `func(UnsubscribeSetCommandResponse)` |
| `executor.RegisterSubscriptionSetCmd.ResponseCh` | `chan<- subscription.SubscriptionSetRegisterResult` | field deleted |
| `executor.RegisterSubscriptionSetCmd.Reply` | — | new: parallel to the protocol sink |
| `executor.UnregisterSubscriptionSetCmd.ResponseCh` | `chan<- UnregisterSubscriptionSetResponse` | field deleted |
| `executor.UnregisterSubscriptionSetCmd.Reply` | — | new |
| `protocol/send_responses.go::SendSubscribeSingleApplied` | tracker-gated (`IsPending` / `Activate`) | straight push identical to `SendSubscribeMultiApplied` |
| `protocol/send_responses.go::SendUnsubscribeSingleApplied` | tracker `Remove` before send | straight push |
| `protocol/send_responses.go::SendSubscriptionError` | tracker `Remove` before send | straight push |
| `protocol/send_txupdate.go::validateActiveSubscriptionUpdates` | per-update `IsActive(SubscriptionID)` gate | function removed; caller simplified |
| `protocol/async_responses.go::watchSubscribeSetResponse` | spawns goroutine, reads `respCh`, enqueues Applied | function removed |
| `protocol/async_responses.go::watchUnsubscribeSetResponse` | spawns goroutine, reads `respCh`, enqueues UnsubscribeApplied | function removed |

Handler files (`handle_subscribe_single.go`, `handle_subscribe_multi.go`,
`handle_unsubscribe_single.go`, `handle_unsubscribe_multi.go`) migrate
from `respCh := make(chan ...); watchSubscribeSetResponse(conn, respCh, ...)`
to constructing a `Reply` closure bound to a `connOnlySender{conn}` and
passing it through the `ExecutorInbox` call.

### SPEC-005 updates implied

SPEC-005 §9.1 rule 4 uses the phrase "pending → active atomically with
tracker activation." The updated prose will re-frame this as:

> Once `SubscribeApplied` is enqueued on the connection's outbound
> queue during registration, any subsequent `TransactionUpdate` for
> that `subscription_id` is guaranteed (by per-connection outbound
> queue FIFO) to be delivered after it. A disconnect between
> registration and the flush of `SubscribeApplied` causes the
> outbound queue to close; the registration result is discarded by
> the transport layer.

This is a pure spec-prose change: the observable behavior (§9.4
ordering, disconnect-discard, duplicate-id rejection) is preserved.
SPEC-005 update lands in the Group B implementation PR, not in this
ADR.

### Test changes

Production-path tests that previously relied on manual tracker seeds
lose those seeds and, in most cases, exercise the real end-to-end
path through the `Reply` closure. The masking `conn.Subscriptions.Reserve(N)`
calls in `protocol/send_responses_test.go`,
`protocol/send_txupdate_test.go`, `protocol/handle_subscribe_test.go`,
`protocol/handle_unsubscribe_test.go`, `protocol/sender_test.go`,
`protocol/reconnect_test.go`, and `protocol/fanout_adapter_test.go`
are removed. New regression pins:

- `TestSubscribeSingleAppliedReachesWireWithoutTestSeed` — handler is
  invoked against a fresh `Conn` with no seeding, Applied is observed
  on the outbound frame stream. Pins TD-136 closed.
- `TestTransactionUpdateLightDeliversForJustRegisteredSub` —
  end-to-end register → commit → fan-out. Delivery does not drop.
  Pins TD-137 closed.
- `TestSubscribeAppliedPrecedesTransactionUpdate` — observed outbound
  frame sequence on a connection shows Applied strictly before first
  update. Pins §9.4.
- `TestDisconnectBetweenRegisterAndReplyDoesNotSend` — closed-conn
  race: Reply invocation after `Conn.closed` fires logs and drops,
  never reaches `OutboundCh`. Pins §9.1 rule 3 via transport.

### Out-of-scope for Group B (tracked separately)

- TD-138: `RegisterSubscriptionSetResponse{Result, Err}` symmetry.
  The `Reply` closure signature introduced here already unifies
  result+error into one value, so the shape that TD-138 calls for is
  reached incidentally. The executor handler now passes a populated
  response (result or error) to `Reply`; no zero-value-on-error
  footgun survives. As a consequence, Group C's TD-138 work is a
  TECH-DEBT.md closure note ("resolved by Group B PR"); no additional
  code change is required.
- TD-139: `Predicates []any` compile-time safety. Unaffected by this
  ADR; retained as-is. Group C remains the place where TD-139's code
  change lands.
- Task 10 host-adapter slice (Phase 2 Slice 2 plan): unblocked by
  this ADR but not executed here. The adapter will supply the
  concrete `ExecutorInbox` implementation whose `Reply` closures use
  the real `connOnlySender`; the in-tree test fakes already satisfy
  the new interface shape.

## Migration plan

One slice, one PR. Strict TDD per `superpowers:test-driven-development`:

1. Write `TestSubscribeSingleAppliedReachesWireWithoutTestSeed` (C1
   regression pin) — fails against current code once the masking
   `Reserve(7)` seed is removed from the test setup.
2. Write `TestTransactionUpdateLightDeliversForJustRegisteredSub` (C2
   regression pin) — fails against current code because
   `validateActiveSubscriptionUpdates` compares wire id against
   internal id.
3. Reshape `ExecutorInbox.RegisterSubscriptionSet` /
   `UnregisterSubscriptionSet` to accept `Reply` closure.
4. Migrate handlers (`handle_subscribe_single.go`,
   `handle_subscribe_multi.go`, `handle_unsubscribe_single.go`,
   `handle_unsubscribe_multi.go`) to build and pass closures.
5. Update `executor/command.go` cmd types and
   `executor/executor.go::handleRegisterSubscriptionSet` /
   `handleUnregisterSubscriptionSet` to invoke `cmd.Reply`
   synchronously.
6. Delete `protocol/async_responses.go::watchSubscribeSetResponse` and
   `watchUnsubscribeSetResponse`.
7. Simplify `protocol/send_responses.go` — all tracker calls removed.
8. Delete `protocol/send_txupdate.go::validateActiveSubscriptionUpdates`.
9. Delete `protocol/conn.go::SubscriptionTracker` type + methods;
   remove `Conn.Subscriptions` field.
10. Remove now-dead masking `Reserve(N)` / `Activate(N)` / `Remove(N)`
    seeds from tests; migrate tests that legitimately exercised
    tracker state to instead exercise `Manager.querySets` or the
    transport-level closed-conn path.
11. Add `TestSubscribeAppliedPrecedesTransactionUpdate` and
    `TestDisconnectBetweenRegisterAndReplyDoesNotSend`.
12. Update SPEC-005 §9.1 rule 4 prose (see above) and
    `docs/superpowers/specs/2026-04-18-subscribe-multi-single-split-design.md`
    §4 to note that the set-based manager is the single admission
    authority.
13. Update `TECH-DEBT.md`: close TD-136 and TD-137, reference the
    Group B PR and this ADR; note TD-140 closed by this ADR.

Full migration is feasible as one PR because the change is a single
coherent refactor: every tracker call is deleted or replaced, and the
`Reply` signature propagates as a mechanical rename. No partial /
bridge state exists where both systems coexist mid-slice.

## References

- TECH-DEBT entries TD-136, TD-137, TD-138, TD-139, TD-140.
- SPEC-005 §9.1 and §9.4 (`docs/decomposition/005-protocol/SPEC-005-protocol.md:480-529`).
- Phase 2 Slice 2 design spec (`docs/superpowers/specs/2026-04-18-subscribe-multi-single-split-design.md`).
- Reference admission:
  `reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs:841-1101`.
