# Phase 4 Slice 2γ — record / log on-disk shape parity audit

Records the decision for the third format-level commitlog parity
sub-slice. Called out in `docs/spacetimedb-parity-roadmap.md` Phase 4
Slice 2, `docs/parity-phase0-ledger.md` row 2γ, and
`NEXT_SESSION_HANDOFF.md`. Written before code so follow-up agents
have a locked spec.

Written clean-room. Reference paths below are cited for behavioral
grounding only; do not copy or transliterate Rust source.

## Reference shape (target)

`reference/SpacetimeDB/crates/commitlog/src/segment.rs` and
`reference/SpacetimeDB/crates/commitlog/src/commit.rs` define the
reference on-disk wire format.

### Segment header (reference)

10 bytes total. Written once at the start of every segment file.

```
offset  size  field                 value / semantics
  0      6    magic                 literal bytes ['(', 'd', 's', ')', '^', '2']
  6      1    log_format_version    current default 1 (V1); V0 supported on decode
  7      1    checksum_algorithm    current default 0 (CRC32C); lookup-table indexed
  8      1    reserved              zero
  9      1    reserved              zero
```

`Header::decode` validates the magic prefix and populates
`log_format_version` / `checksum_algorithm` from bytes 6–7. It does
not reject non-zero reserved bytes (tolerant reader).
`Header::ensure_compatible` rejects `log_format_version >
max_log_format_version` or `checksum_algorithm != expected`.

### Commit wire (reference, V1)

Variable length. A single commit groups N transactions.

```
offset  size    field           endian  semantics
  0       8     min_tx_offset   LE      first tx offset in this commit (64-bit)
  8       8     epoch           LE      leader term; default 0 for single-node
 16       2     n               LE      number of records in the commit (u16)
 18       4     len             LE      byte length of records buffer (u32)
 22     len     records         -       opaque N-record byte buffer
 22+len   4     crc32c          LE      Castagnoli CRC32C of all preceding bytes
```

Framing = 22 (header) + 4 (CRC) = 26 bytes per commit, plus the
variable `records` buffer.

Key behaviors pinned by reference:
- **All-zero header sentinel** — a commit header consisting entirely
  of zero bytes (length `Header::LEN` = 22) is treated as end of
  segment. Enables file preallocation without false corruption.
- **CRC32C covers header + records** — the checksum is accumulated
  over the full pre-CRC payload using Castagnoli CRC32C via
  `Crc32cReader`/`Crc32cWriter`; the trailing CRC is excluded.
- **Opaque records buffer** — commitlog does not interpret the
  `records` byte slice. A `Decoder` trait (in
  `crates/commitlog/src/payload.rs`) is responsible for splitting
  the buffer into `n` transactions and decoding each one.
- **Checksum-mismatch surface** — a CRC mismatch returns an
  `io::Error` of kind `InvalidData` carrying a `ChecksumMismatch`
  payload downcastable from the inner error.
- **Epoch monotonicity** — `Writer::set_epoch` is advisory; the
  writer must ensure the new epoch is greater than the current and
  that any pending commit has been flushed. Epoch going backwards
  mid-segment is not rejected by the commit decoder but can surface
  via `Metadata::max_epoch`.
- **V0 compatibility** — V0 commits lack the 8-byte `epoch` field
  (header LEN = 14). `StoredCommit::decode_internal` dispatches on
  the segment header's `log_format_version`.
- **Checksum-algorithm negotiation** — the segment header byte 7
  selects the CRC algorithm; today only 0 (CRC32C) is populated in
  the `CHECKSUM_LEN` lookup table, so readers can reject unknown
  algorithms cleanly without reshuffling bytes.

## Shunter shape today

`commitlog/segment.go` and `commitlog/changeset_codec.go` define the
current wire.

### Segment header (Shunter)

8 bytes total. Written once at the start of every segment file.

