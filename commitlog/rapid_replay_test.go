package commitlog

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
	"pgregory.net/rapid"
)

type rapidReplayLog struct {
	records []replayRecord
	models  []map[uint64]string
}

type rapidCommitlogFataler interface {
	Helper()
	Fatalf(string, ...any)
}

func TestRapidReplayLogMatchesModel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 1, 20)
		segmentCount := rapid.IntRange(1, min(4, len(log.records))).Draw(t, "segmentCount")
		segments := rapidWriteReplaySegments(t, root, log.records, segmentCount)

		committed := rapidBuildReplayCommittedState(reg)
		maxTxID, err := ReplayLog(committed, segments, 0, reg)
		if err != nil {
			t.Fatalf("ReplayLog: %v", err)
		}
		if maxTxID != types.TxID(len(log.records)) {
			t.Fatalf("maxTxID = %d, want %d", maxTxID, len(log.records))
		}
		assertRapidReplayRows(t, committed, log.models[len(log.records)])
	})
}

func TestRapidReplayFromHorizonMatchesSuffixModel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 1, 20)
		segmentCount := rapid.IntRange(1, min(4, len(log.records))).Draw(t, "segmentCount")
		segments := rapidWriteReplaySegments(t, root, log.records, segmentCount)
		horizon := rapid.Uint64Range(0, uint64(len(log.records))).Draw(t, "horizon")

		committed := rapidBuildReplayCommittedState(reg)
		rapidSeedReplayRows(t, committed, log.models[int(horizon)])
		maxTxID, err := ReplayLog(committed, segments, types.TxID(horizon), reg)
		if err != nil {
			t.Fatalf("ReplayLog from horizon %d: %v", horizon, err)
		}
		if maxTxID != types.TxID(len(log.records)) {
			t.Fatalf("maxTxID = %d, want %d", maxTxID, len(log.records))
		}
		assertRapidReplayRows(t, committed, log.models[len(log.records)])
	})
}

func TestRapidOpenAndRecoverSnapshotTailMatchesFullLog(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		fullRoot := rapidTempDir(t)
		defer os.RemoveAll(fullRoot)
		snapshotRoot := rapidTempDir(t)
		defer os.RemoveAll(snapshotRoot)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 2, 18)
		finalTxID := types.TxID(len(log.records))

		fullSegmentCount := rapid.IntRange(1, min(4, len(log.records))).Draw(t, "fullSegmentCount")
		rapidWriteReplaySegments(t, fullRoot, log.records, fullSegmentCount)
		fullRecovered, fullMaxTxID, fullPlan, fullReport, err := OpenAndRecoverWithReport(fullRoot, reg)
		if err != nil {
			t.Fatalf("full-log OpenAndRecoverWithReport: %v", err)
		}
		if fullMaxTxID != finalTxID {
			t.Fatalf("full-log maxTxID = %d, want %d", fullMaxTxID, finalTxID)
		}
		assertRapidReplayRows(t, fullRecovered, log.models[len(log.records)])
		if fullReport.HasSelectedSnapshot {
			t.Fatalf("full-log selected snapshot report = (%v, %d), want none", fullReport.HasSelectedSnapshot, fullReport.SelectedSnapshotTxID)
		}
		if fullReport.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: finalTxID}) {
			t.Fatalf("full-log replay range = %+v, want 1..%d", fullReport.ReplayedTxRange, finalTxID)
		}
		if fullPlan.AppendMode != AppendInPlace || fullPlan.NextTxID != finalTxID+1 {
			t.Fatalf("full-log resume plan = %+v, want append-in-place at tx %d", fullPlan, finalTxID+1)
		}

		horizon := rapid.IntRange(1, len(log.records)-1).Draw(t, "snapshotHorizon")
		snapshotState := rapidBuildReplayCommittedState(reg)
		rapidSeedReplayRows(t, snapshotState, log.models[horizon])
		writer := NewSnapshotWriter(filepath.Join(snapshotRoot, "snapshots"), reg)
		snapshotState.SetCommittedTxID(types.TxID(horizon))
		if err := writer.CreateSnapshot(snapshotState, types.TxID(horizon)); err != nil {
			t.Fatalf("CreateSnapshot at horizon %d: %v", horizon, err)
		}
		tailRecords := log.records[horizon:]
		tailSegmentCount := rapid.IntRange(1, min(4, len(tailRecords))).Draw(t, "tailSegmentCount")
		tailSegments := rapidWriteReplaySegments(t, snapshotRoot, tailRecords, tailSegmentCount)

		snapshotRecovered, snapshotMaxTxID, snapshotPlan, snapshotReport, err := OpenAndRecoverWithReport(snapshotRoot, reg)
		if err != nil {
			t.Fatalf("snapshot-tail OpenAndRecoverWithReport: %v", err)
		}
		if snapshotMaxTxID != fullMaxTxID {
			t.Fatalf("snapshot-tail maxTxID = %d, want full-log max %d", snapshotMaxTxID, fullMaxTxID)
		}
		assertRapidReplayStatesEqual(t, snapshotRecovered, fullRecovered)
		if !snapshotReport.HasSelectedSnapshot || snapshotReport.SelectedSnapshotTxID != types.TxID(horizon) {
			t.Fatalf("snapshot-tail selected snapshot report = (%v, %d), want tx %d",
				snapshotReport.HasSelectedSnapshot, snapshotReport.SelectedSnapshotTxID, horizon)
		}
		if !snapshotReport.HasDurableLog || snapshotReport.DurableLogHorizon != finalTxID {
			t.Fatalf("snapshot-tail durable log report = (%v, %d), want (true, %d)",
				snapshotReport.HasDurableLog, snapshotReport.DurableLogHorizon, finalTxID)
		}
		if snapshotReport.ReplayedTxRange != (RecoveryTxIDRange{Start: types.TxID(horizon + 1), End: finalTxID}) {
			t.Fatalf("snapshot-tail replay range = %+v, want %d..%d", snapshotReport.ReplayedTxRange, horizon+1, finalTxID)
		}
		lastTailSegment := tailSegments[len(tailSegments)-1]
		if snapshotPlan.AppendMode != AppendInPlace || snapshotPlan.SegmentStartTx != lastTailSegment.StartTx || snapshotPlan.NextTxID != finalTxID+1 {
			t.Fatalf("snapshot-tail resume plan = %+v, want append-in-place on segment %d at tx %d",
				snapshotPlan, lastTailSegment.StartTx, finalTxID+1)
		}
	})
}

func TestRapidOpenAndRecoverAfterCompactionMatchesUncompactedSnapshotTail(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 2, 18)
		finalTxID := types.TxID(len(log.records))
		horizon := rapid.IntRange(1, len(log.records)-1).Draw(t, "snapshotHorizon")

		snapshotState := rapidBuildReplayCommittedState(reg)
		rapidSeedReplayRows(t, snapshotState, log.models[horizon])
		writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
		snapshotState.SetCommittedTxID(types.TxID(horizon))
		if err := writer.CreateSnapshot(snapshotState, types.TxID(horizon)); err != nil {
			t.Fatalf("CreateSnapshot at horizon %d: %v", horizon, err)
		}

		coveredSegment := rapidWriteReplaySegment(t, root, 1, log.records[:horizon])
		tailSegment := rapidWriteReplaySegment(t, root, uint64(horizon+1), log.records[horizon:])
		coveredIdx := filepath.Join(root, OffsetIndexFileName(1))
		tailIdx := filepath.Join(root, OffsetIndexFileName(uint64(horizon+1)))
		rapidCreateOneEntryOffsetIndex(t, coveredIdx, types.TxID(1))
		rapidCreateOneEntryOffsetIndex(t, tailIdx, types.TxID(horizon+1))

		before, beforeMaxTxID, _, _, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatalf("pre-compaction OpenAndRecoverWithReport: %v", err)
		}
		if beforeMaxTxID != finalTxID {
			t.Fatalf("pre-compaction maxTxID = %d, want %d", beforeMaxTxID, finalTxID)
		}

		if err := RunCompaction(root, types.TxID(horizon)); err != nil {
			t.Fatalf("RunCompaction at horizon %d: %v", horizon, err)
		}
		if _, err := os.Stat(coveredSegment); !os.IsNotExist(err) {
			t.Fatalf("covered segment should be compacted, stat err=%v", err)
		}
		if _, err := os.Stat(coveredIdx); !os.IsNotExist(err) {
			t.Fatalf("covered offset index should be compacted, stat err=%v", err)
		}
		if _, err := os.Stat(tailSegment); err != nil {
			t.Fatalf("tail segment should remain after compaction: %v", err)
		}
		if _, err := os.Stat(tailIdx); err != nil {
			t.Fatalf("tail offset index should remain after compaction: %v", err)
		}

		after, afterMaxTxID, afterPlan, afterReport, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatalf("post-compaction OpenAndRecoverWithReport: %v", err)
		}
		if afterMaxTxID != beforeMaxTxID {
			t.Fatalf("post-compaction maxTxID = %d, want pre-compaction max %d", afterMaxTxID, beforeMaxTxID)
		}
		assertRapidReplayStatesEqual(t, after, before)
		assertRapidReplayRows(t, after, log.models[len(log.records)])
		if !afterReport.HasSelectedSnapshot || afterReport.SelectedSnapshotTxID != types.TxID(horizon) {
			t.Fatalf("post-compaction selected snapshot report = (%v, %d), want tx %d",
				afterReport.HasSelectedSnapshot, afterReport.SelectedSnapshotTxID, horizon)
		}
		if !afterReport.HasDurableLog || afterReport.DurableLogHorizon != finalTxID {
			t.Fatalf("post-compaction durable log report = (%v, %d), want (true, %d)",
				afterReport.HasDurableLog, afterReport.DurableLogHorizon, finalTxID)
		}
		if afterReport.ReplayedTxRange != (RecoveryTxIDRange{Start: types.TxID(horizon + 1), End: finalTxID}) {
			t.Fatalf("post-compaction replay range = %+v, want %d..%d", afterReport.ReplayedTxRange, horizon+1, finalTxID)
		}
		if len(afterReport.SegmentCoverage) != 1 || afterReport.SegmentCoverage[0].MinTxID != types.TxID(horizon+1) || afterReport.SegmentCoverage[0].MaxTxID != finalTxID {
			t.Fatalf("post-compaction segment coverage = %+v, want only tail %d..%d", afterReport.SegmentCoverage, horizon+1, finalTxID)
		}
		if afterPlan.AppendMode != AppendInPlace || afterPlan.SegmentStartTx != types.TxID(horizon+1) || afterPlan.NextTxID != finalTxID+1 {
			t.Fatalf("post-compaction resume plan = %+v, want append-in-place on tail at tx %d", afterPlan, finalTxID+1)
		}
	})
}

