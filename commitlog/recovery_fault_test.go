package commitlog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestOpenAndRecoverDurabilityBoundaryFaultMatrix(t *testing.T) {
	type faultCase struct {
		name   string
		setup  func(t *testing.T, root string, reg schema.SchemaRegistry)
		assert func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error)
	}

	cases := []faultCase{
		{
			name: "missing-snapshot-file-with-complete-log-recovers-full-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				createMissingSnapshotCandidate(t, root, 2)
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 3 {
					t.Fatalf("maxTxID = %d, want 3", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				assertMissingSnapshotWasSkipped(t, report, 2)
				if report.HasSelectedSnapshot {
					t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
				}
			},
		},
		{
			name: "missing-snapshot-file-with-log-after-base-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				createMissingSnapshotCandidate(t, root, 2)
				writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected missing base snapshot error")
				}
				if !errors.Is(err, ErrMissingBaseSnapshot) {
					t.Fatalf("error = %v, want ErrMissingBaseSnapshot", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				assertMissingSnapshotWasSkipped(t, report, 2)
				if !report.HasDurableLog || report.DurableLogHorizon != 3 {
					t.Fatalf("durable log report = (%v, %d), want (true, 3)", report.HasDurableLog, report.DurableLogHorizon)
				}
			},
		},
		{
			name: "missing-middle-log-segment-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
				)
				writeReplaySegment(t, root, 4,
					replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected history gap error")
				}
				var gapErr *HistoryGapError
				if !errors.As(err, &gapErr) {
					t.Fatalf("expected HistoryGapError, got %T (%v)", err, err)
				}
				if gapErr.Expected != 3 || gapErr.Got != 4 {
					t.Fatalf("HistoryGapError = %+v, want Expected=3 Got=4", gapErr)
				}
				if !errors.Is(err, ErrOpen) {
					t.Fatalf("history gap error = %v, want ErrOpen category", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				assertZeroRecoveryReport(t, report)
			},
		},
		{
			name: "truncated-first-record-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				path := writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
				)
				truncateScanTestFileToOffset(t, path, int64(SegmentHeaderSize+RecordHeaderSize-1))
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected truncated first record error")
				}
				if !errors.Is(err, ErrTruncatedRecord) {
					t.Fatalf("error = %v, want ErrTruncatedRecord", err)
				}
				if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
					t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
				}
				assertZeroRecoveryReport(t, report)
			},
		},
		{
			name: "zero-filled-active-tail-recovers-valid-prefix",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				path := writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
				)
				appendZeroTail(t, path, RecordOverhead*2)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 2 {
					t.Fatalf("maxTxID = %d, want 2", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
				if len(report.DamagedTailSegments) != 0 {
					t.Fatalf("damaged tail report = %+v, want none for zero-filled tail", report.DamagedTailSegments)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 3 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 3", plan)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			_, reg := testSchema()
			tc.setup(t, root, reg)

			recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
			tc.assert(t, recovered, maxTxID, plan, report, err)
		})
	}
}

func createMissingSnapshotCandidate(t *testing.T, root string, txID types.TxID) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "snapshots", txIDString(uint64(txID))), 0o755); err != nil {
		t.Fatal(err)
	}
}

func appendZeroTail(t *testing.T, path string, byteCount int) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(make([]byte, byteCount)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertMissingSnapshotWasSkipped(t *testing.T, report RecoveryReport, txID types.TxID) {
	t.Helper()
	if len(report.SkippedSnapshots) != 1 {
		t.Fatalf("skipped snapshots = %+v, want one missing snapshot", report.SkippedSnapshots)
	}
	skipped := report.SkippedSnapshots[0]
	if skipped.TxID != txID || skipped.Reason != SnapshotSkipReadFailed || skipped.Detail == "" {
		t.Fatalf("skipped snapshot = %+v, want tx %d read_failed with detail", skipped, txID)
	}
}

func assertZeroRecoveryReport(t *testing.T, report RecoveryReport) {
	t.Helper()
	if report.HasSelectedSnapshot ||
		report.SelectedSnapshotTxID != 0 ||
		report.HasDurableLog ||
		report.DurableLogHorizon != 0 ||
		report.ReplayedTxRange != (RecoveryTxIDRange{}) ||
		report.RecoveredTxID != 0 ||
		report.ResumePlan != (RecoveryResumePlan{}) ||
		len(report.SkippedSnapshots) != 0 ||
		len(report.DamagedTailSegments) != 0 ||
		len(report.SegmentCoverage) != 0 {
		t.Fatalf("recovery report = %+v, want zero report", report)
	}
}
