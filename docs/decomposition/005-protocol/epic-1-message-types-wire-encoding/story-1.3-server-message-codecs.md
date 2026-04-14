# Story 1.3: Server→Client Message Codecs

**Epic:** [Epic 1 — Message Types & Wire Encoding](EPIC.md)
**Spec ref:** SPEC-005 §3.1, §3.2, §8
**Depends on:** Story 1.1
**Blocks:** Epic 3 (InitialConnection send), Epic 5 (all server message delivery)

---

## Summary

BSATN encode for all seven server→client message types. Each message encodes to `[tag: uint8][body: BSATN fields]`. Decode is also provided for testing and potential client-side use.

## Deliverables

- `InitialConnection`:
  ```go
  type InitialConnection struct {
      Identity     [32]byte
      ConnectionID [16]byte
      Token        string
  }
  ```

- `SubscribeApplied`:
  ```go
  type SubscribeApplied struct {
      RequestID      uint32
      SubscriptionID uint32
      TableName      string
      Rows           []byte // encoded RowList
  }
  ```

- `UnsubscribeApplied`:
  ```go
  type UnsubscribeApplied struct {
      RequestID      uint32
      SubscriptionID uint32
      HasRows        bool   // wire: uint8
      Rows           []byte // encoded RowList; present if HasRows
  }
  ```

- `SubscriptionError`:
  ```go
  type SubscriptionError struct {
      RequestID      uint32
      SubscriptionID uint32
      Error          string
  }
  ```

- `TransactionUpdate`:
  ```go
  type TransactionUpdate struct {
      TxID    uint64
      Updates []SubscriptionUpdate
  }
  ```

- `OneOffQueryResult`:
  ```go
  type OneOffQueryResult struct {
      RequestID uint32
      Status    uint8  // 0 = success, 1 = error
      Rows      []byte // encoded RowList; present if Status == 0
      Error     string // present if Status == 1
  }
  ```

- `ReducerCallResult`:
  ```go
  type ReducerCallResult struct {
      RequestID         uint32
      Status            uint8  // 0=committed, 1=failed_user, 2=failed_panic, 3=not_found
      TxID              uint64
      Error             string
      Energy            uint64 // reserved, always 0 in v1
      TransactionUpdate []SubscriptionUpdate
  }
  ```

- `func EncodeServerMessage(msg any) ([]byte, error)` — encodes tag + body. Type-switches on concrete message type.

## Acceptance Criteria

- [ ] Round-trip each of 7 S2C message types: encode → decode → fields match
- [ ] InitialConnection: Identity and ConnectionID byte arrays preserved exactly
- [ ] SubscribeApplied: RowList payload preserved as-is (opaque bytes)
- [ ] UnsubscribeApplied: `HasRows=false` → no Rows field on wire
- [ ] UnsubscribeApplied: `HasRows=true` → Rows field present
- [ ] TransactionUpdate with 0 SubscriptionUpdates encodes correctly
- [ ] TransactionUpdate with 3 SubscriptionUpdates round-trips all entries
- [ ] ReducerCallResult: `Status != 0` → `TransactionUpdate` field encodes as empty slice
- [ ] OneOffQueryResult: `Status=0` → Rows present, Error empty
- [ ] OneOffQueryResult: `Status=1` → Error present, Rows absent

## Design Notes

- `Energy` in `ReducerCallResult` is reserved. Always encode as `0`. Ignore non-zero on decode (forward compat).
- `ReducerCallResult.TransactionUpdate` uses the same `[]SubscriptionUpdate` wire format as `TransactionUpdate.Updates`. Reuse the same encode/decode path.
- Server messages may be wrapped in a compression envelope (Story 1.4) before sending. The codecs here produce the uncompressed `[tag][body]` form.
