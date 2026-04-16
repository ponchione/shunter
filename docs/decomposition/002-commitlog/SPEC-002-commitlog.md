# SPEC-002 — Commit Log

**Status:** Draft  
**Depends on:** SPEC-001 (In-Memory Store) for `Changeset` and `ProductValue` types  
**Depended on by:** SPEC-003 (Transaction Executor) calls `DurabilityHandle`; recovery reconstructs state consumed by SPEC-001

---

## 1. Purpose and Scope

The commit log is the durability layer of Shunter. It provides:

- An append-only, crash-safe log of all committed transactions
- Periodic full-state snapshots to bound startup recovery time
- Recovery: reconstruction of in-memory state from snapshots + log replay

This spec covers:
- On-disk segment file format
- Record structure and framing (header, payload, checksum)
- Encoding of `Changeset` as log payload
- The async durability goroutine and its channel interface
- Snapshot format: what is stored, how objects are addressed, integrity guarantees
- Recovery procedure: startup sequence, corrupt snapshot fallback chain, truncated log handling
- The `DurabilityHandle` interface exported to the executor (SPEC-003)

This spec does not cover:
- `Changeset` / `ProductValue` types (SPEC-001)
- How commits are created (SPEC-003)

---

## 2. On-Disk Layout

### 2.1 Directory Structure

```
{data_dir}/
  commitlog/
    00000000000000000001.log   — segment starting at TX 1
    00000000000000047831.log   — segment starting at TX 47831
    ...                         — (zero-padded 20-digit decimal names)
  snapshots/
    00000000000000047830/       — snapshot at TX 47830
      snapshot                  — BSATN-encoded snapshot struct
      objects/
        {hex-hash-1}            — page / blob content (named by Blake3 hash)
        {hex-hash-2}
        ...
      .lock                     — present only during creation
    ...
```

Segment files are sorted lexicographically by name, which equals sorting by start TX offset. The highest-numbered file is the active (writable) segment.

### 2.2 Segment File Header

Every segment file begins with a fixed 8-byte file header:

```
magic   : [4]byte = {'S','H','N','T'}   — identifies this as a Shunter commit log segment
version : uint8 = 1                      — log format version
flags   : uint8 = 0                      — reserved, must be 0
_pad    : [2]byte = {0, 0}               — reserved, must be 0
```

A file that does not begin with the magic bytes is rejected during recovery. A version field mismatch is a hard error (do not attempt to decode unknown formats).

### 2.3 Record Format

After the segment header, the file contains a sequence of records. v1 defines exactly one record type (`RecordTypeChangeset = 1`), but the framing reserves a type byte now so that future format additions do not require an ambiguous reinterpretation of existing records.

```
Record:
  tx_id        : uint64 LE    — transaction ID (monotonically increasing, assigned at commit)
  record_type  : uint8        — 1 = Changeset
  flags        : uint8        — reserved, must be 0 in v1
  data_len     : uint32 LE    — byte length of the payload
  payload      : [data_len]byte
  crc          : uint32 LE    — CRC32C over all preceding fields in this record
```

Total framing overhead: 8 + 1 + 1 + 4 + 4 = **18 bytes** per record, plus the payload.

Invariants:
- `tx_id` values MUST form one contiguous increasing sequence across the entire log.
- `record_type` values other than `1` are rejected in v1 with `ErrUnknownRecordType`.
- `flags` MUST be zero in v1; non-zero values are rejected with `ErrBadFlags`.
- `data_len` MUST be less than or equal to `MaxRecordPayloadBytes` (default 64 MiB). Larger values are rejected before allocation with `ErrRecordTooLarge`.
- A record is valid only if all framing bytes, the full payload, and the trailing CRC are present.

**Why no epoch field:** SpacetimeDB includes an epoch for multi-master failover. Shunter is single-node; epoch is omitted.

**Why no `n` (transaction count per commit):** SpacetimeDB supports multi-TX commits to amortize per-record overhead. Shunter writes one record per transaction in v1. This simplifies the format; batching can be added in v2 if throughput profiling shows the benefit.

### 2.4 Checksum

Algorithm: **CRC32C** (Castagnoli polynomial). Computed using `crc32.New(crc32.MakeTable(crc32.Castagnoli))` from Go's `hash/crc32` package. The CRC covers the `tx_id`, `record_type`, `flags`, `data_len`, and `payload` fields of the record. It does not cover the segment file header.

