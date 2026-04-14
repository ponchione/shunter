# Story 7.2: Snapshot Concurrency

**Epic:** [Epic 7 — Read-Only Snapshots](EPIC.md)  
**Spec ref:** SPEC-001 §7.2  
**Depends on:** Story 7.1, Story 6.2 (Commit)  
**Blocks:** Nothing

---

## Summary

Verify the RLock/WLock interaction between snapshots and commits. Primarily a **concurrency test story**.

## Deliverables

No new production code. Test suite verifying:

- Multiple concurrent snapshots coexist (multiple RLocks)
- Commit blocks while any snapshot is open (write lock contention with held read locks)
- Commit proceeds after all snapshots close
- Snapshot taken after commit sees post-commit state
- Snapshot taken before commit sees pre-commit state even after commit completes
- Tests/documentation coverage that callers materialize rows and close snapshots before blocking work; this is the operational rule that keeps the RLock design viable in v1

## Acceptance Criteria

- [ ] Two goroutines both hold snapshots simultaneously — no deadlock
- [ ] Goroutine A holds snapshot, goroutine B calls Commit → B blocks
- [ ] Goroutine A closes snapshot → B's Commit proceeds
- [ ] Snapshot before commit: TableScan returns pre-commit rows
- [ ] Snapshot after commit: TableScan returns post-commit rows
- [ ] Rapid open/close snapshot cycles under concurrent commits — no races (pass `-race`)
- [ ] Snapshot held briefly (simulate materialize + close) — commit latency bounded
- [ ] Documentation or test helper used by the snapshot tests models the intended pattern: materialize rows, close snapshot, then do downstream work

## Design Notes

- These tests use `go test -race` to catch data races.
- The "commit blocks while snapshot open" behavior is the fundamental tradeoff of the RLock approach. Tests should verify it works correctly, not that it's fast — performance characteristics are for profiling, not unit tests.
- Operational rule from spec: callers MUST NOT hold snapshots during I/O or blocking work. This story should at least verify the intended short-lived usage pattern and ensure the rule is documented where snapshot callers see it.
