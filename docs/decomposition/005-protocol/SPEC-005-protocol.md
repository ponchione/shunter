# SPEC-005 — Client Protocol

**Status:** Draft  
**Depends on:** SPEC-001 (row encoding), SPEC-002 (BSATN encoding), SPEC-003 (executor interfaces), SPEC-004 (subscription evaluator)  
**Depended on by:** None (terminal spec)

---

## 1. Purpose and Scope

The client protocol defines the WebSocket-based interface between Shunter and its clients. It covers:

- WebSocket connection establishment and authentication
- Message framing and wire encoding
- All client→server and server→client message types
- Subscription lifecycle (register, receive deltas, unregister)
- Reducer call and response semantics
- Keep-alive, backpressure, and disconnection behavior

This spec does not cover:
- Row storage or changeset format (SPEC-001)
- Commit log encoding (SPEC-002)
- Reducer execution (SPEC-003)
- Subscription evaluation algorithm (SPEC-004)

---

## 2. Transport

```go
// ConnectionID is a 16-byte opaque identifier for one WebSocket connection.
// Clients may supply it on connect; the server generates one if absent.
// All-zeros is rejected.
type ConnectionID [16]byte
```

### 2.1 WebSocket

Shunter uses WebSocket (RFC 6455) over HTTP/1.1 or HTTP/2. All application messages use **binary frames** (opcode 0x2). Text frames are rejected with a Close frame.

### 2.2 Protocol Identifier

Shunter defines one protocol version: `v1.bsatn.shunter`

The client includes this string in the `Sec-WebSocket-Protocol` request header. The server echoes it in the response header if accepted. If the server does not support the requested protocol, it closes the connection with status 400.

```
Client: Sec-WebSocket-Protocol: v1.bsatn.shunter
Server: Sec-WebSocket-Protocol: v1.bsatn.shunter
```

### 2.3 Endpoint

```
GET /subscribe?token=<jwt>
    &connection_id=<16-hex-bytes>   [optional]
    &compression=<none|gzip>        [optional; default: none]
```

`token` may alternatively be supplied as `Authorization: Bearer <JWT>` HTTP header (preferred over query parameter).

`connection_id` is a client-supplied 16-byte identifier, hex-encoded. If absent, the server generates one randomly. `connection_id` all-zeros is reserved and rejected with 400. Clients may reuse a previous `connection_id` on reconnect to signal intent to resume (future session-resume feature; no semantic effect in v1).

---

## 3. Wire Encoding

### 3.1 BSATN

All messages are serialized using BSATN (the binary encoding defined in SPEC-002 §3.3). Each WebSocket frame payload contains exactly one complete message, length-delimited by the WebSocket frame header. No additional length prefix on the message itself.

### 3.2 Message Framing

Each message begins with a 1-byte **message type tag**, followed by the BSATN-encoded message body. The tag identifies the message type and determines how to decode the body.

```
[tag: uint8] [body: BSATN-encoded fields]
```

Tags are stable and version-specific.

Behavior on unknown tags:
- **Client → server:** the server MUST close the connection with a protocol error (`1002`) and log the offending tag. Silently ignoring an unknown request would leave the client hanging without a response.
- **Server → client:** the client MUST treat an unknown tag as a protocol error for this protocol version and close or surface a fatal decode error. Forward compatibility for additive message types requires negotiating a newer protocol version, not silently skipping frames in v1.

### 3.3 Compression

Compression is **server → client only** in v1. Client → server messages are never compressed.

When compression is disabled for a connection, server messages use the normal framing:

```
[tag: uint8] [body: BSATN-encoded fields]
```

When compression is enabled for a connection and the server chooses to compress a specific message, the payload is:

```
[compression: uint8] [tag: uint8] [compressed_body: []byte]
```

Where `compressed_body` is the gzip-compressed form of the BSATN body bytes only. The message tag itself is never compressed.

Compression values:
- `0x00` = uncompressed body, but explicit compression envelope present for this message: `[0x00][tag][body]`
- `0x01` = gzip-compressed body: `[0x01][tag][gzip(body)]`

If compression is negotiated as `none`, the explicit compression byte is omitted entirely and all server messages use `[tag][body]`.

Error handling:
- Unknown compression tag → protocol error (`1002`) and close
- Decompression failure → protocol error (`1002`) and close

