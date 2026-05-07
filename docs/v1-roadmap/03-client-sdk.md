# TypeScript Client SDK

Status: open, SDK contract drafted; implementation not started
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

Shunter has contract export and TypeScript codegen. Generated TypeScript
currently includes protocol metadata, row interfaces, table maps, reducer and
declared-read constants/helpers, permission metadata, read-model metadata, and
value-kind mappings.

That is necessary but not sufficient for v1. There is no `package.json`, no
maintained npm/runtime package, and no browser or Node client in the repo.
Generated definitions alone do not solve connection lifecycle, subscription
handles, auth refresh, local cache updates, reconnect policy, or protocol
version mismatch behavior.

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

1. Decide whether the SDK lives inside this repo, a sibling package, or generated
   output plus a small runtime library. Until this is decided, the only stable
   TypeScript surface is contract generation.
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

Remaining:

- Audit current `codegen` output against the proposed SDK contract.
- Add generated fixtures for representative module contracts.
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
```

Add the appropriate TypeScript typecheck/test command once the client package
exists, and record it in this document and the package README.

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
