package commitlog

import (
	"encoding/binary"
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
	seg, err := CreateSegment(root, startTx)
	if err != nil {
		t.Fatalf("CreateSegment: %v", err)
	}
	for _, rec := range records {
		payload := rec.rawPayload
		if payload == nil {
			payload = rapidEncodeReplayChangeset(t, rec)
		}
		if err := seg.Append(&Record{TxID: rec.txID, RecordType: RecordTypeChangeset, Payload: payload}); err != nil {
			_ = seg.Close()
			t.Fatalf("Append tx %d: %v", rec.txID, err)
		}
	}
	if err := seg.Close(); err != nil {
		t.Fatalf("Close segment: %v", err)
	}
	return filepath.Join(root, SegmentFileName(startTx))
}

func rapidEncodeReplayChangeset(t rapidCommitlogFataler, rec replayRecord) []byte {
	t.Helper()
	payload, err := EncodeChangeset(&store.Changeset{
		TxID: types.TxID(rec.txID),
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts:   rec.inserts,
				Deletes:   rec.deletes,
			},
		},
	})
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
