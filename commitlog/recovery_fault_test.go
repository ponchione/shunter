package commitlog

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
			name: "damaged-snapshot-with-complete-log-recovers-full-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 2, map[uint64]string{1: "alice", 2: "bob"})
				corruptSelectionSnapshot(t, root, 2)
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
				assertSkippedSnapshot(t, report, 2, SnapshotSkipReadFailed)
				if report.HasSelectedSnapshot {
					t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
					t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
				}
			},
		},
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
				assertSkippedSnapshot(t, report, 2, SnapshotSkipReadFailed)
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
			name: "missing-newest-snapshot-file-falls-back-to-older-snapshot-and-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{1: "alice"})
				createMissingSnapshotCandidate(t, root, 7)
				writeReplaySegment(t, root, 6,
					replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 7 {
					t.Fatalf("maxTxID = %d, want 7", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
				assertSkippedSnapshot(t, report, 7, SnapshotSkipReadFailed)
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 5 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 5)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 6, End: 7}) {
					t.Fatalf("replayed range = %+v, want 6..7", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 6 || plan.NextTxID != 8 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 6 at tx 8", plan)
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
				assertSkippedSnapshot(t, report, 2, SnapshotSkipReadFailed)
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
			name: "damaged-sealed-segment-tail-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				sealedPath := writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
				)
				writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
				)
				corruptScanTestRecordPayloadByte(t, sealedPath, 1, 0)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err == nil {
					t.Fatal("expected history gap error")
				}
				var gapErr *HistoryGapError
				if !errors.As(err, &gapErr) {
					t.Fatalf("expected HistoryGapError, got %T (%v)", err, err)
				}
				if gapErr.Expected != 2 || gapErr.Got != 3 {
					t.Fatalf("HistoryGapError = %+v, want Expected=2 Got=3", gapErr)
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
			name: "truncated-active-tail-header-recovers-valid-prefix",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				path := writeReplaySegment(t, root, 1,
					replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
					replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("partial-carol")}}},
				)
				truncateScanTestFileToOffset(t, path, int64(scanTestRecordOffset(t, path, 2)+RecordHeaderSize-1))
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 2 {
					t.Fatalf("maxTxID = %d, want 2", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
				if len(report.DamagedTailSegments) != 1 || report.DamagedTailSegments[0].LastTx != 2 {
					t.Fatalf("damaged tail report = %+v, want one segment with LastTx 2", report.DamagedTailSegments)
				}
				if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
					t.Fatalf("resume plan = %+v, want fresh segment at tx 3", plan)
				}
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

func TestOpenAndRecoverSegmentHeaderFaultsFailLoudly(t *testing.T) {
	cases := []struct {
		name   string
		bytes  []byte
		assert func(t *testing.T, err error)
	}{
		{
			name:  "bad-magic",
			bytes: []byte{'X', 'X', 'X', 'X', SegmentVersion, 0, 0, 0},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrBadMagic) {
					t.Fatalf("error = %v, want ErrBadMagic", err)
				}
				if !errors.Is(err, ErrOpen) {
					t.Fatalf("error = %v, want ErrOpen category", err)
				}
			},
		},
		{
			name:  "bad-version",
			bytes: []byte{SegmentMagic[0], SegmentMagic[1], SegmentMagic[2], SegmentMagic[3], SegmentVersion + 1, 0, 0, 0},
			assert: func(t *testing.T, err error) {
				var versionErr *BadVersionError
				if !errors.As(err, &versionErr) {
					t.Fatalf("expected BadVersionError, got %T (%v)", err, err)
				}
				if versionErr.Got != SegmentVersion+1 {
					t.Fatalf("bad version got = %d, want %d", versionErr.Got, SegmentVersion+1)
				}
				if !errors.Is(err, ErrOpen) {
					t.Fatalf("error = %v, want ErrOpen category", err)
				}
			},
		},
		{
			name:  "bad-flags",
			bytes: []byte{SegmentMagic[0], SegmentMagic[1], SegmentMagic[2], SegmentMagic[3], SegmentVersion, 1, 0, 0},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, ErrBadFlags) {
					t.Fatalf("error = %v, want ErrBadFlags", err)
				}
			},
		},
		{
			name:  "truncated-header",
			bytes: []byte{SegmentMagic[0], SegmentMagic[1], SegmentMagic[2]},
			assert: func(t *testing.T, err error) {
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("error = %v, want io.ErrUnexpectedEOF", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			_, reg := testSchema()
			path := filepath.Join(root, SegmentFileName(1))
			if err := os.WriteFile(path, tc.bytes, 0o644); err != nil {
				t.Fatal(err)
			}

			recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
			if err == nil {
				t.Fatal("expected recovery to fail loudly")
			}
			if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
				t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
			}
			assertZeroRecoveryReport(t, report)
			tc.assert(t, err)
		})
	}
}