**Recommendation:** Implement compression as optional in v1. Default to `none`. Add Gzip as a v1 option when large delta messages become a profiling concern.

### 3.4 Row Encoding (RowList)

Rows in subscription responses are encoded as a `RowList`:

```
RowList:
  row_count : uint32 LE
  [ for each row:
      row_len  : uint32 LE
      row_data : [row_len]byte    — ProductValue encoding (SPEC-002 §3.3)
  ]
```

Each row is prefixed with its length. This is simpler than SpacetimeDB's `BsatnRowList` (which uses a `RowSizeHint` union to avoid per-row headers for fixed-size rows). The length-per-row approach adds 4 bytes overhead per row but is unambiguous to decode without schema information.

**When to revisit:** If profiling shows that row delivery bandwidth is a bottleneck for fixed-schema tables, add a `FixedSizeRowList` variant (row count + row size, no per-row length prefix). Defer to v2.

---

## 4. Authentication

### 4.1 JWT Token

Shunter validates client identity via a JWT. The JWT must be signed with a key registered at engine startup. Claims:

| Claim | Type | Meaning |
|---|---|---|
| `sub` | string | Subject identifier |
| `iss` | string | Issuer identifier |
| `aud` | string or []string | Intended audience. v1 servers MAY validate this against configured accepted audiences; if audience validation is disabled, deployments MUST document that choice. |
| `exp` | number | Unix timestamp expiry (optional; if present, must not be in the past) |
| `iat` | number | Issued-at timestamp |
| `hex_identity` | string | Optional redundant identity claim. If present, it MUST match the identity recomputed from `iss` and `sub`. |

`Identity` is a protocol-level 32-byte opaque identifier. Its exact derivation and canonical string form MUST be defined in the shared identity type spec used by the engine and protocol layers. For v1, the only required semantic property is: the same `(iss, sub)` pair always maps to the same `Identity` across reconnections.

### 4.2 Token Generation

Shunter supports two authentication modes:

1. **Strict auth mode**: a valid externally issued JWT is required. Missing or invalid credentials cause the HTTP upgrade to fail with `401`.
2. **Anonymous minting mode**: if no token is presented, the server generates a fresh `Identity`, signs a local JWT for it, and returns that token in `InitialConnection.token`. The client should persist this token and send it on reconnect to preserve identity.

When the server mints a token in anonymous mode, it MUST define:
- the local issuer string
- the audience value(s) placed in the token
- whether `exp` is omitted or set to a finite lifetime

For production deployments, an external identity provider may sign tokens. The engine is configured with the signing key or JWKS endpoint at startup. This spec does not cover external IdP integration details.

### 4.3 Authentication Errors

- No token, and engine is in strict auth mode → `401` before WebSocket upgrade
- Invalid token signature → `401` before WebSocket upgrade
- Expired token → `401` before WebSocket upgrade
- `hex_identity` present but does not match recomputed identity → `401` before WebSocket upgrade
- Zero `connection_id` → `400` before WebSocket upgrade

---

## 5. Connection Lifecycle

### 5.1 Phases

```
1. HTTP upgrade (authentication validated, protocol negotiated)
2. WebSocket open → server sends InitialConnection
3. Client ready: may send Subscribe, CallReducer, OneOffQuery, Unsubscribe
4. Ongoing: server sends TransactionUpdate after relevant commits
5. Disconnect: either side sends Close frame
```

### 5.2 OnConnect Hook

After the WebSocket opens and before `InitialConnection` is sent, the executor runs the `OnConnect` reducer (SPEC-003 §10.3). If `OnConnect` returns an error, the connection is closed with a Close frame (code 1008: Policy Violation). No `InitialConnection` is sent.

### 5.3 OnDisconnect Hook

When the connection closes (for any reason), the executor runs the `OnDisconnect` reducer. All subscriptions for the connection are removed before `OnDisconnect` runs. If `OnDisconnect` returns an error, the error is logged; the disconnect proceeds regardless.

### 5.4 Keep-Alive

The server sends a WebSocket Ping frame every `PingInterval` (default: 15 seconds). The client must respond with Pong. If no data (including Pong) is received within `IdleTimeout` (default: 30 seconds), the server sends a Close frame and closes the connection.

---

## 6. Message Type Tags

### Client→Server tags

