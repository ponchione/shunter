# Story 4.5: SubscriptionManager Interface & Error Types

**Epic:** [Epic 4 — Subscription Manager](EPIC.md)
**Spec ref:** SPEC-004 §10.1, §10.2, §11.1–§11.3, §12.2
**Depends on:** Epic 1 (types), SPEC-001 (`CommittedReadView`), SPEC-003/SPEC-001 (`*Changeset`)
**Blocks:** Story 5.1 (Evaluation Loop uses the interface)

---

## Summary

Public type/interface contract consumed by the executor (SPEC-003). Error types for registration and evaluation failures. This story declares shared types; concrete behavior is implemented in Stories 4.2–4.4 and 5.1.

## Deliverables

- `SubscriptionManager` interface:
  ```go
  type SubscriptionManager interface {
      Register(req SubscriptionRegisterRequest, view CommittedReadView) (SubscriptionRegisterResult, error)
      Unregister(connID ConnectionID, subscriptionID SubscriptionID) error
      DisconnectClient(connID ConnectionID) error
      EvalAndBroadcast(txID TxID, changeset *Changeset, view CommittedReadView, meta PostCommitMeta)
      DroppedClients() <-chan ConnectionID
  }
  ```

- `SubscriptionID` type (if not already defined in SPEC-003 types)

- `SubscriptionRegisterRequest` struct (§4.1) — canonical type declaration used by Story 4.2

- `SubscriptionRegisterResult` struct (§4.1) — canonical type declaration used by Story 4.2

- `SubscriptionUpdate` struct (§10.2)

- `TransactionUpdate` struct (§10.2)

- `CommitFanout` type: `map[ConnectionID][]SubscriptionUpdate`

- Row-level update contract for v1 (§12.2): `SubscriptionUpdate.Inserts` / `Deletes` carry full rows; updates are modeled as delete+insert, not partial-column patches

- Error types:
  - `ErrTooManyTables` — predicate spans >2 tables
  - `ErrUnindexedJoin` — join column lacks index
  - `ErrJoinIndexUnresolved` — join was schema-valid but runtime `IndexResolver` could not resolve the needed committed-state index ID
  - `ErrInvalidPredicate` — type mismatch or structural error
  - `ErrTableNotFound` — predicate references missing table
  - `ErrColumnNotFound` — predicate references missing column
  - `ErrInitialRowLimit` — initial snapshot too large
  - `ErrSubscriptionNotFound` — unknown subscription ID
  - `ErrSubscriptionEval` — evaluation failure (corrupted index, type mismatch)
  - `ErrSendBufferFull` — fan-out delivery could not enqueue to the target client
  - `ErrSendConnGone` — target connection disappeared before delivery completed

- `DroppedClients()` returns a receive-only channel. Fan-out goroutine and manager evaluation-error cleanup may both send; executor drains the single shared stream.

## Acceptance Criteria

- [ ] All error types are distinct via `errors.Is`
- [ ] `SubscriptionManager` interface compilable with concrete implementation
- [ ] `CommitFanout` correctly keyed by ConnectionID
- [ ] `SubscriptionRegisterRequest` carries `ClientIdentity` for parameterized-hash computation
- [ ] `SubscriptionUpdate` carries SubscriptionID, TableID, TableName, Inserts, Deletes
- [ ] `TransactionUpdate` groups updates by TxID
- [ ] v1 update granularity is row-level full-row inserts/deletes only
- [ ] `DroppedClients()` returns non-nil channel

## Design Notes

- This story defines the shared contract only. Implementation is spread across Stories 4.2–4.4 (register/unregister/disconnect) and Story 5.1 (EvalAndBroadcast implementation).
- `ErrTableNotFound` and `ErrColumnNotFound` are introduced by predicate validation (Story 1.2) and reused here; this story should not be read as re-introducing them.
- `ErrJoinIndexUnresolved` belongs to the registration/evaluation boundary, not pure schema validation: the schema can prove an index exists conceptually while the runtime `IndexResolver` still fails to produce the concrete committed-state `IndexID` the evaluator needs.
- `EvalAndBroadcast` has no return value — it sends results via the fan-out channel, not via return. Errors during evaluation are handled per-subscription (§11.1), not propagated to caller.
- The shared dropped-client channel is non-blocking from the executor's point of view because it is buffered. Duplicate connection IDs are permitted; executor-side disconnect cleanup must be idempotent.
- Invariant violations (§11.3) are panics, not error types. Negative dedup counts, orphaned query hashes, subscriber/client map inconsistencies — these are bugs.
