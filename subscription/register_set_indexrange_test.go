package subscription

import (
	"iter"
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// Pins the live initial-snapshot index path: indexed ColEq uses IndexSeek,
// indexed ColRange uses IndexRange, and compound And predicates reuse those
// candidate paths while rechecking the full predicate. Unindexed columns,
// nil resolvers, and non-indexable shapes stay on TableScan.

// countingCommitted wraps a mockCommitted and records per-method call counts
// so tests can assert which read path the evaluator took.
type countingCommitted struct {
	inner           *mockCommitted
	tableScanCalls  int
	indexRangeCalls int
	indexSeekCalls  int
	getRowCalls     int
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
	c.getRowCalls++
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

func TestInitialJoinProjectedSideIndexAvoidsNestedTableScans(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewUint64(7)},
			{types.NewUint64(2), types.NewUint64(7)},
			{types.NewUint64(3), types.NewUint64(7)},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(7)},
			{types.NewUint64(11), types.NewUint64(7)},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1}

	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	rows := collectInserts(res.Update)
	wantRows := []types.ProductValue{
		{types.NewUint64(1), types.NewUint64(7)},
		{types.NewUint64(1), types.NewUint64(7)},
		{types.NewUint64(2), types.NewUint64(7)},
		{types.NewUint64(2), types.NewUint64(7)},
		{types.NewUint64(3), types.NewUint64(7)},
		{types.NewUint64(3), types.NewUint64(7)},
	}
	if len(rows) != len(wantRows) {
		t.Fatalf("rows len = %d, want %d", len(rows), len(wantRows))
	}
	for i := range wantRows {
		if !rows[i].Equal(wantRows[i]) {
			t.Fatalf("rows[%d] = %v, want %v", i, rows[i], wantRows[i])
		}
	}
	if view.indexSeekCalls != 2 {
		t.Fatalf("IndexSeek calls = %d, want 2 (one per non-indexed side row)", view.indexSeekCalls)
	}
	if view.tableScanCalls != 2 {
		t.Fatalf("TableScan calls = %d, want 2 (one scan per table, no nested fallback)", view.tableScanCalls)
	}
}

func TestInitialJoinProjectedSideFilterSkipsJoinProbes(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 1)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(7), types.NewString("skip-a")},
			{types.NewUint64(7), types.NewString("keep")},
			{types.NewUint64(7), types.NewString("skip-b")},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(7)},
			{types.NewUint64(11), types.NewUint64(7)},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColEq{Table: 1, Column: 1, Value: types.NewString("keep")},
	}
	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	rows := collectInserts(res.Update)
	if view.indexSeekCalls != 2 {
		t.Fatalf("IndexSeek calls = %d, want 2 (filter seek + one join probe)", view.indexSeekCalls)
	}
	if view.tableScanCalls != 1 {
		t.Fatalf("TableScan calls = %d, want 1 projected scan", view.tableScanCalls)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2 joined projected rows", len(rows))
	}
	for _, row := range rows {
		if !row[1].Equal(types.NewString("keep")) {
			t.Fatalf("projected row = %v, want filter value keep", row)
		}
	}
}

func TestInitialJoinScanSideRangeFilterSkipsProjectedIndexProbes(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64, 2: types.KindUint64}, 2)
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(7), types.NewString("lhs")},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(7), types.NewUint64(5)},
			{types.NewUint64(11), types.NewUint64(7), types.NewUint64(15)},
			{types.NewUint64(12), types.NewUint64(7), types.NewUint64(25)},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColRange{Table: 2, Column: 2,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
		},
	}
	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	rows := collectInserts(res.Update)
	if view.indexRangeCalls != 1 {
		t.Fatalf("IndexRange calls = %d, want 1 scan-side filter range", view.indexRangeCalls)
	}
	if view.indexSeekCalls != 1 {
		t.Fatalf("IndexSeek calls = %d, want 1 projected join probe", view.indexSeekCalls)
	}
	if view.tableScanCalls != 2 {
		t.Fatalf("TableScan calls = %d, want 2 (scan side + projected order pass)", view.tableScanCalls)
	}
	if len(rows) != 1 || !rows[0][1].Equal(types.NewString("lhs")) {
		t.Fatalf("rows = %v, want one projected LHS row", rows)
	}
}