| Tag | Message |
|---|---|
| 1 | Subscribe |
| 2 | Unsubscribe |
| 3 | CallReducer |
| 4 | OneOffQuery |

### Server→Client tags

| Tag | Message |
|---|---|
| 1 | InitialConnection |
| 2 | SubscribeApplied |
| 3 | UnsubscribeApplied |
| 4 | SubscriptionError |
| 5 | TransactionUpdate |
| 6 | OneOffQueryResult |
| 7 | ReducerCallResult |

---

## 7. Client→Server Messages

### 7.1 Subscribe

Register a new subscription. The client chooses a `subscription_id` unique among its currently active and pending subscriptions.

```
tag: 1
request_id:      uint32 LE    — client-generated; echoed in response
subscription_id: uint32 LE    — client-chosen; unique per active connection
query:           Query        — structured subscription query (see §7.1.1)
```

**Response:** `SubscribeApplied` or `SubscriptionError`

After `SubscribeApplied`, the client receives `TransactionUpdate` for all future commits that affect this subscription's result set.

#### 7.1.1 Query Format

v1 supports **single-table column-equality predicates** only, expressed as a structured query (not raw SQL):

```
Query:
  table_name : string
  predicates : []Predicate

Predicate:
  column : string
  value  : Value     — (SPEC-001 §2.2 Value encoding)
```

Normalization into the SPEC-004 model:
- `predicates = []` maps to `AllRows(table_name)`
- one predicate maps to `ColEq(table_name, column, value)`
- multiple predicates are normalized as a left-associative binary `And` tree:
  `[P1, P2, P3]` → `And{Left: And{Left: P1, Right: P2}, Right: P3}`
  The outermost predicate is always the rightmost element.

A subscription with no predicates matches all rows in the table. Multiple predicates are ANDed.

Rejected in protocol v1:
- range predicates (`<`, `>`, `BETWEEN`)
- OR expressions
- joins
- references to more than one table

`Subscribe` validation MUST fail with `SubscriptionError` if:
- `table_name` does not exist
- any referenced column does not exist on that table
- the same `subscription_id` is already active **or pending** on the connection
- the predicate shape is not part of the v1 subset

**Design decision — no SQL in v1:** Raw SQL requires a parser and full query planner, significantly increasing engine complexity. Structured predicates cover the primary use case (subscribe to rows where column = value) and map directly to the index-backed evaluation in SPEC-004. SQL support is deferred to v2.

**Design decision — equality-only in v1:** Equality predicates are the common hot-path subscription shape and map cleanly onto SPEC-004's pruning indexes. Range and join subscriptions remain part of the evaluator's internal model, but they are not exposed on the public wire protocol in v1.

### 7.2 Unsubscribe

Remove an active subscription.

```
tag: 2
request_id:      uint32 LE
subscription_id: uint32 LE
send_dropped:    uint8         — 0 = no dropped rows; 1 = include current rows in response
```

**Response:** `UnsubscribeApplied`

### 7.3 CallReducer

Invoke a named reducer.

```
tag: 3
request_id:    uint32 LE
reducer_name:  string           — matches a registered reducer name
args:          bytes            — BSATN-encoded ProductValue of reducer arguments
```

**Response:** `ReducerCallResult`

The client is responsible for encoding `args` as a `ProductValue` matching the reducer's declared parameter types. Type mismatch is detected by the executor and returned as a `ReducerCallResult` with `status = FailedUser`.

### 7.4 OneOffQuery

Execute a read-only query that returns current matching rows, without establishing an ongoing subscription.

```
tag: 4
request_id:  uint32 LE
table_name:  string
predicates:  []Predicate    — same format as Subscribe §7.1.1
```

**Response:** `OneOffQueryResult`

The executor runs a read-only query against `CommittedState.Snapshot()` directly. This read is not atomic with subscription registration because it does not register subscription state; it only returns a point-in-time result from committed state.

---

## 8. Server→Client Messages

### 8.1 InitialConnection

First message sent after WebSocket opens (before any client message is processed).

```
tag: 1
identity:      bytes (32)     — client's Identity in canonical 32-byte wire form
connection_id: bytes (16)     — server-assigned or client-provided connection ID
token:         string          — JWT for reconnection; client should persist
```

### 8.2 SubscribeApplied

Subscription registered successfully. Contains all currently matching rows.

