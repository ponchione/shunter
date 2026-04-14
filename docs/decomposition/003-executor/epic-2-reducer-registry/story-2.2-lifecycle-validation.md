# Story 2.2: Lifecycle Validation & Freeze

**Epic:** [Epic 2 — Reducer Registry](EPIC.md)  
**Spec ref:** SPEC-003 §3.2, §10.1  
**Depends on:** Story 2.1  
**Blocks:** Epic 3

---

## Summary

Lifecycle reducer name reservation and registry immutability after startup.

## Deliverables

- Reserved lifecycle names:
  ```go
  var lifecycleNames = map[string]LifecycleKind{
      "OnConnect":    LifecycleOnConnect,
      "OnDisconnect": LifecycleOnDisconnect,
  }
  ```

- Register validation rules:
  - If `reducer.Lifecycle == LifecycleNone` and name is in `lifecycleNames` → error (cannot register normal reducer with reserved name)
  - If `reducer.Lifecycle != LifecycleNone` and name does NOT match expected lifecycle name → error
  - At most one OnConnect and one OnDisconnect reducer

- `func (r *ReducerRegistry) Freeze()`
  - Sets `frozen = true`
  - All subsequent `Register` calls return error

- `func (r *ReducerRegistry) LookupLifecycle(kind LifecycleKind) (*RegisteredReducer, bool)`
  - Shortcut for looking up lifecycle reducers by kind rather than name

## Acceptance Criteria

- [ ] Register normal reducer named "OnConnect" → error
- [ ] Register lifecycle reducer with `LifecycleOnConnect` and name "OnConnect" → accepted
- [ ] Register lifecycle reducer with wrong name for its kind → error
- [ ] Register two OnConnect reducers → error (duplicate name)
- [ ] LookupLifecycle(LifecycleOnConnect) returns registered OnConnect reducer
- [ ] LookupLifecycle for unregistered kind → not found
- [ ] Freeze then Register → error
- [ ] Freeze is idempotent

## Design Notes

- Lifecycle reducers are optional. An application with no OnConnect/OnDisconnect reducers is valid — the executor still manages `sys_clients` rows without calling any reducer.
- Name-to-kind mapping is hardcoded. v2 could make this extensible, but v1 only supports OnConnect and OnDisconnect.
