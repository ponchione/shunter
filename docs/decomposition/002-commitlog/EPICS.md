# SPEC-002 — Epic Decomposition

Source: [SPEC-002-commitlog.md](./SPEC-002-commitlog.md)

---

## Epic 1: BSATN Codec

**Spec sections:** §3.3

Canonical binary encoding for ProductValue. Used by commit log payload AND client wire protocol (SPEC-005). Standalone package — no commitlog dependencies.

**Scope:**
- Value tag table (13 ValueKinds → uint8 tags)
- Encode single Value to bytes (tag + type-specific payload)
- Decode single Value from bytes
- Encode ProductValue (column-ordered sequence of encoded Values)
- Decode ProductValue with schema validation (column count, type tag match)
- Error types: ErrUnknownValueTag, ErrTypeTagMismatch, ErrRowShapeMismatch, ErrRowLengthMismatch, ErrInvalidUTF8

**Testable outcomes:**
- Round-trip each ValueKind through encode/decode
- Decode with wrong tag for schema column → ErrTypeTagMismatch
- Decode row with wrong column count → ErrRowShapeMismatch
- Invalid UTF-8 in string → ErrInvalidUTF8
- Unknown tag byte → ErrUnknownValueTag
- Decode consumes exactly row_len bytes

**Dependencies:** SPEC-001 Epic 1 (Value, ProductValue, ValueKind)

---

## Epic 2: Record Format & Segment I/O

**Spec sections:** §2.1–§2.4

On-disk segment files: header, record framing, CRC32C checksums, sequential read/write.

**Scope:**
- Directory structure conventions (commitlog/, snapshots/)
- Segment file header: magic `SHNT`, version, flags, padding
- Record format: tx_id, record_type, flags, data_len, payload, CRC32C
- Record writer: append records to active segment file
- Record reader: iterate records with framing + CRC validation
- Size limits: MaxRecordPayloadBytes, MaxRowBytes
- Error types: ErrBadMagic, ErrBadVersion, ErrBadFlags, ErrUnknownRecordType, ErrChecksumMismatch, ErrRecordTooLarge

**Testable outcomes:**
- Write 10 records, read all 10 back correctly
- Truncated last record → reader stops at last valid record
- Corrupt CRC → ErrChecksumMismatch
- Bad magic bytes → ErrBadMagic
- data_len exceeds max → ErrRecordTooLarge
- tx_id contiguity validated across records

**Dependencies:** None (pure file format, no SPEC-001 types needed)

---

## Epic 3: Changeset Codec

**Spec sections:** §3.1, §3.2, §3.4

Encode/decode Changeset as the log record payload. Bridges SPEC-001 Changeset type to the on-disk binary format.

**Scope:**
- Changeset payload encoder: version byte + table_count + per-table inserts/deletes using BSATN for rows
- Changeset payload decoder: inverse, with schema validation
- MaxRowBytes enforcement during decode
- Schema-at-commit-time: v1 schema is static, validated from snapshot

**Testable outcomes:**
- Round-trip Changeset with multiple tables, inserts, deletes
- Decode with wrong schema → error
- Row exceeding MaxRowBytes → ErrRowTooLarge
- Empty changeset (no tables) encodes/decodes correctly
- Payload version mismatch → error

**Dependencies:** Epic 1 (BSATN codec), SPEC-001 (Changeset, ProductValue, TableID)

---

## Epic 4: Durability Worker

**Spec sections:** §4.1–§4.6

Async goroutine that decouples commit from disk write. Batch-then-sync for throughput.

**Scope:**
- `DurabilityHandle` interface: EnqueueCommitted, DurableTxID, WaitUntilDurable, Close
- `durabilityWorker` struct: bounded channel, atomic durable TxID, fatal error latch
- Write loop: drain batch, encode, write records, fsync, update durable
- Segment rotation when size exceeds MaxSegmentSize
- Fatal error handling: latch error, stop accepting, panic on subsequent enqueue
- Backpressure: EnqueueCommitted blocks when channel full
- Close: drain, final fsync, return final TxID + error

**Testable outcomes:**
- Send 1000 items, all on disk after Close()
- DurableTxID advances after each fsynced batch
- Channel full → EnqueueCommitted blocks
- Fatal write error → next EnqueueCommitted panics, Close returns error
- Segment rotation creates new file at correct boundary
- Close drains remaining items before returning

**Dependencies:** Story 4.1 depends on Epic 2 (segment writer); the full epic also depends on Epic 3 once Story 4.2 adds changeset encoding

---

## Epic 5: Snapshot I/O

**Spec sections:** §5.1–§5.6

Write and read full-state snapshots for bounded recovery time.

**Scope:**
- Snapshot file format: magic `SHSN`, version, tx_id, schema_version, Blake3 hash, `schema_len`, schema, sequences, per-table `nextID` allocation state, table rows
- Schema snapshot encoding/decoding (§5.3)
- Blake3 integrity: hash everything after hash field, verify on read
- Lockfile protocol: .lock during creation, skip on recovery
- SnapshotWriter interface: CreateSnapshot, including snapshot-in-progress rejection
- Row ordering: deterministic PK order for byte-stable snapshots
- Snapshot reading: full decode + hash verify + schema extract

