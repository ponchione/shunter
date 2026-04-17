# Story 6.1: FanOutWorker

**Epic:** [Epic 6 — Fan-Out & Delivery](EPIC.md)
**Spec ref:** SPEC-004 §8.1, §8.2
**Depends on:** Epic 5 (produces FanOutMessage), SPEC-005 (protocol delivery contract adapted into `FanOutSender`)
**Blocks:** Stories 6.2, 6.3, 6.4

---

## Summary

Separate goroutine that receives computed deltas and delivers them through the protocol layer. Decouples delivery from the executor so slow clients don't block transaction processing.

## Deliverables

- `FanOutWorker` struct:
  ```go
  type FanOutWorker struct {
      inbox          chan FanOutMessage
      sender         FanOutSender
      confirmedReads map[ConnectionID]bool
      dropped        chan ConnectionID
  }
  ```

- `FanOutMessage` struct:
  ```go
  type FanOutMessage struct {
      TxDurable    <-chan TxID
      Fanout       CommitFanout
      CallerConnID *ConnectionID
      CallerResult *ReducerCallResult
  }
  ```

- `NewFanOutWorker(inbox <-chan FanOutMessage, sender FanOutSender, dropped chan<- ConnectionID) *FanOutWorker`
  - caller owns inbox allocation; recommended default capacity remains 64 (§8.4)
  - `dropped` is typically the manager-owned shared dropped-client channel

- `Run(ctx context.Context)` — main loop:
  ```
  for msg := range inbox:
    optionally wait for TxDurable (Story 6.4)
    for each connID in msg.Fanout:
      build TransactionUpdate or caller reducer-result delivery (Story 6.2)
      send via protocol layer (with backpressure, Story 6.3)
  ```

- `SetConfirmedReads(connID ConnectionID, enabled bool)`
- `RemoveClient(connID ConnectionID)`
- `DroppedClients() <-chan ConnectionID`

## Acceptance Criteria

- [ ] FanOutMessage received → delivered to correct clients
- [ ] Worker runs on separate goroutine from executor
- [ ] Inbox channel bounded (default 64)
- [ ] Context cancellation → worker exits cleanly
- [ ] Confirmed-read policy can be set/cleared per connection
- [ ] Caller connection can be routed to reducer-result delivery path instead of standalone TransactionUpdate
- [ ] DroppedClients channel available for executor drain

## Design Notes

- The inbox channel is the handoff point. The executor blocks only on channel send, not on delivery. If inbox is full, executor blocks — this is backpressure from fan-out to executor, separate from client backpressure.
- `FanOutSender`, `ReducerCallResult`, and outbound buffering are protocol-owned contracts from SPEC-005. In the live implementation, `protocol.FanOutSenderAdapter` wraps the protocol `ClientSender` and exposes the three fan-out-facing methods SPEC-004 needs: `SendTransactionUpdate`, `SendReducerResult`, and `SendSubscriptionError`.
- `confirmedReads` reads happen inside the fan-out loop, but exported mutators (`SetConfirmedReads`, `RemoveClient`) are called from runtime/tests outside that loop. Guard the map with a mutex or an equivalent ownership handoff; do not assume single-goroutine access for those mutators.
- `dropped` channel is shared with the manager's evaluation-error path so the executor drains one channel after each post-commit step.
- `TxDurable` is executor-supplied post-commit metadata backed by the durability subsystem. The fan-out worker consumes readiness; it does not depend directly on the exported SPEC-002 `DurabilityHandle` surface.
