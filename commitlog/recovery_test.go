package commitlog

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestOpenAndRecoverSnapshotAndLogRecovery(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoveryAutoIncrementRegistry(t)
	committed := buildRecoveryCommittedState(t, reg)

	jobs, ok := committed.Table(0)
	if !ok {
		t.Fatal("jobs table missing")
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("seed-1")}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("seed-2")}); err != nil {
		t.Fatal(err)
	}
	jobs.SetSequenceValue(3)
	jobs.SetNextID(10)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 2)

	writeRecoverySegment(t, root, reg, 3,
		recoveryRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("replayed-3")}}},
		recoveryRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("replayed-4")}}},
	)

	recovered, maxTxID, err := OpenAndRecover(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 4 {
		t.Fatalf("maxTxID = %d, want 4", maxTxID)
	}
	if got := recovered.CommittedTxID(); got != maxTxID {
		t.Fatalf("committed horizon = %d, want recovered max txID %d", got, maxTxID)
	}

	recoveredJobs, ok := recovered.Table(0)
	if !ok {
		t.Fatal("recovered jobs table missing")
	}
	assertRecoveryRows(t, recoveredJobs, map[uint64]string{1: "seed-1", 2: "seed-2", 3: "replayed-3", 4: "replayed-4"})
	if recoveredJobs.NextID() != 12 {
		t.Fatalf("NextID = %d, want 12", recoveredJobs.NextID())
	}
	if seq, has := recoveredJobs.SequenceValue(); !has || seq != 5 {
		t.Fatalf("SequenceValue = (%d, %v), want (5, true)", seq, has)
	}
	pk := recoveredJobs.PrimaryIndex()
	if pk == nil {
		t.Fatal("primary index should be rebuilt")
	}
	if got := len(pk.Seek(pk.ExtractKey(types.ProductValue{types.NewUint64(4), types.NewString("replayed-4")}))); got != 1 {
		t.Fatalf("primary index seek count = %d, want 1", got)
	}
}

