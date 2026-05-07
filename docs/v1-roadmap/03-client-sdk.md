# TypeScript Client SDK

Status: open, SDK contract drafted; runtime foundation started
Owner: unassigned
Scope: production-credible TypeScript client and generated bindings for Shunter
v1 applications.

## Goal

Ship a client experience that lets an application connect to Shunter, authenticate,
call reducers, run declared queries, subscribe to live views, maintain local
state, and survive reconnects without each app reimplementing protocol plumbing.

The v1 client does not need to be a framework-specific React SDK. It does need
to be stable enough that app authors can build reliable web or Node clients on
top of it.

## Current State

Shunter has contract export, TypeScript codegen, and a minimal checked-in
runtime foundation at `typescript/client` published in-repo as
`@shunter/client`. Generated TypeScript currently includes protocol metadata,
row interfaces, table maps, reducer and declared-read constants/helpers,
permission metadata, read-model metadata, value-kind mappings, module-scoped
aliases for the reducer result and raw declared-query result envelopes, and
runtime type imports for the shared SDK surface.

That is necessary but not sufficient for v1. The runtime package currently
defines protocol constants, protocol compatibility helpers, connection state
types, structured errors, a minimal `createShunterClient` WebSocket lifecycle
shell with initial `IdentityToken` decoding, a managed subscription handle
primitive with idempotent unsubscribe, and typed interfaces for reducer calls,
declared queries, declared views, and table subscriptions. It also has the
first raw-byte reducer request encoder and connected WebSocket send path for
the live v1 `CallReducerMsg` wire shape, plus minimal full-update
`TransactionUpdate` correlation for reducer success/failure. It also has a
raw-byte declared-query request path and `OneOffQueryResponse` correlation. It
also has a reducer result helper that wraps heavy `TransactionUpdate` frames in
a minimal committed/failed envelope while preserving the existing raw
`callReducer()` behavior. It also has raw declared-view subscribe request
encoding, `SubscribeMultiApplied` and `SubscriptionError` correlation, and an
idempotent unsubscribe send path for `UnsubscribeMulti`. It also has raw table
subscription request encoding,
`SubscribeSingleApplied` and `SubscriptionError` correlation, and an idempotent
unsubscribe send path for `UnsubscribeSingle`. Accepted subscriptions are now
registered for raw `TransactionUpdate` and `TransactionUpdateLight` callback
delivery, and unsubscribe promises wait for the matching
`UnsubscribeSingleApplied`/`UnsubscribeMultiApplied` or `SubscriptionError`.
It also has a raw RowList decoder for the live server row-batch payload shape,
with decoded raw per-row byte arrays on one-off query table results, table
initial rows, and optional table unsubscribe rows. Raw subscription updates
also expose optional decoded insert/delete row byte arrays when their payloads
decode as RowList envelopes. A raw declared-query result helper wraps
successful `OneOffQueryResponse` frames in a name-stamped envelope with table
names, raw RowList bytes, split row byte arrays, duration, message ID, and the
raw frame. Declared-view and table subscriptions can opt into managed
subscription handles with `returnHandle: true`; those handles use the same
server-acknowledged unsubscribe path, and table handles expose raw initial row
bytes until typed row decoding lands. It does not yet implement typed reducer
argument/result encoding, schema-aware declared query/view/table row decoding,
typed row callbacks, subscription cache behavior, auth refresh, or reconnect
policy.

The external `opsboard-canary` repository currently uses generated TypeScript
fixtures and handwritten protocol helpers as a canary bridge. Once the v1 SDK
exists, that app should become the main proof that normal TypeScript clients no
longer need handwritten wire-code plumbing.

SpacetimeDB's client SDKs are a useful reference for user experience:
generated types, reducer helpers, subscription handles, callbacks, and local
cache semantics are core parts of the product. Shunter should provide the same
category of ergonomic guarantees while keeping the protocol Shunter-native.

## v1 Client Responsibilities

The TypeScript client should provide:

- connection creation and teardown
- explicit connection states
- protocol version negotiation and mismatch errors
- auth token configuration and refresh hook
- typed reducer calls from generated metadata
- typed declared query calls
- typed declared view subscriptions
- subscription handles with idempotent unsubscribe
- initial snapshot delivery semantics
- transaction update handling
- local cache primitives for tables/views
- reconnect behavior and resubscription policy
- structured error types for auth, validation, protocol, and transport failures

The generated code should provide:

- table row types
- reducer names and argument/result helpers
- declared query/view names and result types
- permission/read metadata useful to clients
- stable module contract version metadata

Current target: [`typescript-sdk-contract.md`](typescript-sdk-contract.md)

## Decisions To Make

1. Package location is decided for v1: keep the small runtime package in this
   repo at `typescript/client` as `@shunter/client`, and have generated
   per-module bindings import shared runtime types from it.
2. Decide how reducer argument encoding is represented. The current Go reducer
   boundary uses raw bytes, so v1 needs either typed adapter conventions or
   generated helpers that make the byte boundary invisible to normal clients.
3. Decide whether local cache is table-oriented, view-oriented, or both.
4. Decide reconnect semantics:
   - drop subscriptions and require caller action
   - automatically resubscribe
   - replay missed updates from a cursor if such a cursor exists
5. Decide how generated clients handle unknown server contract versions.
6. Decide whether raw SQL is exposed in the SDK, and if so, label it as an
   escape hatch rather than the primary app API.

## Implementation Work

Completed or partially complete:

- Audit current `codegen` output at the roadmap level and identify that the
  missing piece is a runtime package, not more standalone type definitions.