```
offset  size  field         value / semantics
  0      4    magic         literal bytes ['S', 'H', 'N', 'T']
  4      1    version       current 1; rejected if != 1
  5      1    flags         reserved; must be zero, else ErrBadFlags
  6      1    padding       zero
  7      1    padding       zero
```

`ReadSegmentHeader` validates magic, rejects `version != 1` with
`*BadVersionError{Got}`, and rejects any non-zero tail byte 5-7 with
`ErrBadFlags`. Strict reader; non-zero reserved bytes fail admission
instead of being tolerated.

### Record wire (Shunter)

Variable length. One physical record per transaction (1:1 tx↔record).

```
offset  size    field         endian  semantics
  0       8     tx_id         LE      64-bit transaction id
  8       1     record_type   -       1 = changeset; any other value rejects with *UnknownRecordTypeError
  9       1     flags         -       reserved; must be zero
 10       4     data_len      LE      byte length of payload (u32)
 14     data_len payload      -       Shunter-specific Changeset encoding (see below)
 14+len   4     crc32c        LE      Castagnoli CRC32C of header[0:14] + payload
```

Framing = 14 (header) + 4 (CRC) = 18 bytes per record, plus the
variable `payload`. `RecordOverhead` constant in `segment.go`
exposes the 18-byte figure.

### Payload = Changeset (Shunter-specific, `commitlog/changeset_codec.go`)

```
offset  size    field                    semantics
  0       1     changeset_version        current 1 (changesetVersion const)
  1       4     table_count              u32 LE
for each table (table_count iterations):
  -       4     table_id                 u32 LE
  -       4     insert_count             u32 LE
  for each insert:
    -     4     row_len                  u32 LE
    -     row_len  row bytes             BSATN-encoded ProductValue
  -       4     delete_count             u32 LE
  for each delete:
    -     4     row_len                  u32 LE
    -     row_len  row bytes             BSATN-encoded ProductValue
```

Tables are emitted in ascending `TableID` order for deterministic
output. `RowTooLargeError` is raised from `decodeRow` when a row
exceeds `MaxRowBytes`.

### Key Shunter behaviors

- **Castagnoli CRC32C** via `hash/crc32` with `MakeTable(Castagnoli)`;
  matches reference algorithm. Scope is header[0:14] + payload; the
  trailing CRC bytes are excluded. Unit pins `ComputeRecordCRC`.
- **1:1 tx:record** — `DurabilityWorker.processBatch` (`durability.go:347-405`)
  calls `seg.Append(rec)` once per queued Changeset; no commit grouping.
  Batch size is a queue-drain parameter (`DrainBatchSize`), not a
  wire-level concept.
- **Strict header rejection** — the 8-byte segment header rejects
  non-zero reserved bytes. Reader-side all-zero record-header tails
  are now treated as end-of-stream for recovery/preallocation
  tolerance; Shunter still does not emit writer-side preallocation.
- **No epoch field** — neither the segment header nor the record
  header carries an `epoch`. Single-node-only deployments; no leader
  term tracking.
- **Typed record-type discriminator** — byte 8 of every record
  header carries a `RecordType` byte. Only `RecordTypeChangeset = 1`
  is accepted today. Reserves header space for future record kinds
  (eg snapshot metadata, epoch-change marker) without a wire break.
- **Monotonic TxID append guard** — `SegmentWriter.Append` refuses a
  record whose TxID is not strictly greater than the last record
  (first record must equal `startTx`). No explicit "commit" grouping
  means every record is its own atomic framing unit.
- **CRC over entire header + payload** — matches reference scope
  semantics (all pre-CRC bytes), though per-record rather than
  per-commit.

## Delta taxonomy

Every field-level / semantic divergence between reference and Shunter.

