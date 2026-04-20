package subscription

import (
	"reflect"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestColEqTablesSingle(t *testing.T) {
	p := ColEq{Table: 7, Column: 2, Value: types.NewInt64(42)}
	if got := p.Tables(); !reflect.DeepEqual(got, []TableID{7}) {
		t.Fatalf("ColEq.Tables() = %v, want [7]", got)
	}
}

func TestColRangeTablesSingle(t *testing.T) {
	p := ColRange{
		Table:  3,
		Column: 1,
		Lower:  Bound{Value: types.NewInt64(10), Inclusive: true},
		Upper:  Bound{Unbounded: true},
	}
	if got := p.Tables(); !reflect.DeepEqual(got, []TableID{3}) {
		t.Fatalf("ColRange.Tables() = %v, want [3]", got)
	}
}

func TestColNeTablesSingle(t *testing.T) {
	p := ColNe{Table: 4, Column: 1, Value: types.NewInt64(9)}
	if got := p.Tables(); !reflect.DeepEqual(got, []TableID{4}) {
		t.Fatalf("ColNe.Tables() = %v, want [4]", got)
	}
}

func TestAllRowsTablesSingle(t *testing.T) {
	p := AllRows{Table: 9}
	if got := p.Tables(); !reflect.DeepEqual(got, []TableID{9}) {
		t.Fatalf("AllRows.Tables() = %v, want [9]", got)
	}
}

func TestJoinTablesBoth(t *testing.T) {
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	if got := p.Tables(); !reflect.DeepEqual(got, []TableID{1, 2}) {
		t.Fatalf("Join.Tables() = %v, want [1 2]", got)
	}
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
	if got := p.Tables(); !reflect.DeepEqual(got, []TableID{5}) {
		t.Fatalf("And.Tables() same-table = %v, want [5]", got)
	}
}

func TestAndTablesTwoTablesOrdered(t *testing.T) {
	p := And{
		Left:  ColEq{Table: 3, Column: 0, Value: types.NewInt32(1)},
		Right: ColEq{Table: 7, Column: 0, Value: types.NewInt32(2)},
	}
	if got := p.Tables(); !reflect.DeepEqual(got, []TableID{3, 7}) {
		t.Fatalf("And.Tables() two-table = %v, want [3 7]", got)
	}
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
	if got := outer.Tables(); !reflect.DeepEqual(got, []TableID{1, 2}) {
		t.Fatalf("And nested = %v, want [1 2]", got)
	}
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
	if got := outer.Tables(); !reflect.DeepEqual(got, []TableID{1, 2}) {
		t.Fatalf("Or nested = %v, want [1 2]", got)
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
	var _ Predicate = Join{}
}
