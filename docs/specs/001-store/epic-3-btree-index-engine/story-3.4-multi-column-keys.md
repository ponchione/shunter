# Story 3.4: Multi-Column Key Support

**Epic:** [Epic 3 — B-Tree Index Engine](EPIC.md)  
**Spec ref:** SPEC-001 §4.3  
**Depends on:** Story 3.2  
**Blocks:** Epic 4 (multi-column index wiring)

---

## Summary

Verify and test that IndexKey + BTreeIndex correctly handle compound keys with 2+ columns.

## Deliverables

This story is primarily a **test and validation story**. The data structures (IndexKey with `[]Value` parts, lexicographic Compare) already support multi-column keys by design. This story:

- Adds comprehensive tests for multi-column scenarios
- Documents key extraction: given a row and a list of column indices, produce an IndexKey
- `func ExtractKey(row ProductValue, columns []int) IndexKey` — utility to build IndexKey from row + column indices

## Acceptance Criteria

- [ ] 2-column key: `(A,1) < (A,2) < (B,1)` — lexicographic on first column, then second
- [ ] 3-column key: correct ordering across all three positions
- [ ] Seek on 2-column key finds exact match only
- [ ] Range scan on 2-column keys: `[(A,1), (A,3))` yields `(A,1)` and `(A,2)` but not `(A,3)`
- [ ] ExtractKey with column indices [0,2] from a 3-column row produces correct 2-part key
- [ ] ExtractKey with single column index produces 1-part key (backward compat)
- [ ] Bytes columns in compound keys use raw lexicographic ordering

## Design Notes

- `ExtractKey` is a pure function: `row[columns[0]], row[columns[1]], ...` → IndexKey. Simple but needs to exist as a named utility for Epic 4 index maintenance.
- Single-column indexes still produce 1-element IndexKey. No special case.
