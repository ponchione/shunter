# Make Client Connectivity State Explicit

Status: generic runtime semantics completed 2026-07-12; product UX remains
trigger-dependent

Promotion trigger: a product is used from plants, jobsites, mobile hotspots, or
other networks where WebSocket interruption is a normal operating condition.

Owners: TypeScript client, generated bindings, protocol lifecycle, app-author
docs

## Current Result

Each successful identity handshake now establishes a monotonically increasing
connection epoch. Connected state reports replay progress, and
`whenSynchronized()` resolves only after every replayed subscription has
received an authoritative applied/error response. Managed handles retain rows
but enter `resynchronizing` across a disconnect, returning to `active` only
with the new epoch; reconnect with replay disabled closes them instead of
leaving stale rows authoritative.

Reducer and procedure calls that were sent but lost their authoritative
response reject with `ShunterCallInterruptedError` and outcome `unknown`.
Server-confirmed failures remain validation errors. Deterministic tests cover
token rejection, retry exhaustion, fresh-server identity, repeated loss,
successful replay, replay failure, and interrupted calls.

Framework banners, disabled-action policy, conflict UX, and an optional
wall-clock "last synchronized" display remain application decisions. The
runtime exposes the state needed for those decisions without selecting them.

## Why

Shunter reconnect is bounded and replays subscriptions, but a disconnected
interval is an authority boundary. An operator-facing application must never
silently present stale cached rows as current or imply that an action committed
when it did not.

## Outcome

Generated-client applications can present connected, reconnecting, stale,
resynchronized, and failed states consistently, with clear mutation outcomes.

## Work

1. [x] Define a connection/cache epoch that changes after each successful identity
   handshake and subscription replay.
2. [x] Expose replay completion through the TypeScript runtime; defer a
   wall-clock presentation timestamp until an application requires it.
3. [x] Specify when managed handles are stale, resynchronizing, active, or closed.
4. [x] Ensure reducer/procedure calls interrupted before an authoritative response
   have an explicit unknown-outcome state where appropriate.
5. [x] Provide connection-state listeners, handle state, and resync progress
   without adding framework-specific adapters prematurely.
6. [x] Test token refresh, auth rejection, server restart, missed updates,
   subscription replay, and repeated network flapping.
7. [x] Document that offline mutation queues are unsupported unless separately
   designed with idempotency and conflict semantics.

## Non-Goals

- offline-first multi-master behavior
- optimistic local writes that appear committed
- indefinite reconnect loops without bounded policy
- hiding transport failure behind cached data
- broad framework adapters before the runtime contract is stable

## Completion Evidence

- [x] deterministic runtime tests for generic connection/cache transitions
- [x] generated bindings expose the shared client state without app-specific
  casts
- [x] interrupted calls cannot be mistaken for confirmed commits
- [ ] product-specific browser presentation of stale/resynchronized UX;
  dormant until a real application selects it
- [x] reconnect behavior remains compatible with protocol version pins
