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
own module-specific names and types.

Current foundation scope: `@shunter/client` exposes protocol constants,
protocol compatibility helpers, connection state types, structured errors, a
minimal `createShunterClient` WebSocket lifecycle shell with initial
`IdentityToken` decoding, a managed subscription handle primitive, and typed
runtime interfaces consumed by generated bindings. It also exposes raw
`Uint8Array` reducer request encoding for Shunter v1 `CallReducerMsg` frames
and a connected-client `callReducer` send path with minimal full-update
`TransactionUpdate` response correlation. It also exposes raw declared-query
request encoding for v1 `DeclaredQueryMsg` frames and correlates
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
frames. Typed reducer argument/result encoding, declared query/view/table row
decoding, typed row callbacks, subscription cache behavior, reconnect policy,
and local cache implementation remain open.

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
server `IdentityToken` frame is decoded into connection metadata.

## Auth

The runtime should accept either a static token string or an async token
provider. A refresh hook should be called before reconnect when the previous
connection failed with an auth-related error or the caller explicitly requests a
refresh.

Strict-mode failures should surface as auth errors, not generic WebSocket close
events.

## Reducer Calls

Generated bindings should expose typed reducer helpers. The plain runtime can
keep a lower-level byte-oriented escape hatch, but normal app code should call
generated helpers.

Open decision: reducer argument/result encoding. The Go runtime boundary is raw
bytes. The SDK needs a documented app-facing convention before generated
helpers are considered v1-stable.

Current foundation: the runtime can encode and send the raw-byte
`CallReducerMsg` shape used by the Go protocol: reducer name, raw args bytes,
request ID, and flags. Full-update
`createShunterClient().callReducer(...)` calls wait for the matching heavy
`TransactionUpdate`, resolve with that raw response frame on committed status,
and reject with a structured validation error on failed status. Calls made with
`NoSuccessNotify` resolve after send because the server may intentionally
suppress committed success echoes for that flag. Typed argument/result codecs
and a user-facing reducer result object remain required v1 follow-ups.

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
error. Typed row decoding and table/view metadata extraction remain required v1
follow-ups.

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
It does not yet update local cache state, decode initial rows, or apply typed
row deltas.

Current table foundation: the runtime can encode a raw `SubscribeSingle` query
string and `createShunterClient().subscribeTable(...)` builds a quoted
whole-table `SELECT * FROM "<table>"` query. The method waits for the matching
`SubscribeSingleApplied` response, rejects matching `SubscriptionError`
responses, and returns an idempotent unsubscribe function that sends
`UnsubscribeSingle`. The runtime can route the accepted raw row-list bytes to a
table-only `onRawRows` callback and raw delta bytes to `onRawUpdate`. It does
not resolve the unsubscribe helper until `UnsubscribeSingleApplied` or a
matching `SubscriptionError`. It does not yet decode the initial row list, call
the typed table row callback, update local cache state, or apply typed row
deltas.

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

- clean caller close does not reconnect
- transient transport failure may reconnect with backoff
- auth failure refreshes token once if a refresh hook is available
- subscriptions are automatically resubscribed only when the SDK can re-deliver
  an initial snapshot and clearly notify callers that state was refreshed
- missed-update replay is out of scope until the protocol exposes a stable
  cursor

## Generated Binding Shape

Generated bindings should expose:

- table row interfaces
- table names and table-to-row maps
- reducer names and typed reducer helper functions
- declared query/view names and typed helpers
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
