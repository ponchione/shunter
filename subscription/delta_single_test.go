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
