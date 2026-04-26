# SPEC-005 — Client Protocol

**Status:** Draft  
**Depends on:** SPEC-001 (`Identity`, `ConnectionID`, `TxID`, `CommittedReadView`, row encoding), SPEC-002 (BSATN encoding; `TxID(0)` sentinel reservation), SPEC-003 (`ExecutorCommand` set, reducer-call outcome metadata on `CallReducerCmd.ResponseCh`, `TxID` contract), SPEC-004 (`CommitFanout`, `FanOutMessage`, `SubscriptionUpdate`, `SubscriptionError`), SPEC-006 (`SchemaLookup`)  
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

Shunter's client protocol is Shunter-native. SpacetimeDB is an architectural
reference, not a wire-compatibility target, and Shunter clients should be built
against this protocol rather than assuming SpacetimeDB client interoperability.
Where reference behavior is cited below, treat it as design evidence unless
the section explicitly says it is part of Shunter's own contract.

---

## 2. Transport

```go
// ConnectionID is a 16-byte opaque identifier for one WebSocket connection.
// Clients may supply it on connect; the server generates one if absent.
// All-zeros is rejected.
// Declared in types/types.go (SPEC-001 §1.1); helpers live in types/connection_id.go.
// SPEC-005 consumes the declaration and does not redeclare the type.
type ConnectionID [16]byte
```

### 2.1 WebSocket

Shunter uses WebSocket (RFC 6455) over HTTP/1.1 or HTTP/2. All application messages use **binary frames** (opcode 0x2). Text frames are rejected with a Close frame.

### 2.2 Protocol Identifier

Current implementation admits two subprotocol tokens for historical reasons:

- `v1.bsatn.shunter` — Shunter-native token; this is the product protocol identifier Shunter clients should use.
- `v1.bsatn.spacetimedb` — historical reference-compatibility token; current code still accepts it, but it is not a product requirement and may be removed.

The client includes one or both tokens in the `Sec-WebSocket-Protocol` request header. The server echoes the selected token in the response header. If the client offers neither token, the server closes the connection with status 400.

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

`compression` accepts exactly `none` or `gzip`. Any other query value is rejected during upgrade with `400`.

---

## 3. Wire Encoding

### 3.1 BSATN

All messages are serialized using BSATN (the binary encoding defined in SPEC-002 §3.3). Each WebSocket frame payload contains exactly one complete message, length-delimited by the WebSocket frame header. No additional length prefix on the message itself.

> **Naming.** "BSATN" is a name imported from SpacetimeDB's `bsatn` crate; it is not a standard encoding format and the Shunter encoding is not byte-compatible with SpacetimeDB's. See the canonical disclaimer in **SPEC-002 §3.1**.

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
- `0x01` = brotli (reserved; Shunter does not implement — returns `ErrBrotliUnsupported` and closes with 1002 reason `brotli unsupported`; implement only if Shunter clients need it)
- `0x02` = gzip-compressed body: `[0x02][tag][gzip(body)]`

If compression is negotiated as `none`, the explicit compression byte is omitted entirely and all server messages use `[tag][body]`.

Error handling:
- Brotli tag (`0x01`) → protocol error (`1002`) with reason `brotli unsupported` and close
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

`Identity` is declared in `types/types.go` as `[32]byte` — SPEC-001 §2.4 owns the contract, and `types/identity.go` carries the derivation helpers (`DeriveIdentity`, `Hex`, `ParseIdentityHex`, `IsZero`). SPEC-005 does not redeclare the type. The required semantic property at the protocol layer is unchanged: the same `(iss, sub)` pair always maps to the same `Identity` across reconnections.

### 4.2 Token Generation

Shunter supports two authentication modes:

