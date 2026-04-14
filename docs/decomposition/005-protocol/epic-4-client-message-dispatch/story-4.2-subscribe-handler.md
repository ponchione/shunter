# Story 4.2: Subscribe Handler

**Epic:** [Epic 4 — Client Message Dispatch](EPIC.md)
**Spec ref:** SPEC-005 §7.1, §7.1.1, §9.1
**Depends on:** Story 4.1
**Blocks:** Epic 5 (subscription state for routing)

**Cross-spec:** SPEC-003 (executor inbox: `RegisterSubscriptionCmd`), SPEC-004 (predicate model: `AllRows`, `ColEq`, `And`)

---

## Summary

Parse and validate `Subscribe` messages. Normalize the structured query into the SPEC-004 predicate model. Track subscription state. Route to executor for registration.

## Deliverables

- `func handleSubscribe(conn *Conn, msg *SubscribeMsg, executor ExecutorInbox, schema SchemaLookup)`:
  1. Validate `subscription_id` not already active or pending → `ErrDuplicateSubscriptionID` → send `SubscriptionError`
  2. Validate `table_name` exists → send `SubscriptionError` if not
  3. Validate each predicate column exists on table → send `SubscriptionError` if not
  4. Validate predicate shape is v1 subset (equality only, single table) → send `SubscriptionError` if not
  5. Reserve `subscription_id` as pending in `SubscriptionTracker`
  6. Normalize predicates to SPEC-004 model:
     - `[]` → `AllRows(table_id)`
     - `[P1]` → `ColEq(table_id, col_id, value)`
     - `[P1, P2, ...]` → left-associative `And` tree: `And{And{P1, P2}, P3}`
  7. Send `RegisterSubscriptionCmd` to executor inbox with callback for `SubscribeApplied` or `SubscriptionError`

- `SchemaLookup` interface — resolves table names to IDs and column names to IDs:
  ```go
  type SchemaLookup interface {
      TableByName(name string) (TableID, *TableSchema, bool)
  }
  ```

- Predicate normalization helpers:
  ```go
  func NormalizePredicate(tableID TableID, schema *TableSchema, preds []Predicate) (spec004.Predicate, error)
  ```

## Acceptance Criteria

- [ ] Valid subscribe → `subscription_id` reserved as pending
- [ ] Duplicate active `subscription_id` → `SubscriptionError` with `ErrDuplicateSubscriptionID`
- [ ] Duplicate pending `subscription_id` → `SubscriptionError`
- [ ] Unknown table → `SubscriptionError`
- [ ] Unknown column → `SubscriptionError`
- [ ] Empty predicates → normalized to `AllRows`
- [ ] Single predicate → normalized to `ColEq`
- [ ] Three predicates `[P1, P2, P3]` → `And{And{ColEq(P1), ColEq(P2)}, ColEq(P3)}`
- [ ] Range predicate → `SubscriptionError` (not v1)
- [ ] `RegisterSubscriptionCmd` sent to executor on success
- [ ] Executor responds with error → `SubscriptionError` sent, subscription_id released

## Design Notes

- The v1 wire protocol uses string table/column names. The protocol layer resolves these to internal IDs via `SchemaLookup` before constructing SPEC-004 predicates. This decouples wire format from internal representation.
- Predicate normalization produces a left-associative `And` tree per spec. The outermost predicate is the rightmost element.
- `SchemaLookup` is read-only. The protocol layer never mutates schema.
