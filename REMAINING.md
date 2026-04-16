# Remaining Implementation Work

Tracks spec/epic work not yet implemented. Tech debt tracked separately in TECH-DEBT.md.

As of 2026-04-16, all currently tracked implementation slices in this ledger are complete.

---

## Phase 4 — Commit-log gaps (SPEC-002)

| Epic | What | Status | Deps |
|------|------|--------|------|
| E5: Snapshot I/O | Schema snapshot codec, snapshot writer/reader, integrity checks | **Done** (implemented in `commitlog/snapshot_io.go`, verified 2026-04-16) | SPEC-001 E8 + SPEC-002 E1 |
| E6: Recovery | Segment scanning, snapshot selection, log replay, `OpenAndRecover` + resume-plan handoff for durability startup | **Done** (implemented in `commitlog/segment_scan.go`, `snapshot_select.go`, `replay.go`, `recovery.go`; verified 2026-04-16) | E2 + E3 + E5 + SPEC-001 E8 |
| E7: Log Compaction | Segment coverage tracking, compaction | **Done** (implemented in `commitlog/compaction.go`, verified 2026-04-16) | E5 + recovery-side segment metadata |

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
| E6 remainder: Fan-Out & Delivery | `FanOutWorker` goroutine, per-connection assembly, backpressure/disconnect-on-lag, confirmed-read gating, executor-wired `TxDurable` / caller metadata, `DroppedClients` signaling | **Done** (5f47d83–0619816 plus 2026-04-16 follow-through) | SPEC-004 E5 + SPEC-005 E5/E6 |

Decomposition docs: `docs/decomposition/004-subscriptions/epic-6-fanout-delivery/`

---

## Dependency chain for remaining work

```
All currently tracked implementation work is done.
```

## Parallelism

No tracked remaining implementation slices in this ledger.
