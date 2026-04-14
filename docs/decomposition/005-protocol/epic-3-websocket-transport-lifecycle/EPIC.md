# Epic 3: WebSocket Transport & Connection Lifecycle

**Parent:** [SPEC-005-protocol.md](../SPEC-005-protocol.md) §2, §5, §12
**Blocked by:** Epic 1 (message encoding for InitialConnection), Epic 2 (auth during upgrade)
**Blocks:** Epic 4 (Client Message Dispatch), Epic 5 (Server Message Delivery), Epic 6 (Backpressure)

**Cross-spec:** SPEC-003 (executor: OnConnect/OnDisconnect lifecycle reducers)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 3.1 | [story-3.1-protocol-options-connection-id.md](story-3.1-protocol-options-connection-id.md) | ProtocolOptions config struct, ConnectionID type with zero-rejection |
| 3.2 | [story-3.2-websocket-upgrade-handler.md](story-3.2-websocket-upgrade-handler.md) | HTTP upgrade with auth, protocol negotiation, endpoint parsing |
| 3.3 | [story-3.3-connection-state.md](story-3.3-connection-state.md) | Per-connection state struct (Identity, ConnectionID, subscriptions, outbound channel) |
| 3.4 | [story-3.4-initial-connection-onconnect.md](story-3.4-initial-connection-onconnect.md) | OnConnect hook via executor, InitialConnection message send |
| 3.5 | [story-3.5-keepalive.md](story-3.5-keepalive.md) | Ping/Pong, idle timeout, connection close on timeout |
| 3.6 | [story-3.6-ondisconnect-cleanup.md](story-3.6-ondisconnect-cleanup.md) | OnDisconnect hook, subscription removal, sys_clients cleanup |

## Implementation Order

```
Story 3.1 (ProtocolOptions + ConnectionID)
  └── Story 3.2 (WebSocket upgrade)
        └── Story 3.3 (Connection state)
              ├── Story 3.4 (InitialConnection + OnConnect)
              ├── Story 3.5 (Keep-alive) — parallel with 3.4
              └── Story 3.6 (OnDisconnect) — parallel with 3.4, 3.5
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 3.1 | `protocol/options.go`, `protocol/connection_id.go` |
| 3.2 | `protocol/upgrade.go`, `protocol/upgrade_test.go` |
| 3.3 | `protocol/conn.go` |
| 3.4 | `protocol/conn_init.go`, `protocol/conn_init_test.go` |
| 3.5 | `protocol/keepalive.go`, `protocol/keepalive_test.go` |
| 3.6 | `protocol/conn_close.go`, `protocol/conn_close_test.go` |