func TestRapidCompactionRetryRemovesCoveredOrphanIndexAndPreservesRecovery(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 2, 24)
		finalTxID := types.TxID(len(log.records))
		horizon := rapid.IntRange(1, len(log.records)-1).Draw(t, "snapshotHorizon")

		rapidWriteReplaySnapshot(t, root, reg, types.TxID(horizon), log.models[horizon])
		coveredPath, coveredEntries := rapidWriteReplaySegmentWithOffsets(t, root, 1, log.records[:horizon])
		tailPath, tailEntries := rapidWriteReplaySegmentWithOffsets(t, root, uint64(horizon+1), log.records[horizon:])
		coveredIdxPath := filepath.Join(root, OffsetIndexFileName(1))
		tailIdxPath := filepath.Join(root, OffsetIndexFileName(uint64(horizon+1)))
		rapidCreateOffsetIndexFromEntries(t, coveredIdxPath, coveredEntries)
		rapidCreateOffsetIndexFromEntries(t, tailIdxPath, tailEntries)

		if err := os.Remove(coveredPath); err != nil {
			t.Fatalf("remove covered segment %s to simulate compaction crash: %v", coveredPath, err)
		}
		rapidAssertFileExists(t, coveredIdxPath)
		rapidAssertFileExists(t, tailPath)
		rapidAssertFileExists(t, tailIdxPath)

		beforeRetry, beforeRetryMaxTxID, beforeRetryPlan, beforeRetryReport, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatalf("OpenAndRecoverWithReport with orphan covered index %s: %v", coveredIdxPath, err)
		}
		if beforeRetryMaxTxID != finalTxID {
			t.Fatalf("pre-retry maxTxID = %d, want %d (report=%+v)", beforeRetryMaxTxID, finalTxID, beforeRetryReport)
		}
		assertRapidReplayRows(t, beforeRetry, log.models[len(log.records)])
		if beforeRetryPlan.AppendMode != AppendInPlace || beforeRetryPlan.SegmentStartTx != types.TxID(horizon+1) || beforeRetryPlan.NextTxID != finalTxID+1 {
			t.Fatalf("pre-retry resume plan = %+v, want append-in-place on tail %d at tx %d",
				beforeRetryPlan, horizon+1, finalTxID+1)
		}

		if err := RunCompaction(root, types.TxID(horizon)); err != nil {
			t.Fatalf("RunCompaction retry at horizon %d with orphan index %s: %v", horizon, coveredIdxPath, err)
		}
		if err := RunCompaction(root, types.TxID(horizon)); err != nil {
			t.Fatalf("RunCompaction second retry at horizon %d: %v", horizon, err)
		}
		rapidAssertFileMissing(t, coveredPath)
		rapidAssertFileMissing(t, coveredIdxPath)
		rapidAssertFileExists(t, tailPath)
		rapidAssertFileExists(t, tailIdxPath)

		afterRetry, afterRetryMaxTxID, afterRetryPlan, afterRetryReport, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatalf("post-retry OpenAndRecoverWithReport: %v", err)
		}
		if afterRetryMaxTxID != beforeRetryMaxTxID {
			t.Fatalf("post-retry maxTxID = %d, want pre-retry max %d (report=%+v)", afterRetryMaxTxID, beforeRetryMaxTxID, afterRetryReport)
		}
		assertRapidReplayStatesEqual(t, afterRetry, beforeRetry)
		assertRapidReplayRows(t, afterRetry, log.models[len(log.records)])
		if afterRetryPlan != beforeRetryPlan {
			t.Fatalf("post-retry resume plan = %+v, want pre-retry plan %+v", afterRetryPlan, beforeRetryPlan)
		}
		if len(afterRetryReport.SegmentCoverage) != 1 || afterRetryReport.SegmentCoverage[0].MinTxID != types.TxID(horizon+1) || afterRetryReport.SegmentCoverage[0].MaxTxID != finalTxID {
			t.Fatalf("post-retry segment coverage = %+v, want only tail %d..%d", afterRetryReport.SegmentCoverage, horizon+1, finalTxID)
		}
	})
}

func TestRapidReplayWithAndWithoutOffsetIndexEquivalent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		n := rapid.Uint64Range(8, 64).Draw(t, "recordCount")
		horizon := rapid.Uint64Range(0, n-1).Draw(t, "horizon")
		step := rapid.Uint64Range(2, 8).Draw(t, "sparseStep")

		segPath, entries := rapidWriteDenseReplaySegment(t, root, 1, n)
		sparse := make([]OffsetIndexEntry, 0, len(entries))
		for i := uint64(0); i < uint64(len(entries)); i += step {
			sparse = append(sparse, entries[i])
		}
		idxPath := filepath.Join(root, OffsetIndexFileName(1))
		idx := rapidPopulateSparseIndex(t, idxPath, uint64(len(sparse)+1), sparse)
		if err := idx.Close(); err != nil {
			t.Fatalf("close offset index: %v", err)
		}

		segments := []SegmentInfo{{Path: segPath, StartTx: 1, LastTx: types.TxID(n), Valid: true, AppendMode: AppendInPlace}}
		withIndex := rapidBuildReplayCommittedState(reg)
		maxWith, err := ReplayLog(withIndex, segments, types.TxID(horizon), reg)
		if err != nil {
			t.Fatalf("ReplayLog with index: %v", err)
		}

		if err := os.Remove(idxPath); err != nil {
			t.Fatalf("remove offset index: %v", err)
		}
		withoutIndex := rapidBuildReplayCommittedState(reg)
		maxWithout, err := ReplayLog(withoutIndex, segments, types.TxID(horizon), reg)
		if err != nil {
			t.Fatalf("ReplayLog without index: %v", err)
		}

		if maxWith != maxWithout || maxWith != types.TxID(n) {
			t.Fatalf("max tx mismatch: with=%d without=%d want=%d", maxWith, maxWithout, n)
		}
		assertRapidReplayStatesEqual(t, withIndex, withoutIndex)
		want := make(map[uint64]string)
		for tx := horizon + 1; tx <= n; tx++ {
			want[tx] = "p"
		}
		assertRapidReplayRows(t, withIndex, want)
	})
}

