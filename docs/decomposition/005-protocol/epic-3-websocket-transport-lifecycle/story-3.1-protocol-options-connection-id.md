# Story 3.1: ProtocolOptions & ConnectionID

**Epic:** [Epic 3 — WebSocket Transport & Connection Lifecycle](EPIC.md)
**Spec ref:** SPEC-005 §2, §12
**Depends on:** Nothing
**Blocks:** Stories 3.2, 3.3

---

## Summary

Configuration struct for tunable protocol parameters and the ConnectionID type.

## Deliverables

- `ConnectionID` type:
  ```go
  type ConnectionID [16]byte
  ```

- `func (c ConnectionID) IsZero() bool` — all bytes zero

- `func (c ConnectionID) Hex() string` — 32 lowercase hex chars

- `func ParseConnectionIDHex(s string) (ConnectionID, error)` — parse from hex

- `func GenerateConnectionID() ConnectionID` — 16 random bytes from `crypto/rand`

- `ProtocolOptions` struct:
  ```go
  type ProtocolOptions struct {
      PingInterval          time.Duration // default: 15s
      IdleTimeout           time.Duration // default: 30s
      CloseHandshakeTimeout time.Duration // default: 250ms
      OutgoingBufferMessages int          // default: 256
      IncomingQueueMessages  int          // default: 64
      MaxMessageSize         int64        // default: 4 MiB
  }
  ```

- `func DefaultProtocolOptions() ProtocolOptions` — returns struct with all defaults

- `ErrZeroConnectionID` error type

## Acceptance Criteria

- [ ] `ConnectionID.IsZero()` true for zero, false for non-zero
- [ ] `Hex()` round-trips through `ParseConnectionIDHex`
- [ ] `GenerateConnectionID()` returns non-zero (vanishingly unlikely but check `IsZero`)
- [ ] Two calls to `GenerateConnectionID()` produce different values
- [ ] `ParseConnectionIDHex` rejects wrong-length, non-hex input
- [ ] `DefaultProtocolOptions()` returns correct defaults (15s, 30s, 250ms, 256, 64, 4 MiB)

## Design Notes

- `ConnectionID` is similar in shape to `Identity` but smaller (16 vs 32 bytes) and has different semantics (connection-scoped, not identity-scoped).
- `MaxMessageSize` protects against oversized incoming frames. Checked at the WebSocket read layer (Story 3.2).
