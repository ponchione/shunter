# P0-RECOVERY-001 — replay horizon / validated-prefix behavior

Records the `P0-RECOVERY-001` parity decision called out in
`docs/parity-phase0-ledger.md` and
`docs/spacetimedb-parity-roadmap.md` Phase 4 Slice 2. Written
companion to the parity pins that lock the chosen shape.

## Reference shape (target)

`reference/SpacetimeDB/crates/commitlog/`:

- `commitlog::Generic::commits_from(offset)` at `src/commitlog.rs:209`
  constructs a `Commits<R>` iterator rooted at `offset`. The iterator
  keeps a `CommitInfo::Initial { next_offset }` state.
- `CommitInfo::adjust_initial_offset` at `src/commitlog.rs:834-845` is
  the per-commit skip rule: while in `Initial { next_offset }`, if the
  commit's `last_tx_offset < next_offset` skip it (`return true`);
  otherwise adjust `next_offset` to the commit boundary and yield. The
  skip granularity is per-commit, not per-segment.
- `stream::reader::commits` at `src/stream/reader.rs:41-62` opens each
  segment file in `Segments::offs` order, emits commits one at a time,
  and terminates on end-of-range (`stream/reader.rs:129-130`) or
  `io::Error` at the inner loop (`stream/reader.rs:116-145`).
- `repo::resume_segment_writer` at `src/repo/mod.rs:258-299` is the
  restart/reopen path. `Metadata::extract` errors go through a match at
  `src/repo/mod.rs:265-275`. On `SegmentMetadata::InvalidCommit { sofar, .. }`
  the sealed prefix is returned rather than the error, so a corrupt
  tail is treated as "open the last good prefix and start a fresh
  segment after it" (also `src/commitlog.rs:488-498` — `InvalidCommit`
  with an empty `tx_range` is the "first commit of last segment
  corrupt" edge, which bubbles as a real error).
- Edge: `first_commit_in_last_segment_corrupt`
  (`src/tests/partial.rs:142-166`) — last segment's first commit is
  zeroed; reopen via `Generic::open` returns
  `io::ErrorKind::InvalidData`. Documented as "we don't automatically
  recover from that" (`tests/partial.rs:134-140`).
- Replay / apply errors carry segment context via `with_segment_context`
  at `src/repo/mod.rs:362-367` and the `error::Traversal` /
  `error::Open` enums (`src/error.rs`).

Summary of reference replay-horizon policy:
- continue across valid segments by opening each in `Segments::offs`
  order;
- skip below the resume horizon at **per-commit** granularity via
  `adjust_initial_offset`;
- stop at the validated prefix on tail damage by treating
  `InvalidCommit` as a sealed-prefix signal;
- attach segment and offset context to replay / open errors.

## Shunter shape today

`commitlog/replay.go` + `commitlog/segment_scan.go` +
`commitlog/recovery.go`:

- `ReplayLog(committed, segments, fromTxID, reg)` at `replay.go:17-79`
  iterates the pre-scanned `segments` slice in order.
- Continue across valid segments: outer loop at `replay.go:20-76`
  walks every segment without breaking on valid EOF
  (`replay.go:33-35`).
- Skip below the resume horizon: `replay.go:21-23` skips an entire
  segment when `segment.LastTx <= fromTxID`; `replay.go:43-48` skips
  individual records when `txID <= fromTxID`. This is a stricter
  early-out than the reference's per-commit rule — Shunter **does not
  open** a segment whose `LastTx <= fromTxID`. Externally equivalent
  because `ScanSegments` already observed the segment's `LastTx` and
  no commit inside a segment with `LastTx <= fromTxID` can contribute.
- Stop at validated prefix: `ScanSegments`
  (`segment_scan.go:39-96`) → `scanOneSegment`
  (`segment_scan.go:188-251`) → `canTreatAsDamagedTail`
  (`segment_scan.go:184-186`) flips the last segment to
  `AppendByFreshNextSegment` on truncated or checksum-mismatched
  trailing records. `ReplayLog` honors this via `shouldStopAfterRecord`
  (`replay.go:13-15`) and stops at the last good record.
- First-commit-of-last-segment corrupt: `canTreatAsDamagedTail`
  requires `recordCount > 0` (`segment_scan.go:184-186`). When the
  first record is corrupt, `scanOneSegment` returns the underlying
  `ChecksumMismatchError` / `ErrTruncatedRecord` and `ScanSegments`
  propagates the error. Same externally visible outcome as reference
  `io::ErrorKind::InvalidData`: open fails closed.
