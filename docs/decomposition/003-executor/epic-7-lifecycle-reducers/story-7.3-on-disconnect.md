# Story 7.3: OnDisconnect Flow

**Epic:** [Epic 7 — Lifecycle Reducers & Client Management](EPIC.md)  
**Spec ref:** SPEC-003 §10.4  
**Depends on:** Story 7.1, Epic 4 (transaction lifecycle), Epic 5 (post-commit pipeline)  
**Blocks:** Story 7.4

---

## Summary

OnDisconnect is an internal executor command after the client is considered gone. Runs reducer and deletes sys_clients row. Disconnect cannot be vetoed — cleanup is guaranteed even on failure.

## Deliverables

- ```go
  func (e *Executor) handleOnDisconnect(connID ConnectionID, identity Identity)
  ```

- **Success path:**
  1. Begin transaction
  2. Look up OnDisconnect reducer in registry
  3. If registered: execute reducer handler
  4. Delete `sys_clients` row for this connection_id
  5. Commit once (reducer writes + row deletion atomic)

- **Failure path** (reducer error or panic):
  1. Roll back transaction (reducer writes discarded)
  2. Log the reducer failure
  3. Begin **new cleanup transaction**
  4. Delete `sys_clients` row
  5. Commit cleanup transaction
  6. Run post-commit pipeline for cleanup commit

- **No OnDisconnect reducer:**
  1. Begin transaction
  2. Delete `sys_clients` row
  3. Commit

## Acceptance Criteria

- [ ] OnDisconnect commits → sys_clients row deleted, reducer writes applied
- [ ] OnDisconnect reducer fails → sys_clients row still deleted via cleanup tx
- [ ] OnDisconnect reducer panics → sys_clients row still deleted via cleanup tx
- [ ] Cleanup transaction runs post-commit pipeline (durability, subscription eval)
- [ ] Disconnect cannot be vetoed by reducer (row always deleted)
- [ ] No OnDisconnect reducer → sys_clients row deleted, commit succeeds
- [ ] Reducer failure logged
- [ ] CallerContext.Source is CallSourceLifecycle

## Design Notes

- The two-transaction guarantee is critical: even if the application's OnDisconnect reducer is buggy, the sys_clients row is always cleaned up. Without this, a failing OnDisconnect would leak client rows indefinitely.
- The cleanup transaction produces its own changeset and TxID. Subscribers to sys_clients will see the delete.
- If the cleanup transaction itself fails (e.g., row already deleted by the first attempt somehow), that's an internal error but not necessarily fatal — the desired state (row deleted) may already be achieved.
