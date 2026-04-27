# Story 1.1: Tag Constants, Wire Types & RowList

**Epic:** [Epic 1 — Message Types & Wire Encoding](EPIC.md)
**Spec ref:** SPEC-005 §3.2, §3.4, §6, §7.1.1
**Depends on:** Nothing
**Blocks:** Stories 1.2, 1.3, 1.4

---

## Summary

Foundation types for the wire protocol. Tag constants identify message types. Wire structs define SQL-query client messages and server update envelopes. RowList is the row-batch encoding used by multiple messages.

## Deliverables

- Client→server tag constants:
  ```go
  const (
      TagSubscribeSingle   uint8 = 1
      TagUnsubscribeSingle uint8 = 2
      TagCallReducer       uint8 = 3
      TagOneOffQuery       uint8 = 4
      TagSubscribeMulti    uint8 = 5
      TagUnsubscribeMulti  uint8 = 6
  )
  ```

- Server→client tag constants:
  ```go
  const (
      TagIdentityToken            uint8 = 1
      TagSubscribeSingleApplied   uint8 = 2
      TagUnsubscribeSingleApplied uint8 = 3
      TagSubscriptionError        uint8 = 4
      TagTransactionUpdate        uint8 = 5
      TagOneOffQueryResponse      uint8 = 6
      TagReducerCallResult        uint8 = 7 // reserved
      TagTransactionUpdateLight   uint8 = 8
      TagSubscribeMultiApplied    uint8 = 9
      TagUnsubscribeMultiApplied  uint8 = 10
  )
  ```

- SQL-query client wire structs:
  ```go
  type SubscribeSingleMsg struct {
      RequestID   uint32
      QueryID     uint32
      QueryString string
  }

  type OneOffQueryMsg struct {
      MessageID   []byte
      QueryString string
  }

  type SubscribeMultiMsg struct {
      RequestID    uint32
      QueryID      uint32
      QueryStrings []string
  }
  ```
  The older structured `Query{TableName, Predicates}` / `Predicate{Column, Value}` wire shape is superseded; protocol v1 carries SQL text only.

- `RowList` encode/decode:
  ```go
  func EncodeRowList(rows [][]byte) []byte
  func DecodeRowList(data []byte) ([][]byte, error)
  ```
  Wire format: `[row_count: uint32 LE] [for each row: [row_len: uint32 LE] [row_data: row_len bytes]]`

- Protocol `SubscriptionUpdate` wire struct (derived from SPEC-004 §10.2 for wire delivery; protocol omits `TableID`, which is evaluator-internal and not meaningful as a client wire identifier):
  ```go
  type SubscriptionUpdate struct {
      QueryID   uint32 // client query_id; internal SubscriptionID is not on the wire
      TableName string
      Inserts   []byte // encoded RowList
      Deletes   []byte // encoded RowList
  }
  ```

- Error types:
  - `ErrUnknownMessageTag` — unrecognized tag byte
  - `ErrMalformedMessage` — body cannot be decoded

## Acceptance Criteria

- [ ] All 6 C2S and 10 S2C tag constants defined, no collisions within their namespace
- [ ] RowList round-trip: encode 0, 1, 100 rows → decode back, count and data match
- [ ] RowList decode with truncated data → `ErrMalformedMessage`
- [ ] RowList decode with row_len exceeding remaining data → `ErrMalformedMessage`
- [ ] Empty RowList (0 rows) encodes to 4 zero bytes, decodes to empty slice
- [ ] SubscribeSingle with one SQL `QueryString` is valid
- [ ] SubscribeMulti with multiple SQL `QueryStrings` is valid
- [ ] No structured `Query` / `Predicate` client wire structs are required or exported

## Design Notes

- RowList uses per-row length prefix (4 bytes overhead per row). Simpler than SpacetimeDB's `RowSizeHint` union. A `FixedSizeRowList` variant (no per-row prefix for fixed-schema rows) deferred to v2.
- Tags are separate namespaces for C2S and S2C. Tag value `1` means `SubscribeSingle` when sent by client, `IdentityToken` when sent by server. No ambiguity because direction is always known.
- `SubscriptionUpdate` wire struct is shared by applied envelopes, light transaction updates, and the committed arm of the heavy transaction update.
- Protocol v1 supports the bounded two-relation join subset documented in SPEC-005 §7.1.1. The wire `SubscriptionUpdate` shape remains query/table-name oriented: `query_id` identifies the client subscription and `table_name` names the row batch, while SPEC-004's internal `TableID` / join anchor remains an evaluator concern.