1. **Strict auth mode**: a valid externally issued JWT is required. Missing or invalid credentials cause the HTTP upgrade to fail with `401`.
2. **Anonymous minting mode**: if no token is presented, the server generates a fresh `Identity`, signs a local JWT for it, and returns that token in `IdentityToken.token`. The client should persist this token and send it on reconnect to preserve identity.

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
2. WebSocket open → server sends IdentityToken
3. Client ready: may send Subscribe, CallReducer, OneOffQuery, Unsubscribe
4. Ongoing: server sends TransactionUpdate after relevant commits
5. Disconnect: either side sends Close frame
```

### 5.2 OnConnect Hook

After the WebSocket opens and before `IdentityToken` is sent, the protocol layer dispatches `OnConnectCmd` (SPEC-003 §2.4) into the executor, which runs the `OnConnect` lifecycle flow (SPEC-003 §10.3). Lifecycle dispatch does NOT use `CallReducerCmd`; the insert-then-reducer transaction shape is not expressible through the normal reducer-call path. If `OnConnect` returns an error, the connection is closed with a Close frame (code 1008: Policy Violation). No `IdentityToken` is sent.

### 5.3 OnDisconnect Hook

When the connection closes (for any reason), the protocol layer dispatches `OnDisconnectCmd` (SPEC-003 §2.4) into the executor, which runs the `OnDisconnect` lifecycle flow (SPEC-003 §10.4). All subscriptions for the connection are removed before `OnDisconnect` runs. If `OnDisconnect` returns an error, the error is logged and a fresh cleanup transaction still deletes the `sys_clients` row (SPEC-003 §10.4 failure path). The disconnect proceeds regardless of reducer outcome.

### 5.4 Keep-Alive

The server sends a WebSocket Ping frame every `PingInterval` (default: 15 seconds). The client must respond with Pong. If no data (including Pong) is received within `IdleTimeout` (default: 30 seconds), the server sends a Close frame and closes the connection.

---

## 6. Message Type Tags

### Client→Server tags

| Tag | Message |
|---|---|
| 1 | SubscribeSingle |
| 2 | UnsubscribeSingle |
| 3 | CallReducer |
| 4 | OneOffQuery |
| 5 | SubscribeMulti |
| 6 | UnsubscribeMulti |

The `SubscribeSingle` / `UnsubscribeSingle` tags are the current v1 names for the former `Subscribe` / `Unsubscribe` byte values (1 and 2), which remain stable. `SubscribeMulti` / `UnsubscribeMulti` carry a query-set under one `query_id`. Canonical source is `protocol/tags.go`.

### Server→Client tags

| Tag | Message |
|---|---|
| 1 | IdentityToken |
| 2 | SubscribeSingleApplied |
| 3 | UnsubscribeSingleApplied |
| 4 | SubscriptionError |
| 5 | TransactionUpdate (heavy, caller-bound) |
| 6 | OneOffQueryResponse |
| 7 | **RESERVED** (formerly `ReducerCallResult`; see §8.7) |
| 8 | TransactionUpdateLight (non-caller delta-only) |
| 9 | SubscribeMultiApplied |
| 10 | UnsubscribeMultiApplied |

Tag 7 is reserved and MUST NOT be reallocated. The current v1 outcome model removed the standalone `ReducerCallResult` envelope, merging the caller outcome into the heavy `TransactionUpdate` (tag 5) and adding `TransactionUpdateLight` (tag 8) for non-callers. Holding tag 7 reserved prevents silent re-allocation if a future contributor reintroduces a separate caller envelope. Canonical source is `protocol/tags.go`.

---

## 7. Client→Server Messages

### 7.1 SubscribeSingle

Register a new single-query subscription. The client chooses a `query_id` unique among its currently active and pending subscriptions.

```
tag: 1
request_id:   uint32 LE    — client-generated; echoed in response
query_id:     uint32 LE    — client-chosen; unique per active connection
query_string: string       — SQL subscription query (see §7.1.1)
```

**Response:** `SubscribeSingleApplied` or `SubscriptionError`

After `SubscribeSingleApplied`, the caller receives the heavy `TransactionUpdate` (§8.5) for commits it originates, and non-callers with matching row-touches receive `TransactionUpdateLight` (§8.8).

#### 7.1.1 Query Format

The current v1 wire carries a **SQL query string** rather than the older structured `Query{table_name, predicates[]}` value. The handler parses the string with `query/sql.Parse` and lowers it into the same SPEC-004 predicate tree the structured wire used to build directly. The structured wire shape is gone; only the `QueryString` field exists on the envelope.

```
query_string : string    — SQL SELECT against exactly one table; single-row result set per match
```

v1 SQL subset supported by `query/sql.Parse`:
- `SELECT <cols> FROM <table>`
- optional `WHERE` clause combining column-equality, column-range (`<`, `<=`, `>`, `>=`), and `AND` conjunctions
- the outermost expression may be a single range/equality predicate or an `AND` tree

Normalization into the SPEC-004 model:
- no `WHERE` clause → `AllRows(table_name)`
- single equality → `ColEq(table_name, column, value)`
- single range → `ColLt` / `ColLe` / `ColGt` / `ColGe`
- `AND` trees are left-associative, mirroring the pre-Slice-1 structured normalization (`[P1 AND P2 AND P3]` → `And{Left: And{Left: P1, Right: P2}, Right: P3}`)

Still rejected in protocol v1 (each as `SubscriptionError`):
- `OR` expressions in the top-level subscription predicate (evaluator has no placement tier; Tier-3 fallback is safe but intentionally not exposed via Subscribe)
- joins / references to more than one table
- aggregates, projections other than `SELECT *`, ordering, limits

`SubscribeSingle` validation MUST fail with `SubscriptionError` if:
- `query_string` fails to parse as SQL
- `table_name` (after parse) does not exist
- any referenced column does not exist on that table
- the same `query_id` is already active **or pending** on the connection
- the expression shape is not part of the v1 subset

**Design decision — SQL in v1.** The structured-predicate shape was rejected once `SubscribeMulti` required carrying multiple queries under one envelope: a SQL string is a natural multi-element payload and gives Shunter clients one query language across subscribe and one-off reads. The original structured-predicate design is now superseded by the SQL wire surface. See `TECH-DEBT.md::OI-002` and the parser doc comments on `SubscribeSingleMsg.QueryString` / `SubscribeMultiMsg.QueryStrings` / `OneOffQueryMsg.QueryString` in `protocol/client_messages.go` for current status.

**Design decision — still single-table in v1.** Equality / range predicates on one table remain the common hot-path subscription shape and map cleanly onto SPEC-004's pruning indexes. Range and join subscriptions remain part of the evaluator's internal model, but joins are still not exposed on the public wire protocol in v1.

#### 7.1b SubscribeMulti

Register a query-set under a single `query_id`. This is the multi-query counterpart to `SubscribeSingle`.

```
tag: 5
request_id:   uint32 LE
query_id:     uint32 LE       — one QueryID covers the whole set
query_count:  uint32 LE
query_strings: [query_count] × string    — each a SQL query per §7.1.1
```

**Response:** `SubscribeMultiApplied` or `SubscriptionError`

Each string is parsed and validated independently with `query/sql.Parse`; any failure aborts the set and emits `SubscriptionError`. The applied response (§8.10) carries the merged initial snapshot as one `[]SubscriptionUpdate` with per-query/table entries stamped with the client `QueryID`; manager-internal `SubscriptionID` values stay below the protocol boundary.

### 7.2 UnsubscribeSingle

Remove a single-query subscription.

```
tag: 2
request_id:   uint32 LE
query_id:     uint32 LE
send_dropped: uint8        — 0 = no dropped rows; 1 = include current rows in response
```

**Response:** `UnsubscribeSingleApplied`

#### 7.2b UnsubscribeMulti

Drop every query registered under the given `query_id` in one call. This is the set-level counterpart to `UnsubscribeSingle`.

```
tag: 6
request_id: uint32 LE
query_id:   uint32 LE
```

**Response:** `UnsubscribeMultiApplied` (§8.11)

### 7.3 CallReducer

Invoke a named reducer.

```
tag: 3
request_id:    uint32 LE
reducer_name:  string           — matches a registered reducer name
args:          bytes            — BSATN-encoded ProductValue of reducer arguments
flags:         uint8            — CallReducerFlags byte (see below)
```

**Response:** the caller receives the heavy `TransactionUpdate` (§8.5) carrying the reducer's `UpdateStatus`. No separate `ReducerCallResult` envelope exists on the wire — tag 7 is reserved (see §6, §8.7, and `docs/parity-decisions.md#outcome-model`).

