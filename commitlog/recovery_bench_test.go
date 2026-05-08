package commitlog

import (
	"strconv"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

const (
	benchmarkReplaySegments          = 4
	benchmarkReplayRecordsPerSegment = 256
)

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