func TestRapidOpenAndRecoverWithAndWithoutOffsetIndexEquivalent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		withIndexRoot := rapidTempDir(t)
		defer os.RemoveAll(withIndexRoot)
		withoutIndexRoot := rapidTempDir(t)
		defer os.RemoveAll(withoutIndexRoot)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 3, 24)
		finalTxID := types.TxID(len(log.records))
		horizon := rapid.IntRange(1, len(log.records)-1).Draw(t, "snapshotHorizon")

		rapidWriteReplaySnapshot(t, withIndexRoot, reg, types.TxID(horizon), log.models[horizon])
		rapidWriteReplaySnapshot(t, withoutIndexRoot, reg, types.TxID(horizon), log.models[horizon])
		withSegmentPath, entries := rapidWriteReplaySegmentWithOffsets(t, withIndexRoot, 1, log.records)
		withoutSegmentPath := rapidWriteReplaySegment(t, withoutIndexRoot, 1, log.records)

		idx := rapidPopulateSparseIndex(t, filepath.Join(withIndexRoot, OffsetIndexFileName(1)), uint64(len(entries)+1), entries)
		if err := idx.Close(); err != nil {
			t.Fatalf("close offset index: %v", err)
		}

		withIndexRecovered, withIndexMaxTxID, withIndexPlan, withIndexReport, err := OpenAndRecoverWithReport(withIndexRoot, reg)
		if err != nil {
			t.Fatalf("OpenAndRecoverWithReport with offset index: %v", err)
		}
		withoutIndexRecovered, withoutIndexMaxTxID, withoutIndexPlan, withoutIndexReport, err := OpenAndRecoverWithReport(withoutIndexRoot, reg)
		if err != nil {
			t.Fatalf("OpenAndRecoverWithReport without offset index: %v", err)
		}

		if withIndexMaxTxID != finalTxID || withoutIndexMaxTxID != finalTxID {
			t.Fatalf("max tx mismatch: with=%d without=%d want=%d (withReport=%+v withoutReport=%+v)",
				withIndexMaxTxID, withoutIndexMaxTxID, finalTxID, withIndexReport, withoutIndexReport)
		}
		assertRapidReplayStatesEqual(t, withIndexRecovered, withoutIndexRecovered)
		assertRapidReplayRows(t, withIndexRecovered, log.models[len(log.records)])
		if withIndexPlan != withoutIndexPlan {
			t.Fatalf("resume plan mismatch: with=%+v without=%+v", withIndexPlan, withoutIndexPlan)
		}
		wantReplay := RecoveryTxIDRange{Start: types.TxID(horizon + 1), End: finalTxID}
		if withIndexReport.ReplayedTxRange != wantReplay || withoutIndexReport.ReplayedTxRange != wantReplay {
			t.Fatalf("replay range mismatch: with=%+v without=%+v want=%+v (segments with=%s without=%s)",
				withIndexReport.ReplayedTxRange, withoutIndexReport.ReplayedTxRange, wantReplay, withSegmentPath, withoutSegmentPath)
		}
		if !withIndexReport.HasSelectedSnapshot || withIndexReport.SelectedSnapshotTxID != types.TxID(horizon) ||
			!withoutIndexReport.HasSelectedSnapshot || withoutIndexReport.SelectedSnapshotTxID != types.TxID(horizon) {
			t.Fatalf("snapshot selection mismatch: with=%+v without=%+v want tx %d", withIndexReport, withoutIndexReport, horizon)
		}
		if withIndexPlan.AppendMode != AppendInPlace || withIndexPlan.SegmentStartTx != 1 || withIndexPlan.NextTxID != finalTxID+1 {
			t.Fatalf("resume plan = %+v, want append-in-place on original segment at tx %d", withIndexPlan, finalTxID+1)
		}
	})
}

func TestRapidOpenAndRecoverCorruptOffsetIndexFallsBackToLinearReplay(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		corruptIndexRoot := rapidTempDir(t)
		defer os.RemoveAll(corruptIndexRoot)
		noIndexRoot := rapidTempDir(t)
		defer os.RemoveAll(noIndexRoot)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 3, 24)
		finalTxID := types.TxID(len(log.records))
		horizon := rapid.IntRange(1, len(log.records)-1).Draw(t, "snapshotHorizon")
		badOffset := rapid.SampledFrom([]uint64{0, uint64(SegmentHeaderSize - 1), ^uint64(0)}).Draw(t, "badOffsetIndexByteOffset")

		rapidWriteReplaySnapshot(t, corruptIndexRoot, reg, types.TxID(horizon), log.models[horizon])
		rapidWriteReplaySnapshot(t, noIndexRoot, reg, types.TxID(horizon), log.models[horizon])
		corruptSegmentPath := rapidWriteReplaySegment(t, corruptIndexRoot, 1, log.records)
		noIndexSegmentPath := rapidWriteReplaySegment(t, noIndexRoot, 1, log.records)
		corruptIndexPath := filepath.Join(corruptIndexRoot, OffsetIndexFileName(1))
		rapidCreateCorruptOffsetIndexEntry(t, corruptIndexPath, types.TxID(horizon), badOffset)

		corruptIndexRecovered, corruptIndexMaxTxID, corruptIndexPlan, corruptIndexReport, err := OpenAndRecoverWithReport(corruptIndexRoot, reg)
		if err != nil {
			t.Fatalf("OpenAndRecoverWithReport with corrupt offset index %s -> %d for segment %s: %v",
				corruptIndexPath, badOffset, corruptSegmentPath, err)
		}
		noIndexRecovered, noIndexMaxTxID, noIndexPlan, noIndexReport, err := OpenAndRecoverWithReport(noIndexRoot, reg)
		if err != nil {
			t.Fatalf("OpenAndRecoverWithReport without offset index for segment %s: %v", noIndexSegmentPath, err)
		}

		if corruptIndexMaxTxID != finalTxID || noIndexMaxTxID != finalTxID {
			t.Fatalf("max tx mismatch: corruptIndex=%d noIndex=%d want=%d (corruptReport=%+v noIndexReport=%+v)",
				corruptIndexMaxTxID, noIndexMaxTxID, finalTxID, corruptIndexReport, noIndexReport)
		}
		assertRapidReplayStatesEqual(t, corruptIndexRecovered, noIndexRecovered)
		assertRapidReplayRows(t, corruptIndexRecovered, log.models[len(log.records)])
		if corruptIndexPlan != noIndexPlan {
			t.Fatalf("resume plan mismatch: corruptIndex=%+v noIndex=%+v", corruptIndexPlan, noIndexPlan)
		}
		wantReplay := RecoveryTxIDRange{Start: types.TxID(horizon + 1), End: finalTxID}
		if corruptIndexReport.ReplayedTxRange != wantReplay || noIndexReport.ReplayedTxRange != wantReplay {
			t.Fatalf("replay range mismatch: corruptIndex=%+v noIndex=%+v want=%+v",
				corruptIndexReport.ReplayedTxRange, noIndexReport.ReplayedTxRange, wantReplay)
		}
		if len(corruptIndexReport.DamagedTailSegments) != 0 || len(noIndexReport.DamagedTailSegments) != 0 {
			t.Fatalf("damaged tail reports: corruptIndex=%+v noIndex=%+v, want none",
				corruptIndexReport.DamagedTailSegments, noIndexReport.DamagedTailSegments)
		}
	})
}