- Define the proposed client runtime API in
  `typescript-sdk-contract.md`.
- Add the initial checked-in `typescript/client` runtime foundation with
  protocol constants, connection state and structured error types, subscription
  handle types, and typed reducer/query/view/table runtime interfaces.
- Update generated TypeScript goldens so generated bindings import and use
  `@shunter/client` runtime types while preserving current byte-level helper
  behavior.
- Add a TypeScript typecheck fixture proving generated TypeScript can import
  and use the runtime type surface.
- Add protocol compatibility helpers that surface structured protocol mismatch
  errors before the WebSocket runtime exists.
- Add an executable TypeScript runtime test for protocol mismatch handling and
  idempotent managed subscription-handle unsubscribe.
- Add a minimal `createShunterClient` lifecycle shell with token resolution,
  Shunter subprotocol negotiation, injected WebSocket construction, state-change
  callbacks, `connect()`, `close()`, and idempotent `dispose()`.
- Decode the initial server `IdentityToken` frame and resolve `connect()` with
  identity token, identity bytes, connection ID bytes, and negotiated
  subprotocol metadata.
- Add raw-byte reducer request encoding for the live v1 `CallReducerMsg` wire
  shape and a connected-client `callReducer` send path that generated
  byte-level helpers can type against.
- Decode the live v1 `TransactionUpdate` reducer response envelope enough for
  full-update reducer calls to resolve on committed updates and reject on
  failed updates. `NoSuccessNotify` calls remain send-ack only because committed
  success responses are intentionally suppressible by that flag.
- Add a reducer result helper that validates reducer name/request ID and wraps
  heavy `TransactionUpdate` frames in committed/failed result envelopes without
  changing the existing raw `callReducer()` return path.
- Add raw-byte declared-query request encoding for the live v1
  `DeclaredQueryMsg` wire shape and correlate `OneOffQueryResponse` frames so
  generated byte-level declared-query helpers can resolve or reject.
- Add a raw declared-query result helper that wraps successful
  `OneOffQueryResponse` frames in a typed envelope while keeping rows as raw
  RowList bytes and split row byte arrays.
- Export generated TypeScript aliases for the runtime reducer result envelope
  and raw declared-query result envelope so module-specific helpers can type
  those surfaces without adding runtime imports.
- Add raw declared-view subscription request encoding for the live v1
  `SubscribeDeclaredView` wire shape, correlate `SubscribeMultiApplied` and
  `SubscriptionError` frames for acceptance/failure, and return an idempotent
  unsubscribe function that sends `UnsubscribeMulti`.
- Add raw table subscription request encoding for the live v1
  `SubscribeSingle` wire shape, correlate `SubscribeSingleApplied` and
  `SubscriptionError` frames for acceptance/failure, and return an idempotent
  unsubscribe function that sends `UnsubscribeSingle`.
- Decode the live v1 `TransactionUpdateLight` envelope and register accepted
  declared-view/table subscriptions for raw update callbacks from initial
  subscribe responses, caller-bound `TransactionUpdate` commits, and
  `TransactionUpdateLight` deltas.
- Correlate `UnsubscribeSingleApplied`, `UnsubscribeMultiApplied`, and
  matching `SubscriptionError` frames so idempotent unsubscribe promises settle
  on server acknowledgement instead of resolving immediately after send.
- Add a raw RowList decoder for Shunter's live
  `[row_count][row_len,row_data...]` payload shape and expose raw per-row byte
  arrays on decoded one-off query table results, table initial rows, and
  optional table unsubscribe rows.
- Add optional raw per-row byte arrays on `RawSubscriptionUpdate` insert/delete
  payloads when those payloads can be decoded as RowList envelopes.
- Wire `returnHandle: true` on declared-view and table subscriptions to the
  managed subscription handle primitive. The handle path preserves the existing
  server-acknowledged unsubscribe behavior; table handles currently hold raw
  initial row bytes and declared-view handles are lifecycle-only.

Remaining:

- Decide and implement typed reducer argument/result encoding conventions
  beyond the current raw `Uint8Array` request path.
- Implement typed reducer result decoding beyond the current raw
  `TransactionUpdate` frame result, schema-aware declared-query row decoding,
  declared view/table typed row callback delivery, and subscription cache
  behavior on top of the WebSocket lifecycle shell.
- Implement schema-aware row decoding for declared query/view/table results and
  subscription updates.
- Implement reconnect, auth refresh, resubscription, and cache behavior.
- Add client tests for:
  - connection state transitions
  - reducer success/failure
  - declared query success/failure
  - declared view initial rows and deltas
  - unsubscribe behavior
  - auth failure
  - reconnect behavior
  - protocol version mismatch
- Wire the external canary app to use only the public SDK surface.
- Document the generated/client compatibility policy in the v1 contract doc.

## Verification

Use the repo's Go verification for generator changes:

```bash
rtk go test ./...
rtk npm --prefix typescript/client run test
```

The TypeScript command currently typechecks the runtime foundation, typechecks
generated golden fixture usage, and runs focused runtime behavior tests. Broaden
it as the WebSocket runtime lands.

## Done Criteria

- A normal app can use generated TypeScript artifacts without writing protocol
  message handlers by hand.
- Reducer calls, declared queries, and declared view subscriptions are typed.
- Reconnect and unsubscribe behavior are documented and tested.
- Protocol/contract mismatch failures are clear.
- The external canary app uses the SDK as an external app would.

## Non-Goals

- React hooks before the plain TypeScript client is stable.
- All SpacetimeDB client SDK features.
- Multi-language clients.
- Raw SQL as the primary client programming model.
