# Phase 4 Slice 2α — per-segment offset index

Records the decision for the next format-level commitlog parity sub-slice
called out in `docs/spacetimedb-parity-roadmap.md` Phase 4 Slice 2 and in
`NEXT_SESSION_HANDOFF.md` (Option 2α, "commitlog offset index file").
Written before code so follow-up agents have a locked spec.

Written clean-room. Reference paths below are cited for behavioral
grounding only; do not copy or transliterate Rust source.

## Reference shape (target)

`reference/SpacetimeDB/crates/commitlog/`:

- `src/index/mod.rs` — `IndexError` enum (`Io`, `OutOfRange`,
  `KeyNotFound`, `InvalidInput(last, input)`, `InvalidFormat`) and the
  `IndexFile` / `IndexFileMut` re-exports.
- `src/index/indexfile.rs` — on-disk layout and operations.
  - Fixed-size mmap-backed file, pre-allocated to
    `cap * ENTRY_SIZE` bytes. `ENTRY_SIZE = 16` = two little-endian
    `u64` (key, value). Generic key via `Into<u64> + From<u64>`.
  - Sentinel: key `0` is reserved as "empty slot". `num_entries` is
    reconstructed on open by scanning linearly until first `0`-key
    (`indexfile.rs:74-87`).
  - `append(key, value)`: rejects when `last_key >= key`
    (`InvalidInput`) or when the file is full (`OutOfRange`). Writes
    in place via the mmap, no separate fsync. `append` is explicitly
    append-only; keys monotonically ascending (`indexfile.rs:164-183`).
  - `find_index(key)`: binary search for the largest entry whose key
    is `<= key`. Returns `KeyNotFound` if `key` is smaller than the
    first entry or the index is empty
    (`indexfile.rs:94-124`).
  - `key_lookup(key)`: thin wrapper over `find_index` returning the
    stored `(found_key, value)` pair (`indexfile.rs:153-156`).
  - `truncate(key)`: drop entries with key `>= key`. If key is smaller
    than first entry, drop everything. Tail of mmap is zero-filled and
    `flush()`ed (`indexfile.rs:197-234`).
  - `async_flush()`: best-effort `MmapMut::flush_async`
    (`indexfile.rs:189-191`).
  - `IndexFile` = read-only wrapper over `IndexFileMut` for reader
    contexts (`indexfile.rs:262-311`).
- `src/repo/mod.rs:25-26, 130-186` — per-segment indexing contract:
  `TxOffsetIndexMut = IndexFileMut<TxOffset>` and
  `TxOffsetIndex = IndexFile<TxOffset>`. `Repo` trait exposes
  `create_offset_index(offset, cap)`,
  `get_offset_index(offset)`,
  `remove_offset_index(offset)`. One index file per segment; named by
  the segment's initial offset.
- `src/segment.rs:299-405` — `OffsetIndexWriter` wraps
  `TxOffsetIndexMut` with cadence state:
  - `min_write_interval = opts.offset_index_interval_bytes` — append
    only when bytes-since-last-append exceeds this threshold.
  - `require_segment_fsync = opts.offset_index_require_segment_fsync`
    — when true, candidate entries are flushed only at
    segment-fsync time; when false, `append_after_commit` may flush
    eagerly.
  - Candidate state: `candidate_min_tx_offset`,
    `candidate_byte_offset` — pending entry, not yet written.
  - `append_after_commit(min_tx_offset, bytes_written, commit_len)`
    (`src/segment.rs:329-347`): after every commit, update bytes
    counter; if over threshold, promote candidate to index append
    immediately.
  - `FileLike::fsync` (`src/segment.rs:368-380`): flush candidate +
    async-flush mmap. Errors are logged and swallowed — the index is
    advisory.
  - `FileLike::ftruncate` (`src/segment.rs:381-390`): delegate to
    `IndexFileMut::truncate(tx_offset)` on segment tail-truncation.
