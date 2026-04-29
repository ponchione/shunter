# Story 1.1: Predicate Interface & Concrete Types

**Epic:** [Epic 1 — Predicate Types & Query Hash](EPIC.md)
**Spec ref:** SPEC-004 §3.1, §3.2, §3.3, §12.1
**Depends on:** Nothing
**Blocks:** Stories 1.2, 1.3

---

## Summary

The structured expression tree that describes subscription filters. Every downstream subsystem (pruning, delta computation, validation) inspects these types, so the v1 query language is an inspectable Go predicate builder rather than opaque callbacks.

## Deliverables

- v1 query-language contract: subscriptions are expressed as inspectable predicate structs built with the Go predicate builder API; opaque Go callback predicates are explicitly unsupported because they defeat pruning (§3.1, §12.1)

- `Predicate` sealed interface:
  ```go
  type Predicate interface {
      Tables() []TableID
      sealed()
  }
  ```

- Concrete predicate types:
  - `ColEq` — column equals literal value. Fields: `Table TableID`, `Column ColID`, `Value Value`
  - `ColRange` — column within range. Fields: `Table TableID`, `Column ColID`, `Lower Bound`, `Upper Bound`
  - `And` — conjunction. Fields: `Left Predicate`, `Right Predicate`
  - `AllRows` — unfiltered table scan. Fields: `Table TableID`
  - `Join` — equi-join on column pair with optional filter. Fields: `Left TableID`, `Right TableID`, `LeftCol ColID`, `RightCol ColID`, `Filter Predicate`

- `Bound` struct for range predicates:
  ```go
  type Bound struct {
      Value     Value
      Inclusive bool
      Unbounded bool  // if true, Value and Inclusive ignored
  }
  ```

- Each concrete type implements `Tables()`:
  - `ColEq`, `ColRange`, `AllRows` → `[]TableID{t.Table}`
  - `And` → deduplicated union of left + right tables
  - `Join` → `[]TableID{t.Left, t.Right}`

- Each concrete type implements `sealed()` (unexported, prevents external implementations)

## Acceptance Criteria

- [ ] `ColEq.Tables()` returns single table
- [ ] `ColRange.Tables()` returns single table
- [ ] `AllRows.Tables()` returns single table
- [ ] `And.Tables()` returns deduplicated union (1 or 2 tables)
- [ ] `Join.Tables()` returns both tables
- [ ] `Join` with nil Filter is valid
- [ ] `Bound{Unbounded: true}` — Value field ignored
- [ ] Sealed interface prevents external implementation (compile-time check)
- [ ] Registration contract accepts structured predicate values, not opaque `func(ProductValue) bool` callbacks

## Design Notes

- `sealed()` is a Go pattern: unexported method on exported interface. External packages cannot implement it.
- `And` can combine predicates on the same table (1 result) or two different tables (2 results). Three-table restriction is enforced in validation (Story 1.2), not here.
- `Join.Filter` is optional (nil means no additional filter beyond the join condition).
- The sealed interface and concrete exported predicate set are the enforcement point for “no opaque Go functions” from §3.1. If callers could supply arbitrary implementations, the evaluator could not rely on inspectable structure for pruning.