Written as 4 bytes, little-endian, immediately after the payload.

---

## 3. Changeset Encoding (Log Payload)

### 3.1 Format Choice

**Recommendation:** Use a simple length-prefixed binary encoding of `Changeset`. Not protobuf (adds a dependency), not JSON (too large). A custom encoding with explicit version byte, matching the simplicity of the log format.

### 3.2 Payload Structure

```
version  : uint8 = 1
table_count : uint32 LE
[ for each table:
    table_id   : uint32 LE
    insert_count : uint32 LE
    [ for each inserted row:
        row_len  : uint32 LE
        row_data : [row_len]byte   — ProductValue encoded as defined in §3.3
    ]
    delete_count : uint32 LE
    [ for each deleted row:
        row_len  : uint32 LE
        row_data : [row_len]byte
    ]
]
```

Lengths are always little-endian `uint32`. No padding between fields. `row_len` MUST be less than or equal to `MaxRowBytes` (default 8 MiB) or decoding fails with `ErrRowTooLarge`.

### 3.3 ProductValue Encoding (BSATN Codec — canonical reference)

> **Canonical reference:** This section defines the BSATN binary encoding used throughout Shunter. Both the commit log payload (§3.2) and the client wire protocol (SPEC-005 §3.1) use this encoding. When implementing the `bsatn` package, use this section as the sole source of truth. SPEC-001 and SPEC-005 cross-reference `SPEC-002 §3.3`.

Each `ProductValue` (a row) is encoded as its column values in schema-defined column order:

```
[ for each column:
    tag  : uint8   — ValueKind discriminant (see table below)
    data : ...     — type-specific payload
]
```

| ValueKind | tag | data |
|---|---|---|
| Bool | 0 | 1 byte: 0x00 (false) or 0x01 (true) |
| Int8 | 1 | 1 byte signed |
| Uint8 | 2 | 1 byte unsigned |
| Int16 | 3 | 2 bytes LE signed |
| Uint16 | 4 | 2 bytes LE unsigned |
| Int32 | 5 | 4 bytes LE signed |
| Uint32 | 6 | 4 bytes LE unsigned |
| Int64 | 7 | 8 bytes LE signed |
| Uint64 | 8 | 8 bytes LE unsigned |
| Float32 | 9 | 4 bytes IEEE-754 LE |
| Float64 | 10 | 8 bytes IEEE-754 LE |
| String | 11 | uint32 LE byte count + UTF-8 bytes |
| Bytes | 12 | uint32 LE byte count + raw bytes |

Tags are stable. Adding new column types requires a new tag value and a payload encoding increment.

Decoder rules:
- The decoder MAY parse bytes value-by-value without schema, but schema is still required to validate the expected column count, expected value kinds, and table identity.
- If a row produces fewer or more values than the schema requires, decoding fails with `ErrRowShapeMismatch`.
- If a tag does not match the schema-expected type for that column, decoding fails with `ErrTypeTagMismatch`.
- If a string payload is not valid UTF-8, decoding fails with `ErrInvalidUTF8`.
- If a decoder finishes a row before consuming exactly `row_len` bytes, or needs bytes past `row_len`, decoding fails with `ErrRowLengthMismatch`.
- Unknown tags fail with `ErrUnknownValueTag`.

v1 scope rule: nullable/optional column values are out of scope for this encoding. SPEC-006 MUST reject nullable column declarations in v1 rather than inventing an implicit null sentinel here.

**TxID stamping (producer side).** `Changeset.TxID` (SPEC-001 §6.1) is allocated and stamped by the executor (Model A; see SPEC-003 §4.4 and §13.2) before the changeset is handed to `DurabilityHandle.EnqueueCommitted`. The commit-log payload format in §3.2 does not repeat `TxID` inside the payload because the enclosing record framing already carries `tx_id`. Producers and decoders MUST keep the two in sync: on decode during recovery, `cs.TxID` MUST be set to the framing `tx_id` before handing the decoded changeset to `store.ApplyChangeset` so downstream consumers see a stamped value.

### 3.4 Schema-at-Commit-Time

Recovery decodes records in log order. Column schema for each table must be known to validate row shape and value types.