```
tag: 2
request_id:      uint32 LE
subscription_id: uint32 LE
table_name:      string
rows:            RowList       — all rows matching the query at subscribe time
```

The rows in `SubscribeApplied` represent a consistent snapshot. They are the starting state the client should use to populate its local cache.

### 8.3 UnsubscribeApplied

Subscription removed.

```
tag: 3
request_id:      uint32 LE
subscription_id: uint32 LE
has_rows:        uint8              — 0 = no rows; 1 = rows follow
rows:            RowList (if has_rows = 1)   — rows that were in the result set at unsubscribe time
```

### 8.4 SubscriptionError

Subscription failed. The subscription with the given `subscription_id` is now dead.

```
tag: 4
request_id:      uint32 LE    — echoes Subscribe request_id; 0 if error occurred during re-evaluation
subscription_id: uint32 LE
error:           string        — diagnostic message; not machine-parseable
```

On receiving this, the client must discard all cached rows for `subscription_id`. The `subscription_id` may be reused immediately.

### 8.5 TransactionUpdate

Sent after every committed transaction that affects at least one of this client's subscriptions.

```
tag: 5
tx_id:    uint64 LE
updates:  []SubscriptionUpdate {
    subscription_id: uint32 LE
    table_name:      string
    inserts:         RowList
    deletes:         RowList
}
```

`SubscriptionUpdate` Go struct is defined in SPEC-004 §10.2. The wire layout maps directly:
- `subscription_id uint32 LE` ← `SubscriptionUpdate.SubscriptionID`
- `table_name string` ← `SubscriptionUpdate.TableName`
- `inserts RowList` ← encoded from `SubscriptionUpdate.Inserts []ProductValue`
- `deletes RowList` ← encoded from `SubscriptionUpdate.Deletes []ProductValue`

The `tx_id` is a monotonically increasing commit identifier. Clients MAY persist it for diagnostics and coarse reconnect bookkeeping, but **v1 provides no resume-from-tx_id mechanism**. A client that disconnects must re-subscribe and rebuild state from a fresh `SubscribeApplied`.

`inserts` and `deletes` are defined in terms of the subscription result set, not physical storage operations:
- `inserts`: rows newly entering the subscription result set in this commit
- `deletes`: rows leaving the subscription result set in this commit

For a row update, treat the old row version and new row version separately:
- old row matches, new row matches → encode as `delete(old)` plus `insert(new)`
- old row matches, new row does not match → encode as `delete(old)` only
- old row does not match, new row matches → encode as `insert(new)` only
- neither version matches → omit the row entirely

A single `TransactionUpdate` may contain entries for multiple subscriptions (if the transaction affected more than one). A subscription with no changes in a given transaction does not appear in the update.

**Important:** If the same row matches multiple subscriptions, it appears in the update for each matching subscription independently. There is no deduplication across subscriptions.

### 8.6 OneOffQueryResult

Response to `OneOffQuery`.

```
tag: 6
request_id: uint32 LE
status:     uint8        — 0 = success; 1 = error
rows:       RowList      — present if status = 0
error:      string       — present if status = 1
```

### 8.7 ReducerCallResult

Response to `CallReducer`. Sent only to the calling client.

```
tag: 7
request_id:       uint32 LE
status:           uint8         — 0 = committed; 1 = failed_user; 2 = failed_panic; 3 = not_found
tx_id:            uint64 LE     — corresponds to TxID (SPEC-003 §6); 0 if the reducer did not commit
error:            string        — empty if status = 0
energy:           uint64 LE     — reserved; always 0 in v1
transaction_update: []SubscriptionUpdate   — same format as TransactionUpdate.updates
                                           — empty if status != 0
                                           — contains this caller's subscription updates from the commit
```

The embedded `transaction_update` is the subset of the transaction's deltas that matches this client's active subscriptions. It is included here rather than as a separate `TransactionUpdate` message to guarantee that the client receives its own reducer's effects atomically with the result.

Rules:
- if `status != 0`, `transaction_update` MUST be empty
- if the reducer committed but this client had no active matching subscriptions, `transaction_update` MUST be empty
- the caller MUST NOT receive a separate `TransactionUpdate` for the same `tx_id`; the embedded update is the complete caller-visible delta for that commit
- other clients still receive ordinary `TransactionUpdate` messages for their own matching subscriptions

