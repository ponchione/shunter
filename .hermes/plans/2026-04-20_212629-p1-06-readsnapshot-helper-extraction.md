# P1-06 ReadSnapshot Helper Extraction Plan

Goal: Split `commitlog.ReadSnapshot` into smaller private helpers along its natural phases (header/hash, schema, metadata, rows) without changing snapshot behavior.

Architecture:
- Keep `ReadSnapshot(dir string)` as the only public entrypoint.
- Extract phase-specific private helpers inside `commitlog/snapshot_io.go` so the top-level function becomes orchestration-only.
- Preserve exact read order, error behavior, hash verification, and decoded `SnapshotData` output.

Tech stack: Go stdlib I/O/binary/os plus existing Blake3/BSATN helpers.

Grounded context:
- Audit slice `P1-06` explicitly calls out `commitlog.ReadSnapshot` as an oversized orchestration function and recommends extraction along natural boundaries: header / schema / row sections.
- Current `ReadSnapshot` in `commitlog/snapshot_io.go:394-533` mixes file opening, header parsing, hash verification, schema decode, metadata section decode, row section decode, schema lookup, and result assembly.
- Existing commitlog tests already exercise snapshot round-trip, hash mismatch, selection, and recovery behavior; this slice should rely on those tests since it is a no-behavior-change refactor.

Files likely to change:
- Modify: `commitlog/snapshot_io.go`

Non-goals:
- No snapshot format changes.
- No additional performance changes beyond clearer internal structure.
- No writer-path changes.

## Task 1: Refactor `ReadSnapshot` into semantic helpers

Objective: separate orchestration from phase logic.

Files:
- Modify: `commitlog/snapshot_io.go`

Helpers to extract:
1. `readSnapshotHeader(f *os.File) (...)`
   - opens/validates magic/version
   - reads txID/schemaVersion/expected hash
2. `verifySnapshotPayloadHash(f *os.File, expected [32]byte) error`
   - hashes bytes after the header and rewinds to payload start
3. `readSnapshotSchema(payload io.Reader) ([]schema.TableSchema, map[schema.TableID]*schema.TableSchema, error)`
4. `readSnapshotSequences(payload io.Reader) (map[schema.TableID]uint64, error)`
5. `readSnapshotNextIDs(payload io.Reader) (map[schema.TableID]uint64, error)`
6. `readSnapshotTables(payload io.Reader, schemaByID map[schema.TableID]*schema.TableSchema) ([]SnapshotTableData, error)`

Top-level `ReadSnapshot` should become a short coordinator that calls those helpers and assembles `SnapshotData`.

## Task 2: Format and validate

Validation steps:
1. `rtk go fmt ./commitlog`
2. `rtk go test ./commitlog -run 'Test(CreateAndReadSnapshotRoundTrip|ReadSnapshotHashMismatch|ReadSnapshotLargeRoundTrip|OpenAndRecover.*|SelectSnapshot.*)' -count=1`
3. `rtk go test ./commitlog -count=1`

Expected result:
- No behavior change.
- `ReadSnapshot` is materially shorter and easier to inspect.
- Existing snapshot/recovery/select tests remain green.