v1 rule: schema is static for the lifetime of a data directory. The engine stores schema in each snapshot and validates on startup that the application-registered schema matches the snapshotted schema exactly. If they differ, startup fails with `ErrSchemaMismatch`.

v1 does not support runtime schema-change records. Adding them in the future requires either a new record type or a new payload version with an explicit migration spec.

Schema is stored in the snapshot (see §5).

---

## 4. Durability Goroutine

### 4.1 Overview

The executor (SPEC-003) must not block on disk I/O. The durability goroutine decouples commit from disk write:

```
Executor goroutine           Durability goroutine
      │                              │
      │── enqueue(Changeset) ───────►│
      │   (may block on backpressure)│── encode Changeset
      │                              │── write record to segment
      │                              │── [batch: repeat for more items]
      │                              │── file.Sync()
      │                              │── update DurableTxID
      │                              │
```

### 4.2 DurabilityHandle Interface

This interface MUST match SPEC-003 exactly.

```go
type DurabilityHandle interface {
    // EnqueueCommitted sends a changeset to the durability worker.
    // Blocks only for bounded-queue backpressure.
    // txID must be strictly increasing across calls.
    EnqueueCommitted(txID TxID, changeset *Changeset)

    // DurableTxID returns the highest TxID confirmed durable on disk.
    // Returns 0 if nothing has been fsynced yet.
    DurableTxID() TxID

    // Close stops new admissions, drains queued work, performs a final fsync,
    // and shuts down the durability worker.
    // Returns the final durable TxID and any latched fatal error.
    Close() (TxID, error)
}
```

### 4.3 Internal Channel

```go
type durabilityWorker struct {
    ch        chan durabilityItem    // bounded; capacity = ChannelCapacity (default 256)
    durable   atomic.Uint64          // last fsynced TxID; read by DurableTxID()
    fatalErr  atomic.Pointer[error]  // nil until first fatal write/sync/rotate error
    closing   atomic.Bool            // true after Close begins
    done      chan struct{}          // closed when goroutine exits
}
```

Channel capacity default: 256. Rationale: large enough to absorb bursts; small enough that backpressure kicks in before memory is stressed. Configurable.

### 4.4 Write Loop

```
loop:
  wait for at least one item or shutdown
  drain additional currently-queued items up to DrainBatchSize (default 64)
  for each item:
    encode Changeset to bytes
    write record (tx_id, record_type, flags, data_len, payload, crc) to active segment
    if segment.size >= MaxSegmentSize: rotate segment
  call file.Sync()
  atomic.Store(&durable, last_written_tx_id)
```

**Why batch-then-sync:** One `fsync` call per batch amortizes the ~5 ms disk seek cost across multiple transactions. At 1000 TPS with 64-item batches, each fsync covers ~64 transactions, reducing disk pressure to ~15 fsyncs/second.

**Why `atomic.Uint64` for durable offset:** The executor reads `DurableTxID()` from its own goroutine. Atomic read/write avoids a mutex for this single integer.

### 4.5 Segment Rotation

```go
func (w *durabilityWorker) maybeRotate() error {
    if w.seg.size < w.opts.MaxSegmentSize {
        return nil
    }
    if err := w.seg.file.Sync(); err != nil {
        return err
    }
    w.seg.file.Close()
    w.seg = openNewSegment(w.dir, w.nextTxID)
    return nil
}
```

A new segment is created with its name equal to `w.nextTxID` (the TX ID of the first record to be written into it).

### 4.6 Write Failure

On any fatal encode, write, sync, or rotation error:
- store the first fatal error in `fatalErr`
- stop accepting new admissions
- drain no further work
- exit the worker after closing `done`

The durability worker itself MUST NOT rely on an unhandled goroutine panic as its control path. Instead, the public handle enters a latched failed state:
- `EnqueueCommitted` MUST panic immediately if called after a fatal durability failure or after `Close` begins
- `DurableTxID` continues returning the last successfully fsynced TxID
- `Close` returns the last durable TxID plus the latched fatal error, if any

Rationale: a post-commit durability failure is not recoverable within the live engine, but the failure mode must still be deterministic and inspectable.

**Executor contract:** SPEC-003 MUST treat any panic from `EnqueueCommitted` as a terminal engine failure and reject future write commands until restart.

---

## 5. Snapshot Format

### 5.1 What a Snapshot Captures

