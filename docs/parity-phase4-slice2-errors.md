# Phase 4 Slice 2β — typed `Traversal` / `Open` error enums

Records the decision for the second format-level commitlog parity
sub-slice. Called out in `docs/spacetimedb-parity-roadmap.md` Phase 4
Slice 2, `docs/parity-phase0-ledger.md` row 2β, and
`NEXT_SESSION_HANDOFF.md`. Written before code so follow-up agents
have a locked spec.

Written clean-room. Reference paths below are cited for behavioral
grounding only; do not copy or transliterate Rust source.

## Reference shape (target)

`reference/SpacetimeDB/crates/commitlog/src/error.rs`:

- `Traversal` enum — iterator-time errors raised by public commitlog
  iterators.
  - `OutOfOrder { expected_offset, actual_offset, prev_error }` —
    an observed commit's `min tx offset` does not match the
    successor of the previous commit.
  - `Forked { offset }` — a commit with the same `min_tx_offset` as
    a previous commit but a different CRC32; "same offset, different
    bytes" suggests a write that failed-to-fsync actually landed.
  - `Decode { offset, source: DecodeError }` — SATS-level decode
    failure inside a record body.
  - `Checksum { offset, source: ChecksumMismatch }` — payload CRC
    did not match the header's stored CRC.
  - `Io(io::Error)` — transparent passthrough of the underlying
    `io::Error`.
- `Append<T>` struct — returned from `Commitlog::append`; owns the
  payload back to the caller after a flush failure. Fields: `txdata:
  T`, `source: io::Error`.
- `ChecksumMismatch` struct — usually wrapped inside another error
  (`Traversal::Checksum` or `io::Error`).
- `SegmentMetadata` enum — returned from per-segment metadata
  extraction during reopen.
  - `InvalidCommit { sofar: Metadata, source: io::Error }` — an
    invalid commit encountered while extracting metadata; `sofar`
    carries the partial progress.
  - `Io(io::Error)` — transparent passthrough.
- `source_chain(e)` helper — recursively concatenates `e.source()`
  chain into `": cause: cause: …"`.

Summary of reference behavioral contract:

- errors are categorized by **where they can surface in the API**,
  not by what caused them low-level. `Traversal` covers everything
  an iterator can yield; `SegmentMetadata` covers everything
  metadata extraction can yield; `Append<T>` is the single write
  failure surface.
- `ChecksumMismatch` is a reusable leaf that shows up inside
  multiple categories.
- no "Open" enum exists in the reference; callers that open a log /
  segment receive raw `io::Error` because file-opening has no
  commitlog-specific structure. Shunter needs an Open category
  because `OpenSegment` / `ScanSegments` / recovery perform both
  file I/O **and** commitlog-specific header validation, and the
  latter currently surfaces as the bare sentinel family
  (`ErrBadMagic`, `BadVersionError`, `ErrBadFlags`,
  `ErrTruncatedRecord` at segment header, `HistoryGapError`,
  `ErrMissingBaseSnapshot`, `ErrNoData`, `ErrUnknownFsyncMode`).
- no "Snapshot" or "Index" enums in the reference (reference
  commitlog crate has neither); Shunter carries both concepts
  inside `commitlog/` and they already behave as discrete categories.

## Shunter shape today

`commitlog/errors.go` exports a flat list that mixes sentinel vars
and typed struct errors:

Sentinels:
- `ErrBadMagic`, `ErrBadFlags`, `ErrTruncatedRecord`,
  `ErrUnknownFsyncMode`, `ErrDurabilityFailed`,
  `ErrSnapshotIncomplete`, `ErrSnapshotInProgress`,
  `ErrMissingBaseSnapshot`, `ErrNoData`,
  `ErrOffsetIndexKeyNotFound`, `ErrOffsetIndexFull`,
  `ErrOffsetIndexCorrupt`.

Typed structs (back-compat aliases `Err<Name>` preserved):
- `BadVersionError`, `UnknownRecordTypeError`,
  `ChecksumMismatchError`, `RecordTooLargeError`,
  `RowTooLargeError`, `SnapshotHashMismatchError`,
  `HistoryGapError`, `SchemaMismatchError` (has `Unwrap`),
  `OffsetIndexNonMonotonicError`.

