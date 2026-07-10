# Make Client Connectivity State Explicit

Status: recommended for operational browser applications

Promotion trigger: a product is used from plants, jobsites, mobile hotspots, or
other networks where WebSocket interruption is a normal operating condition.

Owners: TypeScript client, generated bindings, protocol lifecycle, app-author
docs

## Why

Shunter reconnect is bounded and replays subscriptions, but a disconnected
interval is an authority boundary. An operator-facing application must never
silently present stale cached rows as current or imply that an action committed
when it did not.

## Outcome

Generated-client applications can present connected, reconnecting, stale,
resynchronized, and failed states consistently, with clear mutation outcomes.

## Work

1. Define a connection/cache epoch that changes after each successful identity
   handshake and subscription replay.
2. Expose last authoritative synchronization time and replay completion through
   the TypeScript runtime.
3. Specify when managed handles are stale, resynchronizing, active, or closed.
4. Ensure reducer/procedure calls interrupted before an authoritative response
   have an explicit unknown-outcome state where appropriate.
5. Provide app-facing hooks for banners, disabled actions, and resync progress
   without adding framework-specific adapters prematurely.
6. Test token refresh, auth rejection, server restart, missed updates,
   subscription replay, and repeated network flapping.
7. Document that offline mutation queues are unsupported unless separately
   designed with idempotency and conflict semantics.

## Non-Goals

- offline-first multi-master behavior
- optimistic local writes that appear committed
- indefinite reconnect loops without bounded policy
- hiding transport failure behind cached data
- broad framework adapters before the runtime contract is stable

## Completion Evidence

- deterministic runtime tests for every connection/cache state transition
- generated bindings expose the state without app-specific casts
- interrupted calls cannot be mistaken for confirmed commits
- sample browser lifecycle demonstrates stale and resynchronized UX
- reconnect behavior remains compatible with protocol version pins
