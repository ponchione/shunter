package subscription

import (
	"iter"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// mockCommitted is a minimal in-memory CommittedReadView for tests. It does
// not enforce schema; tests declare the rows and the (table, indexID) →
// column mapping they care about.
type mockCommitted struct {
	rows map[TableID]map[types.RowID]types.ProductValue
	// idxCol maps (tableID, indexID) → column index for single-column
	// equality index lookups used by join delta tests.
	idxCol map[indexRef]int
}

type indexRef struct {
	Table TableID
	Index IndexID
}

func newMockCommitted() *mockCommitted {
	return &mockCommitted{
		rows:   make(map[TableID]map[types.RowID]types.ProductValue),
		idxCol: make(map[indexRef]int),
	}
}

func (m *mockCommitted) addRow(table TableID, id types.RowID, row types.ProductValue) {
	tbl, ok := m.rows[table]
	if !ok {
		tbl = make(map[types.RowID]types.ProductValue)
		m.rows[table] = tbl
	}
	tbl[id] = row
}

func (m *mockCommitted) setIndex(table TableID, index IndexID, col int) {
	m.idxCol[indexRef{Table: table, Index: index}] = col
}

func (m *mockCommitted) TableScan(id TableID) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid, r := range m.rows[id] {
			if !yield(rid, r) {
				return
			}
		}
	}
}

func (m *mockCommitted) IndexSeek(tableID TableID, indexID IndexID, key store.IndexKey) []types.RowID {
	col, ok := m.idxCol[indexRef{Table: tableID, Index: indexID}]
	if !ok || key.Len() != 1 {
		return nil
	}
	want := key.Part(0)
	var out []types.RowID
	for rid, row := range m.rows[tableID] {
		if col >= len(row) {
			continue
		}
		if row[col].Equal(want) {
			out = append(out, rid)
		}
	}
	return out
}

func (m *mockCommitted) IndexScan(tableID TableID, indexID IndexID, value types.Value) iter.Seq2[types.RowID, types.ProductValue] {
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for _, rid := range m.IndexSeek(tableID, indexID, store.NewIndexKey(value)) {
			row, ok := m.GetRow(tableID, rid)
			if !ok {
				continue
			}
			if !yield(rid, row) {
				return
			}
		}
	}
}

func (m *mockCommitted) IndexRange(tableID TableID, indexID IndexID, low, high store.Bound) iter.Seq2[types.RowID, types.ProductValue] {
	col, ok := m.idxCol[indexRef{Table: tableID, Index: indexID}]
	if !ok {
		return func(yield func(types.RowID, types.ProductValue) bool) {}
	}
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid, row := range m.rows[tableID] {
			if col >= len(row) {
				continue
			}
			v := row[col]
			if !low.Unbounded {
				c := v.Compare(low.Value)
				if low.Inclusive {
					if c < 0 {
						continue
					}
				} else if c <= 0 {
					continue
				}
			}
			if !high.Unbounded {
				c := v.Compare(high.Value)
				if high.Inclusive {
					if c > 0 {
						continue
					}
				} else if c >= 0 {
					continue
				}
			}
			if !yield(rid, row) {
				return
			}
		}
	}
}

func (m *mockCommitted) GetRow(tableID TableID, rowID types.RowID) (types.ProductValue, bool) {
	tbl, ok := m.rows[tableID]
	if !ok {
		return nil, false
	}
	r, ok := tbl[rowID]
	return r, ok
}

func (m *mockCommitted) RowCount(tableID TableID) int { return len(m.rows[tableID]) }

func (m *mockCommitted) Close() {}

// mockResolver satisfies IndexResolver for test join evaluations.
type mockResolver struct {
	idx map[indexRef]bool
	// colToIndex maps (table, col) → indexID.
	colToIndex map[struct {
		T TableID
		C ColID
	}]IndexID
}

func newMockResolver() *mockResolver {
	return &mockResolver{
		idx: map[indexRef]bool{},
		colToIndex: map[struct {
			T TableID
			C ColID
		}]IndexID{},
	}
}

func (r *mockResolver) register(table TableID, col ColID, index IndexID) {
	r.colToIndex[struct {
		T TableID
		C ColID
	}{table, col}] = index
}

func (r *mockResolver) IndexIDForColumn(table TableID, col ColID) (IndexID, bool) {
	id, ok := r.colToIndex[struct {
		T TableID
		C ColID
	}{table, col}]
	return id, ok
}
