# TD-116 allocation-discipline implementation plan

> For Hermes: execute this as a narrow SPEC-004 Epic 3 / Story 3.5 slice only.

Goal
- Close TD-116 by implementing the missing hot-path reuse promised by SPEC-004 §9.2 / Story 3.5 in the subscription delta/evaluation path.

Scope
- Stay inside `subscription/` plus the root `TECH-DEBT.md` status update.
- Do not touch unrelated dirty files.
- Preserve current public behavior; this is a performance/GC-discipline follow-through, not a semantic redesign.

Grounded findings
- `subscription/delta_view.go` currently allocates fresh `inserts`, `deletes`, `insertIdx`, and `deleteIdx` maps per `NewDeltaView(...)` call, and fresh per-table slices for copied rows.
- `subscription/placement.go` allocates a fresh `map[QueryHash]struct{}` on every `CollectCandidatesForTable(...)` call.
- `subscription/eval.go` allocates fresh candidate/distinct maps on every `collectCandidates(...)` call.
- `subscription/delta_pool.go` only pools bag-dedup scratch state today.
- Story 3.5 also requires pooled 4 KiB `[]byte` buffers with oversized buffers discarded.

Implementation approach
1. Add failing regression tests first.
2. Introduce pooled scratch state in `subscription/delta_pool.go` for:
   - byte buffers (`[]byte`, default cap 4096, oversized buffers discarded)
   - reusable `DeltaView` backing maps/slices/index maps
   - reusable candidate-set map and distinct-value scratch map
3. Refactor `NewDeltaView(...)` so it borrows pooled scratch, resets it, copies rows into retained-capacity slices, and builds indexes without fresh top-level map allocation.
4. Add release helpers so tests and evaluator code can return pooled scratch after use.
5. Refactor candidate collection so manager-level evaluation reuses one candidate set and one distinct-value map across transactions instead of allocating new ones per call.
6. Keep public helper behavior intact; if a pooled variant is needed internally, wrap it without breaking existing tests/callers.
7. Run targeted tests, then broader subscription/package tests.
8. Mark TD-116 resolved in `TECH-DEBT.md` with verification commands.

Likely files to change
- `subscription/delta_pool.go`
- `subscription/delta_view.go`
- `subscription/eval.go`
- `subscription/placement.go` (only if internal pooled helper is needed)
- `subscription/delta_view_test.go`
- `subscription/placement_test.go`
- `subscription/bench_test.go` or a new focused allocation test file
- `TECH-DEBT.md`

Validation plan
- `rtk go test ./subscription -run 'TestDeltaView|TestCollectCandidates|TestBufferPool'`
- `rtk go test ./subscription`
- `rtk go test ./...`

Main risks
- Returning pooled structures too early and handing callers aliased mutable slices/maps.
- Accidentally changing candidate ordering assumptions in tests.
- Over-optimizing by changing public APIs when internal release helpers are sufficient.

Decision
- Prefer a narrow internal pooling design that keeps exported signatures stable and makes the evaluator responsible for releasing pooled scratch it owns.