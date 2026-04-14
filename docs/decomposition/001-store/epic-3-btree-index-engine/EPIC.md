# Epic 3: B-Tree Index Engine

**Parent:** [SPEC-001-store.md](../SPEC-001-store.md) §4.1–§4.4, §4.6  
**Blocked by:** Epic 1 (Value types, ordering)  
**Blocks:** Epic 4 (Table Indexes & Constraints)

---

## Stories

| Story | File | Summary |
|---|---|---|
| 3.1 | [story-3.1-index-key.md](story-3.1-index-key.md) | IndexKey struct, lexicographic comparison, Bound type |
| 3.2 | [story-3.2-btree-index.md](story-3.2-btree-index.md) | BTreeIndex: insert, remove, point lookup |
| 3.3 | [story-3.3-range-scan.md](story-3.3-range-scan.md) | SeekRange with Bound semantics, full Scan |
| 3.4 | [story-3.4-multi-column-keys.md](story-3.4-multi-column-keys.md) | Compound key ordering, multi-column seek/range |

## Implementation Order

```
Story 3.1 (IndexKey + Bound)
  └── Story 3.2 (BTreeIndex core)
        ├── Story 3.3 (Range scan)
        └── Story 3.4 (Multi-column keys)
```

3.3 and 3.4 are independent of each other, both depend on 3.2.

## Suggested Files

| Story | Go file(s) |
|---|---|
| 3.1 | `index_key.go`, `index_key_test.go` |
| 3.2–3.4 | `btree_index.go`, `btree_index_test.go` |
