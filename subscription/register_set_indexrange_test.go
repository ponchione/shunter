package subscription

import (
	"iter"
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// Pins the follow-on queue #1 migration: initialQuery routes a bare ColRange
// on an indexed column through view.IndexRange, and keeps the TableScan
// fallback for every other shape (unindexed column, nil resolver, compound
// predicates). Prerequisite primitive is CommittedSnapshot.IndexRange backed
// by BTreeIndex.SeekBounds (SPEC-001 §7.2 drift fix, 2026-04-22).

// countingCommitted wraps a mockCommitted and records per-method call counts
// so tests can assert which read path the evaluator took.
type countingCommitted struct {
	inner           *mockCommitted
	tableScanCalls  int
	indexRangeCalls int
	indexSeekCalls  int
}

func newCountingCommitted(inner *mockCommitted) *countingCommitted {
	return &countingCommitted{inner: inner}
}

func (c *countingCommitted) TableScan(id TableID) iter.Seq2[types.RowID, types.ProductValue] {
	c.tableScanCalls++
	return c.inner.TableScan(id)
}

func (c *countingCommitted) IndexScan(tableID TableID, indexID IndexID, value types.Value) iter.Seq2[types.RowID, types.ProductValue] {
	return c.inner.IndexScan(tableID, indexID, value)
}

func (c *countingCommitted) IndexRange(tableID TableID, indexID IndexID, low, high store.Bound) iter.Seq2[types.RowID, types.ProductValue] {
	c.indexRangeCalls++
	return c.inner.IndexRange(tableID, indexID, low, high)
}

func (c *countingCommitted) IndexSeek(tableID TableID, indexID IndexID, key store.IndexKey) []types.RowID {
	c.indexSeekCalls++
	return c.inner.IndexSeek(tableID, indexID, key)
}

func (c *countingCommitted) GetRow(tableID TableID, rowID types.RowID) (types.ProductValue, bool) {
	return c.inner.GetRow(tableID, rowID)
}

func (c *countingCommitted) RowCount(tableID TableID) int { return c.inner.RowCount(tableID) }

func (c *countingCommitted) Close() {}

func registerColRange(t *testing.T, mgr *Manager, view store.CommittedReadView, pred Predicate) []SubscriptionUpdate {
	t.Helper()
	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	return res.Update
}

func collectInserts(upd []SubscriptionUpdate) []types.ProductValue {
	var out []types.ProductValue
	for _, u := range upd {
		out = append(out, u.Inserts...)
	}
	return out
}

// 1: Indexed ColRange uses IndexRange, not TableScan.
func TestInitialQueryIndexedColRangeUsesIndexRange(t *testing.T) {
	s := testSchema() // table 1 col 0 indexed
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(3), types.NewString("c")},
			{types.NewUint64(4), types.NewString("d")},
			{types.NewUint64(5), types.NewString("e")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(2), Inclusive: true},
		Upper: Bound{Value: types.NewUint64(4), Inclusive: true}}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexRangeCalls != 1 {
		t.Fatalf("IndexRange calls = %d, want 1", view.indexRangeCalls)
	}
	if view.tableScanCalls != 0 {
		t.Fatalf("TableScan calls = %d, want 0 (indexed path)", view.tableScanCalls)
	}
	if len(rows) != 3 {
		t.Fatalf("rows len = %d, want 3 (2..4 inclusive)", len(rows))
	}
}

// 2: Exclusive bounds pass through correctly.
func TestInitialQueryIndexedColRangeExclusiveBounds(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(3), types.NewString("c")},
			{types.NewUint64(4), types.NewString("d")},
			{types.NewUint64(5), types.NewString("e")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(2)},
		Upper: Bound{Value: types.NewUint64(5)}}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexRangeCalls != 1 {
		t.Fatalf("IndexRange calls = %d, want 1", view.indexRangeCalls)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2 (3,4)", len(rows))
	}
}

// 3: Unbounded-low / bounded-high.
func TestInitialQueryIndexedColRangeUnboundedLow(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(3), types.NewString("c")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := ColRange{Table: 1, Column: 0,
		Lower: Bound{Unbounded: true},
		Upper: Bound{Value: types.NewUint64(2), Inclusive: true}}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexRangeCalls != 1 {
		t.Fatalf("IndexRange calls = %d, want 1", view.indexRangeCalls)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2 (1,2)", len(rows))
	}
}

// 4: Unbounded-high / bounded-low.
func TestInitialQueryIndexedColRangeUnboundedHigh(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(3), types.NewString("c")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(2), Inclusive: true},
		Upper: Bound{Unbounded: true}}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexRangeCalls != 1 {
		t.Fatalf("IndexRange calls = %d, want 1", view.indexRangeCalls)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2 (2,3)", len(rows))
	}
}