| # | Topic | Reference | Shunter | Category |
|---|---|---|---|---|
| 1 | Segment magic bytes | `(ds)^2` (6 bytes) | `SHNT` (4 bytes) | structural |
| 2 | Segment header length | 10 bytes | 8 bytes | structural |
| 3 | Segment header byte 7 | `checksum_algorithm` (u8) | `flags` (u8, must be zero) | semantic |
| 4 | Segment reserved bytes | tolerated non-zero | rejected non-zero | behavioral |
| 5 | Zero-header EOS sentinel | yes (all-zero header → EOS) | yes, using Shunter's 14-byte record header as the EOS marker | **match semantically** |
| 6 | Framing unit | Commit (groups N transactions) | Record (1 tx per physical record) | structural |
| 7 | Commit `min_tx_offset` | present (u64 LE) | absent; per-record `TxID` u64 LE stored instead | structural |
| 8 | Commit `epoch` field | present (u64 LE) | absent | structural |
| 9 | Commit `n` field | present (u16 LE) | absent; always implicitly 1 per record | structural |
| 10 | Commit `len` field | present (u32 LE) | present as per-record `data_len` (u32 LE) | semantic (role differs: len-of-batch vs len-of-single-record) |
| 11 | Record-type discriminator byte | absent (records are opaque to commitlog) | present (byte 8; 1 = changeset) | structural |
| 12 | Record-flags byte | absent | present (byte 9; must be zero) | structural |
| 13 | Header size per framing unit | 22 bytes (V1) / 14 bytes (V0) | 14 bytes | structural |
| 14 | CRC algorithm | Castagnoli CRC32C | Castagnoli CRC32C | **match** |
| 15 | CRC scope | commit header + records | record header + payload | semantic (scope differs because framing unit differs; coverage is equivalent "all pre-CRC bytes") |
| 16 | CRC width | u32 LE (4 bytes) | u32 LE (4 bytes) | **match** |
| 17 | Integer endianness | LE throughout | LE throughout | **match** |
| 18 | V0/V1 version split | supported; decoder dispatches on segment header version | single version (1); different bytes = rejection | structural |
| 19 | Records buffer format | opaque; `Decoder` trait decides shape | Shunter-canonical `Changeset` format (single-versioned, BSATN rows inside) | **semantic (scope explosion)** |
| 20 | Row-size limit | payload crate concern (not commit layer) | enforced at `decodeRow` with `RowTooLargeError` | semantic |
| 21 | `set_epoch` API | writer-level, requires external leader election | absent | missing feature |
| 22 | Segment metadata extraction | `Metadata::extract` walks commits for `max_epoch`, `max_commit` etc. | `ScanSegments` walks per-record, returns `SegmentInfo` with last TxID | structural |
| 23 | All-zero header tolerance (preallocation) | yes | yes for reader/recovery tolerance; writer-side preallocation is still not emitted | partial match |
| 24 | Offset index sidecar | `.idx` per segment, 16-byte entries (u64 key + u64 byte offset) | `.idx` per segment, 16-byte entries (u64 key + u64 byte offset) | **match** (closed in Slice 2α) |
| 25 | History-gap detection | reference uses `Traversal::OutOfOrder` on iterator; `Metadata::extract` rejects mid-segment gap | Shunter uses `*HistoryGapError` at both inter-segment and intra-segment boundaries | **match semantically** (Slice 2β categorized as `ErrOpen`) |
| 26 | Fork detection (same offset, different CRC) | `Traversal::Forked` | absent; not detected | missing feature (deferred to its own decision doc per Slice 2β) |

**Summary**: 12 "match" / "match semantically" entries plus one
partial match for reader-side all-zero preallocation tolerance; 7
structural differences (framing unit, magic length, version byte
positions, header length, epoch, record-type byte, records-buffer
shape); 3 remaining behavioral differences (reserved-byte strictness,
CRC scope per-commit-vs-per-record, set_epoch API); 2 semantic renames
(byte 7 meaning; len field role); 2 explicit missing features (epoch,
forked-offset detection); 1 scope-explosion entry (records-buffer
format couples to types / bsatn / schema).

## Decision: what 2γ becomes

**2γ closes as a documented-divergence slice, not a byte-parity
rewrite.** Full reference wire byte-compatibility is rejected for
this phase. Rationale:

