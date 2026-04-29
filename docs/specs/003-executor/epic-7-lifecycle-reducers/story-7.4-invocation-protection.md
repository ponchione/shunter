# Story 7.4: Direct Invocation Protection (Verification)

**Epic:** [Epic 7 — Lifecycle Reducers & Client Management](EPIC.md)  
**Spec ref:** SPEC-003 §10.7  
**Depends on:** Story 4.1 (implements the guard), Stories 7.2, 7.3  
**Blocks:** Nothing

---

## Summary

Verification-only story. The lifecycle guard is implemented in Story 4.1 (begin phase). This story adds integration tests that exercise the guard in the context of the full lifecycle flow — verifying that external calls are rejected while internal OnConnect/OnDisconnect calls succeed.

## Deliverables

- Integration tests in `executor/lifecycle_test.go` covering the guard with real lifecycle reducers registered and real executor dispatch.

- No new production code. The guard implementation lives in `handleCallReducer` (Story 4.1):
  ```go
  if reducer.Lifecycle != LifecycleNone && req.Source != CallSourceLifecycle {
      // respond ErrLifecycleReducer
  }
  ```

## Acceptance Criteria

- [ ] External `CallReducerCmd` naming "OnConnect" → `ErrLifecycleReducer`, no transaction
- [ ] External `CallReducerCmd` naming "OnDisconnect" → `ErrLifecycleReducer`, no transaction
- [ ] Internal call (CallSourceLifecycle) to "OnConnect" → allowed, reducer executes
- [ ] Internal call (CallSourceLifecycle) to "OnDisconnect" → allowed, reducer executes
- [ ] External call to normal reducer → not affected by guard
- [ ] Guard checked before transaction begin (no Transaction allocation on reject)

## Design Notes

- This is a verification story, not an implementation story. The guard is a single `if` in Story 4.1. This story exists because the requirement spans two epics (Epic 4 implements, Epic 7 validates in lifecycle context) and the integration tests belong with the lifecycle test suite.
- Keeping the implementation in Epic 4 avoids splitting the actual guard across two places; Epic 7 only proves the lifecycle-specific behavior end-to-end.