// 5: Unindexed column falls back to TableScan + MatchRow.
func TestInitialQueryUnindexedColRangeUsesTableScan(t *testing.T) {
	s := newFakeSchema()
	// Column 1 is declared but not indexed; column 0 is the indexed one.
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindInt32}, 0)
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewInt32(10)},
			{types.NewUint64(2), types.NewInt32(20)},
			{types.NewUint64(3), types.NewInt32(30)},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := ColRange{Table: 1, Column: 1, // unindexed column
		Lower: Bound{Value: types.NewInt32(15), Inclusive: true},
		Upper: Bound{Value: types.NewInt32(25), Inclusive: true}}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexRangeCalls != 0 {
		t.Fatalf("IndexRange calls = %d, want 0 (unindexed path)", view.indexRangeCalls)
	}
	if view.tableScanCalls != 1 {
		t.Fatalf("TableScan calls = %d, want 1 (fallback)", view.tableScanCalls)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1 (value=20)", len(rows))
	}
}

// 6: Nil resolver stays on TableScan even when the column could be indexed.
func TestInitialQueryNilResolverUsesTableScan(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, nil) // no resolver
	pred := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(1), Inclusive: true},
		Upper: Bound{Value: types.NewUint64(2), Inclusive: true}}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexRangeCalls != 0 {
		t.Fatalf("IndexRange calls = %d, want 0 (nil resolver)", view.indexRangeCalls)
	}
	if view.tableScanCalls != 1 {
		t.Fatalf("TableScan calls = %d, want 1 (fallback)", view.tableScanCalls)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
}

// 7: Compound And wrapping a ColRange stays on the TableScan fallback —
// the migration is intentionally narrow to bare ColRange.
func TestInitialQueryCompoundAndStaysOnTableScan(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(3), types.NewString("c")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := And{
		Left: ColRange{Table: 1, Column: 0,
			Lower: Bound{Value: types.NewUint64(1), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(3), Inclusive: true}},
		Right: ColEq{Table: 1, Column: 1, Value: types.NewString("b")},
	}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexRangeCalls != 0 {
		t.Fatalf("IndexRange calls = %d, want 0 (And is not bare ColRange)", view.indexRangeCalls)
	}
	if view.tableScanCalls != 1 {
		t.Fatalf("TableScan calls = %d, want 1 (compound fallback)", view.tableScanCalls)
	}
	if len(rows) != 1 || !rows[0][1].Equal(types.NewString("b")) {
		t.Fatalf("rows = %v, want exactly {2,b}", rows)
	}
}

// 8: Empty-range (low > high) on indexed column still goes through
// IndexRange and yields no rows.
func TestInitialQueryIndexedColRangeEmpty(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(5), Inclusive: true},
		Upper: Bound{Value: types.NewUint64(3), Inclusive: true}}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexRangeCalls != 1 {
		t.Fatalf("IndexRange calls = %d, want 1", view.indexRangeCalls)
	}
	if len(rows) != 0 {
		t.Fatalf("rows len = %d, want 0", len(rows))
	}
}

// 9: UnregisterSet final-delta path also rides on IndexRange (same
// initialQuery dispatch).
func TestUnregisterSetFinalDeltaIndexedColRangeUsesIndexRange(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(3), types.NewString("c")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := ColRange{Table: 1, Column: 0,
		Lower: Bound{Value: types.NewUint64(1), Inclusive: true},
		Upper: Bound{Value: types.NewUint64(3), Inclusive: true}}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, view); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	beforeIndex := view.indexRangeCalls
	beforeTable := view.tableScanCalls
	res, err := mgr.UnregisterSet(types.ConnectionID{1}, 10, view)
	if err != nil {
		t.Fatalf("UnregisterSet = %v", err)
	}
	if view.indexRangeCalls-beforeIndex != 1 {
		t.Fatalf("UnregisterSet IndexRange delta = %d, want 1", view.indexRangeCalls-beforeIndex)
	}
	if view.tableScanCalls-beforeTable != 0 {
		t.Fatalf("UnregisterSet TableScan delta = %d, want 0 (indexed final-delta)", view.tableScanCalls-beforeTable)
	}
	var deletes []types.ProductValue
	for _, u := range res.Update {
		deletes = append(deletes, u.Deletes...)
	}
	if len(deletes) != 3 {
		t.Fatalf("final-delta deletes len = %d, want 3", len(deletes))
	}
}
