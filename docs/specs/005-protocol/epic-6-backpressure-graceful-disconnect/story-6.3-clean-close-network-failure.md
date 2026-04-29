# Story 6.3: Clean Close & Network Failure Detection

**Epic:** [Epic 6 — Backpressure & Graceful Disconnect](EPIC.md)
**Spec ref:** SPEC-005 §11.1, §11.2
**Depends on:** Stories 6.1, 6.2, Epic 3 (keep-alive, disconnect sequence)
**Blocks:** Story 6.4 (reconnection)

---

## Summary

Handle all disconnect scenarios: clean close from either side, server-initiated close (shutdown, policy violation, internal error), and network failure detection via ping timeout.

## Deliverables

- Client-initiated close:
  1. Client sends Close frame
  2. Server echoes Close frame
  3. Run disconnect sequence (Story 3.6)

- Server-initiated close codes:
  - `1000` (Normal Closure): graceful engine shutdown
  - `1008` (Policy Violation): auth failure, buffer overflow, too many requests, OnConnect rejection
  - `1011` (Internal Error): unexpected server error
  - `1002` (Protocol Error): unknown message tag, unknown compression tag, malformed message

- Close handshake timeout:
  - After sending Close frame, wait `CloseHandshakeTimeout` (default: 250ms) for echo
  - If no echo received → forcefully tear down the Shunter-side connection state and close the WebSocket as best the transport allows

- Network failure detection:
  - TCP drops without Close → detected by keep-alive (Story 3.5)
  - No data received for `IdleTimeout` → close connection
  - Run disconnect sequence (subscriptions removed, OnDisconnect fires)

- Graceful server shutdown:
  - Send Close `1000` to all connected clients
  - Wait for close handshake or timeout
  - Run disconnect sequence for each remaining connection

## Acceptance Criteria

- [ ] Client Close → server echoes Close, disconnect sequence runs
- [ ] Server shutdown → `1000` Close sent to all clients
- [ ] Buffer overflow → `1008` Close with reason "send buffer full"
- [ ] Too many requests → `1008` Close with reason "too many requests"
- [ ] Unknown tag → `1002` Close
- [ ] Unknown compression tag or invalid gzip payload → `1002` Close
- [ ] Close handshake timeout → connection teardown proceeds without waiting indefinitely for peer echo
- [ ] TCP drop without Close → detected within IdleTimeout
- [ ] OnDisconnect fires for every disconnect path (clean, timeout, failure)
- [ ] All subscriptions cleaned up for every disconnect path
- [ ] `sys_clients` row is removed for clean close and network-failure cleanup paths
- [ ] No goroutine leaks after any disconnect path

## Design Notes

- Close codes are integers defined in RFC 6455. The spec maps specific server behaviors to specific codes. Do not invent new codes.
- The close handshake timeout prevents a misbehaving client from holding the connection open by never echoing the Close frame, even if the underlying transport library cannot guarantee an exact TCP hard-close deadline.
- Graceful shutdown should drain in-flight messages before closing, but with a bounded timeout. The exact shutdown timeout is an operational concern, not specified.

## Implementation

Uses `github.com/coder/websocket.Conn.CloseWithContext` via the Shunter fork
of coder/websocket. The helper `closeWithHandshake` in `protocol/close.go`
wraps it with `CloseHandshakeTimeout`. Transport death is guaranteed within
`CloseHandshakeTimeout` + a small unwind budget.

The historical fork spec previously linked from this story was a root-level
planning artifact and has been retired. The authoritative contract for this
story is now the live implementation in `protocol/close.go` plus the contract
and close-behavior tests under `protocol/close_test.go` and
`protocol/close_codes_contract_test.go`.
