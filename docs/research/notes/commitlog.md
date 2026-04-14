# Commit Log — Deep Research Note

Research into SpacetimeDB's durability subsystem (`crates/commitlog/`, `crates/durability/`, `crates/snapshot/`). Extracts on-disk format, write path, recovery procedure, and snapshot design.

---

## 1. CRATE: `crates/commitlog/` — Write-Ahead Log

### 1.1 On-Disk Structure

The commit log is a directory of **segment files**. Each segment is a separate file named by its **minimum transaction offset** (e.g., segment starting at TX 0 is the first file; segment starting at TX 10000 is the next). Segments are discovered at startup by scanning the directory and sorting by offset.

One segment is the **active (head) segment**, open for writes. All others are **sealed (tail) segments**, read-only. Rotation triggers when the head exceeds `max_segment_size` (default: 1 GiB).

On rotation:
1. Flush and fsync the current head
2. Add it to the sealed tail list
3. Create a new head segment, starting at the next TX offset

### 1.2 Commit Record Format

Each record written to a segment is called a **Commit**. It contains one or more transactions (currently always one in practice):

```
Header (22 bytes):
  min_tx_offset : u64 LE    — first TX offset in this commit
  epoch         : u64 LE    — term number (for distributed deployments, always 0 in standalone)
  n             : u16 LE    — number of transactions in this commit
  len           : u32 LE    — byte length of the records payload

Records (variable):
  BSATN-encoded TxData payloads, one per transaction

Checksum (4 bytes):
  CRC32C : u32 LE           — CRC32C over the entire commit (header + records)
```

Total framing overhead: 26 bytes per commit (22 header + 4 checksum).

TX offset range covered by this commit: `[min_tx_offset, min_tx_offset + n)`.

### 1.3 Checksum

Algorithm: **CRC32C** (Castagnoli). The checksum is computed over the header fields and records buffer, then written as 4 bytes little-endian immediately after the records. The reader recomputes the CRC and compares; a mismatch indicates corruption.

Partial writes (e.g., power loss mid-write) produce a CRC mismatch, which is the primary detection mechanism. The reader stops cleanly at the first mismatch and treats all preceding records as valid.

### 1.4 TX Offset vs Byte Offset

- **TX offset**: Logical, monotonically increasing counter. Identifies a specific transaction in the global sequence. Immutable.
- **Byte offset**: Physical position within a segment file.

These are not directly related. Mapping from TX offset to byte offset requires sequential scan, partially optimized by an **offset index** file stored alongside each segment. The index maps sampled TX offsets to byte offsets (written every ~4096 bytes of log data). Index entries may be stale after a crash but are safe to use as a scan starting point (scan forward from the indexed byte offset to find the target TX).

### 1.5 Write Path

1. Encode `TxData` into BSATN bytes (the records payload)
2. Compute commit: header fields + records + CRC32C
3. Write to a `BufWriter` (default 8 KiB buffer) backed by the segment file
4. Update the offset index entry if enough bytes have accumulated
5. Flush (drain BufWriter) and fsync: done per-batch by the durability worker, not per-commit

### 1.6 Partial Write Detection and Recovery

On startup, the commitlog traverses segments commit-by-commit and verifies CRC. A bad tail commit becomes a traversal boundary:
- All commits before the bad one are valid.
- Traversal of that segment stops at the first bad commit.
- The implementation may continue into a later segment only if the TX offset sequence remains contiguous.
- Opening the log for resumed writing does **not** mean "blindly truncate the segment and continue in place." In practice, the writer may start a new segment from the last valid TX offset, and some corruption topologies still fail hard (for example if the first commit in the last segment is corrupt).

This means the last few milliseconds of committed transactions may be lost on crash (only up to the last fsync is guaranteed). It also means recovery is conservative: gaps, forks, or some corruption layouts are treated as errors rather than silently truncated away.

### 1.7 Segment Rotation and Retention

Rotation: automatically triggered when `bytes_written >= max_segment_size` after a commit.

Old segments are retained for as long as needed (the engine decides cleanup policy). Segments older than the latest snapshot may be deleted, as the snapshot covers their TX range. The commitlog exposes `reset_to(offset)` which removes all segments and commits newer than the given offset.