- `src/segment.rs:444-513, 616-700` — read path. `Reader::seek_to_offset`
  uses `index_file.key_lookup(start_tx_offset)` to jump to the largest
  indexed tx offset `<= start`, then walks forward through commits
  until exact. `Metadata::extract` accepts an optional index to
  accelerate metadata extraction during segment reopen.
- `src/commitlog.rs:469-471, 721, 1015-1022` — recovery /
  `commits_from` ties into the index via
  `try_seek_using_offset_index`. The read path always has a linear
  fallback if the index is missing or lookup fails.

Summary of reference behavioral contract:

- advisory, per-segment sparse index; key = tx offset, value = byte
  offset within segment;
- write path is cadence-gated by bytes-written since last append, not
  by commit count;
- read path uses `key_lookup` + walk; falls back to linear scan if
  index missing;
- truncate mirrors segment truncate on recovery-driven tail unwind;
- crash model: the index never becomes authoritative — partial writes
  are recoverable because `0`-key stops the entry-count scan, and
  missing/inconsistent indexes degrade to linear scan.

## Shunter shape today

`commitlog/` package has no offset index. All seek and replay paths
are linear scans from segment start:

- `commitlog/segment.go:163-285` — `SegmentWriter` tracks only
  `size`, `startTx`, `lastTx`. `Append(rec)` appends sequentially;
  there is no per-record byte-offset emission.
- `commitlog/segment.go:286-342` — `SegmentReader` has `Next()` only;
  no seek, no random access. `StartTxID` / `LastTxID` provide
  coarse bounds.
- `commitlog/replay.go:17-79` — `ReplayLog` skips a segment entirely
  when `segment.LastTx <= fromTxID`, then scans records linearly
  inside each surviving segment. When the resume horizon lies in the
  middle of a segment, replay decodes and discards every record below
  the horizon before it reaches a useful record. The cost is linear
  in `(fromTxID - segment.StartTx)` records.
- `commitlog/segment_scan.go:ScanSegments` /
  `commitlog/segment.go:OpenSegmentForAppend` — recovery scans each
  candidate segment end-to-end to find the last valid record and
  decide `AppendMode`. No index-driven shortcut.
- `commitlog/compaction.go` — compaction drops whole segments. No
  paired index files exist today, so there is no sidecar cleanup
  obligation yet.

No externally observable correctness gap. The gap is purely
performance: a commit log with a large snapshot horizon or a dense
segment pays a linear cost on every reopen / range read that could be
`O(log n) + (cadence window)` with a sparse offset index.

## Decision: what to build in slice 2α

Land a clean-room per-segment offset index under `commitlog/` that
matches the reference behavioral contract above. No Rust-to-Go port;
re-derive from the locked contract below.

### Artifacts

- new file `commitlog/offset_index.go` — `OffsetIndex` (read-only),
  `OffsetIndexMut` (writer), `OffsetIndexWriter` (cadence wrapper).
- new file `commitlog/offset_index_test.go` — unit pins.
- new integration pins in `commitlog/segment_test.go` and
  `commitlog/replay_test.go`.
- edits in `commitlog/segment.go` to thread optional index-writer
  hook through `SegmentWriter.Append`, `Sync`, and add a reader-side
  `SeekToTxID`.
- edits in `commitlog/durability.go` to create / close / rotate the
  index alongside the segment.
- edits in `commitlog/replay.go` to use the index when available to
  jump past the resume horizon inside a surviving segment.
- edits in `commitlog/compaction.go` to delete the sidecar index
  file when its segment is dropped.
- new error types in `commitlog/errors.go` matching the existing
  sentinel + typed-struct convention.

### On-disk layout

Sidecar file `<dir>/<startTxID>.idx` (`%020d.idx`) next to the
segment's `%020d.log`. Each entry is 16 bytes:

```
offset  size  field
 0       8    tx_id      uint64 little-endian
 8       8    byte_offset uint64 little-endian
```

