# TypeScript SDK Contract

Status: proposed SDK contract; foundation package started
Scope: plain TypeScript runtime API and generated binding shape for Shunter v1
apps.

## Goal

The TypeScript SDK should let browser or Node applications use generated
contracts without hand-writing Shunter protocol handlers. It should be small,
framework-neutral, and stable enough to support the external canary/reference
app.

## Package Shape

Preferred v1 shape:

- a small checked-in TypeScript runtime package
- generated per-module bindings that import the runtime
- generated contract metadata committed by apps or produced during app builds

Package-location decision: the v1 runtime package lives in this repo at
`typescript/client` as `@shunter/client`, so Go codegen goldens and TypeScript
typechecks can evolve together.

The runtime package should own protocol connection lifecycle, auth token
handling, reducer/query/view request plumbing, subscription handles, reconnect
policy, local cache primitives, and structured errors. Generated bindings should
own module-specific names and types. Generated bindings now also export
module-scoped aliases for the runtime reducer result envelope and raw
declared-query result envelope, keeping those helper surfaces typed by the
module's reducer/query name unions without adding runtime imports.

Current foundation scope: `@shunter/client` exposes protocol constants,
protocol compatibility helpers, connection state types, structured errors, a
minimal `createShunterClient` WebSocket lifecycle shell with initial
`IdentityToken` decoding, a managed subscription handle primitive, and typed
runtime interfaces consumed by generated bindings. It also exposes raw
`Uint8Array` reducer request encoding for Shunter v1 `CallReducerMsg` frames
and a connected-client `callReducer` send path with minimal full-update
`TransactionUpdate` response correlation. `decodeReducerCallResult()` wraps
heavy reducer transaction updates in a minimal committed/failed result envelope
while preserving the raw `callReducer()` result, and generated reducer helpers
now include `callXResult(...)` wrappers that use that envelope path for
full-update calls. The runtime also defines explicit reducer argument encoder
conventions (`ReducerArgEncoder`, `encodeReducerArgs()`, and typed-arg call
helpers) for callers that already have module-local codecs. It also exposes raw
declared-query request encoding for v1 `DeclaredQueryMsg` frames and correlates
`OneOffQueryResponse` frames. It also exposes raw declared-view subscription
request encoding for v1 `SubscribeDeclaredView` frames, correlates
`SubscribeMultiApplied` and `SubscriptionError` responses, and returns an
idempotent unsubscribe function that sends `UnsubscribeMulti`. It also exposes
raw table subscription request encoding for v1 `SubscribeSingle` frames,
correlates `SubscribeSingleApplied` and `SubscriptionError` responses, and
returns an idempotent unsubscribe function that sends `UnsubscribeSingle`.
Accepted subscriptions are registered for raw callback delivery from initial
subscribe responses, caller-bound committed `TransactionUpdate` frames, and
`TransactionUpdateLight` frames. Unsubscribe promises wait for matching
`UnsubscribeSingleApplied`/`UnsubscribeMultiApplied` or `SubscriptionError`
frames. The runtime also exposes `decodeRowList()` for the live RowList
payload shape and includes raw per-row byte arrays on decoded one-off query
tables, table initial rows, and optional table unsubscribe rows. Raw
subscription updates now include optional `insertRowBytes` and `deleteRowBytes`
arrays when their insert/delete payloads decode as RowList envelopes.
`decodeRawDeclaredQueryResult()` wraps successful `OneOffQueryResponse` frames
in a raw declared-query result envelope containing table names, raw RowList
bytes, and split row byte arrays for generated helpers to type against.
`decodeDeclaredQueryResult()` builds on that raw helper by mapping each returned
table's row bytes through caller-provided table decoders or a table-name-aware
fallback decoder. Generated bindings expose module-scoped aliases for those
decode options/results and emit `queryXResult(...)` wrappers for executable
declared queries.
`subscribeDeclaredView()` and `subscribeTable()` can also opt into
`returnHandle: true`, resolving with a managed subscription handle backed by the
same server-acknowledged unsubscribe path. Table handles expose raw initial row
bytes by default, or decoded initial rows when `decodeRow` is supplied;
declared-view handles currently expose lifecycle state only.
`subscribeTable()` also accepts a caller-supplied `decodeRow` hook that decodes
raw RowList row bytes for `onRows`/`onInitialRows` and RowList insert/delete
updates for `onUpdate`, leaving raw callbacks unchanged. Generated table
subscription helpers pass through optional row callbacks and subscription
options, and expose module-scoped table decoder aliases. The runtime now also
includes a schema-aware BSATN product row decoder, and generated bindings emit
per-table row decoder functions plus a `tableRowDecoders` map that generated
whole-table subscription helpers use by default. Managed table subscription
handles now apply RowList insert/delete deltas using raw row bytes as local row
identity. The runtime also has an explicit opt-in reconnect policy with bounded
attempts, token-provider refresh per attempt, and automatic resubscription
after a fresh identity handshake. Typed reducer argument/result encoding,
generated declared query/view projection row decoding, declared-query/view
cache behavior, and broader local cache implementation remain open.