func TestInitialQueryIndexedColEqUsesIndexSeek(t *testing.T) {
	s := testSchema() // table 1 col 0 indexed
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(2), types.NewString("c")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexSeekCalls != 1 {
		t.Fatalf("IndexSeek calls = %d, want 1", view.indexSeekCalls)
	}
	if view.indexRangeCalls != 0 {
		t.Fatalf("IndexRange calls = %d, want 0 for equality path", view.indexRangeCalls)
	}
	if view.tableScanCalls != 0 {
		t.Fatalf("TableScan calls = %d, want 0 (indexed equality path)", view.tableScanCalls)
	}
	if view.getRowCalls != 2 {
		t.Fatalf("GetRow calls = %d, want 2 indexed candidates", view.getRowCalls)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(rows))
	}
}

func TestInitialQueryIndexedColEqCompoundRechecksPredicate(t *testing.T) {
	s := testSchema() // table 1 col 0 indexed
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(2), types.NewString("c")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := And{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)},
		Right: ColEq{Table: 1, Column: 1, Value: types.NewString("b")},
	}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexSeekCalls != 1 {
		t.Fatalf("IndexSeek calls = %d, want 1", view.indexSeekCalls)
	}
	if view.tableScanCalls != 0 {
		t.Fatalf("TableScan calls = %d, want 0 (indexed equality compound path)", view.tableScanCalls)
	}
	if len(rows) != 1 || !rows[0][1].Equal(types.NewString("b")) {
		t.Fatalf("rows = %v, want exactly {2,b}", rows)
	}
}

func TestInitialQueryIndexedColEqCompoundSkipsUnindexedCandidate(t *testing.T) {
	s := testSchema() // table 1 col 0 indexed; col 1 is not indexed
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("b")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(2), types.NewString("c")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := And{
		Left:  ColEq{Table: 1, Column: 1, Value: types.NewString("b")},
		Right: ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)},
	}
	rows := collectInserts(registerColRange(t, mgr, view, pred))
	if view.indexSeekCalls != 1 {
		t.Fatalf("IndexSeek calls = %d, want 1", view.indexSeekCalls)
	}
	if view.tableScanCalls != 0 {
		t.Fatalf("TableScan calls = %d, want 0 (later indexed equality path)", view.tableScanCalls)
	}
	if len(rows) != 1 || !rows[0][1].Equal(types.NewString("b")) {
		t.Fatalf("rows = %v, want exactly {2,b}", rows)
	}
}

// Indexed ColRange uses IndexRange, not TableScan.
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

// Compound And wrapping a ColRange uses IndexRange and rechecks the full
// predicate against indexed candidates.
func TestInitialQueryCompoundAndColRangeUsesIndexRange(t *testing.T) {
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
	if view.indexRangeCalls != 1 {
		t.Fatalf("IndexRange calls = %d, want 1", view.indexRangeCalls)
	}
	if view.tableScanCalls != 0 {
		t.Fatalf("TableScan calls = %d, want 0 (indexed compound path)", view.tableScanCalls)
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

func TestUnregisterSetFinalDeltaIndexedColEqUsesIndexSeek(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(2), types.NewString("c")},
		},
	})
	view := newCountingCommitted(inner)
	mgr := NewManager(s, s)
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, view); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	beforeSeek := view.indexSeekCalls
	beforeTable := view.tableScanCalls
	res, err := mgr.UnregisterSet(types.ConnectionID{1}, 10, view)
	if err != nil {
		t.Fatalf("UnregisterSet = %v", err)
	}
	if view.indexSeekCalls-beforeSeek != 1 {
		t.Fatalf("UnregisterSet IndexSeek delta = %d, want 1", view.indexSeekCalls-beforeSeek)
	}
	if view.tableScanCalls-beforeTable != 0 {
		t.Fatalf("UnregisterSet TableScan delta = %d, want 0 (indexed final-delta)", view.tableScanCalls-beforeTable)
	}
	var deletes []types.ProductValue
	for _, u := range res.Update {
		deletes = append(deletes, u.Deletes...)
	}
	if len(deletes) != 2 {
		t.Fatalf("final-delta deletes len = %d, want 2", len(deletes))
	}
}

func TestIndexedInitialQueryStillReceivesCommitDeltas(t *testing.T) {
	s := testSchema()
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(2), types.NewString("initial")},
		},
	})
	view := newCountingCommitted(inner)
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, view); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	mgr.EvalAndBroadcast(types.TxID(1), simpleChangeset(1, []types.ProductValue{
		{types.NewUint64(2), types.NewString("delta")},
		{types.NewUint64(3), types.NewString("ignored")},
	}, nil), view, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 || len(updates[0].Inserts) != 1 {
		t.Fatalf("fanout updates = %+v, want one matching insert", updates)
	}
	if !updates[0].Inserts[0][1].Equal(types.NewString("delta")) {
		t.Fatalf("insert = %v, want delta row", updates[0].Inserts[0])
	}
}
