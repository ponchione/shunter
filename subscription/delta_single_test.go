package subscription

import (
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestMatchRowColEq(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	if !MatchRow(p, 1, types.ProductValue{types.NewUint64(42)}) {
		t.Fatal("expected match")
	}
	if MatchRow(p, 1, types.ProductValue{types.NewUint64(43)}) {
		t.Fatal("should not match")
	}
}

func TestMatchRowOtherTableIsPass(t *testing.T) {
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	if !MatchRow(p, 2, types.ProductValue{types.NewString("irrelevant")}) {
		t.Fatal("predicate on other table should pass through")
	}
}

func TestMatchRowColRangeInclusiveBoundaries(t *testing.T) {
	p := ColRange{
		Table:  1,
		Column: 0,
		Lower:  Bound{Value: types.NewInt64(10), Inclusive: true},
		Upper:  Bound{Value: types.NewInt64(20), Inclusive: true},
	}
	for _, v := range []int64{10, 15, 20} {
		if !MatchRow(p, 1, types.ProductValue{types.NewInt64(v)}) {
			t.Fatalf("inclusive bound miss: %d", v)
		}
	}
	if MatchRow(p, 1, types.ProductValue{types.NewInt64(9)}) {
		t.Fatal("below range should not match")
	}
	if MatchRow(p, 1, types.ProductValue{types.NewInt64(21)}) {
		t.Fatal("above range should not match")
	}
}

func TestMatchRowColRangeExclusive(t *testing.T) {
	p := ColRange{
		Table:  1,
		Column: 0,
		Lower:  Bound{Value: types.NewInt64(10), Inclusive: false},
		Upper:  Bound{Value: types.NewInt64(20), Inclusive: false},
	}
	if MatchRow(p, 1, types.ProductValue{types.NewInt64(10)}) {
		t.Fatal("exclusive lower should not match boundary")
	}
	if MatchRow(p, 1, types.ProductValue{types.NewInt64(20)}) {
		t.Fatal("exclusive upper should not match boundary")
	}
	if !MatchRow(p, 1, types.ProductValue{types.NewInt64(15)}) {
		t.Fatal("middle value should match")
	}
}

func TestMatchRowColNe(t *testing.T) {
	p := ColNe{Table: 1, Column: 0, Value: types.NewUint64(42)}
	if MatchRow(p, 1, types.ProductValue{types.NewUint64(42)}) {
		t.Fatal("equal value should not match")
	}
	if !MatchRow(p, 1, types.ProductValue{types.NewUint64(43)}) {
		t.Fatal("different value should match")
	}
}

func TestMatchRowColNeNull(t *testing.T) {
	p := ColNe{Table: 1, Column: 0, Value: types.NewNull(types.KindUint64)}
	if MatchRow(p, 1, types.ProductValue{types.NewNull(types.KindUint64)}) {
		t.Fatal("null rejected value should not match")
	}
	if !MatchRow(p, 1, types.ProductValue{types.NewUint64(1)}) {
		t.Fatal("non-null value should match != NULL predicate")
	}
}

func TestMatchRowSideColEqColSameSide(t *testing.T) {
	p := ColEqCol{LeftTable: 1, LeftColumn: 0, RightTable: 1, RightColumn: 1}
	if !MatchRowSide(p, 1, 0, types.ProductValue{types.NewUint32(7), types.NewUint32(7)}) {
		t.Fatal("same-side column equality should match equal values")
	}
	if MatchRowSide(p, 1, 0, types.ProductValue{types.NewUint32(7), types.NewUint32(8)}) {
		t.Fatal("same-side column equality should reject different values")
	}
}

func TestMatchJoinPairColEqCol(t *testing.T) {
	p := ColEqCol{LeftTable: 1, LeftColumn: 1, RightTable: 2, RightColumn: 0}
	if !MatchJoinPair(p,
		1, 0, types.ProductValue{types.NewUint32(1), types.NewUint32(9)},
		2, 0, types.ProductValue{types.NewUint32(9), types.NewUint32(2)},
	) {
		t.Fatal("joined pair column equality should match equal values")
	}
	if MatchJoinPair(p,
		1, 0, types.ProductValue{types.NewUint32(1), types.NewUint32(9)},
		2, 0, types.ProductValue{types.NewUint32(8), types.NewUint32(2)},
	) {
		t.Fatal("joined pair column equality should reject different values")
	}
}

func TestMatchJoinPairColNeNull(t *testing.T) {
	p := ColNe{Table: 2, Column: 0, Value: types.NewNull(types.KindUint64)}
	if MatchJoinPair(p,
		1, 0, types.ProductValue{types.NewUint64(9)},
		2, 0, types.ProductValue{types.NewNull(types.KindUint64)},
	) {
		t.Fatal("right-side null rejected value should not match")
	}
	if !MatchJoinPair(p,
		1, 0, types.ProductValue{types.NewUint64(9)},
		2, 0, types.ProductValue{types.NewUint64(10)},
	) {
		t.Fatal("right-side non-null value should match != NULL predicate")
	}
}

func TestMatchJoinPairColEqColSelfJoinAliases(t *testing.T) {
	p := ColEqCol{
		LeftTable:   1,
		LeftColumn:  0,
		LeftAlias:   0,
		RightTable:  1,
		RightColumn: 1,
		RightAlias:  1,
	}
	if !MatchJoinPair(p,
		1, 0, types.ProductValue{types.NewUint32(4), types.NewUint32(99)},
		1, 1, types.ProductValue{types.NewUint32(99), types.NewUint32(4)},
	) {
		t.Fatal("self-join column equality should honor aliases")
	}
	if MatchJoinPair(p,
		1, 0, types.ProductValue{types.NewUint32(4), types.NewUint32(99)},
		1, 1, types.ProductValue{types.NewUint32(99), types.NewUint32(5)},
	) {
		t.Fatal("self-join column equality should reject mismatched aliased value")
	}
}

func TestMatchRowAnd(t *testing.T) {
	p := And{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
		Right: ColEq{Table: 1, Column: 1, Value: types.NewString("a")},
	}
	if !MatchRow(p, 1, types.ProductValue{types.NewUint64(1), types.NewString("a")}) {
		t.Fatal("expected match")
	}
	if MatchRow(p, 1, types.ProductValue{types.NewUint64(1), types.NewString("b")}) {
		t.Fatal("second clause should fail")
	}
}

func TestMatchRowOr(t *testing.T) {
	p := Or{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
		Right: ColEq{Table: 1, Column: 1, Value: types.NewString("b")},
	}
	if !MatchRow(p, 1, types.ProductValue{types.NewUint64(1), types.NewString("a")}) {
		t.Fatal("left clause should match")
	}
	if !MatchRow(p, 1, types.ProductValue{types.NewUint64(2), types.NewString("b")}) {
		t.Fatal("right clause should match")
	}
	if MatchRow(p, 1, types.ProductValue{types.NewUint64(2), types.NewString("a")}) {
		t.Fatal("neither clause should match")
	}
}

func TestMatchRowAllRowsAlways(t *testing.T) {
	p := AllRows{Table: 1}
	if !MatchRow(p, 1, types.ProductValue{types.NewUint64(1)}) {
		t.Fatal("AllRows should always pass")
	}
}

func TestMatchRowNoRowsAlwaysRejects(t *testing.T) {
	p := NoRows{Table: 1}
	if MatchRow(p, 1, types.ProductValue{types.NewUint64(1)}) {
		t.Fatal("NoRows should always reject")
	}
}

func TestEvalSingleTableDeltaNoRowsProducesNoChanges(t *testing.T) {
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(1)}},
		[]types.ProductValue{{types.NewUint64(2)}},
	)
	dv := NewDeltaView(newMockCommitted(), cs, nil)
	inserts, deletes := EvalSingleTableDelta(dv, NoRows{Table: 1}, 1)
	if len(inserts) != 0 || len(deletes) != 0 {
		t.Fatalf("delta = inserts %v deletes %v, want none", inserts, deletes)
	}
}

