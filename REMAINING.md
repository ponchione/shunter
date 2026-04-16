# Remaining Implementation Work

Tracks spec/epic work not yet implemented. Tech debt tracked separately in TECH-DEBT.md.

---

## Phase 4 — Commit-log gaps (SPEC-002)

| Epic | What | Status | Deps |
|------|------|--------|------|
| E5: Snapshot I/O | Schema snapshot codec, snapshot writer/reader, integrity checks | Not implemented (error types only) | SPEC-001 E8 + SPEC-002 E1 |
| E6: Recovery | Segment scanning, snapshot selection, log replay, `OpenAndRecover` | Not implemented (error types only) | E2 + E3 + E5 + SPEC-001 E8 |
| E7: Log Compaction | Segment coverage tracking, compaction | Not implemented | E5 + recovery-side segment metadata |

Decomposition docs: `docs/decomposition/002-commitlog/epic-5-snapshot-io/`, `epic-6-recovery/`, `epic-7-log-compaction/`

---

## Phase 7 — Protocol gaps (SPEC-005)

| Epic/Slice | What | Status | Deps |
|------------|------|--------|------|
| E5: Server Message Delivery | `ClientSender` interface, outbound write loop, response delivery helpers, `TransactionUpdate` assembly, `ReducerCallResult` routing | **Done** (cece4ae–cefef83) | E1 + E3 + E4 |
| E6: Backpressure & Graceful Disconnect | Outgoing/incoming overflow enforcement (close 1008), clean close protocol, reconnection verification | **Done** | E5 |

Decomposition docs: `docs/decomposition/005-protocol/epic-5-server-message-delivery/`, `epic-6-backpressure-graceful-disconnect/`

---

## Phase 8 — Fan-out integration (SPEC-004)

| Epic | What | Status | Deps |
|------|------|--------|------|
| E6 remainder: Fan-Out & Delivery | `FanOutWorker` goroutine, per-connection assembly, backpressure/disconnect-on-lag, confirmed-read gating, `DroppedClients` signaling | Not implemented (`FanOutMessage` contract type only) | SPEC-004 E5 + SPEC-005 E5/E6 |

Decomposition docs: `docs/decomposition/004-subscriptions/epic-6-fanout-delivery/`

---

## Dependency chain for remaining work

```
commitlog E5 + E6 + E7    ← can start now (all upstream landed)
subscription E6 remainder  ← can start now (protocol E5 + E6 done)
```

## Parallelism

Two independent tracks can run simultaneously:

1. **Commit-log track:** E5 → E6 → E7
2. **Fan-out integration:** SPEC-004 E6 (all protocol deps landed)
