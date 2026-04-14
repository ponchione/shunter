# Story 6.1: FanOutWorker

**Epic:** [Epic 6 — Fan-Out & Delivery](EPIC.md)
**Spec ref:** SPEC-004 §8.1, §8.2
**Depends on:** Epic 5 (produces FanOutMessage), SPEC-005 (ClientSender / delivery contract)
**Blocks:** Stories 6.2, 6.3, 6.4

---

## Summary

Separate goroutine that receives computed deltas and delivers them through the protocol layer. Decouples delivery from the executor so slow clients don't block transaction processing.

## Deliverables

- `FanOutWorker` struct:
  ```go
  type FanOutWorker struct {
      inbox          chan FanOutMessage
      sender         ClientSender
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

- `NewFanOutWorker(inboxSize int) *FanOutWorker`
  - `inboxSize` default: 64 (§8.4)

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
- `ClientSender`, `ReducerCallResult`, and outbound buffering are protocol-owned contracts from SPEC-005. This story uses them; it does not define protocol internals.
- `confirmedReads` map is accessed only from the fan-out goroutine — no mutex needed.
- `dropped` channel is read by the executor goroutine. Non-blocking reads via `DroppedClients()`.
- `TxDurable` is executor-supplied post-commit metadata backed by the durability subsystem. The fan-out worker consumes readiness; it does not depend directly on the exported SPEC-002 `DurabilityHandle` surface.
