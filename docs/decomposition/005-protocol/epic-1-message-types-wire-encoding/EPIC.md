# Epic 1: Message Types & Wire Encoding

**Parent:** [SPEC-005-protocol.md](../SPEC-005-protocol.md) §3, §6, §7 (struct layouts), §8 (struct layouts)
**Blocked by:** Nothing — leaf epic
**Blocks:** Epic 3 (WebSocket Transport), Epic 4 (Client Message Dispatch), Epic 5 (Server Message Delivery)

**Cross-spec:** Uses BSATN encoding conventions from SPEC-002.

---

## Stories

| Story | File | Summary |
|---|---|---|
| 1.1 | [story-1.1-tag-constants-wire-types.md](story-1.1-tag-constants-wire-types.md) | Message tag enums, Query/Predicate wire structs, RowList encode/decode |
| 1.2 | [story-1.2-client-message-codecs.md](story-1.2-client-message-codecs.md) | BSATN encode/decode for Subscribe, Unsubscribe, CallReducer, OneOffQuery |
| 1.3 | [story-1.3-server-message-codecs.md](story-1.3-server-message-codecs.md) | BSATN encode/decode for all 7 server→client messages |
| 1.4 | [story-1.4-compression-envelope.md](story-1.4-compression-envelope.md) | Gzip compression envelope, wrap/unwrap, error handling |

## Implementation Order

```
Story 1.1 (Tags + wire types + RowList)
  ├── Story 1.2 (C2S codecs)
  └── Story 1.3 (S2C codecs)
Story 1.4 (Compression) — parallel with 1.2/1.3, depends on 1.1
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 1.1 | `protocol/tags.go`, `protocol/wire_types.go`, `protocol/rowlist.go`, `protocol/rowlist_test.go` |
| 1.2 | `protocol/client_messages.go`, `protocol/client_messages_test.go` |
| 1.3 | `protocol/server_messages.go`, `protocol/server_messages_test.go` |
| 1.4 | `protocol/compression.go`, `protocol/compression_test.go` |
