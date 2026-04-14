# Story 6.2: Incoming Backpressure

**Epic:** [Epic 6 — Backpressure & Graceful Disconnect](EPIC.md)
**Spec ref:** SPEC-005 §10.2
**Depends on:** Epic 3 (connection state), Epic 4 (read loop)
**Blocks:** Story 6.3 (disconnect triggers)

---

## Summary

Per-connection limit on in-flight incoming messages. Prevents a client from flooding the server with requests faster than the executor can process them.

## Deliverables

- Incoming queue tracking in read loop:
  - Maintain a counter or semaphore of in-flight messages (messages read but not yet fully processed)
  - Capacity: `IncomingQueueMessages` (default: 64)
  - On reading next message: if counter would exceed limit → close connection with Close `1008`, reason `"too many requests"`

- The overflow-causing message is NOT processed

- Integration with read loop (Story 4.1):
  - Increment counter before dispatching to handler
  - Decrement counter when handler completes (or when executor acknowledges receipt)

## Acceptance Criteria

- [ ] Client sends messages within limit → all processed normally
- [ ] Client sends message that would exceed `IncomingQueueMessages` → connection closed with `1008`
- [ ] Overflow-causing message not processed (not sent to executor)
- [ ] Close reason string is `"too many requests"`
- [ ] Counter correctly tracks in-flight messages (increments on read, decrements on completion)
- [ ] Rapid burst within limit → all accepted

## Design Notes

- "In-flight" means: message has been read from the WebSocket but the corresponding operation has not completed. For `Subscribe` and `CallReducer`, completion is when the executor acknowledges the command. For `OneOffQuery`, completion is when the result is sent.
- A semaphore (buffered channel of struct{}) is a simple implementation: acquire before dispatch, release on completion.
- This protects the executor inbox and server memory from unbounded growth due to a misbehaving client.