Implementation note: the committed delta pipeline computes per-connection updates for the commit. Before standalone delivery, the caller connection's update slice (if any) is diverted into `ReducerCallResult` and omitted from the ordinary `TransactionUpdate` broadcast for that same connection.

---

## 9. Subscription Semantics

### 9.1 Subscription State Machine

```
[not subscribed]
    ↓ Subscribe(subscription_id)
[pending: server validating + evaluating]
    ↓ SubscribeApplied
[active: receiving TransactionUpdates]
    ↓ Unsubscribe(subscription_id)
[pending removal]
    ↓ UnsubscribeApplied
[not subscribed]

[pending or active]
    ↓ SubscriptionError
[not subscribed]

[pending or active]
    ↓ disconnect
[not subscribed]
```

State rules:
- a `subscription_id` is reserved as soon as `Subscribe` is accepted for processing; a second `Subscribe` with the same ID while pending or active MUST fail with `SubscriptionError`
- `Unsubscribe` for a pending or unknown `subscription_id` returns `ErrSubscriptionNotFound`
- if the client disconnects while a subscription is pending, the registration result is discarded and the subscription never becomes active

### 9.2 Client-Maintained State

The client is responsible for maintaining a local cache per subscription:
- On `SubscribeApplied`: populate cache with initial rows
- On `TransactionUpdate.inserts`: add rows to cache
- On `TransactionUpdate.deletes`: remove rows from cache
- On `SubscriptionError`: discard cache entirely
- On `UnsubscribeApplied`: discard cache

The cache at any point in time should equal the result of the subscription query run against committed state after the last received `TransactionUpdate`.

### 9.3 Multiple Subscriptions

A client may have multiple active subscriptions simultaneously. Each has a unique `subscription_id`. They are independent: a `TransactionUpdate` may contain updates for multiple subscriptions in one message, or may contain updates for only some.

### 9.4 Subscription During Active Transaction

The executor serializes subscription registration with commits (SPEC-003 §2.5). `Subscribe` commands go through the executor's inbox. The `SubscribeApplied` response is consistent: the initial rows match committed state as of the moment the subscription was registered, and the first `TransactionUpdate` this client receives for this subscription will contain only changes from transactions that committed after registration.

Ordering guarantee on one connection:
- `SubscribeApplied(subscription_id)` MUST be delivered before any `TransactionUpdate` that references that `subscription_id`
- for a reducer call made by this same connection, `ReducerCallResult(tx_id)` replaces the caller's standalone `TransactionUpdate(tx_id)` rather than racing with it

---

## 10. Backpressure

### 10.1 Server → Client

The server buffers outgoing messages per-client up to `OutgoingBufferMessages` (default: 256 messages). If enqueueing the **next** outbound message would exceed that limit, the server MUST:
1. leave already-queued messages untouched
2. enqueue or send a Close frame if possible (`1008`, reason: `"send buffer full"`)
3. stop accepting further outbound application messages for that connection
4. close the connection

The overflow-causing application message is not delivered. The client must reconnect.

**Design decision:** Disconnect on buffer overflow rather than drop messages. Dropped deltas would corrupt the client's local cache (it would be missing rows). Disconnection is recoverable: the client reconnects and re-subscribes, rebuilding the cache from a fresh `SubscribeApplied`.

### 10.2 Client → Server

The server maintains a per-connection incoming message queue with capacity `IncomingQueueMessages` (default: 64). If receiving the **next** client message would exceed that queue limit, the server closes the connection with Close code `1008`, reason: `"too many requests"`. The overflow-causing message is not processed.

---

## 11. Disconnection

### 11.1 Clean Close

Either side may send a WebSocket Close frame. The receiver echoes a Close frame. The connection is then closed. Close codes follow RFC 6455.

Server-initiated closes:
- `1000` (Normal Closure): graceful engine shutdown
- `1008` (Policy Violation): authentication failure, buffer overflow, too many requests
- `1011` (Internal Error): unexpected server error

### 11.2 Network Failure

If the underlying TCP connection drops without a Close frame, the server detects this via the Ping timeout (idle timeout of 30 s). All subscriptions and the `sys_clients` row are cleaned up, and `OnDisconnect` runs.

### 11.3 Reconnection