The client is responsible for encoding `args` as a `ProductValue` matching the reducer's declared parameter types. Type mismatch is detected by the executor and returned as a heavy `TransactionUpdate` with `Status = StatusFailed{Error}`.

**CallReducerFlags (uint8):**

| Value | Name | Meaning |
|---|---|---|
| 0 | `FullUpdate` | Caller always receives the heavy `TransactionUpdate` on success / failure / OOE. Default. |
| 1 | `NoSuccessNotify` | On `StatusCommitted` the caller is not echoed. Failure envelopes (`StatusFailed`, `StatusOutOfEnergy`) are still delivered so the caller observes non-success outcomes. |

Any other value is rejected as `ErrMalformedMessage`.

### 7.4 OneOffQuery

Execute a read-only SQL query that returns current matching rows, without establishing an ongoing subscription. The current v1 wire uses a SQL string and an opaque client-chosen `message_id` byte slice for correlation.

```
tag: 4
message_id:   bytes     — opaque client-chosen correlator (matches reference `OneOffQuery.message_id`)
query_string: string    — SQL query per §7.1.1
```

**Response:** `OneOffQueryResponse` (§8.6) carrying the same `message_id`.

The executor runs a read-only query against `CommittedState.Snapshot()` directly. This read is not atomic with subscription registration because it does not register subscription state; it only returns a point-in-time result from committed state.

Implementation status: the one-off SELECT surface is intentionally broader than the subscription SQL subset and is tracked under `TECH-DEBT.md::OI-002`. Current query-only widenings include `LIMIT`, column projections, `COUNT(*) [AS] alias`, unindexed two-table joins, cross-join `WHERE` column equality, and the bounded cross-join `WHERE` equality-plus-one-column-literal-filter shape; subscriptions still reject cross-join `WHERE` before executor registration.

---

## 8. Server→Client Messages

### 8.1 IdentityToken

First message sent after WebSocket opens (before any client message is processed).

```
tag: 1
identity:      bytes (32)     — client's Identity in canonical 32-byte wire form
token:         string          — JWT for reconnection; client should persist
connection_id: bytes (16)     — server-assigned or client-provided connection ID
```

### 8.2 SubscribeSingleApplied

Single-query subscription registered successfully. Contains all currently matching rows.

```
tag: 2
request_id:                         uint32 LE
total_host_execution_duration_us:   uint64 LE     — server-measured wall time
query_id:                           uint32 LE
table_name:                         string
rows:                               RowList       — all rows matching the query at subscribe time
```

`query_id` identifies one logical subscription on the connection.

The rows in `SubscribeSingleApplied` represent a consistent snapshot. They are the starting state the client should use to populate its local cache.

The `SubscribeMulti` counterpart is `SubscribeMultiApplied` (§8.10).

### 8.3 UnsubscribeSingleApplied

Single-query subscription removed.

```
tag: 3
request_id:                         uint32 LE
total_host_execution_duration_us:   uint64 LE
query_id:                           uint32 LE
has_rows:                           uint8               — 0 = no rows; 1 = rows follow
rows:                               RowList (if has_rows = 1)   — rows that were in the result set at unsubscribe time
```

The `UnsubscribeMulti` counterpart is `UnsubscribeMultiApplied` (§8.11).

### 8.4 SubscriptionError

Subscription failed. The subscription identified by `query_id` (if present) is now dead.

```
tag: 4
total_host_execution_duration_us: uint64 LE       — server-measured wall time
request_id: Optional<uint32 LE>    — echoes Subscribe request_id when still known
query_id:   Optional<uint32 LE>    — identifies the affected subscription when known
table_id:   Optional<TableID>      — narrows the drop scope to one return-table family when known
error:      string                 — diagnostic message; not machine-parseable
```

`Optional<T>` is encoded as `uint8 present + T` (0 = absent, 1 = present; `T` omitted when absent). Wire-optional shape mirrors the reference envelope and lets the server drop one return-table family under a multi-query `query_id` without tearing down the whole set.

On receiving this, the client must discard all cached rows for the affected `query_id` (or the `(query_id, table_id)` sub-family when `table_id` is present). A bare `query_id` may be reused immediately.

**Go↔wire mapping.** Subscribe/unsubscribe admission paths build this envelope with the client-chosen `query_id`. The subscription evaluator's post-commit `SubscriptionError` Go value (SPEC-004 §10.2) still carries a typed internal `SubscriptionID` plus diagnostic fields (`QueryHash`, `Predicate`) for server logging, but the fan-out adapter does not expose those internals: evaluation-origin errors are emitted with absent `request_id`, absent `query_id`, absent `table_id`, and `error = Message`.

**Absent-`request_id` semantics.** An absent `request_id` is a spontaneous failure where the server no longer has any originating subscribe request identity to report. A present `request_id` MUST echo the `request_id` of the triggering `SubscribeSingle` / `SubscribeMulti`, including post-register reevaluation failures when the subscription manager still retains that metadata. Clients that omit `request_id` on subscribe accept that correlated failures and genuinely uncorrelated failures are indistinguishable; recommend setting `request_id >= 1` for robust client-side correlation.

### 8.5 TransactionUpdate (heavy, caller-bound)

The current v1 outcome model makes `TransactionUpdate` the **single caller-bound envelope** for every reducer outcome — success, failure, and a reserved `OutOfEnergy` arm. Non-callers whose subscribed rows are touched receive `TransactionUpdateLight` (§8.8) instead. Non-callers with no matching rows receive nothing.

```
tag: 5
status:                         UpdateStatus            — three-arm tagged union (see below)
caller_identity:                bytes (32)              — the caller's Identity
caller_connection_id:           bytes (16)              — the caller's ConnectionID
reducer_call:                   ReducerCallInfo         — see below
timestamp:                      int64 LE                — server-captured reducer dispatch time (Unix epoch microseconds)
energy_quanta_used:             bytes (16)              — reserved u128 LE; always 0 in v1 (no billing/quota model)
total_host_execution_duration:  int64 LE                — measured reducer wall time, microseconds
```

