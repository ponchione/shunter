# WebSocket library: `github.com/coder/websocket`

**Date:** 2026-04-14
**Phase:** 7 (SPEC-005 protocol core, Story 3.2 onwards)

## Decision

Use `github.com/coder/websocket` (ISC) for all WebSocket transport in the `protocol/` package. Rejected `github.com/gorilla/websocket` (BSD-2).

## Why

- **Context-first API.** Shunter threads `context.Context` through executor, scheduler, and post-commit paths. `coder.Conn.Read(ctx)` matches; gorilla still uses `SetReadDeadline(time.Time)`, which forces deadline-derivation gymnastics at every handler.
- **No per-frame permessage-deflate.** Matches SPEC-005 §3.3 app-level gzip envelope. One less attack surface, no double-compression risk.
- **Maintenance pace.** gorilla archived in late 2022 and unarchived with slower cadence since; coder acquired and actively ships the nhooyr lineage.
- **Smaller API surface.** Shunter is aiming for a coherent hosted runtime, not a kitchen-sink transport stack. Fewer knobs = less public config burden while the runtime surface is still being defined.

## Tradeoff accepted

gorilla has more production mileage against weird public-internet middleboxes and proxies. Shunter still values that less than a clean context API and a smaller transport surface while the hosted runtime/server shape is being locked down.
