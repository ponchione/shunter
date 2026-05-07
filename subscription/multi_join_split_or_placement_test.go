package subscription

import (
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func splitOrLocalFilterMultiJoinPredicate() MultiJoin {
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = Or{
		Left: ColEq{
			Table:  1,
			Column: 0,
			Alias:  0,
			Value:  types.NewUint64(7),
		},
		Right: ColRange{
			Table:  2,
			Column: 0,
			Alias:  1,
			Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
			Upper:  Bound{Unbounded: true},
		},
	}
	return pred
}

func splitOrMultiHopFilterMultiJoinPredicate() MultiJoin {
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = Or{
		Left: ColEq{
			Table:  1,
			Column: 0,
			Alias:  0,
			Value:  types.NewUint64(7),
		},
		Right: ColRange{
			Table:  3,
			Column: 0,
			Alias:  2,
			Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
			Upper:  Bound{Unbounded: true},
		},
	}
	return pred
}

func splitOrAllRemoteRangeMultiJoinPredicate() MultiJoin {
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = And{
		Left: AllRows{Table: 1},
		Right: Or{
			Left: ColRange{
				Table:  2,
				Column: 0,
				Alias:  1,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
			Right: ColNe{
				Table:  3,
				Column: 0,
				Alias:  2,
				Value:  types.NewUint64(7),
			},
		},
	}
	return pred
}

func splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 0, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  3,
				Column: 0,
				Alias:  2,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func fourTableDualIndexedMultiJoinTestSchema() *fakeSchema {
	s := dualIndexedMultiJoinTestSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(4, cols, 0, 1)
	return s
}

func splitOrLongNonKeyPreservingMultiHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
			{Table: 4, Alias: 3},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 0, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 2, Table: 3, Column: 0, Alias: 2},
				Right: MultiJoinColumnRef{Relation: 3, Table: 4, Column: 1, Alias: 3},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  4,
				Column: 0,
				Alias:  3,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrThreeHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
			{Table: 4, Alias: 3},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 0, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 2, Table: 3, Column: 0, Alias: 2},
				Right: MultiJoinColumnRef{Relation: 3, Table: 4, Column: 1, Alias: 3},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  4,
				Column: 0,
				Alias:  3,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func path3MultiJoinTestSchema() *fakeSchema {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols, 1)
	s.addTable(3, cols, 1)
	s.addTable(4, cols, 1)
	return s
}

func fiveTableDualIndexedMultiJoinTestSchema() *fakeSchema {
	s := fourTableDualIndexedMultiJoinTestSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(5, cols, 0, 1)
	return s
}

func sixTableDualIndexedMultiJoinTestSchema() *fakeSchema {
	s := fiveTableDualIndexedMultiJoinTestSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(6, cols, 0, 1)
	return s
}

func sevenTableDualIndexedMultiJoinTestSchema() *fakeSchema {
	s := sixTableDualIndexedMultiJoinTestSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(7, cols, 0, 1)
	return s
}

func eightTableDualIndexedMultiJoinTestSchema() *fakeSchema {
	s := sevenTableDualIndexedMultiJoinTestSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(8, cols, 0, 1)
	return s
}

func nineTableDualIndexedMultiJoinTestSchema() *fakeSchema {
	s := eightTableDualIndexedMultiJoinTestSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(9, cols, 0, 1)
	return s
}

func tenTableDualIndexedMultiJoinTestSchema() *fakeSchema {
	s := nineTableDualIndexedMultiJoinTestSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(10, cols, 0, 1)
	return s
}

func mustJoinPathTraversalEdge(t *testing.T, tables []TableID, fromCols, toCols []ColID, rhsFilterCol ColID) joinPathTraversalEdge {
	t.Helper()
	edge, ok := newJoinPathTraversalEdge(tables, fromCols, toCols, rhsFilterCol)
	if !ok {
		t.Fatalf("invalid test path edge: tables=%v from=%v to=%v", tables, fromCols, toCols)
	}
	return edge
}

func splitOrFourHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
			{Table: 4, Alias: 3},
			{Table: 5, Alias: 4},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 0, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 2, Table: 3, Column: 0, Alias: 2},
				Right: MultiJoinColumnRef{Relation: 3, Table: 4, Column: 1, Alias: 3},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 3, Table: 4, Column: 0, Alias: 3},
				Right: MultiJoinColumnRef{Relation: 4, Table: 5, Column: 1, Alias: 4},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  5,
				Column: 0,
				Alias:  4,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrFiveHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
			{Table: 4, Alias: 3},
			{Table: 5, Alias: 4},
			{Table: 6, Alias: 5},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 0, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 2, Table: 3, Column: 0, Alias: 2},
				Right: MultiJoinColumnRef{Relation: 3, Table: 4, Column: 1, Alias: 3},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 3, Table: 4, Column: 0, Alias: 3},
				Right: MultiJoinColumnRef{Relation: 4, Table: 5, Column: 1, Alias: 4},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 4, Table: 5, Column: 0, Alias: 4},
				Right: MultiJoinColumnRef{Relation: 5, Table: 6, Column: 1, Alias: 5},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  6,
				Column: 0,
				Alias:  5,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrSixHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
			{Table: 4, Alias: 3},
			{Table: 5, Alias: 4},
			{Table: 6, Alias: 5},
			{Table: 7, Alias: 6},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 0, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 2, Table: 3, Column: 0, Alias: 2},
				Right: MultiJoinColumnRef{Relation: 3, Table: 4, Column: 1, Alias: 3},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 3, Table: 4, Column: 0, Alias: 3},
				Right: MultiJoinColumnRef{Relation: 4, Table: 5, Column: 1, Alias: 4},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 4, Table: 5, Column: 0, Alias: 4},
				Right: MultiJoinColumnRef{Relation: 5, Table: 6, Column: 1, Alias: 5},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 5, Table: 6, Column: 0, Alias: 5},
				Right: MultiJoinColumnRef{Relation: 6, Table: 7, Column: 1, Alias: 6},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  7,
				Column: 0,
				Alias:  6,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrSevenHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
			{Table: 4, Alias: 3},
			{Table: 5, Alias: 4},
			{Table: 6, Alias: 5},
			{Table: 7, Alias: 6},
			{Table: 8, Alias: 7},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 0, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 2, Table: 3, Column: 0, Alias: 2},
				Right: MultiJoinColumnRef{Relation: 3, Table: 4, Column: 1, Alias: 3},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 3, Table: 4, Column: 0, Alias: 3},
				Right: MultiJoinColumnRef{Relation: 4, Table: 5, Column: 1, Alias: 4},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 4, Table: 5, Column: 0, Alias: 4},
				Right: MultiJoinColumnRef{Relation: 5, Table: 6, Column: 1, Alias: 5},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 5, Table: 6, Column: 0, Alias: 5},
				Right: MultiJoinColumnRef{Relation: 6, Table: 7, Column: 1, Alias: 6},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 6, Table: 7, Column: 0, Alias: 6},
				Right: MultiJoinColumnRef{Relation: 7, Table: 8, Column: 1, Alias: 7},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  8,
				Column: 0,
				Alias:  7,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrEightHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
			{Table: 4, Alias: 3},
			{Table: 5, Alias: 4},
			{Table: 6, Alias: 5},
			{Table: 7, Alias: 6},
			{Table: 8, Alias: 7},
			{Table: 9, Alias: 8},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 0, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 2, Table: 3, Column: 0, Alias: 2},
				Right: MultiJoinColumnRef{Relation: 3, Table: 4, Column: 1, Alias: 3},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 3, Table: 4, Column: 0, Alias: 3},
				Right: MultiJoinColumnRef{Relation: 4, Table: 5, Column: 1, Alias: 4},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 4, Table: 5, Column: 0, Alias: 4},
				Right: MultiJoinColumnRef{Relation: 5, Table: 6, Column: 1, Alias: 5},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 5, Table: 6, Column: 0, Alias: 5},
				Right: MultiJoinColumnRef{Relation: 6, Table: 7, Column: 1, Alias: 6},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 6, Table: 7, Column: 0, Alias: 6},
				Right: MultiJoinColumnRef{Relation: 7, Table: 8, Column: 1, Alias: 7},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 7, Table: 8, Column: 0, Alias: 7},
				Right: MultiJoinColumnRef{Relation: 8, Table: 9, Column: 1, Alias: 8},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  9,
				Column: 0,
				Alias:  8,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrNineHopFilterMultiJoinPredicate() MultiJoin {
	relations := make([]MultiJoinRelation, 10)
	for i := range relations {
		relations[i] = MultiJoinRelation{Table: TableID(i + 1), Alias: uint8(i)}
	}
	conditions := make([]MultiJoinCondition, 9)
	conditions[0] = MultiJoinCondition{
		Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
		Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
	}
	for i := 1; i < len(conditions); i++ {
		conditions[i] = MultiJoinCondition{
			Left:  MultiJoinColumnRef{Relation: i, Table: TableID(i + 1), Column: 0, Alias: uint8(i)},
			Right: MultiJoinColumnRef{Relation: i + 1, Table: TableID(i + 2), Column: 1, Alias: uint8(i + 1)},
		}
	}
	return MultiJoin{
		Relations:         relations,
		Conditions:        conditions,
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  10,
				Column: 0,
				Alias:  9,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 1, Alias: 1},
			{Table: 2, Alias: 2},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 2, Table: 2, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 1, Column: 1, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 2, Column: 1, Alias: 2},
			},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: ColEq{
				Table:  1,
				Column: 0,
				Alias:  0,
				Value:  types.NewUint64(7),
			},
			Right: ColRange{
				Table:  1,
				Column: 0,
				Alias:  1,
				Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper:  Bound{Unbounded: true},
			},
		},
	}
}

func splitOrBranchLocalConnectorMultiJoinPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
		},
		ProjectedRelation: 0,
		Filter: Or{
			Left: And{
				Left: ColEq{
					Table:  1,
					Column: 0,
					Alias:  0,
					Value:  types.NewUint64(7),
				},
				Right: ColEqCol{
					LeftTable:   1,
					LeftColumn:  1,
					LeftAlias:   0,
					RightTable:  2,
					RightColumn: 1,
					RightAlias:  1,
				},
			},
			Right: And{
				Left: ColRange{
					Table:  3,
					Column: 0,
					Alias:  2,
					Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
					Upper:  Bound{Unbounded: true},
				},
				Right: ColEqCol{
					LeftTable:   2,
					LeftColumn:  1,
					LeftAlias:   1,
					RightTable:  3,
					RightColumn: 1,
					RightAlias:  2,
				},
			},
		},
	}
}