Keys stored are the **first tx id of an indexed commit** (`min tx
offset` in reference terms); values are the segment byte offset of
that commit's record header (i.e. the position a reader would
`Seek(SeekStart)` to before calling `DecodeRecord`).

The file is pre-allocated to `cap * 16` bytes at create time; unused
entries are left as all-zero, which the sentinel rule treats as
"absent". `cap` derives from options (see below).

Endianness: little-endian, matching reference and matching the
existing record header encoding in `segment.go`.

**Sentinel:** key `0` is reserved as "empty slot". Tx id `0` is
already invalid in the existing segment contract (first tx in a
segment is at least `startTxID >= 1`), so the sentinel does not
collide with real data.

**Entry count on open:** linear scan from index 0 until first
`0`-key or file end, matching reference `num_entries`. This makes
partial writes self-truncating — any entry not fully written leaves
a `0`-key tail.

### Writer cadence

- `OffsetIndexWriter` state: `head *OffsetIndexMut`,
  `minWriteIntervalBytes uint64`, `bytesSinceLastAppend uint64`,
  `candidateTxID types.TxID`, `candidateByteOffset uint64`,
  `haveCandidate bool`.
- After each `SegmentWriter.Append`, the caller invokes
  `OffsetIndexWriter.AppendAfterCommit(txID, byteOffset, recordLen)`.
  Implementation:
  - add `recordLen` to `bytesSinceLastAppend`;
  - if no candidate yet, stash `(txID, byteOffset)` as candidate and
    return;
  - if `bytesSinceLastAppend >= minWriteIntervalBytes`, flush the
    candidate to the underlying index (reset counters) and stash the
    new entry as the next candidate;
  - otherwise retain the existing candidate (earliest wins — we want
    the smallest tx id per cadence window).
- `OffsetIndexWriter.Sync()` — called from `SegmentWriter.Sync` —
  flushes the pending candidate (if any) and issues the backing
  `f.Sync()` on the index file. This gives the index the **same
  durability point** as the segment: if the segment is durable up to
  tx N, the index is durable up to at least the last-appended
  indexed tx `<= N`.
- `OffsetIndexWriter.Truncate(txID)` — called from recovery paths
  that truncate the segment tail (`OpenSegmentForAppend` and any
  future recovery unwind). Drops entries with key `>= txID` from the
  underlying index and zero-fills the tail.

**Cadence target.** Default `minWriteIntervalBytes = 64 KiB`. Typical
record payload is kilobytes; this yields one index entry per roughly
16-64 commits. Per-segment capacity then sits around
`MaxSegmentSize / minWriteIntervalBytes` = `(512 MiB) / (64 KiB)` =
8192 entries. Allocate `cap = 16384` (= 256 KiB sidecar file) as
headroom. Both values expose themselves as `CommitLogOptions` fields:

```
OffsetIndexIntervalBytes uint64 // default 64 << 10
OffsetIndexCap           uint64 // default 16384
```

Zero `OffsetIndexIntervalBytes` disables indexing (writer becomes a
no-op, no sidecar is created). Test harnesses set this to `0` to
suppress the sidecar when they do not care about seek behavior.

### Reader path

`OffsetIndex` (read-only) mirrors the reference `IndexFile`:
`OpenOffsetIndex(path) (*OffsetIndex, error)`, `KeyLookup(txID)
(found types.TxID, byteOffset uint64, err error)`, `Entries()
iterator`.

New `SegmentReader.SeekToTxID(target types.TxID, idx *OffsetIndex)
error`:

1. If `idx == nil` — linear fallback: reset to header, `Next()` until
   `rec.TxID == target` (or exhausted).
2. Else `idx.KeyLookup(target)` to find `(foundTxID, byteOffset)`:
   - `KeyNotFound` → linear fallback from segment header.
   - any other error → linear fallback after logging (the index is
     advisory).
   - success → `Seek(byteOffset, SeekStart)`, then walk forward with
     `Next()` until `lastTx == target` or `target` passed.

