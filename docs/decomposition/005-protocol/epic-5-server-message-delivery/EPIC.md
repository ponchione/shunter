# Epic 5: Server Message Delivery

**Parent:** [SPEC-005-protocol.md](../SPEC-005-protocol.md) §8, §9.2–§9.4, §13
**Blocked by:** Epic 1 (message encoding, RowList), Epic 3 (connection send channel), Epic 4 (subscription state for routing)
**Blocks:** Epic 6 (Backpressure — outgoing message pipeline)

**Cross-spec:** SPEC-003 (ReducerCallResult metadata, TxID), SPEC-004 (CommitFanout, SubscriptionUpdate, FanOutMessage)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 5.1 | [story-5.1-client-sender.md](story-5.1-client-sender.md) | ClientSender interface, per-connection outbound writer (serialize, compress, enqueue) |
| 5.2 | [story-5.2-response-messages.md](story-5.2-response-messages.md) | SubscribeApplied, UnsubscribeApplied, SubscriptionError, OneOffQueryResult delivery |
| 5.3 | [story-5.3-transaction-update-delivery.md](story-5.3-transaction-update-delivery.md) | TransactionUpdate from CommitFanout, per-connection assembly, ordering guarantee |
| 5.4 | [story-5.4-reducer-call-result.md](story-5.4-reducer-call-result.md) | ReducerCallResult with caller-delta diversion, no duplicate TransactionUpdate |

## Implementation Order

```
Story 5.1 (ClientSender + outbound writer)
  └── Story 5.2 (Response messages)
        ├── Story 5.3 (TransactionUpdate delivery)
        └── Story 5.4 (ReducerCallResult) — parallel with 5.3
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 5.1 | `protocol/sender.go`, `protocol/sender_test.go` |
| 5.2 | `protocol/send_responses.go`, `protocol/send_responses_test.go` |
| 5.3 | `protocol/send_txupdate.go`, `protocol/send_txupdate_test.go` |
| 5.4 | `protocol/send_reducer_result.go`, `protocol/send_reducer_result_test.go` |
