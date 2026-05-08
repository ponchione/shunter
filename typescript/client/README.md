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
path for full-update reducer calls, including failed reducer updates surfaced
by the connected client. `ReducerArgEncoder`, `encodeReducerArgs()`,
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
codecs. Generated bindings expose `queryXResult(...)` and
`queryXDecoded(...)` wrappers with module-scoped row decoders when the contract
contains declared-read row metadata.
`subscribeDeclaredView()` and `subscribeTable()` also accept `returnHandle:
true` to resolve with a managed subscription handle wired to the same
server-acknowledged unsubscribe path. Table handles expose raw row bytes from
the initial snapshot by default, or decoded initial rows when `decodeRow` is
supplied. Declared-view subscriptions can also accept `decodeRow` to invoke
decoded `onInitialRows` and `onUpdate` callbacks, and declared-view handles use
RowList insert/delete payloads to maintain decoded row sets when a decoder is
available. Table subscriptions can accept `decodeRow` to invoke decoded
`onRows`/`onInitialRows` and `onUpdate` callbacks from split RowList row bytes
while keeping raw callbacks unchanged; without a decoder, table `onRows` and
`onInitialRows` receive raw row bytes. The runtime also exports
`decodeBsatnProduct()` and `encodeBsatnProduct()` for schema-aware product row
codecs, and generated bindings emit per-table row decoder functions plus a
`tableRowDecoders` map that generated table subscription helpers use by
default. Managed table and declared-view handles apply RowList insert/delete
updates using raw row bytes as local identity. The runtime also supports
explicit opt-in reconnect with bounded retry,
token-provider refresh per attempt, and subscription replay after a fresh
identity handshake.

The lifecycle shell offers Shunter's v1 subprotocol, appends a configured token
as the server-supported `token` query parameter, tracks `idle`/`connecting`/
`connected`/`reconnecting`/`closing`/`closed`/`failed` states, and accepts an
injected WebSocket factory for Node tests or host-specific transports.
`connect()` resolves after the first server frame is decoded as an
`IdentityToken`. Passing `reconnect: { enabled: true }` reconnects unexpected
transport failures with configurable bounded backoff.
Token providers must resolve to strings; invalid results fail before a
WebSocket is created.
Unsupported or malformed connected server frames fail the client as protocol
errors, rejecting pending operations and closing active managed handles.
Server-side subscription evaluation errors that are not scoped to a pending
request also fail the client so live handles do not remain silently stale.
Scoped errors for already accepted subscriptions are treated the same way when
no pending subscribe or unsubscribe matches them.
Full-update `callReducer()` calls currently resolve with the raw
`TransactionUpdate` response frame on committed status and reject on failed
status. `NoSuccessNotify` calls resolve after send because successful server
echoes may be suppressed. Reducer calls reject explicit request IDs that are
already awaiting a full-update response. Generated helpers can use `decodeReducerCallResult()`
or `callReducerWithResult()` to wrap heavy transaction update frames in a
reducer name/request ID/status envelope; connected-client reducer failures are
converted into failed result envelopes on that path. Typed reducer callers can use
`encodeReducerArgs()` and `callReducerWithEncodedArgs()` when they provide
their own argument encoder; generated bindings provide schema-derived argument
encoders and standalone reducer result product decoders when the contract
exports those schemas.
`runDeclaredQuery()` currently resolves with the raw `OneOffQueryResponse`
frame on success and rejects on response errors. Consumers that want a typed raw
envelope can pass that frame to `decodeRawDeclaredQueryResult()`.
`subscribeDeclaredView()` currently resolves after `SubscribeMultiApplied`,
rejects on `SubscriptionError`, and returns an unsubscribe function that sends
one `UnsubscribeMulti` frame for repeated calls and resolves after the matching
acknowledgement. It accepts `decodeRow`, `onInitialRows`, `onUpdate`, and
`returnHandle` options for typed declared-view consumers when update payloads
include RowList row bytes.
`subscribeTable()` currently sends a quoted whole-table `SubscribeSingle` SQL
query, resolves after `SubscribeSingleApplied`, rejects on `SubscriptionError`,
and returns an unsubscribe function that sends one `UnsubscribeSingle` frame
for repeated calls and resolves after the matching acknowledgement.
Explicit subscription IDs are rejected while the same request/query ID is
pending, active, or awaiting unsubscribe acknowledgement.
Auto-generated subscription IDs skip those occupied request/query IDs.
Passing `returnHandle: true` to either subscription method preserves the same
acceptance and acknowledgement semantics while resolving with a
`SubscriptionHandle` whose `unsubscribe()` is idempotent.
Declared-view and table subscriptions can opt into raw row-list/update bytes
with `onRawUpdate` and table-only `onRawRows` callbacks. Callback consumers can
use `decodeRowList()` to split live RowList payloads into raw per-row bytes, or
read `insertRowBytes`/`deleteRowBytes` from raw updates when present. Raw row
callbacks receive cloned row bytes so callback mutation cannot corrupt decoded
initial rows or managed handles.
Table subscriptions can also pass `decodeRow` when the caller already has a
schema-aware row decoder; the runtime will call the table `onRows`/
`onInitialRows` callbacks for accepted initial rows and `onUpdate` for RowList
insert/delete deltas. Without `decodeRow`, table `onRows`/`onInitialRows`
callbacks receive cloned raw row bytes. Generated table subscription helpers
pass through those callbacks and options. When `returnHandle: true` is also
set, the returned table handle starts with decoded initial rows. Generated
bindings now provide table row decoders for exported table schemas and default
generated table subscription helpers to those decoders. Managed table handles
keep their row sets current when later transaction updates include RowList
insert/delete row bytes.
Declared query consumers that want decoded rows can call
`decodeDeclaredQueryResult()` with table-specific decoders; generated
declared-query helpers install contract-derived decoders by default when row
metadata exists. Consumers that need raw RowList bytes can keep using
`decodeRawDeclaredQueryResult()`.

Generated module bindings should import types from `@shunter/client` and keep
module-specific table, reducer, query, and view names in the generated file.
They also export module-scoped aliases for reducer result and raw
declared-query result envelopes so helper code can keep those surfaces tied to
generated name unions.

## Verification

```bash
rtk npm --prefix typescript/client run test
```