Read path never fails because of index corruption; it degrades to
linear scan. This is the reference posture and is required to keep
crash-safety pluggable from above.

### Replay integration

`commitlog/replay.go`:

- `ReplayLog` takes the same `segments []SegmentInfo`, and an
  optional index per segment (looked up via `<startTxID>.idx`). For
  a segment whose `StartTx <= fromTxID < LastTx`:
  - if index exists: `idx.KeyLookup(fromTxID + 1)` → seek reader to
    `byteOffset`, then read records as usual (the existing
    `txID <= fromTxID` guard at `replay.go:43-48` still skips any
    records between the sparse index entry and the exact horizon;
    nothing else changes).
  - if no index: current linear skip behavior.
- For segments whose `StartTx > fromTxID`: unchanged. Read from
  segment start.
- For segments whose `LastTx <= fromTxID`: unchanged. Skip entirely
  (the existing early-out at `replay.go:21-23` still applies).

### Durability / rotation integration

`commitlog/durability.go`:

- `DurabilityWorker` owns an optional `OffsetIndexWriter` alongside
  `seg *SegmentWriter`.
- On `NewDurabilityWorkerWithResumePlan` + each segment rotation
  inside `processBatch`, construct the paired index (open or create
  the sidecar). If index construction fails, log and proceed without
  indexing; do not fail the durability worker (index is advisory).
- On `SegmentWriter.Append`, the worker captures
  `byteOffset = seg.Size() - (RecordOverhead + len(rec.Payload))`
  post-append and calls `OffsetIndexWriter.AppendAfterCommit`.
- On `SegmentWriter.Sync`, the worker calls
  `OffsetIndexWriter.Sync()` after the segment fsyncs. Ordering:
  segment fsync first, then index fsync, so the index can never
  reference a byte offset not yet durable.
- On segment rotation, the worker closes the current index and
  opens a new index for the fresh segment. If a
  `AppendByFreshNextSegment` resume plan is taken, the fresh segment
  gets a fresh index.

### Compaction integration

`commitlog/compaction.go` already enumerates segments to drop. Before
`os.Remove(logPath)`, also attempt `os.Remove(sidecarPath)` and
ignore `os.IsNotExist` — old deployments without indexes stay
correct.

### Error model

New additions to `commitlog/errors.go` matching the existing style:

```go
var (
    ErrOffsetIndexKeyNotFound = errors.New("commitlog: offset index key not found")
    ErrOffsetIndexFull        = errors.New("commitlog: offset index full")
    ErrOffsetIndexCorrupt     = errors.New("commitlog: offset index corrupt")
)

type OffsetIndexNonMonotonicError struct {
    Last uint64
    Got  uint64
}
func (e *OffsetIndexNonMonotonicError) Error() string {
    return fmt.Sprintf("commitlog: offset index non-monotonic: last=%d got=%d", e.Last, e.Got)
}
```

Callers in reader / replay paths *must not* propagate these errors
to the user — log and fall back to linear scan.
`OffsetIndexWriter.AppendAfterCommit` on a full index logs once,
stops writing for this segment, and continues (the tail of the
segment is simply unindexed).

### What this slice does **not** change

- recovery scan in `OpenSegmentForAppend` stays linear. Index
  lookups during recovery would require trusting the index across a
  crash, which is a separate decision with more failure modes.
- snapshot selection (`snapshot_select.go`) unchanged.
- typed error enums for `Traversal` / `Open` (reference
  `src/error.rs`) — separate slice, tracked as Phase 4 Slice 2β.
- record / log-shape format compatibility with the reference wire —
  separate slice, tracked as Phase 4 Slice 2γ.
- migration of existing deployments. `commitlog/` has no on-disk
  backward-compat requirement yet; missing-sidecar is graceful.
- `CLIENT_CHANNEL_CAPACITY`-style precise numeric parity on the
  cadence threshold — we pick a sensible Shunter default and let
  callers override via options.
