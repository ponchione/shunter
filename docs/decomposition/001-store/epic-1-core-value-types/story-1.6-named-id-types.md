# Story 1.6: Named ID Types

**Epic:** [Epic 1 — Core Value Types](EPIC.md)  
**Spec ref:** SPEC-001 §2.3, §2.4, §2.5  
**Depends on:** Nothing  
**Blocks:** Epic 2 (Schema & Table Storage)

---

## Summary

Simple named types for type safety in function signatures.

## Deliverables

- `type RowID uint64` — row identifier within a table
- `type Identity [32]byte` — canonical client identifier
- `type ColID int` — zero-based column index

## Acceptance Criteria

- [ ] RowID: assignable from uint64, comparable, usable as map key
- [ ] Identity: comparable, usable as map key, zero value is 32 zero bytes
- [ ] ColID: assignable from int, usable as slice index

## Design Notes

- These are trivial but exist as their own story because they're cross-cutting types referenced everywhere. Ship them early so other stories can import them.
- Debug-oriented `String()` methods are optional convenience only; they are not part of the spec contract for these types.
