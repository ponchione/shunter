# Story 3.3: Connection State

**Epic:** [Epic 3 — WebSocket Transport & Connection Lifecycle](EPIC.md)
**Spec ref:** SPEC-005 §5.1, §9.1, §9.3
**Depends on:** Story 3.1, Story 3.2
**Blocks:** Stories 3.4, 3.5, 3.6, 5.2, 5.3, Epic 4, Epic 5

---

## Summary

Per-connection state struct that tracks identity, subscriptions, compression mode, and outbound channel for one WebSocket client.

## Deliverables

- `Conn` struct:
  ```go
  type Conn struct {
      ID             ConnectionID
      Identity       Identity
      Token          string        // JWT for this connection (minted or validated)
      Compression    bool          // true if gzip negotiated
      Subscriptions  *SubscriptionTracker
      OutboundCh     chan []byte   // buffered; capacity = OutgoingBufferMessages; never closed directly
      ws             *websocket.Conn // underlying WebSocket
      opts           *ProtocolOptions
      closeOnce      sync.Once
      closed         chan struct{}
  }
  ```

- `SubscriptionTracker` — tracks per-connection subscription state machine (§9.1):
  ```go
  type SubscriptionState uint8
  const (
      SubPending SubscriptionState = iota
      SubActive
  )

  type SubscriptionTracker struct {
      mu     sync.Mutex
      subs   map[uint32]SubscriptionState // subscription_id → state
  }
  ```

- `func (t *SubscriptionTracker) Reserve(id uint32) error` — mark as pending; error if already exists
- `func (t *SubscriptionTracker) Activate(id uint32)` — pending → active
- `func (t *SubscriptionTracker) Remove(id uint32) error` — remove; error if not found
- `func (t *SubscriptionTracker) IsActiveOrPending(id uint32) bool`
- `func (t *SubscriptionTracker) RemoveAll() []uint32` — remove all, return IDs

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

- [ ] Reserve subscription_id → tracked as pending
- [ ] Reserve duplicate subscription_id → `ErrDuplicateSubscriptionID`
- [ ] Activate pending subscription → state becomes active
- [ ] Remove active subscription → gone, `IsActiveOrPending` returns false
- [ ] Remove unknown subscription → `ErrSubscriptionNotFound`
- [ ] RemoveAll returns all tracked IDs and clears state
- [ ] ConnManager Add/Get/Remove lifecycle works correctly
- [ ] OutboundCh capacity matches `ProtocolOptions.OutgoingBufferMessages`

## Design Notes

- `SubscriptionTracker` is goroutine-safe (mutex-protected) because the read loop (incoming messages) and delivery goroutine (outgoing messages) both touch subscription state.
- The `subscription_id` is client-chosen (uint32). The tracker only enforces uniqueness within this connection.
- `OutboundCh` is the bounded channel that the backpressure system (Epic 6) monitors. The outbound writer goroutine reads from this channel and writes to the WebSocket.
- `closed` channel is used for coordinating shutdown across the read loop, write loop, keepalive goroutine, and outbound senders. Disconnect signals `closed`; it does not close `OutboundCh`.
