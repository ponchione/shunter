# Story 7.3: OnDisconnect Flow

**Epic:** [Epic 7 — Lifecycle Reducers & Client Management](EPIC.md)  
**Spec ref:** SPEC-003 §10.4  
**Depends on:** Story 7.1, Epic 4 (transaction lifecycle), Epic 5 (post-commit pipeline)  
**Blocks:** Story 7.4

---

## Summary

OnDisconnect is dispatched via the bespoke `OnDisconnectCmd` executor command (SPEC-003 §2.4) after the client is considered gone. Runs reducer and deletes sys_clients row. Disconnect cannot be vetoed — cleanup is guaranteed even on failure. All pinned contracts live in SPEC-003 §10.4 (CallSource / TxID / panic / fatal-state).

## Deliverables

- ```go
  func (e *Executor) handleOnDisconnect(connID ConnectionID, identity Identity)
  ```
  Dispatched from `OnDisconnectCmd`. Not a `CallReducerCmd` variant (§2.4).

- **Success path:**
  1. Begin transaction
  2. Look up OnDisconnect reducer in registry
  3. If registered: execute reducer handler with `CallerContext.Source = CallSourceLifecycle`
  4. Delete `sys_clients` row for this connection_id
  5. Commit once (reducer writes + row deletion atomic); allocates one TxID

- **Failure path** (reducer error or panic):
  1. Roll back reducer transaction (no TxID allocated; in-progress sequence IDs discarded per SPEC-001 rollback semantics)
  2. Log the reducer failure
  3. Begin **fresh cleanup transaction** (no reducer runs inside it)
  4. Delete `sys_clients` row
  5. Commit cleanup transaction; allocates the sole TxID consumed by this failed OnDisconnect
  6. Run post-commit pipeline for the cleanup commit with `source = CallSourceLifecycle`

- **No OnDisconnect reducer:**
  1. Begin transaction
  2. Delete `sys_clients` row
  3. Commit; allocates one TxID

## Acceptance Criteria

- [ ] OnDisconnect commits → sys_clients row deleted, reducer writes applied, one TxID allocated
- [ ] OnDisconnect reducer fails → sys_clients row still deleted via cleanup tx; exactly one TxID allocated (for the cleanup commit)
- [ ] OnDisconnect reducer panics → sys_clients row still deleted via cleanup tx
- [ ] Cleanup transaction runs post-commit pipeline (durability, subscription eval) with `source = CallSourceLifecycle`
- [ ] Disconnect cannot be vetoed by reducer (row always deleted)
- [ ] No OnDisconnect reducer → sys_clients row deleted, commit succeeds
- [ ] Reducer failure logged
- [ ] `CallerContext.Source = CallSourceLifecycle` during the reducer run; same source stamped on cleanup post-commit pipeline even though no reducer runs there
- [ ] Cleanup post-commit panic → executor-fatal per SPEC-003 §5.4 (same rule as any post-commit panic)
- [ ] Pre-commit panic inside the cleanup transaction → logged; executor does NOT latch fatal on its own account; row may leak until the startup dangling-client sweep runs (see SPEC-AUDIT SPEC-003 §2.2)
- [ ] `OnDisconnectCmd` still attempts cleanup when `e.fatal == true`; `CallReducerCmd` remains rejected in the same state (leaking sys_clients rows is worse than rejecting writes)

## Design Notes

- The two-transaction guarantee is critical: even if the application's OnDisconnect reducer is buggy, the sys_clients row is always cleaned up. Without this, a failing OnDisconnect would leak client rows indefinitely.
- Cleanup consumes exactly one TxID — the rolled-back reducer tx produces none (stamping is tied to commit, SPEC-003 §6). Earlier drafts described "two TxIDs"; SPEC-003 §10.4 pins the one-TxID model.
- `CallSourceLifecycle` is reused for the cleanup post-commit pipeline rather than introducing a separate `CallSourceSystem`; the cleanup is framed as the tail of the same OnDisconnect operation.
- If the cleanup transaction itself fails (e.g., row already deleted by the first attempt somehow), that's an internal error but not necessarily fatal — the desired state (row deleted) may already be achieved.
