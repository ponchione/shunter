# Epic 6: Fan-Out & Delivery

**Parent:** [SPEC-004-subscriptions.md](../SPEC-004-subscriptions.md) §8
**Blocked by:** Epic 5 (Evaluation Loop — produces CommitFanout)
**Blocks:** Nothing (terminal epic)

**Cross-spec:** Depends on SPEC-005 (protocol sender / outbound buffering / caller reducer-result delivery contract).

---

## Stories

| Story | File | Summary |
|---|---|---|
| 6.1 | [story-6.1-fanout-worker.md](story-6.1-fanout-worker.md) | FanOutWorker goroutine, inbox channel, delivery loop |
| 6.2 | [story-6.2-per-connection-assembly.md](story-6.2-per-connection-assembly.md) | Build TransactionUpdate per connection, preserve subscription boundaries |
| 6.3 | [story-6.3-backpressure.md](story-6.3-backpressure.md) | Bounded client buffer, disconnect-on-lag (v1), DroppedClients channel |
| 6.4 | [story-6.4-confirmed-reads.md](story-6.4-confirmed-reads.md) | TxDurable wait for confirmed-read clients |

## Implementation Order

```
Story 6.1 (FanOutWorker)
  └── Story 6.2 (Per-connection assembly)
        ├── Story 6.3 (Backpressure)
        └── Story 6.4 (Confirmed reads) — parallel with 6.3
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 6.1 | `fanout.go`, `fanout_test.go` |
| 6.2 | `fanout_assembly.go`, `fanout_assembly_test.go` |
| 6.3 | `fanout_backpressure.go`, `fanout_backpressure_test.go` |
| 6.4 | `fanout_durable.go`, `fanout_durable_test.go` |
