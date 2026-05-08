package commitlog

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

const (
	benchmarkReplaySegments          = 4
	benchmarkReplayRecordsPerSegment = 256
)

var benchmarkSnapshotRecoveryCases = []struct {
	name         string
	snapshotRows int
	tailRecords  int
}{
	{name: "small", snapshotRows: 128, tailRecords: 16},
	{name: "medium", snapshotRows: 1024, tailRecords: 128},
	{name: "large", snapshotRows: 4096, tailRecords: 512},
}

func BenchmarkReplayLogSegmentedLog(b *testing.B) {
	root, reg, wantTxID := buildBenchmarkReplayLog(b)
	segments, durableHorizon, err := ScanSegments(root)
	if err != nil {
		b.Fatal(err)
	}
	if durableHorizon != wantTxID {
		b.Fatalf("durable horizon = %d, want %d", durableHorizon, wantTxID)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		committed := newReplayCommittedState(b, reg)
		maxTxID, err := ReplayLog(committed, segments, 0, reg)
		if err != nil {
			b.Fatal(err)
		}
		if maxTxID != wantTxID {
			b.Fatalf("max replay txID = %d, want %d", maxTxID, wantTxID)
		}
		table, ok := committed.Table(0)
		if !ok {
			b.Fatal("players table missing")
		}
		if table.RowCount() != int(wantTxID) {
			b.Fatalf("players row count = %d, want %d", table.RowCount(), wantTxID)
		}
	}
}

func BenchmarkOpenAndRecoverSegmentedLog(b *testing.B) {
	root, reg, wantTxID := buildBenchmarkReplayLog(b)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		committed, maxTxID, _, report, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			b.Fatal(err)
		}
		if maxTxID != wantTxID {
			b.Fatalf("max recovered txID = %d, want %d", maxTxID, wantTxID)
		}
		if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: wantTxID}) {
			b.Fatalf("replayed range = %+v, want 1..%d", report.ReplayedTxRange, wantTxID)
		}
		table, ok := committed.Table(0)
		if !ok {
			b.Fatal("players table missing")
		}
		if table.RowCount() != int(wantTxID) {
			b.Fatalf("players row count = %d, want %d", table.RowCount(), wantTxID)
		}
	}
}

func BenchmarkOpenAndRecoverSnapshotOnly(b *testing.B) {
	for _, tc := range benchmarkSnapshotRecoveryCases {
		b.Run(tc.name, func(b *testing.B) {
			root, reg, wantTxID := buildBenchmarkSnapshotRecovery(b, tc.snapshotRows, 0)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				committed, maxTxID, _, report, err := OpenAndRecoverWithReport(root, reg)
				if err != nil {
					b.Fatal(err)
				}
				if maxTxID != wantTxID {
					b.Fatalf("max recovered txID = %d, want %d", maxTxID, wantTxID)
				}
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != wantTxID {
					b.Fatalf("selected snapshot = (%v, %d), want (true, %d)",
						report.HasSelectedSnapshot, report.SelectedSnapshotTxID, wantTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{}) {
					b.Fatalf("replayed range = %+v, want none", report.ReplayedTxRange)
				}
				assertBenchmarkRecoveredRows(b, committed, int(wantTxID))
			}
		})
	}
}

func BenchmarkOpenAndRecoverSnapshotWithTailReplay(b *testing.B) {
	for _, tc := range benchmarkSnapshotRecoveryCases {
		b.Run(tc.name, func(b *testing.B) {
			root, reg, wantTxID := buildBenchmarkSnapshotRecovery(b, tc.snapshotRows, tc.tailRecords)
			snapshotTxID := types.TxID(tc.snapshotRows)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				committed, maxTxID, _, report, err := OpenAndRecoverWithReport(root, reg)
				if err != nil {
					b.Fatal(err)
				}
				if maxTxID != wantTxID {
					b.Fatalf("max recovered txID = %d, want %d", maxTxID, wantTxID)
				}
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != snapshotTxID {
					b.Fatalf("selected snapshot = (%v, %d), want (true, %d)",
						report.HasSelectedSnapshot, report.SelectedSnapshotTxID, snapshotTxID)
				}
				wantReplayRange := RecoveryTxIDRange{Start: snapshotTxID + 1, End: wantTxID}
				if report.ReplayedTxRange != wantReplayRange {
					b.Fatalf("replayed range = %+v, want %+v", report.ReplayedTxRange, wantReplayRange)
				}
				assertBenchmarkRecoveredRows(b, committed, int(wantTxID))
			}
		})
	}
}

func buildBenchmarkReplayLog(b *testing.B) (string, schema.SchemaRegistry, types.TxID) {
	b.Helper()
	root := b.TempDir()
	_, reg := testSchema()
	for segment := uint64(0); segment < benchmarkReplaySegments; segment++ {
		startTxID := segment*benchmarkReplayRecordsPerSegment + 1
		records := make([]replayRecord, benchmarkReplayRecordsPerSegment)
		for i := range records {
			txID := startTxID + uint64(i)
			records[i] = replayRecord{
				txID:    txID,
				inserts: []types.ProductValue{{types.NewUint64(txID), types.NewString(strconv.FormatUint(txID, 10))}},
			}
		}
		writeReplaySegment(b, root, startTxID, records...)
	}
	return root, reg, types.TxID(benchmarkReplaySegments * benchmarkReplayRecordsPerSegment)
}

func buildBenchmarkSnapshotRecovery(b *testing.B, snapshotRows, tailRecords int) (string, schema.SchemaRegistry, types.TxID) {
	b.Helper()
	root := b.TempDir()
	committed, reg := buildLargeSnapshotCommittedState(b, snapshotRows)
	snapshotTxID := types.TxID(snapshotRows)
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(b, writer, committed, snapshotTxID)

	if tailRecords > 0 {
		startTxID := uint64(snapshotRows + 1)
		records := make([]replayRecord, tailRecords)
		for i := range records {
			txID := startTxID + uint64(i)
			records[i] = replayRecord{
				txID:    txID,
				inserts: []types.ProductValue{{types.NewUint64(txID), types.NewString("player-" + strconv.FormatUint(txID, 10))}},
			}
		}
		writeReplaySegment(b, root, startTxID, records...)
	}

	return root, reg, types.TxID(snapshotRows + tailRecords)
}

func assertBenchmarkRecoveredRows(b *testing.B, committed *store.CommittedState, wantRows int) {
	b.Helper()
	table, ok := committed.Table(0)
	if !ok {
		b.Fatal("players table missing")
	}
	if table.RowCount() != wantRows {
		b.Fatalf("players row count = %d, want %d", table.RowCount(), wantRows)
	}
}