- Attach segment / tx context to errors: every non-EOF error path in
  `ReplayLog` wraps the underlying error with
  `"commitlog: replay <phase> [tx %d] segment %s: %w"`
  (`replay.go:27, 38, 40, 54, 56, 61, 63, 74`). Close-error paths
  carry both the primary and the close error.

Summary of current Shunter policy:
- continue across valid segments: same outcome;
- skip below resume horizon: same outcome, stricter granularity
  (segment-level short-circuit on top of per-record skip);
- stop at validated prefix: same outcome (damaged tail stops at the
  last good record; first-commit-corrupt fails closed);
- error context: same externally visible contract (segment path + tx
  id wrapped around every replay error), different helper shape
  (inline `fmt.Errorf` wrapping vs reference's `with_segment_context`).

## Decision: narrow and pin

All four ledger sub-behaviors are implemented and pinned by existing
tests; no externally visible divergence remains. Close the ledger row
by pinning the existing parity-close tests as the `P0-RECOVERY-001`
anchor, lock the one internal-mechanism difference (segment-level
short-circuit) as an explicit intentional optimization, and retain
every other deferral with a reference citation.

**Closed as parity-close (existing pins):**

- Continue across valid segments:
  `commitlog/replay_test.go::TestReplayLogReplaysAcrossSegmentsFromZeroAndReturnsMaxTxID`
  (ref `src/stream/reader.rs:41-62`,
  `src/commitlog.rs:541-545`).
- Skip records at or below resume horizon:
  `commitlog/replay_test.go::TestReplayLogSkipsRecordsAtOrBelowFromTxID`,
  `commitlog/replay_test.go::TestReplayLogSkipAllRecordsReturnsFromTxID`,
  `commitlog/replay_test.go::TestReplayLogEmptyReplayReturnsFromTxID`
  (ref `src/commitlog.rs:834-845`).
- Stop at validated prefix on tail damage:
  `commitlog/replay_test.go::TestReplayLogDamagedTailStopsAtValidatedPrefix`,
  `commitlog/replay_test.go::TestReplayLogSkipsDamagedTailSegmentWhenFromTxIDAlreadyAtValidatedPrefix`,
  `commitlog/segment_scan_test.go::TestScanSegmentsCorruptActiveSegmentAfterValidPrefixUsesFreshNextSegment`,
  `commitlog/segment_scan_test.go::TestScanSegmentsChecksumMismatchAfterValidPrefixUsesFreshNextSegment`,
  `commitlog/recovery_test.go::TestOpenAndRecoverDetailedCorruptActiveSegmentAfterValidPrefixStartsFreshNextSegment`,
  `commitlog/recovery_test.go::TestRecoveryResumePlanDamagedTailStartsFreshNextSegment`,
  `commitlog/recovery_test.go::TestRecoveryResumePlanCleanTailReopensActiveSegment`,
  `commitlog/recovery_test.go::TestOpenAndRecoverDetailedDamagedTailReturnsFreshNextSegmentPlan`
  (ref `src/repo/mod.rs:258-299`, `src/commitlog.rs:488-498`).
- First-commit-of-last-segment corrupt fails closed:
  `commitlog/segment_scan_test.go::TestScanSegmentsCorruptFirstRecordActiveSegment`,
  `commitlog/commitlog_test.go::TestOpenSegmentForAppendCorruptFirstRecordFailsClosed`,
  `commitlog/phase4_acceptance_test.go::TestDurabilityWorkerResumePlanAppendInPlaceCorruptFirstRecordFailsClosed`
  (ref `src/tests/partial.rs::first_commit_in_last_segment_corrupt`
  at `src/tests/partial.rs:142-166` — reference expects
  `io::ErrorKind::InvalidData`; Shunter returns a non-nil error from
  `ScanSegments` at the equivalent seam).
- Attach tx / segment context to replay and apply errors:
  `commitlog/replay_test.go::TestReplayLogDecodeErrorIncludesTxAndSegmentContext`,
  `commitlog/replay_test.go::TestReplayLogApplyErrorIncludesTxAndSegmentContext`
  (ref `src/repo/mod.rs:362-367`, `src/error.rs` `Traversal` /
  `Open`).

**New parity pin landed here:**

- `commitlog/parity_replay_horizon_test.go::TestParityP0Recovery001SegmentSkipDoesNotOpenExhaustedSegment`
  pins the intentional divergence that Shunter short-circuits at
  segment granularity when `SegmentInfo.LastTx <= fromTxID` (skipping
  the segment file entirely — `replay.go:21-23`) instead of the
  reference's per-commit `adjust_initial_offset`
  (`src/commitlog.rs:834-845`). Same externally visible outcome: no
  commit from such a segment can contribute above the horizon, because
  `ScanSegments` already observed the segment's `LastTx`. The
  Shunter-side skip avoids reopening the segment file at all.

**Deferred with reference-citing rationale:**

- **Per-commit skip granularity** (`src/commitlog.rs:834-845`).
  Shunter skips at segment granularity based on the `LastTx` summary
  produced by `ScanSegments`. Externally equivalent. Pinned as the
  intentional mechanism divergence above.
- **`error::Traversal` / `error::Open` enum shape**
  (`src/error.rs`). Reference returns typed errors; Shunter wraps with
  `fmt.Errorf("...: %w", err)` and relies on sentinel values and
  typed errors at the leaves (`ChecksumMismatchError`,
  `ErrTruncatedRecord`, `UnknownRecordTypeError`, `ErrBadFlags`,
  `HistoryGapError`, `store.PrimaryKeyViolationError`). Same
  externally observable contract (callers can type-assert the leaf
  error and the wrapping string carries segment path + tx id). Not
  worth reshaping without a concrete consumer asking for the reference
  shape.
- **Format-level log / changeset parity**
  (`src/commit.rs`, `src/payload/txdata.rs`). Shunter's commitlog is
  a rewrite, not format-compatible with the reference. Tracked under
  `OI-003` as a broader Phase 4 scope decision, not a replay-horizon
  concern.
- **Offset index file** (`src/index/mod.rs`,
  `src/index/indexfile.rs`). Reference can resume replay via an
  offset index; Shunter does a linear scan. Tracked under `OI-003` as
  a broader Phase 4 scope decision.
- **Mid-segment corruption that is not at the tail**. Neither side
  has a recovery story: the reference's streaming reader bubbles the
  error (`src/stream/reader.rs:116-145`); Shunter's
  `canTreatAsDamagedTail` requires `isLast && recordCount > 0`
  (`segment_scan.go:184-186`). Non-tail corruption fails closed on
  both sides. Not a parity gap.

## Why narrow-and-pin and not full emulation

- All four ledger sub-behaviors are already parity-close under
  observation. The remaining differences are mechanism-only and have
  no externally visible effect on a client observing the replay
  outcome.
- Full emulation would require (a) per-commit skip granularity via a
  `CommitInfo`-style state machine inside `ReplayLog`, (b) a typed
  `Traversal` / `Open` error enum mirroring the reference, and (c)
  format-level parity. (a) and (b) add internal complexity without a
  client-observable payoff; (c) is a separate Phase 4 scope decision
  tracked under `OI-003`.
- Option 1 (emulate now) widens scope without closing an observed
  complaint. "Do nothing" leaves the ledger row open and the
  divergences implicit — the state Phase 0 was built to eliminate.

## What this decision blocks / unblocks

Unblocks:

- `P0-RECOVERY-001` row in `docs/parity-phase0-ledger.md` moves from
  `in_progress` to `closed (divergences explicit)` once the parity pin
  lands.
- `TECH-DEBT.md` OI-007 drops the `P0-RECOVERY-001` bullet.

Does not unblock:

- Phase 4 Slice 2 commitlog format-level parity (offset index, record
  / log shape compatibility). Those remain Phase 4 follow-on decisions
  tracked under `OI-003`.
- Phase 4 Slice 3 value / encoding capability parity.

## Authoritative artifacts

- This document.
- `commitlog/parity_replay_horizon_test.go::TestParityP0Recovery001SegmentSkipDoesNotOpenExhaustedSegment`
  — new pin locking the segment-level short-circuit as an intentional
  Shunter-side optimization.
- Existing pins re-asserted as parity-close (listed above).
- `docs/parity-phase0-ledger.md` — `P0-RECOVERY-001` row moves from
  `in_progress` to `closed (divergences explicit)`.
- `TECH-DEBT.md` — `OI-007` drops the `P0-RECOVERY-001` bullet.
- `docs/spacetimedb-parity-roadmap.md` — Phase 4 Slice 2 section
  updated to note this slice closed.
- `docs/current-status.md` — commitlog / recovery bullet updated.