1. **Scope explosion** — delta entry #19 (records-buffer format)
   couples commitlog parity to a Shunter-specific Changeset format
   that embeds BSATN-encoded `ProductValue`s. Byte-parity would
   require also matching the reference `Txdata` / reducer-call
   flags / BFLATN-vs-BSATN row encoding, which spans types, bsatn,
   schema, subscription, executor. That is a multi-phase rewrite,
   not a commitlog slice.
2. **No operational-replacement trigger yet** — byte-parity's only
   user-visible benefit is being able to read reference-created
   logs. No workload today requires this. Until a concrete
   operational-replacement trigger surfaces, the ROI is negative.
3. **Migration cost** — any change to Shunter's current 8-byte
   segment header or 14-byte record header invalidates every
   existing on-disk segment. A migration story is required
   (re-sync, in-place upgrade, or tolerate-both-formats reader).
   Not worth paying now.
4. **Structural divergence is intentional** — delta entries #6, #8,
   #11, #12 reflect deliberate Shunter design choices (1:1
   tx:record, no epoch, typed record-type discriminator). Reversing
   them would add complexity without a downstream consumer.

### What 2γ *does* produce

1. **This decision doc** as the locked audit-and-divergence record.
   Every reference/Shunter delta is named, categorized, and has a
   rationale for its resolution.
2. **A wire-shape pin suite** (`commitlog/wire_shape_test.go`) that
   latches the current Shunter wire as a **canonical contract**.
   Currently, the wire is defined by a collection of byte-offset
   constants and inline binary.LittleEndian calls scattered across
   `segment.go` and `changeset_codec.go`; no single suite asserts
   the full layout byte-for-byte. The new pins make accidental wire
   drift (e.g. silently renumbering a field or changing endianness
   in a refactor) loudly visible.
3. **A ledger update** flipping 2γ from `open (next)` to `closed
   (divergences recorded)` with a one-paragraph summary of what is
   explicitly deferred.
4. **A tech-debt update** (`TECH-DEBT.md` OI-007) naming this slice
   closed and recording the follow-on parity themes as deferred.

### What 2γ does *not* produce

- No wire-format change. `WriteSegmentHeader`, `EncodeRecord`, and
  `EncodeChangeset` bytes are unchanged.
- No on-disk migration. Every existing segment reads identically
  before and after.
- No runtime API change. `SegmentReader` / `SegmentWriter` /
  `ReplayLog` / `DurabilityWorker` surfaces are unchanged.
- No new leaf errors, no new sentinels, no new typed structs. The
  Slice 2β error taxonomy is complete for the current wire.
- No reference `epoch` field. No reference commit grouping. No
  reference V0/V1 split. No byte-compatible magic. No forked-offset
  detection. Reader-side zero-header EOS tolerance was added later;
  Shunter still does not emit writer-side preallocation.

These deferrals are explicit and named in the "Out-of-scope
follow-ons" section at the bottom.

## Pin plan

All pins land in new file `commitlog/wire_shape_test.go` in session
2.

### Segment-header layout pins

1. `TestSegmentHeaderLayoutBytes` — write a segment header to a
   `bytes.Buffer`, assert the 8 bytes are exactly
   `['S','H','N','T', 0x01, 0x00, 0x00, 0x00]`. Pins magic, version
   byte, flags byte, and two padding bytes simultaneously.
2. `TestSegmentHeaderSizeConstant` — `SegmentHeaderSize == 8`.
3. `TestSegmentHeaderMagicConstant` — `SegmentMagic == [4]byte{'S','H','N','T'}`.
4. `TestSegmentHeaderVersionConstant` — `SegmentVersion == 1`.
5. `TestSegmentHeaderRejectsNonMagicPrefix` — write a header where
   byte 0 is `'T'` instead of `'S'`, `ReadSegmentHeader` returns an
   error matching `errors.Is(_, ErrBadMagic)` and
   `errors.Is(_, ErrOpen)` (Slice 2β category).
6. `TestSegmentHeaderRejectsVersionMismatch` — byte 4 = 2;
   `errors.As` into `*BadVersionError` with `Got == 2`.