## Runtime API

Connection construction:

```ts
const client = createShunterClient({
  url: "ws://127.0.0.1:3000/subscribe",
  token: async () => token,
  protocol: contracts.protocol,
});
```

Expected runtime concepts:

- `connect()`, `close()`, and idempotent `dispose()`
- explicit states: `idle`, `connecting`, `connected`, `reconnecting`,
  `closing`, `closed`, `failed`
- state-change callbacks or async iterator
- structured errors for auth, validation, protocol mismatch, transport, timeout,
  and closed-client operations
- connection metadata exposing server identity token, negotiated protocol, and
  generated contract version metadata

Current lifecycle behavior: the runtime offers Shunter's v1 WebSocket
subprotocol, appends configured tokens as the server-supported `token` query
parameter for browser compatibility, and accepts an injected WebSocket factory
for Node or host-specific transports. `connect()` resolves after the initial
server `IdentityToken` frame is decoded into connection metadata. Passing
`reconnect: { enabled: true }` enables bounded reconnect attempts for
unexpected transport failures; each attempt resolves the configured token
source again, reconnects with backoff, and replays accepted subscription
requests after the new identity frame is received.

## Auth

The runtime accepts either a static token string or an async token provider.
Opt-in reconnect calls the token provider again before each reconnect attempt,
which is the v1 refresh hook for browser-compatible query-token auth.

Strict-mode failures should surface as auth errors, not generic WebSocket close
events.

## Reducer Calls

Generated bindings should expose typed reducer helpers. The plain runtime can
keep a lower-level byte-oriented escape hatch, but normal app code should call
generated helpers.

Open decision: generated reducer argument/result encoding. The Go runtime
boundary is raw bytes. The runtime now has an explicit app-facing encoder hook
convention, but generated schema-derived codecs require `ModuleContract` to
export reducer argument/result product schemas before typed reducer helpers are
considered v1-stable.

Current foundation: the runtime can encode and send the raw-byte
`CallReducerMsg` shape used by the Go protocol: reducer name, raw args bytes,
request ID, and flags. Full-update
`createShunterClient().callReducer(...)` calls wait for the matching heavy
`TransactionUpdate`, resolve with that raw response frame on committed status,
and reject with a structured validation error on failed status. Calls made with
`NoSuccessNotify` resolve after send because the server may intentionally
suppress committed success echoes for that flag. `decodeReducerCallResult()`
can wrap heavy `TransactionUpdate` frames in a reducer name/request ID/status
envelope with an optional caller-supplied result decoder.
`callReducerWithResult()` calls the lower-level raw reducer caller, waits for
the full-update frame, and returns that envelope. Generated bindings emit
`callXResult(...)` helpers on top of it while leaving `callX(...)` as the raw
`Uint8Array` helper. `encodeReducerArgs()` clones raw `Uint8Array` args and
requires a `ReducerArgEncoder` for typed args; `callReducerWithEncodedArgs()`
and `callReducerWithEncodedArgsResult()` apply that convention before invoking
the raw reducer path. Generated schema-aware argument/result codecs and
changing normal generated helpers away from raw bytes remain required v1
follow-ups.

Candidate v1 default:

- generated helpers encode reducer args with Shunter BSATN when the generator
  can derive a product shape
- apps can override with a module-local codec for reducers that intentionally
  accept raw bytes
- reducer results are returned as bytes unless a generated result type exists

Reducer call result:

- reducer name
- request ID
- status
- committed TX ID when committed
- typed return value or raw bytes
- structured user, permission, protocol, transport, or closed-client error

## Declared Queries

Generated bindings should expose typed declared query helpers by executable
query name. Metadata-only declarations should be generated as metadata but not
as callable helpers.

Current foundation: the runtime can encode the named `DeclaredQueryMsg` shape
used by the Go protocol and `createShunterClient().runDeclaredQuery(...)` waits
for a matching `OneOffQueryResponse`. Successful responses resolve with the raw
response frame; server error responses reject with a structured validation
error. `decodeRawDeclaredQueryResult()` turns a successful raw response frame
into a name-stamped result with table names, raw RowList bytes, split row byte
slices, duration, message ID, and the raw frame. `decodeDeclaredQueryResult()`
can then map those table row bytes with caller-supplied decoders and fails
clearly when a returned table has no decoder. Generated table row decoders are
available for whole-table result rows. Declared query/view projection row
decoders require exported projection schemas and table/view metadata extraction
before they can be generated.

Query calls should return:

- typed rows
- request ID
- optional table/view metadata needed by local caches
- structured errors for missing permissions, validation, protocol mismatch, and
  transport failure

Raw SQL should be available only as a clearly named escape hatch if v1 decides
to expose it in the SDK.

## Declared Views And Subscriptions

Generated bindings should expose typed declared view subscription helpers.