---

## 2. CRATE: `crates/durability/` — Async Durability Worker

### 2.1 Architecture

The durability worker is a **Tokio actor** in a dedicated async task. The commit pipeline sends `TxData` to the worker via a bounded `mpsc` channel; the worker writes to disk asynchronously.

**Channel capacity:** `4 * batch_capacity` (default: 4 × 4096 = 16,384 slots). If the channel is full, the caller blocks (backpressure).

### 2.2 Batching

The actor drains as many transactions as available from the channel in one `recv_many` call, then writes them all to the commitlog before calling fsync. Each transaction is written as a separate commit record (not bundled into one multi-tx record). The comment in the source notes it is "unclear when it is both correct and beneficial to bundle more than a single transaction into a commit."

### 2.3 Fsync Policy

After each batch: flush BufWriter, call `fsync` on the segment file. **One fsync per batch**, regardless of how many transactions were in the batch. This amortizes fsync cost: at 100 TPS each fsync covers ~10 transactions; at 1000 TPS each fsync covers ~100.

### 2.4 Durable Offset Tracking

The worker maintains a `tokio::sync::watch` channel broadcasting the **durable TX offset**: the highest TX offset confirmed on disk after the last successful fsync. Consumers can call `DurableOffset::get()` for the current value or `DurableOffset::wait_for(n)` to block until TX n is durable.

The durable offset is set to `commitlog.max_committed_offset()` at startup (recovering state from the last run).

### 2.5 Failure Handling

- **Write failure** (commit to log fails): the local durability actor task crashes because the blocking commit path uses `expect(...)`. This is effectively a fatal failure, but the important architectural point is that the worker does not provide a graceful per-write recovery path.
- **Fsync failure**: log a warning, exit the actor loop, and leave the last successfully published durable offset intact. The close path does not surface a rich I/O error; callers observe shutdown plus the last seen durable offset.

### 2.6 Shutdown

Close sends a shutdown signal to the actor. The actor closes transaction intake, drains remaining queued transactions, performs a final flush/sync if possible, then exits. `Close::await` returns the last durable offset it observed; it does not provide a detailed I/O error channel.

---

## 3. CRATE: `crates/snapshot/` — Snapshot Creation and Recovery

### 3.1 What a Snapshot Contains

A snapshot is a point-in-time serialization of the entire in-memory database state:

```
Snapshot {
    magic:              [4]byte = b"txyz"
    version:            u8 = 0
    database_identity:  Identity
    replica_id:         u64
    module_abi_version: [2]u16
    tx_offset:          u64         — TX offset represented by this snapshot
    blobs:              []BlobEntry — large object reference counts
    tables:             []TableEntry — per-table page hashes
}
```

Page data and blobs are stored as content-addressed objects in an `objects/` subdirectory named by their Blake3 hash.

### 3.2 File Layout

```
{snapshots_root}/
  {tx_offset}.snapshot_dir/
    .lockfile                      — present only during creation
    snapshot.snapshot_bsatn        — BSATN snapshot struct + Blake3 hash prefix
    objects/
      {hash1}                      — page bytes or blob bytes
      {hash2}
      ...
  {tx_offset}.invalid_snapshot/   — invalidated (not used)
  {tx_offset}.archived_snapshot/  — archived cold storage
```

Snapshot directories are named by TX offset and are immutable once the lockfile is removed.

### 3.3 Object Deduplication (Hardlinking)

When creating a new snapshot, for each page/blob the writer checks whether the same hash exists in the previous snapshot's `objects/` directory. If yes, it creates a **hard link** instead of copying. This means successive snapshots share unchanged pages on disk at filesystem level.

Requires: both snapshots are on the same filesystem.

### 3.4 Integrity

Each snapshot object is stored with its Blake3 hash as the filename. On read, the hash is recomputed and compared. A mismatch → `SnapshotError::HashMismatch` → treat snapshot as corrupt.

The BSATN snapshot struct itself is prefixed with a 32-byte Blake3 hash on disk for top-level integrity.

### 3.5 Snapshot Creation Trigger