7. `TestSegmentHeaderRejectsNonZeroFlags` — byte 5 = 1; returns
   error matching `errors.Is(_, ErrBadFlags)` and
   `errors.Is(_, ErrTraversal)`. (Decision-doc session-2 update:
   the 2β category for `ErrBadFlags` landed as a single-category
   leaf — `Is(ErrTraversal) → true` — rather than the call-site
   split originally proposed in the 2β decision doc. Pins 7, 8, 30
   reflect the realized 2β shape, not the proposed split.)
8. `TestSegmentHeaderRejectsNonZeroPadding` — byte 6 = 1 or byte 7
   = 1; each returns `ErrBadFlags` (covered by the same strict
   guard), with the same `ErrTraversal` category as pin 7.

### Record-header layout pins

9. `TestRecordHeaderLayoutBytes` — encode a record with `TxID =
   0x0102030405060708`, `RecordType = 1`, `Flags = 0`, payload =
   `[]byte{0xAA}`; assert bytes 0-13 are exactly `[0x08, 0x07,
   0x06, 0x05, 0x04, 0x03, 0x02, 0x01, 0x01, 0x00, 0x01, 0x00,
   0x00, 0x00]` (little-endian TxID, type byte, flags byte,
   little-endian data_len).
10. `TestRecordHeaderSizeConstant` — `RecordHeaderSize == 14`.
11. `TestRecordCRCSizeConstant` — `RecordCRCSize == 4`.
12. `TestRecordOverheadConstant` — `RecordOverhead == 18`.
13. `TestRecordTypeChangesetConstant` — `RecordTypeChangeset == 1`.
14. `TestEncodeRecordLittleEndianTxID` — TxID encoding matches
    `binary.LittleEndian.PutUint64`.
15. `TestEncodeRecordLittleEndianDataLen` — data_len encoding
    matches `binary.LittleEndian.PutUint32` (at bytes 10-13).
16. `TestEncodeRecordLittleEndianCRC` — CRC tail matches
    `binary.LittleEndian.PutUint32`.

### CRC algorithm pins

17. `TestRecordCRCIsCastagnoli` — compute expected CRC32C via
    `crc32.New(crc32.MakeTable(crc32.Castagnoli))` over
    header[0:14] + payload; assert equal to `ComputeRecordCRC`.
18. `TestRecordCRCScopeCoversHeaderAndPayload` — mutate one byte
    in the header and one byte in the payload between two
    otherwise identical records; assert the CRCs differ in both
    cases. Guards against accidental scope narrowing.
19. `TestRecordCRCExcludesTrailingCRC` — assert the CRC value
    stored at bytes `14+data_len .. 18+data_len` is *not* part of
    its own computation (regression guard for circular-checksum
    bugs).
20. `TestDecodeRecordRejectsCRCFlip` — flip one bit in the
    encoded CRC region; `DecodeRecord` returns error matching
    `errors.As` into `*ChecksumMismatchError` with correct
    `Expected` / `Got` fields, and `errors.Is(_, ErrTraversal)`
    (Slice 2β category).

### Changeset payload layout pins

21. `TestChangesetVersionConstant` — `changesetVersion == 1`.
22. `TestChangesetEmptyLayoutBytes` — encode an empty changeset
    (no tables); assert bytes are exactly
    `[0x01, 0x00, 0x00, 0x00, 0x00]` (version + table_count=0).
23. `TestChangesetSingleTableLayoutBytes` — encode a changeset
    with one table, one insert, one byte payload; assert exact
    byte layout: version (1) + table_count (4 LE) + table_id (4
    LE) + insert_count (4 LE) + row_len (4 LE) + row bytes +
    delete_count (4 LE). Uses a fixed, trivial BSATN-encoded row
    to keep the test hermetic.
24. `TestChangesetTableOrderDeterministic` — encode a changeset
    with two tables where the map iteration order would otherwise
    be unstable; assert table_id ordering ascends on every encode
    (guards the `slices.Sort(tableIDs)` contract).
