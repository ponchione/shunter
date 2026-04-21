# P0-04 Streamed Snapshot Read Path Implementation Plan

Goal: Replace `ReadSnapshot`'s whole-file `os.ReadFile` path with a streaming decoder that validates the header and Blake3 hash while reading from the snapshot file handle, reducing peak memory and avoiding repeated schema lookups.

Architecture:
- Keep `ReadSnapshot(dir string)` as the public API and preserve snapshot format and returned `SnapshotData` shape.
- Read from an `*os.File` via `io.Reader` primitives instead of loading the entire file into a `[]byte`.
- Parse the fixed header directly from the stream, tee the post-hash payload through a Blake3 hasher, and compare the computed hash after decoding completes.
- Replace repeated linear `findTableSchema` scans with a one-time `map[schema.TableID]*schema.TableSchema` built from the decoded schema.
- Reuse a bounded row scratch buffer across rows to avoid fresh row allocation churn where practical.

Grounded context:
- `commitlog/snapshot_io.go:394-502` currently uses `os.ReadFile`, `bytes.NewReader`, per-row `make([]byte, rowLen)`, and repeated `findTableSchema` scans.
- `docs/performance-audit-punchlist-2026-04-20.md:150-185` identifies this as the next P0 slice and explicitly calls for reader-based parsing, bounded scratch buffers, and one-time schema lookup.
- `docs/decomposition/002-commitlog/epic-5-snapshot-io/story-5.3-snapshot-reader.md` requires format/behavior preservation, including hash verification, schema_len handling, sequence/nextID decode, and row decode using BSATN.
- `bsatn.DecodeProductValueFromBytes` is available and preserves row-length mismatch semantics, so row scratch reuse can stay compatible without rewriting BSATN decoding.

Files likely to change:
- Modify: `commitlog/snapshot_io.go`
- Modify: `commitlog/snapshot_test.go`

Non-goals:
- Do not change snapshot on-disk format.
- Do not change snapshot writer behavior in this slice.
- Do not change recovery semantics beyond using the improved `ReadSnapshot` path.

## Task 1: Add/extend tests first

Objective: lock in large-snapshot streamed-read behavior before modifying production code.

Files:
- Modify: `commitlog/snapshot_test.go`

Steps:
1. Add a large snapshot read regression test using the existing large committed-state helper.
2. Exercise a snapshot with enough rows to make row-by-row streaming meaningful.
3. Assert round-trip correctness, including row counts and representative row contents.
4. Keep existing hash mismatch and round-trip tests green.

Validation:
- `rtk go test ./commitlog -run 'Test(CreateAndReadSnapshotRoundTrip|ReadSnapshotHashMismatch|SnapshotLarge.*)' -count=1`

## Task 2: Implement streaming `ReadSnapshot`

Objective: decode directly from the file handle while hashing and reduce repeated allocations/lookups.

Files:
- Modify: `commitlog/snapshot_io.go`

Implementation outline:
1. Open `{dir}/snapshot` with `os.Open`.
2. Read and validate the fixed 52-byte header directly from the file:
   - magic
   - version
   - txID
   - schemaVersion
   - expected hash
3. Wrap the remaining file reader in `io.TeeReader(file, hasher)` so all payload bytes are hashed as they are decoded.
4. Decode `schema_len`, then read schema bytes only for that section and call `DecodeSchemaSnapshot`.
5. Build a `map[schema.TableID]*schema.TableSchema` once from decoded schema.
6. Decode sequences and nextIDs from the streaming reader.
7. For table rows:
   - read row length
   - resize/reuse a single scratch `[]byte` buffer when possible
   - fill only the needed prefix with `io.ReadFull`
   - decode the row with `bsatn.DecodeProductValueFromBytes(buf[:rowLen], ts)`
8. After payload decode completes successfully, compare computed Blake3 against header hash and return `ErrSnapshotHashMismatch` on mismatch.
9. Preserve EOF/error behavior for truncated snapshots.
10. Remove `findTableSchema` if no longer used.

## Task 3: Verify broadly

Objective: prove snapshot read/recovery paths still pass.

Validation steps:
1. `rtk go fmt ./commitlog`
2. `rtk go test ./commitlog -run 'Test(CreateAndReadSnapshotRoundTrip|ReadSnapshotHashMismatch|SnapshotLarge.*|OpenAndRecover.*|SelectSnapshot.*)' -count=1`
3. `rtk go test ./commitlog -count=1`

Expected outcome:
- All snapshot/recovery/select tests stay green.
- `ReadSnapshot` no longer performs whole-file loading or repeated schema scans.

Risks / notes:
- Hash verification now occurs after streamed decode rather than before any decode work; this preserves correctness and lowers memory, but truncated/corrupt files may fail during decode before hash compare. That remains within the story acceptance criteria (`hash verification fails or EOF`).
- Schema bytes still need a temporary buffer because the schema section is length-prefixed and decoded by existing code; this is acceptable since the main win is removing whole-file buffering.