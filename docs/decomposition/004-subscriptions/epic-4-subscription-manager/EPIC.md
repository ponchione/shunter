# Epic 4: Subscription Manager

**Parent:** [SPEC-004-subscriptions.md](../SPEC-004-subscriptions.md) §4, §10
**Blocked by:** Epic 1 (Predicate, QueryHash), Epic 2 (Pruning Indexes for placement)
**Blocks:** Epic 5 (Evaluation Loop — subscriber lookup, manager state)

**Cross-spec:** Depends on SPEC-001 `CommittedReadView` for initial query execution and SPEC-003/SPEC-001 `Changeset` / executor-owned registration ordering.

---

## Stories

| Story | File | Summary |
|---|---|---|
| 4.1 | [story-4.1-query-state.md](story-4.1-query-state.md) | Internal query state: compiled plans, subscriber set, ref counting |
| 4.2 | [story-4.2-register.md](story-4.2-register.md) | Full registration flow: validate, compile, hash, dedup, initial query, index |
| 4.3 | [story-4.3-unregister.md](story-4.3-unregister.md) | Remove client from subscription, ref-count cleanup |
| 4.4 | [story-4.4-disconnect-client.md](story-4.4-disconnect-client.md) | Batch unsubscribe on connection drop |
| 4.5 | [story-4.5-manager-interface.md](story-4.5-manager-interface.md) | SubscriptionManager interface, DroppedClients channel, error types |

## Implementation Order

```
Story 4.1 (Query state)
  └── Story 4.2 (Register)
        └── Story 4.3 (Unregister)
              └── Story 4.4 (Disconnect)
Story 4.5 (Interface + errors) — parallel with 4.1
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 4.1 | `query_state.go` |
| 4.2 | `manager_register.go`, `manager_register_test.go` |
| 4.3 | `manager_unregister.go`, `manager_unregister_test.go` |
| 4.4 | `manager_disconnect.go`, `manager_disconnect_test.go` |
| 4.5 | `manager.go`, `subscription_errors.go` |
