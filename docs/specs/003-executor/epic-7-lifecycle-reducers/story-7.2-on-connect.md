# Story 7.2: OnConnect Flow

**Epic:** [Epic 7 — Lifecycle Reducers & Client Management](EPIC.md)  
**Spec ref:** SPEC-003 §10.3  
**Depends on:** Story 7.1, Epic 4 (transaction lifecycle), Epic 5 (post-commit pipeline)  
**Blocks:** Story 7.4

---

## Summary

OnConnect is an internal executor command from the protocol layer. Inserts sys_clients row and optionally runs the OnConnect reducer, all in one transaction.

## Deliverables

- ```go
  func (e *Executor) handleOnConnect(connID ConnectionID, identity Identity)
  ```
  Called internally from `OnConnectCmd` dispatch (not via public Submit of a `CallReducerCmd`).

- Transaction flow:
  1. Begin transaction
  2. Insert `sys_clients` row: `{connection_id, identity, connected_at: now_ns}`
  3. Look up OnConnect reducer in registry
  4. If registered:
     - Build `ReducerContext` with `CallSourceLifecycle`
     - Execute OnConnect handler (with panic recovery)
     - If handler returns error or panics: rollback entire transaction (including sys_clients insert), reject connection
  5. If not registered: skip reducer, proceed to commit
  6. Commit
  7. On commit success: connection accepted, run post-commit pipeline
  8. On commit failure: connection rejected

## Acceptance Criteria

- [ ] OnConnect commits → sys_clients row present
- [ ] OnConnect reducer fails → sys_clients row absent (full rollback)
- [ ] OnConnect reducer panics → sys_clients row absent, executor continues
- [ ] No OnConnect reducer registered → sys_clients row inserted, commit succeeds
- [ ] sys_clients insert + reducer writes are in same transaction, single commit
- [ ] Connection rejected on any failure (error, panic, commit failure)
- [ ] Post-commit pipeline runs on success (durability, subscription eval)
- [ ] CallerContext.Source is CallSourceLifecycle

## Design Notes

- OnConnect is gating: if it fails, the connection is rejected. This lets applications enforce authentication or authorization in OnConnect.
- The sys_clients row insert happens before the reducer runs, so the reducer can read it (e.g., to inspect connected_at or set up related state).
- The protocol layer sends OnConnect as an internal command before allowing the client to issue normal reducer calls. This ensures the client appears in sys_clients before any of its reducers execute.
