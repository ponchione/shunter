package commitlog

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestOpenAndRecoverUsesOffsetIndexForSnapshotCoveredStartupScan(t *testing.T) {
	root := t.TempDir()
	const snapshotHorizon = 180
	const finalTxID = 220

	committed, reg := buildLargeSnapshotCommittedState(t, snapshotHorizon)
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, snapshotHorizon)

	records := recoveryOffsetIndexReplayRecords(1, finalTxID)
	_, entries := rapidWriteReplaySegmentWithOffsets(t, root, 1, records)
	rapidCreateOffsetIndexFromEntries(t, filepath.Join(root, OffsetIndexFileName(1)), entries)

	scannedTxIDs := captureSegmentScanTxIDs(t)
	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatalf("OpenAndRecoverWithReport: %v", err)
	}
	if maxTxID != finalTxID {
		t.Fatalf("max txID = %d, want %d (report=%+v)", maxTxID, finalTxID, report)
	}
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != snapshotHorizon {
		t.Fatalf("selected snapshot = (%v, %d), want (true, %d)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID, snapshotHorizon)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: snapshotHorizon + 1, End: finalTxID}) {
		t.Fatalf("replayed range = %+v, want %d..%d", report.ReplayedTxRange, snapshotHorizon+1, finalTxID)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != finalTxID+1 {
		t.Fatalf("resume plan = %+v, want append-in-place on retained segment at tx %d", plan, finalTxID+1)
	}
	assertRecoveryOffsetIndexRows(t, recovered, finalTxID)

	scanned := scannedTxIDs()
	if len(scanned) == 0 {
		t.Fatal("startup scan decoded no records, want indexed suffix scan")
	}
	if len(scanned) >= snapshotHorizon {
		t.Fatalf("startup scan decoded %d records, want snapshot-covered prefix skipped; first=%d last=%d",
			len(scanned), scanned[0], scanned[len(scanned)-1])
	}
	if got, want := scanned[0], uint64(snapshotHorizon+1); got != want {
		t.Fatalf("first startup-scanned txID = %d, want %d", got, want)
	}
	if got, want := scanned[len(scanned)-1], uint64(finalTxID); got != want {
		t.Fatalf("last startup-scanned txID = %d, want %d", got, want)
	}
}

func TestOpenAndRecoverFallsBackToLinearScanForUnsafeOffsetIndexes(t *testing.T) {
	tests := []struct {
		name       string
		writeIndex func(t *testing.T, path string, entries []OffsetIndexEntry)
	}{
		{
			name: "missing",
		},
		{
			name: "corrupt",
			writeIndex: func(t *testing.T, path string, _ []OffsetIndexEntry) {
				t.Helper()
				if err := os.WriteFile(path, []byte{1, 2, 3}, 0o600); err != nil {
					t.Fatalf("write corrupt index: %v", err)
				}
			},
		},
		{
			name: "non-monotonic",
			writeIndex: func(t *testing.T, path string, entries []OffsetIndexEntry) {
				t.Helper()
				writeRawRecoveryOffsetIndex(t, path, []OffsetIndexEntry{
					entries[0],
					entries[30],
					entries[20],
				}, 4)
			},
		},
		{
			name: "stale",
			writeIndex: func(t *testing.T, path string, entries []OffsetIndexEntry) {
				t.Helper()
				writeRawRecoveryOffsetIndex(t, path, []OffsetIndexEntry{
					{TxID: 31, ByteOffset: entries[0].ByteOffset},
				}, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			const snapshotHorizon = 30
			const finalTxID = 40

			committed, reg := buildLargeSnapshotCommittedState(t, snapshotHorizon)
			writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
			createSnapshotAt(t, writer, committed, snapshotHorizon)

			records := recoveryOffsetIndexReplayRecords(1, finalTxID)
			_, entries := rapidWriteReplaySegmentWithOffsets(t, root, 1, records)
			if tt.writeIndex != nil {
				tt.writeIndex(t, filepath.Join(root, OffsetIndexFileName(1)), entries)
			}

			scannedTxIDs := captureSegmentScanTxIDs(t)
			recovered, maxTxID, _, report, err := OpenAndRecoverWithReport(root, reg)
			if err != nil {
				t.Fatalf("OpenAndRecoverWithReport: %v", err)
			}
			if maxTxID != finalTxID {
				t.Fatalf("max txID = %d, want %d (report=%+v)", maxTxID, finalTxID, report)
			}
			if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != snapshotHorizon {
				t.Fatalf("selected snapshot = (%v, %d), want (true, %d)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID, snapshotHorizon)
			}
			assertRecoveryOffsetIndexRows(t, recovered, finalTxID)
			scanned := scannedTxIDs()
			if len(scanned) != finalTxID {
				t.Fatalf("startup scan decoded %d records, want full linear scan of %d records: %v", len(scanned), finalTxID, scanned)
			}
			if scanned[0] != 1 || scanned[len(scanned)-1] != finalTxID {
				t.Fatalf("startup scan txIDs = first %d last %d, want 1..%d", scanned[0], scanned[len(scanned)-1], finalTxID)
			}
		})
	}
}

func TestOpenAndRecoverIgnoresOffsetIndexWithoutSnapshot(t *testing.T) {
	root := t.TempDir()
	const finalTxID = 12
	_, reg := testSchema()

	records := recoveryOffsetIndexReplayRecords(1, finalTxID)
	rapidWriteReplaySegmentWithOffsets(t, root, 1, records)
	if err := os.WriteFile(filepath.Join(root, OffsetIndexFileName(1)), []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatalf("write corrupt index: %v", err)
	}

	scannedTxIDs := captureSegmentScanTxIDs(t)
	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatalf("OpenAndRecoverWithReport: %v", err)
	}
	if maxTxID != finalTxID {
		t.Fatalf("max txID = %d, want %d (report=%+v)", maxTxID, finalTxID, report)
	}
	if report.HasSelectedSnapshot {
		t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: finalTxID}) {
		t.Fatalf("replayed range = %+v, want 1..%d", report.ReplayedTxRange, finalTxID)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != finalTxID+1 {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx %d", plan, finalTxID+1)
	}
	assertRecoveryOffsetIndexRows(t, recovered, finalTxID)

	scanned := scannedTxIDs()
	if len(scanned) < finalTxID {
		t.Fatalf("startup scan decoded %d records, want at least one full linear scan of %d records: %v", len(scanned), finalTxID, scanned)
	}
	for i := uint64(0); i < finalTxID; i++ {
		if scanned[i] != i+1 {
			t.Fatalf("startup scan txIDs first pass = %v, want prefix 1..%d", scanned, finalTxID)
		}
	}
}

func recoveryOffsetIndexReplayRecords(startTxID, endTxID uint64) []replayRecord {
	records := make([]replayRecord, 0, endTxID-startTxID+1)
	for txID := startTxID; txID <= endTxID; txID++ {
		records = append(records, replayRecord{
			txID:    txID,
			inserts: []types.ProductValue{{types.NewUint64(txID), types.NewString("player-" + strconv.FormatUint(txID, 10))}},
		})
	}
	return records
}

func captureSegmentScanTxIDs(t *testing.T) func() []uint64 {
	t.Helper()
	var scanned []uint64
	previous := segmentScanRecordHook
	segmentScanRecordHook = func(rec *Record) {
		scanned = append(scanned, rec.TxID)
	}
	t.Cleanup(func() {
		segmentScanRecordHook = previous
	})
	return func() []uint64 {
		return append([]uint64(nil), scanned...)
	}
}

func writeRawRecoveryOffsetIndex(t *testing.T, path string, entries []OffsetIndexEntry, capEntries int) {
	t.Helper()
	buf := make([]byte, capEntries*OffsetIndexEntrySize)
	for i, entry := range entries {
		binary.LittleEndian.PutUint64(buf[i*OffsetIndexEntrySize+offsetIndexKeyOff:], uint64(entry.TxID))
		binary.LittleEndian.PutUint64(buf[i*OffsetIndexEntrySize+offsetIndexValOff:], entry.ByteOffset)
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write raw offset index: %v", err)
	}
}

func assertRecoveryOffsetIndexRows(t *testing.T, committed *store.CommittedState, wantRows types.TxID) {
	t.Helper()
	table, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	if table.RowCount() != int(wantRows) {
		t.Fatalf("players row count = %d, want %d", table.RowCount(), wantRows)
	}
}
