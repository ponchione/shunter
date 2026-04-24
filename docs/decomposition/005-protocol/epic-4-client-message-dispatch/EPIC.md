# Epic 4: Client Message Dispatch

**Parent:** [SPEC-005-protocol.md](../SPEC-005-protocol.md) §7, §9.1
**Blocked by:** Epic 1 (message decoding), Epic 3 (connection state and lifecycle)
**Blocks:** Epic 5 (Server Message Delivery — QueryID response/update routing)

**Cross-spec:** SPEC-001 (`CommittedState.Snapshot()` for OneOffQuery), SPEC-003 (executor command inbox), SPEC-004 (predicate normalization model)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 4.1 | [story-4.1-frame-reader-dispatch.md](story-4.1-frame-reader-dispatch.md) | Incoming frame reader, tag dispatch loop, text frame rejection |
| 4.2 | [story-4.2-subscribe-handler.md](story-4.2-subscribe-handler.md) | Parse SubscribeSingle/Multi, validate SQL, normalize predicates, manager-authoritative QueryID registration |
| 4.3 | [story-4.3-unsubscribe-callreducer.md](story-4.3-unsubscribe-callreducer.md) | Unsubscribe + CallReducer handlers, validation, executor routing |
| 4.4 | [story-4.4-oneoff-query.md](story-4.4-oneoff-query.md) | OneOffQuery handler, read-only snapshot query |

## Implementation Order

```
Story 4.1 (Frame reader + dispatch)
  ├── Story 4.2 (Subscribe handler)
  ├── Story 4.3 (Unsubscribe + CallReducer) — parallel with 4.2
  └── Story 4.4 (OneOffQuery) — parallel with 4.2, 4.3
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 4.1 | `protocol/dispatch.go`, `protocol/dispatch_test.go` |
| 4.2 | `protocol/handle_subscribe.go`, `protocol/handle_subscribe_test.go` |
| 4.3 | `protocol/handle_unsubscribe.go`, `protocol/handle_callreducer.go`, `protocol/handle_callreducer_test.go` |
| 4.4 | `protocol/handle_oneoff.go`, `protocol/handle_oneoff_test.go` |