func TestRapidOpenAndRecoverAfterBoundaryCompactionMatchesUncompacted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 4, 24)
		finalTxID := types.TxID(len(log.records))
		horizon := rapid.IntRange(2, len(log.records)-1).Draw(t, "snapshotHorizon")
		boundaryStartIndex := rapid.IntRange(1, horizon-1).Draw(t, "boundaryStartIndex")
		boundaryEndExclusive := rapid.IntRange(horizon+1, len(log.records)).Draw(t, "boundaryEndExclusive")
		boundaryStartTx := types.TxID(boundaryStartIndex + 1)
		tailStartTx := types.TxID(boundaryEndExclusive + 1)

		rapidWriteReplaySnapshot(t, root, reg, types.TxID(horizon), log.models[horizon])
		prefixPath, prefixEntries := rapidWriteReplaySegmentWithOffsets(t, root, 1, log.records[:boundaryStartIndex])
		boundaryPath, boundaryEntries := rapidWriteReplaySegmentWithOffsets(t, root, uint64(boundaryStartTx), log.records[boundaryStartIndex:boundaryEndExclusive])
		prefixIdxPath := filepath.Join(root, OffsetIndexFileName(1))
		boundaryIdxPath := filepath.Join(root, OffsetIndexFileName(uint64(boundaryStartTx)))
		rapidCreateOffsetIndexFromEntries(t, prefixIdxPath, prefixEntries)
		rapidCreateOffsetIndexFromEntries(t, boundaryIdxPath, boundaryEntries)

		var tailPath string
		if boundaryEndExclusive < len(log.records) {
			var tailEntries []OffsetIndexEntry
			tailPath, tailEntries = rapidWriteReplaySegmentWithOffsets(t, root, uint64(tailStartTx), log.records[boundaryEndExclusive:])
			rapidCreateOffsetIndexFromEntries(t, filepath.Join(root, OffsetIndexFileName(uint64(tailStartTx))), tailEntries)
		}

		before, beforeMaxTxID, beforePlan, beforeReport, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatalf("pre-compaction OpenAndRecoverWithReport: %v", err)
		}
		if beforeMaxTxID != finalTxID {
			t.Fatalf("pre-compaction maxTxID = %d, want %d (report=%+v)", beforeMaxTxID, finalTxID, beforeReport)
		}
		assertRapidReplayRows(t, before, log.models[len(log.records)])

		if err := RunCompaction(root, types.TxID(horizon)); err != nil {
			t.Fatalf("RunCompaction at horizon %d with boundary %d..%d: %v", horizon, boundaryStartIndex+1, boundaryEndExclusive, err)
		}
		rapidAssertFileMissing(t, prefixPath)
		rapidAssertFileMissing(t, prefixIdxPath)
		rapidAssertFileExists(t, boundaryPath)
		rapidAssertFileExists(t, boundaryIdxPath)
		if tailPath != "" {
			rapidAssertFileExists(t, tailPath)
		}

		after, afterMaxTxID, afterPlan, afterReport, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatalf("post-compaction OpenAndRecoverWithReport: %v", err)
		}
		if afterMaxTxID != beforeMaxTxID {
			t.Fatalf("post-compaction maxTxID = %d, want pre-compaction max %d (report=%+v)", afterMaxTxID, beforeMaxTxID, afterReport)
		}
		assertRapidReplayStatesEqual(t, after, before)
		assertRapidReplayRows(t, after, log.models[len(log.records)])
		if afterPlan != beforePlan {
			t.Fatalf("post-compaction resume plan = %+v, want pre-compaction plan %+v", afterPlan, beforePlan)
		}
		if !afterReport.HasSelectedSnapshot || afterReport.SelectedSnapshotTxID != types.TxID(horizon) {
			t.Fatalf("post-compaction selected snapshot report = (%v, %d), want tx %d",
				afterReport.HasSelectedSnapshot, afterReport.SelectedSnapshotTxID, horizon)
		}
		if afterReport.ReplayedTxRange != (RecoveryTxIDRange{Start: types.TxID(horizon + 1), End: finalTxID}) {
			t.Fatalf("post-compaction replay range = %+v, want %d..%d (report=%+v)", afterReport.ReplayedTxRange, horizon+1, finalTxID, afterReport)
		}
		if len(afterReport.SegmentCoverage) == 0 || afterReport.SegmentCoverage[0].MinTxID != boundaryStartTx {
			t.Fatalf("post-compaction segment coverage = %+v, want first retained segment to start at tx %d", afterReport.SegmentCoverage, boundaryStartTx)
		}
	})
}

func TestRapidOpenAndRecoverCorruptNewestSnapshotFallsBackToOlder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		fullRoot := rapidTempDir(t)
		defer os.RemoveAll(fullRoot)
		fallbackRoot := rapidTempDir(t)
		defer os.RemoveAll(fallbackRoot)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 3, 24)
		finalTxID := types.TxID(len(log.records))
		olderHorizon := rapid.IntRange(1, len(log.records)-1).Draw(t, "olderSnapshotHorizon")
		corruptHorizon := rapid.IntRange(olderHorizon+1, len(log.records)).Draw(t, "corruptSnapshotHorizon")

		fullSegmentCount := rapid.IntRange(1, min(4, len(log.records))).Draw(t, "fullSegmentCount")
		rapidWriteReplaySegments(t, fullRoot, log.records, fullSegmentCount)
		fullRecovered, fullMaxTxID, _, fullReport, err := OpenAndRecoverWithReport(fullRoot, reg)
		if err != nil {
			t.Fatalf("full-log OpenAndRecoverWithReport: %v", err)
		}
		if fullMaxTxID != finalTxID {
			t.Fatalf("full-log maxTxID = %d, want %d (report=%+v)", fullMaxTxID, finalTxID, fullReport)
		}

		rapidWriteReplaySnapshot(t, fallbackRoot, reg, types.TxID(olderHorizon), log.models[olderHorizon])
		rapidWriteReplaySnapshot(t, fallbackRoot, reg, types.TxID(corruptHorizon), log.models[corruptHorizon])
		corruptSnapshotPath := rapidCorruptReplaySnapshot(t, fallbackRoot, types.TxID(corruptHorizon))
		tailRecords := log.records[olderHorizon:]
		tailSegmentCount := rapid.IntRange(1, min(4, len(tailRecords))).Draw(t, "tailSegmentCount")
		tailSegments := rapidWriteReplaySegments(t, fallbackRoot, tailRecords, tailSegmentCount)

		fallbackRecovered, fallbackMaxTxID, fallbackPlan, fallbackReport, err := OpenAndRecoverWithReport(fallbackRoot, reg)
		if err != nil {
			t.Fatalf("fallback OpenAndRecoverWithReport with corrupt snapshot %s: %v", corruptSnapshotPath, err)
		}
		if fallbackMaxTxID != fullMaxTxID {
			t.Fatalf("fallback maxTxID = %d, want full-log max %d (report=%+v)", fallbackMaxTxID, fullMaxTxID, fallbackReport)
		}
		assertRapidReplayStatesEqual(t, fallbackRecovered, fullRecovered)
		assertRapidReplayRows(t, fallbackRecovered, log.models[len(log.records)])
		if !fallbackReport.HasSelectedSnapshot || fallbackReport.SelectedSnapshotTxID != types.TxID(olderHorizon) {
			t.Fatalf("fallback selected snapshot report = (%v, %d), want older tx %d (report=%+v)",
				fallbackReport.HasSelectedSnapshot, fallbackReport.SelectedSnapshotTxID, olderHorizon, fallbackReport)
		}
		if len(fallbackReport.SkippedSnapshots) != 1 {
			t.Fatalf("fallback skipped snapshots = %+v, want one corrupt newest snapshot tx %d", fallbackReport.SkippedSnapshots, corruptHorizon)
		}
		skipped := fallbackReport.SkippedSnapshots[0]
		if skipped.TxID != types.TxID(corruptHorizon) || skipped.Reason != SnapshotSkipReadFailed || skipped.Detail == "" {
			t.Fatalf("fallback skipped snapshot = %+v, want tx %d read_failed with detail", skipped, corruptHorizon)
		}
		if fallbackReport.ReplayedTxRange != (RecoveryTxIDRange{Start: types.TxID(olderHorizon + 1), End: finalTxID}) {
			t.Fatalf("fallback replay range = %+v, want %d..%d (report=%+v)",
				fallbackReport.ReplayedTxRange, olderHorizon+1, finalTxID, fallbackReport)
		}
		lastTailSegment := tailSegments[len(tailSegments)-1]
		if fallbackPlan.AppendMode != AppendInPlace || fallbackPlan.SegmentStartTx != lastTailSegment.StartTx || fallbackPlan.NextTxID != finalTxID+1 {
			t.Fatalf("fallback resume plan = %+v, want append-in-place on segment %d at tx %d",
				fallbackPlan, lastTailSegment.StartTx, finalTxID+1)
		}
	})
}

func TestRapidOpenAndRecoverSnapshotLogBoundaryGapFailsLoudly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 3, 24)
		finalTxID := types.TxID(len(log.records))
		horizon := rapid.IntRange(1, len(log.records)-2).Draw(t, "snapshotHorizon")
		gapStartTx := rapid.IntRange(horizon+2, len(log.records)).Draw(t, "gapStartTx")

		rapidWriteReplaySnapshot(t, root, reg, types.TxID(horizon), log.models[horizon])
		segmentPath := rapidWriteReplaySegment(t, root, uint64(gapStartTx), log.records[gapStartTx-1:])

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatalf("OpenAndRecoverWithReport accepted snapshot/log gap: snapshot=%d gapStart=%d segment=%s recoveredTxID=%d plan=%+v report=%+v",
				horizon, gapStartTx, segmentPath, maxTxID, plan, report)
		}
		var gapErr *HistoryGapError
		if !errors.As(err, &gapErr) {
			t.Fatalf("error = %T (%v), want HistoryGapError for snapshot=%d gapStart=%d segment=%s", err, err, horizon, gapStartTx, segmentPath)
		}
		if gapErr.Expected != uint64(horizon+1) || gapErr.Got != uint64(gapStartTx) {
			t.Fatalf("HistoryGapError = %+v, want Expected=%d Got=%d", gapErr, horizon+1, gapStartTx)
		}
		if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
			t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero after boundary gap (report=%+v)", recovered, maxTxID, plan, report)
		}
		if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != types.TxID(horizon) {
			t.Fatalf("report selected snapshot = (%v, %d), want tx %d (report=%+v)",
				report.HasSelectedSnapshot, report.SelectedSnapshotTxID, horizon, report)
		}
		if !report.HasDurableLog || report.DurableLogHorizon != finalTxID {
			t.Fatalf("report durable log = (%v, %d), want (true, %d)", report.HasDurableLog, report.DurableLogHorizon, finalTxID)
		}
		if report.RecoveredTxID != 0 || report.ResumePlan != (RecoveryResumePlan{}) || report.ReplayedTxRange != (RecoveryTxIDRange{}) {
			t.Fatalf("report after boundary gap = %+v, want no recovered tx, resume plan, or replay range", report)
		}
	})
}

