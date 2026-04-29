# Story 3.1: DeltaView & Delta Indexes

**Epic:** [Epic 3 — DeltaView & Delta Computation](EPIC.md)
**Spec ref:** SPEC-004 §6.4, §10.3
**Depends on:** Epic 1 (Predicate types), SPEC-001 (CommittedReadView), SPEC-003/SPEC-001 (`*Changeset` commit output)
**Blocks:** Stories 3.2, 3.3, 3.5

---

## Summary

Wraps committed state + transaction deltas into a unified data source. Builds per-transaction scratch indexes over delta rows so fragments like `dT1(+) join T2'` can use equality lookups on the delta side rather than linear scans.

## Deliverables

- `DeltaView` struct:
  ```go
  type DeltaView struct {
      committed store.CommittedReadView
      inserts   map[TableID][]ProductValue
      deletes   map[TableID][]ProductValue
      deltaIdx  DeltaIndexes
  }
  ```

- `DeltaIndexes` struct:
  ```go
  type DeltaIndexes struct {
      // insertIdx[table][col][encodedValue] = positions into inserts[table]
      insertIdx map[TableID]map[ColID]map[string][]int
      // deleteIdx mirrors the same shape for deletes.
      deleteIdx map[TableID]map[ColID]map[string][]int
  }
  ```
  Position-valued (int indices into the corresponding slice) rather than row-valued — avoids double-storing `ProductValue`.

- `NewDeltaView(committed CommittedReadView, changeset *Changeset, activeColumns map[TableID][]ColID) *DeltaView`
  - Copies insert/delete slices from the changeset
  - Builds scratch indexes only for the columns listed in `activeColumns` (the columns referenced by at least one active subscription)
  - Eager construction: once per transaction, not per subscription

- Access methods:
  - `InsertedRows(table TableID) []ProductValue`
  - `DeletedRows(table TableID) []ProductValue`
  - `DeltaIndexScan(table TableID, col ColID, value Value, inserted bool) []ProductValue` — equality lookup on the scratch index
  - `CommittedScan(table TableID) RowIterator` — delegates to the committed view
  - `CommittedIndexSeek(table TableID, indexID IndexID, key store.IndexKey) []RowID` — delegates to the committed view; committed-side access still uses real `IndexID`

## Acceptance Criteria

- [ ] Construct DeltaView from changeset → inserts/deletes accessible per table
- [ ] Scratch index built for specified columns only
- [ ] Scratch index not built for unspecified columns
- [ ] `DeltaIndexScan` returns correct rows matching value
- [ ] `DeltaIndexScan` on non-indexed column → panic (caller bug)
- [ ] Empty changeset for a table → empty slices, no scratch indexes
- [ ] Committed access methods delegate correctly
- [ ] Benchmark: scratch index construction < 1 ms for 100 rows × 3 active columns

## Design Notes

- `activeColumns` is computed once per evaluation cycle from the set of active subscriptions. This avoids building scratch indexes for columns no subscription cares about.
- Scratch index values store positions (int slices) rather than copying rows. This avoids double-storing ProductValue data.
- DeltaView does not own the committed view — the caller (evaluation loop) manages its lifecycle.
- SPEC-001's `CommittedReadView` must be closed by the caller before any blocking work. DeltaView may borrow it for in-process evaluation only; it must not extend the snapshot lifetime into fan-out or channel waits.
- **Keying rationale**: the delta side uses `ColID` because these indexes are ephemeral — they have no identity beyond the transaction. The committed side still uses the real `IndexID`. SpacetimeDB's reference implementation exposes a single `DeltaStore` trait keyed by `IndexId` for both sides; Shunter prefers the ColID-keyed scratch design so `DeltaView` stays independent of `SchemaRegistry` / `IndexResolver`. The evaluator does the resolver lookup exactly once, on the committed side, when needed.