Snapshot creation is the **engine's responsibility**, not the snapshot crate's. In core, SpacetimeDB requests snapshots after a fixed commit frequency (`1_000_000` TX offsets), on explicit request, and can also request them from the durability side when a new commitlog segment begins. The snapshot crate only provides the creation/read/invalidation APIs.

### 3.6 Lockfile During Creation

A `.lockfile` is placed in the snapshot directory at the start of creation and removed at the end. If the engine crashes during snapshot creation, the lockfile remains. On startup, a snapshot with a lockfile is treated as incomplete and rejected.

### 3.7 Invalidation on Reset

When the log is reset to offset N (e.g., epoch change), all snapshots newer than N are renamed to `.invalid_snapshot` to prevent their use during recovery.

---

## 4. Recovery Procedure

### 4.1 Startup Sequence

```
1. Open commitlog / durable history
   a. Scan directory for segment files (sorted by min TX offset)
   b. Traverse commits in order, verify CRC32C, and require contiguous TX offsets
   c. Determine the durable horizon (highest contiguous valid TX in log)
   d. Treat gaps, overlaps, forks, or non-tail corruption as hard recovery errors

2. Find latest usable snapshot
   a. Scan snapshot directory for the latest complete snapshot not newer than the durable horizon
   b. Skip any with lockfile (incomplete) or permanent corruption (hash / format / deserialize failure)
   c. Use first usable one; if none, only start from empty state if the durable log still begins at the first TX offset

3. Reconstruct in-memory state
   a. If snapshot found: load tables and blobs from snapshot objects
   b. Restore datastore from snapshot, then rebuild secondary state (indexes, sequences, etc.) after replay as required
   c. If no snapshot: bootstrap empty state

4. Replay durable history
   a. start_offset = snapshot.tx_offset + 1 (or initial offset if no snapshot)
   b. Decode and apply each transaction from start_offset to the durable horizon
   c. Rebuild post-replay derived state

5. Engine is ready
   a. Resume durability starting from max_applied_offset + 1
```

### 4.2 Corruption Fallback Chain

1. Find newest snapshot not newer than the durable horizon
2. If that snapshot is permanently corrupt → try the next-oldest usable snapshot
3. If all snapshots are unusable → fall back to log-only replay only if the remaining log still starts from the initial TX offset
4. If the durable history has a gap, overlap, fork, or unrecoverable corruption topology → fail recovery rather than synthesizing a partial state

In the worst case with no valid snapshot and a complete contiguous log, the engine replays the entire log. With no valid snapshot and a compacted/missing-prefix log, recovery must stop with an error.

---

## 5. Key Insights for Shunter's Go Design

### 5.1 Segment files are the right unit of organization

Fixed-size files, named by start offset, are simple to scan, rotate, and delete. The Go implementation should mirror this: a directory of files named `{offset:020d}.log` (zero-padded for sorted directory listing).

### 5.2 Epoch field is unnecessary in Go

SpacetimeDB's `epoch` field supports multi-master failover where two nodes can each write to the same logical log. Shunter is single-node and single-process. Omit the epoch field from the Go record format.

### 5.3 CRC32C is available in Go's standard library

`hash/crc32` provides `crc32.MakeTable(crc32.Castagnoli)`. Use it. Byte order: little-endian, same as SpacetimeDB.

### 5.4 Async durability worker is the right model

The bounded channel with per-batch fsync is the correct pattern. The Go equivalent: a goroutine reading from a buffered channel, writing records, calling `file.Sync()` after each batch.

### 5.5 Snapshot deduplication needs same filesystem

The hardlink optimization for snapshot objects requires the snapshots directory and the objects within to be on the same filesystem. Document this constraint for Go.

### 5.6 Recovery applies TxData, not raw bytes

The log stores BSATN-encoded TxData (inserts and deletes per table). Recovery replays these as write operations against the store. In Go, the TxData encoding should be the same format used for the changeset (ProductValue slices encoded as binary).

### 5.7 Magic number identifies the log format

SpacetimeDB uses per-format versioning (a format version byte in segment headers). Shunter should include a magic header in segment files (4 bytes, e.g., `SHNT`) and a version byte. This allows detection of wrong-format files and future format evolution.
