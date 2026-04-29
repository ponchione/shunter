# Story 3.4: InitialConnection & OnConnect Hook

**Epic:** [Epic 3 — WebSocket Transport & Connection Lifecycle](EPIC.md)
**Spec ref:** SPEC-005 §5.1, §5.2, §8.1
**Depends on:** Story 3.3, Epic 1 (InitialConnection codec)
**Blocks:** Epic 4 (client can send messages after InitialConnection)

---

## Summary

After WebSocket upgrade succeeds, run the `OnConnect` lifecycle reducer. If it succeeds, send `InitialConnection` to the client. If it fails, close the connection before any messages are exchanged.

## Deliverables

- Connection initialization sequence (runs in a goroutine per connection):
  1. Send `OnConnect` command to executor inbox (SPEC-003 `ExecutorCommand`)
  2. Wait for executor response
  3. On success: encode and send `InitialConnection{Identity, ConnectionID, Token}`
  4. On error: send Close frame (code `1008`: Policy Violation), do NOT send `InitialConnection`
  5. On success: start read loop (Epic 4), write loop, and keepalive goroutine (Story 3.5)

- `func (c *Conn) RunLifecycle(ctx context.Context, executor ExecutorInbox)` — orchestrates the above sequence

## Acceptance Criteria

- [ ] OnConnect success → `InitialConnection` is first message received by client
- [ ] `InitialConnection.Identity` matches derived Identity
- [ ] `InitialConnection.ConnectionID` matches negotiated ConnectionID
- [ ] `InitialConnection.Token` is the JWT (validated or minted)
- [ ] OnConnect error → Close frame with code `1008` sent
- [ ] OnConnect error → no `InitialConnection` sent
- [ ] OnConnect error → connection fully cleaned up (not in ConnManager)
- [ ] No client messages processed before `InitialConnection` is sent

## Design Notes

- `ExecutorInbox` is an interface or channel type matching SPEC-003's executor command pattern. The protocol layer sends commands; it does not import executor internals.
- The `Token` in `InitialConnection` is important for anonymous mode: the client needs it to reconnect with the same Identity.
- If the executor is slow to respond to `OnConnect`, the client waits. No timeout on `OnConnect` itself (the executor has its own scheduling). The idle timeout does not apply during this phase because keep-alive has not started yet.