Where each currently surfaces (admission seams):

| Seam | File | Errors returned |
|---|---|---|
| Segment header decode | `segment.go:56-70` | `ErrBadMagic`, `*BadVersionError`, `ErrBadFlags` |
| Record decode (iterator) | `segment.go:102-155` | `ErrTruncatedRecord`, `*RecordTooLargeError`, `*ChecksumMismatchError`, `*UnknownRecordTypeError`, `ErrBadFlags` |
| Segment tail scan | `segment_scan.go:63-170` | `ErrTruncatedRecord`, `*ChecksumMismatchError`, `*UnknownRecordTypeError`, `ErrBadFlags`, `*HistoryGapError` |
| Segment enumeration | `segment_scan.go:205-240` | `*HistoryGapError` (cross-segment monotonicity break) |
| Changeset decode | `changeset_codec.go:150-160` | `*RowTooLargeError` |
| Recovery planning | `recovery.go:50-70` | `ErrNoData`, `ErrMissingBaseSnapshot` |
| Snapshot header | `snapshot_io.go:430-460` | `ErrBadMagic`, `*BadVersionError` |
| Snapshot write | `snapshot_io.go:200-220` | `ErrSnapshotInProgress`, `ErrSnapshotIncomplete` |
| Snapshot hash verify | `snapshot_io.go:470-480` | `*SnapshotHashMismatchError` |
| Snapshot ↔ registry | `snapshot_select.go:40-135` | `ErrMissingBaseSnapshot`, `*SchemaMismatchError` |
| Durability ctor | `durability.go:90-100` | `ErrUnknownFsyncMode` (wrapped) |
| Durability worker fatal | `durability.go:210-240` | `panic(fmt.Errorf("%w: %w", ErrDurabilityFailed, fatal))` |
| Offset index read/write | `offset_index.go` | `ErrOffsetIndexKeyNotFound`, `ErrOffsetIndexFull`, `ErrOffsetIndexCorrupt`, `*OffsetIndexNonMonotonicError` |