- BSATN / protocol-level changes.
- mmap. The Go implementation uses `pread` / `pwrite` over a
  regular `*os.File`. Correctness is identical; mmap is a perf
  optimization left for a later slice if profiling demands it.

## Pin plan

Unit pins in `commitlog/offset_index_test.go`:

1. `TestOffsetIndexAppendAndLookupExact` — append `(k, v)` pairs,
   verify each exact key returns its own value.
2. `TestOffsetIndexLookupLargestLessOrEqual` — sparse keys, assert
   `KeyLookup(k+1)` returns the entry for `k`.
3. `TestOffsetIndexKeyNotFoundBelowFirst` — `KeyLookup(firstKey-1)`
   returns `ErrOffsetIndexKeyNotFound`.
4. `TestOffsetIndexEmpty` — fresh index, `KeyLookup(any)` returns
   `ErrOffsetIndexKeyNotFound`.
5. `TestOffsetIndexNonMonotonicAppendRejected` — append with key
   `<=` last key returns `*OffsetIndexNonMonotonicError`.
6. `TestOffsetIndexAppendBeyondCap` — fill to `cap`, next append
   returns `ErrOffsetIndexFull`.
7. `TestOffsetIndexTruncateAtExistingKey` — truncate drops target
   and everything after; `KeyLookup(target-1)` still succeeds,
   `KeyLookup(target)` returns `ErrOffsetIndexKeyNotFound`.
8. `TestOffsetIndexTruncateBelowFirstEmptiesIndex` — truncate at
   key less than first entry empties the file.
9. `TestOffsetIndexReopenRecoversNumEntries` — write N entries,
   close, reopen, assert `Entries()` yields the same N pairs in
   order.
10. `TestOffsetIndexPartialTailIsIgnored` — write N entries, then
    manually zero the trailing half-entry; reopen, assert entry
    count is N (partial write stops the scan at first `0`-key).
11. `TestOffsetIndexWriterCadenceHoldsCandidate` — feed the cadence
    writer smaller-than-interval commits; assert no entries
    appended until the running total crosses
    `minWriteIntervalBytes`.
12. `TestOffsetIndexWriterCadenceFlushesOnSync` — feed sub-interval
    commits, call `Sync()`, assert candidate is promoted.
13. `TestOffsetIndexWriterCadenceAdvancesEarliestInWindow` —
    within a single window, multiple commits should yield the
    earliest (lowest tx id) as the candidate.

Integration pins:

14. `TestSegmentReaderSeekToTxIDUsesIndex` —
    `commitlog/segment_test.go`: populate a segment with records at
    txs `10, 20, 30, 40, 50`, produce a sparse index
    covering `{10, 30, 50}`; `SeekToTxID(35)` lands on the record
    for tx `40` on first `Next()`.
15. `TestSegmentReaderSeekToTxIDFallsBackWithoutIndex` —
    `SegmentReader.SeekToTxID(target, nil)` returns the same result
    as the indexed path (linear fallback correctness).
16. `TestSegmentReaderSeekToTxIDFallsBackOnMissingKey` —
    `idx.KeyLookup(target)` returns `KeyNotFound`; seek still
    succeeds via linear fallback.
17. `TestReplayLogUsesIndexToSkipPastHorizon` —
    `commitlog/replay_test.go`: build a segment with 1024 records,
    populate a sparse index, replay from a mid-segment horizon,
    assert the replay decoded strictly fewer records than the
    linear path would have (measure via a counting hook).
18. `TestReplayLogCorrectWhenIndexMissing` — same scenario but
    delete the sidecar before replay, assert identical final state.
19. `TestDurabilityWorkerCreatesAndPopulatesIndexPerSegment` —
    `commitlog/durability_test.go` (add if absent): enqueue enough
    commits to cross the cadence threshold, `Close()`, open the
    sidecar, assert entries are present and monotonic.
