# SPEC-005 — Epic Decomposition

Source: [SPEC-005-protocol.md](./SPEC-005-protocol.md)

---

## Epic 1: Message Types & Wire Encoding

**Spec sections:** §3, §6, §7 (struct layouts), §8 (struct layouts)

Serialization foundation. Every other epic sends or receives these types.

**Scope:**
- Client→server and server→client message tag constants (§6)
- Go structs for all 11 message types (4 C2S, 7 S2C)
- `Query` and `Predicate` wire types for structured subscription predicates (§7.1.1)
- `RowList` encode/decode with per-row length prefix (§3.4)
- BSATN encode/decode for each message type (§3.1)
- Message framing: `[tag][body]` (§3.2)
- Compression envelope: `[compression][tag][compressed_body]` (§3.3)
- Unknown tag handling: close with protocol error `1002`

**Testable outcomes:**
- Round-trip encode/decode each of 11 message types
- RowList: encode N rows, decode back — row count and data match
- Compression: encode with gzip, decode — matches uncompressed original
- Uncompressed envelope (`0x00` prefix) round-trips correctly
- Unknown tag → `ErrUnknownMessageTag`
- Unknown compression tag → `ErrUnknownCompressionTag`
- Invalid gzip payload → `ErrDecompressionFailed`
- Malformed body → `ErrMalformedMessage`

**Dependencies:** None. This is the leaf.

**Cross-spec:** Uses BSATN encoding conventions from SPEC-002.

---

## Epic 2: Authentication & Identity

**Spec sections:** §4

HTTP-level authentication before WebSocket upgrade.

**Scope:**
- `Identity` type: 32-byte opaque identifier derived from `(iss, sub)` pair
- Canonical string form of Identity for wire encoding
- JWT validation: signature verification, expiry check, claim extraction (`sub`, `iss`, `aud`, `exp`, `iat`, `hex_identity`)
- `hex_identity` cross-check: if present, must match recomputed Identity
- Strict auth mode: require valid externally issued JWT
- Anonymous minting mode: generate fresh Identity, sign local JWT, return in `InitialConnection.token`
- Configurable signing key / JWKS at engine startup

**Testable outcomes:**
- Valid JWT with (iss, sub) → correct Identity derived
- Same (iss, sub) pair → always same Identity
- Different (iss, sub) → different Identity
- Expired token → rejection
- Invalid signature → rejection
- `hex_identity` present but mismatched → rejection
- Anonymous mode, no token → fresh Identity minted, JWT returned
- Anonymous mode, returning with minted token → same Identity preserved

**Dependencies:** None. This is the leaf.

---

## Epic 3: WebSocket Transport & Connection Lifecycle

**Spec sections:** §2, §5, §12

WebSocket plumbing, connection state, lifecycle hooks.

**Scope:**
- `ConnectionID` type: `[16]byte`, hex-encoded on wire, all-zeros rejected
- `ProtocolOptions` config struct (§12): `PingInterval`, `IdleTimeout`, `CloseHandshakeTimeout`, `OutgoingBufferMessages`, `IncomingQueueMessages`, `MaxMessageSize`
- WebSocket upgrade handler: authenticate via Epic 2, validate `Sec-WebSocket-Protocol: v1.bsatn.shunter`, parse `connection_id` and `compression` query params
- Per-connection state struct: Identity, ConnectionID, active subscription set, compression mode, outbound channel
- `InitialConnection` message: first message after upgrade, before any client messages processed
- `OnConnect` reducer hook (SPEC-003 §10.3): runs after upgrade, before `InitialConnection`; error → close with `1008`
- Keep-alive: server Ping every `PingInterval`, close on `IdleTimeout` with no data
- `OnDisconnect` reducer hook: runs on any close; subscriptions removed first; error logged, disconnect proceeds
- Close handshake timeout

**Testable outcomes:**
- Successful upgrade with valid protocol → WebSocket open, `InitialConnection` received
- Wrong protocol header → `400`
- Missing auth in strict mode → `401`
- `connection_id = 0x00...00` → `400`
- Client-supplied `connection_id` echoed in `InitialConnection`
- Absent `connection_id` → server generates random 16 bytes
- `OnConnect` success → `InitialConnection` sent
- `OnConnect` error → Close `1008`, no `InitialConnection`
- Ping timeout → connection closed
- `OnDisconnect` fires on close (clean or timeout)
- `OnDisconnect` error → logged, disconnect proceeds