Clients should reconnect with exponential backoff. On reconnect:
1. Present the same `token` from `InitialConnection` to preserve `Identity`
2. Re-subscribe to all desired subscriptions (server has no subscription state from previous connection)
3. Use `tx_id` from the last received `TransactionUpdate` to detect whether rows may have changed since disconnect (no gap-fill mechanism in v1; clients re-fetch initial state via `SubscribeApplied`)

---

## 12. Configuration

```go
type ProtocolOptions struct {
    // PingInterval: how often to send WebSocket Ping frames.
    // Default: 15s.
    PingInterval time.Duration

    // IdleTimeout: close connection if no data received in this duration.
    // Default: 30s.
    IdleTimeout time.Duration

    // CloseHandshakeTimeout: wait for Close echo before forceful close.
    // Default: 250ms.
    CloseHandshakeTimeout time.Duration

    // OutgoingBufferMessages: max queued outgoing messages per client.
    // Default: 256.
    OutgoingBufferMessages int

    // IncomingQueueMessages: max queued incoming messages per client.
    // Default: 64.
    IncomingQueueMessages int

    // MaxMessageSize: reject incoming messages larger than this.
    // Default: 4 MiB.
    MaxMessageSize int64
}
```

---

## 13. Interfaces to Other Subsystems

### SPEC-003 (Transaction Executor)

The protocol layer sends commands to the executor via its inbox (`ExecutorCommand`):
- `CallReducerCmd` — for `CallReducer` messages
- `RegisterSubscriptionCmd` — for `Subscribe` messages  
- `UnregisterSubscriptionCmd` — for `Unsubscribe` messages
- `DisconnectClientSubscriptionsCmd` — on client disconnect

The executor sends `ReducerCallResult` back to the protocol layer via the `ResponseCh` embedded in `CallReducerCmd`.

### SPEC-004 (Subscription Evaluator)

After each commit, the subscription evaluator does **not** write to sockets directly. Instead, it sends a `FanOutMessage` to the fan-out worker. `CommitFanout` is defined in SPEC-004 §7 as `map[ConnectionID][]SubscriptionUpdate`. `SubscriptionUpdate` is defined in SPEC-004 §10.2.

Delivery contract:
1. evaluator computes `CommitFanout` for the committed transaction and sends `FanOutMessage{TxDurable, Fanout}` to the fan-out worker inbox
2. the fan-out worker treats each `CommitFanout[connID]` entry as the `[]SubscriptionUpdate` payload for one `TransactionUpdate`
3. if the commit originated from `CallReducer`, the protocol/executor integration identifies the caller's `ConnectionID` and routes that connection's update slice into `ReducerCallResult.transaction_update` instead of also sending a standalone `TransactionUpdate`
4. all remaining connection entries are delivered as standalone `TransactionUpdate` messages
5. protocol layer serializes, optionally compresses, and enqueues those messages to websocket connections

The fan-out worker constructs `TransactionUpdate{TxID: ..., Updates: updates}` from the `CommitFanout` entries before calling `SendTransactionUpdate`.

```go
type ClientSender interface {
    // SendTransactionUpdate queues a standalone post-commit delta for a client.
    // Returns ErrClientBufferFull if the client's outgoing buffer is full.
    SendTransactionUpdate(connID ConnectionID, update *TransactionUpdate) error

    // SendReducerResult queues the caller-visible reducer outcome, including
    // the caller's embedded transaction_update subset.
    SendReducerResult(connID ConnectionID, result *ReducerCallResult) error
}
```

### SPEC-001 (In-Memory Store)

`OneOffQuery` uses `CommittedState.Snapshot()` directly (read-only, bypassing subscription registration ordering) to serve the query result. This is safe because `OneOffQuery` does not create persistent subscription state and therefore does not need atomic registration semantics.

---

## 14. Error Catalog

| Error | Condition |
|---|---|
| `ErrUnknownMessageTag` | Client sent a message with an unrecognized tag |
| `ErrUnknownCompressionTag` | Server or client observed a compression tag not defined by this protocol version |
| `ErrMalformedMessage` | Message body could not be decoded |
| `ErrDecompressionFailed` | Compressed frame could not be decompressed |
| `ErrDuplicateSubscriptionID` | Subscribe with a `subscription_id` already active or pending |
| `ErrSubscriptionNotFound` | Unsubscribe for a `subscription_id` not active |
| `ErrReducerNotFound` | CallReducer named a reducer not registered |
| `ErrLifecycleReducer` | CallReducer named a lifecycle reducer (OnConnect/OnDisconnect) |
| `ErrClientBufferFull` | Server cannot send to this client (triggers disconnect) |
| `ErrZeroConnectionID` | Client-supplied connection_id is all zeros |