**`UpdateStatus` (tagged union):**

```
arm tag (uint8):
  0 = Committed{update: []SubscriptionUpdate}       — caller's visible row-delta slice (may be empty)
  1 = Failed{error: string}                         — reducer-side failure or pre-commit rejection
  2 = OutOfEnergy{}                                 — reserved; never emitted by the v1 executor
```

**`ReducerCallInfo`:**

```
reducer_name: string
reducer_id:   uint32 LE     — monotonically assigned at ReducerRegistry.Register (SPEC-006)
args:         bytes         — BSATN-encoded ProductValue echoed from CallReducer
request_id:   uint32 LE     — echoes CallReducer.request_id
```

`SubscriptionUpdate` Go struct is defined in SPEC-004 §10.2. The wire layout of each entry in `Committed.update` (and in §8.8 `TransactionUpdateLight.update`) is:

```
SubscriptionUpdate:
  query_id:        uint32 LE
  table_name:      string
  inserts:         RowList    — rows newly entering the result set
  deletes:         RowList    — rows leaving the result set
```

Mapping from the Go value to the wire:
- `query_id uint32 LE` ← `SubscriptionUpdate.QueryID`
- `table_name string` ← `SubscriptionUpdate.TableName`
- `inserts RowList` ← encoded from `SubscriptionUpdate.Inserts []ProductValue`
- `deletes RowList` ← encoded from `SubscriptionUpdate.Deletes []ProductValue`

This wire form is intentionally single-table per entry. Joined `SubscriptionUpdate` values never appear on the wire; the evaluator's internal `TableID` anchor for joins remains an internal-only concern.

`inserts` and `deletes` are defined in terms of the subscription result set, not physical storage operations:
- `inserts`: rows newly entering the subscription result set in this commit
- `deletes`: rows leaving the subscription result set in this commit

For a row update, treat the old row version and new row version separately:
- old row matches, new row matches → encode as `delete(old)` plus `insert(new)`
- old row matches, new row does not match → encode as `delete(old)` only
- old row does not match, new row matches → encode as `insert(new)` only
- neither version matches → omit the row entirely

A single `Committed.update` (or `TransactionUpdateLight.update`) may contain entries for multiple client query IDs or multiple query/table entries under the same query ID. A subscription query with no changes in a given transaction does not appear.

**Important:** If the same row matches multiple subscriptions, it appears in the update for each matching subscription independently. There is no deduplication across subscriptions.

**`tx_id` exposure.** The caller's commit TxID is **not** a standalone wire field on `TransactionUpdate` in v1. Clients recover commit identity through their `SubscribeSingleApplied` / `SubscribeMultiApplied` seeding and successive deltas; v1 provides no `resume_from_tx_id` mechanism. A client that disconnects must re-subscribe and rebuild state from a fresh `SubscribeSingleApplied` / `SubscribeMultiApplied`. (For rejection paths where no transaction was ever opened, the executor emits a synthetic heavy `TransactionUpdate` with `Status = Failed{Error}` and `ReducerCallInfo` populated from the request; the "no committed transaction" signal is implicit in the `Failed` arm. See `docs/parity-decisions.md#outcome-model`.)

**Dispatch rule (repeated from the decision doc):**
- Caller always receives this heavy `TransactionUpdate` on `Committed` / `Failed` / `OutOfEnergy`, subject to the `CallReducerFlags::NoSuccessNotify` opt-out on `Committed` (§7.3).
- Non-callers with row-touches receive `TransactionUpdateLight` (§8.8).
- Non-callers with no row-touches receive nothing.

**Shunter decision — no energy economy.** `OutOfEnergy` is present in the union for wire stability but is never emitted by the v1 executor. `energy_quanta_used` is permanently `0` unless Shunter later adds its own local quota system. SpacetimeDB-style hosted billing/metering is not a Shunter product goal.

**Shunter decision — failure-arm collapse.** Shunter's internal executor distinguishes `failed_user`, `failed_panic`, and `not_found` reducer outcomes. v1 collapses all three onto `Failed{Error}` on the wire, retaining the distinguishing information in the error string. This should change only if Shunter clients need a more machine-readable failure contract.

### 8.6 OneOffQueryResponse

Response to `OneOffQuery`.

```
tag: 6
message_id: bytes                  — echoes OneOffQueryMsg.MessageID
error:      Optional<string>       — absent on success, present on failure
tables:     []OneOffTable          — empty on failure
total_host_execution_duration: int64 LE — server-measured wall time in microseconds
```

`OneOffTable` is:

```
table_name: string
rows:       RowList
```

### 8.7 (RESERVED) — formerly ReducerCallResult

**Tag 7 is reserved.** The former `ReducerCallResult` envelope was removed from the wire surface. The caller outcome is now carried by the heavy `TransactionUpdate` (§8.5); non-callers receive `TransactionUpdateLight` (§8.8). The tag byte is held reserved so it cannot be silently re-allocated if a future contributor reintroduces a separate caller envelope. A server decoder MUST reject tag 7 with `ErrUnknownMessageTag`; a client decoder MUST treat tag 7 as a fatal protocol error (§3.2).

Authoritative pins:
- `protocol/parity_message_family_test.go::TestPhase15TagReducerCallResultReserved`
- `protocol/server_messages_test.go::TestTagReducerCallResultIsReserved`

### 8.8 TransactionUpdateLight

Delivered to non-caller connections whose subscribed rows were touched by a commit. Delta-only — no caller identity, reducer call info, timestamp, or duration fields.

```
tag: 8
request_id: uint32 LE                    — echoes the originating CallReducer.request_id when known; 0 for non-reducer commits
update:     []SubscriptionUpdate         — same per-entry shape as §8.5 Committed.update
```

If a non-caller has no matching row-touches for a given commit, no `TransactionUpdateLight` is sent to that connection. This envelope never carries reducer-outcome metadata; the caller is the sole recipient of the heavy `TransactionUpdate`.