**Testable outcomes:**
- Write snapshot, reload, row count, sequence state, and nextID allocation state match
- Corrupt 1 byte → ErrSnapshotHashMismatch
- Lockfile present → ErrSnapshotIncomplete
- Schema round-trip through snapshot encoding
- Deterministic: same state produces same bytes (minus hash field)

**Dependencies:** Epic 1 (BSATN for row encoding), SPEC-001 (CommittedState, Table, Sequence, snapshot export hooks from Epic 8 Story 8.3)

---

## Epic 6: Recovery

**Spec sections:** §6.1–§6.5

OpenAndRecover: reconstruct in-memory state from snapshots + log replay.

**Scope:**
- `OpenAndRecover(dir, schema) → (*CommittedState, TxID, error)`
- Segment scanning: list, validate headers, find durable replay horizon
- Snapshot selection: newest valid snapshot ≤ durable horizon
- Corrupt snapshot fallback chain
- Schema mismatch detection (ErrSchemaMismatch)
- Log replay: decode changesets, call ApplyChangeset (SPEC-001 §5.8)
- Truncated tail record handling (stop at last valid)
- Resume-after-crash contract: distinguish append-in-place vs fresh-next-segment vs hard-error tail states
- History gap detection (ErrHistoryGap)
- No-snapshot recovery from tx 1 (ErrMissingBaseSnapshot if log doesn't start at 1)
- Empty data dir handling (`ErrNoData`)

**Testable outcomes:**
- Snapshot at 1000 + log 1001–1500 → correct final state
- No snapshot + log from 1–500 → correct from scratch
- No snapshot + log starting at tx > 1 → ErrMissingBaseSnapshot
- Missing middle segment → ErrHistoryGap
- Corrupt newest snapshot → falls back to older one
- Truncated tail record → recovery uses prior valid records
- Damaged writable tail with valid prefix → future appends start in a fresh next segment
- Schema mismatch → ErrSchemaMismatch
- Two-consecutive-crash recovery works
- Crash during snapshot (lockfile) → skipped, recover from prior + log

**Dependencies:** Epic 2 (segment reader), Epic 3 (changeset decoder), Epic 5 (snapshot reader), SPEC-001 (ApplyChangeset, CommittedState, snapshot export hooks from Epic 8 Story 8.3)

---

## Epic 7: Log Compaction

**Spec sections:** §7

Delete segments fully covered by a snapshot.

**Scope:**
- Segment coverage analysis: compute [min_tx_id, max_tx_id] per sealed segment
- Safe deletion: only segments fully covered by snapshot
- Boundary segment retention: segment spanning snapshot boundary kept
- Active segment never deleted
- Post-snapshot compaction trigger

**Testable outcomes:**
- Snapshot at tx 1000, segment spans 900–1100 → retained
- Snapshot at tx 1000, segment spans 1–900 → deleted
- Active segment never deleted regardless of coverage
- No snapshot → no compaction
- Compaction after snapshot is fsynced, not before

**Dependencies:** Story 6.1 (segment metadata), Epic 5 (snapshot completion)

---

## Dependency Graph

```
SPEC-001 Epic 1 (Value types)
  └── Epic 1: BSATN Codec
        └── Epic 3: Changeset Codec
        └── Epic 5: Snapshot I/O
Epic 2: Record Format & Segment I/O
  └── Epic 4: Durability Worker ← Epic 3
  └── Epic 6: Recovery ← Epic 3, Epic 5
  └── Epic 7: Log Compaction ← Epic 5
```

## Error Types

Errors introduced where first needed:

| Error | Introduced in |
|---|---|
| `ErrUnknownValueTag` | Epic 1 |
| `ErrTypeTagMismatch` | Epic 1 |
| `ErrRowShapeMismatch` | Epic 1 |
| `ErrRowLengthMismatch` | Epic 1 |
| `ErrInvalidUTF8` | Epic 1 |
| `ErrBadMagic` | Epic 2 |
| `ErrBadVersion` | Epic 2 |
| `ErrBadFlags` | Epic 2 |
| `ErrUnknownRecordType` | Epic 2 |
| `ErrChecksumMismatch` | Epic 2 |
| `ErrRecordTooLarge` | Epic 2 |
| `ErrRowTooLarge` | Epic 3 |
| `ErrDurabilityFailed` | Epic 4 |
| `ErrSnapshotIncomplete` | Epic 5 |
| `ErrSnapshotHashMismatch` | Epic 5 |
| `ErrSnapshotInProgress` | Epic 5 |
| `ErrSchemaMismatch` | Epic 6 |
| `ErrHistoryGap` | Epic 6 |
| `ErrMissingBaseSnapshot` | Epic 6 |
| `ErrNoData` | Epic 6 |