Current foundation: the runtime can encode the named
`SubscribeDeclaredView` shape used by the Go protocol and
`createShunterClient().subscribeDeclaredView(...)` waits for the matching
`SubscribeMultiApplied` response. `SubscriptionError` responses reject with a
structured validation error. On acceptance, the method returns an idempotent
unsubscribe function that sends `UnsubscribeMulti` for the accepted query ID.
The runtime can route raw `SubscriptionUpdate` bytes to `onRawUpdate` callbacks
from the initial response and later delta frames. The unsubscribe helper does
not resolve until `UnsubscribeMultiApplied` or a matching `SubscriptionError`.
Callers can request `returnHandle: true` to receive a `SubscriptionHandle`
whose `unsubscribe()` uses that same acknowledgement path; until schema-aware
view decoding lands, the handle is lifecycle-only and has an empty row set. It
does not yet update local cache state, decode initial rows, or apply typed row
deltas.
Raw update callback consumers can call `decodeRowList()` on live insert/delete
payloads when they need per-row byte slices before generated schema codecs
exist, or use optional `insertRowBytes`/`deleteRowBytes` fields on raw updates
when the payloads already decoded as RowList envelopes.

Current table foundation: the runtime can encode a raw `SubscribeSingle` query
string and `createShunterClient().subscribeTable(...)` builds a quoted
whole-table `SELECT * FROM "<table>"` query. The method waits for the matching
`SubscribeSingleApplied` response, rejects matching `SubscriptionError`
responses, and returns an idempotent unsubscribe function that sends
`UnsubscribeSingle`. The runtime can route the accepted raw row-list bytes to a
table-only `onRawRows` callback and raw delta bytes to `onRawUpdate`. When
callers provide `decodeRow`, the runtime decodes split RowList row bytes for
the table `onRows`/`onInitialRows` callbacks and decodes RowList insert/delete
updates for `onUpdate`; generated table helpers pass those arguments through
without requiring callers to drop to the raw runtime API. It also
splits table initial and optional unsubscribe RowList payloads into raw per-row
bytes on the decoded message envelopes. It does not resolve the unsubscribe
helper until `UnsubscribeSingleApplied` or a matching `SubscriptionError`. It
can also resolve with a `SubscriptionHandle` when callers pass
`returnHandle: true`; the handle starts active with the raw initial row byte
slices, or decoded initial rows when `decodeRow` is provided, and closes after
the acknowledged unsubscribe. Generated table helpers now default `decodeRow`
to the generated table decoder for that table, so whole-table subscriptions
receive typed row callbacks and typed handle initial rows without handwritten
decoders. Managed table handles keep a row-byte-keyed cache and apply RowList
insert/delete updates from later delta frames, preserving raw callbacks and
typed `onUpdate` delivery. Broader cache primitives and declared-view cache
updates remain open.

Subscription handles must provide:

- initial rows
- current state
- update callbacks or async iterator
- idempotent `unsubscribe()`
- `closed` promise or final-state notification

Initial snapshot semantics:

- the helper resolves only after the server accepts the subscription and the
  initial rows are available
- rejected subscriptions must not leave local cache state registered

Delta semantics:

- table-shaped views update by inserted/deleted row sets
- projected views update with projected row shape
- aggregate views update using the row shape documented in the module contract
- ordering/limit/offset initial snapshots do not imply ordered post-commit
  delivery unless the runtime contract explicitly says so

## Local Cache

The runtime should offer minimal cache primitives, not framework-specific state
management.

Required cache operations:

- replace initial view rows atomically
- apply transaction deltas
- read by primary key or stable row identity where the contract exposes one
- subscribe to cache change notifications
- clear cache on close or protocol mismatch

Open decision: table-oriented cache, view-oriented cache, or both. The reference
app should drive the smallest useful choice.

## Reconnect Policy

Default v1 policy should be explicit and conservative:

- reconnect is disabled unless `reconnect: { enabled: true }` is passed
- clean caller close does not reconnect
- unexpected transport failure reconnects with bounded backoff attempts
- token providers are called again before each reconnect attempt
- accepted subscriptions are automatically resubscribed only after a fresh
  identity handshake; managed table handles receive a refreshed initial snapshot
- missed-update replay is out of scope until the protocol exposes a stable
  cursor

## Generated Binding Shape

Generated bindings should expose:

- table row interfaces
- schema-aware table row decoder functions and a table decoder map
- table names and table-to-row maps
- reducer names and typed reducer helper functions
- declared query/view names and typed helpers
- module-scoped reducer result and raw declared-query result envelope aliases
- permissions/read-model metadata
- module contract format/version metadata
- runtime protocol metadata needed for negotiation

Identifier normalization and collision suffixes are stable for v1 generated
codegen output. The SDK should consume generated names as emitted and should
not re-normalize contract strings independently.

## Test Matrix

SDK tests should cover:

- state transitions
- token provider success/failure/refresh
- protocol version mismatch
- reducer success, user failure, permission failure, and transport failure
- declared query success/failure
- declared view initial rows and deltas
- idempotent unsubscribe
- reconnect with resubscription
- close during in-flight requests
- local cache replacement and delta application

## Non-Goals

- React hooks before the plain TypeScript runtime is stable
- SpacetimeDB client API compatibility
- SQL mutation helpers
- multi-language client generation
- hidden reconnect semantics that mutate app state without notification
