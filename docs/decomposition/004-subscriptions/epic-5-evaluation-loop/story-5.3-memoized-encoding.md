# Story 5.3: Memoized Encoding

**Epic:** [Epic 5 — Evaluation Loop](EPIC.md)
**Spec ref:** SPEC-004 §7.4
**Depends on:** Story 5.1 (fanout assembly)
**Blocks:** Story 5.4 (benchmark / verification story consumes this path)

---

## Summary

When multiple clients share the same query, encode the delta once and share encoded bytes across all recipients.

## Deliverables

- `memoizedResult` struct:
  ```go
  type memoizedResult struct {
      binary []byte  // nil until first binary client needs it
      json   []byte  // nil until first JSON client needs it
  }
  ```

- `memoCache` per evaluation cycle: `map[QueryHash]*memoizedResult`

- Lazy encoding:
  - First binary client for a query triggers binary encoding → cached
  - First JSON client triggers JSON encoding → cached
  - Subsequent clients reuse cached bytes

- Cache lifecycle: created at start of `EvalTransaction`, discarded at end. Not persisted across transactions.

## Acceptance Criteria

- [ ] Two binary clients, same query → `binary` encoded once
- [ ] One binary + one JSON client, same query → each format encoded once
- [ ] Single client → no wasted encoding (only requested format computed)
- [ ] Cache cleared between transactions (no stale data)
- [ ] Encoded bytes shared by reference, not copied per client

## Design Notes

- Encoding format is defined by SPEC-005 (protocol layer), not here. The memoization layer calls into protocol encoding and caches the result.
- Lazy vs eager: lazy encoding avoids computing a format no client uses. If all clients are binary, JSON is never computed.
- Memory: cache holds encoded bytes for all affected queries in one transaction. For a transaction affecting 100 queries with average 1 KiB encoding, that's ~100 KiB — trivial.
- v2: could extend to delta compression (only send diff from previous delta). Out of scope for v1.