A snapshot is a point-in-time serialization of all committed in-memory state:
- All rows in all tables
- The current state of all sequences (auto-increment counters)
- The schema (table definitions, index definitions)
- The TX ID at which the snapshot was taken

Indexes are **not stored** in snapshots. They are rebuilt from row data during recovery. This avoids storing redundant data and simplifies snapshot format evolution.

### 5.2 Snapshot File Format

```
Snapshot file (snapshots/{tx_id}/snapshot):

  magic              : [4]byte = {'S','H','S','N'}
  version            : uint8 = 1
  _pad               : [3]byte = {0,0,0}
  tx_id              : uint64 LE       — TX ID represented by this snapshot
  schema_version     : uint32 LE       — schema registry version captured in this snapshot
  hash               : [32]byte        — Blake3 hash of all subsequent bytes in this file

  [ the following is covered by the hash above ]

  schema_len         : uint32 LE
  schema             : [schema_len]byte   — encoded SchemaSnapshot (see §5.3)

  seq_count          : uint32 LE
  [ for each sequence, sorted by table_id ascending:
      table_id       : uint32 LE
      next_id        : uint64 LE
  ]

  table_count        : uint32 LE
  [ for each table, sorted by table_id ascending:
      table_id       : uint32 LE
      row_count      : uint64 LE
      [ for each row in deterministic primary-key order:
          row_len    : uint32 LE
          row_data   : [row_len]byte   — ProductValue (same encoding as §3.3)
      ]
  ]
```

Notes:
- Internal `RowID` values are not stored. SPEC-001 defines them as non-stable internal identifiers; recovery rebuilds them.
- Indexes are rebuilt from rows after recovery; they are not serialized into the snapshot.
- Deterministic ordering is required so that repeated snapshots of the same logical state produce byte-stable contents aside from the outer hash field.

### 5.3 Schema Snapshot

The schema snapshot encodes all registered tables and their column/index definitions. This is written into every snapshot so that recovery can validate schema compatibility:

```
schema_version : uint32 LE   — application-defined schema version (from SPEC-006 registration)
table_count    : uint32 LE
[ for each table:
    table_id   : uint32 LE
    name_len   : uint32 LE
    name       : [name_len]byte
    col_count  : uint32 LE
    [ for each column:
        col_idx  : uint32 LE
        name_len : uint32 LE
        name     : [name_len]byte
        type_tag : uint8          — ValueKind
    ]
    // Decoder note: col_idx is stored as uint32 but decoded as int for ColumnSchema.Index.
    // Values exceeding math.MaxInt32 are a hard recovery error.
    idx_count  : uint32 LE
    [ for each index:
        idx_name_len : uint32 LE
        idx_name     : [idx_name_len]byte
        unique       : uint8        — 0 or 1
        primary      : uint8        — 0 or 1
        col_count    : uint32 LE
        [ for each column:
            col_idx : uint32 LE
        ]
    ]
]
```

### 5.4 Snapshot Integrity

The 32-byte Blake3 hash covers every byte after the hash field, from `schema_len` to end of file. On read, recompute and compare. A mismatch means the snapshot is corrupt; do not use it.

**Why Blake3:** Fast (GB/s on modern hardware), collision-resistant, produces fixed 32-byte output. `lukechampine.com/blake3` or `zeebo/blake3` provides Go implementations.

### 5.5 Lockfile During Creation

Snapshot creation MUST follow this order:
1. Create `snapshots/{tx_id}/`.
2. Create empty `snapshots/{tx_id}/.lock`.
3. Write the snapshot file contents.
4. `fsync` the snapshot file.
5. `fsync` the snapshot directory.
6. Remove `.lock`.
7. `fsync` the snapshot directory again.

On startup, any snapshot directory containing a `.lock` file is treated as incomplete and skipped.

### 5.6 Snapshot Trigger Policy

The commit log package does not decide when to snapshot. It exposes:

```go
type SnapshotWriter interface {
    // CreateSnapshot writes a snapshot for the current committed state at txID.
    // Blocks until the snapshot file and containing directory are fully written and fsynced.
    // Returns an error if another snapshot is in progress.
    CreateSnapshot(committed *CommittedState, txID TxID) error
}
```

v1 policy: the **recommended default is `SnapshotInterval = 0`** (no automatic interval-based snapshotting). The engine SHOULD call `CreateSnapshot` exactly once on graceful shutdown — immediately before closing the durability worker, while no new commits are being accepted.