func TestOpenAndRecoverSnapshotPlusTailMatchesFullLogReplay(t *testing.T) {
	fullLogRoot := t.TempDir()
	snapshotRoot := t.TempDir()
	_, reg := testSchema()
	writeReplaySegment(t, fullLogRoot, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
		replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
		replayRecord{txID: 5, inserts: []types.ProductValue{{types.NewUint64(5), types.NewString("eve")}}},
	)
	writeFaultSnapshot(t, snapshotRoot, reg, 3, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
	writeReplaySegment(t, snapshotRoot, 4,
		replayRecord{txID: 4, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
		replayRecord{txID: 5, inserts: []types.ProductValue{{types.NewUint64(5), types.NewString("eve")}}},
	)

	fullRecovered, fullMaxTxID, fullPlan, fullReport, err := OpenAndRecoverWithReport(fullLogRoot, reg)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRecovered, snapshotMaxTxID, snapshotPlan, snapshotReport, err := OpenAndRecoverWithReport(snapshotRoot, reg)
	if err != nil {
		t.Fatal(err)
	}
	if fullMaxTxID != 5 || snapshotMaxTxID != 5 {
		t.Fatalf("max tx mismatch: full=%d snapshot=%d want 5", fullMaxTxID, snapshotMaxTxID)
	}
	assertReplayStatesEqual(t, fullRecovered, snapshotRecovered)
	assertReplayPlayerRows(t, snapshotRecovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol", 4: "dave", 5: "eve"})

	if fullReport.ReplayedTxRange != (RecoveryTxIDRange{Start: 1, End: 5}) {
		t.Fatalf("full-log replay range = %+v, want 1..5", fullReport.ReplayedTxRange)
	}
	if fullPlan.AppendMode != AppendInPlace || fullPlan.SegmentStartTx != 1 || fullPlan.NextTxID != 6 {
		t.Fatalf("full-log resume plan = %+v, want append-in-place on segment 1 at tx 6", fullPlan)
	}
	if !snapshotReport.HasSelectedSnapshot || snapshotReport.SelectedSnapshotTxID != 3 {
		t.Fatalf("snapshot report selected = (%v, %d), want (true, 3)", snapshotReport.HasSelectedSnapshot, snapshotReport.SelectedSnapshotTxID)
	}
	if snapshotReport.ReplayedTxRange != (RecoveryTxIDRange{Start: 4, End: 5}) {
		t.Fatalf("snapshot replay range = %+v, want 4..5", snapshotReport.ReplayedTxRange)
	}
	if snapshotPlan.AppendMode != AppendInPlace || snapshotPlan.SegmentStartTx != 4 || snapshotPlan.NextTxID != 6 {
		t.Fatalf("snapshot resume plan = %+v, want append-in-place on segment 4 at tx 6", snapshotPlan)
	}
}

func TestOpenAndRecoverWithReportReturnsStructuredRecoveryReport(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	base := buildRecoveryCommittedState(t, reg)
	players, _ := base.Table(0)
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, base, 5)
	createSnapshotAt(t, writer, base, 10)
	corruptSelectionSnapshot(t, root, 10)

	writeReplaySegment(t, root, 6,
		replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
		replayRecord{txID: 8, inserts: []types.ProductValue{{types.NewUint64(4), types.NewString("dave")}}},
		replayRecord{txID: 9, inserts: []types.ProductValue{{types.NewUint64(5), types.NewString("erin")}}},
		replayRecord{txID: 10, inserts: []types.ProductValue{{types.NewUint64(6), types.NewString("frank")}}},
	)

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 10 {
		t.Fatalf("maxTxID = %d, want 10", maxTxID)
	}
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 5 {
		t.Fatalf("selected snapshot report = (%v, %d), want (true, 5)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != 10 {
		t.Fatalf("durable log report = (%v, %d), want (true, 10)", report.HasDurableLog, report.DurableLogHorizon)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 6, End: 10}) {
		t.Fatalf("replayed tx range = %+v, want 6..10", report.ReplayedTxRange)
	}
	if report.RecoveredTxID != maxTxID {
		t.Fatalf("recovered txID report = %d, want %d", report.RecoveredTxID, maxTxID)
	}
	if report.ResumePlan != plan {
		t.Fatalf("resume plan report = %+v, want %+v", report.ResumePlan, plan)
	}
	if len(report.SegmentCoverage) != 1 || report.SegmentCoverage[0].MinTxID != 6 || report.SegmentCoverage[0].MaxTxID != 10 {
		t.Fatalf("segment coverage report = %+v, want one 6..10 segment", report.SegmentCoverage)
	}
	if len(report.SkippedSnapshots) != 1 {
		t.Fatalf("skipped snapshots = %+v, want one corrupt fallback", report.SkippedSnapshots)
	}
	skipped := report.SkippedSnapshots[0]
	if skipped.TxID != 10 || skipped.Reason != SnapshotSkipReadFailed || skipped.Detail == "" {
		t.Fatalf("skipped snapshot report = %+v, want tx 10 read failure with detail", skipped)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol", 4: "dave", 5: "erin", 6: "frank"})
}

func TestOpenAndRecoverSnapshotLogBoundaryGapFailsLoudly(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	base := buildRecoveryCommittedState(t, reg)
	players, _ := base.Table(0)
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, base, 5)
	writeReplaySegment(t, root, 7,
		replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(7), types.NewString("skipped-tx6")}}},
	)

	_, _, _, report, err := OpenAndRecoverWithReport(root, reg)
	if err == nil {
		t.Fatal("expected snapshot/log boundary gap error")
	}
	if !errors.Is(err, ErrOpen) {
		t.Fatalf("boundary gap error = %v, want ErrOpen category", err)
	}
	var gapErr *HistoryGapError
	if !errors.As(err, &gapErr) {
		t.Fatalf("expected HistoryGapError, got %T (%v)", err, err)
	}
	if gapErr.Expected != 6 || gapErr.Got != 7 {
		t.Fatalf("HistoryGapError = %+v, want Expected=6 Got=7", gapErr)
	}
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 5 {
		t.Fatalf("report selected snapshot = (%v, %d), want (true, 5)", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != 7 {
		t.Fatalf("report durable log = (%v, %d), want (true, 7)", report.HasDurableLog, report.DurableLogHorizon)
	}
}

func TestOpenAndRecoverDetailedSnapshotReplayDoesNotRegressSequenceFromExplicitAutoincrementRows(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoveryAutoIncrementRegistry(t)
	committed := buildRecoveryCommittedState(t, reg)

	jobs, ok := committed.Table(0)
	if !ok {
		t.Fatal("jobs table missing")
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("seed-1")}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("seed-2")}); err != nil {
		t.Fatal(err)
	}
	jobs.SetSequenceValue(50)
	jobs.SetNextID(10)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 2)
	writeRecoverySegment(t, root, reg, 3,
		recoveryRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(42), types.NewString("explicit-42")}}},
	)

	recovered, maxTxID, plan, err := OpenAndRecoverDetailed(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("maxTxID = %d, want 3", maxTxID)
	}
	if plan.NextTxID != 4 {
		t.Fatalf("resume plan next tx = %d, want 4", plan.NextTxID)
	}

	recoveredJobs, ok := recovered.Table(0)
	if !ok {
		t.Fatal("recovered jobs table missing")
	}
	assertRecoveryRows(t, recoveredJobs, map[uint64]string{1: "seed-1", 2: "seed-2", 42: "explicit-42"})
	if recoveredJobs.NextID() != 11 {
		t.Fatalf("NextID after recovery = %d, want 11", recoveredJobs.NextID())
	}
	if seq, has := recoveredJobs.SequenceValue(); !has || seq != 50 {
		t.Fatalf("SequenceValue after recovery = (%d, %v), want (50, true)", seq, has)
	}

	tx := store.NewTransaction(recovered, reg)
	rowID, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("post-recovery-auto")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, rowID)
	if !ok {
		t.Fatal("post-recovery row missing from transaction view")
	}
	if got := row[0].AsUint64(); got != 50 {
		t.Fatalf("post-recovery autoincrement value = %d, want 50", got)
	}
	if seq, has := recoveredJobs.SequenceValue(); !has || seq != 51 {
		t.Fatalf("SequenceValue after post-recovery insert = (%d, %v), want (51, true)", seq, has)
	}
}

