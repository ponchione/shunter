# @shunter/client

Status: checked-in v1 SDK runtime foundation.

This package owns the shared TypeScript runtime surface that generated Shunter
bindings import. The current slice includes constants, protocol compatibility
helpers, state and error types, a minimal `createShunterClient` WebSocket
lifecycle shell with initial `IdentityToken` decoding, a managed subscription
handle primitive, and typed runtime interfaces. It does not implement
reducer/query/view protocol messages, reconnect policy, reducer argument
encoding, row decoding, or cache behavior yet.

The lifecycle shell offers Shunter's v1 subprotocol, appends a configured token
as the server-supported `token` query parameter, tracks `idle`/`connecting`/
`connected`/`closing`/`closed`/`failed` states, and accepts an injected
WebSocket factory for Node tests or host-specific transports. `connect()`
resolves after the first server frame is decoded as an `IdentityToken`.

Generated module bindings should import types from `@shunter/client` and keep
module-specific table, reducer, query, and view names in the generated file.

## Verification

```bash
rtk npm --prefix typescript/client run test
```
