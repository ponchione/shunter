# Story 5.1: ClientSender Interface & Outbound Writer

**Epic:** [Epic 5 — Server Message Delivery](EPIC.md)
**Spec ref:** SPEC-005 §8.5, §8.7, §13
**Depends on:** Epic 1 (message encoding), Epic 3 (connection outbound channel)
**Blocks:** Stories 5.2, 5.3, 5.4

---

## Summary

The interface that the fan-out worker and executor call to deliver messages to clients. Backed by a per-connection outbound writer goroutine.

## Deliverables

- `ClientSender` interface:
  ```go
  type ClientSender interface {
      SendTransactionUpdate(connID ConnectionID, update *TransactionUpdate) error
      SendReducerResult(connID ConnectionID, result *ReducerCallResult) error
      Send(connID ConnectionID, msg any) error  // convenience helper for SubscribeApplied, etc.
  }
  ```

- `func NewClientSender(connManager *ConnManager) ClientSender` — implementation that looks up connections and enqueues serialized frames
- `FanOutSenderAdapter` over `ClientSender` also owns the fan-out `SendSubscriptionError` path used by SPEC-004, routing it through `ClientSender.Send(connID, msg)` with the protocol wire `SubscriptionError`

- Per-connection outbound writer goroutine:
  1. Read from `conn.OutboundCh`
  2. Write binary frame to WebSocket
  3. Exit when `conn.closed` fires and any best-effort flush is complete

- `func (c *Conn) runOutboundWriter(ctx context.Context)` — the writer goroutine

- Send path:
  1. Encode message to `[tag][body]` bytes (Epic 1 codecs)
  2. If compression enabled and message is gzip-eligible: wrap in compression envelope (Story 1.4)
  3. Check `conn.closed`; if already closed → return `ErrConnNotFound`
  4. Non-blocking send to `conn.OutboundCh`
  5. If channel full → return `ErrClientBufferFull` (caller handles disconnect)

## Acceptance Criteria

- [ ] Send message → arrives on WebSocket as binary frame
- [ ] Compression enabled → message wrapped in compression envelope
- [ ] Compression disabled → message sent as `[tag][body]`
- [ ] Channel full → returns `ErrClientBufferFull` (does not block)
- [ ] Closed / missing connection → returns `ErrConnNotFound`
- [ ] Writer goroutine exits when connection closes (no leak)
- [ ] Messages delivered in FIFO order (channel preserves order)

## Design Notes

- The non-blocking send is critical: the fan-out worker or executor must never block on a slow client. `ErrClientBufferFull` signals to the caller that the client should be disconnected (Epic 6).
- Compression decision per message: small messages (< threshold) may skip compression even when negotiated. This is a future optimization; v1 can compress all or none per connection.
- Design decision: `Send` is a protocol-layer convenience helper for direct response messages (`SubscribeApplied`, `UnsubscribeApplied`, `SubscriptionError`, `OneOffQueryResult`). The normative cross-subsystem contract in SPEC-005 §13 is the two typed methods above.
- `Conn.OutboundCh` is never closed by disconnect. Shutdown is coordinated through `conn.closed`, which avoids the send-on-closed-channel race between concurrent sends and disconnect cleanup.