func TestMultiJoinPlacementUsesSplitOrLocalFilterEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrLocalFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left split-OR local value placement = %v, want [%v]", got, hash)
	}
	leftRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(leftRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left split-OR range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(2, 0, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right split-OR local range placement = %v, want [%v]", got, hash)
	}
	rightValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(rightValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right split-OR value edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.JoinEdge.exists[leftRangeEdge]; len(got) != 0 {
		t.Fatalf("left split-OR broad existence candidates = %v, want none", got)
	}
	if got := idx.JoinEdge.exists[rightValueEdge]; len(got) != 0 {
		t.Fatalf("right split-OR broad existence candidates = %v, want none", got)
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for split-OR filter placement", table, got)
		}
	}
	if got := idx.Table.Lookup(3); len(got) != 0 {
		t.Fatalf("TableIndex[3] = %v, want existing condition-edge placement", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrMultiHopUsesTransitiveEndpointAndMiddleRelationEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left endpoint split-OR local value placement = %v, want [%v]", got, hash)
	}
	leftEndpointRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(leftEndpointRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left endpoint split-OR transitive range edge placement = %v, want [%v]", got, hash)
	}
	middleValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(middleValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("middle split-OR value edge placement = %v, want [%v]", got, hash)
	}
	middleRangeEdge := JoinEdge{LHSTable: 2, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(middleRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("middle split-OR range edge placement = %v, want [%v]", got, hash)
	}
	rightEndpointValueEdge := JoinEdge{LHSTable: 3, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(rightEndpointValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right endpoint split-OR transitive value edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(3, 0, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right endpoint split-OR local range placement = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("broad condition existence edges = %+v, want none for covered split-OR placement", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrUsesBranchLocalConnectorFilterEdges(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrBranchLocalConnectorMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	valueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(valueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("branch-local connector value edge = %v, want [%v]", got, hash)
	}
	rangeEdge := JoinEdge{LHSTable: 2, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(rangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("branch-local connector range edge = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("branch-local connector existence fallback = %+v, want none", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex[2] = %v, want empty for covered middle relation", got)
	}
	for _, table := range []TableID{1, 3} {
		if got := idx.Table.Lookup(table); len(got) != 1 || got[0] != hash {
			t.Fatalf("TableIndex[%d] = %v, want fallback [%v]", table, got, hash)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrBranchLocalConnectorPrunesMismatch(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrBranchLocalConnectorMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(200), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched branch-local connector candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("value branch-local connector candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("range branch-local connector candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrBranchLocalConnectorUsesDeltaRows(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrBranchLocalConnectorMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changedRows := []types.ProductValue{{types.NewUint64(200), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 2, changedRows, noOverlap, add)
	if len(candidates) != 0 {
		t.Fatalf("non-overlapping branch-local connector candidates = %v, want empty", candidates)
	}

	valueOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 2, changedRows, valueOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("value-overlap branch-local connector candidates = %v, want only %v", candidates, hash)
	}

	rangeOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			3: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 2, changedRows, rangeOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("range-overlap branch-local connector candidates = %v, want only %v", candidates, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			3: {Deletes: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 2, changedRows, deleteOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("delete-overlap branch-local connector candidates = %v, want only %v", candidates, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrMultiHopEndpointPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	leftMismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched left endpoint multi-hop candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("remote-filter left endpoint multi-hop candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		2: {{types.NewUint64(100), types.NewUint64(20)}},
	})
	rightMismatch := []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 3, rightMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched right endpoint multi-hop candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 3, rightMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("remote-filter right endpoint multi-hop candidates = %v, want [%v]", got, hash)
	}
}

func TestMultiJoinPlacementAndWrappedSplitOrAllRemoteRangeBranchesUseEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrAllRemoteRangeMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	remoteRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(remoteRangeEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("all-remote range branch edge = %v, want [%v]", got, hash)
	}
	remoteNeEdge := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(remoteNeEdge, types.NewUint64(6)); len(got) != 1 || got[0] != hash {
		t.Fatalf("all-remote ColNe lower branch edge = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(remoteNeEdge, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("all-remote ColNe rejected branch edge = %v, want empty", got)
	}
	if got := idx.JoinRangeEdge.Lookup(remoteNeEdge, types.NewUint64(8)); len(got) != 1 || got[0] != hash {
		t.Fatalf("all-remote ColNe upper branch edge = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("endpoint-local value placement = %v, want empty for all-remote branches", got)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("all-remote branch existence fallback = %+v, want none", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for all-remote split-OR placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementAndWrappedSplitOrAllRemoteRangeBranchesFallBackWhenPartiallyCovered(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols, 1)
	s.addTable(3, cols)
	idx := NewPruningIndexes()
	pred := splitOrAllRemoteRangeMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	coveredBranchEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(coveredBranchEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("partial all-remote range edge = %v, want empty when another branch is uncovered", got)
	}
	uncoveredBranchEdge := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(uncoveredBranchEdge, types.NewUint64(8)); len(got) != 0 {
		t.Fatalf("unindexed all-remote ColNe edge = %v, want empty", got)
	}
	conditionEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[conditionEdge][hash]; !ok {
		t.Fatalf("condition-edge fallback missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want condition-edge fallback", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinAndWrappedSplitOrAllRemoteRangeBranchesPruneMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrAllRemoteRangeMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(40), types.NewUint64(20)}},
		3: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("mismatched all-remote split-OR candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(60), types.NewUint64(20)}},
		3: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("range all-remote split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(40), types.NewUint64(20)}},
		3: {{types.NewUint64(8), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("ColNe all-remote split-OR candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinAndWrappedSplitOrAllRemoteRangeBranchesUseSameTransactionRows(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrAllRemoteRangeMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 1, changed, rejected, add)
	if len(candidates) != 0 {
		t.Fatalf("rejected same-tx all-remote split-OR candidates = %v, want empty", candidates)
	}

	rangeOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 1, changed, rangeOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("range same-tx all-remote split-OR candidates = %v, want only %v", candidates, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			3: {Deletes: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
		},
	}
	clear(candidates)
	collectJoinFilterDeltaCandidates(idx, 1, changed, deleteOverlap, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("ColNe delete same-tx all-remote split-OR candidates = %v, want only %v", candidates, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrSameTransactionFilterEdges(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := splitOrLocalFilterMultiJoinPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{16},
		QueryID:    160,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var hash QueryHash
	for h := range mgr.registry.byHash {
		hash = h
	}
	committed := buildMockCommitted(s, nil)
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping split-OR same-tx filter-edge candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping split-OR same-tx filter-edge candidates = %v, want only %v", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrSameTransactionTransitiveFilterEdges(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := splitOrMultiHopFilterMultiJoinPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{17},
		QueryID:    170,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var hash QueryHash
	for h := range mgr.registry.byHash {
		hash = h
	}
	committed := buildMockCommitted(s, nil)
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping transitive split-OR same-tx candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping transitive split-OR same-tx candidates = %v, want only %v", got, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Deletes: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Deletes: []types.ProductValue{{types.NewUint64(60), types.NewUint64(20)}}},
		},
	}
	got = mgr.collectCandidatesInto(deleteOverlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping transitive split-OR same-tx delete candidates = %v, want only %v", got, hash)
	}
}

func TestMultiJoinPlacementSplitOrRepeatedAliasUsesSelfAndMiddleRelationEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR value placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR range placement = %v, want [%v]", got, hash)
	}
	selfEdge := JoinEdge{LHSTable: 1, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(selfEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR self value edge = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(selfEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR self range edge = %v, want [%v]", got, hash)
	}
	middleValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(middleValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR middle value edge = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(middleValueEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("repeated-alias split-OR middle range edge = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("repeated-alias split-OR existence edges = %+v, want none", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for repeated-alias split-OR placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrRepeatedAliasPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(40), types.NewUint64(20)}},
		2: {{types.NewUint64(100), types.NewUint64(20)}},
	})

	mismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched repeated-alias split-OR table candidates = %v, want empty", got)
	}

	localMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(99)}}
	got := CollectCandidatesForTable(idx, 1, localMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("local repeated-alias split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	edgeMatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 1, edgeMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("self-edge repeated-alias split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	middleMismatch := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, middleMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched repeated-alias split-OR middle candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, middleMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("value-edge repeated-alias split-OR middle candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, middleMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("range-edge repeated-alias split-OR middle candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrRepeatedAliasSameTransactionRows(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{20},
		QueryID:    200,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var hash QueryHash
	for h := range mgr.registry.byHash {
		hash = h
	}
	committed := buildMockCommitted(s, nil)
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{
				{types.NewUint64(8), types.NewUint64(20)},
				{types.NewUint64(40), types.NewUint64(20)},
			}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping repeated-alias split-OR same-tx candidates = %v, want empty", got)
	}

	selfOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{
				{types.NewUint64(8), types.NewUint64(20)},
				{types.NewUint64(60), types.NewUint64(20)},
			}},
		},
	}
	got := mgr.collectCandidatesInto(selfOverlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("self-overlap repeated-alias split-OR same-tx candidates = %v, want only %v", got, hash)
	}

	middleOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}},
		},
	}
	got = mgr.collectCandidatesInto(middleOverlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("middle-overlap repeated-alias split-OR same-tx candidates = %v, want only %v", got, hash)
	}
}

func TestMultiJoinPlacementSplitOrRepeatedAliasUsesConditionEdgesWhenSelfEdgeUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols)
	s.addTable(2, cols, 1)
	idx := NewPruningIndexes()
	pred := splitOrRepeatedAliasMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	selfEdge := JoinEdge{LHSTable: 1, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(selfEdge, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("unindexed repeated-alias self value edge = %v, want empty", got)
	}
	if got := idx.JoinRangeEdge.Lookup(selfEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("unindexed repeated-alias self range edge = %v, want empty", got)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial repeated-alias local value placement = %v, want empty", got)
	}
	conditionEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[conditionEdge][hash]; !ok {
		t.Fatalf("condition fallback edge missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want condition-edge placement", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[2] = %v, want fallback [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrNonKeyPreservingMultiHopUsesPathEdges(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftEndpointRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(leftEndpointRangeEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("non-key-preserving transitive range edge placement = %v, want empty", got)
	}
	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3},
		[]ColID{1, 0},
		[]ColID{1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("non-key-preserving path range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("endpoint local placement = %v, want [%v]", got, hash)
	}
	leftConditionEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[leftConditionEdge][hash]; ok {
		t.Fatalf("fallback condition edge present: %+v, want none", idx.JoinEdge.exists)
	}
	rightPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{3, 2, 1},
		[]ColID{1, 1},
		[]ColID{0, 1},
		0,
	)
	if got := idx.joinPathEdge.Lookup(rightPathEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("non-key-preserving path value edge placement = %v, want [%v]", got, hash)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for non-key-preserving path placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrNonKeyPreservingPathEdgesPruneMismatch(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
	})
	leftMismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched non-key-preserving path left candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(60), types.NewUint64(30)}},
	})
	got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching non-key-preserving path left candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		2: {{types.NewUint64(30), types.NewUint64(20)}},
	})
	rightMismatch := []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}
	if got := CollectCandidatesForTable(idx, 3, rightMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched non-key-preserving path right candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
		2: {{types.NewUint64(30), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 3, rightMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching non-key-preserving path right candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrNonKeyPreservingPathEdgesUseSameTransactionRows(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{21},
		QueryID:    210,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var hash QueryHash
	for h := range mgr.registry.byHash {
		hash = h
	}
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, buildMockCommitted(s, nil), scratch); len(got) != 0 {
		t.Fatalf("non-overlapping same-tx path candidates = %v, want empty", got)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(30)}}},
		},
	}
	got := mgr.collectCandidatesInto(allChangedOverlap, buildMockCommitted(s, nil), scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("all-changed same-tx path candidates = %v, want only %v", got, hash)
	}

	allChangedDeleteOverlap := &store.Changeset{
		TxID: 5,
		Tables: map[TableID]*store.TableChangeset{
			1: {Deletes: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Deletes: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Deletes: []types.ProductValue{{types.NewUint64(60), types.NewUint64(30)}}},
		},
	}
	got = mgr.collectCandidatesInto(allChangedDeleteOverlap, buildMockCommitted(s, nil), scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("all-changed same-tx delete path candidates = %v, want only %v", got, hash)
	}

	midCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
	})
	rhsChangedOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(30)}}},
		},
	}
	got = mgr.collectCandidatesInto(rhsChangedOverlap, midCommitted, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("rhs-changed same-tx path candidates = %v, want only %v", got, hash)
	}

	rhsCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(60), types.NewUint64(30)}},
	})
	midChangedOverlap := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
		},
	}
	got = mgr.collectCandidatesInto(midChangedOverlap, rhsCommitted, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("mid-changed same-tx path candidates = %v, want only %v", got, hash)
	}
}

