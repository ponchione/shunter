# Story 6.1: Outgoing Backpressure

**Epic:** [Epic 6 — Backpressure & Graceful Disconnect](EPIC.md)
**Spec ref:** SPEC-005 §10.1
**Depends on:** Epic 3 (connection outbound channel), Epic 5 (outgoing message pipeline)
**Blocks:** Story 6.3 (disconnect triggers)

---

## Summary

Per-client outgoing buffer with hard limit. When a slow client falls behind, disconnect rather than drop messages (dropped deltas would corrupt client cache).

## Deliverables

- Outgoing buffer enforcement in `ClientSender.Send`:
  1. Non-blocking send to `conn.OutboundCh` (buffered channel, capacity = `OutgoingBufferMessages`)
  2. If channel full: return `ErrClientBufferFull`
  3. Caller (fan-out worker or response sender) calls `conn.Disconnect` on `ErrClientBufferFull`

- Disconnect behavior on buffer full:
  1. Leave already-queued messages untouched (the writer goroutine will attempt to flush)
  2. Enqueue Close frame if possible (code `1008`, reason: `"send buffer full"`)
  3. Stop accepting further outbound messages for that connection
  4. Close the connection

- `ErrClientBufferFull` error type

## Acceptance Criteria

- [ ] Outbound channel has capacity = `OutgoingBufferMessages`
- [ ] Messages enqueue without blocking when buffer has space
- [ ] Buffer full → `ErrClientBufferFull` returned immediately (no blocking)
- [ ] Buffer full → connection closed with Close `1008`
- [ ] Already-queued messages not dropped on overflow
- [ ] Overflow-causing message not delivered
- [ ] After disconnect, further sends to same connection return error
- [ ] Writer goroutine attempts to flush remaining queued messages before closing WebSocket

## Design Notes

- **Why disconnect, not drop:** Dropped `TransactionUpdate` deltas would leave the client's local cache in an inconsistent state (missing rows it should have, or keeping rows it shouldn't). Reconnection is the only safe recovery.
- The buffer is a Go buffered channel. `select` with `default` case implements non-blocking send.
- The writer goroutine drains the channel before closing the WebSocket, giving already-queued messages a chance to be delivered. But no guarantee — the TCP connection may already be in trouble if the client is truly slow.