func TestRapidOpenAndRecoverMissingBaseSnapshotFailsLoudly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 2, 24)
		finalTxID := types.TxID(len(log.records))
		startTx := rapid.IntRange(2, len(log.records)).Draw(t, "logStartTx")
		segmentPath := rapidWriteReplaySegment(t, root, uint64(startTx), log.records[startTx-1:])

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatalf("OpenAndRecoverWithReport accepted missing base snapshot: startTx=%d segment=%s recoveredTxID=%d plan=%+v report=%+v",
				startTx, segmentPath, maxTxID, plan, report)
		}
		if !errors.Is(err, ErrMissingBaseSnapshot) {
			t.Fatalf("error = %T (%v), want ErrMissingBaseSnapshot for startTx=%d segment=%s", err, err, startTx, segmentPath)
		}
		if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
			t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero after missing base snapshot (report=%+v)", recovered, maxTxID, plan, report)
		}
		if report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 0 {
			t.Fatalf("report selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
		}
		if !report.HasDurableLog || report.DurableLogHorizon != finalTxID {
			t.Fatalf("report durable log = (%v, %d), want (true, %d)", report.HasDurableLog, report.DurableLogHorizon, finalTxID)
		}
		if report.RecoveredTxID != 0 || report.ResumePlan != (RecoveryResumePlan{}) || report.ReplayedTxRange != (RecoveryTxIDRange{}) {
			t.Fatalf("report after missing base snapshot = %+v, want no recovered tx, resume plan, or replay range", report)
		}
	})
}

func TestRapidZeroFilledTailMatchesCleanRecovery(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cleanRoot := rapidTempDir(t)
		defer os.RemoveAll(cleanRoot)
		zeroTailRoot := rapidTempDir(t)
		defer os.RemoveAll(zeroTailRoot)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 1, 24)
		finalTxID := types.TxID(len(log.records))
		zeroTailBytes := rapid.IntRange(1, RecordOverhead*3).Draw(t, "zeroTailBytes")

		cleanPath := rapidWriteReplaySegment(t, cleanRoot, 1, log.records)
		zeroTailPath := rapidWriteReplaySegment(t, zeroTailRoot, 1, log.records)
		rapidAppendZeroTail(t, zeroTailPath, zeroTailBytes)

		cleanRecovered, cleanMaxTxID, cleanPlan, cleanReport, err := OpenAndRecoverWithReport(cleanRoot, reg)
		if err != nil {
			t.Fatalf("clean OpenAndRecoverWithReport for %s: %v", cleanPath, err)
		}
		zeroTailRecovered, zeroTailMaxTxID, zeroTailPlan, zeroTailReport, err := OpenAndRecoverWithReport(zeroTailRoot, reg)
		if err != nil {
			t.Fatalf("zero-tail OpenAndRecoverWithReport for %s with %d zero bytes: %v", zeroTailPath, zeroTailBytes, err)
		}

		if cleanMaxTxID != finalTxID || zeroTailMaxTxID != finalTxID {
			t.Fatalf("max tx mismatch: clean=%d zeroTail=%d want=%d (cleanReport=%+v zeroTailReport=%+v)",
				cleanMaxTxID, zeroTailMaxTxID, finalTxID, cleanReport, zeroTailReport)
		}
		assertRapidReplayStatesEqual(t, zeroTailRecovered, cleanRecovered)
		assertRapidReplayRows(t, zeroTailRecovered, log.models[len(log.records)])
		if zeroTailReport.ReplayedTxRange != cleanReport.ReplayedTxRange {
			t.Fatalf("zero-tail replay range = %+v, want clean range %+v (zeroTailReport=%+v)",
				zeroTailReport.ReplayedTxRange, cleanReport.ReplayedTxRange, zeroTailReport)
		}
		if len(zeroTailReport.DamagedTailSegments) != 0 {
			t.Fatalf("zero-tail damaged tail report = %+v, want none for safe zero tail", zeroTailReport.DamagedTailSegments)
		}
		if zeroTailPlan != cleanPlan || zeroTailPlan.AppendMode != AppendInPlace || zeroTailPlan.NextTxID != finalTxID+1 {
			t.Fatalf("zero-tail resume plan = %+v, clean plan = %+v, want append-in-place at tx %d",
				zeroTailPlan, cleanPlan, finalTxID+1)
		}
	})
}

func TestRapidSealedSegmentCorruptionFailsLoudly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 3, 24)
		split := rapid.IntRange(2, len(log.records)-1).Draw(t, "sealedSegmentRecordCount")
		corruptRecordIndex := rapid.IntRange(1, split-1).Draw(t, "corruptSealedRecordIndex")

		sealedPath := rapidWriteReplaySegment(t, root, 1, log.records[:split])
		tailPath := rapidWriteReplaySegment(t, root, uint64(split+1), log.records[split:])
		corruptOffset := rapidRecordPayloadOffset(t, sealedPath, corruptRecordIndex, 0)
		rapidCorruptFileByte(t, sealedPath, corruptOffset)

		segments, horizon, scanErr := ScanSegments(root)
		if scanErr == nil {
			t.Fatalf("ScanSegments accepted corrupt sealed segment %s at record index %d byte offset %d; segments=%+v horizon=%d tail=%s",
				sealedPath, corruptRecordIndex, corruptOffset, segments, horizon, tailPath)
		}

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatalf("OpenAndRecoverWithReport accepted corrupt sealed segment %s at record index %d byte offset %d; recoveredTxID=%d plan=%+v report=%+v tail=%s",
				sealedPath, corruptRecordIndex, corruptOffset, maxTxID, plan, report, tailPath)
		}
		if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
			t.Fatalf("partial recovery after corrupt sealed segment: recovered=%v maxTxID=%d plan=%+v report=%+v scanErr=%v err=%v",
				recovered, maxTxID, plan, report, scanErr, err)
		}
	})
}

func TestRapidScanDamagedTailReplaysValidPrefix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		root := rapidTempDir(t)
		defer os.RemoveAll(root)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 2, 20)
		validPrefix := rapid.IntRange(1, len(log.records)-1).Draw(t, "validPrefix")

		path := rapidWriteReplaySegment(t, root, uint64(log.records[0].txID), log.records)
		truncateAt := rapidRecordPayloadOffset(t, path, validPrefix, 0)
		if err := os.Truncate(path, int64(truncateAt)); err != nil {
			t.Fatalf("truncate damaged tail: %v", err)
		}

		segments, horizon, err := ScanSegments(root)
		if err != nil {
			t.Fatalf("ScanSegments: %v", err)
		}
		if horizon != types.TxID(validPrefix) {
			t.Fatalf("horizon = %d, want %d", horizon, validPrefix)
		}
		if len(segments) != 1 || segments[0].LastTx != types.TxID(validPrefix) || segments[0].AppendMode != AppendByFreshNextSegment {
			t.Fatalf("segments = %+v, want one damaged-tail segment through tx %d", segments, validPrefix)
		}

		committed := rapidBuildReplayCommittedState(reg)
		maxTxID, err := ReplayLog(committed, segments, 0, reg)
		if err != nil {
			t.Fatalf("ReplayLog damaged tail: %v", err)
		}
		if maxTxID != types.TxID(validPrefix) {
			t.Fatalf("maxTxID = %d, want %d", maxTxID, validPrefix)
		}
		assertRapidReplayRows(t, committed, log.models[validPrefix])

		recovered, recoveredTxID, plan, err := OpenAndRecoverDetailed(root, reg)
		if err != nil {
			t.Fatalf("OpenAndRecoverDetailed damaged tail: %v", err)
		}
		if recoveredTxID != types.TxID(validPrefix) {
			t.Fatalf("recoveredTxID = %d, want %d", recoveredTxID, validPrefix)
		}
		assertRapidReplayRows(t, recovered, log.models[validPrefix])
		if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != recoveredTxID+1 || plan.NextTxID != recoveredTxID+1 {
			t.Fatalf("resume plan = %+v, want fresh next segment at tx %d", plan, recoveredTxID+1)
		}
	})
}