func TestMultiJoinPlacementSplitOrThreeHopUsesPath3Edges(t *testing.T) {
	s := path3MultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrThreeHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	pathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4},
		[]ColID{1, 0, 0},
		[]ColID{1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(pathEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("three-hop path range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("three-hop endpoint local placement = %v, want [%v]", got, hash)
	}
	shortPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 4},
		[]ColID{1, 0},
		[]ColID{1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(shortPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("two-hop path edge placement = %v, want empty for three-hop path", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want empty for covered three-hop path", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrThreeHopPathEdgesUseCommittedRows(t *testing.T) {
	s := path3MultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrThreeHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(40), types.NewUint64(40)}},
	})
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("mismatched three-hop path candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(60), types.NewUint64(40)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching three-hop path candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrThreeHopPathEdgesUseSameTransactionRows(t *testing.T) {
	s := path3MultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrThreeHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(40)}}},
		},
	}
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rejected, nil, nil, add)
	if len(candidates) != 0 {
		t.Fatalf("rejected same-tx three-hop path candidates = %v, want empty", candidates)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(40)}}},
		},
	}
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, allChangedOverlap, nil, nil, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("all-changed same-tx three-hop path candidates = %v, want only %v", candidates, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			2: {Deletes: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Deletes: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Deletes: []types.ProductValue{{types.NewUint64(60), types.NewUint64(40)}}},
		},
	}
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, deleteOverlap, nil, nil, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("delete same-tx three-hop path candidates = %v, want only %v", candidates, hash)
	}

	rhsChanged := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			4: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(40)}}},
		},
	}
	midCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rhsChanged, midCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("rhs-changed same-tx three-hop path candidates = %v, want only %v", candidates, hash)
	}

	mid2Changed := &store.Changeset{
		TxID: 5,
		Tables: map[TableID]*store.TableChangeset{
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
		},
	}
	outerCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		4: {{types.NewUint64(60), types.NewUint64(40)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, mid2Changed, outerCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("mid2-changed same-tx three-hop path candidates = %v, want only %v", candidates, hash)
	}

	mid1Changed := &store.Changeset{
		TxID: 6,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
		},
	}
	tailCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(60), types.NewUint64(40)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, mid1Changed, tailCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("mid1-changed same-tx three-hop path candidates = %v, want only %v", candidates, hash)
	}
}