---

## 15. Open Questions

1. **Query language evolution.** v1 uses structured predicates (table + equality conditions). The path to SQL subscriptions needs a query language spec. When should this be planned? Recommendation: after v1 implementation is complete and subscription usage patterns are observed.

2. **Gap-fill / resume protocol.** Clients may remember the last `tx_id`, but v1 has no way to request missed deltas. They must re-subscribe and accept a full `SubscribeApplied`. Should the server support `resume_from_tx_id` for short disconnections? This requires the server to retain a short delta buffer. Deferred to v2.

3. **Subscription for all rows (no predicate) vs table watch.** A subscription with no predicates returns all table rows. For large tables this may be expensive. Should there be an explicit `WatchTable` message, or is the no-predicate Subscribe sufficient? No distinction needed in v1 — document the performance implication clearly.

4. **Identity type spec ownership.** The protocol relies on a shared `Identity` type but this spec does not define its canonical derivation or string form. Should that live in a dedicated shared type spec or be added to SPEC-001/another foundational spec?

5. **Anonymous token policy defaults.** If anonymous minting mode is enabled, what are the default issuer, audience, and expiry settings? This spec requires them to be configured/documented but does not prescribe defaults.

---

## 16. Verification

| Test | What it verifies |
|---|---|
| Connect, receive InitialConnection | Connection establishment |
| Connect with invalid JWT → 401 | Authentication rejection |
| Connect with `connection_id = 0x00...00` → 400 | Zero connection_id rejection |
| Subscribe, receive SubscribeApplied with correct rows | Subscription registration and initial state |
| Subscribe with duplicate `subscription_id` while pending or active → SubscriptionError | Subscription ID reservation rules |
| Subscribe, insert rows via reducer, receive TransactionUpdate | Delta delivery after commit |
| Subscribe, delete rows via reducer, receive TransactionUpdate with deletes | Delete delta delivery |
| Update row so old+new both match predicate → delete(old)+insert(new) | In-place update delta semantics |
| Update row so it enters predicate → insert only | Moved-into-range semantics |
| Update row so it leaves predicate → delete only | Moved-out-of-range semantics |
| Subscribe, insert+delete same row in one reducer → no TransactionUpdate rows | Net-effect semantics |
| Two subscriptions, one commit affecting both → one TransactionUpdate with two entries | Multi-subscription delta |
| Unsubscribe with `send_dropped=1`, verify rows in UnsubscribeApplied | Dropped rows on unsubscribe |
| Subscribe with invalid table name → SubscriptionError | Compile-time error |
| CallReducer success with active subscriptions → ReducerCallResult with embedded transaction_update | Reducer result with caller delta |
| CallReducer success with no active subscriptions → empty embedded transaction_update and no separate TransactionUpdate | Caller no-subscription semantics |
| CallReducer that returns error → ReducerCallResult with status=failed_user | User error result |
| CallReducer + Subscribe to same table: verify no duplicate TransactionUpdate | Caller gets result only via ReducerCallResult |
| OneOffQuery → correct rows returned from committed snapshot | One-off read semantics |
| Unknown client message tag → protocol error close | Incoming framing validation |
| Unknown compression tag / invalid gzip payload → protocol error close | Compression envelope validation |
| Client sends > IncomingQueueMessages rapidly → connection closed | Incoming backpressure |
| Server can't deliver to slow client → connection closed (buffer full) | Outgoing backpressure |
| Idle connection for > IdleTimeout → connection closed | Idle timeout |
| Client disconnects cleanly → OnDisconnect fires, sys_clients row removed | Disconnect lifecycle |
| Client disconnects without Close frame → server detects via Ping timeout | Network failure detection |
| Reconnect with same token → same Identity in InitialConnection | Identity preservation |
| OnConnect reducer returns error → connection closed before InitialConnection | OnConnect rejection |
| `SubscribeApplied(subscription_id)` always arrives before any `TransactionUpdate` referencing that ID | Per-connection ordering guarantee |
