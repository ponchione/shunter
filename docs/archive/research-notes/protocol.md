# Client Protocol — Deep Research Note

Research into SpacetimeDB's WebSocket client protocol (`crates/client-api/`, `crates/client-api-messages/`). Extracts message types, wire format, connection lifecycle, and subscription semantics.

---

## 1. Protocol Versioning

SpacetimeDB supports two protocol generations:

- **v1**: `v1.json.spacetimedb` (JSON) and `v1.bsatn.spacetimedb` (BSATN binary). Legacy, still supported.
- **v2**: `v2.bsatn.spacetimedb`. Binary only. Current implementation.

Version is negotiated via the `Sec-WebSocket-Protocol` HTTP header during WebSocket upgrade. Client sends a list of supported protocols in preference order; server selects the most preferred one it supports and echoes it in the response header.

---

## 2. Connection Establishment

### 2.1 HTTP Endpoint

```
GET /{database_name_or_identity}/subscribe
    ?connection_id=<hex-bytes>        [optional; server generates if absent]
    &compression=<none|brotli|gzip>   [default: brotli]
    &light=<true|false>               [optional; bandwidth-reduction toggle]
    &confirmed=<true|false>           [v2 default: true]
```

Standard WebSocket upgrade headers required (`Upgrade: websocket`, `Sec-WebSocket-Version: 13`, etc.).

### 2.2 Authentication

Before the WebSocket upgrade is accepted, HTTP middleware resolves client credentials:
- **Bearer token** in `Authorization: Bearer <JWT>` header (preferred)
- **Query parameter** `?token=<JWT>` (fallback)
- If no credentials are supplied, the anonymous-auth path may mint a fresh local identity and token rather than rejecting the request outright
- JWT claims accepted by the auth layer include:
  - `hex_identity` (optional on input; if present it must match the computed identity)
  - `iss`
  - `sub`
  - `aud`
  - `iat`
  - `exp`
  - arbitrary extra claims
- `Identity` is a 256-bit value derived by `Identity::from_claims(issuer, subject)`. The exact derivation is **not** a simple `hash(issuer + subject)`; the implementation uses a custom BLAKE3-based scheme over `issuer|subject` plus a prefix/checksum layout.

For Shunter design purposes, the important extracted behavior is:
- identity is deterministic from `(iss, sub)`
- a token may optionally carry the derived identity redundantly
- missing credentials do **not** universally imply `401`; anonymous identity minting is supported in the client API path

### 2.3 First Server Message: InitialConnection

After the WebSocket is open, the server immediately sends `InitialConnection` as the first message:
```
InitialConnection {
    identity:      Identity      (256-bit)
    connection_id: ConnectionId  (128-bit, server-assigned)
    token:         string        (JWT for reconnection, clients should store this)
}
```

Clients can use the token to reconnect and supply the same `connection_id` as a query param. `ConnectionId::ZERO` is reserved; server rejects it with 400.

---

## 3. Client→Server Message Types (v2)

All client messages carry a `request_id: u32` set by the client. The server echoes it in the corresponding response, enabling async correlation.

| Message | Fields | Purpose |
|---|---|---|
| `Subscribe` | `request_id`, `query_set_id: u32`, `query_strings: []string` | Register a subscription (one or more SQL SELECT statements) |
| `Unsubscribe` | `request_id`, `query_set_id: u32`, `flags: u8` | Remove a subscription |
| `CallReducer` | `request_id`, `reducer: string`, `args: bytes` (BSATN-encoded), `flags: u8` | Invoke a transactional reducer |
| `CallProcedure` | `request_id`, `procedure: string`, `args: bytes` (BSATN-encoded), `flags: u8` | Invoke a non-transactional procedure |
| `OneOffQuery` | `request_id`, `query_string: string` | Execute a one-shot read query |