func TestMultiJoinPlacementSplitOrFourHopUsesPath4Edges(t *testing.T) {
	s := fiveTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrFourHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 5},
		[]ColID{1, 0, 0, 0},
		[]ColID{1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("four-hop path range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("four-hop endpoint local placement = %v, want [%v]", got, hash)
	}
	rightPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{5, 4, 3, 2, 1},
		[]ColID{1, 1, 1, 1},
		[]ColID{0, 0, 0, 1},
		0,
	)
	if got := idx.joinPathEdge.Lookup(rightPathEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("four-hop path value edge placement = %v, want [%v]", got, hash)
	}
	shortPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 5},
		[]ColID{1, 0, 0},
		[]ColID{1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(shortPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("three-hop path edge placement = %v, want empty for four-hop path", got)
	}
	for _, table := range []TableID{1, 2, 3, 4, 5} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for covered four-hop path", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrFourHopPathEdgesUseCommittedRows(t *testing.T) {
	s := fiveTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrFourHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(40), types.NewUint64(50)}},
	})
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("mismatched four-hop path candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(60), types.NewUint64(50)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching four-hop path candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrFourHopPathEdgesUseSameTransactionRows(t *testing.T) {
	s := fiveTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrFourHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(50)}}},
		},
	}
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rejected, nil, nil, add)
	if len(candidates) != 0 {
		t.Fatalf("rejected same-tx four-hop path candidates = %v, want empty", candidates)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(50)}}},
		},
	}
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, allChangedOverlap, nil, nil, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("all-changed same-tx four-hop path candidates = %v, want only %v", candidates, hash)
	}

	rhsChanged := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			5: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(50)}}},
		},
	}
	midsCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rhsChanged, midsCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("rhs-changed same-tx four-hop path candidates = %v, want only %v", candidates, hash)
	}
}

func TestMultiJoinPlacementSplitOrFiveHopUsesPath5Edges(t *testing.T) {
	s := sixTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrFiveHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 5, 6},
		[]ColID{1, 0, 0, 0, 0},
		[]ColID{1, 1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("five-hop path range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("five-hop endpoint local placement = %v, want [%v]", got, hash)
	}
	rightPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{6, 5, 4, 3, 2, 1},
		[]ColID{1, 1, 1, 1, 1},
		[]ColID{0, 0, 0, 0, 1},
		0,
	)
	if got := idx.joinPathEdge.Lookup(rightPathEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("five-hop path value edge placement = %v, want [%v]", got, hash)
	}
	shortPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 6},
		[]ColID{1, 0, 0, 0},
		[]ColID{1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(shortPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("four-hop path edge placement = %v, want empty for five-hop path", got)
	}
	for _, table := range []TableID{1, 2, 3, 4, 5, 6} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for covered five-hop path", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrFiveHopPathEdgesUseCommittedRows(t *testing.T) {
	s := sixTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrFiveHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(40), types.NewUint64(70)}},
	})
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("mismatched five-hop path candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(60), types.NewUint64(70)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching five-hop path candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrFiveHopPathEdgesUseSameTransactionRows(t *testing.T) {
	s := sixTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrFiveHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(70)}}},
		},
	}
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rejected, nil, nil, add)
	if len(candidates) != 0 {
		t.Fatalf("rejected same-tx five-hop path candidates = %v, want empty", candidates)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(70)}}},
		},
	}
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, allChangedOverlap, nil, nil, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("all-changed same-tx five-hop path candidates = %v, want only %v", candidates, hash)
	}

	rhsChanged := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			6: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(70)}}},
		},
	}
	midsCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rhsChanged, midsCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("rhs-changed same-tx five-hop path candidates = %v, want only %v", candidates, hash)
	}

	firstMidChanged := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
		},
	}
	tailCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(60), types.NewUint64(70)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, firstMidChanged, tailCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("first-mid changed same-tx five-hop path candidates = %v, want only %v", candidates, hash)
	}
}

func TestMultiJoinPlacementSplitOrSixHopUsesPath6Edges(t *testing.T) {
	s := sevenTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrSixHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 5, 6, 7},
		[]ColID{1, 0, 0, 0, 0, 0},
		[]ColID{1, 1, 1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("six-hop path range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("six-hop endpoint local placement = %v, want [%v]", got, hash)
	}
	rightPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{7, 6, 5, 4, 3, 2, 1},
		[]ColID{1, 1, 1, 1, 1, 1},
		[]ColID{0, 0, 0, 0, 0, 1},
		0,
	)
	if got := idx.joinPathEdge.Lookup(rightPathEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("six-hop path value edge placement = %v, want [%v]", got, hash)
	}
	shortPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 5, 7},
		[]ColID{1, 0, 0, 0, 0},
		[]ColID{1, 1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(shortPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("five-hop path edge placement = %v, want empty for six-hop path", got)
	}
	for _, table := range []TableID{1, 2, 3, 4, 5, 6, 7} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for covered six-hop path", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrSixHopPathEdgesUseCommittedRows(t *testing.T) {
	s := sevenTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrSixHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(40), types.NewUint64(80)}},
	})
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("mismatched six-hop path candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(60), types.NewUint64(80)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching six-hop path candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrSixHopPathEdgesUseSameTransactionRows(t *testing.T) {
	s := sevenTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrSixHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6: {Inserts: []types.ProductValue{{types.NewUint64(80), types.NewUint64(70)}}},
			7: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(80)}}},
		},
	}
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rejected, nil, nil, add)
	if len(candidates) != 0 {
		t.Fatalf("rejected same-tx six-hop path candidates = %v, want empty", candidates)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6: {Inserts: []types.ProductValue{{types.NewUint64(80), types.NewUint64(70)}}},
			7: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(80)}}},
		},
	}
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, allChangedOverlap, nil, nil, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("all-changed same-tx six-hop path candidates = %v, want only %v", candidates, hash)
	}

	rhsChanged := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			7: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(80)}}},
		},
	}
	midsCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rhsChanged, midsCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("rhs-changed same-tx six-hop path candidates = %v, want only %v", candidates, hash)
	}

	firstMidChanged := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
		},
	}
	tailCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(60), types.NewUint64(80)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, firstMidChanged, tailCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("first-mid changed same-tx six-hop path candidates = %v, want only %v", candidates, hash)
	}
}

