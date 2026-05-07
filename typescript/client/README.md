# @shunter/client

Status: checked-in v1 SDK runtime foundation.

This package owns the shared TypeScript runtime surface that generated Shunter
bindings import. The current slice includes constants, protocol compatibility
helpers, state and error types, a minimal `createShunterClient` WebSocket
lifecycle shell with initial `IdentityToken` decoding, a managed subscription
handle primitive, typed runtime interfaces, and raw reducer request encoding
plus connected WebSocket sending for the v1 `CallReducerMsg` shape and minimal
full-update `TransactionUpdate` response correlation. It also includes raw
declared-query request encoding and `OneOffQueryResponse` correlation, raw
declared-view subscription request encoding,
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
rows. It does not implement typed reducer argument/result encoding,
schema-aware declared query/view/table row decoding, typed row callbacks,
subscription cache behavior, or reconnect policy yet.

The lifecycle shell offers Shunter's v1 subprotocol, appends a configured token
as the server-supported `token` query parameter, tracks `idle`/`connecting`/
`connected`/`closing`/`closed`/`failed` states, and accepts an injected
WebSocket factory for Node tests or host-specific transports. `connect()`
resolves after the first server frame is decoded as an `IdentityToken`.
Full-update `callReducer()` calls currently resolve with the raw
`TransactionUpdate` response frame on committed status and reject on failed
status. `NoSuccessNotify` calls resolve after send because successful server
echoes may be suppressed.
`runDeclaredQuery()` currently resolves with the raw `OneOffQueryResponse`
frame on success and rejects on response errors.
`subscribeDeclaredView()` currently resolves after `SubscribeMultiApplied`,
rejects on `SubscriptionError`, and returns an unsubscribe function that sends
one `UnsubscribeMulti` frame for repeated calls and resolves after the matching
acknowledgement.
`subscribeTable()` currently sends a quoted whole-table `SubscribeSingle` SQL
query, resolves after `SubscribeSingleApplied`, rejects on `SubscriptionError`,
and returns an unsubscribe function that sends one `UnsubscribeSingle` frame
for repeated calls and resolves after the matching acknowledgement.
Declared-view and table subscriptions can opt into raw row-list/update bytes
with `onRawUpdate` and table-only `onRawRows` callbacks while typed decoding is
still pending. Callback consumers can use `decodeRowList()` to split live
RowList payloads into raw per-row bytes before generated schema codecs exist.

Generated module bindings should import types from `@shunter/client` and keep
module-specific table, reducer, query, and view names in the generated file.

## Verification

```bash
rtk npm --prefix typescript/client run test
```
