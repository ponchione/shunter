# Story 2.1: Reducer Registry

**Epic:** [Epic 2 — Reducer Registry](EPIC.md)  
**Spec ref:** SPEC-003 §3.2  
**Depends on:** Epic 1 (RegisteredReducer, LifecycleKind)  
**Blocks:** Story 2.2

---

## Summary

Registration map for reducers. Name uniqueness enforced at registration time. Lookup by name for dispatch.

## Deliverables

- ```go
  type ReducerRegistry struct {
      reducers map[string]*RegisteredReducer
      frozen   bool
  }
  ```

- `func NewReducerRegistry() *ReducerRegistry`

- `func (r *ReducerRegistry) Register(reducer RegisteredReducer) error`
  - Reject if name already registered → error
  - Reject if registry is frozen → error
  - Store by name

- `func (r *ReducerRegistry) Lookup(name string) (*RegisteredReducer, bool)`
  - Return registered reducer and true, or nil and false

- `func (r *ReducerRegistry) All() []*RegisteredReducer`
  - Return all registered reducers (used by schema introspection)

## Acceptance Criteria

- [ ] Register reducer, Lookup by name → found
- [ ] Register duplicate name → error
- [ ] Lookup non-existent name → not found
- [ ] Register after Freeze → error
- [ ] All() returns every registered reducer
- [ ] Zero-value registry is not usable (must call NewReducerRegistry)

## Design Notes

- Registry is not concurrent-safe after Freeze. All registration happens single-threaded at startup before the executor goroutine starts. Lookup is safe for concurrent reads after Freeze because the map is immutable.
