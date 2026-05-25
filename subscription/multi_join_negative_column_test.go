package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestMultiJoinNegativeColumnRejects(t *testing.T) {
	relations := []MultiJoinRelation{
		{Table: 1},
		{Table: 2},
		{Table: 3},
	}
	tuple := []types.ProductValue{
		{types.NewUint32(7)},
		{types.NewUint32(7)},
		{types.NewUint32(9)},
	}

	if value, ok := multiJoinConditionColumnValue(tuple, MultiJoinColumnRef{Relation: 0, Table: 1, Column: -1}); ok {
		t.Fatalf("negative condition column returned %v, want no value", value)
	}

	predicates := []Predicate{
		ColEq{Table: 1, Column: -1, Value: types.NewUint32(7)},
		ColNe{Table: 2, Column: -1, Value: types.NewUint32(7)},
		ColRange{
			Table:  1,
			Column: -1,
			Lower:  Bound{Value: types.NewUint32(1), Inclusive: true},
			Upper:  Bound{Value: types.NewUint32(9), Inclusive: true},
		},
		ColEqCol{LeftTable: 1, LeftColumn: -1, RightTable: 2, RightColumn: 0},
		ColEqCol{LeftTable: 1, LeftColumn: 0, RightTable: 2, RightColumn: -1},
	}
	for _, pred := range predicates {
		if matchMultiJoinTuple(pred, relations, tuple) {
			t.Fatalf("%T with negative column matched; want reject", pred)
		}
	}
}