func TestEvalSingleTableDeltaInserts(t *testing.T) {
	cs := simpleChangeset(1,
		[]types.ProductValue{
			{types.NewUint64(42), types.NewString("match")},
			{types.NewUint64(99), types.NewString("miss")},
		}, nil,
	)
	dv := NewDeltaView(nil, cs, nil)
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	ins, del := EvalSingleTableDelta(dv, p, 1)
	if len(ins) != 1 || len(del) != 0 {
		t.Fatalf("ins/del = %d/%d, want 1/0", len(ins), len(del))
	}
}

func TestEvalSingleTableDeltaDeletes(t *testing.T) {
	cs := simpleChangeset(1, nil,
		[]types.ProductValue{
			{types.NewUint64(42)},
			{types.NewUint64(99)},
		},
	)
	dv := NewDeltaView(nil, cs, nil)
	p := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	ins, del := EvalSingleTableDelta(dv, p, 1)
	if len(ins) != 0 || len(del) != 1 {
		t.Fatalf("ins/del = %d/%d, want 0/1", len(ins), len(del))
	}
}

func TestEvalSingleTableDeltaAllRowsPassThrough(t *testing.T) {
	cs := simpleChangeset(1,
		[]types.ProductValue{
			{types.NewUint64(1)},
			{types.NewUint64(2)},
		},
		[]types.ProductValue{{types.NewUint64(3)}},
	)
	dv := NewDeltaView(nil, cs, nil)
	p := AllRows{Table: 1}
	ins, del := EvalSingleTableDelta(dv, p, 1)
	if len(ins) != 2 || len(del) != 1 {
		t.Fatalf("ins/del = %d/%d, want 2/1", len(ins), len(del))
	}
}

func TestEvalSingleTableDeltaEmpty(t *testing.T) {
	cs := simpleChangeset(1, nil, nil)
	dv := NewDeltaView(nil, cs, nil)
	p := AllRows{Table: 1}
	ins, del := EvalSingleTableDelta(dv, p, 1)
	if len(ins) != 0 || len(del) != 0 {
		t.Fatalf("ins/del should both be empty, got %d/%d", len(ins), len(del))
	}
}