20. `TestDurabilityWorkerRotatesIndexOnSegmentRotation` — set a
    small `MaxSegmentSize` to force rotation, assert each segment
    has its own sidecar and entries refer to each segment's
    coordinate space.
21. `TestCompactionRemovesSidecarIndex` —
    `commitlog/compaction_test.go`: compact a segment, assert the
    `.idx` file is gone alongside the `.log`.

Crash-safety pins:

22. `TestOffsetIndexWriterSurvivesCrashBeforeFsync` — write entries
    via `AppendAfterCommit` but **do not** `Sync()`; reopen, assert
    reader yields the entries that happened to land before the
    crash (partial-tail tolerant); final state must never error.
23. `TestReplayCorrectAfterPartialIndexTail` — write a segment and
    its sidecar, truncate the sidecar to a half-entry, replay,
    assert final state matches a clean-index replay.

## Session breakdown

This slice is too big for a single session. Plan:

- **Session 1 (this doc).** Decision doc only. No code. Lock the
  spec. Update ledger + handoff.
- **Session 2.** Build `OffsetIndex` / `OffsetIndexMut` /
  `OffsetIndexWriter` in `commitlog/offset_index.go` plus pins 1-13
  (unit). Do not touch `SegmentWriter` or `DurabilityWorker`. Land
  when `rtk go test ./commitlog -run OffsetIndex` is green and the
  full suite still matches the 1420 baseline.
- **Session 3.** Wire into `SegmentWriter` / `SegmentReader` +
  pins 14-16 (seek). Still no durability / replay integration.
- **Session 4.** Wire into `DurabilityWorker` (create / sync /
  rotate / close) + pins 19-20. At this point indexes are being
  produced but nothing reads them during replay.
- **Session 5.** Wire into `ReplayLog` + pins 17-18, wire into
  `compaction.go` + pin 21, land crash-safety pins 22-23. Update
  handoff to close the slice.

Each session ends with:

- targeted `rtk go test ./commitlog` green;
- `rtk go fmt` + `rtk go vet` clean on touched files;
- `rtk go test ./...` matches or exceeds the clean-tree baseline;
- `NEXT_SESSION_HANDOFF.md` updated naming the next concrete step;
- `docs/parity-phase0-ledger.md` updated if any ledger row moves.

## Acceptance gate for the whole slice

Close the slice only when all of:

- every pin in the plan above is landed and passing;
- no externally observable regression — the `1420` clean-tree
  baseline rises by the number of net-new pins without touching any
  existing pin;
- replay with a mid-segment horizon is measurably cheaper on a
  synthetic segment (no hard numeric target in parity land, but the
  counting hook in pin 17 must show reduction);
- `commitlog/compaction.go` cleans up sidecars;
- `NEXT_SESSION_HANDOFF.md` "What just landed" summarizes cadence,
  capacity, and the per-segment sidecar convention;
- `docs/parity-phase0-ledger.md` records Phase 4 Slice 2α as
  closed and names the remaining 2β / 2γ sub-slices;
- `TECH-DEBT.md` moves any offset-index entry out of the open
  list.

## Out-of-scope follow-ons

- Phase 4 Slice 2β — typed `Traversal` / `Open` error enums matching
  reference `src/error.rs`.
- Phase 4 Slice 2γ — record / log on-disk shape parity with the
  reference wire (header fields, version negotiation, trailer).
- Offset-index-assisted recovery scan in `OpenSegmentForAppend` —
  requires a separate crash-safety decision doc before code.
- mmap-backed index — perf optimization, only after profiling shows
  `pread`/`pwrite` is the bottleneck.

## Clean-room reminder

Reference citations above are grounding only. Implementation must be
re-derived in Go from the locked contract, not translated from the
Rust source. Errors, type names, method names, and structure should
follow the existing `commitlog/` package conventions (sentinel vars +
typed-struct errors, `io.Reader`/`io.Writer` style where possible,
no mmap, standard library I/O).
