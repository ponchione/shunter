# Epic 6: Backpressure & Graceful Disconnect

**Parent:** [SPEC-005-protocol.md](../SPEC-005-protocol.md) §10, §11
**Blocked by:** Epic 3 (connection management, keep-alive), Epic 5 (outgoing message pipeline)
**Blocks:** Nothing (terminal epic)

**Cross-spec:** SPEC-003 (OnDisconnect lifecycle reducer, DisconnectClientSubscriptionsCmd)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 6.1 | [story-6.1-outgoing-backpressure.md](story-6.1-outgoing-backpressure.md) | Outgoing buffer limit, disconnect-on-overflow, Close 1008 |
| 6.2 | [story-6.2-incoming-backpressure.md](story-6.2-incoming-backpressure.md) | Incoming queue limit, disconnect-on-flood, Close 1008 |
| 6.3 | [story-6.3-clean-close-network-failure.md](story-6.3-clean-close-network-failure.md) | Clean close protocol, network failure detection, subscription cleanup |
| 6.4 | [story-6.4-reconnection-verification.md](story-6.4-reconnection-verification.md) | End-to-end reconnection tests: identity preservation, re-subscribe from scratch |

## Implementation Order

```
Story 6.1 (Outgoing backpressure)
Story 6.2 (Incoming backpressure) — parallel with 6.1
Story 6.3 (Clean close + network failure) — after 6.1, 6.2
  └── Story 6.4 (Reconnection verification) — after 6.3
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 6.1 | `protocol/backpressure_out.go`, `protocol/backpressure_out_test.go` |
| 6.2 | `protocol/backpressure_in.go`, `protocol/backpressure_in_test.go` |
| 6.3 | `protocol/close.go`, `protocol/close_test.go` |
| 6.4 | `protocol/reconnect_test.go` |
