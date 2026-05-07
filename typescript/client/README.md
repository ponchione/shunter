# @shunter/client

Status: checked-in v1 SDK runtime foundation.

This package owns the shared TypeScript runtime surface that generated Shunter
bindings import. The current slice intentionally includes constants, state and
error types, and typed runtime interfaces only. It does not implement the
WebSocket connection, reconnect policy, reducer argument encoding, row decoding,
or cache behavior yet.

Generated module bindings should import types from `@shunter/client` and keep
module-specific table, reducer, query, and view names in the generated file.

## Verification

```bash
rtk npm --prefix typescript/client run typecheck
```
