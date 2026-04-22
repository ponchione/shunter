# Next session handoff

Use this file to start the next agent on the next real Shunter parity / hardening step with no prior context.

## What just landed (2026-04-22, Phase 4 Slice 2γ Session 1 — record / log shape decision doc, divergence audit locked)

Session 1 opens Phase 4 Slice 2γ with a decision-doc-only deliverable. No code. Deliverable: `docs/parity-phase4-slice2-record-shape.md`.

Audit outcome:

- **Reference wire extracted** from `reference/SpacetimeDB/crates/commitlog/src/commit.rs` and `segment.rs`: 10-byte segment header (`(ds)^2` magic + version + checksum-algorithm + 2 reserved), 22-byte V1 commit header (`min_tx_offset u64 LE + epoch u64 LE + n u16 LE + len u32 LE`), opaque N-record buffer, trailing CRC32C. V0 compat (no epoch, 14-byte header). All-zero header = EOS sentinel. CRC32C over header + records, excluding trailing CRC.
- **Shunter wire documented**: 8-byte segment header (`SHNT` magic + version + flags + 2 padding, all strict-zero), 14-byte per-record header (`TxID u64 LE + record_type u8 + flags u8 + data_len u32 LE`), payload = Shunter-canonical `Changeset` (versioned, BSATN-encoded rows), trailing CRC32C over header + payload.
- **Delta taxonomy**: 26 entries across 4 categories. 11 match or match-semantically (CRC algo / CRC width / integer endianness / offset index sidecar / history-gap detection / etc.); 7 structural (framing unit is record vs commit; no epoch; no commit `n` / `min_tx_offset`; typed record-type discriminator byte; record-flags byte; header size; reference V0/V1 split absent in Shunter); 5 behavioral (strict reserved bytes; no zero-header EOS; per-record CRC vs per-commit CRC scope; no preallocation tolerance; no `set_epoch` API); 2 semantic renames (byte 7 is `flags` vs `checksum_algorithm`; `len` is per-record vs batch); 1 explicit missing feature (forked-offset detection, already deferred by 2β).
- **Decision**: 2γ closes as a **documented-divergence slice**, not a byte-parity rewrite. Rationale: (1) scope explosion — delta entry #19 (records-buffer format) would couple commitlog parity to BSATN / schema / types / subscription / executor; (2) no operational-replacement trigger today; (3) migration cost for any header change invalidates existing segments; (4) several structural divergences (1:1 tx:record, no epoch, typed record-type) are deliberate Shunter design.
- **Session 2 deliverable**: a 33-pin wire-shape contract suite in new file `commitlog/wire_shape_test.go`. No `segment.go` / `changeset_codec.go` / `durability.go` / `replay.go` / `recovery.go` / `snapshot_io.go` changes. Pin categories: segment-header layout (8 pins), record-header layout (8 pins), CRC algorithm (4 pins), changeset payload layout (6 pins), divergence-from-reference behavioral contract (5 pins), constants (1 pin), integration round-trip (1 pin).