No externally observable correctness gap. The gap is ergonomic:
callers that want category-level handling (e.g. "any traversal
error → linear rescan", "any open error → fresh-next-segment
fallback") must enumerate the long list of leaf errors by hand.
After this slice, `errors.Is(err, ErrTraversal)` /
`errors.Is(err, ErrOpen)` etc. will return true for any member of
the category, while existing leaf `errors.Is` / `errors.As` checks
continue to work unchanged.

## Decision: what to build in slice 2β

Introduce **category sentinel errors** in `commitlog/errors.go` that
group the existing errors by admission seam. Wire every call site so
the returned error matches its category via `errors.Is`, while the
leaf sentinel / typed struct identity is preserved.

No literal translation of the Rust enum; this is a Go-idiomatic
categorial grouping. The goal is: any caller can write

```go
if errors.Is(err, commitlog.ErrTraversal) { /* fall back to linear scan */ }
var bv *commitlog.BadVersionError
if errors.As(err, &bv) { /* surface bv.Got */ }
```

and both checks succeed simultaneously.

### Artifacts

- edits in `commitlog/errors.go` — add category sentinels, add `Is`
  methods on the existing typed structs, add a small helper
  `wrapCategory(cat error, leaf error) error` for sentinel-family
  errors.
- edits at every call site listed in the "Admission seams" table
  above so the returned error carries the category. For typed
  structs the change is the `Is` method (no per-call-site churn).
  For bare sentinel returns the change is a `wrapCategory` wrap:

  ```go
  // before
  return ErrBadMagic
  // after
  return wrapCategory(ErrOpen, ErrBadMagic)
  ```

  Back-compat: `errors.Is(err, ErrBadMagic)` continues to return
  true because the leaf sentinel is preserved in the Unwrap chain.
- new file `commitlog/errors_category_test.go` — pins below.
- no changes to `offset_index.go`, `segment.go`, `snapshot_io.go`,
  etc. beyond the call-site wraps.
- no new dependencies.

### Category taxonomy

Five categories. The Shunter set is larger than the reference's
because Shunter carries snapshot and offset-index concerns inside
the same `commitlog/` package.

```go
var (
    // ErrTraversal categorizes every error that can surface from an
    // in-progress iterator (SegmentReader.Next, ReplayLog per-commit
    // decode, changeset decode mid-record). Analogous to reference
    // Traversal enum.
    ErrTraversal = errors.New("commitlog: traversal error")

    // ErrOpen categorizes every error surfaced while opening a segment
    // or enumerating segments, including commitlog-specific header
    // validation and recovery planning. Reference has no direct
    // analog because reference opens surface raw io::Error; Shunter
    // has commitlog-specific structure at open time so the category
    // exists here.
    ErrOpen = errors.New("commitlog: open error")

    // ErrDurability categorizes fatal durability-worker failures.
    // Currently surfaces only via panic value, but the category
    // sentinel lets callers test panic-recovered values uniformly.
    ErrDurability = errors.New("commitlog: durability error")

    // ErrSnapshot categorizes every error surfaced during snapshot
    // read/write/selection. Shunter-specific category; reference
    // commitlog does not own snapshots.
    ErrSnapshot = errors.New("commitlog: snapshot error")

    // ErrIndex categorizes every error from the per-segment offset
    // index (read, write, reopen). Shunter-specific category;
    // reference offset index was added in Shunter's Slice 2α.
    ErrIndex = errors.New("commitlog: offset index error")
)
```

**Category assignment** (every existing leaf gets exactly one
category):

| Leaf | Category | Site |
|---|---|---|
| `ErrBadMagic` | `ErrOpen` | segment header, snapshot header |
| `BadVersionError` | `ErrOpen` | segment header, snapshot header |
| `ErrBadFlags` (at header) | `ErrOpen` | segment header |
| `ErrBadFlags` (mid-record) | `ErrTraversal` | record decode |
| `ErrTruncatedRecord` (at header) | `ErrOpen` | segment scan header |
| `ErrTruncatedRecord` (mid-record) | `ErrTraversal` | record decode, changeset codec |
| `RecordTooLargeError` | `ErrTraversal` | record decode |
| `ChecksumMismatchError` | `ErrTraversal` | record decode, segment scan |
| `UnknownRecordTypeError` | `ErrTraversal` | record decode, segment scan |
| `HistoryGapError` | `ErrOpen` | segment scan, segment enumeration |
| `RowTooLargeError` | `ErrTraversal` | changeset codec |
| `ErrNoData` | `ErrOpen` | recovery |
| `ErrMissingBaseSnapshot` | `ErrOpen` | recovery, snapshot select |
| `ErrUnknownFsyncMode` | `ErrOpen` | durability worker ctor |
| `ErrDurabilityFailed` | `ErrDurability` | durability worker panic |
| `ErrSnapshotInProgress` | `ErrSnapshot` | snapshot write |
| `ErrSnapshotIncomplete` | `ErrSnapshot` | snapshot open |
| `SnapshotHashMismatchError` | `ErrSnapshot` | snapshot verify |
| `SchemaMismatchError` | `ErrSnapshot` | snapshot select |
| `ErrOffsetIndexKeyNotFound` | `ErrIndex` | offset index read |
| `ErrOffsetIndexFull` | `ErrIndex` | offset index write |
| `ErrOffsetIndexCorrupt` | `ErrIndex` | offset index open |
| `OffsetIndexNonMonotonicError` | `ErrIndex` | offset index write |

The `ErrBadFlags` and `ErrTruncatedRecord` rows split by call site.
Both sentinels are used at segment-open-time (header) and
mid-stream (record body). The split is resolved by the wrap: the
caller at each site wraps with the appropriate category. The leaf
sentinel identity is unchanged, so any existing `errors.Is(err,
ErrTruncatedRecord)` continues to work regardless of site.

### Wrap mechanism

For typed struct errors, add `Is` method:

```go
func (e *ChecksumMismatchError) Is(target error) bool {
    return target == ErrTraversal
}
```

For bare sentinels, a single helper:

```go
// wrapCategory wraps leaf in an error whose Unwrap chain contains
// both cat and leaf. The returned error:
//   - errors.Is(e, cat)  → true
//   - errors.Is(e, leaf) → true
//   - Error() returns leaf.Error() unchanged so existing log /
//     test text matches remain stable
func wrapCategory(cat, leaf error) error {
    if cat == nil || leaf == nil {
        return leaf
    }
    return &categorizedError{cat: cat, leaf: leaf}
}

type categorizedError struct {
    cat  error
    leaf error
}

func (e *categorizedError) Error() string   { return e.leaf.Error() }
func (e *categorizedError) Unwrap() []error { return []error{e.leaf, e.cat} }
```

Go 1.20+ `errors.Is` walks multi-Unwrap. The category appears in
the chain, as does the leaf. The surface text is the leaf's text so
existing substring-based tests and logs do not churn.

**Why not multi-%w at each call site?** Two reasons.
1. `fmt.Errorf("%w: %w", cat, leaf)` mutates the surface text
   ("commitlog: open error: commitlog: bad magic bytes"), which
   breaks any existing text-equality assertion.
2. A dedicated wrapper keeps the intent visible and lets us add
   instance-level metadata later (e.g. file path, byte offset)
   without a second refactor.

**Why not `Is` on the sentinels themselves?** Bare sentinels are
`*errors.errorString` instances, not declared types — we cannot
attach methods. Wrapping at the call site is the only option that
preserves pointer identity.

**Why not convert every bare sentinel to a typed struct?** That
would break `err == ErrBadMagic` identity checks that already exist
in tests (`phase4_acceptance_test.go:64` uses the sentinel as a
table value, `errors.Is` via identity). Keeping the sentinel and
wrapping categorially preserves both identity and `errors.Is`.

### Seam-by-seam wire plan

Each bullet names the file, the existing error flow, and the
replacement.

1. **`commitlog/segment.go` — `readHeader`** (lines 56-70).
   - `return ErrBadMagic` → `return wrapCategory(ErrOpen, ErrBadMagic)`
   - `return &BadVersionError{…}` unchanged; `BadVersionError.Is(ErrOpen)` added.
   - `return ErrBadFlags` → `return wrapCategory(ErrOpen, ErrBadFlags)`
2. **`commitlog/segment.go` — `DecodeRecord`** (lines 102-155).
   - `return nil, ErrTruncatedRecord` (×3) → `return nil, wrapCategory(ErrTraversal, ErrTruncatedRecord)` (×3).
   - `return nil, &RecordTooLargeError{…}` unchanged; `RecordTooLargeError.Is(ErrTraversal)` added.
   - `return nil, &ChecksumMismatchError{…}` unchanged; `ChecksumMismatchError.Is(ErrTraversal)` added.
   - `return nil, &UnknownRecordTypeError{…}` unchanged; `UnknownRecordTypeError.Is(ErrTraversal)` added.
   - `return nil, ErrBadFlags` (mid-record) → `return nil, wrapCategory(ErrTraversal, ErrBadFlags)`.
3. **`commitlog/segment_scan.go`** (lines 63-240).
   - all `return nil, ErrTruncatedRecord` (×5) → `wrapCategory(ErrOpen, ErrTruncatedRecord)` (header-time) or `wrapCategory(ErrTraversal, ErrTruncatedRecord)` (mid-record). Walk each site individually.
   - `return nil, ErrBadFlags` → `wrapCategory(ErrTraversal, ErrBadFlags)` (mid-record per site comment).
   - `return nil, &ChecksumMismatchError{…}` / `&UnknownRecordTypeError{…}` unchanged.
   - `return …, &HistoryGapError{…}` (×3) unchanged; `HistoryGapError.Is(ErrOpen)` added.
4. **`commitlog/changeset_codec.go`** (line 153).
   - `return nil, 0, &RowTooLargeError{…}` unchanged; `RowTooLargeError.Is(ErrTraversal)` added.
5. **`commitlog/recovery.go`** (lines 50-70).
   - `return nil, 0, RecoveryResumePlan{}, ErrNoData` → `wrapCategory(ErrOpen, ErrNoData)`.
   - `return nil, 0, RecoveryResumePlan{}, ErrMissingBaseSnapshot` → `wrapCategory(ErrOpen, ErrMissingBaseSnapshot)`.
6. **`commitlog/snapshot_io.go`** (lines 200-480).
   - `return ErrSnapshotInProgress` → `wrapCategory(ErrSnapshot, ErrSnapshotInProgress)`.
   - `return 0, 0, [32]byte{}, ErrBadMagic` → `wrapCategory(ErrOpen, ErrBadMagic)` (snapshot file is being opened).
   - `return 0, 0, [32]byte{}, &BadVersionError{…}` — relies on `BadVersionError.Is(ErrOpen)`.
   - `return &SnapshotHashMismatchError{…}` — `SnapshotHashMismatchError.Is(ErrSnapshot)` added.
   - any `return ErrSnapshotIncomplete` site — `wrapCategory(ErrSnapshot, ErrSnapshotIncomplete)`.
7. **`commitlog/snapshot_select.go`** (lines 40-135).
   - `return nil, ErrMissingBaseSnapshot` → `wrapCategory(ErrOpen, ErrMissingBaseSnapshot)`.
   - `return &SchemaMismatchError{…}` (all sites) — `SchemaMismatchError.Is(ErrSnapshot)` added; existing `Unwrap` method preserved for cause chain.
8. **`commitlog/durability.go`** (lines 90-240).
   - `return fmt.Errorf("%w: %d", ErrUnknownFsyncMode, mode)` → `return fmt.Errorf("%w: %w: %d", ErrOpen, ErrUnknownFsyncMode, mode)` (multi-%w here is fine because the mode integer already mutates the surface text).
   - `panic(fmt.Errorf("%w: %w", ErrDurabilityFailed, fatal))` → `panic(fmt.Errorf("%w: %w: %w", ErrDurability, ErrDurabilityFailed, fatal))`. Category present in the panic-recovered error's chain.
9. **`commitlog/offset_index.go`** (no call-site churn).
   - `ErrOffsetIndexKeyNotFound` / `ErrOffsetIndexFull` / `ErrOffsetIndexCorrupt` — all sites already return the bare sentinel. Two paths:
     - (preferred) wrap at the call sites that *emit* them, so advisory callers in `segment.go`/`replay.go` already see the category. Wrap count is ~6 sites.
     - (alternative) leave the offset-index leaf sentinels bare; category only applies to non-index errors. Reject this alternative — it makes `ErrIndex` dead from a caller's perspective.
   - Chosen: wrap at emission sites in `offset_index.go`. `OffsetIndexNonMonotonicError.Is(ErrIndex)` added for the typed case.

### What this slice does **not** change

- no new leaf errors, no renames. Every existing sentinel + typed
  struct keeps its name, text, and pointer identity.
- no change to `errors.Is` semantics of existing tests. Every
  existing test that uses `errors.Is(err, ErrBadMagic)` etc.
  continues to pass unchanged.
- no reference `Forked` / `OutOfOrder` split. `HistoryGapError`
  already plays the `OutOfOrder` role; `Forked` (same offset,
  different CRC) is not currently detected and needs record-layer
  changes to detect — deferred to its own decision doc.
- no reference `Append<T>` payload-return surface. Shunter's
  durability worker owns the payload and panics on fatal failure
  rather than returning it to the caller; introducing
  payload-return would be a bigger API change.
- no change to panic-vs-return policy anywhere. The durability
  worker still panics on fatal; the category sentinel just appears
  in the panic value's Unwrap chain.
- no `source_chain` helper. `errors.Unwrap` and `%w` already give
  callers the chain they need.
- no snapshot / index re-categorization later. This slice finalizes
  all five categories.
- no public re-export of category sentinels from other packages
  (e.g. `store`, `recovery`); a consumer that wants category checks
  imports `commitlog` directly.

## Pin plan

All pins land in new file `commitlog/errors_category_test.go` in
session 2.

### Typed-struct category pins (each struct + its category via `Is`/`As`)

1. `TestChecksumMismatchErrorCategory` — `&ChecksumMismatchError{…}`
   → `errors.Is(_, ErrTraversal)` is true, `errors.As` into
   `*ChecksumMismatchError` succeeds, fields match.
2. `TestBadVersionErrorCategory` — `&BadVersionError{Got: 2}` →
   `errors.Is(_, ErrOpen)` is true, `errors.As` into
   `*BadVersionError` succeeds.
3. `TestUnknownRecordTypeErrorCategory` — similar with
   `ErrTraversal`.
4. `TestRecordTooLargeErrorCategory` — similar with `ErrTraversal`.
5. `TestRowTooLargeErrorCategory` — similar with `ErrTraversal`.
6. `TestHistoryGapErrorCategory` — similar with `ErrOpen`.
7. `TestSchemaMismatchErrorCategory` — similar with `ErrSnapshot`;
   additionally asserts existing `Unwrap` chain to `Cause` still
   works.
8. `TestSnapshotHashMismatchErrorCategory` — similar with
   `ErrSnapshot`.
9. `TestOffsetIndexNonMonotonicErrorCategory` — similar with
   `ErrIndex`.

### Sentinel category pins (wrap helper in `commitlog/errors.go`)

10. `TestWrapCategorySentinelBadMagic` — `wrapCategory(ErrOpen,
    ErrBadMagic)` satisfies both `errors.Is(_, ErrOpen)` and
    `errors.Is(_, ErrBadMagic)`; `Error()` returns the leaf text
    unchanged.
11. `TestWrapCategorySentinelTruncatedRecord` — same shape, with
    `ErrTraversal` on one site and `ErrOpen` on another (the
    category depends on the site, not the leaf).
12. `TestWrapCategoryNilGuards` — `wrapCategory(nil, leaf)` returns
    `leaf` unwrapped; `wrapCategory(cat, nil)` returns `nil`.

### End-to-end admission-seam pins (full path, not constructed errors)

13. `TestSegmentHeaderBadMagicReturnsOpenCategory` — open a segment
    file with wrong magic bytes, expect the returned error matches
    `errors.Is(_, ErrOpen)` and `errors.Is(_, ErrBadMagic)`.
14. `TestDecodeRecordChecksumMismatchReturnsTraversalCategory` —
    craft a record with a bad CRC, read via `DecodeRecord`; returned
    error matches `errors.Is(_, ErrTraversal)` and `errors.As` into
    `*ChecksumMismatchError` with correct fields.
15. `TestScanSegmentsHistoryGapReturnsOpenCategory` — two segments
    with non-contiguous tx ids; `ScanSegments` returns an error that
    matches both `errors.Is(_, ErrOpen)` and `errors.As` into
    `*HistoryGapError`.
16. `TestReplayLogCorruptRecordReturnsTraversalCategory` — replay a
    log whose mid-record CRC is flipped; returned error matches
    `errors.Is(_, ErrTraversal)`.
17. `TestRecoveryNoDataReturnsOpenCategory` — empty directory with
    no snapshot or log data; `NewRecoveryPlan` (or whatever the
    public entry is) returns an error matching `errors.Is(_,
    ErrOpen)` and `errors.Is(_, ErrNoData)`.
18. `TestRecoveryMissingBaseSnapshotReturnsOpenCategory` — log data
    present but no snapshot; error matches `errors.Is(_, ErrOpen)`
    and `errors.Is(_, ErrMissingBaseSnapshot)`.
19. `TestSnapshotHashMismatchReturnsSnapshotCategory` — snapshot
    file whose stored hash does not match the data hash; error
    matches `errors.Is(_, ErrSnapshot)` and `errors.As` into
    `*SnapshotHashMismatchError`.
20. `TestSnapshotSelectSchemaMismatchReturnsSnapshotCategory` —
    snapshot whose schema disagrees with the registry; error
    matches `errors.Is(_, ErrSnapshot)` and `errors.As` into
    `*SchemaMismatchError`.
21. `TestDurabilityWorkerFatalPanicsWithDurabilityCategory` —
    simulate a fatal worker failure; the recovered panic value's
    Unwrap chain contains both `ErrDurability` and
    `ErrDurabilityFailed`.
22. `TestDurabilityCtorUnknownFsyncModeReturnsOpenCategory` — build
    a durability worker with an unknown fsync mode; returned error
    matches `errors.Is(_, ErrOpen)` and `errors.Is(_,
    ErrUnknownFsyncMode)`.
23. `TestOffsetIndexKeyNotFoundReturnsIndexCategory` — fresh index,
    `KeyLookup(anyKey)` returns error matching `errors.Is(_,
    ErrIndex)` and `errors.Is(_, ErrOffsetIndexKeyNotFound)`.
24. `TestOffsetIndexFullReturnsIndexCategory` — fill to cap and
    append again; returned error matches `errors.Is(_, ErrIndex)`
    and `errors.Is(_, ErrOffsetIndexFull)`.
25. `TestOffsetIndexNonMonotonicReturnsIndexCategory` —
    non-monotonic append; returned error matches `errors.Is(_,
    ErrIndex)` and `errors.As` into `*OffsetIndexNonMonotonicError`.

### Back-compat pins

26. `TestBackCompatSentinelIdentityPreserved` — existing identity
    checks (`err == ErrBadMagic` via `errors.Is`) continue to work
    after the wrap. Table-driven across every sentinel in the
    table above.
27. `TestBackCompatTypedStructErrorAsStillWorks` — existing
    `errors.As` checks (e.g. `var b *BadVersionError; errors.As(err,
    &b)`) still succeed after the category layering. Table-driven.
28. `TestBackCompatErrorMessageUnchanged` — surface `Error()` string
    of a categorized error equals the leaf's surface text. Pins
    that we do not accidentally break existing text-equality
    assertions elsewhere in the suite. Not exhaustive across the
    whole suite — one representative case per sentinel family.

## Session breakdown

This slice is smaller than 2α. Plan: two sessions.

- **Session 1 (this doc).** Decision doc only. No code. Lock the
  spec. Update ledger + handoff.
- **Session 2.** Implement:
  - category sentinels and `wrapCategory` helper in `errors.go`;
  - `Is` methods on every typed struct listed in the taxonomy;
  - call-site wraps at every seam in the "Seam-by-seam wire plan"
    section;
  - all pins 1-28 in `commitlog/errors_category_test.go`.

  Land when:
  - `rtk go test ./commitlog -run ErrorCategory` green;
  - `rtk go test ./commitlog -count=1` green with no regression;
  - `rtk go test ./...` meets or exceeds the clean-tree baseline
    (1444 + 28 net new pins = 1472 target).

If the implementation reveals an under-specified detail (e.g. a
sentinel surfaces at a site not listed in the taxonomy), stop and
update this decision doc first, land the doc edit, then resume.

## Acceptance gate for the whole slice

Close 2β only when all of:

- every pin in the plan above is landed and passing;
- no externally observable regression — the 1444 clean-tree
  baseline rises by the number of net-new pins without touching
  any existing pin;
- every leaf error in the taxonomy has a passing category pin
  AND a passing back-compat pin;
- `NEXT_SESSION_HANDOFF.md` "What just landed" summarizes the
  taxonomy (five categories, leaf-preserving wrap mechanism, seam
  coverage);
- `docs/parity-phase0-ledger.md` 2β row flipped to `closed`;
- `TECH-DEBT.md` OI-007 paragraph updated to name 2β closed and
  point at 2γ as the next open sub-slice (or to name 2γ as
  deferred if no evidence pushes it to active);
- `docs/parity-phase4-slice2-errors.md` retained as the locked
  spec for audit.

## Out-of-scope follow-ons

- Phase 4 Slice 2γ — record / log on-disk shape parity with the
  reference wire (header fields, version negotiation, trailer).
  Biggest remaining commitlog parity theme.
- Reference `Traversal::Forked` detection — requires tracking CRC
  per commit across reopen boundaries. Deferred to its own decision
  doc.
- Reference `Append<T>` payload-return surface — requires public
  commitlog API to grow an append-with-payload-return method.
  Deferred; Shunter's durability worker owns the payload and the
  ergonomic win is not obvious today.
- `source_chain` helper — not needed; `errors.Unwrap` and `%w`
  satisfy existing log formatting.
- Cross-package category exports (e.g. `store` re-exporting
  `commitlog.ErrTraversal`) — consumers import `commitlog` directly.

## Clean-room reminder

Reference citations above are grounding only. Implementation must
be re-derived in Go from the locked contract, not translated from
the Rust source. Category sentinels, wrap helper, and `Is` methods
follow existing `commitlog/` package conventions. Do not rename
existing sentinels or typed structs. Do not change any existing
surface `Error()` text.
