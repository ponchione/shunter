# Story 1.2: Predicate Validation

**Epic:** [Epic 1 — Predicate Types & Query Hash](EPIC.md)
**Spec ref:** SPEC-004 §3.3, §4.1 (step 1)
**Depends on:** Story 1.1
**Blocks:** Epic 4 (Subscription Manager — validation on register)

---

## Summary

Validate predicate constraints before a subscription is accepted. Reject early with clear errors.

## Deliverables

- `ValidatePredicate(pred Predicate, schema SchemaLookup) error`

- `SchemaLookup` interface (or function type) for checking table/column/index existence plus name resolution used by later wire/debug paths:
  ```go
  type SchemaLookup interface {
      TableExists(TableID) bool
      ColumnExists(TableID, ColID) bool
      ColumnType(TableID, ColID) ValueKind
      HasIndex(TableID, ColID) bool
      // TableName returns the declared name for wire/debug use
      // (subscription errors, fan-out labels). Empty string is
      // acceptable when the caller does not carry names. Not consulted
      // by validation itself.
      TableName(TableID) string
  }
  ```

- Validation rules:
  1. **Table count**: `pred.Tables()` has at most 2 entries → `ErrTooManyTables` if >2
  2. **Table existence**: every table in `Tables()` exists in schema → `ErrTableNotFound`
  3. **Column existence**: every column referenced exists → `ErrColumnNotFound`
  4. **Type match**: literal `Value` kind matches column type → `ErrInvalidPredicate`
  5. **Join index**: `Join` predicate requires `HasIndex(Left, LeftCol)` or `HasIndex(Right, RightCol)` → `ErrUnindexedJoin`
  6. **Literal values only**: no column-to-column references outside join conditions → `ErrInvalidPredicate`

- Error types:
  - `ErrTooManyTables` — predicate spans >2 tables
  - `ErrUnindexedJoin` — join column has no index on either side
  - `ErrInvalidPredicate` — type mismatch or non-literal reference
  - `ErrTableNotFound` — predicate references missing table
  - `ErrColumnNotFound` — predicate references missing column

## Acceptance Criteria

- [ ] Single-table `ColEq` with valid schema → nil error
- [ ] `And` combining 3 different tables → `ErrTooManyTables`
- [ ] `Join` with index on left column → valid
- [ ] `Join` with index on right column → valid
- [ ] `Join` with no index on either column → `ErrUnindexedJoin`
- [ ] `ColEq` referencing nonexistent table → `ErrTableNotFound`
- [ ] `ColEq` referencing nonexistent column → `ErrColumnNotFound`
- [ ] `ColEq` with value kind mismatching column type → `ErrInvalidPredicate`
- [ ] `ColRange` bounds type mismatch → `ErrInvalidPredicate`
- [ ] Nested `And` with valid predicates → valid

## Design Notes

- `SchemaLookup` is a narrow read-only interface. During registration, the executor provides this from committed state. The validation function does not need full store access, and does not consult `TableName` — that method exists so the same lookup serves eval-time code paths (SubscriptionError, FanOut labels) that need a display name per table.
- `ErrTableNotFound` and `ErrColumnNotFound` are introduced here and then reused by registration and interface-contract stories; Epic 4 should not be read as re-introducing them.
- Validation is O(predicate_size), which is small (bounded by the 2-table constraint).
