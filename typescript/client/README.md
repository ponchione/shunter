# @shunter/client

Status: checked-in v1 SDK runtime foundation.

This package owns the shared TypeScript runtime surface that generated Shunter
bindings import. The current slice includes constants, protocol compatibility
helpers, state and error types, a minimal `createShunterClient` WebSocket
lifecycle shell with initial `IdentityToken` decoding, a managed subscription
handle primitive, typed runtime interfaces, and raw reducer request encoding
plus connected WebSocket sending for the v1 `CallReducerMsg` shape and minimal
full-update `TransactionUpdate` response correlation. `decodeReducerCallResult()`
wraps heavy reducer `TransactionUpdate` frames in a minimal committed/failed
result envelope without changing `callReducer()`'s raw-frame behavior.
`callReducerWithResult()` lets generated helpers call through that envelope
path for full-update reducer calls. `ReducerArgEncoder`, `encodeReducerArgs()`,
`callReducerWithEncodedArgs()`, and `callReducerWithEncodedArgsResult()` define
the explicit typed-argument-to-`Uint8Array` convention for callers that already
have module-local codecs. It also includes raw declared-query request encoding
and `OneOffQueryResponse`
correlation, raw declared-view subscription request encoding,
`SubscribeMultiApplied`/`SubscriptionError` correlation, an idempotent
unsubscribe send path for `UnsubscribeMulti`, raw table subscription request
encoding, `SubscribeSingleApplied`/`SubscriptionError` correlation, and an
idempotent unsubscribe send path for `UnsubscribeSingle`. Accepted
subscriptions are registered for raw `TransactionUpdate` and
`TransactionUpdateLight` callback delivery, and unsubscribe promises now wait
for `UnsubscribeSingleApplied`/`UnsubscribeMultiApplied` or matching
`SubscriptionError`. It also exposes a raw `decodeRowList()` helper for the
live RowList payload shape and attaches raw per-row byte arrays to decoded
one-off query table rows, table initial rows, and optional table unsubscribe
rows. Raw subscription updates include optional `insertRowBytes` and
`deleteRowBytes` arrays when their insert/delete payloads can be decoded as
RowList envelopes. `decodeRawDeclaredQueryResult()` wraps successful
`OneOffQueryResponse` frames in a raw declared-query result envelope containing
table names, raw RowList bytes, and split row byte arrays.
`decodeDeclaredQueryResult()` maps those raw table row bytes through
caller-provided table decoders when consumers already have schema-aware row
codecs. Generated bindings can expose `queryXResult(...)` wrappers and
module-scoped decoded-query/table-decoder aliases over these runtime surfaces.
`subscribeDeclaredView()` and `subscribeTable()` also accept `returnHandle:
true` to resolve with a managed subscription handle wired to the same
server-acknowledged unsubscribe path. Table handles expose raw row bytes from
the initial snapshot by default, or decoded initial rows when `decodeRow` is
supplied; declared-view handles are lifecycle-only until schema-aware view row
decoding lands. Table subscriptions can accept `decodeRow` to invoke decoded
`onRows`/`onInitialRows` and `onUpdate` callbacks from split RowList row bytes
while keeping raw callbacks unchanged. The runtime also exports
`decodeBsatnProduct()` for schema-aware row decoding, and generated bindings
emit per-table row decoder functions plus a `tableRowDecoders` map that
generated table subscription helpers use by default. Managed table handles
apply RowList insert/delete updates using raw row bytes as local identity. It
does not implement
typed reducer argument/result encoding, declared query/view projection row
decoding, declared-query/view cache behavior, or reconnect policy yet.

The lifecycle shell offers Shunter's v1 subprotocol, appends a configured token
as the server-supported `token` query parameter, tracks `idle`/`connecting`/
`connected`/`closing`/`closed`/`failed` states, and accepts an injected
WebSocket factory for Node tests or host-specific transports. `connect()`
resolves after the first server frame is decoded as an `IdentityToken`.
Full-update `callReducer()` calls currently resolve with the raw
`TransactionUpdate` response frame on committed status and reject on failed
status. `NoSuccessNotify` calls resolve after send because successful server
echoes may be suppressed. Generated helpers can use `decodeReducerCallResult()`
or `callReducerWithResult()` to wrap heavy transaction update frames in a
reducer name/request ID/status envelope while typed result decoding remains
pending. Typed reducer callers can use `encodeReducerArgs()` and
`callReducerWithEncodedArgs()` when they provide their own argument encoder;
generated schema-derived encoders remain pending.
`runDeclaredQuery()` currently resolves with the raw `OneOffQueryResponse`
frame on success and rejects on response errors. Consumers that want a typed raw
envelope can pass that frame to `decodeRawDeclaredQueryResult()`.
`subscribeDeclaredView()` currently resolves after `SubscribeMultiApplied`,
rejects on `SubscriptionError`, and returns an unsubscribe function that sends
one `UnsubscribeMulti` frame for repeated calls and resolves after the matching
acknowledgement.
`subscribeTable()` currently sends a quoted whole-table `SubscribeSingle` SQL
query, resolves after `SubscribeSingleApplied`, rejects on `SubscriptionError`,
and returns an unsubscribe function that sends one `UnsubscribeSingle` frame
for repeated calls and resolves after the matching acknowledgement.
Passing `returnHandle: true` to either subscription method preserves the same
acceptance and acknowledgement semantics while resolving with a
`SubscriptionHandle` whose `unsubscribe()` is idempotent.
Declared-view and table subscriptions can opt into raw row-list/update bytes
with `onRawUpdate` and table-only `onRawRows` callbacks while typed decoding is
still pending. Callback consumers can use `decodeRowList()` to split live
RowList payloads into raw per-row bytes before generated schema codecs exist,
or read `insertRowBytes`/`deleteRowBytes` from raw updates when present.
Table subscriptions can also pass `decodeRow` when the caller already has a
schema-aware row decoder; the runtime will call the table `onRows`/
`onInitialRows` callbacks for accepted initial rows and `onUpdate` for RowList
insert/delete deltas. Generated table subscription helpers pass through those
callbacks and options. When `returnHandle: true` is also set, the returned
table handle starts with decoded initial rows. Generated bindings now provide
table row decoders for exported table schemas and default generated table
subscription helpers to those decoders. Managed table handles keep their row
sets current when later transaction updates include RowList insert/delete row
bytes.
Declared query consumers that want decoded rows can call
`decodeDeclaredQueryResult()` with table-specific decoders; consumers that need
raw RowList bytes can keep using `decodeRawDeclaredQueryResult()`.

Generated module bindings should import types from `@shunter/client` and keep
module-specific table, reducer, query, and view names in the generated file.
They also export module-scoped aliases for reducer result and raw
declared-query result envelopes so helper code can keep those surfaces tied to
generated name unions.

## Verification

```bash
rtk npm --prefix typescript/client run test
```
