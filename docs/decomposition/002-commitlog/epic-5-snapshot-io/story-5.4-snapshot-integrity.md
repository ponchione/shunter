# Story 5.4: Snapshot Integrity & Error Types

**Epic:** [Epic 5 — Snapshot I/O](EPIC.md)  
**Spec ref:** SPEC-002 §5.4, §5.5, §9  
**Depends on:** Nothing  
**Blocks:** Stories 5.2, 5.3

---

## Summary

Blake3 hash utilities, lockfile detection, and snapshot-specific error types.

## Deliverables

- Blake3 utilities:
  - `func ComputeSnapshotHash(data []byte) [32]byte`
  - Uses `lukechampine.com/blake3` or `github.com/zeebo/blake3`

- Lockfile helpers:
  - `func HasLockFile(snapshotDir string) bool`
  - `func CreateLockFile(snapshotDir string) error`
  - `func RemoveLockFile(snapshotDir string) error`

- Snapshot constants:
  ```go
  var SnapshotMagic = [4]byte{'S', 'H', 'S', 'N'}
  const SnapshotVersion uint8 = 1
  const SnapshotHeaderSize = 52  // 4 + 1 + 3 + 8 + 4 + 32
  ```

- Error types:

| Error | Type | Trigger |
|---|---|---|
| `ErrSnapshotIncomplete` | sentinel | .lock file present |
| `ErrSnapshotHashMismatch` | struct | Blake3 hash doesn't match |
| `ErrSnapshotInProgress` | sentinel | CreateSnapshot called while another snapshot is already running |

  - `ErrSnapshotHashMismatch` fields: `Expected [32]byte`, `Got [32]byte`

## Acceptance Criteria

- [ ] ComputeSnapshotHash deterministic for same input
- [ ] Different input → different hash
- [ ] HasLockFile detects .lock presence
- [ ] CreateLockFile + HasLockFile → true
- [ ] RemoveLockFile + HasLockFile → false
- [ ] Error types satisfy `error` interface
- [ ] ErrSnapshotHashMismatch message includes hex-encoded hash prefix for debugging
- [ ] ErrSnapshotInProgress is returned for concurrent snapshot attempts

## Design Notes

- Blake3 chosen for speed (GB/s on modern hardware) and collision resistance. 32-byte fixed output.
- Lockfile is a simple empty file. No PID, no advisory locks. If it exists, the snapshot is incomplete.
- Header size calculation: magic(4) + version(1) + pad(3) + tx_id(8) + schema_version(4) + hash(32) = 52 bytes. Verify against spec.