**Rationale:** Synchronous snapshot creation holds `CommittedState.mu` for read during full state serialization. For a 100 MB–1 GB working set this takes tens to hundreds of milliseconds, during which all commits block. Triggering snapshots under live write load is a significant latency hazard; the default avoids it entirely.

**When to override:** Applications that require bounded recovery time and cannot guarantee graceful shutdown (e.g., processes that may be killed abruptly) may set `SnapshotInterval > 0` to trigger periodic snapshots, accepting the commit-latency cost. When using periodic mode, the executor MUST quiesce (stop accepting new writes) for the full duration of snapshot creation.

**Async snapshot path:** Deferred to v2. Requires explicit copy-on-write or epoch-based read-view semantics so commits can continue during serialization.

---

## 6. Recovery Procedure

### 6.1 Startup Sequence

```
func OpenAndRecover(dir string, schema SchemaRegistry) (*CommittedState, TxID, error)
```

1. **Scan commit log segments first.** List `commitlog/` files sorted by name and validate that segment start TX IDs are strictly increasing.
2. **Determine the durable replay horizon.** Scan segments in order and find the highest contiguous valid `tx_id` reachable from the earliest segment:
   - validate segment header magic/version
   - validate each record framing and CRC
   - validate contiguous `tx_id` sequence across records and across segment boundaries
   - if the active tail segment ends with a truncated record or CRC-mismatched partial tail write, stop at the last valid contiguous record
   - if a non-tail segment is corrupt, a segment is missing, or the sequence has a gap/fork/out-of-order record, return a hard recovery error
3. **Scan snapshot directory.** List snapshot subdirectories sorted by TX ID descending. Skip any with a `.lock` file. Only snapshots with `tx_id <= durable_horizon` are candidates.
4. **Load snapshot** (if found):
   a. Read snapshot file and verify Blake3 hash
   b. Compare embedded schema to `SchemaRegistry` (SPEC-006 §7) exactly: schema version (from `SchemaRegistry.Version()` — semantics pinned in SPEC-006 §6.1; the snapshot header integer is authoritative when header and body disagree), all table IDs, table names, column indices/names/types in declaration order, and index definitions (names, column references, Unique, Primary). If any field differs, return `ErrSchemaMismatch`.
   c. Reconstruct table rows from snapshot contents
   d. Rebuild indexes from those rows
   e. Restore sequence counters from snapshot sequence entries
   f. Record `snapshot_tx_id`
5. **If no valid snapshot found:**
   - if the earliest remaining log record has `tx_id = 1`, start from empty `CommittedState`
   - otherwise return `ErrMissingBaseSnapshot`; the log history has already been compacted and there is no safe base state to restore from
6. **Replay log from `snapshot_tx_id + 1`:**
   a. Skip records with `tx_id <= snapshot_tx_id`
   b. Decode `Changeset` from payload and stamp `cs.TxID = record.tx_id` (payload does not carry TxID; see §3.3)
   c. Call `store.ApplyChangeset(committed, cs)` (SPEC-001 §5.8). Fatal error if it returns non-nil.
   d. Track `max_applied_tx_id`
7. **Return** `(committed, max_applied_tx_id, nil)`. The executor resumes issuing TX IDs from `max_applied_tx_id + 1`.

### 6.2 Corrupt Snapshot Fallback Chain

```
Try newest snapshot <= durable_horizon
  → hash/version/schema failure?
    → Try next older snapshot <= durable_horizon
      → ...
        → No usable snapshot:
            - start from empty only if log still begins at tx 1
            - otherwise fail with ErrMissingBaseSnapshot
```

A bad snapshot never authorizes replay past a log gap. Snapshot fallback only changes the starting base state; the replay suffix must still be contiguous and valid.

### 6.3 Schema Mismatch on Recovery

If the snapshot's schema does not match the registered schema:
- Return `ErrSchemaMismatch` with details (which table/column/index differs)
- Do not attempt recovery
- The operator must manually migrate or wipe the data directory

Schema evolution is out of scope for v1. Document this clearly.

### 6.4 Truncated Record and Resume Handling

A truncated tail record (partial write at crash time) produces a CRC mismatch or EOF while reading framing/payload. Recovery uses all prior valid records and treats the first invalid tail record as the replay horizon.