func TestRapidDamagedTailResumeMatchesFullLogRecovery(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		fullRoot := rapidTempDir(t)
		defer os.RemoveAll(fullRoot)
		damagedRoot := rapidTempDir(t)
		defer os.RemoveAll(damagedRoot)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 3, 24)
		finalTxID := types.TxID(len(log.records))
		validPrefix := rapid.IntRange(1, len(log.records)-1).Draw(t, "validPrefix")

		fullSegmentCount := rapid.IntRange(1, min(4, len(log.records))).Draw(t, "fullSegmentCount")
		rapidWriteReplaySegments(t, fullRoot, log.records, fullSegmentCount)
		fullRecovered, fullMaxTxID, _, fullReport, err := OpenAndRecoverWithReport(fullRoot, reg)
		if err != nil {
			t.Fatalf("full-log OpenAndRecoverWithReport: %v", err)
		}
		if fullMaxTxID != finalTxID {
			t.Fatalf("full-log maxTxID = %d, want %d (report=%+v)", fullMaxTxID, finalTxID, fullReport)
		}
		assertRapidReplayRows(t, fullRecovered, log.models[len(log.records)])

		damagedPath := rapidWriteReplaySegment(t, damagedRoot, uint64(log.records[0].txID), log.records)
		nextPayload := rapidEncodeReplayChangeset(t, log.records[validPrefix])
		payloadBytesKept := rapid.IntRange(0, len(nextPayload)).Draw(t, "damagedTailPayloadBytesKept")
		truncateAt := rapidRecordPayloadOffset(t, damagedPath, validPrefix, payloadBytesKept)
		if err := os.Truncate(damagedPath, int64(truncateAt)); err != nil {
			t.Fatalf("truncate damaged tail at byte %d after prefix %d: %v", truncateAt, validPrefix, err)
		}

		prefixRecovered, prefixMaxTxID, prefixPlan, prefixReport, err := OpenAndRecoverWithReport(damagedRoot, reg)
		if err != nil {
			t.Fatalf("prefix OpenAndRecoverWithReport after truncating %s at byte %d: %v", damagedPath, truncateAt, err)
		}
		if prefixMaxTxID != types.TxID(validPrefix) {
			t.Fatalf("prefix maxTxID = %d, want %d (report=%+v)", prefixMaxTxID, validPrefix, prefixReport)
		}
		assertRapidReplayRows(t, prefixRecovered, log.models[validPrefix])
		if len(prefixReport.DamagedTailSegments) != 1 || prefixReport.DamagedTailSegments[0].Path != damagedPath {
			t.Fatalf("prefix damaged tails = %+v, want only %s", prefixReport.DamagedTailSegments, damagedPath)
		}
		if prefixReport.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: types.TxID(validPrefix)}) {
			t.Fatalf("prefix replay range = %+v, want 1..%d (report=%+v)", prefixReport.ReplayedTxRange, validPrefix, prefixReport)
		}
		if prefixPlan.AppendMode != AppendByFreshNextSegment || prefixPlan.SegmentStartTx != prefixMaxTxID+1 || prefixPlan.NextTxID != prefixMaxTxID+1 {
			t.Fatalf("prefix resume plan = %+v, want fresh successor at tx %d (report=%+v)", prefixPlan, prefixMaxTxID+1, prefixReport)
		}

		dw, err := NewDurabilityWorkerWithResumePlan(damagedRoot, prefixPlan, DefaultCommitLogOptions())
		if err != nil {
			t.Fatalf("NewDurabilityWorkerWithResumePlan(%+v): %v", prefixPlan, err)
		}
		for _, rec := range log.records[validPrefix:] {
			dw.EnqueueCommitted(rec.txID, rapidReplayChangeset(rec))
		}
		if durableTxID, err := dw.Close(); err != nil {
			t.Fatalf("Close resumed durability worker after prefix %d with plan %+v: %v", validPrefix, prefixPlan, err)
		} else if durableTxID != uint64(finalTxID) {
			t.Fatalf("resumed durable txID = %d, want %d", durableTxID, finalTxID)
		}

		resumedRecovered, resumedMaxTxID, resumedPlan, resumedReport, err := OpenAndRecoverWithReport(damagedRoot, reg)
		if err != nil {
			t.Fatalf("resumed OpenAndRecoverWithReport: %v", err)
		}
		if resumedMaxTxID != finalTxID {
			t.Fatalf("resumed maxTxID = %d, want %d (report=%+v)", resumedMaxTxID, finalTxID, resumedReport)
		}
		assertRapidReplayStatesEqual(t, resumedRecovered, fullRecovered)
		assertRapidReplayRows(t, resumedRecovered, log.models[len(log.records)])
		if len(resumedReport.DamagedTailSegments) != 1 || resumedReport.DamagedTailSegments[0].Path != damagedPath {
			t.Fatalf("resumed damaged tails = %+v, want retained damaged predecessor %s", resumedReport.DamagedTailSegments, damagedPath)
		}
		if resumedReport.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: finalTxID}) {
			t.Fatalf("resumed replay range = %+v, want 1..%d (report=%+v)", resumedReport.ReplayedTxRange, finalTxID, resumedReport)
		}
		if resumedPlan.AppendMode != AppendInPlace || resumedPlan.SegmentStartTx != types.TxID(validPrefix+1) || resumedPlan.NextTxID != finalTxID+1 {
			t.Fatalf("resumed plan = %+v, want append-in-place on successor %d at tx %d (report=%+v)",
				resumedPlan, validPrefix+1, finalTxID+1, resumedReport)
		}
	})
}

func TestRapidTornRolloverTailResumeMatchesFullLogRecovery(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		fullRoot := rapidTempDir(t)
		defer os.RemoveAll(fullRoot)
		tornRoot := rapidTempDir(t)
		defer os.RemoveAll(tornRoot)
		_, reg := testSchema()
		log := drawRapidReplayLog(t, 3, 24)
		finalTxID := types.TxID(len(log.records))
		validPrefix := rapid.IntRange(1, len(log.records)-1).Draw(t, "validPrefix")
		partialHeaderBytes := rapid.IntRange(1, RecordHeaderSize-1).Draw(t, "partialHeaderBytes")

		fullSegmentCount := rapid.IntRange(1, min(4, len(log.records))).Draw(t, "fullSegmentCount")
		rapidWriteReplaySegments(t, fullRoot, log.records, fullSegmentCount)
		fullRecovered, fullMaxTxID, _, fullReport, err := OpenAndRecoverWithReport(fullRoot, reg)
		if err != nil {
			t.Fatalf("full-log OpenAndRecoverWithReport: %v", err)
		}
		if fullMaxTxID != finalTxID {
			t.Fatalf("full-log maxTxID = %d, want %d (report=%+v)", fullMaxTxID, finalTxID, fullReport)
		}

		prefixPath := rapidWriteReplaySegment(t, tornRoot, 1, log.records[:validPrefix])
		tornPath := rapidWriteTornRolloverSegment(t, tornRoot, uint64(validPrefix+1), partialHeaderBytes)

		prefixRecovered, prefixMaxTxID, prefixPlan, prefixReport, err := OpenAndRecoverWithReport(tornRoot, reg)
		if err != nil {
			t.Fatalf("prefix OpenAndRecoverWithReport with torn rollover %s after prefix %s: %v", tornPath, prefixPath, err)
		}
		if prefixMaxTxID != types.TxID(validPrefix) {
			t.Fatalf("prefix maxTxID = %d, want %d (report=%+v)", prefixMaxTxID, validPrefix, prefixReport)
		}
		assertRapidReplayRows(t, prefixRecovered, log.models[validPrefix])
		if len(prefixReport.DamagedTailSegments) != 1 || prefixReport.DamagedTailSegments[0].Path != tornPath {
			t.Fatalf("prefix damaged tails = %+v, want only torn rollover %s", prefixReport.DamagedTailSegments, tornPath)
		}
		if prefixPlan.AppendMode != AppendByFreshNextSegment || prefixPlan.SegmentStartTx != prefixMaxTxID+1 || prefixPlan.NextTxID != prefixMaxTxID+1 {
			t.Fatalf("prefix resume plan = %+v, want fresh replacement at tx %d (report=%+v)", prefixPlan, prefixMaxTxID+1, prefixReport)
		}

		dw, err := NewDurabilityWorkerWithResumePlan(tornRoot, prefixPlan, DefaultCommitLogOptions())
		if err != nil {
			t.Fatalf("NewDurabilityWorkerWithResumePlan(%+v): %v", prefixPlan, err)
		}
		for _, rec := range log.records[validPrefix:] {
			dw.EnqueueCommitted(rec.txID, rapidReplayChangeset(rec))
		}
		if durableTxID, err := dw.Close(); err != nil {
			t.Fatalf("Close resumed durability worker after torn rollover %s: %v", tornPath, err)
		} else if durableTxID != uint64(finalTxID) {
			t.Fatalf("resumed durable txID = %d, want %d", durableTxID, finalTxID)
		}

		resumedRecovered, resumedMaxTxID, resumedPlan, resumedReport, err := OpenAndRecoverWithReport(tornRoot, reg)
		if err != nil {
			t.Fatalf("resumed OpenAndRecoverWithReport after torn rollover replacement: %v", err)
		}
		if resumedMaxTxID != finalTxID {
			t.Fatalf("resumed maxTxID = %d, want %d (report=%+v)", resumedMaxTxID, finalTxID, resumedReport)
		}
		assertRapidReplayStatesEqual(t, resumedRecovered, fullRecovered)
		assertRapidReplayRows(t, resumedRecovered, log.models[len(log.records)])
		if len(resumedReport.DamagedTailSegments) != 0 {
			t.Fatalf("resumed damaged tails = %+v, want none after torn rollover replacement", resumedReport.DamagedTailSegments)
		}
		if resumedReport.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: finalTxID}) {
			t.Fatalf("resumed replay range = %+v, want 1..%d (report=%+v)", resumedReport.ReplayedTxRange, finalTxID, resumedReport)
		}
		if resumedPlan.AppendMode != AppendInPlace || resumedPlan.SegmentStartTx != types.TxID(validPrefix+1) || resumedPlan.NextTxID != finalTxID+1 {
			t.Fatalf("resumed plan = %+v, want append-in-place on replacement segment %d at tx %d",
				resumedPlan, validPrefix+1, finalTxID+1)
		}
	})
}

