# Story 1.1: Tag Constants, Wire Types & RowList

**Epic:** [Epic 1 — Message Types & Wire Encoding](EPIC.md)
**Spec ref:** SPEC-005 §3.2, §3.4, §6, §7.1.1
**Depends on:** Nothing
**Blocks:** Stories 1.2, 1.3, 1.4

---

## Summary

Foundation types for the wire protocol. Tag constants identify message types. Wire structs define the structured query format. RowList is the row-batch encoding used by multiple messages.

## Deliverables

- Client→server tag constants:
  ```go
  const (
      TagSubscribe    uint8 = 1
      TagUnsubscribe  uint8 = 2
      TagCallReducer  uint8 = 3
      TagOneOffQuery  uint8 = 4
  )
  ```

- Server→client tag constants:
  ```go
  const (
      TagInitialConnection  uint8 = 1
      TagSubscribeApplied   uint8 = 2
      TagUnsubscribeApplied uint8 = 3
      TagSubscriptionError  uint8 = 4
      TagTransactionUpdate  uint8 = 5
      TagOneOffQueryResult  uint8 = 6
      TagReducerCallResult  uint8 = 7
  )
  ```

- `Query` wire struct:
  ```go
  type Query struct {
      TableName  string
      Predicates []Predicate
  }
  ```

- `Predicate` wire struct:
  ```go
  type Predicate struct {
      Column string
      Value  Value  // SPEC-001 §2.2 Value encoding
  }
  ```

- `RowList` encode/decode:
  ```go
  func EncodeRowList(rows [][]byte) []byte
  func DecodeRowList(data []byte) ([][]byte, error)
  ```
  Wire format: `[row_count: uint32 LE] [for each row: [row_len: uint32 LE] [row_data: row_len bytes]]`

- Protocol `SubscriptionUpdate` wire struct (derived from SPEC-004 §10.2 for wire delivery; protocol omits `TableID`, which is evaluator-internal and not meaningful for protocol-v1 join-free subscriptions):
  ```go
  type SubscriptionUpdate struct {
      SubscriptionID uint32
      TableName      string
      Inserts        []byte // encoded RowList
      Deletes        []byte // encoded RowList
  }
  ```

- Error types:
  - `ErrUnknownMessageTag` — unrecognized tag byte
  - `ErrMalformedMessage` — body cannot be decoded

## Acceptance Criteria

- [ ] All 4 C2S and 7 S2C tag constants defined, no collisions within their namespace
- [ ] RowList round-trip: encode 0, 1, 100 rows → decode back, count and data match
- [ ] RowList decode with truncated data → `ErrMalformedMessage`
- [ ] RowList decode with row_len exceeding remaining data → `ErrMalformedMessage`
- [ ] Empty RowList (0 rows) encodes to 4 zero bytes, decodes to empty slice
- [ ] Query with zero predicates is valid
- [ ] Query with multiple predicates is valid

## Design Notes

- RowList uses per-row length prefix (4 bytes overhead per row). Simpler than SpacetimeDB's `RowSizeHint` union. A `FixedSizeRowList` variant (no per-row prefix for fixed-schema rows) deferred to v2.
- Tags are separate namespaces for C2S and S2C. Tag value `1` means `Subscribe` when sent by client, `InitialConnection` when sent by server. No ambiguity because direction is always known.
- `SubscriptionUpdate` wire struct defined here because it appears in both `TransactionUpdate` and `ReducerCallResult` messages.
- Protocol v1 `Subscribe` rejects joins and other multi-table subscriptions. Therefore the wire `SubscriptionUpdate` shape is intentionally single-table: `table_name` is authoritative on the wire, while SPEC-004's internal `TableID` / join anchor remains an evaluator concern.
