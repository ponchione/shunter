package store

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

type recordingStoreObserver struct {
	rows map[string]uint64
}

func (o *recordingStoreObserver) LogStoreSnapshotLeaked(string) {}

func (o *recordingStoreObserver) RecordStoreReadRows(kind string, rows uint64) {
	if o.rows == nil {
		o.rows = make(map[string]uint64)
	}
	o.rows[kind] += rows
}

func (o *recordingStoreObserver) requireRows(t *testing.T, kind string, want uint64) {
	t.Helper()
	if got := o.rows[kind]; got != want {
		t.Fatalf("%s rows = %d, want %d; all rows=%v", kind, got, want, o.rows)
	}
}

func seedStoreReadObservationState(t *testing.T) (*CommittedState, schema.IndexID) {
	t.Helper()
	cs, reg := buildTestState()
	tbl, _ := cs.Table(0)
	for _, row := range []types.ProductValue{
		mkRow(1, "alice"),
		mkRow(2, "bob"),
		mkRow(3, "carol"),
	} {
		if err := tbl.InsertRow(tbl.AllocRowID(), row); err != nil {
			t.Fatal(err)
		}
	}
	ts, _ := reg.Table(0)
	pkIdx, ok := ts.PrimaryIndex()
	if !ok {
		t.Fatal("table 0 has no primary index")
	}
	return cs, pkIdx.ID
}

func TestCommittedSnapshotRecordsStoreReadRowsByKind(t *testing.T) {
	cs, pkIdx := seedStoreReadObservationState(t)
	observer := &recordingStoreObserver{}
	cs.SetObserver(observer)

	snap := cs.Snapshot()
	defer snap.Close()

	for range snap.TableScan(0) {
	}
	_ = snap.IndexSeek(0, pkIdx, NewIndexKey(types.NewUint64(1)))
	for range snap.IndexScan(0, pkIdx, types.NewUint64(2)) {
	}
	for range snap.IndexRange(0, pkIdx, Inclusive(types.NewUint64(1)), Inclusive(types.NewUint64(2))) {
	}

	observer.requireRows(t, StoreReadKindTableScan, 3)
	observer.requireRows(t, StoreReadKindIndexSeek, 1)
	observer.requireRows(t, StoreReadKindIndexScan, 1)
	observer.requireRows(t, StoreReadKindIndexRange, 2)
}

func TestCommittedSnapshotRecordsRowsDeliveredBeforeIteratorStop(t *testing.T) {
	cs, _ := seedStoreReadObservationState(t)
	observer := &recordingStoreObserver{}
	cs.SetObserver(observer)

	snap := cs.Snapshot()
	defer snap.Close()

	for range snap.TableScan(0) {
		break
	}

	observer.requireRows(t, StoreReadKindTableScan, 1)
}