### 8.9 (reserved for future use)

### 8.10 SubscribeMultiApplied

Response to `SubscribeMulti` (§7.1b). Carries the merged initial snapshot for all queries registered under one `query_id`.

```
tag: 9
request_id:                         uint32 LE
total_host_execution_duration_us:   uint64 LE
query_id:                           uint32 LE
update:                             []SubscriptionUpdate    — one entry per emitted query/table family, with query_id set and Inserts populated
```

Reference: `SubscribeMultiApplied` at `reference/SpacetimeDB/crates/client-api-messages/src/websocket/v1.rs`.

### 8.11 UnsubscribeMultiApplied

Response to `UnsubscribeMulti` (§7.2b). Carries the Deletes-populated entries for rows still live at unsubscribe time.

```
tag: 10
request_id:                         uint32 LE
total_host_execution_duration_us:   uint64 LE
query_id:                           uint32 LE
update:                             []SubscriptionUpdate    — one entry per emitted query/table family, with query_id set and Deletes populated
```

---

## 9. Subscription Semantics

### 9.1 Subscription State Machine

```
[not subscribed]
    ↓ SubscribeSingle / SubscribeMulti(query_id)
[pending: server validating + evaluating]
    ↓ SubscribeSingleApplied / SubscribeMultiApplied
[active: receiving TransactionUpdate / TransactionUpdateLight]
    ↓ UnsubscribeSingle / UnsubscribeMulti(query_id)
[not subscribed]

[pending or active]
    ↓ SubscriptionError
[not subscribed]

[pending or active]
    ↓ disconnect
[not subscribed]
```