func TestOpenAndRecoverSnapshotSchemaMismatchFailsLoudly(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeFaultSnapshot(t, root, reg, 5, map[uint64]string{1: "alice"})
	writeReplaySegment(t, root, 6,
		replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)

	mismatchReg := cloneSelectionRegistry(reg, func(tables map[schema.TableID]schema.TableSchema) {
		players := tables[0]
		players.Columns[1].Type = schema.KindUint64
		tables[0] = players
	})

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, mismatchReg)
	if err == nil {
		t.Fatal("expected schema mismatch error")
	}
	var mismatchErr *SchemaMismatchError
	if !errors.As(err, &mismatchErr) {
		t.Fatalf("expected SchemaMismatchError, got %T (%v)", err, err)
	}
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("error = %v, want ErrSnapshot category", err)
	}
	if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
		t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != 6 {
		t.Fatalf("durable log report = (%v, %d), want (true, 6)", report.HasDurableLog, report.DurableLogHorizon)
	}
	if report.HasSelectedSnapshot || len(report.SkippedSnapshots) != 0 || report.RecoveredTxID != 0 {
		t.Fatalf("report = %+v, want no selected/skipped snapshot and no recovered tx", report)
	}
}

func TestOpenAndRecoverSnapshotPastDurableHorizonMatrix(t *testing.T) {
	type horizonCase struct {
		name   string
		setup  func(t *testing.T, root string, reg schema.SchemaRegistry)
		assert func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error)
	}

	cases := []horizonCase{
		{
			name: "newest-snapshot-past-horizon-falls-back-to-older-snapshot",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{1: "alice"})
				writeFaultSnapshot(t, root, reg, 10, map[uint64]string{99: "too-new"})
				writeReplaySegment(t, root, 6,
					replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
					replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
					replayRecord{txID: 8, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
				)
			},
			assert: func(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, err error) {
				if err != nil {
					t.Fatal(err)
				}
				if maxTxID != 8 {
					t.Fatalf("maxTxID = %d, want 8", maxTxID)
				}
				assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol", 4: "dave"})
				assertSkippedSnapshot(t, report, 10, SnapshotSkipPastDurableHorizon)
				if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 5 {
					t.Fatalf("selected snapshot report = (%v, %d), want (true, 5)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if !report.HasDurableLog || report.DurableLogHorizon != 8 {
					t.Fatalf("durable log report = (%v, %d), want (true, 8)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 6, End: 8}) {
					t.Fatalf("replayed range = %+v, want 6..8", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 6 || plan.NextTxID != 9 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 6 at tx 9", plan)
				}
			},
		},
		{
			name: "only-snapshot-past-horizon-recovers-complete-base-log",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{99: "too-new"})
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
				assertSkippedSnapshot(t, report, 5, SnapshotSkipPastDurableHorizon)
				if report.HasSelectedSnapshot {
					t.Fatalf("selected snapshot = (%v, %d), want none", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
				}
				if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 3}) {
					t.Fatalf("replayed range = %+v, want 1..3", report.ReplayedTxRange)
				}
				if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 4 {
					t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 4", plan)
				}
			},
		},
		{
			name: "only-snapshot-past-horizon-with-missing-base-log-fails-loudly",
			setup: func(t *testing.T, root string, reg schema.SchemaRegistry) {
				writeFaultSnapshot(t, root, reg, 5, map[uint64]string{99: "too-new"})
				writeReplaySegment(t, root, 3,
					replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
					replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
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
				assertSkippedSnapshot(t, report, 5, SnapshotSkipPastDurableHorizon)
				if !report.HasDurableLog || report.DurableLogHorizon != 4 {
					t.Fatalf("durable log report = (%v, %d), want (true, 4)", report.HasDurableLog, report.DurableLogHorizon)
				}
				if report.HasSelectedSnapshot || report.RecoveredTxID != 0 {
					t.Fatalf("report = %+v, want no selected snapshot or recovered tx", report)
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

func TestOpenAndRecoverHeaderOnlySegmentBoundaries(t *testing.T) {
	t.Run("first-segment-before-first-record-recovers-empty-state", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		createHeaderOnlySegment(t, root, 1)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatal(err)
		}
		if maxTxID != 0 {
			t.Fatalf("maxTxID = %d, want 0", maxTxID)
		}
		assertReplayPlayerRows(t, recovered, map[uint64]string{})
		if !report.HasDurableLog || report.DurableLogHorizon != 0 {
			t.Fatalf("durable log report = (%v, %d), want (true, 0)", report.HasDurableLog, report.DurableLogHorizon)
		}
		if len(report.SegmentCoverage) != 1 || report.SegmentCoverage[0].MinTxID != 1 || report.SegmentCoverage[0].MaxTxID != 0 {
			t.Fatalf("segment coverage = %+v, want one empty 1..0 segment", report.SegmentCoverage)
		}
		if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != 1 {
			t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx 1", plan)
		}
	})

	t.Run("rollover-segment-before-first-record-recovers-valid-prefix", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		)
		createHeaderOnlySegment(t, root, 3)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err != nil {
			t.Fatal(err)
		}
		if maxTxID != 2 {
			t.Fatalf("maxTxID = %d, want 2", maxTxID)
		}
		assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
		if !report.HasDurableLog || report.DurableLogHorizon != 2 {
			t.Fatalf("durable log report = (%v, %d), want (true, 2)", report.HasDurableLog, report.DurableLogHorizon)
		}
		if len(report.SegmentCoverage) != 2 || report.SegmentCoverage[1].MinTxID != 3 || report.SegmentCoverage[1].MaxTxID != 2 {
			t.Fatalf("segment coverage = %+v, want empty active rollover segment 3..2", report.SegmentCoverage)
		}
		if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
			t.Fatalf("resume plan = %+v, want append-in-place on segment 3 at tx 3", plan)
		}
	})

	t.Run("header-only-segment-after-gap-fails-loudly", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		createHeaderOnlySegment(t, root, 3)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected missing base snapshot error")
		}
		if !errors.Is(err, ErrMissingBaseSnapshot) {
			t.Fatalf("error = %v, want ErrMissingBaseSnapshot", err)
		}
		if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
			t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
		}
		if !report.HasDurableLog || report.DurableLogHorizon != 2 {
			t.Fatalf("durable log report = (%v, %d), want (true, 2)", report.HasDurableLog, report.DurableLogHorizon)
		}
	})
}

