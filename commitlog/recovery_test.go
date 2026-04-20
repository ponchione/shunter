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
	if err := writer.CreateSnapshot(committed, 2); err != nil {
		t.Fatal(err)
	}

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

func TestOpenAndRecoverDetailedSnapshotReplayIgnoresExplicitAutoincrementRowsWhenRestoringSequence(t *testing.T) {
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
	if err := writer.CreateSnapshot(committed, 2); err != nil {
		t.Fatal(err)
	}
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

	tx := store.NewTransaction(recovered, reg)
	rowID, err := tx.Insert(0, types.ProductValue{types.NewUint64(0), types.NewString("post-recovery-auto")})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := tx.GetRow(0, rowID)
	if !ok {
		t.Fatal("post-recovery row missing from transaction view")
	}
	if got := row[0].AsUint64(); got != 3 {
		t.Fatalf("post-recovery autoincrement value = %d, want 3", got)
	}
	if seq, has := recoveredJobs.SequenceValue(); !has || seq != 4 {
		t.Fatalf("SequenceValue after post-recovery insert = (%d, %v), want (4, true)", seq, has)
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
	if err := writer.CreateSnapshot(base, 5); err != nil {
		t.Fatal(err)
	}
	if err := writer.CreateSnapshot(base, 6); err != nil {
		t.Fatal(err)
	}
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
	if err := writer.CreateSnapshot(committed, 5); err != nil {
		t.Fatal(err)
	}

	recovered, maxTxID, err := OpenAndRecover(root, reg)
	if err != nil {
		t.Fatal(err)
	}
	if maxTxID != 5 {
		t.Fatalf("maxTxID = %d, want 5", maxTxID)
	}
	assertReplayPlayerRows(t, recovered, map[uint64]string{1: "alice"})
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
