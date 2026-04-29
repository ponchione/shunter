# Epic 1: Predicate Types & Query Hash

**Parent:** [SPEC-004-subscriptions.md](../SPEC-004-subscriptions.md) §3
**Blocked by:** Nothing — leaf epic
**Blocks:** Epic 2 (Pruning Indexes), Epic 3 (DeltaView & Delta Computation), Epic 4 (Subscription Manager)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 1.1 | [story-1.1-predicate-interface.md](story-1.1-predicate-interface.md) | Predicate sealed interface, concrete types (ColEq, ColRange, And, AllRows, Join), Bound |
| 1.2 | [story-1.2-predicate-validation.md](story-1.2-predicate-validation.md) | Validate predicate constraints: table count, index requirement, literal values |
| 1.3 | [story-1.3-query-hash.md](story-1.3-query-hash.md) | Canonical serialization, blake3 hash, parameterized vs non-parameterized |

## Implementation Order

```
Story 1.1 (Predicate types)
  ├── Story 1.2 (Validation)
  └── Story 1.3 (Query hash)
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 1.1 | `predicate.go`, `predicate_test.go` |
| 1.2 | `predicate_validate.go`, `predicate_validate_test.go` |
| 1.3 | `query_hash.go`, `query_hash_test.go` |