func TestOpenAndRecoverLogicalReplayFaultsFailLoudly(t *testing.T) {
	t.Run("valid-record-with-invalid-changeset-payload", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: []byte{0xff}},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected invalid changeset payload to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "changeset too short") {
			t.Fatalf("replay decode error %q missing payload failure detail", err)
		}
	})

	t.Run("valid-record-with-unsafe-duplicate-primary-key", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice-again")}}},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected duplicate primary key payload to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		var pkErr *store.PrimaryKeyViolationError
		if !errors.As(err, &pkErr) {
			t.Fatalf("expected PrimaryKeyViolationError, got %T (%v)", err, err)
		}
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay apply error %q missing tx or segment context", err)
		}
	})

	t.Run("valid-record-with-trailing-changeset-bytes", func(t *testing.T) {
		root := t.TempDir()
		_, reg := testSchema()
		payload, err := EncodeChangeset(&store.Changeset{
			TxID: 2,
			Tables: map[schema.TableID]*store.TableChangeset{
				0: {
					TableID:   0,
					TableName: "players",
					Inserts:   []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload, 0xde, 0xad, 0xbe, 0xef)
		segmentPath := writeReplaySegment(t, root, 1,
			replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
			replayRecord{txID: 2, rawPayload: payload},
		)

		recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
		if err == nil {
			t.Fatal("expected trailing changeset bytes to fail recovery")
		}
		assertNoRecoveredStateAfterReplayFault(t, recovered, maxTxID, plan, report, 2)
		if !strings.Contains(err.Error(), "tx 2") || !strings.Contains(err.Error(), segmentPath) {
			t.Fatalf("replay decode error %q missing tx or segment context", err)
		}
		if !strings.Contains(err.Error(), "trailing changeset bytes") {
			t.Fatalf("replay decode error %q missing trailing-bytes detail", err)
		}
	})
}

func TestOpenAndRecoverFallsBackWhenOffsetIndexPointsInsideSegmentHeader(t *testing.T) {
	root := t.TempDir()
	const horizon = types.TxID(512)
	const lastTx = uint64(1024)

	committed, reg := buildLargeSnapshotCommittedState(t, int(horizon))
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, horizon)

	_, entries := writeDenseReplaySegment(t, root, 1, lastTx)
	idxPath := filepath.Join(root, OffsetIndexFileName(1))
	idx := populateSparseIndex(t, idxPath, 4, []OffsetIndexEntry{
		{TxID: horizon, ByteOffset: 1},
		entries[768],
	})
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != types.TxID(lastTx) {
		t.Fatalf("maxTxID = %d, want %d", maxTxID, lastTx)
	}
	players, ok := recovered.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	if players.RowCount() != int(lastTx) {
		t.Fatalf("players row count = %d, want %d", players.RowCount(), lastTx)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 1 || plan.NextTxID != types.TxID(lastTx+1) {
		t.Fatalf("resume plan = %+v, want append-in-place on segment 1 at tx %d", plan, lastTx+1)
	}
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != horizon {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, %d)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID, horizon)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: horizon + 1, End: types.TxID(lastTx)}) {
		t.Fatalf("replayed range = %+v, want %d..%d", report.ReplayedTxRange, horizon+1, lastTx)
	}
}

