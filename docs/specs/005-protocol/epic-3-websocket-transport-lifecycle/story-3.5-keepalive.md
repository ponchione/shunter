# Story 3.5: Keep-Alive

**Epic:** [Epic 3 — WebSocket Transport & Connection Lifecycle](EPIC.md)
**Spec ref:** SPEC-005 §5.4
**Depends on:** Story 3.3
**Blocks:** Epic 6 (network failure detection relies on idle timeout)

---

## Summary

Server sends WebSocket Ping frames at regular intervals. If the client does not respond within the idle timeout, the connection is closed.

## Deliverables

- Keep-alive goroutine per connection:
  1. Every `PingInterval` (default: 15s), send a WebSocket Ping frame
  2. Track last received data timestamp (any frame: Pong, data, etc.)
  3. If no data received within `IdleTimeout` (default: 30s), close connection

- `func (c *Conn) runKeepalive(ctx context.Context)` — the keep-alive goroutine

- Integration: the read loop updates the last-received timestamp on every frame. The keepalive goroutine checks this timestamp.

## Acceptance Criteria

- [ ] Ping sent every `PingInterval`
- [ ] Client Pong resets idle timer
- [ ] Any received data (not just Pong) resets idle timer
- [ ] No data for `IdleTimeout` → Close frame sent, connection closed
- [ ] Keepalive stops when connection is closed for other reasons (no goroutine leak)
- [ ] Custom `PingInterval` and `IdleTimeout` from ProtocolOptions respected

## Design Notes

- WebSocket Ping/Pong is at the WebSocket frame level, not application level. Most WebSocket libraries handle Pong responses automatically on the client side.
- The idle timer is based on any received data, not just Pong. This means active data traffic also keeps the connection alive.
- `PingInterval` should be less than `IdleTimeout` to give the client time to respond. Default: 15s ping, 30s idle = one missed pong allowed.
