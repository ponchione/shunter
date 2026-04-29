# Epic 1: Core Value Types

**Parent:** [SPEC-001-store.md](../SPEC-001-store.md) §2  
**Blocked by:** Nothing — leaf epic  
**Blocks:** Epic 2 (Schema & Table Storage), Epic 3 (B-Tree Index Engine)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 1.1 | [story-1.1-valuekind-value-struct.md](story-1.1-valuekind-value-struct.md) | ValueKind enum, Value tagged union, constructors, accessors |
| 1.2 | [story-1.2-value-equality.md](story-1.2-value-equality.md) | Value.Equal() per §2.2 rules |
| 1.3 | [story-1.3-value-ordering.md](story-1.3-value-ordering.md) | Value.Compare() total order for B-tree |
| 1.4 | [story-1.4-value-hashing.md](story-1.4-value-hashing.md) | Value.Hash() for set-semantics |
| 1.5 | [story-1.5-product-value.md](story-1.5-product-value.md) | ProductValue type, row equality/hashing/copy |
| 1.6 | [story-1.6-named-id-types.md](story-1.6-named-id-types.md) | RowID, Identity, ColID |

## Implementation Order

```
Story 1.1 (ValueKind + Value)
  ├── Story 1.2 (Equality)
  ├── Story 1.3 (Ordering)
  └── Story 1.4 (Hashing)
        └── Story 1.5 (ProductValue)
Story 1.6 (Named IDs) — independent, parallel with any
```

## Suggested Files

| Story | Go file(s) |
|---|---|
| 1.1–1.4 | `value.go`, `value_test.go` |
| 1.5 | `product_value.go`, `product_value_test.go` |
| 1.6 | `types.go` |
