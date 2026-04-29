# Story 6.4: Reconnection Verification

**Epic:** [Epic 6 — Backpressure & Graceful Disconnect](EPIC.md)
**Spec ref:** SPEC-005 §11.3, §16
**Depends on:** Story 6.3
**Blocks:** Nothing (terminal story)

---

## Summary

End-to-end verification that reconnection works: same token → same Identity, client must re-subscribe from scratch, no server-side subscription state carries over.

This is primarily a verification/test story. The implementation is already covered by earlier stories (auth preserves Identity, connection state is per-connection). This story ensures the pieces compose correctly.

## Deliverables

- Integration test suite covering reconnection:
  1. Connect, receive `InitialConnection`, note Identity
  2. Subscribe, receive `SubscribeApplied`
  3. Disconnect (clean close)
  4. Reconnect with same token
  5. Verify `InitialConnection.Identity` matches step 1
  6. Verify NO `TransactionUpdate` messages arrive (subscriptions did not carry over)
  7. Re-subscribe, verify `SubscribeApplied` with fresh initial rows

- Additional reconnection scenarios:
  - Reconnect after network failure (server-side timeout)
  - Reconnect after buffer overflow disconnect
  - Reconnect with same `connection_id` (future resume semantics — no effect in v1, but verify accepted)

## Acceptance Criteria

- [ ] Same token on reconnect → same Identity in InitialConnection
- [ ] No subscriptions carry over from previous connection
- [ ] Re-subscribe after reconnect → fresh SubscribeApplied with current rows
- [ ] If rows changed during disconnect, re-subscribe shows updated state
- [ ] Reconnect with same `connection_id` → accepted (no semantic effect in v1)
- [ ] Reconnect with new `connection_id` → accepted, different ConnectionID
- [ ] Reconnect after buffer overflow disconnect → works normally

## Design Notes

- v1 has no gap-fill mechanism. The client has no way to request missed deltas. It must re-subscribe and rebuild state from `SubscribeApplied`. The `tx_id` from the last received `TransactionUpdate` can be used for coarse "did anything change?" detection, but no server-side support exists for resumption.
- This is a verification story, not a feature story. It validates that the system composed from Epics 2, 3, 4, and 5 behaves correctly across reconnection boundaries.