`Unsubscribe.flags = 1` (`SendDroppedRows`): server includes full current row set in `UnsubscribeApplied` response (so client can confirm what it's dropping).

---

## 4. Server→Client Message Types (v2)

| Message | Fields | Purpose |
|---|---|---|
| `InitialConnection` | `identity`, `connection_id`, `token` | First message after connection, always |
| `SubscribeApplied` | `request_id`, `query_set_id`, `rows: QueryRows` | Subscription registered; initial matching rows |
| `UnsubscribeApplied` | `request_id`, `query_set_id`, `rows: Option<QueryRows>` | Subscription removed; optionally dropped rows |
| `SubscriptionError` | `request_id: Option<u32>`, `query_set_id`, `error: string` | Subscription failed (compile-time or re-evaluation) |
| `TransactionUpdate` | `query_sets: []QuerySetUpdate` | Post-commit delta broadcast |
| `OneOffQueryResult` | `request_id`, `result: QueryRows or error` | One-shot query result |
| `ReducerResult` | `request_id`, `timestamp`, `result: ReducerOutcome` | Reducer call outcome |
| `ProcedureResult` | `request_id`, `timestamp`, `duration`, `status` | Procedure call outcome |

### ReducerOutcome variants:
- `Ok { ret_value: bytes, transaction_update: TransactionUpdate }` — committed, with subscription updates embedded
- `OkEmpty` — committed, no return value, no subscription updates (bandwidth optimization)
- `Err(bytes)` — user-defined error (BSATN-encoded error type)
- `InternalError(string)` — panic or system error (diagnostic only, not canonical)

---

## 5. Row Encoding: BsatnRowList

The key data structure for row delivery is `BsatnRowList`:

```
BsatnRowList {
    size_hint: RowSizeHint
    rows_data: bytes    // packed BSATN rows, no separators
}

RowSizeHint:
  | FixedSize(u16)         // all rows are exactly this many bytes
  | RowOffsets([]u64)      // starting byte offset of each row within rows_data
```

There are no per-row length prefixes. Row boundaries are determined entirely from `size_hint`:
- `FixedSize(n)`: row count = `len(rows_data) / n`; row i starts at `i * n`
- `RowOffsets([o0, o1, ...]`: row i starts at `o_i`, ends at `o_{i+1}` (or end of data)

This encoding enables parallel decode: offsets are known before reading any row payload.

`QueryRows` is a list of per-table row sets:
```
QueryRows {
    tables: []SingleTableRows {
        table: string    // table name
        rows: BsatnRowList
    }
}
```

`TransactionUpdate` contains `[]QuerySetUpdate`:
```
QuerySetUpdate {
    query_set_id: u32
    tables: []TableUpdate {
        table_name: string
        rows: []TableUpdateRows    // inserts and deletes per table
    }
}
```

---

## 6. Subscription Semantics

### 6.1 QuerySetId

Each subscription is identified by a `QuerySetId: u32`, **chosen by the client** (not server-assigned). Multiple queries can be bundled into one subscription by passing multiple strings to `query_strings`. All queries in one subscription share one `query_set_id`.

A `query_set_id` must not conflict with an existing active subscription on the same connection. If it does, the server returns `SubscriptionError`.

### 6.2 Subscribe Flow

1. Client sends `Subscribe { query_set_id: 7, query_strings: ["SELECT * FROM players WHERE guild_id = 5"] }`
2. Server compiles query, evaluates against current committed state
3. Server sends `SubscribeApplied { query_set_id: 7, rows: <all currently matching rows> }`
4. For every future commit that changes rows matching the query: server sends `TransactionUpdate { query_sets: [{ query_set_id: 7, tables: [...] }] }`

v2 behavior: `Subscribe` **adds** a new subscription. The existing subscriptions are not modified.

v1 behavior (different!): `Subscribe` **replaces all** existing subscriptions for the connection. This is a major semantic difference.

### 6.3 SubscriptionError

Errors at two points:
- **Subscribe time**: query fails to compile or is invalid → `SubscriptionError { request_id: Some(42), ... }`
- **Re-evaluation time**: query fails during transaction evaluation → `SubscriptionError { request_id: None, ... }`

On receiving `SubscriptionError`, the client must discard all cached rows for that `query_set_id`. The `query_set_id` may be reused immediately.

### 6.4 Delta Semantics

`TransactionUpdate.tables` contains only the net-effect delta from the transaction:
- `inserts`: rows that are now newly part of the subscription result set
- `deletes`: rows that were removed from the subscription result set

This is identical to the store's net-effect changeset: if a row is inserted and deleted in one transaction, it appears in neither. A row moving out of the query result set (even if not physically deleted) appears as a delete.

---

## 7. Reducer Calls

### 7.1 CallReducer

Client sends args as BSATN-encoded bytes. The server deserializes according to the reducer's declared parameter types.

The caller always receives `ReducerResult` regardless of subscription state. Within `ReducerResult.result.Ok`, the `transaction_update` field contains the delta for the caller's own active query sets for that committed transaction.

If the caller has no matching active subscriptions, the update may be empty (`OkEmpty` in the v2 wire format when both the return value and update are empty).

### 7.2 Caller Delivery Semantics

What is verified from the audited source:
- the reducer response embeds the caller's `TransactionUpdate`
- `ReducerOutcome::OkEmpty` exists as an optimization when both return value and transaction update are empty

What is **not** established by the audited files alone:
- whether the caller also receives a second standalone `TransactionUpdate` broadcast for the same commit

Therefore the safe conclusion for Shunter design is:
- embedding the caller-visible delta in the reducer result is a proven part of the protocol
- suppressing a separate caller broadcast is a design choice Shunter may adopt, but should be treated as a deliberate v1 rule rather than as a source-proven SpacetimeDB fact unless deeper runtime code is audited

---

## 8. Compression

Server-side only (client messages are never compressed). A 1-byte compression tag is prepended to each server message:
- `0x00` = no compression (raw BSATN follows)
- `0x01` = Brotli compressed
- `0x02` = Gzip compressed

Default: Brotli. Applied only when message size exceeds a threshold (small messages sent uncompressed). The client declares its compression preference in the connection URL query params.

---

## 9. WebSocket Frame Handling

- All v2 messages use **binary frames** (opcode 0x2)
- Messages larger than 4096 bytes are fragmented into continuation frames (RFC 6455)
- Control frames (Ping, Pong, Close) may be interleaved but not application-frame continuations
- Max message size: 32 MiB
- Server sends Ping every 15 seconds; expects Pong response
- Idle timeout: 30 seconds without any data (including Ping/Pong)
- Close handshake: initiator sends Close, waits 250 ms for response, then forcefully closes

---

## 10. Backpressure

Server maintains a per-connection incoming message queue. Default capacity: 16,384 messages. If a client sends faster than the server processes:
- Server sends WebSocket Close frame with code `Again`
- Server closes the connection immediately

Client-side throttling is the expected behavior. SDKs should buffer outgoing messages and not flood the server.

---

## 11. Protocol Constants

| Setting | Default |
|---|---|
| Ping interval | 15 s |
| Idle timeout | 30 s |
| Close handshake timeout | 250 ms |
| Incoming queue | 16,384 messages |
| Max message size | 32 MiB |

---

## 12. Key Insights for Shunter's Go Design

### 12.1 Shunter needs only one protocol version

SpacetimeDB maintains v1 (JSON + BSATN) and v2 (BSATN) for backwards compatibility. Shunter has no existing clients to support — define one clean protocol version from day one. Use BSATN encoding (matching the store's encoding from SPEC-002). Call it `v1.bsatn.shunter`.

### 12.2 QuerySetId being client-chosen is elegant

Having the client pick its own subscription IDs allows the client to assign semantic meaning without a round-trip. Shunter should adopt this.

### 12.3 BsatnRowList's RowSizeHint is a bandwidth optimization, not a simplicity win

The `FixedSize` vs `RowOffsets` dual encoding is clever but adds decoder complexity. For v1, Shunter can use a simpler encoding: each row prefixed with a `uint32 LE` byte count. This is slightly larger on the wire but much simpler to encode and decode. Revisit if profiling shows row delivery bandwidth is a bottleneck.

### 12.4 Procedures are not needed

SpacetimeDB's `CallProcedure` is for non-transactional side effects (sending emails, etc.). Shunter's thesis is that all state mutations are transactional. Omit procedures in v1.

### 12.5 OneOffQuery is a convenience, not a necessity

Clients can get a one-time read by subscribing and immediately unsubscribing. `OneOffQuery` is a convenience shortcut. Include it in v1 because it simplifies client code significantly (no subscribe+unsubscribe dance for read-only queries).

### 12.6 Confirmed mode should be the only mode in v1

SpacetimeDB's "unconfirmed" mode (send updates before fsync) exists for low-latency use cases. In Shunter v1, always send updates after the commit is applied to in-memory state (which is what "confirmed" means for in-memory state — the disk durability is async). Document this as "commit-visible" semantics: clients receive updates after the transaction is committed to memory, not after it reaches disk.

### 12.7 v1 "replace all subscriptions" semantics are a footgun

SpacetimeDB's v1 protocol replaces all subscriptions on each Subscribe call. This makes atomic subscription updates simple on the server but creates surprising client behavior. Shunter should use v2 semantics: Subscribe adds, Unsubscribe removes.

### 12.8 Embedding TransactionUpdate inside ReducerResult is correct

The caller's `ReducerResult.Ok.transaction_update` containing their own transaction's deltas avoids a race where the caller might receive a `TransactionUpdate` before the `ReducerResult`. Keep this pattern.