func drawRapidReplayLog(t *rapid.T, minRecords, maxRecords int) rapidReplayLog {
	n := rapid.IntRange(minRecords, maxRecords).Draw(t, "recordCount")
	model := make(map[uint64]string)
	models := make([]map[uint64]string, 0, n+1)
	models = append(models, cloneRapidReplayModel(model))
	records := make([]replayRecord, 0, n)

	for tx := 1; tx <= n; tx++ {
		insert := len(model) == 0 || rapid.Bool().Draw(t, "insert")
		if insert {
			pk := rapid.SampledFrom(rapidAvailableReplayPKs(model)).Draw(t, "insertPK")
			name := rapid.StringMatching(`[A-Za-z0-9_]{0,16}`).Draw(t, "insertName")
			row := types.ProductValue{types.NewUint64(pk), types.NewString(name)}
			records = append(records, replayRecord{txID: uint64(tx), inserts: []types.ProductValue{row}})
			model[pk] = name
		} else {
			pk := rapid.SampledFrom(rapidReplayModelKeys(model)).Draw(t, "deletePK")
			name := model[pk]
			row := types.ProductValue{types.NewUint64(pk), types.NewString(name)}
			records = append(records, replayRecord{txID: uint64(tx), deletes: []types.ProductValue{row}})
			delete(model, pk)
		}
		models = append(models, cloneRapidReplayModel(model))
	}

	return rapidReplayLog{records: records, models: models}
}

func rapidAvailableReplayPKs(model map[uint64]string) []uint64 {
	out := make([]uint64, 0, 65-len(model))
	for pk := uint64(0); pk <= 64; pk++ {
		if _, exists := model[pk]; !exists {
			out = append(out, pk)
		}
	}
	return out
}

func rapidReplayModelKeys(model map[uint64]string) []uint64 {
	keys := make([]uint64, 0, len(model))
	for pk := range model {
		keys = append(keys, pk)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func cloneRapidReplayModel(model map[uint64]string) map[uint64]string {
	cp := make(map[uint64]string, len(model))
	for pk, name := range model {
		cp[pk] = name
	}
	return cp
}

func rapidBuildReplayCommittedState(reg schema.SchemaRegistry) *store.CommittedState {
	committed := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		tableSchema, _ := reg.Table(tableID)
		committed.RegisterTable(tableID, store.NewTable(tableSchema))
	}
	return committed
}

func rapidTempDir(t rapidCommitlogFataler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "shunter-rapid-commitlog-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	return dir
}

func rapidWriteReplaySnapshot(t rapidCommitlogFataler, root string, reg schema.SchemaRegistry, txID types.TxID, rows map[uint64]string) {
	t.Helper()
	snapshotState := rapidBuildReplayCommittedState(reg)
	rapidSeedReplayRows(t, snapshotState, rows)
	snapshotState.SetCommittedTxID(txID)
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	if err := writer.CreateSnapshot(snapshotState, txID); err != nil {
		t.Fatalf("CreateSnapshot at horizon %d: %v", txID, err)
	}
}

func rapidCorruptReplaySnapshot(t rapidCommitlogFataler, root string, txID types.TxID) string {
	t.Helper()
	path := filepath.Join(root, "snapshots", txIDString(uint64(txID)), snapshotFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Fatalf("snapshot %s is empty", path)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write corrupted snapshot %s: %v", path, err)
	}
	return path
}

func rapidSeedReplayRows(t rapidCommitlogFataler, committed *store.CommittedState, rows map[uint64]string) {
	t.Helper()
	table, ok := committed.Table(0)
	if !ok {
		t.Fatalf("players table missing")
	}
	for _, pk := range rapidReplayModelKeys(rows) {
		if err := table.InsertRow(table.AllocRowID(), types.ProductValue{types.NewUint64(pk), types.NewString(rows[pk])}); err != nil {
			t.Fatalf("seed row %d: %v", pk, err)
		}
	}
}

func rapidWriteReplaySegments(t rapidCommitlogFataler, root string, records []replayRecord, segmentCount int) []SegmentInfo {
	t.Helper()
	if len(records) == 0 {
		t.Fatalf("cannot write empty replay segment set")
	}
	if segmentCount > len(records) {
		segmentCount = len(records)
	}
	segments := make([]SegmentInfo, 0, segmentCount)
	start := 0
	for i := range segmentCount {
		remainingRecords := len(records) - start
		remainingSegments := segmentCount - i
		chunkSize := (remainingRecords + remainingSegments - 1) / remainingSegments
		chunk := records[start : start+chunkSize]
		path := rapidWriteReplaySegment(t, root, chunk[0].txID, chunk)
		appendMode := AppendForbidden
		if i == segmentCount-1 {
			appendMode = AppendInPlace
		}
		segments = append(segments, SegmentInfo{
			Path:       path,
			StartTx:    types.TxID(chunk[0].txID),
			LastTx:     types.TxID(chunk[len(chunk)-1].txID),
			Valid:      true,
			AppendMode: appendMode,
		})
		start += chunkSize
	}
	return segments
}

func rapidWriteReplaySegment(t rapidCommitlogFataler, root string, startTx uint64, records []replayRecord) string {
	t.Helper()
	path, _ := rapidWriteReplaySegmentWithOffsets(t, root, startTx, records)
	return path
}

func rapidWriteReplaySegmentWithOffsets(t rapidCommitlogFataler, root string, startTx uint64, records []replayRecord) (string, []OffsetIndexEntry) {
	t.Helper()
	seg, err := CreateSegment(root, startTx)
	if err != nil {
		t.Fatalf("CreateSegment: %v", err)
	}
	entries := make([]OffsetIndexEntry, 0, len(records))
	for _, rec := range records {
		payload := rec.rawPayload
		if payload == nil {
			payload = rapidEncodeReplayChangeset(t, rec)
		}
		if err := seg.Append(&Record{TxID: rec.txID, RecordType: RecordTypeChangeset, Payload: payload}); err != nil {
			_ = seg.Close()
			t.Fatalf("Append tx %d: %v", rec.txID, err)
		}
		off, ok := seg.LastRecordByteOffset()
		if !ok {
			_ = seg.Close()
			t.Fatalf("LastRecordByteOffset missing after tx %d", rec.txID)
		}
		entries = append(entries, OffsetIndexEntry{TxID: types.TxID(rec.txID), ByteOffset: uint64(off)})
	}
	if err := seg.Close(); err != nil {
		t.Fatalf("Close segment: %v", err)
	}
	return filepath.Join(root, SegmentFileName(startTx)), entries
}

func rapidReplayChangeset(rec replayRecord) *store.Changeset {
	return &store.Changeset{
		TxID: types.TxID(rec.txID),
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts:   rec.inserts,
				Deletes:   rec.deletes,
			},
		},
	}
}