**Dependencies:** Epic 1 (InitialConnection message encoding), Epic 2 (authentication during upgrade)

**Cross-spec:** SPEC-003 (executor: OnConnect/OnDisconnect lifecycle reducers)

---

## Epic 4: Client Message Dispatch

**Spec sections:** §7, §9.1

Parse incoming binary frames and route to subsystems.

**Scope:**
- Incoming frame reader: read binary frame, reject text frames with Close
- Tag dispatch loop: decode tag byte, route to handler, unknown tag → protocol error `1002`
- **Subscribe handler (§7.1):** decode `Subscribe`, validate `Query` (table exists, columns exist, no duplicate subscription_id, v1 predicate subset only), normalize predicates to SPEC-004 model (`AllRows` / `ColEq` / left-associative `And` tree), send `RegisterSubscriptionCmd` to executor inbox
- **Unsubscribe handler (§7.2):** decode `Unsubscribe`, validate subscription_id is active or pending, send `UnregisterSubscriptionCmd` to executor
- **CallReducer handler (§7.3):** decode `CallReducer`, reject lifecycle reducer names (`OnConnect`, `OnDisconnect`), send `CallReducerCmd` to executor inbox with `ResponseCh`
- **OneOffQuery handler (§7.4):** decode `OneOffQuery`, execute read-only query against `CommittedState.Snapshot()`, return `OneOffQueryResult`
- Per-connection subscription state machine (§9.1): track pending/active/removed states per `subscription_id`; `subscription_id` reserved on accept, reusable after error or unsubscribe

**Testable outcomes:**
- Valid Subscribe → `RegisterSubscriptionCmd` sent to executor
- Subscribe with nonexistent table → `SubscriptionError`
- Subscribe with nonexistent column → `SubscriptionError`
- Subscribe with duplicate active subscription_id → `SubscriptionError`
- Subscribe with duplicate pending subscription_id → `SubscriptionError`
- Multi-predicate normalization: `[P1, P2, P3]` → `And{And{P1, P2}, P3}`
- No predicates → `AllRows(table)`
- Range predicate → rejected (v1)
- CallReducer for `OnConnect` → `ErrLifecycleReducer`
- CallReducer for unknown reducer → `ErrReducerNotFound` via executor
- OneOffQuery → rows returned from committed snapshot
- Text WebSocket frame → Close frame
- Unknown tag → protocol error `1002`

**Dependencies:** Epic 1 (message decoding), Epic 3 (connection state, subscription tracking)

**Cross-spec:** SPEC-001 (`CommittedState.Snapshot()` for OneOffQuery), SPEC-003 (executor command inbox), SPEC-004 (predicate normalization model)

---

## Epic 5: Server Message Delivery

**Spec sections:** §8, §9.2–§9.4, §13

Outbound message construction and delivery to WebSocket connections.

**Scope:**
- `ClientSender` interface (§13): `SendTransactionUpdate(connID, *TransactionUpdate) error`, `SendReducerResult(connID, *ReducerCallResult) error`
- Per-connection outbound writer: serialize message, apply optional compression, write to WebSocket
- **SubscribeApplied** delivery: initial rows as RowList, consistent snapshot
- **UnsubscribeApplied** delivery: optionally include dropped rows (`send_dropped`)
- **SubscriptionError** delivery: diagnostic message, subscription_id now dead
- **OneOffQueryResult** delivery: success with RowList or error with message
- **TransactionUpdate** construction from `CommitFanout` (SPEC-004 §7): iterate `map[ConnectionID][]SubscriptionUpdate`, build `TransactionUpdate{TxID, Updates}`, send per connection
- **ReducerCallResult** with embedded `transaction_update`:
  - Caller connection's update slice diverted from standalone `TransactionUpdate` into `ReducerCallResult`
  - Caller MUST NOT receive separate `TransactionUpdate` for same `tx_id`
  - If `status != 0`, embedded update MUST be empty
- Per-connection ordering guarantee: `SubscribeApplied(id)` delivered before any `TransactionUpdate` referencing that `id`