func TestMultiJoinPlacementSplitOrSevenHopUsesPath7Edges(t *testing.T) {
	s := eightTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrSevenHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 5, 6, 7, 8},
		[]ColID{1, 0, 0, 0, 0, 0, 0},
		[]ColID{1, 1, 1, 1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("seven-hop path range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("seven-hop endpoint local placement = %v, want [%v]", got, hash)
	}
	rightPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{8, 7, 6, 5, 4, 3, 2, 1},
		[]ColID{1, 1, 1, 1, 1, 1, 1},
		[]ColID{0, 0, 0, 0, 0, 0, 1},
		0,
	)
	if got := idx.joinPathEdge.Lookup(rightPathEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("seven-hop path value edge placement = %v, want [%v]", got, hash)
	}
	shortPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 5, 6, 8},
		[]ColID{1, 0, 0, 0, 0, 0},
		[]ColID{1, 1, 1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(shortPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("six-hop path edge placement = %v, want empty for seven-hop path", got)
	}
	for _, table := range []TableID{1, 2, 3, 4, 5, 6, 7, 8} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for covered seven-hop path", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrSevenHopPathEdgesUseCommittedRows(t *testing.T) {
	s := eightTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrSevenHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(90), types.NewUint64(80)}},
		8: {{types.NewUint64(40), types.NewUint64(90)}},
	})
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("mismatched seven-hop path candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(90), types.NewUint64(80)}},
		8: {{types.NewUint64(60), types.NewUint64(90)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching seven-hop path candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrSevenHopPathEdgesUseSameTransactionRows(t *testing.T) {
	s := eightTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrSevenHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6: {Inserts: []types.ProductValue{{types.NewUint64(80), types.NewUint64(70)}}},
			7: {Inserts: []types.ProductValue{{types.NewUint64(90), types.NewUint64(80)}}},
			8: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(90)}}},
		},
	}
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rejected, nil, nil, add)
	if len(candidates) != 0 {
		t.Fatalf("rejected same-tx seven-hop path candidates = %v, want empty", candidates)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6: {Inserts: []types.ProductValue{{types.NewUint64(80), types.NewUint64(70)}}},
			7: {Inserts: []types.ProductValue{{types.NewUint64(90), types.NewUint64(80)}}},
			8: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(90)}}},
		},
	}
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, allChangedOverlap, nil, nil, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("all-changed same-tx seven-hop path candidates = %v, want only %v", candidates, hash)
	}

	rhsChanged := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			8: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(90)}}},
		},
	}
	midsCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(90), types.NewUint64(80)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rhsChanged, midsCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("rhs-changed same-tx seven-hop path candidates = %v, want only %v", candidates, hash)
	}

	firstMidChanged := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
		},
	}
	tailCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(90), types.NewUint64(80)}},
		8: {{types.NewUint64(60), types.NewUint64(90)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, firstMidChanged, tailCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("first-mid changed same-tx seven-hop path candidates = %v, want only %v", candidates, hash)
	}
}

func TestMultiJoinPlacementSplitOrEightHopUsesPath8Edges(t *testing.T) {
	s := nineTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrEightHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 5, 6, 7, 8, 9},
		[]ColID{1, 0, 0, 0, 0, 0, 0, 0},
		[]ColID{1, 1, 1, 1, 1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("eight-hop path range edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("eight-hop endpoint local placement = %v, want [%v]", got, hash)
	}
	rightPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{9, 8, 7, 6, 5, 4, 3, 2, 1},
		[]ColID{1, 1, 1, 1, 1, 1, 1, 1},
		[]ColID{0, 0, 0, 0, 0, 0, 0, 1},
		0,
	)
	if got := idx.joinPathEdge.Lookup(rightPathEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("eight-hop path value edge placement = %v, want [%v]", got, hash)
	}
	shortPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 5, 6, 7, 9},
		[]ColID{1, 0, 0, 0, 0, 0, 0},
		[]ColID{1, 1, 1, 1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(shortPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("seven-hop path edge placement = %v, want empty for eight-hop path", got)
	}
	for _, table := range []TableID{1, 2, 3, 4, 5, 6, 7, 8, 9} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for covered eight-hop path", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrEightHopPathEdgesUseCommittedRows(t *testing.T) {
	s := nineTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrEightHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(90), types.NewUint64(80)}},
		8: {{types.NewUint64(100), types.NewUint64(90)}},
		9: {{types.NewUint64(40), types.NewUint64(100)}},
	})
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("mismatched eight-hop path candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(90), types.NewUint64(80)}},
		8: {{types.NewUint64(100), types.NewUint64(90)}},
		9: {{types.NewUint64(60), types.NewUint64(100)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching eight-hop path candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrEightHopPathEdgesUseSameTransactionRows(t *testing.T) {
	s := nineTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrEightHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6: {Inserts: []types.ProductValue{{types.NewUint64(80), types.NewUint64(70)}}},
			7: {Inserts: []types.ProductValue{{types.NewUint64(90), types.NewUint64(80)}}},
			8: {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(90)}}},
			9: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(100)}}},
		},
	}
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rejected, nil, nil, add)
	if len(candidates) != 0 {
		t.Fatalf("rejected same-tx eight-hop path candidates = %v, want empty", candidates)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5: {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6: {Inserts: []types.ProductValue{{types.NewUint64(80), types.NewUint64(70)}}},
			7: {Inserts: []types.ProductValue{{types.NewUint64(90), types.NewUint64(80)}}},
			8: {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(90)}}},
			9: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(100)}}},
		},
	}
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, allChangedOverlap, nil, nil, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("all-changed same-tx eight-hop path candidates = %v, want only %v", candidates, hash)
	}

	rhsChanged := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			9: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(100)}}},
		},
	}
	midsCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(90), types.NewUint64(80)}},
		8: {{types.NewUint64(100), types.NewUint64(90)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rhsChanged, midsCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("rhs-changed same-tx eight-hop path candidates = %v, want only %v", candidates, hash)
	}

	firstMidChanged := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
		},
	}
	tailCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(50), types.NewUint64(40)}},
		5: {{types.NewUint64(70), types.NewUint64(50)}},
		6: {{types.NewUint64(80), types.NewUint64(70)}},
		7: {{types.NewUint64(90), types.NewUint64(80)}},
		8: {{types.NewUint64(100), types.NewUint64(90)}},
		9: {{types.NewUint64(60), types.NewUint64(100)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, firstMidChanged, tailCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("first-mid changed same-tx eight-hop path candidates = %v, want only %v", candidates, hash)
	}
}

