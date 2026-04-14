# Story 3.2: WebSocket Upgrade Handler

**Epic:** [Epic 3 — WebSocket Transport & Connection Lifecycle](EPIC.md)
**Spec ref:** SPEC-005 §2.1–§2.3, §4.3
**Depends on:** Story 3.1, Epic 2 (auth)
**Blocks:** Story 3.3

---

## Summary

HTTP handler that authenticates the request, negotiates the WebSocket protocol, and upgrades the connection.

## Deliverables

- `func (s *Server) HandleSubscribe(w http.ResponseWriter, r *http.Request)` — the `/subscribe` endpoint handler:
  1. Extract token from `Authorization: Bearer <JWT>` header, falling back to `?token=` query param
  2. Authenticate: call `ValidateJWT` (strict mode) or `MintAnonymousToken` (anonymous mode, no token)
  3. Parse optional `?connection_id=` — hex-decode, reject zero with `400`; if absent, `GenerateConnectionID()`
  4. Parse optional `?compression=` — accept `none` (default) or `gzip`; reject unknown with `400`
  5. Validate `Sec-WebSocket-Protocol` header contains `v1.bsatn.shunter`; reject with `400` if missing
  6. Upgrade to WebSocket (binary mode), echo `v1.bsatn.shunter` in response protocol header
  7. Set `MaxMessageSize` read limit on WebSocket connection
  8. Hand off to connection lifecycle (Story 3.4: OnConnect + InitialConnection)

- Authentication error responses (before upgrade):
  - No token + strict mode → `401`
  - Invalid signature → `401`
  - Expired token → `401`
  - `hex_identity` mismatch → `401`
  - Zero `connection_id` → `400`

## Acceptance Criteria

- [ ] Valid token + correct protocol → WebSocket upgrade succeeds
- [ ] Token in `Authorization` header → accepted
- [ ] Token in `?token=` query param → accepted
- [ ] No token, strict mode → `401` before upgrade
- [ ] Invalid token → `401` before upgrade
- [ ] No token, anonymous mode → upgrade succeeds, token minted
- [ ] `?connection_id=<valid hex>` → used as ConnectionID
- [ ] No `connection_id` param → server generates one
- [ ] `?connection_id=00000000000000000000000000000000` → `400`
- [ ] `Sec-WebSocket-Protocol` missing `v1.bsatn.shunter` → `400`
- [ ] `?compression=gzip` → compression enabled for connection
- [ ] `?compression=none` → compression disabled
- [ ] `?compression=zstd` → `400` (unknown)

## Design Notes

- Token in query param is supported for clients that cannot set headers (some WebSocket implementations). `Authorization` header is preferred per spec.
- The upgrade handler does NOT send `InitialConnection` or run `OnConnect`. It hands off to the connection lifecycle goroutine (Story 3.4) which handles those in sequence.
- Use a standard WebSocket library (e.g., `nhooyr.io/websocket` or `gorilla/websocket`).