Resume rules:
- the implementation MUST locate the last valid contiguous record before resuming writes
- if the invalid data is only in the writable tail segment and at least one valid record precedes it, the implementation MAY resume by creating a fresh next segment starting at `last_valid_tx_id + 1`
- the implementation MUST NOT assume it can safely overwrite arbitrary trailing bytes in-place without first proving the write position
- if the first record in the last segment is corrupt and there is no prior valid prefix in that segment, opening for append is a hard error until operator intervention or explicit reset

### 6.5 History Gap Handling

The following are hard recovery errors in v1:
- a missing segment file that creates a TX gap
- two segments that claim overlapping but non-identical TX ranges
- any out-of-order `tx_id`
- any non-tail corruption that breaks contiguous history

Recovery MUST fail closed rather than silently instantiate a state whose durable history is incomplete.

---

## 7. Log Compaction

After a snapshot is successfully created at `snapshot_tx_id`:
- any segment whose highest record `tx_id` is less than or equal to `snapshot_tx_id` is eligible for deletion
- the first segment that may still contain a record with `tx_id > snapshot_tx_id` MUST be retained, even if its file name / start TX is older than the snapshot
- the active writable segment is never deleted

**Compaction policy (v1):** After each snapshot, compute each sealed segment's `[min_tx_id, max_tx_id]` coverage and delete only segments fully covered by the snapshot. Deletion by segment start offset alone is forbidden.

**Reader safety:** v1 does not support long-lived concurrent commitlog readers while the engine is online. Recovery is the only commitlog traversal path. Compaction therefore runs only after snapshot completion and only against sealed segments not currently opened by recovery.

**Safety:** Do not delete segments until the snapshot is fully written and fsynced.

---

## 8. Configuration

```go
type CommitLogOptions struct {
    // MaxSegmentSize: rotate to a new segment file after this many bytes.
    // Default: 512 MiB.
    MaxSegmentSize int64

    // MaxRecordPayloadBytes: hard upper bound for one encoded record payload.
    // Default: 64 MiB.
    MaxRecordPayloadBytes uint32

    // MaxRowBytes: hard upper bound for one encoded row value inside a payload.
    // Default: 8 MiB.
    MaxRowBytes uint32

    // ChannelCapacity: durability goroutine input channel buffer size.
    // Default: 256.
    ChannelCapacity int

    // DrainBatchSize: max records to drain before calling fsync.
    // Default: 64.
    DrainBatchSize int

    // SnapshotInterval: call CreateSnapshot after this many commits.
    // 0 = never snapshot automatically.
    // Default: 100_000.
    SnapshotInterval uint64
}
```

---

## 9. Error Catalog

| Error | Condition |
|---|---|
| `ErrBadMagic` | Segment file does not begin with `SHNT` |
| `ErrBadVersion` | Segment or snapshot has unknown version byte |
| `ErrBadFlags` | Record flags are non-zero in v1 |
| `ErrUnknownRecordType` | Record type is not defined in this format version |
| `ErrChecksumMismatch` | Record CRC32C does not match |
| `ErrRecordTooLarge` | `data_len` exceeds `MaxRecordPayloadBytes` |
| `ErrRowTooLarge` | `row_len` exceeds `MaxRowBytes` |
| `ErrUnknownValueTag` | Row encoding contains an unknown tag |
| `ErrTypeTagMismatch` | Encoded tag does not match the schema-expected column type |
| `ErrRowShapeMismatch` | Decoded row has the wrong number of values for the table schema |
| `ErrRowLengthMismatch` | Decoder under- or over-consumes bytes within a row frame |
| `ErrInvalidUTF8` | Encoded string value is not valid UTF-8 |
| `ErrSnapshotIncomplete` | Snapshot directory has a `.lock` file |
| `ErrSnapshotHashMismatch` | Snapshot Blake3 hash does not match content |
| `ErrSchemaMismatch` | Snapshot schema differs from registered schema |
| `ErrHistoryGap` | Recovery found a missing/overlapping/out-of-order TX range |
| `ErrMissingBaseSnapshot` | No usable snapshot exists and the remaining log no longer starts at tx 1 |
| `ErrDurabilityFailed` | Durability worker latched a fatal write/sync/rotation error |
| `ErrNoData` | Recovery found no segments and no snapshots |

