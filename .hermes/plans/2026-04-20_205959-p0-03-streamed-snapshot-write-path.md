# P0-03 Streamed Snapshot Write Path Implementation Plan

> For Hermes: execute this plan directly in the current repo after saving it; keep the snapshot file format unchanged.

Goal: Replace the current full-buffer snapshot write path with a streaming writer that hashes and writes incrementally, reducing peak memory and copy churn without changing the on-disk snapshot format.

Architecture:
- Keep `FileSnapshotWriter.CreateSnapshot` as the public entrypoint and preserve its locking, lockfile, fsync, and deterministic ordering behavior.
- Move snapshot body assembly from `buildSnapshotContent` into a streaming pipeline that writes header + body directly to the final file while computing the Blake3 hash over the bytes after the hash field.
- Keep `ReadSnapshot` unchanged for this slice; only the writer path changes.

Tech stack: Go, stdlib `io`/`encoding/binary`/`os`, existing Blake3 dependency, existing BSATN encoder.

---

Current context / grounded findings
- `commitlog/snapshot_io.go:209-271` writes the file only after `buildSnapshotContent` returns a fully materialized `[]byte`.
- `commitlog/snapshot_io.go:274-363` currently allocates:
  - one full `body` buffer,
  - one schema buffer,
  - one output buffer containing header + body,
  - one fresh row buffer per row before copying row bytes into `body`.
- Specs explicitly allow either “build in memory” or “streaming to temp file” as long as format and crash-safety ordering stay intact (`docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.2-snapshot-writer.md`).
- Existing tests cover round-trip, sequence omission, hash mismatch, and concurrent snapshot rejection in `commitlog/snapshot_test.go`.
- There is no snapshot benchmark yet.

Files likely to change
- Modify: `commitlog/snapshot_io.go`
- Modify: `commitlog/snapshot_test.go`

Non-goals
- Do not change snapshot file layout or header semantics.
- Do not change snapshot read path in this slice.
- Do not change snapshot triggering/orchestration behavior.

---

## Task 1: Add regression/perf-oriented tests first

Objective: Lock down format stability and add a measurable large-snapshot exercise before changing implementation.

Files:
- Modify: `commitlog/snapshot_test.go`

Steps:
1. Add a helper that builds a larger committed state with deterministic row contents.
2. Add a regression test that writes a larger snapshot and verifies:
   - snapshot round-trips through `ReadSnapshot`
   - the header/body are still valid
   - table row counts match expected values
3. Add a benchmark that repeatedly calls `CreateSnapshot` on a large committed state and reports allocations.
4. Run the new targeted test/benchmark first to ensure the test exists and benchmark compiles against the old implementation.

Validation command:
- `rtk go test ./commitlog -run 'Test(CreateAndReadSnapshotRoundTrip|SnapshotLarge.*)' -count=1`
- `rtk go test ./commitlog -run '^$' -bench BenchmarkCreateSnapshotLarge -benchmem -count=1`

---

## Task 2: Replace full-buffer assembly with a streaming snapshot encoder

Objective: Stream bytes to the file while hashing, and avoid per-row fresh buffer allocations where practical.

Files:
- Modify: `commitlog/snapshot_io.go`

Implementation outline:
1. Open the final snapshot file before serialization.
2. Write the fixed 52-byte header immediately:
   - magic
   - version + padding
   - txID
   - schema version
   - 32-byte zero hash placeholder
3. Create a Blake3 hasher and a writer that tees payload bytes to both the file and the hasher.
4. Replace `buildSnapshotContent` with a streaming helper, e.g. `writeSnapshotBody(dst io.Writer, committed *store.CommittedState) error`, which writes:
   - `schema_len` + schema bytes
   - sequence section
   - nextID section
   - table section with deterministic row order
5. Avoid copying row bytes twice by:
   - reusing a single `bytes.Buffer` for row encoding,
   - resetting it for each row,
   - writing row length + bytes directly to the streaming writer.
6. After the body is fully written, compute the final hash from the hasher state.
7. Seek back to the hash field offset, patch the 32-byte hash, then seek to the end if needed before sync/close.
8. Keep existing lockfile/fsync ordering unchanged.
9. Remove obsolete full-buffer helper(s) or convert them into streaming helpers if still useful.

Key invariants:
- Hash must still cover all bytes after the hash field.
- Output bytes must remain deterministic for the same committed state.
- `CreateSnapshot` still holds the same snapshot exclusivity behavior.

---

## Task 3: Verify correctness and formatting

Objective: Prove the writer still satisfies existing contracts and capture benchmark results.

Files:
- Modify only if failures require narrow follow-up fixes.

Validation steps:
1. Format touched files:
   - `rtk go fmt ./commitlog`
2. Run targeted snapshot tests:
   - `rtk go test ./commitlog -run 'Test(CreateAndReadSnapshotRoundTrip|SnapshotOmitsSequenceEntriesForTablesWithoutAutoincrement|ReadSnapshotHashMismatch|ConcurrentSnapshotReturnsInProgress|SnapshotLarge.*)' -count=1`
3. Run the full commitlog package tests:
   - `rtk go test ./commitlog -count=1`
4. Run the large benchmark once for allocation/throughput comparison:
   - `rtk go test ./commitlog -run '^$' -bench BenchmarkCreateSnapshotLarge -benchmem -count=1`

Expected outcome:
- All snapshot tests remain green.
- Benchmark compiles and provides a baseline for lower allocation pressure after the change.

Risks / tradeoffs
- Streaming directly to the final file means partial files exist until lock removal; this is already the contract via `.lock` and must remain intact.
- Schema bytes still need temporary buffering to prefix `schema_len`; that is acceptable because the dominant problem is whole-snapshot buffering, not the comparatively small schema section.
- Deterministic output depends on preserving existing table/row ordering logic exactly.

Open questions resolved for execution
- Use final-file streaming + hash backpatching, not temp-file rename; it preserves current crash-safety behavior and requires the smallest surface change.
- Keep `ReadSnapshot` untouched; P0-04 is the separate read-path streaming slice.