func TestOpenAndRecoverDetailedReplayExplicitAutoincrementValueRaisesRecoveredSequence(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoveryAutoIncrementRegistry(t)
	committed := buildRecoveryCommittedState(t, reg)

	jobs, ok := committed.Table(0)
	if !ok {
		t.Fatal("jobs table missing")
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("seed-1")}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewUint64(2), types.NewString("seed-2")}); err != nil {
		t.Fatal(err)
	}
	jobs.SetSequenceValue(3)
	jobs.SetNextID(10)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 2)
	writeRecoverySegment(t, root, reg, 3,
		recoveryRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(42), types.NewString("explicit-42")}}},
	)

	recovered, maxTxID, plan, err := OpenAndRecoverDetailed(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("maxTxID = %d, want 3", maxTxID)
	}
	if plan.NextTxID != 4 {
		t.Fatalf("resume plan next tx = %d, want 4", plan.NextTxID)
	}

	recoveredJobs, ok := recovered.Table(0)
	if !ok {
		t.Fatal("recovered jobs table missing")
	}
	assertRecoveryRows(t, recoveredJobs, map[uint64]string{1: "seed-1", 2: "seed-2", 42: "explicit-42"})
	if seq, has := recoveredJobs.SequenceValue(); !has || seq != 43 {
		t.Fatalf("SequenceValue after recovery = (%d, %v), want (43, true)", seq, has)
	}

	tx := store.NewTransaction(recovered, reg)
	rowID, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("post-recovery-auto")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, rowID)
	if !ok {
		t.Fatal("post-recovery row missing from transaction view")
	}
	if got := row[0].AsUint64(); got != 43 {
		t.Fatalf("post-recovery autoincrement value = %d, want 43", got)
	}
}