func TestCreateSnapshotParentSyncFailureLeavesNoSelectableArtifacts(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
	players, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	committed.SetCommittedTxID(9)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg).(*FileSnapshotWriter)
	syncErr := errors.New("parent sync failed")
	writer.syncDir = func(path string) error {
		if path == writer.baseDir {
			return syncErr
		}
		return nil
	}

	err := writer.CreateSnapshot(committed, 9)
	if !errors.Is(err, ErrSnapshot) {
		t.Fatalf("snapshot creation error = %v, want ErrSnapshot category", err)
	}
	if !errors.Is(err, syncErr) {
		t.Fatalf("snapshot creation error = %v, want wrapped sync failure", err)
	}
	var completionErr *SnapshotCompletionError
	if !errors.As(err, &completionErr) {
		t.Fatalf("expected SnapshotCompletionError, got %v", err)
	}
	if completionErr.Phase != "sync-parent" || completionErr.Path != writer.baseDir {
		t.Fatalf("completion error = %+v, want sync-parent on base dir", completionErr)
	}

	snapshotDir := filepath.Join(writer.baseDir, "9")
	if HasLockFile(snapshotDir) {
		t.Fatal("snapshot lock should not exist when parent sync fails before lock creation")
	}
	for _, name := range []string{snapshotTempFileName, snapshotFileName} {
		if _, err := os.Stat(filepath.Join(snapshotDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist after parent sync failure, stat err=%v", name, err)
		}
	}
	ids, listErr := ListSnapshots(writer.baseDir)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(ids) != 1 || ids[0] != 9 {
		t.Fatalf("ListSnapshots = %v, want incomplete directory to remain discoverable for read-failure reporting", ids)
	}
	_, readErr := ReadSnapshot(snapshotDir)
	if readErr == nil {
		t.Fatal("incomplete snapshot directory should not be readable")
	}
}

func assertNoRecoveredStateAfterReplayFault(t *testing.T, recovered *store.CommittedState, maxTxID types.TxID, plan RecoveryResumePlan, report RecoveryReport, durableHorizon types.TxID) {
	t.Helper()
	if recovered != nil || maxTxID != 0 || plan != (RecoveryResumePlan{}) {
		t.Fatalf("partial recovery = (%v, %d, %+v), want nil/zero", recovered, maxTxID, plan)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != durableHorizon {
		t.Fatalf("durable log report = (%v, %d), want (true, %d)", report.HasDurableLog, report.DurableLogHorizon, durableHorizon)
	}
	if report.HasSelectedSnapshot || report.RecoveredTxID != 0 || report.ReplayedTxRange != (RecoveryTxIDRange{}) || report.ResumePlan != (RecoveryResumePlan{}) {
		t.Fatalf("report = %+v, want no selected snapshot, recovered tx, replay range, or resume plan", report)
	}
}

func writeFaultSnapshot(t *testing.T, root string, reg schema.SchemaRegistry, txID types.TxID, rows map[uint64]string) {
	t.Helper()
	committed := buildRecoveryCommittedState(t, reg)
	players, ok := committed.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	for id, name := range rows {
		if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(id), types.NewString(name)}); err != nil {
			t.Fatal(err)
		}
	}
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, txID)
}

func createHeaderOnlySegment(t *testing.T, root string, startTxID uint64) {
	t.Helper()
	seg, err := CreateSegment(root, startTxID)
	if err != nil {
		t.Fatal(err)
	}
	if err := seg.Close(); err != nil {
		t.Fatal(err)
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

func assertSkippedSnapshot(t *testing.T, report RecoveryReport, txID types.TxID, reason SnapshotSkipReason) {
	t.Helper()
	if len(report.SkippedSnapshots) != 1 {
		t.Fatalf("skipped snapshots = %+v, want one skipped snapshot", report.SkippedSnapshots)
	}
	skipped := report.SkippedSnapshots[0]
	if skipped.TxID != txID || skipped.Reason != reason {
		t.Fatalf("skipped snapshot = %+v, want tx %d %s with detail", skipped, txID, reason)
	}
	if reason == SnapshotSkipReadFailed && skipped.Detail == "" {
		t.Fatalf("skipped snapshot = %+v, want read failure detail", skipped)
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