Explicitly deferred (each carried forward in the decision doc's out-of-scope list):

- Reference byte-compatible segment magic (`(ds)^2` vs `SHNT`).
- Reference commit grouping (N transactions per physical unit; requires reshaping durability worker, header, replay).
- Reference `epoch` field + `set_epoch` API (requires leader election; not on roadmap).
- Reference V0/V1 version split (Shunter is V1 permanently).
- All-zero-header EOS sentinel + preallocation-friendly writes (no current consumer).
- Checksum-algorithm negotiation byte (today byte 5 is `flags`, rejection semantics already match; rename is purely documentary).
- Forked-offset detection (`Traversal::Forked`) — reconfirmed deferred from 2β.
- Full records-buffer format parity (couples to BSATN / types / schema / subscription / executor by an order of magnitude).
- Reference `Append<T>` payload-return API — reconfirmed deferred from 2β.

Verification:
- `rtk go test ./commitlog -count=1` → `Go test: 185 passed in 1 packages` (clean-tree baseline confirmed before and after session-1 doc edits; no code touched).
- `rtk go test ./...` → `Go test: 1511 passed in 10 packages` (current clean-tree truth; the handoff's earlier 1501 figure counted only non-subtest runs, re-counting with subtests included yields 1511).
- Session 1 edits are doc-only: `docs/parity-phase4-slice2-record-shape.md` (new), `docs/parity-phase0-ledger.md` (2γ row flipped to `in_progress`), `TECH-DEBT.md` OI-007 (2γ decision-doc paragraph added), `NEXT_SESSION_HANDOFF.md` (this section).

Clean-tree baseline: `Go test: 1511 passed in 10 packages`.

## Next session: Phase 4 Slice 2γ Session 2 — wire-shape pin suite

Implement the 33-pin `commitlog/wire_shape_test.go` per the decision doc's pin plan. No production-code changes; tests only. Land when:

- `rtk go test ./commitlog -run WireShape -count=1 -v` green;
- `rtk go test ./commitlog -count=1` baseline rises by ≥33 from 185;
- `rtk go test ./...` meets or exceeds 1511 + (net-new pin count);
- `rtk go fmt ./commitlog` / `rtk go vet ./commitlog` clean;
- `docs/parity-phase0-ledger.md` 2γ row flipped from `in_progress` to `closed (divergences recorded)`;
- `TECH-DEBT.md` OI-007 updated to name 2γ closed;
- this handoff's top-of-file "What just landed" section updated to summarize Session 2.

If the Session-2 implementer discovers a byte offset or constant that differs from the decision doc's claims, stop and update `docs/parity-phase4-slice2-record-shape.md` first, land the doc edit, then resume.

## What landed earlier (2026-04-22, Phase 4 Slice 2β Session 2 — category sentinels + call-site wraps, slice closed)

Session 2 closes Phase 4 Slice 2β — typed `Traversal` / `Open` error enums. Decision doc `docs/parity-phase4-slice2-errors.md` is unchanged — Session 2 is pure implementation against the locked spec.

Landed:

- `commitlog/errors.go`:
  - five category sentinels: `ErrTraversal`, `ErrOpen`, `ErrDurability`, `ErrSnapshot`, `ErrIndex`.
  - `wrapCategory(cat, leaf error) error` helper + unexported `categorizedError` struct. `Error()` returns the leaf's surface text unchanged; `Unwrap() []error` returns `{leaf, cat}` so Go 1.20+ multi-Unwrap `errors.Is` matches both. Nil guards: `wrapCategory(nil, leaf) = leaf`; `wrapCategory(cat, nil) = nil`.
  - `Is(target error) bool` methods on the nine typed structs — `BadVersionError`/`HistoryGapError` → `ErrOpen`; `UnknownRecordTypeError`/`ChecksumMismatchError`/`RecordTooLargeError`/`RowTooLargeError` → `ErrTraversal`; `SchemaMismatchError`/`SnapshotHashMismatchError` → `ErrSnapshot`; `OffsetIndexNonMonotonicError` → `ErrIndex`. Each returns true iff target matches its category. `SchemaMismatchError.Unwrap()` → `Cause` is preserved (already existed; the new `Is` method does not interfere with it).
- `commitlog/segment.go`:
  - `ReadSegmentHeader`: bare `ErrBadMagic` / `ErrBadFlags` returns now wrap with `ErrOpen`.
  - `DecodeRecord`: three `ErrTruncatedRecord` returns and the mid-record `ErrBadFlags` return wrap with `ErrTraversal`. Typed struct returns (`BadVersionError`, `ChecksumMismatchError`, `RecordTooLargeError`, `UnknownRecordTypeError`) unchanged — their `Is` methods carry the category.
- `commitlog/segment_scan.go::scanNextRecord`:
  - two header-time truncation sites (remaining < RecordHeaderSize; EOF mid-header) wrap with `ErrOpen`.
  - three mid-record truncation sites (dataLen overflow; EOF mid-payload; EOF mid-CRC) wrap with `ErrTraversal`.
  - mid-record `ErrBadFlags` wraps with `ErrTraversal`.
  - `HistoryGapError` sites unchanged — `HistoryGapError.Is` carries `ErrOpen`.
- `commitlog/recovery.go::OpenAndRecoverDetailed`: `ErrNoData` and `ErrMissingBaseSnapshot` returns wrap with `ErrOpen`.
- `commitlog/snapshot_io.go`: `ErrSnapshotInProgress` wraps with `ErrSnapshot`; snapshot-header `ErrBadMagic` wraps with `ErrOpen`. `BadVersionError` and `SnapshotHashMismatchError` sites unchanged — their `Is` methods carry the category.
- `commitlog/snapshot_select.go`: `ErrMissingBaseSnapshot` return wraps with `ErrOpen`. `SchemaMismatchError` sites unchanged.
- `commitlog/durability.go`:
  - `validateFsyncMode`: `fmt.Errorf("%w: %d", ErrUnknownFsyncMode, mode)` → `fmt.Errorf("%w: %w: %d", ErrOpen, ErrUnknownFsyncMode, mode)` (multi-%w is fine here because the mode integer already mutates the surface text).
  - panic value in `EnqueueCommitted` (both fatal branches) grew `ErrDurability` as an additional leading `%w` so the recovered error's Unwrap chain contains `ErrDurability`, `ErrDurabilityFailed`, and the underlying fatal cause.
- `commitlog/offset_index.go`: `ErrOffsetIndexFull` (in `Append`) and both `ErrOffsetIndexKeyNotFound` sites (in `offsetIndexLookup`: empty index; target below first key) wrap with `ErrIndex`. `OffsetIndexNonMonotonicError.Is` carries `ErrIndex` for the typed case. `ErrOffsetIndexCorrupt` is declared but never emitted at present — no wrap needed.
- `commitlog/segment_test.go`: one existing pin (`TestSegmentReaderSeekToTxIDFallsBackOnMissingKey`) used raw `err != ErrOffsetIndexKeyNotFound` identity; swapped for `!errors.Is(err, ErrOffsetIndexKeyNotFound)` to match every other pin in the suite. This was the only raw-`==` sentinel check in the commitlog package; back-compat semantics are preserved via `errors.Is`.
- `commitlog/errors_category_test.go` (new file) — 28 pins:
  - typed-struct category pins 1-9 (Checksum / BadVersion / UnknownRecordType / RecordTooLarge / RowTooLarge / HistoryGap / SchemaMismatch / SnapshotHashMismatch / OffsetIndexNonMonotonic). Pin 7 additionally re-asserts `SchemaMismatchError.Unwrap()` → Cause still chains.
  - wrap-helper pins 10-12 (bad-magic wrap; same leaf different categories by site for truncated-record; nil-guard behavior).
  - end-to-end admission-seam pins 13-25 (bad-magic segment file; decode-record CRC flip; scan-segments history gap; replay mid-record CRC flip; recovery-no-data; recovery-missing-base-snapshot; snapshot-hash-mismatch; snapshot-select schema-mismatch; durability-worker fatal panic; durability-ctor unknown-fsync-mode; offset-index-key-not-found; offset-index-full; offset-index-non-monotonic).
  - back-compat pins 26-28: sentinel-identity preserved table-driven across every sentinel (incl. the fmt-wrapped `ErrUnknownFsyncMode` and `ErrDurabilityFailed` paths); typed-struct `errors.As` preserved table-driven across all nine structs; wrapped-error `Error()` surface text equals the leaf's text and never contains category strings ("traversal error", "open error", etc.) — one representative per sentinel family.

Explicitly preserved (back-compat pinned):
- no sentinel renames, no typed-struct renames, no surface `Error()` text change.
- `errors.Is(err, <leaf>)` for every sentinel still returns true after the category wrap.
- `errors.As(err, &<typed-struct>)` for every typed struct still succeeds.
- `SchemaMismatchError.Unwrap() → Cause` chain still works (pin 7).

Deliberately deferred to their own decision docs (unchanged from Session 1):
- reference `Traversal::Forked` detection (same offset, different CRC) — needs record-layer CRC tracking across reopens.
- reference `Append<T>` payload-return surface — requires public commitlog API change; Shunter durability worker currently owns the payload.
- record / log on-disk shape parity — covered by Phase 4 Slice 2γ (separate decision doc, not yet open).
- `source_chain` helper — not needed; `errors.Unwrap` + `%w` satisfy existing log formatting.

Verification:
- `rtk go test ./commitlog -run ErrorCategory -count=1 -v` → `Go test: 9 passed in 1 packages` (pins 1-9; the `-run ErrorCategory` filter matches only the typed-struct category pin names, by design)
- `rtk go test ./commitlog -count=1` → `Go test: 175 passed in 1 packages` (118 baseline + 57 subtest-counted test runs from the 28 new pins — `TestBackCompatSentinelIdentityPreserved`, `TestBackCompatTypedStructErrorAsStillWorks`, and `TestBackCompatErrorMessageUnchanged` each use `t.Run` subtests so go test counts them individually)
- `rtk go test ./...` → `Go test: 1501 passed in 10 packages` (1444 baseline + 57 new test runs). The decision doc's "1472 target" line assumed one test-run per pin; my subtest structure legitimately scores higher. No regression: every baseline test still passes.
- `rtk go fmt ./commitlog`, `rtk go vet ./commitlog` → clean

Ledger / debt follow-through:
- `docs/parity-phase0-ledger.md` — 2β row flipped from `in_progress` to `closed` with a summary of what landed; 2γ row flipped from `deferred` to `open (next)`.
- `TECH-DEBT.md` OI-007 — summary paragraph rewritten to name 2β closed and 2γ as the next open / deferred sub-slice (needs its own decision doc).
- `TECH-DEBT.md` OI-003 — summary paragraph updated to name 2α and 2β closed.

Clean-tree baseline at session close: `Go test: 1501 passed in 10 packages`.

## What landed earlier (2026-04-22, Phase 4 Slice 2α Session 5 — replay + compaction wiring, slice closed)

Session 5 closes the multi-session Phase 4 Slice 2α per-segment offset index slice. Decision doc `docs/parity-phase4-slice2-offset-index.md` is unchanged — session 5 is pure implementation against the locked spec.

Landed:

- `commitlog/replay.go`:
  - New helper `seekReplayReaderToHorizon(reader, segmentPath, startTx, fromTxID)`: no-op when `fromTxID < startTx`; otherwise derives `<dir>/OffsetIndexFileName(startTx)` via `filepath.Dir(segmentPath)`, stats the sidecar (early-return on any stat error including `IsNotExist`), opens via `OpenOffsetIndex`, and calls `reader.SeekToTxID(fromTxID+1, idx)`. Every error path (open failure, seek failure) logs and falls back to linear scan. The index handle is always closed via `defer idx.Close()`.
  - `ReplayLog` calls the helper after `OpenSegment` and before the per-segment read loop, gated on `segment.StartTx <= fromTxID`. The existing `txID <= fromTxID` guard at `replay.go:85-91` still skips any records between the sparse index entry and the exact horizon — no structural change to the traversal loop.
  - New unexported `replayDecodeHook func(*Record)` — fired once per record decoded inside ReplayLog's segment loop. Always nil in production; set by the pin-17 test to count records.
- `commitlog/compaction.go`:
  - `RunCompaction` loop now, after `os.Remove(logPath)`, also attempts `os.Remove(offsetIndexPathForSegment(path))` and ignores `os.IsNotExist`. New unexported helper `offsetIndexPathForSegment(segmentPath string) string` swaps `.log` → `.idx` on the canonical `%020d.log` filename.
- `commitlog/offset_index.go`:
  - `scanOffsetIndexPrefix` additionally treats `key != 0 && val == 0` as sentinel-empty (end of valid prefix). Real record byte offsets are always `>= SegmentHeaderSize (=8)`, so `val == 0` is a reliable indicator of a partial write where the key half landed but the value half did not (pre-allocated zero bytes showing through). Without this, a post-crash reopen on a key-only partial tail would yield a bogus entry with `byteOffset=0`, and any index-assisted seek would land at segment byte 0 (pre-header) and decode garbage. Pin 23 specifically locks this.
- No touches to `durability.go`, `segment.go`, `recovery.go`, or the snapshot path. Session 4's durability wiring is unchanged — Session 5 is strictly reader + cleanup side + one reader-tolerance fix.

Pins landed (5 decision-doc pins + 1 back-compat pin):

17. `TestReplayLogUsesIndexToSkipPastHorizon` (`replay_test.go`): builds a 1024-record segment via new helper `writeDenseReplaySegment`, populates a sparse index at every 64th record (cap=64), replays from `fromTxID=512` twice — once with sidecar present, once after `os.Remove(idxPath)`. Uses `countingReplayHook` to count records decoded by ReplayLog's loop on each pass. Observed counts: `indexed=512, linear=1024`. Asserts `indexed < linear` and `assertReplayStatesEqual` (same final committed state across both).
18. `TestReplayLogCorrectWhenIndexMissing`: same scenario, but runs one pass with a fresh sidecar and one pass after deletion; asserts the two final committed states are byte-identical via the same `assertReplayStatesEqual` helper.
21. `TestCompactionRemovesSidecarIndex` (`compaction_test.go`): three segments (start txs 1, 4, 7) each with a populated `.idx`; `RunCompaction(dir, 6)` drops segments 1 and 4; assert both their `.log` and `.idx` files are gone, segment 7 and its sidecar remain.
21b (bonus). `TestCompactionToleratesMissingSidecar`: two segments without any sidecars; `RunCompaction` completes cleanly — the `IsNotExist` guard keeps old deployments correct.
22. `TestOffsetIndexWriterSurvivesCrashBeforeFsync` (`offset_index_test.go`): feeds six commits (interval=100, recordLen=100) through `OffsetIndexWriter.AppendAfterCommit`; closes the writer WITHOUT calling `Sync()` first; reopens via `OpenOffsetIndex` (read-only); asserts at least one entry survives, every surviving entry corresponds to a commit that was fed in, and the surviving prefix is monotonic. The pending candidate slot is lost (expected) but flushed entries — already landed in the backing file via `WriteAt` — are readable.
23. `TestReplayCorrectAfterPartialIndexTail` (`replay_test.go`): writes a 1024-record segment + sparse index, runs a clean-index replay to capture the reference state, then opens the sidecar and zero-fills the value half of the last valid entry (simulating "key half landed, value half did not"). A fresh replay with the corrupted sidecar must produce a committed state identical to the clean-index replay. This exercises the new `val == 0` sentinel in `scanOffsetIndexPrefix`.

Per-segment sidecar convention (now fully realized): filename `%020d.idx` next to the paired `%020d.log`; 16-byte entries, two little-endian uint64 (tx id key, byte offset value); file pre-allocated to `OffsetIndexCap * 16` bytes with unused slots zero-filled. Cadence defaults: `OffsetIndexIntervalBytes = 64 KiB`, `OffsetIndexCap = 16384` (256 KiB sidecar per segment). Durability ordering: `seg.Sync()` before `idx.Sync()`, so the sidecar never references an undurable segment byte offset. Advisory posture throughout: every reader / replay / compaction path degrades to linear scan or silent skip on any index error; construction failures log and disable indexing rather than bubble.

Verification:
- `rtk go test ./commitlog -count=1` → `Go test: 118 passed in 1 packages`
- `rtk go test ./...` → `Go test: 1444 passed in 10 packages` (1438 baseline + 6 new pins)
- `rtk go fmt ./commitlog`, `rtk go vet ./commitlog` → clean

Ledger / debt follow-through:
- `docs/parity-phase0-ledger.md` — Slice 2α row flipped to `closed` with a full summary of Sessions 1-5; 2β row updated from `deferred (blocked on 2α)` to `open (next)`.
- `TECH-DEBT.md` OI-007 — 2α paragraph rewritten to name Session 5's reader + cleanup wiring and close-out; names 2β as the next open Phase 4 sub-slice.

Clean-tree baseline: `Go test: 1444 passed in 10 packages` (previous 1438 + 6 new pins — pins 17, 18, 21, 22, 23 plus `TestCompactionToleratesMissingSidecar`).

## What landed earlier (2026-04-21, Phase 4 Slice 2α Session 2 — standalone offset index writer/reader)

Session 2 of the multi-session Phase 4 Slice 2α per-segment offset index slice. Decision doc `docs/parity-phase4-slice2-offset-index.md` is unchanged — session 2 is pure implementation against the locked spec.

Landed:

- new `commitlog/offset_index.go` with three types:
  - `OffsetIndex` — read-only view. `OpenOffsetIndex(path)`, `KeyLookup(target types.TxID) (found types.TxID, byteOffset uint64, err error)`, `Entries()`, `NumEntries()`, `Close()`.
  - `OffsetIndexMut` — writer. `CreateOffsetIndex(path, cap)` pre-allocates `cap * 16` bytes of zero; `OpenOffsetIndexMut(path, cap)` reopens, extending to `cap*16` if the file is smaller and scanning the leading valid prefix. `Append(txID, byteOffset)`, `Truncate(target)`, `Sync()`, `Close()`, plus `KeyLookup` / `Entries` / `NumEntries` / `Cap` for symmetry with the read-only view.
  - `OffsetIndexWriter` — cadence wrapper over `OffsetIndexMut`. `NewOffsetIndexWriter(head, minWriteIntervalBytes)`, `AppendAfterCommit(txID, byteOffset, recordLen)`, `Sync()` (flushes pending candidate + head.Sync), `Truncate(target)`, `Close()`. First call stashes a candidate; subsequent calls inside a cadence window retain the earliest (lowest-txID) candidate; when `bytesSinceLastAppend >= minWriteIntervalBytes` on a later commit, the candidate is flushed and the incoming commit becomes the next candidate. `ErrOffsetIndexFull` is caught at the writer: index is marked full and subsequent appends are no-ops (advisory posture — index is always optional).
- on-disk layout per decision doc: 16-byte entries, two little-endian `uint64` (tx id key, byte offset value). Key `0` is the empty-slot sentinel; `Append(0, _)` rejects as non-monotonic on a fresh index. Pre-allocated `cap * 16` zero-filled slots on create; partial tail (zero key, arbitrary value bytes) is ignored on reopen per the sentinel rule.
- read path: `KeyLookup` binary-searches for the largest entry with key `<= target`. Empty index or target below first key returns `ErrOffsetIndexKeyNotFound`. `Truncate(target)` drops every entry with key `>= target` via binary-search + zero-fill (matches reference `truncate` semantics: `drop everything` when target < first key; `drop target and above` when target is found or between entries).
- new errors in `commitlog/errors.go`: sentinel `ErrOffsetIndexKeyNotFound`, `ErrOffsetIndexFull`, `ErrOffsetIndexCorrupt`; typed `OffsetIndexNonMonotonicError{Last, Got uint64}` using existing package style.
- no touches to `segment.go`, `durability.go`, `replay.go`, `recovery.go`, or `compaction.go` — strictly standalone. `OffsetIndexEntrySize` (16) exported for future segment-wiring code.
- pread/pwrite via `*os.File.ReadAt` / `WriteAt`; no mmap. Correctness matches the reference advisory contract; mmap remains deferred pending profiling.

Pins landed (`commitlog/offset_index_test.go`, pins 1-13 from the decision doc):

1. `TestOffsetIndexAppendAndLookupExact` — exact-key lookup returns its own value.
2. `TestOffsetIndexLookupLargestLessOrEqual` — sparse keys; `KeyLookup(k+1)` / `KeyLookup(anything above last)` return the correct predecessor entry.
3. `TestOffsetIndexKeyNotFoundBelowFirst` — below first key → `ErrOffsetIndexKeyNotFound`.
4. `TestOffsetIndexEmpty` — fresh index: any `KeyLookup` returns `ErrOffsetIndexKeyNotFound`, `Entries()` empty, `NumEntries==0`.
5. `TestOffsetIndexNonMonotonicAppendRejected` — duplicate key, smaller key, zero key (including zero on a fresh index) all return `*OffsetIndexNonMonotonicError` with `Last`/`Got` populated correctly.
6. `TestOffsetIndexAppendBeyondCap` — fill to `cap`, next append returns `ErrOffsetIndexFull`; count remains at cap.
7. `TestOffsetIndexTruncateAtExistingKey` — reference semantics: `Truncate(target)` drops the target entry and everything after; `KeyLookup(target-1)` still succeeds on the surviving prefix; target entry is absent from `Entries()`. (The decision doc's literal "KeyLookup(target) returns ErrOffsetIndexKeyNotFound" is physically impossible with "largest `<=`" semantics when a lower entry survives, so the pin expresses the intent via `Entries()` + `NumEntries` + the predecessor lookup succeeding.)
8. `TestOffsetIndexTruncateBelowFirstEmptiesIndex` — target below first empties the file; subsequent `Append` accepts any positive key.
9. `TestOffsetIndexReopenRecoversNumEntries` — write N, Sync, Close, reopen via `OpenOffsetIndexMut` and `OpenOffsetIndex`; both yield the same N entries in order.
10. `TestOffsetIndexPartialTailIsIgnored` — simulate partial write by writing only the value half of entry N (key half remains zero sentinel); reopen, assert `NumEntries == N`.
11. `TestOffsetIndexWriterCadenceHoldsCandidate` — sub-interval commits never flush.
12. `TestOffsetIndexWriterCadenceFlushesOnSync` — `Sync()` promotes the pending candidate; second `Sync()` is idempotent.
13. `TestOffsetIndexWriterCadenceAdvancesEarliestInWindow` — within a window, earliest (lowest-txID) commit wins the candidate slot; crossing the interval flushes the candidate and stashes the incoming commit.

Verification:
- `rtk go test ./commitlog -run OffsetIndex -count=1 -v` → `Go test: 13 passed in 1 packages`
- `rtk go test ./...` → `Go test: 1433 passed in 10 packages` (1420 baseline + 13 new pins)
- `rtk go fmt ./commitlog/...`, `rtk go vet ./commitlog/...` → clean

Clean-tree baseline at session 2 close: `Go test: 1433 passed in 10 packages` (previous 1420 + 13 new unit pins). Now 1436 after Session 3.

## What landed earlier (2026-04-21, column-kind widening Slice 5 — `KindArrayString` realized)

Fifth column-kind widening slice closes the last narrow reference-backed rows from `check.rs:487-489` (`select * from t where arr = :sender` / "The :sender param is an identity") and `check.rs:523-525` (`select t.* from t join s on t.arr = s.arr` / "Product values are not comparable") as **positive** parity contracts at the protocol admission surface. Both shapes were rejected incidentally today (coerce default branch for the first; join compile had no type guard for the second); pins now promote the rejection from incidental to named reference-parity contract once `arr` is instantiable as a column kind.

Representation. `types.ValueKind` gained `KindArrayString` (index 18). `types.Value` grew a `strArr []string` slot with a defensive-copy constructor (`NewArrayString`) and accessor (`AsArrayString`). `Equal` walks element-wise; `Compare` does lexicographic element compare with length tiebreak (unused at admission surface, kept for post-admission safety); `Hash` writes kind byte + u32 BE count + per-element (u32 BE length + utf8 bytes); `payloadLen` returns `4 + sum(4 + len(s))`.

BSATN. `TagArrayString byte = 18`. Encode: tag + u32 LE count + per-element (u32 LE length + utf8 bytes). Decode validates each element as utf8 via the existing `KindString` rule (`ErrInvalidUTF8` on failure). `EncodedValueSize` returns `1 + 4 + sum(4 + len(s))`. BSATN uses LE; the subscription canonical-hash uses BE — same convention as other primitives.

Coerce. `query/sql/coerce.go` adds an explicit `case types.KindArrayString` returning `mismatch()` for every scalar literal shape (no array literal grammar exists at the Shunter SQL surface). The `:sender` branch at the top of `coerceValue` already rejects non-KindBytes columns, so `arr = :sender` rejects with `:sender parameter cannot be coerced to ArrayString`.

Subscription canonical hash. `subscription/hash.go::encodeValue` adds `case types.KindArrayString:` writing u32 BE count + per-element (u32 BE length + utf8 bytes). `ArrayString{"alpha"}` and scalar `String "alpha"` hash distinctly because the kind tag differs; per-element ordering / length are reflected in the payload so `{"alpha","beta"}` vs `{"beta","alpha"}` also hash distinctly.

Join-ON guard. `protocol/handle_subscribe.go::compileSQLQueryString` adds a new `isArrayKind` helper (returns true for `KindArrayString`, extension point for future array element widenings) and rejects join-ON when either side is an array kind: `"join ON t.arr = s.arr: array/product values are not comparable"`. No runtime path lowers an array-array comparison onto `subscription.Join`.

Autoincrement. `schema.AutoIncrementBounds` returns `ok=false` for `KindArrayString` (pinned by the existing `TestAutoIncrementBoundsNonInteger` with a new entry).

Schema mapping. `schema.GoTypeToValueKind` continues to reject generic slices other than `[]byte`; column-kind instantiation remains library-API driven (explicit `ColumnSchema{Type: schema.KindArrayString}`), consistent with how 128/256-bit and Timestamp kinds landed.

Unlocked reference rows:
- `check.rs:487-489` `select * from t where arr = :sender` — rejects with ":sender parameter cannot be coerced to ArrayString". Pinned by `TestHandleSubscribeSingle_ParityArraySenderRejected` and `TestHandleOneOffQuery_ParityArraySenderRejected`.
- `check.rs:523-525` `select t.* from t join s on t.arr = s.arr` — rejects with "join ON t.arr = s.arr: array/product values are not comparable". Pinned by `TestHandleSubscribeSingle_ParityArrayJoinOnRejected` and `TestHandleOneOffQuery_ParityArrayJoinOnRejected`.

Verification:
- `rtk go test ./types -count=1` → adds 6 new tests (`TestRoundTripArrayString`, `TestArrayStringDefensiveCopyOn{Construct,Access}`, `TestEqualArrayString`, `TestCompareArrayString`, `TestAccessorArrayStringPanicsOnWrongKind`; `TestValueKindString` extended)
- `rtk go test ./bsatn -count=1` → adds 5 round-trip entries + `TestEncodedValueSizeArrayString` + `TestEncodeArrayStringLittleEndianLayout` + `TestDecodeArrayStringRejectsInvalidUTF8` (3 new tests)
- `rtk go test ./schema -count=1` → `valuekind_export_test.go` extended for ArrayString in both `TestValueKindExportStringAll` and `TestAutoIncrementBoundsNonInteger`
- `rtk go test ./query/sql -count=1` → 2 new coerce pins (`TestCoerceSenderRejectsArrayStringColumn`, `TestCoerceLiteralsRejectedOnArrayStringColumn`)
- `rtk go test ./subscription -count=1` → 2 new hash pins (`TestQueryHashArrayStringVsString`, `TestQueryHashArrayStringDiffersByPayload`) plus 3 new entries in `TestQueryHashAllKindsRoundTrip`
- `rtk go test ./protocol -run 'ParityArray' -count=1 -v` → 4 new protocol pins
- `rtk go fmt ./...`, `rtk go vet ./...` → clean
- `rtk go test ./...` → `Go test: 1420 passed in 10 packages`

Clean-tree baseline: `Go test: 1420 passed in 10 packages` (previous 1403 + 17 net new).

With this slice every narrow reference-backed SQL/query-surface anchor derivable from `check.rs` / `sub.rs` / `sql.rs` / `rls.rs` / `statement.rs` that is realizable against Shunter's column-kind enum is now drained. Product column kinds and non-String array element kinds remain unrealizable; neither has a narrow reference-backed pin anchor inviting a next slice. Candidates for the next session are all larger scope:

1. **Tier-B hardening** (OI-004 remaining watch items, OI-008 top-level bootstrap) — don't force without concrete leak evidence.
2. **Format-level commitlog parity** (offset index, typed errors, record shape) — each would need its own decision doc.
3. **One of the `P0-SCHED-001` deferrals** (`fn_start`-clamped "now", one-shot panic deletion, past-due intended-time ordering) if workload evidence surfaces.
4. **Generalize array element kinds beyond string** — requires either per-element-kind `Value` slots or a parameterized `KindArray(elem)` representation. Would eventually unlock product comparison too.

## What landed earlier (2026-04-21, column-kind widening Slice 4 — BigDecimal literal path / `u256 = 1e40`)

Fourth column-kind widening slice closes the last narrow reference-backed row from `check.rs:284-332` that was realizable against `schema.ValueKind`. A new `LitBigInt` literal kind (in `query/sql/parser.go`) carries an arbitrary-precision `*big.Int`. `parseNumericLiteral` now routes `.eE` token bodies through `big.Rat.SetString` first: if the rational is integer-valued (`r.IsInt()`), it collapses to `LitInt` when it fits int64 and otherwise promotes to `LitBigInt`. Non-integer rationals fall back to `strconv.ParseFloat(64)` and stay `LitFloat`. Plain integer bodies (no `.eE`) also promote to `LitBigInt` when they overflow int64.

`query/sql/coerce.go` gained four helpers (`coerceBigIntToInt128`, `coerceBigIntToUint128`, `coerceBigIntToInt256`, `coerceBigIntToUint256`). Each uses `big.Int.FillBytes` into a fixed-width big-endian buffer and `binary.BigEndian.Uint64` on each 8-byte slice to decompose into 2 / 4 uint64 words, with two's-complement materialization for negatives (add `2^128` / `2^256` before decompose) and range checks against `[int128Min, int128Max]` / `[-0, 2^128-1]` / `[int256Min, int256Max]` / `[0, 2^256-1]`. `KindFloat32` / `KindFloat64` accept `LitBigInt` via `new(big.Float).SetInt(x).Float64()` so the already-landed `f32 = 1e40 → +Inf` behavior is preserved after the parser promotes `1e40` to `LitBigInt`. The existing `LitInt` branches for 128/256-bit kinds are unchanged (preserved as the fast path for int64-sized literals).

Unlocked reference row:
- `check.rs:330-332` `select * from t where u256 = 1e40` / "u256" now accepts end-to-end. `1e40 = 10^40 = 0x1D...` fits u256 (max ~1.16e77); the parser produces `LitBigInt(10^40)` and coerce decomposes it as `(0, 0x1D..., ..., ...)`. Pinned by `TestHandleSubscribeSingle_ParityValidLiteralU256Scientific` and `TestHandleOneOffQuery_ParityValidLiteralU256Scientific`.

Verification:
- `rtk go test ./query/sql -count=1` → `Go test: 117 passed in 1 packages` (adds 2 parser + 9 coerce = 11 new tests; the old `TestParseWhereScientificNotationOverflowFloat` was superseded by the new `TestParseWhereScientificNotationOverflowBigInt` since `1e40` now parses as `LitBigInt` rather than `LitFloat`)
- `rtk go test ./protocol -run 'ParityValidLiteralU256Scientific|ParityUint256Negative|ParityValidLiteralOnEach' -count=1 -v` → 34 passed
- `rtk go fmt ./query/sql ./protocol`, `rtk go vet ./query/sql ./protocol` → clean
- `rtk go test ./...` → `Go test: 1403 passed in 10 packages`

Clean-tree baseline: `Go test: 1403 passed in 10 packages` (previous 1391 + 13 new tests − 1 superseded test = +12 net).

With this slice the `check.rs:284-332` `valid_literals` block is drained for every shape realizable against `schema.ValueKind`. Remaining candidates (all require larger-scope runtime widening):
1. **Array column kind (narrow: `KindString` elements only)** — recursive `Value` representation, new BSATN tag, minimal coerce surface; pin `SELECT * FROM t WHERE arr = :sender` rejection (which today fires incidentally via `Coerce` default branch) as a **positive** parity contract with array support. Biggest representation change.
2. **Product column kind** — would also unlock `check.rs:523-525` product-value comparison in join ON.

## What landed earlier (2026-04-21, column-kind widening Slice 3 — `Timestamp` realized)

Third column-kind widening slice. `types.ValueKind` gained `KindTimestamp` reusing the existing `i64` slot as microseconds since the Unix epoch (sign-extended for pre-epoch times); BSATN added tag 17 encoding 8 bytes LE; `query/sql/coerce.go` accepts `LitString` targeting a `KindTimestamp` column and parses RFC3339 shapes via two layouts — `2006-01-02T15:04:05.999999999Z07:00` and `2006-01-02 15:04:05.999999999Z07:00` — which together cover `T`/space separator, with/without fractional seconds (up to nanoseconds, truncated to micros via `time.UnixMicro`, matching reference `chrono::DateTime::timestamp_micros`), and `Z` or numeric offset; subscription canonical hashing writes 8 bytes big-endian with the `KindTimestamp` tag separating identical raw micros from an `Int64` payload. Autoincrement remains 64-bit-integer-only (`schema.AutoIncrementBounds` returns `ok=false` for Timestamp). `schema.GoTypeToValueKind` continues to reject Timestamp — column-kind instantiation stays library-API driven.

Unlocked reference rows:
- `check.rs:334-352` `valid_literals` rows for `ts = '2025-02-10T15:45:30Z'`, `ts = '2025-02-10T15:45:30.123Z'`, `ts = '2025-02-10T15:45:30.123456789Z'` (nanosecond precision truncated), `ts = '2025-02-10 15:45:30+02:00'`, and `ts = '2025-02-10 15:45:30.123+02:00'` now accept end-to-end. Pinned by `TestHandleSubscribeSingle_ParityTimestampLiteralAccepted` / `TestHandleOneOffQuery_ParityTimestampLiteralAccepted` (5 table-driven subtests each). Malformed timestamp literals (empty, date-only, `"not-a-timestamp"`, partial `2025-02-10T15:45`) continue to reject via `TestHandleSubscribeSingle_ParityTimestampMalformedRejected` / `TestHandleOneOffQuery_ParityTimestampMalformedRejected`.

Verification:
- `rtk go test ./types -count=1` → 4 new tests (`TestRoundTripTimestamp`, `TestEqualTimestamp`, `TestCompareTimestamp`, `TestAccessorTimestampPanicsOnWrongKind`)
- `rtk go test ./bsatn -count=1` → 5 new round-trip entries + `TestEncodedValueSizeTimestamp` + `TestEncodeTimestampLittleEndian` (byte-order pin)
- `rtk go test ./query/sql -count=1` → 5 new coerce pins (`TestCoerceStringLiteralToTimestamp` [5 shapes], `TestCoerceMalformedTimestampRejected`, `TestCoerceIntLiteralOnTimestampRejected`, `TestCoerceFloatLiteralOnTimestampRejected`, `TestCoerceSenderRejectsTimestampColumn`)
- `rtk go test ./subscription -count=1` → 2 new hash pins (`TestQueryHashTimestampVsInt64`, `TestQueryHashTimestampDiffersByPayload`) plus 3 new entries in `TestQueryHashAllKindsRoundTrip`
- `rtk go test ./schema -count=1` → `valuekind_export_test.go` extended for Timestamp in both `TestValueKindExportStringAll` and `TestAutoIncrementBoundsNonInteger`
- `rtk go test ./protocol -run 'ParityTimestamp' -count=1 -v` → 14 passed (10 subtests + 4 top-level tests)
- `rtk go fmt ./...`, `rtk go vet ./...` → clean
- `rtk go test ./...` → `Go test: 1391 passed in 10 packages`

Clean-tree baseline: `Go test: 1391 passed in 10 packages` (previous 1364 + 27 new tests).

Remaining column-kind widening candidates (pick one, keep scope narrow):
1. **`u256 = 1e40` BigDecimal literal path** — introduce a big-integer-aware literal (`LitBigInt` with `*big.Int`) or a 256-bit-specific code path in `parseNumericLiteral`, widen 256-bit coerce to accept the BigDecimal-integer-valued-overflow case; pin `TestHandle*_ParityValidLiteralU256Scientific`. Bounded scope but introduces a new literal type.
2. **Array column kind (narrow: KindString elements only)** — recursive Value with element kind embedded in the column schema, new BSATN tag, minimal coerce surface; pin `SELECT * FROM t WHERE arr = :sender` rejection (which today fires incidentally via `Coerce` default branch) as a **positive** parity contract with array support. Biggest representation change.

## What landed earlier (2026-04-21, column-kind widening Slice 2 — `i256` / `u256` realized, without BigDecimal literal path)

Second column-kind widening slice. `types.ValueKind` gained `KindInt256` / `KindUint256`; `types.Value` grew a `w256 [4]uint64` slot (index 0 most-significant, signed for Int256; index 3 least-significant); BSATN added tags 15 (Int256) and 16 (Uint256) encoding 32 bytes LE with the least-significant word first; `query/sql/coerce.go` promotes `LitInt` to 256-bit via `NewInt256FromInt64` / `NewUint256FromUint64` — int64 always fits both widths trivially, and `u256 = -1` rejects on the existing negative-LitInt guard; subscription canonical hashing writes word 0 through word 3 big-endian (32 bytes). Autoincrement remains 64-bit-only by design — `schema.AutoIncrementBounds` returns `ok=false` for 256-bit, so the store/commitlog autoincrement paths never see a 256-bit kind. `schema.GoTypeToValueKind` continues to reject 256-bit (Go has no native `int256` / `uint256`); column-kind instantiation is library-API driven.

Unlocked reference rows:
- `check.rs:360-370` `valid_literals_for_type` rows `i256 = 127` and `u256 = 127` now accept end-to-end. Pinned by extending the existing `TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth` / `TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth` bundle with two new subtests each (no structural change, just new rows in the `cases` slice).
- `check.rs:382-385` `invalid_literals` negative-on-unsigned extended to the u256 row via dedicated `TestHandleSubscribeSingle_ParityUint256NegativeRejected` / `TestHandleOneOffQuery_ParityUint256NegativeRejected`.

Still deferred (needs BigDecimal-style literal widening):
- `check.rs:330-332` row `u256 = 1e40` — today's `parseNumericLiteral` tries `strconv.ParseFloat(64)` on `1e40`, the result is finite but overflows int64, so it stays `LitFloat`; the 256-bit coerce path only accepts `LitInt`. Matching the reference `parse_int` BigDecimal path needs a big-integer-aware literal type and a widened 256-bit coerce surface.

Verification:
- `rtk go test ./types -count=1` → 8 new round-trip / Equal / Compare / accessor-panic tests for Int256/Uint256
- `rtk go test ./bsatn -count=1` → 7 new round-trip values + `TestEncodedValueSize256` + `TestEncode256LittleEndian` (byte-order pin)
- `rtk go test ./query/sql -count=1` → 7 new coerce pins (`TestCoerceIntLiteralToInt256`, `TestCoerceNegativeIntLiteralToInt256`, `TestCoerceIntLiteralToUint256`, `TestCoerceNegativeIntoUint256Fails`, `TestCoerceStringLiteralOnInt256Rejected`, `TestCoerceFloatLiteralOnUint256Rejected`, `TestCoerceSenderRejectsInt256Column`)
- `rtk go test ./subscription -count=1` → 2 new hash pins (`TestQueryHashInt256VsUint256`, `TestQueryHashInt256DiffersByPayload`) plus 4 new entries in `TestQueryHashAllKindsRoundTrip`
- `rtk go test ./schema -count=1` → `valuekind_export_test.go` extended for 256-bit in both `TestValueKindExportStringAll` and `TestAutoIncrementBoundsNonInteger`
- `rtk go test ./protocol -run 'ParityValidLiteralOnEachIntegerWidth|ParityUint256NegativeRejected|ParityUint128NegativeRejected' -count=1 -v` → 34 passed
- `rtk go fmt ./...`, `rtk go vet ./...` → clean
- `rtk go test ./...` → `Go test: 1364 passed in 10 packages`

Clean-tree baseline: `Go test: 1364 passed in 10 packages` (previous 1339 + 25 new tests).

Remaining column-kind widening candidate slices (pick one, keep scope narrow):
1. **Timestamp column kind** — reuse `i64` slot for microseconds since unix epoch, add RFC3339 literal grammar (`LitTimestamp` or extend `LitString` parsing to attempt timestamp on KindTimestamp columns); pin `ts = '2025-02-10T15:45:30Z'`.
2. **`u256 = 1e40` BigDecimal literal path** — introduce a big-integer-aware literal (`LitBigInt` with `*big.Int`) or a 256-bit-specific code path in `parseNumericLiteral`, widen 256-bit coerce to accept the BigDecimal-integer-valued-overflow case; pin `TestHandle*_ParityValidLiteralU256Scientific`. Scope bounded but introduces a new literal type.
3. **Array column kind (narrow: KindString elements only)** — recursive Value with element kind embedded in the column schema, new BSATN tag, minimal coerce surface; pin `SELECT * FROM t WHERE arr = :sender` rejection (which today fires incidentally via `Coerce` default branch) as a **positive** parity contract with array support. Biggest representation change.

## What landed earlier (2026-04-21, column-kind widening Slice 1 — `i128` / `u128` realized)

First column-kind widening slice. `types.ValueKind` gained `KindInt128` / `KindUint128`; `types.Value` grew two uint64 storage slots (`hi128`, `lo128`); BSATN added tags 13 (Int128) and 14 (Uint128) encoding 16 bytes LE (lo then hi); `query/sql/coerce.go` promotes `LitInt` to 128-bit via `NewInt128FromInt64` / `NewUint128FromUint64` — int64 always fits both widths so the coerce branches are one-liners, and `u128 = -1` still rejects on the existing negative-LitInt guard; subscription canonical hashing writes hi then lo (16 bytes big-endian). Autoincrement remains 64-bit-only by design — `schema.AutoIncrementBounds` returns `ok=false` for 128-bit, so `store/transaction.go::newAutoIncrementValue` / `store/recovery.go::replayAutoIncrementValueAsUint64` / `commitlog/recovery.go::autoIncrementValueAsUint64` never see a 128-bit kind. `schema.GoTypeToValueKind` continues to reject 128-bit (Go has no native `int128` / `uint128`); column-kind instantiation is library-API driven.

Unlocked reference rows:
- `check.rs:360-370` `valid_literals_for_type` rows `i128 = 127` and `u128 = 127` now accept end-to-end. Pinned by extending the existing `TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth` / `TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth` bundle with two new subtests each (no structural change, just new rows in the `cases` slice).
- `check.rs:382-385` `invalid_literals` negative-on-unsigned extended to the u128 row via dedicated `TestHandleSubscribeSingle_ParityUint128NegativeRejected` / `TestHandleOneOffQuery_ParityUint128NegativeRejected`.

Verification:
- `rtk go test ./types -count=1` → 9 new round-trip / Equal / Compare / accessor-panic tests
- `rtk go test ./bsatn -count=1` → 7 new round-trip values + `TestEncodedValueSize128`
- `rtk go test ./query/sql -count=1` → 6 new coerce pins (`TestCoerceIntLiteralToInt128`, `TestCoerceNegativeIntLiteralToInt128`, `TestCoerceIntLiteralToUint128`, `TestCoerceNegativeIntoUint128Fails`, `TestCoerceStringLiteralOnInt128Rejected`, `TestCoerceFloatLiteralOnUint128Rejected`, `TestCoerceSenderRejectsInt128Column`)
- `rtk go test ./subscription -count=1` → 2 new hash pins (`TestQueryHashInt128VsUint128`, `TestQueryHashInt128DiffersByPayload`) plus 4 new entries in `TestQueryHashAllKindsRoundTrip`
- `rtk go test ./schema -count=1` → `valuekind_export_test.go` extended for 128-bit in both `TestValueKindExportStringAll` and `TestAutoIncrementBoundsNonInteger`
- `rtk go test ./protocol -run 'ParityValidLiteralOnEachIntegerWidth|ParityUint128NegativeRejected' -count=1 -v` → 28 passed (24 subtests + 4 top-level tests)
- `rtk go fmt ./...`, `rtk go vet ./...` → clean
- `rtk go test ./...` → `Go test: 1339 passed in 10 packages`

Clean-tree baseline: `Go test: 1339 passed in 10 packages` (previous 1315 + 24 new tests).

Remaining `check.rs:360-370` / `check.rs:284-332` shapes still deferred:
- `i256` / `u256` column kinds — need 32-byte storage (`[4]uint64` or similar) and BigDecimal-style literal widening to accept `u256 = 1e40` (the ref test case; `1e40` overflows `int64` so today's `parseNumericLiteral` collapses it to `LitFloat`)
- timestamp column kind — needs RFC3339 SQL literal grammar (today's numeric lexer cannot produce `'2025-02-10T15:45:30Z'` as a timestamp literal; string-literal on timestamp column rejects)
- array / product column kinds — recursive `Value` representation, also unblocks `check.rs:523-525` product-value comparison

Next candidate slices (pick one, keep scope narrow):
1. **i256 / u256 without `1e40`** — 32-byte storage (`hi, midhi, midlo, lo uint64`), BSATN tags 15 / 16 (32 bytes LE), coerce from `LitInt`; pin `i256 = 127` / `u256 = 127` (leaving `u256 = 1e40` as a separate follow-up that needs BigDecimal literal support).
2. **Timestamp column kind** — reuse `i64` slot for microseconds since unix epoch, add RFC3339 literal grammar (`LitTimestamp` or extend `LitString` parsing to attempt timestamp on KindTimestamp columns); pin `ts = '2025-02-10T15:45:30Z'`.
3. **Array column kind (narrow: KindString elements only)** — recursive Value with element kind embedded in the column schema, new BSATN tag, minimal coerce surface; pin `SELECT * FROM t WHERE arr = :sender` rejection (which today fires incidentally via `Coerce` default branch) as a **positive** parity contract with array support. Biggest representation change.

## What landed earlier (2026-04-21, `sql.rs:457-476` `parse_sql::invalid` pure-syntax rejection pin bundle)

Reference `parse_sql::invalid` test block at `reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs:457-476` asserts seven shapes reject at the parser boundary:

- `select from t` — Empty SELECT
- `select a from where b = 1` — Empty FROM
- `select a from t where` — Empty WHERE
- `select a, count(*) from t group by` — Empty GROUP BY
- `select count(*) from t` — Aggregate without alias
- `` — Empty string
- ` ` — Whitespace only

Empty-string and whitespace-only shapes were already pinned by the earlier `sub.rs::unsupported` bundle (`ParityEmptyStatementRejected` / `ParityWhitespaceOnlyStatementRejected`), so this slice adds five new pins at each admission surface.

All five new shapes reject incidentally inside `parseProjection` at `query/sql/parser.go:553-572`. Shunter's SELECT-only parser requires the projection to be `*` or `<qualifier>.*`, so:
- `SELECT FROM t` — `parseProjection` reads `FROM` as identifier qualifier, expects `.` next, finds `t` → rejects with "projection must be '*' or 'table.*'"
- `SELECT a FROM WHERE b = 1` — reads `a` as identifier qualifier, expects `.` next, finds `FROM` → same rejection
- `SELECT a FROM t WHERE` — same as above, rejects on `a` before empty WHERE is examined
- `SELECT a, COUNT(*) FROM t GROUP BY` — rejects on leading `a` before the empty GROUP BY is examined
- `SELECT COUNT(*) FROM t` — reads `count` as identifier qualifier, expects `.` next, finds `(` → same rejection

No runtime widening was required — the shapes land at the parser boundary before the reference-style empty-FROM / empty-WHERE / empty-GROUP BY / aggregate-without-alias conditions are ever reached.

New pins landed (10 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParitySqlInvalidEmptySelectRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidEmptyFromRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidEmptyWhereRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidEmptyGroupByRejected`, `TestHandleSubscribeSingle_ParitySqlInvalidAggregateWithoutAliasRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: matching five `TestHandleOneOffQuery_ParitySqlInvalid*` pins in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParitySqlInvalid|TestHandleOneOffQuery_ParitySqlInvalid' -count=1 -v` → `Go test: 10 passed in 1 packages`
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean
- `rtk go test ./...` → `Go test: 1315 passed in 10 packages`

Clean-tree baseline: `Go test: 1315 passed in 10 packages` (previous 1305 + 10 new pin rows).

## What landed earlier (2026-04-21, `sql.rs:411-436` `parse_sql::unsupported` rejection pin bundle)

Reference parse_sql rejection block at `reference/SpacetimeDB/crates/sql-parser/src/parser/sql.rs:411-436` lists ten shapes the reference general-SQL parser rejects before type-checking. Eight of them are SELECT-level (the remaining two are DML `update ... join ... set` / `update t set a = 1 from s where ...` which are already covered by the existing `ParityDMLStatementRejected` pins):

- `select 1` — SELECT with literal projection, no FROM
- `select a from s.t` — multi-part table name
- `select * from t where a = B'1010'` — bit-string literal
- `select a.*, b, c from t` — wildcard projection mixed with bare columns
- `select * from t order by a limit b` — ORDER BY with LIMIT expression
- `select a, count(*) from t group by a` — aggregate with GROUP BY
- `select a.* from t as a, s as b where a.id = b.id and b.c = 1` — implicit comma join
- `select t.* from t join s on int = u32` — unqualified JOIN ON vars

All eight were already rejected incidentally at Shunter's SELECT-only parser boundary:
- `SELECT 1` / `SELECT a ...` / `SELECT a, COUNT(*)` — `parseProjection` rejects non-`*` / non-`table.*` projection at `query/sql/parser.go:553-572`
- `SELECT t.*, b, c FROM t` — after `t.*` parseStatement expects FROM, finds `,`, rejects with "expected FROM, got \",\""
- `SELECT * FROM t ORDER BY u32 LIMIT u32` — ORDER BY trips parseStatement's EOF guard at `parser.go:547-549` with "unexpected token \"ORDER\"" before the LIMIT identifier is examined
- `SELECT * FROM t WHERE u32 = B'1010'` — lexer tokenizes `B` as identifier, `parseLiteral` rejects with "expected literal, got identifier \"B\""
- `SELECT a.* FROM t AS a, s AS b ...` — after `t AS a` parseStatement's EOF/keyword guard hits `,` and rejects with "unexpected token \",\""
- `SELECT t.* FROM t JOIN s ON int = u32` — `parseJoinClause` calls `parseQualifiedColumnRef` for the left side of ON (`parser.go:629`); the bare identifier `int` fails with "expected qualified column reference"
- `SELECT a FROM s.t` — parseProjection rejects bare `a` before FROM parsing reaches `s.t`

No runtime widening was required. The new pins latch the reference parity contract at the protocol admission boundary.

New pins landed (16 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParitySqlUnsupportedSelectLiteralWithoutFromRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedMultiPartTableNameRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedBitStringLiteralRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedWildcardWithBareColumnsRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedOrderByWithLimitExpressionRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedAggregateWithGroupByRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedImplicitCommaJoinRejected`, `TestHandleSubscribeSingle_ParitySqlUnsupportedUnqualifiedJoinOnVarsRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: matching eight `TestHandleOneOffQuery_ParitySqlUnsupported*` pins in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParitySqlUnsupported|TestHandleOneOffQuery_ParitySqlUnsupported' -count=1 -v` → `Go test: 16 passed in 1 packages`
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean (no new issues)
- `rtk go test ./...` → `Go test: 1305 passed in 10 packages`

Clean-tree baseline: `Go test: 1305 passed in 10 packages` (previous 1289 + 16 new pin rows).

Intentional divergence from earlier slice remains recorded: Shunter unifies admission behind one subscription-shape contract; reference `parse_sql` path also accepts DML (`insert`, `delete`, `update`) in its `supported` block, which Shunter rejects under the unified contract. The DML shapes are pinned as rejections by the existing `ParityDMLStatementRejected` pins.

## What landed earlier (2026-04-21, `sub.rs:157-168` `unsupported` rejection pin bundle + intentional one-off-vs-SQL divergence recorded)

Reference subscription-parser `unsupported` test block at `reference/SpacetimeDB/crates/sql-parser/src/parser/sub.rs:157-168` covers five shapes the reference rejects before type-checking:
- `delete from t` — DML not allowed in subscription parse path
- `` (empty string) — empty after skip
- ` ` (whitespace only) — same as empty after tokenizer skip
- `select distinct a from t` — DISTINCT projection
- `select * from (select * from t) join (select * from s) on a = b` — subquery in FROM

All five were already rejected incidentally by Shunter's SELECT-only parser:
- DML / empty / whitespace fail at `query/sql/parser.go:475-477` `expectKeyword("SELECT")` (non-SELECT leading token or EOF-only token stream)
- DISTINCT fails at `parseProjection` (`query/sql/parser.go:553-572`) which only accepts `*` / `table.*`; DISTINCT is consumed as a qualifier candidate, the next token is `a` not `.`, and the parser emits "projection must be '*' or 'table.*'"
- subquery-in-FROM fails at `parseStatement` `tableTok := p.peek(); if !isIdentifierToken(tableTok)` (`query/sql/parser.go:485-488`) — the `(` token is `tokLParen`, not an identifier

No runtime widening was required. The new pins latch the reference parity contract at the protocol admission boundary.

New pins landed (10 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityDMLStatementRejected`, `TestHandleSubscribeSingle_ParityEmptyStatementRejected`, `TestHandleSubscribeSingle_ParityWhitespaceOnlyStatementRejected`, `TestHandleSubscribeSingle_ParityDistinctProjectionRejected`, `TestHandleSubscribeSingle_ParitySubqueryInFromRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityDMLStatementRejected`, `TestHandleOneOffQuery_ParityEmptyStatementRejected`, `TestHandleOneOffQuery_ParityWhitespaceOnlyStatementRejected`, `TestHandleOneOffQuery_ParityDistinctProjectionRejected`, `TestHandleOneOffQuery_ParitySubqueryInFromRejected` in `protocol/handle_oneoff_test.go`

**Intentional divergence recorded (one-off vs reference SQL statement path):** Reference splits `parse_and_type_sub` (subscription, narrow) and `parse_and_type_sql` (one-off SQL, wider). The SQL path at `reference/SpacetimeDB/crates/expr/src/statement.rs:521-551` accepts `select str from t` (bare column projection), `select str, arr from t` (multi-col bare projection), `select t.str, arr from t` (mixed qualified/unqualified), and `select * from t limit 5` (LIMIT). Shunter unifies both behind one `compileSQLQueryString` admission surface that enforces the subscription-shape contract for SubscribeSingle / SubscribeMulti / OneOffQuery, so those four shapes are rejected on all surfaces — pinned as rejections by `TestHandleOneOffQuery_ParityBareColumnProjectionRejected` and `TestHandleOneOffQuery_ParityLimitClauseRejected`. Widening would require LIMIT runtime support, bare / mixed projection plumbing, and reversing the already-landed pins — out of scope for a narrow slice. Divergence recorded in `docs/parity-phase0-ledger.md` (under the `sub.rs::unsupported` paragraph) and `TECH-DEBT.md`. If workload evidence surfaces a real need for ref-style one-off SQL semantics, promote it to its own multi-slice anchor.

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityDMLStatementRejected|TestHandleSubscribeSingle_ParityEmptyStatementRejected|TestHandleSubscribeSingle_ParityWhitespaceOnlyStatementRejected|TestHandleSubscribeSingle_ParityDistinctProjectionRejected|TestHandleSubscribeSingle_ParitySubqueryInFromRejected|TestHandleOneOffQuery_ParityDMLStatementRejected|TestHandleOneOffQuery_ParityEmptyStatementRejected|TestHandleOneOffQuery_ParityWhitespaceOnlyStatementRejected|TestHandleOneOffQuery_ParityDistinctProjectionRejected|TestHandleOneOffQuery_ParitySubqueryInFromRejected' -count=1 -v` → `Go test: 10 passed in 1 packages`
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean
- `rtk go test ./...` → `Go test: 1289 passed in 10 packages`

Clean-tree baseline: `Go test: 1289 passed in 10 packages` (previous 1279 + 10 new pin rows).

## What landed earlier (2026-04-21, `check.rs:360-370` `valid_literals_for_type` column-width breadth pin bundle)

Reference `valid_literals_for_type` at `reference/SpacetimeDB/crates/expr/src/check.rs:360-370` iterates every numeric column kind (`i8, u8, i16, u16, i32, u32, i64, u64, f32, f64, i128, u128, i256, u256`) and asserts `SELECT * FROM t WHERE {ty} = 127` type-checks. Shunter realizes the subset that maps to `schema.ValueKind` — the 10 widths `i8/u8/i16/u16/i32/u32/i64/u64/f32/f64`; `i128`, `u128`, `i256`, `u256` are not realizable (no `schema.ValueKind` variant) and are deliberately skipped.

All 10 realizable widths were already rejected-or-accepted incidentally via `query/sql/coerce.go`:
- `coerceSigned` at `coerce.go:105-113` accepts LitInt within range for `KindInt8/Int16/Int32/Int64`
- `coerceUnsigned` at `coerce.go:115-127` accepts LitInt within range for `KindUint8/Uint16/Uint32/Uint64`
- `KindFloat32` / `KindFloat64` branches at `coerce.go:66-83` promote LitInt via `float32(lit.Int)` / `float64(lit.Int)` (integer-literal-to-float promotion landed with the 2026-04-21 scientific-notation slice)

`= 127` fits every kind's range (i8's max is 127 exactly), so no runtime widening was needed. The new pins latch the reference column-width parity contract at the protocol admission boundary.

New pins landed (2 top-level tests, 20 subtests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth` (10 subtests: i8, u8, i16, u16, i32, u32, i64, u64, f32, f64) in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth` (10 subtests: same 10 widths) in `protocol/handle_oneoff_test.go`

Each subtest builds a single-column table of the given kind, runs `SELECT * FROM t WHERE {colname} = 127`, and asserts admission success. SubscribeSingle pins the executor's ColEq predicate carries a width-native value; OneOff pins Status == 0 and stores a matching row.

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityValidLiteralOnEachIntegerWidth|TestHandleOneOffQuery_ParityValidLiteralOnEachIntegerWidth' -count=1 -v` → `Go test: 22 passed in 1 packages` (2 parents + 20 subtests)
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean
- `rtk go test ./...` → `Go test: 1279 passed in 10 packages`

Clean-tree baseline: `Go test: 1279 passed in 10 packages` (previous 1257 + 22 new pin rows)

## What landed earlier (2026-04-21, `check.rs:382-401` `invalid_literals` rejection pin bundle)

Reference `invalid_literals` block `reference/SpacetimeDB/crates/expr/src/check.rs:373-406` tests five shapes that must reject at the type-check boundary:
- `u8 = -1` (lines 382-385) — negative integer against unsigned column
- `u8 = 1e3` (lines 386-389) — scientific-notation collapses to LitInt(1000), out of range for u8 (max 255)
- `u8 = 0.1` (lines 390-393) — non-integral decimal stays LitFloat, rejected against integer column
- `u32 = 1e-3` (lines 394-397) — `1e-3 = 0.001` non-integral, LitFloat, rejected against unsigned column
- `i32 = 1e-3` (lines 398-401) — same shape, rejected against signed column

All five were already rejected incidentally inside `compileSQLQueryString` / `parseQueryString` via `coerceUnsigned` / `coerceSigned` in `query/sql/coerce.go`:
- negative LitInt → `coerceUnsigned` line 119 rejects
- out-of-range LitInt → `coerceUnsigned` line 123 rejects
- LitFloat against integer column → `coerceUnsigned` line 116 / `coerceSigned` line 106 `mismatch()`

No runtime widening needed; coerce-layer already has broad mechanism tests (`TestCoerceNegativeIntoUnsignedFails`, `TestCoerceIntToSignedRangeCheck`, `TestCoerceRejectsFloatLiteralOnUint32Column`) so no new coerce pins were added (precedent from `check.rs:483-497` bundle). The new pins latch the reference parity contract at the protocol admission boundary.

New pins landed (10 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityInvalidLiteralNegativeIntOnUnsignedRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralScientificOverflowRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralFloatOnUnsignedRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnUnsignedRejected`, `TestHandleSubscribeSingle_ParityInvalidLiteralNegativeExponentOnSignedRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityInvalidLiteralNegativeIntOnUnsignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralScientificOverflowRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralFloatOnUnsignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnUnsignedRejected`, `TestHandleOneOffQuery_ParityInvalidLiteralNegativeExponentOnSignedRejected` in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityInvalidLiteral|TestHandleOneOffQuery_ParityInvalidLiteral' -count=1 -v` → `Go test: 10 passed in 1 packages`
- `rtk go fmt ./protocol`, `rtk go vet ./protocol` → clean
- `rtk go test ./...` → `Go test: 1257 passed in 10 packages`

Clean-tree baseline after this slice: `Go test: 1257 passed in 10 packages` (previous 1247 + 10 new pins; now superseded by the 2026-04-21 `valid_literals_for_type` breadth bundle above — current baseline 1279)

## Prior slice (2026-04-21, scientific-notation + leading-dot float literal parity bundle)

Reference valid-literal bundle `reference/SpacetimeDB/crates/expr/src/check.rs:302-328` is now supported end-to-end on the Shunter SQL surface:
- `u32 = 1e3` / `u32 = 1E3` — scientific notation, integer-valued, binds to unsigned integer column (lines 302-308)
- `f32 = 1e3` — integer-shaped scientific notation on float column (lines 310-312)
- `f32 = 1e-3` — negative exponent, non-integral, binds to float column (lines 314-316)
- `f32 = .1` — leading-dot float, no integer part (lines 322-324)
- `f32 = 1e40` — overflow to `+Inf` on float32 (lines 326-328; `types.NewFloat32` accepts `+Inf`, only `NaN` rejected)

`f32 = 0.1` (lines 318-320) was already supported.

Implementation:
- `query/sql/parser.go`: extracted numeric body parsing into `tokenizeNumeric(s, i, start)` so both the signed/digit-started and leading-`.digit` entry points share the exponent/fractional logic. Added a new `case c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9'` above the existing `tokDot` dispatch so `.1` routes into numeric rather than a dangling dot. Signed-prefix branch now also accepts `+.1` / `-.1`. Exponent tail is `[eE][+-]?[digits]+`; missing digits after `e`/`E`/sign still error as `malformed numeric literal`.
- `query/sql/parser.go`: replaced the old `strings.Contains(t.text, ".")` int/float split in `parseLiteral` with `parseNumericLiteral(text)` — when the body contains `.`, `e`, or `E`, parse via `strconv.ParseFloat(64)`, then collapse to `LitInt` iff the result is finite, `math.Trunc(f) == f`, and within `[math.MinInt64, math.MaxInt64]` (mirroring the reference `BigDecimal::is_integer()` filter in `crates/expr/src/lib.rs::parse_int`). Non-integral or out-of-range stays `LitFloat`.
- `query/sql/coerce.go`: widened `KindFloat32` / `KindFloat64` to accept `LitInt` (promoted via `float32(lit.Int)` / `float64(lit.Int)`), matching reference `parse_float` BigDecimal promotion. `LitFloat` still rejected on integer columns at the existing `coerceSigned` / `coerceUnsigned` seams — `u32 = 1.3` (non-integral) stays pinned as an admission error.
- `query/sql/coerce_test.go`: dropped the stale `TestCoerceUnsupportedKind` (comment said "floats deferred" but floats have worked since the 2026-04-21 float-literal slice). Replaced with `TestCoerceIntegerLiteralPromotesToFloat64`, `TestCoerceIntegerLiteralPromotesToFloat32`, and `TestCoerceFloatLiteralOverflowsToFloat32Infinity`.

Malformed-input guards preserved:
- `TestParseWhereTrailingDotRejected` (`1.`) — trailing dot with no fractional digits still errors.
- `TestParseWhereBareExponentRejected` (`1e`) — exponent letter with no digits still errors.
- `TestParseWhereTrailingIdentifierAfterNumericRejected` (`1efoo`) — numeric followed by identifier still errors (guards against the exponent widening accidentally consuming `1e` then leaving `foo` as a dangling identifier).

New pins landed (18 tests net, clean-tree baseline 1229 → 1247):
- query/sql parser: 8 new tests (5 accept, 3 reject) in `query/sql/parser_test.go`
- query/sql coerce: 3 new tests net (3 added, `TestCoerceUnsupportedKind` dropped as stale)
- protocol subscribe-single: 4 new tests in `protocol/handle_subscribe_test.go`
- protocol one-off: 4 new tests in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./query/sql -count=1` → `Go test: 88 passed in 1 packages`
- `rtk go test ./protocol -count=1` → `Go test: 335 passed in 1 packages`
- `rtk go fmt ./query/sql ./protocol`, `rtk go vet ./query/sql ./protocol` → clean
- `rtk go test ./...` → `Go test: 1247 passed in 10 packages`

Remaining `check.rs:284-332` valid-literal shapes still open (both not realizable against Shunter's column-kind enum, so they are deferred, not next slices):
- 128/256-bit integer column kinds (`i128`, `u128`, `i256`, `u256`) — no such `schema.ValueKind` variant
- timestamp columns — no such `schema.ValueKind` variant

Clean-tree baseline: `Go test: 1247 passed in 10 packages` (previous 1229 + 18 new pins)

## Prior slice (2026-04-21, leading-`+` numeric literal parity micro-slice)

Reference valid-literal shape `reference/SpacetimeDB/crates/expr/src/check.rs:297-300` (`select * from t where u32 = +1` / "Leading `+`") is now supported end-to-end. Probe of `check.rs::valid_literals` (`check.rs:284-332`) showed Shunter already accepted leading `-` but rejected leading `+` at the lexer: `parser.go::tokenize` line 362 matched `c == '-' || (c >= '0' && c <= '9')` only, so `+7` fell through to `tokSymbol` and `parseLiteral` errored.

One-line lexer widening: the numeric-literal case in `tokenize` now matches `c == '-' || c == '+' || (c >= '0' && c <= '9')` and mirrors the leading-sign dispatch symmetrically. `strconv.ParseInt(s, 10, 64)` accepts the `+` prefix natively, so `parseLiteral` and `coerce.go::coerceUnsigned` / `coerceSigned` required no changes.

New pins landed (3 tests):
- query/sql parser: `TestParseWhereLeadingPlusInt` in `query/sql/parser_test.go`
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityLeadingPlusIntLiteral` in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityLeadingPlusIntLiteral` in `protocol/handle_oneoff_test.go`

Verification:
- `rtk go test ./query/sql -run 'TestParseWhereLeadingPlusInt|TestParseWhereNegativeInt' -count=1 -v` → 2 passed
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityLeadingPlusIntLiteral|TestHandleOneOffQuery_ParityLeadingPlusIntLiteral' -count=1 -v` → 2 passed
- `rtk go fmt ./query/sql ./protocol`, `rtk go vet ./query/sql ./protocol` → clean
- `rtk go test ./...` → `Go test: 1229 passed in 10 packages`

Remaining `check.rs:284-332` valid-literal shapes still open (all require real lexer + coerce widening):
- scientific notation: `u32 = 1e3` (→ 1000 as integer), `u32 = 1E3` (case-insensitive), `f32 = 1e3` (integer parses as float), `f32 = 1e-3` (negative exponent), `f32 = 1e40` (overflow → +Inf)
- leading-dot floats: `f32 = .1`
- 128/256-bit integer column kinds (`i128`, `u128`, `i256`, `u256`) and timestamp columns: not realizable against Shunter's `schema.ValueKind` enum — skip.

Clean-tree baseline: `Go test: 1229 passed in 10 packages` (previous 1226 + 3 new pins)

## Previous slice (2026-04-21, parser-surface check.rs negative-shape pin bundle)

Reference type-check rejection shapes at `reference/SpacetimeDB/crates/expr/src/check.rs` lines 506-509 (`select * from t as r where t.u32 = 5` / base-table qualifier out of scope after alias), 510-513 (`select u32 from t` / bare column projection), 515-517 (`select * from t join s` / join without qualified projection), 519-521 (`select t.* from t join t` / self-join without aliases), 526-528 (`select t.* from t join s on t.u32 = r.u32 join s as r` / forward alias reference), 530-533 (`select * from t limit 5` / LIMIT clause), and 534-537 (`select t.* from t join s on t.u32 = s.u32 where bytes = 0xABCD` / unqualified WHERE column inside join) are now explicitly pinned at both the SubscribeSingle and OneOffQuery admission surfaces (14 new tests). All seven shapes were already rejected incidentally at the SQL parser boundary (`parseProjection`, `parseStatement` EOF-check, `parseStatement` joined-projection guard, `parseJoinClause` self-join guard, `parseQualifiedColumnRef` / `parseComparison` via `resolveQualifier`, and `parseComparison` requireQualify under a join binding) — no runtime widening was required. The pins promote the rejections from incidental parser-level errors to named reference-parity contracts latched on the protocol admission boundary.

Grounded anchors walked before edits:
- `check.rs:506-509`: `SELECT * FROM t AS r WHERE t.u32 = 5` — `parser.go::parseComparison` calls `resolveQualifier("t", {R: t})` which returns `!ok` → `qualified column "t" does not match relation`.
- `check.rs:510-513`: `SELECT u32 FROM t` — `parser.go::parseProjection` rejects any projection other than `*` / `table.*` at lines 517-528.
- `check.rs:515-517`: `SELECT * FROM t JOIN s` — `parser.go::parseStatement` line 468-469 rejects a join query whose projection qualifier is empty: `join queries require a qualified projection`.
- `check.rs:519-521`: `SELECT t.* FROM t JOIN t` — `parser.go::parseJoinClause` line 577-578 detects `leftTable==rightTable && leftAlias==rightAlias` → `self join requires aliases`.
- `check.rs:526-528`: `SELECT t.* FROM t JOIN s ON t.u32 = r.u32 JOIN s AS r` — `parser.go::parseQualifiedColumnRef` rejects the `r.u32` qualifier at line 629-631; the forward-alias reference fails before the multi-way-join guard at lines 482-489 fires.
- `check.rs:530-533`: `SELECT * FROM t LIMIT 5` — `parser.go::parseStatement` reaches the EOF check at line 505-506 with `LIMIT` still in the token stream and rejects with `unexpected token "LIMIT"`. The already-existing trailing-keyword fast-path in `parseWhere` (lines 641-645) only fires when a WHERE clause precedes the keyword; the standalone `LIMIT` case was already rejected by the EOF guard.
- `check.rs:534-537`: `SELECT t.* FROM t JOIN s ON t.u32 = s.u32 WHERE bytes = 0xABCD` — `parser.go::parseComparison` at lines 761-762 enforces `bindings.requireQualify` under a join binding: `join WHERE columns must be qualified`.

Pinned but deliberately skipped:
- `check.rs:523-525` (`SELECT t.* FROM t JOIN s ON t.arr = s.arr` / product-value comparison): not realizable against the Shunter column-kind enum. `schema.ValueKind` (re-exported from `types`) enumerates only `KindBool`, `KindInt{8,16,32,64}`, `KindUint{8,16,32,64}`, `KindFloat{32,64}`, `KindString`, `KindBytes` — there is no array / product kind — so the shape reference rejects cannot arise at the Shunter admission boundary. Skipping is intentional; if Shunter ever adds a composite column kind, this shape becomes a fresh landing candidate for either a runtime widening (accept + reject in join-ON compile) or a named parser rejection.

New pins landed (14 tests):
- protocol subscribe-single: `TestHandleSubscribeSingle_ParityBaseTableQualifierAfterAliasRejected`, `TestHandleSubscribeSingle_ParityBareColumnProjectionRejected`, `TestHandleSubscribeSingle_ParityJoinWithoutQualifiedProjectionRejected`, `TestHandleSubscribeSingle_ParitySelfJoinWithoutAliasesRejected`, `TestHandleSubscribeSingle_ParityForwardAliasReferenceRejected`, `TestHandleSubscribeSingle_ParityLimitClauseRejected`, `TestHandleSubscribeSingle_ParityUnqualifiedWhereInJoinRejected` in `protocol/handle_subscribe_test.go`
- protocol one-off: `TestHandleOneOffQuery_ParityBaseTableQualifierAfterAliasRejected`, `TestHandleOneOffQuery_ParityBareColumnProjectionRejected`, `TestHandleOneOffQuery_ParityJoinWithoutQualifiedProjectionRejected`, `TestHandleOneOffQuery_ParitySelfJoinWithoutAliasesRejected`, `TestHandleOneOffQuery_ParityForwardAliasReferenceRejected`, `TestHandleOneOffQuery_ParityLimitClauseRejected`, `TestHandleOneOffQuery_ParityUnqualifiedWhereInJoinRejected` in `protocol/handle_oneoff_test.go`

Scope kept narrow: no parser changes, no runtime widening, no new test files. `SubscribeMulti` inherits the compile path through shared parser+`compileSQLQueryString`; no dedicated pin added (covered by the same parser mechanism as SubscribeSingle). Existing parity pins from earlier in the calendar week are unchanged — the new 506-537 pins sit alongside the `check.rs:498-504` type-mismatch pins and the `check.rs:483-497` unknown-table / unknown-column pins as named reference-parity contracts.

Docs follow-through: `docs/current-status.md`, `docs/parity-phase0-ledger.md`, and `TECH-DEBT.md` now record the fourteen new pins as landed and call out `check.rs:523-525` as not realizable against the Shunter column-kind enum.

Verification run after landing the slice:
- `rtk go test ./protocol -run '<all 14 new test names>' -count=1 -v` → `Go test: 14 passed in 1 packages`
- `rtk go fmt ./protocol`
- `rtk go vet ./protocol` → `No issues found`
- `rtk go test ./...` → `Go test: 1226 passed in 10 packages`

Current clean-tree baseline:
- `Go test: 1226 passed in 10 packages` (previous 1212 + 14 new pins)

## Previous slice (2026-04-21, unknown-table / unknown-column parity pin bundle)

Reference type-check rejection shapes `check.rs:483-485` (`select * from r` / unknown FROM table), `check.rs:491-493` (`select * from t where t.a = 1` / qualified unknown WHERE column), and `check.rs:495-497` (`select * from t as r where r.a = 1` / alias-qualified unknown WHERE column) are now explicitly pinned at the SubscribeSingle and OneOffQuery admission surfaces. Previously the rejection was incidental — `SchemaLookup.TableByName` returned `!ok` in `compileSQLQueryString` (`protocol/handle_subscribe.go:152-154`) and `rel.ts.Column` returned `!ok` in `normalizeSQLFilterForRelations` (`protocol/handle_subscribe.go:250-253`), but nothing named the reference parity contract.

- Grounded anchors before edits:
  - `reference/SpacetimeDB/crates/expr/src/check.rs:483-485` for the unknown FROM table rejection (`"Table r does not exist"`).
  - `reference/SpacetimeDB/crates/expr/src/check.rs:491-493` for the qualified unknown WHERE column rejection (`"Field a does not exist on table t"`).
  - `reference/SpacetimeDB/crates/expr/src/check.rs:495-497` for the alias-qualified unknown WHERE column rejection (same message; alias resolves back to base table in Shunter's parser `relationBindings`).
- No production code widening was required. `compileSQLQueryString` shared between SubscribeSingle / SubscribeMulti / OneOffQuery already rejects all three shapes incidentally; walking the path confirmed:
  - `SELECT * FROM r` fails at `sl.TableByName(stmt.ProjectedTable)` (`handle_subscribe.go:152-154`)
  - `SELECT * FROM t WHERE t.a = 1` fails at `rel.ts.Column(f.Column)` inside `normalizeSQLFilterForRelations` (`handle_subscribe.go:250-253`)
  - `SELECT * FROM t AS r WHERE r.a = 1` — parser's `resolveQualifier` maps alias `r` back to base table `t`, then the filter lookup fails the same way
- New pins landed (6 tests):
  - protocol subscribe-single: `TestHandleSubscribeSingle_ParityUnknownTableRejected`, `TestHandleSubscribeSingle_ParityUnknownColumnRejected`, `TestHandleSubscribeSingle_ParityAliasedUnknownColumnRejected` in `protocol/handle_subscribe_test.go`
  - protocol one-off: `TestHandleOneOffQuery_ParityUnknownTableRejected`, `TestHandleOneOffQuery_ParityUnknownColumnRejected`, `TestHandleOneOffQuery_ParityAliasedUnknownColumnRejected` in `protocol/handle_oneoff_test.go`
- Scope kept narrow: no parser changes, no runtime widening, no new test file. `SubscribeMulti` inherits the compile path through shared `compileSQLQueryString`; no dedicated pin added (covered by same mechanism as SubscribeSingle). Existing `TestHandleSubscribeSingle_UnknownTable` / `TestHandleOneOffQuery_UnknownTable` / `TestHandleOneOffQuery_UnknownColumn` tests left unchanged — the new pins are named reference-parity contracts alongside them rather than replacements.
- Docs follow-through: `docs/current-status.md`, `docs/parity-phase0-ledger.md`, and `TECH-DEBT.md` now record the three new pins as landed; the pinned tests are named in the ledger.

Verification run after landing the slice:
- `rtk go test ./protocol -run 'TestHandleSubscribeSingle_ParityUnknownTableRejected|TestHandleSubscribeSingle_ParityUnknownColumnRejected|TestHandleSubscribeSingle_ParityAliasedUnknownColumnRejected|TestHandleOneOffQuery_ParityUnknownTableRejected|TestHandleOneOffQuery_ParityUnknownColumnRejected|TestHandleOneOffQuery_ParityAliasedUnknownColumnRejected' -count=1 -v`
- `rtk go fmt ./protocol`
- `rtk go vet ./protocol`
- `rtk go test ./...`

Current clean-tree baseline:
- `Go test: 1212 passed in 10 packages` (previous 1206 + 6 new pins — this was the baseline before the 506-537 bundle landed; see the top-of-file 1226 figure for the current clean-tree truth)

Flaky test note: no known clean-tree intermittent tests remain after the 2026-04-21 subscription, scheduler, protocol lifecycle, message-family, and SQL/query-surface follow-through.

## Current startup notes

- Authoritative next step is the top-of-file `## Next session is Phase 4 Slice 2β` block.
- Treat the historical sections above as provenance only.
- Do not use any superseded guidance below this point; it has been intentionally trimmed to reduce token waste.
- For broader project state, read the current source-of-truth docs already listed in `AGENTS.md` / `docs/EXECUTION-ORDER.md` rather than relying on stale copied summaries here.