func rapidEncodeReplayChangeset(t rapidCommitlogFataler, rec replayRecord) []byte {
	t.Helper()
	payload, err := EncodeChangeset(rapidReplayChangeset(rec))
	if err != nil {
		t.Fatalf("EncodeChangeset tx %d: %v", rec.txID, err)
	}
	return payload
}

func rapidWriteDenseReplaySegment(t rapidCommitlogFataler, root string, startTx, n uint64) (string, []OffsetIndexEntry) {
	t.Helper()
	seg, err := CreateSegment(root, startTx)
	if err != nil {
		t.Fatalf("CreateSegment: %v", err)
	}
	entries := make([]OffsetIndexEntry, 0, n)
	for i := uint64(0); i < n; i++ {
		tx := startTx + i
		payload := rapidEncodeReplayChangeset(t, replayRecord{
			txID:    tx,
			inserts: []types.ProductValue{{types.NewUint64(tx), types.NewString("p")}},
		})
		if err := seg.Append(&Record{TxID: tx, RecordType: RecordTypeChangeset, Payload: payload}); err != nil {
			_ = seg.Close()
			t.Fatalf("Append tx %d: %v", tx, err)
		}
		off, ok := seg.LastRecordByteOffset()
		if !ok {
			_ = seg.Close()
			t.Fatalf("LastRecordByteOffset missing after tx %d", tx)
		}
		entries = append(entries, OffsetIndexEntry{TxID: types.TxID(tx), ByteOffset: uint64(off)})
	}
	if err := seg.Close(); err != nil {
		t.Fatalf("Close segment: %v", err)
	}
	return filepath.Join(root, SegmentFileName(startTx)), entries
}

func rapidPopulateSparseIndex(t rapidCommitlogFataler, path string, cap uint64, entries []OffsetIndexEntry) *OffsetIndex {
	t.Helper()
	mut, err := CreateOffsetIndex(path, cap)
	if err != nil {
		t.Fatalf("CreateOffsetIndex: %v", err)
	}
	for _, entry := range entries {
		if err := mut.Append(entry.TxID, entry.ByteOffset); err != nil {
			_ = mut.Close()
			t.Fatalf("offset index append %+v: %v", entry, err)
		}
	}
	if err := mut.Sync(); err != nil {
		_ = mut.Close()
		t.Fatalf("offset index sync: %v", err)
	}
	if err := mut.Close(); err != nil {
		t.Fatalf("offset index close: %v", err)
	}
	idx, err := OpenOffsetIndex(path)
	if err != nil {
		t.Fatalf("OpenOffsetIndex: %v", err)
	}
	return idx
}

func rapidCreateOffsetIndexFromEntries(t rapidCommitlogFataler, path string, entries []OffsetIndexEntry) {
	t.Helper()
	idx := rapidPopulateSparseIndex(t, path, uint64(len(entries)+1), entries)
	if err := idx.Close(); err != nil {
		t.Fatalf("close offset index: %v", err)
	}
}

func rapidCreateCorruptOffsetIndexEntry(t rapidCommitlogFataler, path string, txID types.TxID, byteOffset uint64) {
	t.Helper()
	idx, err := CreateOffsetIndex(path, 2)
	if err != nil {
		t.Fatalf("CreateOffsetIndex: %v", err)
	}
	if err := idx.Append(txID, byteOffset); err != nil {
		_ = idx.Close()
		t.Fatalf("offset index append corrupt entry tx %d offset %d: %v", txID, byteOffset, err)
	}
	if err := idx.Sync(); err != nil {
		_ = idx.Close()
		t.Fatalf("offset index sync: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("offset index close: %v", err)
	}
}

func rapidCreateOneEntryOffsetIndex(t rapidCommitlogFataler, path string, txID types.TxID) {
	t.Helper()
	idx, err := CreateOffsetIndex(path, 2)
	if err != nil {
		t.Fatalf("CreateOffsetIndex: %v", err)
	}
	if err := idx.Append(txID, SegmentHeaderSize); err != nil {
		_ = idx.Close()
		t.Fatalf("offset index append tx %d: %v", txID, err)
	}
	if err := idx.Sync(); err != nil {
		_ = idx.Close()
		t.Fatalf("offset index sync: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("offset index close: %v", err)
	}
}

func rapidRecordPayloadOffset(t rapidCommitlogFataler, path string, recordIndex int, payloadOffset int) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read segment %s: %v", path, err)
	}
	offset := SegmentHeaderSize
	for idx := 0; idx < recordIndex; idx++ {
		if offset+RecordHeaderSize > len(data) {
			t.Fatalf("record %d header out of bounds in %s", idx, path)
		}
		payloadLen := int(binary.LittleEndian.Uint32(data[offset+10 : offset+14]))
		offset += RecordOverhead + payloadLen
	}
	if offset+RecordHeaderSize > len(data) {
		t.Fatalf("record %d header out of bounds in %s", recordIndex, path)
	}
	return offset + RecordHeaderSize + payloadOffset
}

func rapidAppendZeroTail(t rapidCommitlogFataler, path string, byteCount int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open %s for zero tail append: %v", path, err)
	}
	if _, err := f.Write(make([]byte, byteCount)); err != nil {
		_ = f.Close()
		t.Fatalf("append %d zero tail bytes to %s: %v", byteCount, path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s after zero tail append: %v", path, err)
	}
}

func rapidWriteTornRolloverSegment(t rapidCommitlogFataler, root string, startTx uint64, partialHeaderBytes int) string {
	t.Helper()
	seg, err := CreateSegment(root, startTx)
	if err != nil {
		t.Fatalf("CreateSegment torn rollover: %v", err)
	}
	if err := seg.Close(); err != nil {
		t.Fatalf("Close torn rollover segment: %v", err)
	}
	path := filepath.Join(root, SegmentFileName(startTx))
	rapidAppendNonZeroTail(t, path, partialHeaderBytes)
	return path
}

func rapidAppendNonZeroTail(t rapidCommitlogFataler, path string, byteCount int) {
	t.Helper()
	data := make([]byte, byteCount)
	for i := range data {
		data[i] = 0xa5
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open %s for nonzero tail append: %v", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		t.Fatalf("append %d nonzero tail bytes to %s: %v", byteCount, path, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s after nonzero tail append: %v", path, err)
	}
}

func rapidCorruptFileByte(t rapidCommitlogFataler, path string, offset int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s for byte corruption: %v", path, err)
	}
	if offset < 0 || offset >= len(data) {
		t.Fatalf("corrupt offset %d out of bounds for %s (%d bytes)", offset, path, len(data))
	}
	data[offset] ^= 0xff
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write corrupted %s: %v", path, err)
	}
}

func rapidAssertFileMissing(t rapidCommitlogFataler, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat err=%v", filepath.Base(path), err)
	}
}

func rapidAssertFileExists(t rapidCommitlogFataler, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", filepath.Base(path), err)
	}
}

func assertRapidReplayRows(t rapidCommitlogFataler, committed *store.CommittedState, want map[uint64]string) {
	t.Helper()
	table, ok := committed.Table(0)
	if !ok {
		t.Fatalf("players table missing")
	}
	got := make(map[uint64]string, table.RowCount())
	for _, row := range table.Scan() {
		got[row[0].AsUint64()] = row[1].AsString()
	}
	if len(got) != len(want) {
		t.Fatalf("players row count = %d, want %d (got=%v want=%v)", len(got), len(want), got, want)
	}
	for pk, wantName := range want {
		if gotName, ok := got[pk]; !ok || gotName != wantName {
			t.Fatalf("players rows = %v, want %v", got, want)
		}
	}
}

func assertRapidReplayStatesEqual(t rapidCommitlogFataler, a, b *store.CommittedState) {
	t.Helper()
	aRows := rapidReplayRows(a)
	bRows := rapidReplayRows(b)
	if len(aRows) != len(bRows) {
		t.Fatalf("row count mismatch: a=%v b=%v", aRows, bRows)
	}
	for pk, aName := range aRows {
		if bName, ok := bRows[pk]; !ok || bName != aName {
			t.Fatalf("states differ: a=%v b=%v", aRows, bRows)
		}
	}
}

func rapidReplayRows(committed *store.CommittedState) map[uint64]string {
	table, ok := committed.Table(0)
	if !ok {
		return nil
	}
	rows := make(map[uint64]string, table.RowCount())
	for _, row := range table.Scan() {
		rows[row[0].AsUint64()] = row[1].AsString()
	}
	return rows
}
