# Story 1.2: Client→Server Message Codecs

**Epic:** [Epic 1 — Message Types & Wire Encoding](EPIC.md)
**Spec ref:** SPEC-005 §3.1, §3.2, §7
**Depends on:** Story 1.1
**Blocks:** Epic 4 (Client Message Dispatch)

---

## Summary

BSATN encode/decode for all six client→server message types. Each message is decoded from a binary frame: `[tag: uint8][body: BSATN fields]`.

## Deliverables

- `SubscribeSingleMsg` struct + decode:
  ```go
  type SubscribeSingleMsg struct {
      RequestID   uint32
      QueryID     uint32
      QueryString string
  }
  ```

- `UnsubscribeSingleMsg` struct + decode:
  ```go
  type UnsubscribeSingleMsg struct {
      RequestID uint32
      QueryID   uint32
  }
  ```

- `CallReducerMsg` struct + decode:
  ```go
  type CallReducerMsg struct {
      RequestID   uint32
      ReducerName string
      Args        []byte // raw BSATN-encoded ProductValue
  }
  ```

- `OneOffQueryMsg` struct + decode:
  ```go
  type OneOffQueryMsg struct {
      MessageID   []byte
      QueryString string
  }
  ```

- `SubscribeMultiMsg` / `UnsubscribeMultiMsg` structs + decode:
  ```go
  type SubscribeMultiMsg struct {
      RequestID    uint32
      QueryID      uint32
      QueryStrings []string
  }

  type UnsubscribeMultiMsg struct {
      RequestID uint32
      QueryID   uint32
  }
  ```

- `func DecodeClientMessage(frame []byte) (tag uint8, msg any, err error)` — reads tag byte, dispatches to per-type decoder. Returns `ErrUnknownMessageTag` for unrecognized tags.

## Acceptance Criteria

- [ ] Round-trip each C2S message type: encode → decode → fields match
- [ ] SubscribeSingle with one SQL query string decodes correctly
- [ ] SubscribeMulti with 3 query strings decodes all three
- [ ] UnsubscribeSingle / UnsubscribeMulti decode `query_id` without a `send_dropped` byte
- [ ] CallReducer with empty `Args` (zero-length byte slice) is valid
- [ ] Unknown tag byte → `ErrUnknownMessageTag`
- [ ] Truncated body → `ErrMalformedMessage`
- [ ] Empty frame (zero bytes) → `ErrMalformedMessage`

## Design Notes

- Client→server messages are never compressed in v1. No compression handling needed in decode path.
- `CallReducerMsg.Args` is kept as raw bytes. The protocol layer does not validate argument types — that's the executor's job (SPEC-003).
- `DecodeClientMessage` returns `any` because the caller (dispatch loop) switches on the tag anyway. A type-switch on `SubscribeSingleMsg`, `SubscribeMultiMsg`, and the other concrete message types is the expected pattern.