State rules (see `docs/adr/2026-04-19-subscription-admission-model.md` for the landed manager-authoritative Shape 1 rationale):
- a `query_id` is reserved as soon as `SubscribeSingle` / `SubscribeMulti` is accepted for processing; a second subscribe with the same ID while pending or active MUST fail with `SubscriptionError` (manager-authoritative: rejected by the subscription manager's `(ConnID, QueryID)` registry, not a protocol-layer tracker)
- `UnsubscribeSingle` / `UnsubscribeMulti` for a `query_id` that is not currently registered under the connection's live `(ConnID, QueryID)` set registry returns `ErrSubscriptionNotFound`
- if the client disconnects while a subscription is pending, the registration result is discarded and the subscription never becomes active: the executor invokes the registration `Reply` closure synchronously, but the closure's `connOnlySender` short-circuits on a closed `<-conn.closed` channel and returns `ErrConnNotFound`; no Applied envelope ever reaches `OutboundCh`
- once the Applied envelope (`SubscribeSingleApplied` / `SubscribeMultiApplied`) is enqueued on the connection's outbound queue during registration, any subsequent `TransactionUpdate` / `TransactionUpdateLight` for that `query_id` is guaranteed to be delivered after it. The ordering is preserved by the per-connection `OutboundCh` FIFO: the executor's register handler synchronously enqueues the Applied envelope before returning, and any later fan-out delivery on the same executor goroutine enqueues strictly after it. No separate tracker state machine or activation gate is involved, so no stale activation can survive a disconnect
- the Unsubscribe Applied envelope is a confirmation message for a removal that has already been applied in the executor / subscription-manager pipeline; there is no separate long-lived "pending removal" state in v1

### 9.2 Client-Maintained State

The client is responsible for maintaining a local cache per subscription:
- On `SubscribeSingleApplied` / `SubscribeMultiApplied`: populate cache with initial rows
- On `TransactionUpdate.Status::Committed.update` / `TransactionUpdateLight.update` inserts: add rows to cache
- On deletes in either envelope: remove rows from cache
- On `SubscriptionError`: discard cache entirely (or the `(query_id, table_id)` sub-family when `table_id` is present)
- On `UnsubscribeSingleApplied` / `UnsubscribeMultiApplied`: discard cache

The cache at any point in time should equal the result of the subscription query run against committed state after the last received delta envelope (heavy or light).

### 9.3 Multiple Subscriptions

A client may have multiple active subscriptions simultaneously. Each has a unique `query_id`. They are independent: a single commit's delta for this client — whether delivered as the caller's heavy `TransactionUpdate.Status::Committed.update` or as a non-caller `TransactionUpdateLight.update` — may contain entries for multiple subscriptions, or may contain entries for only some.

### 9.4 Subscription During Active Transaction

The executor serializes subscription registration with commits (SPEC-003 §2.5). Subscribe commands go through the executor's inbox. The Applied response is consistent: the initial rows match committed state as of the moment the subscription was registered, and the first delta envelope this client receives for this subscription will contain only changes from transactions that committed after registration.

Ordering guarantee on one connection:
- `SubscribeSingleApplied` / `SubscribeMultiApplied(query_id)` MUST be delivered before any `TransactionUpdate` (heavy) or `TransactionUpdateLight` that references that `query_id`
- for a reducer call made by this same connection, the heavy `TransactionUpdate` (§8.5) carrying the caller's `UpdateStatus` replaces any separate `TransactionUpdateLight` on the same commit; the caller is never double-delivered
- if the client disconnects or removes the `(ConnID, QueryID)` entry before a queued Applied envelope is delivered, that later registration result is discarded rather than activating stale state

---

## 10. Backpressure

### 10.1 Server → Client

The server buffers outgoing messages per-client up to `OutgoingBufferMessages` (default: `16 * 1024` messages). If enqueueing the **next** outbound message would exceed that limit, the server MUST:
1. leave already-queued messages untouched
2. enqueue or send a Close frame if possible (`1008`, reason: `"send buffer full"`)
3. stop accepting further outbound application messages for that connection
4. close the connection

The overflow-causing application message is not delivered. The client must reconnect.

**Design decision:** Disconnect on buffer overflow rather than drop messages. Dropped deltas would corrupt the client's local cache (it would be missing rows). Disconnection is recoverable: the client reconnects and re-subscribes, rebuilding the cache from a fresh `SubscribeSingleApplied` / `SubscribeMultiApplied`.

### 10.2 Client → Server

The server maintains a per-connection incoming message queue with capacity `IncomingQueueMessages` (default: 64). If receiving the **next** client message would exceed that queue limit, the server closes the connection with Close code `1008`, reason: `"too many requests"`. The overflow-causing message is not processed.

---

## 11. Disconnection

### 11.1 Clean Close

Either side may send a WebSocket Close frame. The receiver echoes a Close frame. The connection is then closed. Close codes follow RFC 6455.

Server-initiated closes:
- `1000` (Normal Closure): graceful engine shutdown
- `1008` (Policy Violation): authentication failure, buffer overflow (`"send buffer full"`), too many requests (`"too many requests"`), OnConnect rejection
- `1011` (Internal Error): unexpected server error

`Conn.OutboundCh` MUST NOT be closed as part of disconnect. Shutdown is signaled through `Conn.closed`; senders check that signal before enqueueing. This avoids the send-on-closed-channel race between concurrent `Send(...)` and disconnect. The writer goroutine exits on `Conn.closed` and may best-effort flush already queued frames before the underlying WebSocket closes.

### 11.2 Network Failure

If the underlying TCP connection drops without a Close frame, the server detects this via the Ping timeout (idle timeout of 30 s). All subscriptions and the `sys_clients` row are cleaned up, and `OnDisconnect` runs.

### 11.3 Reconnection

Clients should reconnect with exponential backoff. On reconnect:
1. Present the same `token` from `IdentityToken` to preserve `Identity`
2. Re-subscribe to all desired subscriptions (server has no subscription state from previous connection)
3. There is no `tx_id` wire field on post-commit envelopes in v1 (see §8.5); clients cannot use a commit ID to detect changes since disconnect. v1 has no gap-fill mechanism — clients re-fetch initial state via `SubscribeSingleApplied` / `SubscribeMultiApplied`.

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
    // This is a Shunter-side teardown bound, not a guarantee that the transport
    // library can always force-close the underlying TCP socket at that exact
    // deadline once a Close handshake is already in flight.
    CloseHandshakeTimeout time.Duration

    // OutgoingBufferMessages: max queued outgoing messages per client.
    // Default: 16 * 1024.
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
- `RegisterSubscriptionSetCmd` — for `SubscribeSingleMsg` / `SubscribeMultiMsg` messages
- `UnregisterSubscriptionSetCmd` — for `UnsubscribeSingleMsg` / `UnsubscribeMultiMsg` messages
- `DisconnectClientSubscriptionsCmd` — on client disconnect

The executor sends protocol-originated reducer outcomes back through `CallReducerCmd.ProtocolResponseCh`. The former standalone `ReducerCallResult` Go shape was replaced by the heavy `TransactionUpdate` envelope (§8.5); the response carries `UpdateStatus`, `ReducerCallInfo`, and caller metadata so the fan-out integration can route the caller's delta through the heavy envelope and the non-callers' deltas through `TransactionUpdateLight` (§8.8).

```go
type ExecutorInbox interface {
    OnConnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error
    OnDisconnect(ctx context.Context, connID types.ConnectionID, identity types.Identity) error
    DisconnectClientSubscriptions(ctx context.Context, connID types.ConnectionID) error
    RegisterSubscriptionSet(ctx context.Context, req RegisterSubscriptionSetRequest) error
    UnregisterSubscriptionSet(ctx context.Context, req UnregisterSubscriptionSetRequest) error
    CallReducer(ctx context.Context, req CallReducerRequest) error
}
```

### SPEC-004 (Subscription Evaluator)

After each commit, the subscription evaluator does **not** write to sockets directly. Instead, it sends a `FanOutMessage` to the fan-out worker. The `FanOutMessage` Go shape (carrying `TxDurable`, `Fanout`, `Errors`, `CallerConnID`, `CallerResult`) is declared in SPEC-004 §8.1; SPEC-005 does not redeclare the struct. `CommitFanout` is defined in SPEC-004 §7 as `map[ConnectionID][]SubscriptionUpdate`. `SubscriptionUpdate` is defined in SPEC-004 §10.2.

Delivery contract:
1. evaluator computes `CommitFanout` for the committed transaction and sends `FanOutMessage{TxDurable, Fanout, Errors, CallerConnID, CallerResult}` to the fan-out worker inbox
2. the fan-out worker treats each `CommitFanout[connID]` entry as the `[]SubscriptionUpdate` payload for one post-commit envelope
3. any `SubscriptionError` entries in `FanOutMessage.Errors` are delivered before normal updates for the same batch
4. if the commit originated from `CallReducer`, the fan-out/protocol integration routes the caller connection's update slice into `TransactionUpdate.Status::Committed.update` (§8.5); on `Failed` / `OutOfEnergy` the caller receives the heavy envelope with an empty `update`. The caller is not also delivered a standalone `TransactionUpdateLight`.
5. all remaining connection entries are delivered as `TransactionUpdateLight` messages (§8.8)
6. protocol layer serializes, optionally compresses, and enqueues those messages to websocket connections

The fan-out worker constructs the heavy `TransactionUpdate` from the caller's `CommitFanout` slice plus the reducer-outcome metadata (`UpdateStatus`, `CallerIdentity`, `CallerConnectionID`, `ReducerCall`, `Timestamp`, `TotalHostExecutionDuration`) returned by the executor on `CallReducerCmd.ResponseCh`. Non-caller entries become `TransactionUpdateLight{RequestID, Update}`.

Protocol v1 exposes no wire-level confirmed-read flag. Non-caller fan-out delivery defaults to confirmed reads: the fan-out worker waits on `TxDurable` before sending `SubscriptionError` or `TransactionUpdateLight` to a connection unless an internal fast-read policy opts that connection out. Protocol-originated caller-heavy `TransactionUpdate` responses are emitted after commit and synchronous subscription evaluation, but before fsync completion; clients that need crash-survivable acknowledgement must treat the commit log durability boundary as a future explicit feature.

```go
type ClientSender interface {
    // Send encodes msg and enqueues the frame on the connection's
    // outbound channel. Used for direct server→client response
    // messages that do not have a dedicated typed method:
    // SubscribeSingleApplied, SubscribeMultiApplied,
    // UnsubscribeSingleApplied, UnsubscribeMultiApplied,
    // SubscriptionError, OneOffQueryResponse. Returns
    // ErrClientBufferFull if the client's outgoing buffer is full.
    Send(connID ConnectionID, msg any) error

    // SendTransactionUpdate queues the heavy caller-bound envelope
    // (§8.5). Used for CallReducer outcomes; not used for non-caller
    // deltas. Returns ErrClientBufferFull if the client's outgoing
    // buffer is full.
    SendTransactionUpdate(connID ConnectionID, update *TransactionUpdate) error

    // SendTransactionUpdateLight queues a non-caller delta-only envelope
    // (§8.8). Returns ErrClientBufferFull if the client's outgoing
    // buffer is full.
    SendTransactionUpdateLight(connID ConnectionID, update *TransactionUpdateLight) error
}
```

**Relationship to SPEC-004 `FanOutSender`.** SPEC-004 §8.1 declares the narrower seam that the subscription fan-out worker talks to. The protocol layer satisfies that contract with a thin adapter (`FanOutSenderAdapter`) over `ClientSender`, routing `SendSubscriptionError` through the generic `Send(connID, msg)` path with a protocol-wire `SubscriptionError` value (§8.4). The two interfaces are intentionally distinct: `ClientSender` is the cross-subsystem delivery surface owned by the protocol package; `FanOutSender` is the subscription-side seam that hides protocol-package concerns from the subscription package. Delivery errors (`ErrClientBufferFull`, `ErrConnNotFound`) are mapped by the adapter to subscription-layer sentinels (`ErrSendBufferFull`, `ErrSendConnGone`) so the fan-out worker reacts without importing protocol types.

### SPEC-001 (In-Memory Store)

`OneOffQuery` uses `CommittedState.Snapshot()` directly (read-only, bypassing subscription registration ordering) to serve the query result. This is safe because `OneOffQuery` does not create persistent subscription state and therefore does not need atomic registration semantics.

### SPEC-006 (Schema)

Subscribe and OneOffQuery handlers (Story 4.2 / 4.4) need to resolve table names to IDs and validate column references before forwarding requests to the executor. They consume the `SchemaLookup` interface declared in SPEC-006 §7 — specifically `TableByName(name) (TableID, *TableSchema, bool)` and the column-metadata methods. The protocol package may declare its own narrower local interface for testing, but the canonical type lives in SPEC-006; `*SchemaRegistry` satisfies it directly. The handler receives the schema reference at upgrade time (see Story 3.x `UpgradeContext.Schema`); the registry is immutable for the engine's lifetime per SPEC-006 §5.1 freeze.

---

## 14. Error Catalog

| Error | Condition |
|---|---|
| `ErrUnknownMessageTag` | Client sent a message with an unrecognized tag |
| `ErrUnknownCompressionTag` | Server or client observed a compression tag not defined by this protocol version |
| `ErrMalformedMessage` | Message body could not be decoded |
| `ErrDecompressionFailed` | Compressed frame could not be decompressed |
| `ErrQueryIDAlreadyLive` | Subscribe with a `query_id` already active or pending on the connection |
| `ErrSubscriptionNotFound` | Unsubscribe for a `query_id` not active on the connection |
| `ErrReducerNotFound` | CallReducer named a reducer not registered |
| `ErrLifecycleReducer` | CallReducer named a lifecycle reducer (OnConnect/OnDisconnect) |
| `ErrClientBufferFull` | Server cannot send to this client (triggers disconnect) |
| `ErrConnNotFound` | Delivery target connection disappeared before send |
| `ErrTextFrameReceived` | Client sent a WebSocket text frame |
| `ErrMaxMessageSize` | Incoming frame exceeded configured read limit |
| `ErrTooManyRequests` | Incoming in-flight queue would exceed `IncomingQueueMessages` |
| `ErrZeroConnectionID` | Client-supplied connection_id is all zeros |

---

## 15. Open Questions

1. **Query language evolution.** *Closed for v1.* v1 carries SQL query strings on `SubscribeSingle` / `SubscribeMulti` / `OneOffQuery` (§7.1.1). The supported SQL subset is single-table equality/range predicates with `AND`; OR/join/aggregation remain rejected. Further language widening should be driven by Shunter client needs.

2. **Gap-fill / resume protocol.** v1 has no commit-id wire field on post-commit envelopes and no way to request missed deltas. Clients must re-subscribe and accept a full `SubscribeSingleApplied` / `SubscribeMultiApplied`. Should the server support `resume_from_tx_id` for short disconnections? This requires the server to retain a short delta buffer and expose a commit-id. Deferred to v2.

3. **Subscription for all rows (no predicate) vs table watch.** A subscription with no `WHERE` clause returns all table rows. For large tables this may be expensive. Should there be an explicit `WatchTable` message, or is the no-predicate SQL subscription sufficient? No distinction needed in v1 — document the performance implication clearly.

4. **Identity type spec ownership.** *Closed.* `Identity` is declared in `types/types.go` (SPEC-001 §2.4 owns the contract); derivation helpers (`DeriveIdentity`, `Hex`, `ParseIdentityHex`, `IsZero`) live in `types/identity.go`. SPEC-005 consumes the declaration and does not redeclare the type.

5. **Anonymous token policy defaults.** If anonymous minting mode is enabled, what are the default issuer, audience, and expiry settings? This spec requires them to be configured/documented but does not prescribe defaults.

---

## 16. Reference-Informed Shunter Decisions

- **Protocol identifier:** Shunter-owned clients should use `v1.bsatn.shunter` (§2.2). Current code still admits `v1.bsatn.spacetimedb` for historical compatibility with earlier reference-comparison work, but SpacetimeDB client compatibility is not a product goal.
- **Compression envelope tags:** Shunter uses `0x00` = none, `0x01` = brotli (reserved, unsupported — `ErrBrotliUnsupported`), `0x02` = gzip (§3.3). Brotli should be implemented only if Shunter clients need it.
- **Outgoing backpressure limit:** v1 bounds each connection's outbound queue at `OutgoingBufferMessages` with default `16 * 1024` (§10.1, §12). Shunter disconnects lagging clients to keep memory bounded.
- **TransactionUpdate shape:** v1 uses a heavy/light envelope split (§8.5, §8.8). `energy_quanta_used` is stubbed to `0`, `OutOfEnergy` is never emitted, and `Failed` collapses Shunter's internal `failed_user`/`failed_panic`/`not_found` distinction onto one arm with the detail carried in the error string.
- **Subscription RPC surface:** v1 exposes `SubscribeSingle`, `SubscribeMulti`, `UnsubscribeSingle`, `UnsubscribeMulti`, `CallReducer`, and `OneOffQuery` (§6, §7). There is no legacy single `Subscribe` / `Unsubscribe` wire family or separate `QuerySetId` protocol family.
- **CallReducer wire shape:** v1 `CallReducer` carries `{request_id, reducer_name, args, flags}` (§7.3). The `flags` byte matches the reference `CallReducerFlags` (FullUpdate / NoSuccessNotify); no other flag values are defined in v1.
- **OneOffQuery language:** v1 `OneOffQuery` uses the same SQL subset as `SubscribeSingle` / `SubscribeMulti` (§7.4 / §7.1.1), giving Shunter clients one read-query surface.
- **Close-code policy:** Shunter's documented close behavior includes `1000`, `1002`, `1008`, and `1011` with Shunter-specific reason strings for protocol/policy failures (§11.1). It does not try to mirror SpacetimeDB's full close-code/reason matrix.
- **Energy model:** v1 has no energy subsystem. `TransactionUpdate.energy_quanta_used` is reserved and always `0`; `UpdateStatus::OutOfEnergy` is present in the tagged union for wire stability but is never emitted (§8.5). SpacetimeDB-style hosted billing/metering is not a Shunter product goal.
- **ConnectionID reuse semantics:** reusing `connection_id` on reconnect is only a client hint for future resume features; it has no server-side resume semantics in v1 (§2.3, §11.3).
- **Reserved tag 7:** the former `ReducerCallResult` tag byte is held reserved rather than reused (§6, §8.7). A future contributor reintroducing a separate caller envelope MUST pick a fresh tag, not reclaim 7.

---

## 17. Verification

| Test | What it verifies |
|---|---|
| Connect, receive IdentityToken | Connection establishment |
| Connect with invalid JWT → 401 | Authentication rejection |
| Connect with `connection_id = 0x00...00` → 400 | Zero connection_id rejection |
| SubscribeSingle, receive SubscribeSingleApplied with correct rows | Subscription registration and initial state |
| SubscribeMulti, receive SubscribeMultiApplied with merged initial rows | Multi-query subscription registration |
| SubscribeSingle / SubscribeMulti with duplicate `query_id` while pending or active → SubscriptionError | Subscription ID reservation rules |
| Non-caller: subscribe, another client's reducer inserts matching rows → TransactionUpdateLight | Non-caller delta delivery |
| Caller: CallReducer success with active subscriptions → heavy TransactionUpdate carrying `Status::Committed{update}` | Caller delta delivery |
| Delete rows via reducer, verify deletes arm of the appropriate envelope | Delete delta delivery |
| Update row so old+new both match predicate → delete(old)+insert(new) | In-place update delta semantics |
| Update row so it enters predicate → insert only | Moved-into-range semantics |
| Update row so it leaves predicate → delete only | Moved-out-of-range semantics |
| Insert+delete same row in one reducer → no delta entry for that row | Net-effect semantics |
| Two subscriptions, one commit affecting both → one delivery envelope with two SubscriptionUpdate entries | Multi-subscription delta |
| UnsubscribeSingle with `send_dropped=1`, verify rows in UnsubscribeSingleApplied | Dropped rows on unsubscribe |
| SubscribeSingle with invalid / unparseable SQL → SubscriptionError | Query parse validation |
| SubscribeSingle referencing unknown table → SubscriptionError | Table existence validation |
| CallReducer success with active subscriptions → heavy TransactionUpdate with caller metadata + non-empty `Committed.update` | Caller-metadata + caller delta |
| CallReducer success with no active subscriptions → heavy TransactionUpdate with empty `Committed.update` (no separate TransactionUpdateLight to caller) | Caller no-subscription semantics |
| CallReducer that returns a user error → heavy TransactionUpdate with `Status = Failed{Error}` | User-error outcome |
| CallReducer that panics → heavy TransactionUpdate with `Status = Failed{Error}` (panic text) | Panic outcome |
| CallReducer for unregistered reducer → heavy TransactionUpdate with `Status = Failed{Error}` (not-found) | Not-found outcome |
| CallReducer with `Flags = NoSuccessNotify` + commit → caller is not echoed on Committed | NoSuccessNotify opt-out |
| CallReducer + Subscribe to same table: caller receives heavy envelope only, non-callers receive light | No double-delivery to caller |
| Tag 7 on server→client frame → fatal decode error | Reserved-tag enforcement |
| OneOffQuery → correct rows returned from committed snapshot; `message_id` echoed | One-off read semantics |
| Unknown client message tag → protocol error close | Incoming framing validation |
| Unknown compression tag / invalid gzip payload → protocol error close | Compression envelope validation |
| Client sends > IncomingQueueMessages rapidly → connection closed | Incoming backpressure |
| Server can't deliver to slow client → connection closed (buffer full) | Outgoing backpressure |
| Idle connection for > IdleTimeout → connection closed | Idle timeout |
| Client disconnects cleanly → OnDisconnect fires, sys_clients row removed | Disconnect lifecycle |
| Client disconnects without Close frame → server detects via Ping timeout | Network failure detection |
| Reconnect with same token → same Identity in IdentityToken | Identity preservation |
| OnConnect reducer returns error → connection closed before IdentityToken | OnConnect rejection |
| Applied envelope for `query_id` always arrives before any delta envelope referencing that ID | Per-connection ordering guarantee |
