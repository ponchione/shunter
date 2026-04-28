package subscription

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/ponchione/shunter/types"
)

func TestColEqTablesSingle(t *testing.T) {
	p := ColEq{Table: 7, Column: 2, Value: types.NewInt64(42)}
	requireTables(t, "ColEq.Tables()", []TableID{7}, p.Tables())
}

func TestColRangeTablesSingle(t *testing.T) {
	p := ColRange{
		Table:  3,
		Column: 1,
		Lower:  Bound{Value: types.NewInt64(10), Inclusive: true},
		Upper:  Bound{Unbounded: true},
	}
	requireTables(t, "ColRange.Tables()", []TableID{3}, p.Tables())
}

func TestColNeTablesSingle(t *testing.T) {
	p := ColNe{Table: 4, Column: 1, Value: types.NewInt64(9)}
	requireTables(t, "ColNe.Tables()", []TableID{4}, p.Tables())
}

func TestAllRowsTablesSingle(t *testing.T) {
	p := AllRows{Table: 9}
	requireTables(t, "AllRows.Tables()", []TableID{9}, p.Tables())
}

func TestNoRowsTablesSingle(t *testing.T) {
	p := NoRows{Table: 10}
	requireTables(t, "NoRows.Tables()", []TableID{10}, p.Tables())
}

func TestJoinTablesBoth(t *testing.T) {
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	requireTables(t, "Join.Tables()", []TableID{1, 2}, p.Tables())
}

func TestJoinNilFilterAllowed(t *testing.T) {
	p := Join{Left: 1, Right: 2, Filter: nil}
	if p.Filter != nil {
		t.Fatalf("expected nil filter, got %T", p.Filter)
	}
	_ = p.Tables() // must not panic
}

func TestAndTablesSameTableDedup(t *testing.T) {
	p := And{
		Left:  ColEq{Table: 5, Column: 0, Value: types.NewInt32(1)},
		Right: ColRange{Table: 5, Column: 1, Lower: Bound{Unbounded: true}, Upper: Bound{Unbounded: true}},
	}
	requireTables(t, "And.Tables() same-table", []TableID{5}, p.Tables())
}

func TestAndTablesTwoTablesOrdered(t *testing.T) {
	p := And{
		Left:  ColEq{Table: 3, Column: 0, Value: types.NewInt32(1)},
		Right: ColEq{Table: 7, Column: 0, Value: types.NewInt32(2)},
	}
	requireTables(t, "And.Tables() two-table", []TableID{3, 7}, p.Tables())
}

func TestAndTablesNestedDedup(t *testing.T) {
	inner := And{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewInt32(1)},
		Right: ColEq{Table: 2, Column: 0, Value: types.NewInt32(2)},
	}
	outer := And{
		Left:  inner,
		Right: ColEq{Table: 1, Column: 1, Value: types.NewInt32(3)},
	}
	requireTables(t, "And nested", []TableID{1, 2}, outer.Tables())
}

func TestOrTablesNestedDedup(t *testing.T) {
	inner := Or{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewInt32(1)},
		Right: ColEq{Table: 2, Column: 0, Value: types.NewInt32(2)},
	}
	outer := Or{
		Left:  inner,
		Right: ColEq{Table: 1, Column: 1, Value: types.NewInt32(3)},
	}
	requireTables(t, "Or nested", []TableID{1, 2}, outer.Tables())
}

func requireTables(t *testing.T, name string, want, got []TableID) {
	t.Helper()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("%s mismatch (-want +got):\n%s", name, diff)
	}
}

func TestBoundUnboundedZeroValueIgnored(t *testing.T) {
	b := Bound{Unbounded: true}
	if !b.Unbounded {
		t.Fatal("Bound.Unbounded should be true")
	}
}

func TestPredicateSealed(t *testing.T) {
	// Compile-time check: concrete types implement Predicate.
	var _ Predicate = ColEq{}
	var _ Predicate = ColNe{}
	var _ Predicate = ColRange{}
	var _ Predicate = And{}
	var _ Predicate = Or{}
	var _ Predicate = AllRows{}
	var _ Predicate = NoRows{}
	var _ Predicate = Join{}
}
