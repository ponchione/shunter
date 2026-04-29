# Story 3.3: Connection State

**Epic:** [Epic 3 — WebSocket Transport & Connection Lifecycle](EPIC.md)
**Spec ref:** SPEC-005 §5.1, §9.1, §9.3
**Depends on:** Story 3.1, Story 3.2
**Blocks:** Stories 3.4, 3.5, 3.6, 5.2, 5.3, Epic 4, Epic 5

---

## Summary

Per-connection state struct that tracks identity, compression mode, outbound channel, and lifecycle state for one WebSocket client. Client query liveness is manager-authoritative, not tracked by a protocol-local subscription tracker.

## Deliverables

- `Conn` struct:
  ```go
  type Conn struct {
      ID             ConnectionID
      Identity       Identity
      Token          string        // JWT for this connection (minted or validated)
      Compression    bool          // true if gzip negotiated
      OutboundCh     chan []byte   // buffered; capacity = OutgoingBufferMessages; never closed directly
      ws             *websocket.Conn // underlying WebSocket
      opts           *ProtocolOptions
      closeOnce      sync.Once
      closed         chan struct{}
  }
  ```

- No protocol-owned `SubscriptionTracker`. The subscription manager's `(ConnID, QueryID)` registry is the single source of truth for pending/active query IDs. Duplicate client `query_id` admission fails through `subscription.ErrQueryIDAlreadyLive`, and disconnect cleanup is performed through executor/subscription-manager commands.

- `ConnManager` — tracks all active connections:
  ```go
  type ConnManager struct {
      mu    sync.RWMutex
      conns map[ConnectionID]*Conn
  }
  ```

- `func (m *ConnManager) Add(conn *Conn)`
- `func (m *ConnManager) Remove(id ConnectionID)`
- `func (m *ConnManager) Get(id ConnectionID) *Conn`

## Acceptance Criteria

- [ ] Conn carries no protocol-local subscription tracker
- [ ] Duplicate active/pending client `query_id` is rejected by the manager as `ErrQueryIDAlreadyLive`
- [ ] Disconnect cleanup routes through executor/subscription-manager teardown, not tracker mutation
- [ ] ConnManager Add/Get/Remove lifecycle works correctly
- [ ] OutboundCh capacity matches `ProtocolOptions.OutgoingBufferMessages`

## Design Notes

- Protocol no longer owns a goroutine-shared subscription tracker; admission and liveness are owned by the subscription manager keyed by `(ConnID, QueryID)`.
- The client-visible correlator is `query_id` (uint32). Manager-internal `SubscriptionID` values stay below the protocol boundary.
- `OutboundCh` is the bounded channel that the backpressure system (Epic 6) monitors. The outbound writer goroutine reads from this channel and writes to the WebSocket.
- `closed` channel is used for coordinating shutdown across the read loop, write loop, keepalive goroutine, and outbound senders. Disconnect signals `closed`; it does not close `OutboundCh`.