func TestMultiJoinPlacementSplitOrNineHopUsesGenericPathEdges(t *testing.T) {
	s := tenTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrNineHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		[]ColID{1, 0, 0, 0, 0, 0, 0, 0, 0},
		[]ColID{1, 1, 1, 1, 1, 1, 1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("nine-hop generic path range edge placement = %v, want [%v]", got, hash)
	}
	rightEdge := mustJoinPathTraversalEdge(t,
		[]TableID{10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
		[]ColID{1, 1, 1, 1, 1, 1, 1, 1, 1},
		[]ColID{0, 0, 0, 0, 0, 0, 0, 0, 1},
		0,
	)
	if got := idx.joinPathEdge.Lookup(rightEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("nine-hop generic path value edge placement = %v, want [%v]", got, hash)
	}
	for table := TableID(1); table <= 10; table++ {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for covered nine-hop path", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrNineHopPathEdgesUseCommittedRows(t *testing.T) {
	s := tenTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrNineHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2:  {{types.NewUint64(30), types.NewUint64(20)}},
		3:  {{types.NewUint64(40), types.NewUint64(30)}},
		4:  {{types.NewUint64(50), types.NewUint64(40)}},
		5:  {{types.NewUint64(70), types.NewUint64(50)}},
		6:  {{types.NewUint64(80), types.NewUint64(70)}},
		7:  {{types.NewUint64(90), types.NewUint64(80)}},
		8:  {{types.NewUint64(100), types.NewUint64(90)}},
		9:  {{types.NewUint64(110), types.NewUint64(100)}},
		10: {{types.NewUint64(40), types.NewUint64(110)}},
	})
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("mismatched nine-hop path candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2:  {{types.NewUint64(30), types.NewUint64(20)}},
		3:  {{types.NewUint64(40), types.NewUint64(30)}},
		4:  {{types.NewUint64(50), types.NewUint64(40)}},
		5:  {{types.NewUint64(70), types.NewUint64(50)}},
		6:  {{types.NewUint64(80), types.NewUint64(70)}},
		7:  {{types.NewUint64(90), types.NewUint64(80)}},
		8:  {{types.NewUint64(100), types.NewUint64(90)}},
		9:  {{types.NewUint64(110), types.NewUint64(100)}},
		10: {{types.NewUint64(60), types.NewUint64(110)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching nine-hop path candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrNineHopPathEdgesUseSameTransactionRows(t *testing.T) {
	s := tenTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrNineHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	changed := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	candidates := make(map[QueryHash]struct{})
	add := func(h QueryHash) {
		candidates[h] = struct{}{}
	}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2:  {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3:  {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4:  {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5:  {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6:  {Inserts: []types.ProductValue{{types.NewUint64(80), types.NewUint64(70)}}},
			7:  {Inserts: []types.ProductValue{{types.NewUint64(90), types.NewUint64(80)}}},
			8:  {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(90)}}},
			9:  {Inserts: []types.ProductValue{{types.NewUint64(110), types.NewUint64(100)}}},
			10: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(110)}}},
		},
	}
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, rejected, nil, nil, add)
	if len(candidates) != 0 {
		t.Fatalf("rejected same-tx nine-hop path candidates = %v, want empty", candidates)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2:  {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3:  {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4:  {Inserts: []types.ProductValue{{types.NewUint64(50), types.NewUint64(40)}}},
			5:  {Inserts: []types.ProductValue{{types.NewUint64(70), types.NewUint64(50)}}},
			6:  {Inserts: []types.ProductValue{{types.NewUint64(80), types.NewUint64(70)}}},
			7:  {Inserts: []types.ProductValue{{types.NewUint64(90), types.NewUint64(80)}}},
			8:  {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(90)}}},
			9:  {Inserts: []types.ProductValue{{types.NewUint64(110), types.NewUint64(100)}}},
			10: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(110)}}},
		},
	}
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, allChangedOverlap, nil, nil, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("all-changed same-tx nine-hop path candidates = %v, want only %v", candidates, hash)
	}

	firstMidChanged := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
		},
	}
	tailCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3:  {{types.NewUint64(40), types.NewUint64(30)}},
		4:  {{types.NewUint64(50), types.NewUint64(40)}},
		5:  {{types.NewUint64(70), types.NewUint64(50)}},
		6:  {{types.NewUint64(80), types.NewUint64(70)}},
		7:  {{types.NewUint64(90), types.NewUint64(80)}},
		8:  {{types.NewUint64(100), types.NewUint64(90)}},
		9:  {{types.NewUint64(110), types.NewUint64(100)}},
		10: {{types.NewUint64(60), types.NewUint64(110)}},
	})
	clear(candidates)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, changed, firstMidChanged, tailCommitted, s, add)
	if _, ok := candidates[hash]; !ok || len(candidates) != 1 {
		t.Fatalf("first-mid changed same-tx nine-hop path candidates = %v, want only %v", candidates, hash)
	}
}