---

## 10. Performance Constraints

| Operation | Target |
|---|---|
| Encode + write one record (1 KB payload) | < 10 µs (excluding fsync) |
| fsync one batch of 64 records | < 20 ms (SSD) |
| Snapshot creation (1 million rows, 1 KB each) | < 30 s |
| Recovery (snapshot load + 10k log records) | < 5 s |
| Recovery (no snapshot, 1M log records) | < 60 s |

---

## 11. Interfaces to Other Subsystems

### SPEC-003 (Transaction Executor)
The executor receives a `DurabilityHandle` at engine startup. After each commit it calls `EnqueueCommitted(txID, changeset)`. It also calls `CreateSnapshot` according to the configured snapshot policy. Recovery is invoked at engine startup before the executor begins processing requests.

### SPEC-001 (In-Memory Store)
Recovery applies replayed `Changeset` values to `CommittedState`. The snapshot writes and reads rows as `ProductValue` slices using the encoding defined in §3.3. Snapshot recovery rebuilds internal row IDs and indexes rather than persisting them.

---

## 12. Open Questions

1. **Snapshot creation timing.** v1 permits synchronous snapshot creation. If production latency shows this is too expensive, v2 should introduce an async snapshot path with explicit copy-on-write/read-view rules.

2. **Multiple snapshot retention.** v1 should keep at least the newest two successful snapshots. Whether retention should be count-based, age-based, or size-based is deferred.

3. **Write-ahead guarantee level.** v1 durability is batch-fsync. A strict per-transaction sync mode may be added later if an operator-facing durability/latency tradeoff is required.

4. **Record-type expansion.** v1 reserves a record type byte but defines only `Changeset`. Future types (schema changes, checkpoints, metadata) need their own standalone spec and migration rules.

5. **Snapshot compression.** Snapshots may become large enough that optional compression is worthwhile, but compression is deferred until the uncompressed format and recovery path are proven stable.

---

## 13. Verification

| Test | What it verifies |
|---|---|
| Write 10 records, open new reader, verify all 10 decoded correctly | Basic write/read round-trip |
| Write records, crash-simulate (truncate last record), open and replay | CRC/EOF tail handling stops at the replay horizon |
| Write records past MaxSegmentSize, verify rotation creates new file | Segment rotation |
| Write records to 3 segments, replay all | Multi-segment replay |
| Create snapshot, verify hash, reload, check row count and sequence state match | Snapshot write/read integrity |
| Create snapshot with lockfile present, attempt load → ErrSnapshotIncomplete | Incomplete snapshot rejection |
| Write snapshot, corrupt 1 byte, load → ErrSnapshotHashMismatch | Hash detection |
| Two snapshots, corrupt latest, load next older snapshot | Fallback to older snapshot |
| Recovery: snapshot at 1000 + contiguous log from 1001–1500 → correct final state | Full recovery path |
| Recovery: no snapshot + log from 1–500 → correct final state | Recovery from log only |
| Recovery: no snapshot + earliest log tx > 1 → ErrMissingBaseSnapshot | Refuse partial-history restore |
| Delete a middle segment file and attempt recovery → ErrHistoryGap | Missing segment detection |
| Corrupt a middle sealed segment and attempt recovery | Non-tail corruption is fatal |
| Corrupt first record in active tail segment and reopen for append | Hard-error edge case on resume |
| Replay log with out-of-order or skipped tx_id | Contiguity enforcement |
| Snapshot at tx 1000 where one segment spans 900–1100, then compact | Boundary segment is retained |
| Schema mismatch (registered schema differs from snapshot) → ErrSchemaMismatch | Schema validation |
| Unknown value tag / wrong tag for schema / invalid UTF-8 / bad row_len | Decoder validation rules |
| Durability goroutine: send 1000 items, all appear on disk after Close() | Flush-on-close correctness |
| DurableTxID advances after each fsynced batch | Durable offset tracking |
| EnqueueCommitted blocks when channel full | Backpressure |
| Fatal write/sync error latches worker failure; next EnqueueCommitted panics; Close returns error | Deterministic fatal durability behavior |
| Crash with partial tail write, restart, append more, crash again, restart again | Two-consecutive-crash recovery |
| Crash during snapshot creation leaves .lock; restart skips it and recovers from prior snapshot + log | Snapshot-in-progress crash handling |
