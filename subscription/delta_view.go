package subscription

import (
	"fmt"
	"iter"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// DeltaView merges committed state with one transaction's inserted/deleted rows.
// The committed view is borrowed; callers must not retain it across blocking work.
type DeltaView struct {
	committed store.CommittedReadView
	inserts   map[TableID][]types.ProductValue
	deletes   map[TableID][]types.ProductValue
	deltaIdx  DeltaIndexes
}

// DeltaIndexes maps (table, column) → encoded value → positions in the
// corresponding inserts/deletes slice. Positions (int indices) avoid
// double-storing row data.
type DeltaIndexes struct {
	insertIdx map[TableID]map[ColID]map[valueKey][]int
	deleteIdx map[TableID]map[ColID]map[valueKey][]int
}

// NewDeltaView constructs a DeltaView from a changeset and a committed snapshot.
// activeColumns is the set of columns per table that at least one active
// subscription cares about; delta indexes are built only for these columns.
func NewDeltaView(
	committed store.CommittedReadView,
	changeset *store.Changeset,
	activeColumns map[TableID][]ColID,
) *DeltaView {
	dv := acquireDeltaView()
	dv.committed = committed
	if changeset == nil {
		return dv
	}
	for tid, tc := range changeset.Tables {
		if tc == nil {
			continue
		}
		if len(tc.Inserts) > 0 {
			ins := acquireProductValueSlice(len(tc.Inserts))
			ins = append(ins, tc.Inserts...)
			dv.inserts[tid] = ins
		}
		if len(tc.Deletes) > 0 {
			del := acquireProductValueSlice(len(tc.Deletes))
			del = append(del, tc.Deletes...)
			dv.deletes[tid] = del
		}
	}
	for table, cols := range activeColumns {
		dv.buildDeltaIndex(table, cols)
	}
	return dv
}

// Release returns the DeltaView's scratch allocations to the internal pools.
// The DeltaView and any slices returned from it must not be used afterwards.
func (dv *DeltaView) Release() {
	if dv == nil {
		return
	}
	releaseDeltaView(dv)
}

func (dv *DeltaView) buildDeltaIndex(table TableID, cols []ColID) {
	if ins := dv.inserts[table]; len(ins) > 0 {
		dv.deltaIdx.insertIdx[table] = buildDeltaIndexForRows(ins, cols)
	}
	if del := dv.deletes[table]; len(del) > 0 {
		dv.deltaIdx.deleteIdx[table] = buildDeltaIndexForRows(del, cols)
	}
}

func buildDeltaIndexForRows(rows []types.ProductValue, cols []ColID) map[ColID]map[valueKey][]int {
	byCol := acquireTableDeltaIndex()
	for _, col := range cols {
		byVal := acquireValuePositionIndex()
		for i, row := range rows {
			if int(col) >= len(row) {
				continue
			}
			key := encodeValueKey(row[col])
			byVal[key] = append(byVal[key], i)
		}
		byCol[col] = byVal
	}
	return byCol
}

// InsertedRows returns the delta inserts for a table (nil if none).
func (dv *DeltaView) InsertedRows(table TableID) []types.ProductValue {
	return dv.inserts[table]
}

// DeletedRows returns the delta deletes for a table (nil if none).
func (dv *DeltaView) DeletedRows(table TableID) []types.ProductValue {
	return dv.deletes[table]
}

// DeltaIndexScan returns the delta rows whose indexed column equals value.
// inserted=true scans the insert side, false scans the delete side.
// Panics when the requested column does not have a built delta index.
func (dv *DeltaView) DeltaIndexScan(table TableID, col ColID, value Value, inserted bool) []types.ProductValue {
	src := dv.deltaIdx.deleteIdx
	rows := dv.deletes[table]
	if inserted {
		src = dv.deltaIdx.insertIdx
		rows = dv.inserts[table]
	}
	if rows == nil {
		return nil
	}
	byCol, ok := src[table]
	if !ok {
		panic(fmt.Sprintf("subscription: DeltaIndexScan on table %d with no delta indexes", table))
	}
	byVal, ok := byCol[col]
	if !ok {
		panic(fmt.Sprintf("subscription: DeltaIndexScan on table %d col %d with no delta index", table, col))
	}
	positions := byVal[encodeValueKey(value)]
	if len(positions) == 0 {
		return nil
	}
	out := make([]types.ProductValue, 0, len(positions))
	for _, pos := range positions {
		out = append(out, rows[pos])
	}
	return out
}

// CommittedScan delegates to the underlying committed view.
func (dv *DeltaView) CommittedScan(table TableID) iter.Seq2[types.RowID, types.ProductValue] {
	if dv.committed == nil {
		return func(func(types.RowID, types.ProductValue) bool) {}
	}
	return dv.committed.TableScan(table)
}

// CommittedIndexSeek delegates to the underlying committed view.
// Returns nil when there is no committed view attached.
func (dv *DeltaView) CommittedIndexSeek(table TableID, indexID IndexID, key store.IndexKey) []types.RowID {
	if dv.committed == nil {
		return nil
	}
	return dv.committed.IndexSeek(table, indexID, key)
}

// CommittedView exposes the underlying read view for advanced callers that
// need row materialization (store.GetRow).
func (dv *DeltaView) CommittedView() store.CommittedReadView { return dv.committed }