25. `TestChangesetDecodeRejectsUnknownVersion` — first byte = 2;
    decode returns error containing the phrase "unsupported
    changeset version". (No category pin — changeset-layer
    decode errors do not flow through the 2β taxonomy today.)
26. `TestChangesetDecodeRejectsRowTooLarge` — row_len >
    MaxRowBytes; decode returns `*RowTooLargeError`, which via
    Slice 2β is `errors.Is(_, ErrTraversal)`.

### Divergence-from-reference pins (behavioral contract)

27. `TestShunterHasNoEpochField` — assert `RecordHeaderSize == 14`
    (matches no-epoch design). Documentation pin: comment in the
    test references delta entry #8.
28. `TestShunterHasNoCommitGrouping` — write two changesets via
    `DurabilityWorker.EnqueueCommitted`; after sync, assert the
    segment contains exactly two physical records, not one grouped
    commit. Asserts the 1:1 tx↔record invariant (delta entry #6).
29. `TestShunterRecordTypeByteIsDiscriminator` — construct a
    record with `RecordType = 99` and write it manually to a
    segment; `DecodeRecord` returns `*UnknownRecordTypeError` with
    `Type == 99`. Documents the typed-discriminator feature
    (delta entry #11).
30. `TestShunterRejectsNonZeroFlagsMidRecord` — construct a record
    with `Flags = 1`; `DecodeRecord` returns error matching
    `errors.Is(_, ErrBadFlags)` and `errors.Is(_, ErrTraversal)`.
    Documents the strict-flags choice (delta entry #12).
31. `TestWireShapeShunterZeroRecordHeaderActsAsEOS` — write a
    segment header followed by an all-zero record-header region
    (simulated preallocated tail); `scanOneSegment` accepts it as
    end-of-stream and leaves the segment appendable in place. This
    documents the post-2γ reader-side zero-header EOS follow-through
    (delta entries #5, #23).

### Constants / structural pins

32. `TestWireConstantsMatchBytes` — table-driven pin that
    `SegmentHeaderSize`, `RecordHeaderSize`, `RecordCRCSize`, and
    `RecordOverhead` match the byte counts demonstrated in pins
    1 / 9. Regression guard: if any constant drifts, this and one
    of the layout-bytes pins fail simultaneously, naming the
    inconsistency loudly.

### Integration pin (end-to-end)

33. `TestSegmentRoundTripByteIdenticalAfterEncodeDecode` — encode
    a deterministic changeset, write it as a record, reopen the
    segment, decode; re-encode the decoded changeset; assert the
    two byte sequences are bit-identical. Pins that encode/decode
    is a bijection for the supported shapes — guards any future
    refactor against adding a lossy normalization.

Count: 33 pins, all landing in one new test file.

## Session breakdown

This slice is smaller than 2α. Plan: **two sessions**.

- **Session 1 (this doc).** Decision doc only. No code. Lock the
  divergence audit. Update ledger + tech-debt + handoff.
- **Session 2.** Implement the pin suite:
  - new file `commitlog/wire_shape_test.go` with pins 1–33;
  - no changes to `segment.go`, `changeset_codec.go`,
    `durability.go`, `replay.go`, `recovery.go`, or
    `snapshot_io.go`;
  - no new dependencies.

  Land when:
  - `rtk go test ./commitlog -run WireShape -count=1 -v` green;
  - `rtk go test ./commitlog -count=1` green, baseline rises by
    the number of new pins (33 or more if subtests are used) from
    the current 185;
  - `rtk go test ./...` meets or exceeds the clean-tree baseline
    (1511 + ≥33 net new).

If the implementation reveals an under-specified detail (e.g. a
constant the taxonomy didn't name, or a byte offset that differs
from what this doc claims), stop and update this decision doc
first, land the doc edit, then resume.

## Acceptance gate for the whole slice

Close 2γ only when all of:

- every pin in the plan above is landed and passing;
- no externally observable regression — the 1511 clean-tree
  baseline rises by the number of net-new pins without touching
  any existing pin;
- `NEXT_SESSION_HANDOFF.md` "What just landed" summarizes the
  divergence audit outcome (what is pinned, what is explicitly
  deferred);
- `docs/parity-phase0-ledger.md` 2γ row flipped from `open (next)`
  to `closed (divergences recorded)`;
- `TECH-DEBT.md` OI-007 paragraph updated to name 2γ closed and
  carry forward the named deferrals as tracked tech debt;
- `docs/parity-phase4-slice2-record-shape.md` retained as the
  locked spec for audit.

## Out-of-scope follow-ons (explicitly deferred by this slice)

Each of the following is a named reference-parity gap that is *not*
closed by 2γ. Any future slice that wants to close one of these
must open its own decision doc.

- **Reference byte-compatible segment magic** (`(ds)^2` vs `SHNT`).
  Requires either a version-byte protocol extension that allows
  readers to dispatch on both magics, or an on-disk migration. Real
  motivation only arises if Shunter needs to read reference-created
  logs.
- **Reference commit grouping** (N transactions per physical commit
  unit). Requires reshaping `DurabilityWorker.processBatch` to emit
  one framing unit per batch, adding `n` / `len` / batch-CRC fields
  to the header, and teaching `ReplayLog` / `scanOneSegment` /
  `DecodeRecord` to loop over the records buffer. Reader and
  writer both change. Nontrivial; do not start without a named
  consumer.
- **Reference `epoch` field**. Requires leader-election machinery
  (not present in Shunter) and 8 extra bytes in the header. Defer
  until distributed deployments are on the roadmap.
- **Reference V0/V1 version split**. Shunter is at V1 permanently;
  adding V0 support is pointless without a reference-created log to
  decode.
- **Reference all-zero-header EOS sentinel**. Closed for Shunter's
  reader/recovery path: an all-zero 14-byte record header is now
  treated as end-of-stream and pinned by
  `commitlog/wire_shape_test.go::TestWireShapeShunterZeroRecordHeaderActsAsEOS`
  plus `commitlog/replay_test.go::TestReplayLogPreallocatedZeroTailStopsAtLastRecord`.
- **Checksum-algorithm negotiation**. Today byte 5 of the segment
  header is `flags` (must be zero). Renaming it to
  `checksum_algorithm` (value 0 = CRC32C; reject non-zero) is a
  one-line comment change that aligns the Shunter vocabulary with
  the reference's. Currently both reject non-zero with the same
  surface error, so the rename is purely documentary. Deferred to
  its own micro-slice if the vocabulary alignment becomes load-
  bearing.
- **Forked-offset detection (`Traversal::Forked`)**. Requires
  tracking CRC per TxID across reopens. Already deferred by Slice
  2β; reconfirmed here.
- **Full records-buffer format parity**. Would require matching the
  reference `Txdata` / `ReducerCallFlags` / BFLATN row encoding
  across types / bsatn / schema / subscription / executor. Beyond
  this slice's scope by an order of magnitude.
- **Reference `Append<T>` payload-return API**. Already deferred by
  Slice 2β; reconfirmed here.
- **Writer `set_epoch` API**. Dead code without leader election.
  Defer with the epoch-field item.
- **Preallocation-friendly writes** (`fallocate` + zero-filled
  tail). Reader-side tolerance is in place; emitting preallocated
  segment files remains deferred until a workload needs it.

## Clean-room reminder

Reference citations above are grounding only. Implementation must
be re-derived in Go from the locked contract, not translated from
the Rust source. The wire-shape pins follow the existing
`commitlog/` package testing conventions (`bytes.Buffer`
round-trips, `errors.Is` / `errors.As` for error pins, hermetic
fixtures). Do not copy the reference Rust layout struct names,
field orders, or magic values; Shunter's wire is intentionally
distinct and this slice's job is to pin the current distinct shape
as canonical, not to import the reference shape.