func TestOpenAndRecoverDetailedReplayExplicitSignedAutoincrementValueRaisesRecoveredSequence(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoverySignedAutoIncrementRegistry(t)
	committed := buildRecoveryCommittedState(t, reg)

	jobs, ok := committed.Table(0)
	if !ok {
		t.Fatal("jobs table missing")
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewInt64(1), types.NewString("seed-1")}); err != nil {
		t.Fatal(err)
	}
	if err := jobs.InsertRow(jobs.AllocRowID(), types.ProductValue{types.NewInt64(2), types.NewString("seed-2")}); err != nil {
		t.Fatal(err)
	}
	jobs.SetSequenceValue(3)
	jobs.SetNextID(10)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 2)
	writeRecoverySegment(t, root, reg, 3,
		recoveryRecord{txID: 3, inserts: []types.ProductValue{{types.NewInt64(42), types.NewString("explicit-42")}}},
	)

	recovered, maxTxID, plan, err := OpenAndRecoverDetailed(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("maxTxID = %d, want 3", maxTxID)
	}
	if plan.NextTxID != 4 {
		t.Fatalf("resume plan next tx = %d, want 4", plan.NextTxID)
	}

	recoveredJobs, ok := recovered.Table(0)
	if !ok {
		t.Fatal("recovered jobs table missing")
	}
	assertSignedRecoveryRows(t, recoveredJobs, map[int64]string{1: "seed-1", 2: "seed-2", 42: "explicit-42"})
	if seq, has := recoveredJobs.SequenceValue(); !has || seq != 43 {
		t.Fatalf("SequenceValue after recovery = (%d, %v), want (43, true)", seq, has)
	}

	tx := store.NewTransaction(recovered, reg)
	rowID, err := tx.Insert(0, types.ProductValue{types.NewInt64(0), types.NewString("post-recovery-auto")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, rowID)
	if !ok {
		t.Fatal("post-recovery row missing from transaction view")
	}
	if got := row[0].AsInt64(); got != 43 {
		t.Fatalf("post-recovery autoincrement value = %d, want 43", got)
	}
}

func TestOpenAndRecoverDetailedReplayNegativeSignedAutoincrementDoesNotMaskPositiveSequenceAdvance(t *testing.T) {
	root := t.TempDir()
	reg := buildRecoverySignedAutoIncrementRegistry(t)
	committed := buildRecoveryCommittedState(t, reg)
	committed.SetCommittedTxID(1)

	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 1)
	writeRecoverySegment(t, root, reg, 2,
		recoveryRecord{txID: 2, inserts: []types.ProductValue{{types.NewInt64(-7), types.NewString("explicit-negative")}}},
		recoveryRecord{txID: 3, inserts: []types.ProductValue{{types.NewInt64(42), types.NewString("explicit-positive")}}},
	)

	recovered, maxTxID, plan, err := OpenAndRecoverDetailed(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("maxTxID = %d, want 3", maxTxID)
	}
	if plan.NextTxID != 4 {
		t.Fatalf("resume plan next tx = %d, want 4", plan.NextTxID)
	}

	recoveredJobs, ok := recovered.Table(0)
	if !ok {
		t.Fatal("recovered jobs table missing")
	}
	assertSignedRecoveryRows(t, recoveredJobs, map[int64]string{-7: "explicit-negative", 42: "explicit-positive"})
	if seq, has := recoveredJobs.SequenceValue(); !has || seq != 43 {
		t.Fatalf("SequenceValue after recovery = (%d, %v), want (43, true)", seq, has)
	}

	tx := store.NewTransaction(recovered, reg)
	rowID, err := tx.Insert(0, types.ProductValue{types.NewInt64(0), types.NewString("post-recovery-auto")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, rowID)
	if !ok {
		t.Fatal("post-recovery row missing from transaction view")
	}
	if got := row[0].AsInt64(); got != 43 {
		t.Fatalf("post-recovery autoincrement value = %d, want 43", got)
	}
}

func TestOpenAndRecoverFromScratchWithoutSnapshot(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)

	recovered, maxTxID, err := OpenAndRecover(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
	if len(recovered.TableIDs()) != len(reg.Tables()) {
		t.Fatalf("registered tables = %d, want %d", len(recovered.TableIDs()), len(reg.Tables()))
	}
	for _, tableID := range reg.Tables() {
		table, ok := recovered.Table(tableID)
		if !ok || table.Schema().ID != tableID {
			t.Fatalf("missing recovered table %d", tableID)
		}
	}
}

func TestOpenAndRecoverMissingBaseSnapshot(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeReplaySegment(t, root, 3,
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)

	_, _, err := OpenAndRecover(root, reg)
	if !errors.Is(err, ErrMissingBaseSnapshot) {
		t.Fatalf("expected ErrMissingBaseSnapshot, got %v", err)
	}
}

