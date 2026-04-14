# Story 4.2: Execute Phase

**Epic:** [Epic 4 — Reducer Transaction Lifecycle](EPIC.md)  
**Spec ref:** SPEC-003 §3.4, §3.5, §4.3  
**Depends on:** Story 4.1  
**Blocks:** Stories 4.3, 4.4

---

## Summary

Call the reducer handler inside a panic recovery block. Capture result, error, or panic for the commit/rollback decision.

## Deliverables

- Execute algorithm:
  ```go
  var (
      ret     []byte
      err     error
      panicked any
  )

  func() {
      defer func() {
          if r := recover(); r != nil {
              panicked = r
          }
      }()
      ret, err = reducer.Handler(ctx, req.Args)
  }()
  ```

- Decision routing:
  - `panicked != nil` → go to rollback (Story 4.4) with `StatusFailedPanic`
  - `err != nil` → go to rollback (Story 4.4) with `StatusFailedUser`
  - Neither → go to commit (Story 4.3)

- Reducer execution invariants owned by this phase:
  - reducer execution is synchronous on the executor goroutine
  - reducer code has no executor API for synchronous nested reducer invocation
  - `ReducerContext`, `Transaction`, iterators, snapshots, row references, and scheduler internals become invalid immediately after the handler returns
  - transaction-owned objects are executor-goroutine-only and must not be used from another goroutine
  - reducers must not perform blocking network, disk, or RPC I/O on the executor goroutine
  - background work must not capture `ReducerContext` or `Transaction`

- Documentation / guardrails:
  - add Go doc comments on `ReducerContext` and reducer execution helpers describing the lifetime and goroutine constraints above
  - keep reducer execution surface minimal so reducers cannot recursively invoke other reducers through `ReducerContext`

## Acceptance Criteria

- [ ] Reducer returns (ret, nil) → proceed to commit
- [ ] Reducer returns (nil, err) → proceed to rollback, err captured
- [ ] Reducer panics → panicked captured, executor goroutine not killed
- [ ] Panic recovery catches all panic types (string, error, arbitrary value)
- [ ] After panic recovery, Transaction is not committed
- [ ] ret bytes are preserved for commit path response
- [ ] Executor/reducer API exposes no synchronous nested reducer-call surface from inside `ReducerContext`
- [ ] Go doc comments on `ReducerContext` / reducer execution explicitly forbid retain-after-return and cross-goroutine use
- [ ] Typed-adapter argument decode failures from SPEC-006 surface as ordinary reducer errors for rollback/status mapping
- [ ] Reducer execution docs explicitly forbid blocking network/disk/RPC I/O and background capture of transaction-owned objects

## Design Notes

- The panic recovery here is the inner layer — it handles expected reducer misbehavior. The outer `dispatchSafely` layer (Story 3.2) handles unexpected executor infrastructure panics.
- Reducer return value (`ret`) is BSATN-encoded bytes. The executor does not interpret it — it's passed through to ReducerResponse.ReturnBSATN.
- The no-I/O and no-retain rules are programming constraints, not hard sandbox guarantees. The decomposition still needs an explicit owner for them so implementation docs and reviews enforce them consistently.