func TestMultiJoinPlacementSplitOrNonKeyPreservingPathFallsBackWhenMidUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols, 0)
	s.addTable(3, cols, 1)
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3},
		[]ColID{1, 0},
		[]ColID{1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("unindexed non-key-preserving path edge = %v, want empty", got)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial endpoint local placement = %v, want empty when path is uncovered", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[1] = %v, want fallback [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrNonKeyPreservingPathFallsBackWhenRHSUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols, 0, 1)
	s.addTable(3, cols)
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3},
		[]ColID{1, 0},
		[]ColID{1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("rhs-unindexed non-key-preserving path edge = %v, want empty", got)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial endpoint local placement = %v, want empty when RHS path is uncovered", got)
	}
	conditionEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[conditionEdge][hash]; !ok {
		t.Fatalf("condition-edge fallback missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want condition-edge fallback", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrLongNonKeyPreservingMultiHopUsesPath3Edges(t *testing.T) {
	s := fourTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrLongNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3, 4},
		[]ColID{1, 0, 0},
		[]ColID{1, 1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("long non-key-preserving path3 range placement = %v, want [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("long non-key-preserving left local placement = %v, want [%v]", got, hash)
	}
	rightPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{4, 3, 2, 1},
		[]ColID{1, 1, 1},
		[]ColID{0, 0, 1},
		0,
	)
	if got := idx.joinPathEdge.Lookup(rightPathEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("long non-key-preserving path3 value placement = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("long non-key-preserving broad existence fallback = %+v, want none", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2, 3, 4} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for long non-key path placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementSplitOrLongNonKeyPreservingPath3FallsBackWhenUnindexed(t *testing.T) {
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	cases := []struct {
		name              string
		indexes           map[TableID][]ColID
		wantTableFallback bool
	}{
		{
			name: "mid1 join column",
			indexes: map[TableID][]ColID{
				1: {1},
				2: {0},
				3: {0, 1},
				4: {0, 1},
			},
			wantTableFallback: true,
		},
		{
			name: "mid2 join column",
			indexes: map[TableID][]ColID{
				1: {1},
				2: {0, 1},
				3: {0},
				4: {0, 1},
			},
		},
		{
			name: "rhs join column",
			indexes: map[TableID][]ColID{
				1: {1},
				2: {0, 1},
				3: {0, 1},
				4: {0},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeSchema()
			for table, indexes := range tc.indexes {
				s.addTable(table, cols, indexes...)
			}
			idx := NewPruningIndexes()
			pred := splitOrLongNonKeyPreservingMultiHopFilterMultiJoinPredicate()
			hash := ComputeQueryHash(pred, nil)
			placeSubscriptionForResolver(idx, pred, hash, s)

			pathEdge := mustJoinPathTraversalEdge(t,
				[]TableID{1, 2, 3, 4},
				[]ColID{1, 0, 0},
				[]ColID{1, 1, 1},
				0,
			)
			if got := idx.joinRangePathEdge.Lookup(pathEdge, types.NewUint64(60)); len(got) != 0 {
				t.Fatalf("partial path3 range placement = %v, want empty", got)
			}
			if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
				t.Fatalf("partial path3 endpoint local placement = %v, want empty", got)
			}

			conditionEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
			if tc.wantTableFallback {
				if got := idx.JoinEdge.exists[conditionEdge]; len(got) != 0 {
					t.Fatalf("condition-edge fallback = %v, want none", got)
				}
				if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != hash {
					t.Fatalf("TableIndex[1] = %v, want fallback [%v]", got, hash)
				}
			} else {
				if _, ok := idx.JoinEdge.exists[conditionEdge][hash]; !ok {
					t.Fatalf("condition-edge fallback missing: %+v", idx.JoinEdge.exists)
				}
				if got := idx.Table.Lookup(1); len(got) != 0 {
					t.Fatalf("TableIndex[1] = %v, want condition-edge fallback", got)
				}
			}

			removeSubscriptionForResolver(idx, pred, hash, s)
			if !pruningIndexesEmpty(idx) {
				t.Fatalf("indexes after remove = %+v, want empty", idx)
			}
		})
	}
}

func TestCollectCandidatesMultiJoinSplitOrLongNonKeyPreservingPath3PrunesMismatch(t *testing.T) {
	s := fourTableDualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrLongNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(40), types.NewUint64(40)}},
	})
	leftMismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched long non-key path3 left candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
		4: {{types.NewUint64(60), types.NewUint64(40)}},
	})
	got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching long non-key path3 left candidates = %v, want [%v]", got, hash)
	}

	localMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(99)}}
	got = CollectCandidatesForTable(idx, 1, localMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("local long non-key path3 left candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
	})
	rightMismatch := []types.ProductValue{{types.NewUint64(40), types.NewUint64(40)}}
	if got := CollectCandidatesForTable(idx, 4, rightMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched long non-key path3 right candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(30)}},
	})
	got = CollectCandidatesForTable(idx, 4, rightMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching long non-key path3 right candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrLongNonKeyPreservingPath3UsesDeltaRows(t *testing.T) {
	s := fourTableDualIndexedMultiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := splitOrLongNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{23},
		QueryID:    230,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	var hash QueryHash
	for h := range mgr.registry.byHash {
		hash = h
	}
	committed := buildMockCommitted(s, nil)
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(40)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping long non-key path3 candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Inserts: []types.ProductValue{{types.NewUint64(60), types.NewUint64(40)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping long non-key path3 candidates = %v, want only %v", got, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Deletes: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
			2: {Deletes: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Deletes: []types.ProductValue{{types.NewUint64(40), types.NewUint64(30)}}},
			4: {Deletes: []types.ProductValue{{types.NewUint64(60), types.NewUint64(40)}}},
		},
	}
	got = mgr.collectCandidatesInto(deleteOverlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping long non-key path3 delete candidates = %v, want only %v", got, hash)
	}
}

func TestMultiJoinPlacementSplitOrFallsBackWhenDirectEdgeUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols)
	s.addTable(3, cols, 1)
	idx := NewPruningIndexes()
	pred := splitOrLocalFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[1] = %v, want fallback [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial left local split-OR placement = %v, want empty", got)
	}
	leftRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(leftRangeEdge, types.NewUint64(60)); len(got) != 0 {
		t.Fatalf("unindexed left range-edge placement = %v, want empty", got)
	}
	if got := idx.Range.Lookup(2, 0, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right local split-OR range placement = %v, want [%v]", got, hash)
	}
	rightValueEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(rightValueEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("indexed right value-edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex[2] = %v, want empty for covered relation", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrMultiHopMiddleRelationPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrMultiHopFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched multi-hop middle candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
		3: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("left endpoint multi-hop middle candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
		3: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("right endpoint multi-hop middle candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinSplitOrColNeBranchUsesRangeEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := multiJoinUnfilteredTestPredicate()
	pred.Filter = Or{
		Left: ColNe{
			Table:  1,
			Column: 0,
			Alias:  0,
			Value:  types.NewUint64(7),
		},
		Right: ColRange{
			Table:  2,
			Column: 0,
			Alias:  1,
			Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
			Upper:  Bound{Unbounded: true},
		},
	}
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Range.Lookup(1, 0, types.NewUint64(6)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left ColNe lower range placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("left ColNe rejected value placement = %v, want empty", got)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(8)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left ColNe upper range placement = %v, want [%v]", got, hash)
	}
	rightRangeEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(rightRangeEdge, types.NewUint64(6)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right ColNe lower range-edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(rightRangeEdge, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("right ColNe rejected range-edge placement = %v, want empty", got)
	}
	if got := idx.JoinRangeEdge.Lookup(rightRangeEdge, types.NewUint64(8)); len(got) != 1 || got[0] != hash {
		t.Fatalf("right ColNe upper range-edge placement = %v, want [%v]", got, hash)
	}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("right ColNe edge rejected candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 2, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("right ColNe edge candidates = %v, want [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinSplitOrLocalFilterPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrLocalFilterMultiJoinPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(40), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched split-OR left candidates = %v, want empty", got)
	}

	localMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(99)}}
	got := CollectCandidatesForTable(idx, 1, localMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("left local split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(60), types.NewUint64(20)}},
	})
	edgeMatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 1, edgeMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("left edge split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
	})
	mismatch = []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched split-OR right candidates = %v, want empty", got)
	}

	localRangeMatch := []types.ProductValue{{types.NewUint64(60), types.NewUint64(99)}}
	got = CollectCandidatesForTable(idx, 2, localRangeMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("right local split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	edgeMatch = []types.ProductValue{{types.NewUint64(40), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 2, edgeMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("right edge split-OR candidates = %v, want [%v]", got, hash)
	}
}