func TestOpenAndRecoverCorruptNewestAndLockedSnapshotsFallback(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	base := buildRecoveryCommittedState(t, reg)
	players, _ := base.Table(0)
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, base, 5)
	createSnapshotAt(t, writer, base, 6)
	corruptSelectionSnapshot(t, root, 6)

	for _, txID := range []types.TxID{7, 8} {
		dir := filepath.Join(root, "snapshots", txIDString(uint64(txID)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := CreateLockFile(dir); err != nil {
			t.Fatal(err)
		}
	}

	writeReplaySegment(t, root, 6,
		replayRecord{txID: 6, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 7, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)

	recovered, maxTxID, err := OpenAndRecover(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 7 {
		t.Fatalf("maxTxID = %d, want 7", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
}

func TestOpenAndRecoverSnapshotOnlyReturnsSnapshotState(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
	players, _ := committed.Table(0)
	if err := players.InsertRow(players.AllocRowID(), types.ProductValue{types.NewUint64(1), types.NewString("alice")}); err != nil {
		t.Fatal(err)
	}
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 5)

	recovered, maxTxID, err := OpenAndRecover(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 5 {
		t.Fatalf("maxTxID = %d, want 5", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice"})
}

func TestOpenAndRecoverEmptyBootstrapSnapshotOnlyReturnsEmptyState(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 0)

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 0 {
		t.Fatalf("maxTxID = %d, want 0", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 0 {
		t.Fatalf("selected snapshot report = (%v, %d), want bootstrap snapshot", report.HasSelectedSnapshot, report.SelectedSnapshotTxID)
	}
	if report.RecoveredTxID != 0 || report.ReplayedTxRange != (RecoveryTxIDRange{}) || report.HasDurableLog {
		t.Fatalf("report = %+v, want bootstrap snapshot-only recovery without log replay", report)
	}
	if plan.AppendMode != AppendByFreshNextSegment || plan.SegmentStartTx != 1 || plan.NextTxID != 1 {
		t.Fatalf("resume plan = %+v, want fresh segment at tx 1", plan)
	}
}

func TestOpenAndRecoverSnapshotOnlyResumeAppendsTail(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	committed := buildRecoveryCommittedState(t, reg)
	players, _ := committed.Table(0)
	for _, row := range []types.ProductValue{
		{types.NewUint64(1), types.NewString("alice")},
		{types.NewUint64(2), types.NewString("bob")},
	} {
		if err := players.InsertRow(players.AllocRowID(), row); err != nil {
			t.Fatal(err)
		}
	}
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 2)

	firstRecovered, firstMaxTxID, firstPlan, firstReport, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if firstMaxTxID != 2 {
		t.Fatalf("first maxTxID = %d, want 2", firstMaxTxID)
	}
	assertReplayPlayerRows(t, firstRecovered, map[uint64]string{1: "alice", 2: "bob"})
	if !firstReport.HasSelectedSnapshot || firstReport.SelectedSnapshotTxID != 2 {
		t.Fatalf("first selected snapshot = (%v, %d), want tx 2 (report=%+v)",
			firstReport.HasSelectedSnapshot, firstReport.SelectedSnapshotTxID, firstReport)
	}
	if firstReport.HasDurableLog || firstReport.ReplayedTxRange != (RecoveryTxIDRange{}) {
		t.Fatalf("first report = %+v, want snapshot-only recovery without durable log replay", firstReport)
	}
	if firstPlan.AppendMode != AppendByFreshNextSegment || firstPlan.SegmentStartTx != 3 || firstPlan.NextTxID != 3 {
		t.Fatalf("first plan = %+v, want fresh segment at tx 3", firstPlan)
	}

	dw, err := NewDurabilityWorkerWithResumePlan(root, firstPlan, DefaultCommitLogOptions())
	if err != nil {
		t.Fatalf("resume from snapshot-only plan %+v: %v", firstPlan, err)
	}
	dw.EnqueueCommitted(3, &store.Changeset{
		TxID: 3,
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts:   []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}},
			},
		},
	})
	if finalTxID, err := dw.Close(); err != nil {
		t.Fatal(err)
	} else if finalTxID != 3 {
		t.Fatalf("durability final txID = %d, want 3", finalTxID)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("second maxTxID = %d, want 3", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
	if !report.HasSelectedSnapshot || report.SelectedSnapshotTxID != 2 {
		t.Fatalf("second selected snapshot = (%v, %d), want tx 2 (report=%+v)",
			report.HasSelectedSnapshot, report.SelectedSnapshotTxID, report)
	}
	if !report.HasDurableLog || report.DurableLogHorizon != 3 {
		t.Fatalf("second durable log = (%v, %d), want horizon 3 (report=%+v)",
			report.HasDurableLog, report.DurableLogHorizon, report)
	}
	if report.ReplayedTxRange != (RecoveryTxIDRange{Start: 3, End: 3}) {
		t.Fatalf("second replay range = %+v, want 3..3 (report=%+v)", report.ReplayedTxRange, report)
	}
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 3 || plan.NextTxID != 4 {
		t.Fatalf("second plan = %+v, want append-in-place on tx 3 segment", plan)
	}
}

func TestOpenAndRecoverSnapshotRebuildsSecondaryIndexes(t *testing.T) {
	root := t.TempDir()
	reg := buildSelectionRegistry(t, selectionRegistryConfig{extraNameIndex: true})
	committed := buildSelectionCommittedState(t, reg)
	writer := NewSnapshotWriter(filepath.Join(root, "snapshots"), reg)
	createSnapshotAt(t, writer, committed, 5)

	recovered, maxTxID, err := OpenAndRecover(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 5 {
		t.Fatalf("maxTxID = %d, want 5", maxTxID)
	}
	players, ok := recovered.Table(0)
	if !ok {
		t.Fatal("players table missing")
	}
	var nameIndexID schema.IndexID
	found := false
	ts, _ := reg.Table(0)
	for _, idx := range ts.Indexes {
		if idx.Name == "by_name" {
			nameIndexID = idx.ID
			found = true
			break
		}
	}
	if !found {
		t.Fatal("by_name index missing from registry")
	}
	idx := players.IndexByID(nameIndexID)
	if idx == nil {
		t.Fatal("by_name index missing from recovered table")
	}
	if got := len(idx.Seek(store.NewIndexKey(types.NewString("alice")))); got != 1 {
		t.Fatalf("by_name index seek count = %d, want 1", got)
	}
}

func TestOpenAndRecoverNoData(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()

	_, _, err := OpenAndRecover(root, reg)
	if !errors.Is(err, ErrNoData) {
		t.Fatalf("expected ErrNoData, got %v", err)
	}
}

func TestOpenAndRecoverDetailedCorruptActiveSegmentAfterValidPrefixStartsFreshNextSegment(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	path := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)
	corruptScanTestRecordPayloadByte(t, path, 2, 0)

	recovered, maxTxID, plan, err := OpenAndRecoverDetailed(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
	if plan.AppendMode != AppendByFreshNextSegment {
		t.Fatalf("appendMode = %d, want %d", plan.AppendMode, AppendByFreshNextSegment)
	}
	if plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
		t.Fatalf("resume plan = %+v, want segmentStartTx=3 nextTxID=3", plan)
	}
}

func TestRecoveryResumePlanDamagedTailStartsFreshNextSegment(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	path := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)
	truncateScanTestFileToOffset(t, path, int64(scanTestRecordPayloadOffset(t, path, 2, 10)))

	recovered, maxTxID, plan, err := OpenAndRecoverDetailed(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
	if plan.AppendMode != AppendByFreshNextSegment {
		t.Fatalf("appendMode = %d, want %d", plan.AppendMode, AppendByFreshNextSegment)
	}
	if plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
		t.Fatalf("resume plan = %+v, want segmentStartTx=3 nextTxID=3", plan)
	}

	compatRecovered, compatMaxTxID, err := OpenAndRecover(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if compatMaxTxID != maxTxID {
		t.Fatalf("OpenAndRecover maxTxID = %d, want %d", compatMaxTxID, maxTxID)
	}
	assertReplayPlayerRows(t, compatRecovered, map[uint64]string{1: "alice", 2: "bob"})
}

func TestRecoveryResumePlanCleanTailReopensActiveSegment(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(5), types.NewString("eve")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(6), types.NewString("frank")}}},
	)

	recovered, maxTxID, plan, err := OpenAndRecoverDetailed(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{5: "eve", 6: "frank"})
	if plan.AppendMode != AppendInPlace {
		t.Fatalf("appendMode = %d, want %d", plan.AppendMode, AppendInPlace)
	}
	if plan.SegmentStartTx != 1 || plan.NextTxID != 3 {
		t.Fatalf("resume plan = %+v, want segmentStartTx=1 nextTxID=3", plan)
	}
}

func TestOpenAndRecoverDetailedDamagedTailReturnsFreshNextSegmentPlan(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	path := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}}},
	)
	truncateScanTestFileToOffset(t, path, int64(scanTestRecordPayloadOffset(t, path, 2, 10)))

	recovered, maxTxID, plan, err := OpenAndRecoverDetailed(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 2 {
		t.Fatalf("maxTxID = %d, want 2", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob"})
	if plan.AppendMode != AppendByFreshNextSegment {
		t.Fatalf("AppendMode = %d, want %d", plan.AppendMode, AppendByFreshNextSegment)
	}
	if plan.SegmentStartTx != 3 || plan.NextTxID != 3 {
		t.Fatalf("resume plan = %+v, want SegmentStartTx=3 NextTxID=3", plan)
	}
}

func TestOpenAndRecoverDetailedSecondRestartKeepsDamagedTailSegmentRecoverable(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	damagedPath := writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("partial-carol")}}},
	)
	truncateScanTestFileToOffset(t, damagedPath, int64(scanTestRecordPayloadOffset(t, damagedPath, 2, 10)))

	firstRecovered, firstMaxTxID, firstPlan, err := OpenAndRecoverDetailed(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if firstMaxTxID != 2 {
		t.Fatalf("first recovery maxTxID = %d, want 2", firstMaxTxID)
	}
	assertReplayPlayerRows(t, firstRecovered, map[uint64]string{1: "alice", 2: "bob"})
	if firstPlan.AppendMode != AppendByFreshNextSegment || firstPlan.SegmentStartTx != 3 || firstPlan.NextTxID != 3 {
		t.Fatalf("first recovery plan = %+v, want fresh segment at tx 3", firstPlan)
	}

	dw, err := NewDurabilityWorkerWithResumePlan(root, firstPlan, DefaultCommitLogOptions())
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(3, &store.Changeset{
		TxID: 3,
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts:   []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}},
			},
		},
	})
	if finalTxID, err := dw.Close(); err != nil {
		t.Fatal(err)
	} else if finalTxID != 3 {
		t.Fatalf("durability final txID = %d, want 3", finalTxID)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("second recovery maxTxID = %d, want 3", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 3 || plan.NextTxID != 4 {
		t.Fatalf("second recovery plan = %+v, want append-in-place on fresh tx 3 segment", plan)
	}
	if len(report.DamagedTailSegments) != 1 || report.DamagedTailSegments[0].Path != damagedPath {
		t.Fatalf("damaged tail report = %+v, want only %s", report.DamagedTailSegments, damagedPath)
	}
}

func TestOpenAndRecoverDetailedSecondRestartReplacesTornRolloverTail(t *testing.T) {
	root := t.TempDir()
	_, reg := testSchema()
	writeReplaySegment(t, root, 1,
		replayRecord{txID: 1, inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("alice")}}},
		replayRecord{txID: 2, inserts: []types.ProductValue{{types.NewUint64(2), types.NewString("bob")}}},
	)
	damagedPath := writeReplaySegment(t, root, 3,
		replayRecord{txID: 3, inserts: []types.ProductValue{{types.NewUint64(3), types.NewString("partial-carol")}}},
	)
	truncateScanTestFileToOffset(t, damagedPath, int64(SegmentHeaderSize+RecordHeaderSize-1))

	firstRecovered, firstMaxTxID, firstPlan, firstReport, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if firstMaxTxID != 2 {
		t.Fatalf("first recovery maxTxID = %d, want 2", firstMaxTxID)
	}
	assertReplayPlayerRows(t, firstRecovered, map[uint64]string{1: "alice", 2: "bob"})
	if firstPlan.AppendMode != AppendByFreshNextSegment || firstPlan.SegmentStartTx != 3 || firstPlan.NextTxID != 3 {
		t.Fatalf("first recovery plan = %+v, want fresh segment at tx 3", firstPlan)
	}
	if len(firstReport.DamagedTailSegments) != 1 || firstReport.DamagedTailSegments[0].Path != damagedPath {
		t.Fatalf("first damaged tail report = %+v, want only %s", firstReport.DamagedTailSegments, damagedPath)
	}

	dw, err := NewDurabilityWorkerWithResumePlan(root, firstPlan, DefaultCommitLogOptions())
	if err != nil {
		t.Fatal(err)
	}
	dw.EnqueueCommitted(3, &store.Changeset{
		TxID: 3,
		Tables: map[schema.TableID]*store.TableChangeset{
			0: {
				TableID:   0,
				TableName: "players",
				Inserts:   []types.ProductValue{{types.NewUint64(3), types.NewString("carol")}},
			},
		},
	})
	if finalTxID, err := dw.Close(); err != nil {
		t.Fatal(err)
	} else if finalTxID != 3 {
		t.Fatalf("durability final txID = %d, want 3", finalTxID)
	}

	recovered, maxTxID, plan, report, err := OpenAndRecoverWithReport(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 3 {
		t.Fatalf("second recovery maxTxID = %d, want 3", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice", 2: "bob", 3: "carol"})
	if plan.AppendMode != AppendInPlace || plan.SegmentStartTx != 3 || plan.NextTxID != 4 {
		t.Fatalf("second recovery plan = %+v, want append-in-place on replacement tx 3 segment", plan)
	}
	if len(report.DamagedTailSegments) != 0 {
		t.Fatalf("second damaged tail report = %+v, want none after replacement", report.DamagedTailSegments)
	}
}

type recoveryRecord struct {
	txID    uint64
	inserts []types.ProductValue
	deletes []types.ProductValue
}

func buildRecoveryAutoIncrementRegistry(t *testing.T) schema.SchemaRegistry {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "jobs",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindUint64, PrimaryKey: true, AutoIncrement: true},
			{Name: "name", Type: schema.KindString},
		},
	})
	engine, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return engine.Registry()
}