**Testable outcomes:**
- SubscribeApplied contains correct initial rows
- TransactionUpdate: insert rows → appear in `inserts` RowList
- TransactionUpdate: delete rows → appear in `deletes` RowList
- Update row matching predicate → `delete(old)` + `insert(new)`
- Row enters predicate → `insert` only
- Row leaves predicate → `delete` only
- Insert+delete same row in one tx → no rows in TransactionUpdate
- Two subscriptions affected by one commit → single TransactionUpdate with two entries
- ReducerCallResult: committed reducer → embedded update with caller's matching deltas
- ReducerCallResult: no active subscriptions → empty embedded update, no separate TransactionUpdate
- ReducerCallResult: failed reducer → status set, empty embedded update
- Caller does NOT receive standalone TransactionUpdate for same tx_id
- SubscribeApplied always delivered before TransactionUpdate for that subscription_id
- Unsubscribe with `send_dropped=1` → UnsubscribeApplied includes current rows

**Dependencies:** Epic 1 (message encoding, RowList), Epic 3 (connection send channel), Epic 4 (subscription state for routing)

**Cross-spec:** SPEC-003 (ReducerCallResult metadata, TxID), SPEC-004 (CommitFanout, SubscriptionUpdate, FanOutMessage)

---

## Epic 6: Backpressure & Graceful Disconnect

**Spec sections:** §10, §11

Buffer management, disconnect semantics, reconnection.

**Scope:**
- **Outgoing backpressure (§10.1):** per-client buffer of `OutgoingBufferMessages` (default: 256); on overflow → enqueue Close `1008` "send buffer full", close connection; overflow-causing message not delivered
- **Incoming backpressure (§10.2):** per-connection queue of `IncomingQueueMessages` (default: 64); on overflow → Close `1008` "too many requests"; overflow-causing message not processed
- **Clean close (§11.1):** either side sends Close frame; server codes: `1000` (shutdown), `1008` (policy), `1011` (internal error); close handshake timeout
- **Network failure detection (§11.2):** TCP drop without Close → detected via Ping timeout (idle timeout 30s); cleanup subscriptions + sys_clients row, run `OnDisconnect`
- **Reconnection semantics (§11.3):** client presents same token → same Identity; re-subscribes from scratch (no server-side subscription state from previous connection); `tx_id` for diagnostics only, no gap-fill in v1

**Testable outcomes:**
- Slow client (full outgoing buffer) → connection closed with `1008`
- Overflow-causing message not delivered
- Already-queued messages left untouched before close
- Client flood (> IncomingQueueMessages) → connection closed with `1008`
- Overflow-causing incoming message not processed
- Clean close from client → server echoes Close
- Graceful server shutdown → `1000` Close to all clients
- TCP drop → connection cleaned up within IdleTimeout
- OnDisconnect fires after network failure detection
- Reconnect with same token → same Identity in InitialConnection
- Reconnect → must re-subscribe (no carryover subscriptions)

**Dependencies:** Epic 3 (connection management, keep-alive), Epic 5 (outgoing message pipeline)

**Cross-spec:** SPEC-003 (OnDisconnect lifecycle reducer, DisconnectClientSubscriptionsCmd)

---

## Dependency Graph

```
Epic 1: Message Types & Wire Encoding
  │
  ├── Epic 3: WebSocket Transport & Connection Lifecycle ← Epic 2
  │     │
  │     ├── Epic 4: Client Message Dispatch ← Epic 1
  │     │     │
  │     │     └── Epic 5: Server Message Delivery ← Epic 1, Epic 3
  │     │           │
  │     │           └── Epic 6: Backpressure & Graceful Disconnect ← Epic 3
  │     │
  │     └── Epic 6 (also depends on Epic 3 directly)
  │
  └── Epic 4 (also depends on Epic 1 directly)

Epic 2: Authentication & Identity
  └── Epic 3 (auth used during WebSocket upgrade)
```

Linearized build order: 1 → 2 → 3 → 4 → 5 → 6

## Error Types

Errors introduced where first needed:

| Error | Introduced in |
|---|---|
| `ErrUnknownMessageTag` | Epic 1 (tag decode) |
| `ErrUnknownCompressionTag` | Epic 1 (compression envelope) |
| `ErrMalformedMessage` | Epic 1 (body decode) |
| `ErrDecompressionFailed` | Epic 1 (gzip decompress) |
| `ErrZeroConnectionID` | Epic 3 (upgrade handler) |
| `ErrDuplicateSubscriptionID` | Epic 4 (subscribe handler) |
| `ErrSubscriptionNotFound` | Epic 4 (unsubscribe handler) |
| `ErrReducerNotFound` | Epic 4 (call reducer — surfaced from SPEC-003) |
| `ErrLifecycleReducer` | Epic 4 (call reducer — OnConnect/OnDisconnect blocked) |
| `ErrClientBufferFull` | Epic 6 (outgoing backpressure) |
