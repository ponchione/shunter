# Story 4.1: Frame Reader & Tag Dispatch

**Epic:** [Epic 4 — Client Message Dispatch](EPIC.md)
**Spec ref:** SPEC-005 §3.2, §6
**Depends on:** Epic 1 (message decoding), Epic 3 (connection state)
**Blocks:** Stories 4.2, 4.3, 4.4

---

## Summary

Read loop that receives binary WebSocket frames, decodes the tag byte, and dispatches to the appropriate handler. Rejects text frames and unknown tags.

## Deliverables

- `func (c *Conn) readLoop(ctx context.Context, handlers *MessageHandlers)` — per-connection read goroutine:
  1. Read next WebSocket frame
  2. Reject text frames → send Close frame with protocol error
  3. Check frame size against `MaxMessageSize` → close if exceeded
  4. Decode using `DecodeClientMessage` (Story 1.2)
  5. Enqueue to incoming processing queue (bounded by `IncomingQueueMessages`)
  6. Dispatch by tag to registered handler

- `MessageHandlers` struct:
  ```go
  type MessageHandlers struct {
      OnSubscribe    func(conn *Conn, msg *SubscribeMsg)
      OnUnsubscribe  func(conn *Conn, msg *UnsubscribeMsg)
      OnCallReducer  func(conn *Conn, msg *CallReducerMsg)
      OnOneOffQuery  func(conn *Conn, msg *OneOffQueryMsg)
  }
  ```

- `ExecutorInbox` interface consumed by dispatch handlers:
  ```go
  type ExecutorInbox interface {
      Submit(ctx context.Context, cmd ExecutorCommand) error
  }
  ```

- Error handling in read loop:
  - `ErrUnknownMessageTag` → Close `1002` (protocol error), log tag value
  - `ErrMalformedMessage` → Close `1002`, log details
  - WebSocket read error → trigger disconnect (Story 3.6)

## Acceptance Criteria

- [ ] Binary frame with valid tag → dispatched to correct handler
- [ ] Text frame → Close frame sent, connection closed
- [ ] Unknown tag → Close `1002`, connection closed
- [ ] Malformed body → Close `1002`, connection closed
- [ ] Frame exceeding `MaxMessageSize` → connection closed
- [ ] Read error (broken pipe) → disconnect sequence triggered
- [ ] Read loop exits cleanly when connection is closed by other goroutine

## Design Notes

- The read loop is single-goroutine per connection. Message handlers may be synchronous (for quick operations) or may enqueue to the executor (for Subscribe, CallReducer).
- `MaxMessageSize` is checked at the WebSocket library level (most libraries support `SetReadLimit`).
- The incoming queue bound (Epic 6, Story 6.2) is logically part of this read loop, but the backpressure enforcement is implemented in Epic 6. Here, the read loop just counts in-flight messages.