func buildRecoverySignedAutoIncrementRegistry(t *testing.T) schema.SchemaRegistry {
	t.Helper()
	b := schema.NewBuilder()
	b.SchemaVersion(1)
	b.TableDef(schema.TableDefinition{
		Name: "jobs",
		Columns: []schema.ColumnDefinition{
			{Name: "id", Type: schema.KindInt64, PrimaryKey: true, AutoIncrement: true},
			{Name: "name", Type: schema.KindString},
		},
	})
	engine, err := b.Build(schema.EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return engine.Registry()
}

func buildRecoveryCommittedState(t *testing.T, reg schema.SchemaRegistry) *store.CommittedState {
	t.Helper()
	committed := store.NewCommittedState()
	for _, tableID := range reg.Tables() {
		tableSchema, _ := reg.Table(tableID)
		committed.RegisterTable(tableID, store.NewTable(tableSchema))
	}
	return committed
}

func writeRecoverySegment(t *testing.T, root string, reg schema.SchemaRegistry, startTx uint64, records ...recoveryRecord) string {
	t.Helper()
	seg, err := CreateSegment(root, startTx)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range records {
		payload, err := EncodeChangeset(&store.Changeset{
			TxID: types.TxID(rec.txID),
			Tables: map[schema.TableID]*store.TableChangeset{
				0: {
					TableID:   0,
					TableName: "jobs",
					Inserts:   rec.inserts,
					Deletes:   rec.deletes,
				},
			},
		})
		if err != nil {
			_ = seg.Close()
			t.Fatal(err)
		}
		if err := seg.Append(&Record{TxID: rec.txID, RecordType: RecordTypeChangeset, Payload: payload}); err != nil {
			_ = seg.Close()
			t.Fatal(err)
		}
	}
	if err := seg.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, SegmentFileName(startTx))
}

func assertRecoveryRows(t *testing.T, table *store.Table, want map[uint64]string) {
	t.Helper()
	got := make(map[uint64]string, table.RowCount())
	for _, row := range table.Scan() {
		got[row[0].AsUint64()] = row[1].AsString()
	}
	if len(got) != len(want) {
		t.Fatalf("row count = %d, want %d (got=%v)", len(got), len(want), got)
	}
	ids := make([]uint64, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if got[id] != want[id] {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	}
}

func assertSignedRecoveryRows(t *testing.T, table *store.Table, want map[int64]string) {
	t.Helper()
	got := make(map[int64]string, table.RowCount())
	for _, row := range table.Scan() {
		got[row[0].AsInt64()] = row[1].AsString()
	}
	if len(got) != len(want) {
		t.Fatalf("row count = %d, want %d (got=%v)", len(got), len(want), got)
	}
	ids := make([]int64, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		if got[id] != want[id] {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	}
}
