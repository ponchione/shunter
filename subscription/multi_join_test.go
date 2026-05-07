package subscription

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func multiJoinTestSchema() *fakeSchema {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols, 1)
	s.addTable(3, cols, 1)
	return s
}

func multiJoinTestPredicate() MultiJoin {
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
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
		},
		ProjectedRelation: 0,
		Filter:            ColNe{Table: 3, Column: 0, Alias: 2, Value: types.NewUint64(99)},
	}
}

func multiJoinUnfilteredTestPredicate() MultiJoin {
	pred := multiJoinTestPredicate()
	pred.Filter = nil
	return pred
}

func repeatedMultiJoinAllAliasesFilteredPredicate() MultiJoin {
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
		Filter: And{
			Left: ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint64(7)},
			Right: ColRange{Table: 1, Column: 0, Alias: 1,
				Lower: Bound{Value: types.NewUint64(10), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
}

func repeatedMultiJoinConditionPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 1, Alias: 1},
			{Table: 2, Alias: 2},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 0, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 2, Table: 2, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 1, Column: 1, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 2, Column: 1, Alias: 2},
			},
		},
		ProjectedRelation: 0,
	}
}

func repeatedMultiJoinMixedAliasPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 1, Alias: 1},
			{Table: 2, Alias: 2},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 1, Column: 1, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 2, Column: 1, Alias: 2},
			},
		},
		ProjectedRelation: 0,
		Filter:            ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint64(7)},
	}
}

func repeatedMultiJoinKeyPreservingAliasFilterPredicate() MultiJoin {
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
		Filter:            ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint64(7)},
	}
}

func repeatedMultiJoinUncoveredAliasPredicate() MultiJoin {
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
		},
		ProjectedRelation: 0,
		Filter:            ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint64(7)},
	}
}

func repeatedMultiJoinCrossAliasFilterPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 1, Alias: 1},
			{Table: 2, Alias: 2},
		},
		ProjectedRelation: 0,
		Filter: And{
			Left: ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint64(7)},
			Right: ColEqCol{
				LeftTable:   1,
				LeftColumn:  1,
				LeftAlias:   1,
				RightTable:  2,
				RightColumn: 1,
				RightAlias:  2,
			},
		},
	}
}

func repeatedMultiJoinSelfAliasFilterPredicate() MultiJoin {
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
		},
		ProjectedRelation: 0,
		Filter: ColEqCol{
			LeftTable:   1,
			LeftColumn:  0,
			LeftAlias:   0,
			RightTable:  1,
			RightColumn: 1,
			RightAlias:  1,
		},
	}
}

func countMultiJoinRIDAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateCount,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  3,
			Column: 0,
			Alias:  2,
		},
	}
}

func countDistinctMultiJoinTIDAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateCount,
		Distinct:     true,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  1,
			Column: 0,
			Alias:  0,
		},
	}
}

func sumMultiJoinRIDAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateSum,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "total", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  3,
			Column: 0,
			Alias:  2,
		},
	}
}

func multiJoinBaseContents() map[TableID][]types.ProductValue {
	return map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewUint64(10)},
			{types.NewUint64(2), types.NewUint64(20)},
			{types.NewUint64(3), types.NewUint64(20)},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(10)},
			{types.NewUint64(21), types.NewUint64(20)},
			{types.NewUint64(22), types.NewUint64(20)},
		},
		3: {
			{types.NewUint64(100), types.NewUint64(10)},
			{types.NewUint64(99), types.NewUint64(20)},
			{types.NewUint64(300), types.NewUint64(20)},
		},
	}
}

func multiJoinCommitted(includeExtraR bool) *mockCommitted {
	s := multiJoinTestSchema()
	contents := multiJoinBaseContents()
	if includeExtraR {
		contents[3] = append(contents[3], types.ProductValue{types.NewUint64(301), types.NewUint64(20)})
	}
	return buildMockCommitted(s, contents)
}

func TestMultiJoinAggregatesRegisterInitialRowsAndDeltas(t *testing.T) {
	s := multiJoinTestSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := multiJoinTestPredicate()
	connID := types.ConnectionID{8}
	before := multiJoinCommitted(false)
	tests := []struct {
		queryID   uint32
		aggregate *Aggregate
		want      uint64
		column    string
	}{
		{80, countStarAggregate(), 5, "n"},
		{81, countMultiJoinRIDAggregate(), 5, "n"},
		{82, countDistinctMultiJoinTIDAggregate(), 3, "n"},
		{83, sumMultiJoinRIDAggregate(), 1300, "total"},
	}
	for _, tt := range tests {
		res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID:     connID,
			QueryID:    tt.queryID,
			Predicates: []Predicate{pred},
			Aggregates: []*Aggregate{tt.aggregate},
		}, before)
		if err != nil {
			t.Fatalf("RegisterSet multi-join aggregate queryID=%d: %v", tt.queryID, err)
		}
		if len(res.Update) != 1 {
			t.Fatalf("initial update count queryID=%d = %d, want 1", tt.queryID, len(res.Update))
		}
		update := res.Update[0]
		if update.TableID != 1 || len(update.Columns) != 1 || update.Columns[0].Name != tt.column {
			t.Fatalf("multi-join aggregate shape queryID=%d = table %d columns %#v, want t/%s", tt.queryID, update.TableID, update.Columns, tt.column)
		}
		if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != tt.want {
			t.Fatalf("multi-join aggregate initial rows queryID=%d = %#v, want %d", tt.queryID, update.Inserts, tt.want)
		}
	}

	extra := types.ProductValue{types.NewUint64(301), types.NewUint64(20)}
	csInsert := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			3: {Inserts: []types.ProductValue{extra}},
		},
	}
	mgr.EvalAndBroadcast(types.TxID(1), csInsert, multiJoinCommitted(true), PostCommitMeta{})
	first := aggregateUpdatesByQueryID((<-inbox).Fanout[connID])
	requireAggregateDelta(t, first[80], 5, 9, "COUNT(*)")
	requireAggregateDelta(t, first[81], 5, 9, "COUNT(r.id)")
	requireAggregateDelta(t, first[83], 1300, 2504, "SUM(r.id)")
	if _, ok := first[82]; ok {
		t.Fatalf("COUNT(DISTINCT t.id) changed on duplicate projected ids: %+v", first[82])
	}
}

func TestMultiJoinAggregatesDeltaMatchesFreshEvaluationForIntermediateRelation(t *testing.T) {
	s := multiJoinTestSchema()
	pred := multiJoinTestPredicate()
	deletedMiddle := types.ProductValue{types.NewUint64(21), types.NewUint64(20)}
	insertedMiddle := types.ProductValue{types.NewUint64(23), types.NewUint64(20)}

	aggregates := []multiJoinAggregateCase{
		{queryID: 180, aggregate: countStarAggregate(), column: "n"},
		{queryID: 181, aggregate: countMultiJoinRIDAggregate(), column: "n"},
		{queryID: 182, aggregate: countDistinctMultiJoinTIDAggregate(), column: "n"},
		{queryID: 183, aggregate: sumMultiJoinRIDAggregate(), column: "total"},
	}
	tests := []struct {
		name      string
		after     map[TableID][]types.ProductValue
		changeset *store.Changeset
		want      map[uint32]aggregateDelta
	}{
		{
			name: "insert",
			after: func() map[TableID][]types.ProductValue {
				rows := multiJoinBaseContents()
				rows[2] = append(rows[2], insertedMiddle)
				return rows
			}(),
			changeset: &store.Changeset{
				TxID: 1,
				Tables: map[TableID]*store.TableChangeset{
					2: {Inserts: []types.ProductValue{insertedMiddle}},
				},
			},
			want: map[uint32]aggregateDelta{
				180: {before: 5, after: 7},
				181: {before: 5, after: 7},
				183: {before: 1300, after: 1900},
			},
		},
		{
			name: "delete",
			after: func() map[TableID][]types.ProductValue {
				rows := multiJoinBaseContents()
				rows[2] = []types.ProductValue{
					{types.NewUint64(10), types.NewUint64(10)},
					{types.NewUint64(22), types.NewUint64(20)},
				}
				return rows
			}(),
			changeset: &store.Changeset{
				TxID: 2,
				Tables: map[TableID]*store.TableChangeset{
					2: {Deletes: []types.ProductValue{deletedMiddle}},
				},
			},
			want: map[uint32]aggregateDelta{
				180: {before: 5, after: 3},
				181: {before: 5, after: 3},
				183: {before: 1300, after: 700},
			},
		},
		{
			name: "same-key-replace",
			after: func() map[TableID][]types.ProductValue {
				rows := multiJoinBaseContents()
				rows[2] = []types.ProductValue{
					{types.NewUint64(10), types.NewUint64(10)},
					{types.NewUint64(22), types.NewUint64(20)},
					insertedMiddle,
				}
				return rows
			}(),
			changeset: &store.Changeset{
				TxID: 3,
				Tables: map[TableID]*store.TableChangeset{
					2: {
						Inserts: []types.ProductValue{insertedMiddle},
						Deletes: []types.ProductValue{deletedMiddle},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := requireMultiJoinAggregateDeltasMatchFresh(t, s, pred, aggregates, buildMockCommitted(s, multiJoinBaseContents()), buildMockCommitted(s, tt.after), tt.changeset)
			if len(got) != len(tt.want) {
				t.Fatalf("aggregate update count = %d (%+v), want %d", len(got), got, len(tt.want))
			}
			for _, agg := range aggregates {
				want, shouldChange := tt.want[agg.queryID]
				update, changed := got[agg.queryID]
				if !shouldChange {
					if changed {
						t.Fatalf("query %d emitted unchanged aggregate delta: %+v", agg.queryID, update)
					}
					continue
				}
				requireAggregateDelta(t, update, want.before, want.after, agg.column)
			}
		})
	}
}

func TestMultiJoinRegisterInitialRowsAndDeltas(t *testing.T) {
	s := multiJoinTestSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := multiJoinTestPredicate()
	connID := types.ConnectionID{7}
	before := multiJoinCommitted(false)
	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    70,
		Predicates: []Predicate{pred},
	}, before)
	if err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	if len(res.Update) != 1 {
		t.Fatalf("initial updates = %d, want 1", len(res.Update))
	}
	if !productRowsHaveUint64IDs(res.Update[0].Inserts, 1, 2, 2, 3, 3) {
		t.Fatalf("initial rows = %v, want projected ids 1,2,2,3,3", res.Update[0].Inserts)
	}

	extra := types.ProductValue{types.NewUint64(301), types.NewUint64(20)}
	csInsert := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			3: {Inserts: []types.ProductValue{extra}},
		},
	}
	mgr.EvalAndBroadcast(types.TxID(1), csInsert, multiJoinCommitted(true), PostCommitMeta{})
	insertMsg := <-inbox
	insertUpdate := requireSingleFanoutUpdate(t, insertMsg, connID)
	if !productRowsHaveUint64IDs(insertUpdate.Inserts, 2, 2, 3, 3) || len(insertUpdate.Deletes) != 0 {
		t.Fatalf("insert delta inserts/deletes = %v/%v, want ids 2,2,3,3 and no deletes", insertUpdate.Inserts, insertUpdate.Deletes)
	}

	csDelete := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			3: {Deletes: []types.ProductValue{extra}},
		},
	}
	mgr.EvalAndBroadcast(types.TxID(2), csDelete, multiJoinCommitted(false), PostCommitMeta{})
	deleteMsg := <-inbox
	deleteUpdate := requireSingleFanoutUpdate(t, deleteMsg, connID)
	if !productRowsHaveUint64IDs(deleteUpdate.Deletes, 2, 2, 3, 3) || len(deleteUpdate.Inserts) != 0 {
		t.Fatalf("delete delta inserts/deletes = %v/%v, want no inserts and delete ids 2,2,3,3", deleteUpdate.Inserts, deleteUpdate.Deletes)
	}
}

func TestMultiJoinDeltaMatchesFreshEvaluationForIntermediateRelation(t *testing.T) {
	s := multiJoinTestSchema()
	pred := multiJoinTestPredicate()
	deletedMiddle := types.ProductValue{types.NewUint64(21), types.NewUint64(20)}
	insertedMiddle := types.ProductValue{types.NewUint64(23), types.NewUint64(20)}
	unmatchedMiddle := types.ProductValue{types.NewUint64(24), types.NewUint64(30)}

	tests := []struct {
		name        string
		after       map[TableID][]types.ProductValue
		changeset   *store.Changeset
		wantInserts []uint64
		wantDeletes []uint64
	}{
		{
			name: "insert",
			after: func() map[TableID][]types.ProductValue {
				rows := multiJoinBaseContents()
				rows[2] = append(rows[2], insertedMiddle)
				return rows
			}(),
			changeset: &store.Changeset{
				TxID: 1,
				Tables: map[TableID]*store.TableChangeset{
					2: {Inserts: []types.ProductValue{insertedMiddle}},
				},
			},
			wantInserts: []uint64{2, 3},
		},
		{
			name: "delete",
			after: func() map[TableID][]types.ProductValue {
				rows := multiJoinBaseContents()
				rows[2] = []types.ProductValue{
					{types.NewUint64(10), types.NewUint64(10)},
					{types.NewUint64(22), types.NewUint64(20)},
				}
				return rows
			}(),
			changeset: &store.Changeset{
				TxID: 2,
				Tables: map[TableID]*store.TableChangeset{
					2: {Deletes: []types.ProductValue{deletedMiddle}},
				},
			},
			wantDeletes: []uint64{2, 3},
		},
		{
			name: "same-key-replace",
			after: func() map[TableID][]types.ProductValue {
				rows := multiJoinBaseContents()
				rows[2] = []types.ProductValue{
					{types.NewUint64(10), types.NewUint64(10)},
					{types.NewUint64(22), types.NewUint64(20)},
					insertedMiddle,
				}
				return rows
			}(),
			changeset: &store.Changeset{
				TxID: 3,
				Tables: map[TableID]*store.TableChangeset{
					2: {
						Inserts: []types.ProductValue{insertedMiddle},
						Deletes: []types.ProductValue{deletedMiddle},
					},
				},
			},
		},
		{
			name: "unmatched-insert",
			after: func() map[TableID][]types.ProductValue {
				rows := multiJoinBaseContents()
				rows[2] = append(rows[2], unmatchedMiddle)
				return rows
			}(),
			changeset: &store.Changeset{
				TxID: 4,
				Tables: map[TableID]*store.TableChangeset{
					2: {Inserts: []types.ProductValue{unmatchedMiddle}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta := requireMultiJoinIncrementalMatchesFresh(t, s, pred, buildMockCommitted(s, multiJoinBaseContents()), buildMockCommitted(s, tt.after), tt.changeset)
			if !productRowsHaveUint64IDs(delta.inserts, tt.wantInserts...) {
				t.Fatalf("insert ids = %v, want %v", delta.inserts, tt.wantInserts)
			}
			if !productRowsHaveUint64IDs(delta.deletes, tt.wantDeletes...) {
				t.Fatalf("delete ids = %v, want %v", delta.deletes, tt.wantDeletes)
			}
		})
	}
}

func TestMultiJoinPlacementUsesLocalFilterIndexesForDistinctTables(t *testing.T) {
	idx := NewPruningIndexes()
	pred := multiJoinTestPredicate()
	hash := ComputeQueryHash(pred, nil)
	PlaceSubscription(idx, pred, hash)

	for _, table := range []TableID{1, 2} {
		got := idx.Table.Lookup(table)
		if len(got) != 1 || got[0] != hash {
			t.Fatalf("TableIndex[%d] = %v, want [%v]", table, got, hash)
		}
	}
	if got := idx.Range.Lookup(3, 0, types.NewUint64(100)); len(got) != 1 || got[0] != hash {
		t.Fatalf("RangeIndex[3.0 != 99] match = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(3, 0, types.NewUint64(99)); len(got) != 0 {
		t.Fatalf("RangeIndex[3.0 != 99] rejected value = %v, want empty", got)
	}
	if got := idx.Table.Lookup(3); len(got) != 0 {
		t.Fatalf("TableIndex[3] = %v, want empty for locally filtered distinct relation", got)
	}

	RemoveSubscription(idx, pred, hash)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementUsesRequiredRemoteFilterEdgesForDistinctTables(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := multiJoinTestPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftEdge := JoinEdge{LHSTable: 1, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(leftEdge, types.NewUint64(100)); len(got) != 1 || got[0] != hash {
		t.Fatalf("left required remote range-edge placement = %v, want [%v]", got, hash)
	}
	if got := idx.JoinRangeEdge.Lookup(leftEdge, types.NewUint64(99)); len(got) != 0 {
		t.Fatalf("left rejected required remote range-edge placement = %v, want empty", got)
	}
	middleEdge := JoinEdge{LHSTable: 2, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(middleEdge, types.NewUint64(100)); len(got) != 1 || got[0] != hash {
		t.Fatalf("middle required remote range-edge placement = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("required remote filter placement existence edges = %+v, want none", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for required remote filter placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinRequiredRemoteFilterEdgesPruneMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := multiJoinTestPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(99), types.NewUint64(20)}},
	})
	leftMismatch := []types.ProductValue{{types.NewUint64(500), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched required remote left candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(100), types.NewUint64(20)}},
	})
	got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching required remote left candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(99), types.NewUint64(20)}},
	})
	middleMismatch := []types.ProductValue{{types.NewUint64(200), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, middleMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched required remote middle candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(100), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, middleMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching required remote middle candidates = %v, want [%v]", got, hash)
	}
}

func TestMultiJoinPlacementUsesRequiredRemoteFilterPathEdges(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	pred.Filter = ColNe{Table: 3, Column: 0, Alias: 2, Value: types.NewUint64(99)}
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftPathEdge := mustJoinPathTraversalEdge(t,
		[]TableID{1, 2, 3},
		[]ColID{1, 0},
		[]ColID{1, 1},
		0,
	)
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(100)); len(got) != 1 || got[0] != hash {
		t.Fatalf("required remote range path edge = %v, want [%v]", got, hash)
	}
	if got := idx.joinRangePathEdge.Lookup(leftPathEdge, types.NewUint64(99)); len(got) != 0 {
		t.Fatalf("required remote rejected range path edge = %v, want empty", got)
	}
	middleEdge := JoinEdge{LHSTable: 2, RHSTable: 3, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinRangeEdge.Lookup(middleEdge, types.NewUint64(100)); len(got) != 1 || got[0] != hash {
		t.Fatalf("required remote middle range edge = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(3, 0, types.NewUint64(100)); len(got) != 1 || got[0] != hash {
		t.Fatalf("required remote local range placement = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("required remote path placement existence edges = %+v, want none", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for required remote path placement", table, got)
		}
	}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(99), types.NewUint64(30)}},
	})
	leftMismatch := []types.ProductValue{{types.NewUint64(500), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched required remote path candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
		3: {{types.NewUint64(100), types.NewUint64(30)}},
	})
	got := CollectCandidatesForTable(idx, 1, leftMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching required remote path candidates = %v, want [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinRequiredRemoteFilterPathEdgesUseSameTransactionRows(t *testing.T) {
	s := dualIndexedMultiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := splitOrNonKeyPreservingMultiHopFilterMultiJoinPredicate()
	pred.Filter = ColNe{Table: 3, Column: 0, Alias: 2, Value: types.NewUint64(99)}
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	leftRows := []types.ProductValue{{types.NewUint64(500), types.NewUint64(20)}}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(99), types.NewUint64(30)}}},
		},
	}
	got := make(map[QueryHash]struct{})
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, leftRows, rejected, nil, nil, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if len(got) != 0 {
		t.Fatalf("rejected same-tx required remote path candidates = %v, want empty", got)
	}

	allChangedOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(30)}}},
		},
	}
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, leftRows, allChangedOverlap, nil, nil, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("all-changed same-tx required remote path candidates = %v, want only %v", got, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			2: {Deletes: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
			3: {Deletes: []types.ProductValue{{types.NewUint64(100), types.NewUint64(30)}}},
		},
	}
	clear(got)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, leftRows, deleteOverlap, nil, nil, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("all-changed same-tx required remote path delete candidates = %v, want only %v", got, hash)
	}

	midCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(30), types.NewUint64(20)}},
	})
	rhsChangedOverlap := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			3: {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(30)}}},
		},
	}
	clear(got)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, leftRows, rhsChangedOverlap, midCommitted, s, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("rhs-changed same-tx required remote path candidates = %v, want only %v", got, hash)
	}

	rhsCommitted := buildMockCommitted(s, map[TableID][]types.ProductValue{
		3: {{types.NewUint64(100), types.NewUint64(30)}},
	})
	midChangedOverlap := &store.Changeset{
		TxID: 5,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(30), types.NewUint64(20)}}},
		},
	}
	clear(got)
	collectJoinPathTraversalFilterDeltaCandidates(idx, 1, leftRows, midChangedOverlap, rhsCommitted, s, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("mid-changed same-tx required remote path candidates = %v, want only %v", got, hash)
	}
}

func TestCollectCandidatesMultiJoinRequiredRemoteFilterEdgesUseSameTransactionRows(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := multiJoinTestPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	leftRows := []types.ProductValue{{types.NewUint64(500), types.NewUint64(20)}}
	got := make(map[QueryHash]struct{})
	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			3: {Inserts: []types.ProductValue{{types.NewUint64(99), types.NewUint64(20)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 1, leftRows, rejected, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if len(got) != 0 {
		t.Fatalf("rejected same-tx required remote left candidates = %v, want empty", got)
	}

	noOverlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			3: {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(21)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 1, leftRows, noOverlap, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if len(got) != 0 {
		t.Fatalf("non-overlapping same-tx required remote left candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			3: {Inserts: []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 1, leftRows, overlap, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping same-tx required remote left candidates = %v, want only %v", got, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 4,
		Tables: map[TableID]*store.TableChangeset{
			3: {Deletes: []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}},
		},
	}
	clear(got)
	collectJoinFilterDeltaCandidates(idx, 1, leftRows, deleteOverlap, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping same-tx required remote left delete candidates = %v, want only %v", got, hash)
	}

	middleRows := []types.ProductValue{{types.NewUint64(200), types.NewUint64(20)}}
	clear(got)
	collectJoinFilterDeltaCandidates(idx, 2, middleRows, overlap, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping same-tx required remote middle candidates = %v, want only %v", got, hash)
	}
}

func TestMultiJoinPlacementUsesJoinConditionExistenceEdgesForDistinctTables(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := multiJoinUnfilteredTestPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	tests := []JoinEdge{
		{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 2, RHSTable: 3, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 3, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
	}
	for _, edge := range tests {
		if _, ok := idx.JoinEdge.exists[edge][hash]; !ok {
			t.Fatalf("missing multi-join existence edge %+v in %+v", edge, idx.JoinEdge.exists)
		}
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

func TestMultiJoinPlacementRepeatedTableUsesAliasLocalFiltersWhenEveryAliasConstrained(t *testing.T) {
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinAllAliasesFilteredPredicate()
	hash := ComputeQueryHash(pred, nil)
	PlaceSubscription(idx, pred, hash)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-0 value placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(11)); len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-1 range placement = %v, want [%v]", got, hash)
	}
	if got := idx.Range.Lookup(1, 0, types.NewUint64(10)); len(got) != 0 {
		t.Fatalf("alias-1 rejected range boundary = %v, want empty", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want empty for fully alias-constrained repeated table", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[2] = %v, want fallback [%v]", got, hash)
	}

	RemoveSubscription(idx, pred, hash)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementRepeatedTableUsesJoinConditionEdgesWhenEveryAliasConstrained(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinConditionPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	tests := []JoinEdge{
		{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
	}
	for _, edge := range tests {
		if _, ok := idx.JoinEdge.exists[edge][hash]; !ok {
			t.Fatalf("missing repeated multi-join condition edge %+v in %+v", edge, idx.JoinEdge.exists)
		}
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementRepeatedTableCombinesAliasFiltersAndConditionEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinMixedAliasPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-0 value placement = %v, want [%v]", got, hash)
	}
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[edge][hash]; !ok {
		t.Fatalf("alias-1 condition edge missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want empty for compound alias placement", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex[2] = %v, want empty for condition placement", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementRepeatedTableUsesRequiredRemoteAliasFilterEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinKeyPreservingAliasFilterPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-0 required filter placement = %v, want [%v]", got, hash)
	}
	selfEdge := JoinEdge{LHSTable: 1, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(selfEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-1 required remote filter edge = %v, want [%v]", got, hash)
	}
	middleEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 0}
	if got := idx.JoinEdge.Lookup(middleEdge, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("middle required remote filter edge = %v, want [%v]", got, hash)
	}
	if len(idx.JoinEdge.exists) != 0 {
		t.Fatalf("required remote alias filter existence edges = %+v, want none", idx.JoinEdge.exists)
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for required remote alias filter placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectCandidatesMultiJoinRepeatedRequiredRemoteAliasFiltersPruneMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinKeyPreservingAliasFilterPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(9), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched repeated required remote candidates = %v, want empty", got)
	}

	localMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(99)}}
	got := CollectCandidatesForTable(idx, 1, localMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("local repeated required remote candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 1, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("edge repeated required remote candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(8), types.NewUint64(20)}},
	})
	middleMismatch := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 2, middleMismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched middle required remote candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewUint64(20)}},
	})
	got = CollectCandidatesForTable(idx, 2, middleMismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching middle required remote candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinRepeatedRequiredRemoteAliasFiltersUseSameTransactionRows(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinKeyPreservingAliasFilterPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	aliasRows := []types.ProductValue{{types.NewUint64(9), types.NewUint64(20)}}
	got := make(map[QueryHash]struct{})
	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 1, aliasRows, rejected, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if len(got) != 0 {
		t.Fatalf("rejected same-tx repeated required alias candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 1, aliasRows, overlap, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping same-tx repeated required alias candidates = %v, want only %v", got, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Deletes: []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}},
		},
	}
	clear(got)
	collectJoinFilterDeltaCandidates(idx, 1, aliasRows, deleteOverlap, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping same-tx repeated required alias delete candidates = %v, want only %v", got, hash)
	}

	middleRows := []types.ProductValue{{types.NewUint64(100), types.NewUint64(20)}}
	clear(got)
	collectJoinFilterDeltaCandidates(idx, 2, middleRows, rejected, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if len(got) != 0 {
		t.Fatalf("rejected same-tx repeated middle candidates = %v, want empty", got)
	}

	collectJoinFilterDeltaCandidates(idx, 2, middleRows, overlap, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping same-tx repeated middle candidates = %v, want only %v", got, hash)
	}
}

func TestMultiJoinPlacementRepeatedTableFallsBackWhenAliasUncovered(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinUncoveredAliasPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[1] = %v, want fallback [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial alias filter placement = %v, want empty when another alias is uncovered", got)
	}
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[edge][hash]; ok {
		t.Fatalf("partial condition edge placement present for uncovered repeated table: %+v", idx.JoinEdge.exists)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementRepeatedTableFallsBackWhenMixedAliasConditionEdgeUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols)
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinMixedAliasPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[1] = %v, want fallback [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial alias filter placement = %v, want empty when condition edge is unindexed", got)
	}
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[edge][hash]; ok {
		t.Fatalf("unindexed condition edge placement present: %+v", idx.JoinEdge.exists)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementRepeatedTableUsesCrossAliasFilterEdges(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinCrossAliasFilterPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-0 value placement = %v, want [%v]", got, hash)
	}
	leftEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[leftEdge][hash]; !ok {
		t.Fatalf("alias-1 filter edge missing: %+v", idx.JoinEdge.exists)
	}
	rightEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[rightEdge][hash]; !ok {
		t.Fatalf("alias-2 filter edge missing: %+v", idx.JoinEdge.exists)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want empty for cross-alias filter placement", got)
	}
	if got := idx.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("TableIndex[2] = %v, want empty for cross-alias filter placement", got)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementRepeatedTableCrossAliasFilterFallsBackWhenUnindexed(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 1)
	s.addTable(2, cols)
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinCrossAliasFilterPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	if got := idx.Table.Lookup(1); len(got) != 1 || got[0] != hash {
		t.Fatalf("TableIndex[1] = %v, want fallback [%v]", got, hash)
	}
	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("partial alias filter placement = %v, want empty when filter edge is unindexed", got)
	}
	leftEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[leftEdge][hash]; ok {
		t.Fatalf("unindexed left filter edge placement present: %+v", idx.JoinEdge.exists)
	}
	rightEdge := JoinEdge{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1}
	if _, ok := idx.JoinEdge.exists[rightEdge][hash]; !ok {
		t.Fatalf("indexed right filter edge missing: %+v", idx.JoinEdge.exists)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementRepeatedTableUsesSelfAliasFilterEdges(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 0, 1)
	s.addTable(2, cols, 1)
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinSelfAliasFilterPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)

	tests := []JoinEdge{
		{LHSTable: 1, RHSTable: 1, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 1},
		{LHSTable: 1, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 0, RHSFilterCol: 0},
		{LHSTable: 2, RHSTable: 1, LHSJoinCol: 1, RHSJoinCol: 1, RHSFilterCol: 1},
	}
	for _, edge := range tests {
		if _, ok := idx.JoinEdge.exists[edge][hash]; !ok {
			t.Fatalf("self-alias filter edge missing %+v in %+v", edge, idx.JoinEdge.exists)
		}
	}
	for _, table := range []TableID{1, 2} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] = %v, want empty for self-alias filter placement", table, got)
		}
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestMultiJoinPlacementRepeatedTableKeepsTableFallback(t *testing.T) {
	idx := NewPruningIndexes()
	pred := MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 1, Alias: 1},
			{Table: 2, Alias: 2},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 1, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 1, Column: 1, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 2, Column: 1, Alias: 2},
			},
		},
		ProjectedRelation: 0,
		Filter:            ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint64(7)},
	}
	hash := ComputeQueryHash(pred, nil)
	PlaceSubscription(idx, pred, hash)

	if got := idx.Value.Lookup(1, 0, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("repeated-table MultiJoin value placement = %v, want empty", got)
	}
	for _, table := range []TableID{1, 2} {
		got := idx.Table.Lookup(table)
		if len(got) != 1 || got[0] != hash {
			t.Fatalf("TableIndex[%d] = %v, want [%v]", table, got, hash)
		}
	}
}

func TestCollectCandidatesMultiJoinRepeatedAliasFiltersPruneMismatch(t *testing.T) {
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinAllAliasesFilteredPredicate()
	hash := ComputeQueryHash(pred, nil)
	PlaceSubscription(idx, pred, hash)

	mismatch := []types.ProductValue{{types.NewUint64(9), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, nil, nil); len(got) != 0 {
		t.Fatalf("mismatched repeated-alias candidates = %v, want empty", got)
	}

	aliasZeroMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}
	got := CollectCandidatesForTable(idx, 1, aliasZeroMatch, nil, nil)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-0 repeated-alias candidates = %v, want [%v]", got, hash)
	}

	aliasOneMatch := []types.ProductValue{{types.NewUint64(11), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 1, aliasOneMatch, nil, nil)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-1 repeated-alias candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinRepeatedConditionPrunesCommittedMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinConditionPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(20)}},
	})

	mismatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(8)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched repeated-condition candidates = %v, want empty", got)
	}

	aliasZeroMatch := []types.ProductValue{{types.NewUint64(20), types.NewUint64(8)}}
	got := CollectCandidatesForTable(idx, 1, aliasZeroMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-0 repeated-condition candidates = %v, want [%v]", got, hash)
	}

	aliasOneMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 1, aliasOneMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-1 repeated-condition candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinRepeatedConditionUsesDeltaOppositeRows(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := repeatedMultiJoinConditionPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{10},
		QueryID:    100,
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
			1: {Inserts: []types.ProductValue{{types.NewUint64(77), types.NewUint64(88)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(99)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping repeated-condition same-tx candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(77), types.NewUint64(88)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(77)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping repeated-condition same-tx candidates = %v, want only %v", got, hash)
	}
}

func TestCollectCandidatesMultiJoinRepeatedMixedAliasPlacementPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinMixedAliasPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(20)}},
	})

	mismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(9)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched compound alias candidates = %v, want empty", got)
	}

	aliasFilterMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(9)}}
	got := CollectCandidatesForTable(idx, 1, aliasFilterMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-filter compound candidates = %v, want [%v]", got, hash)
	}

	aliasConditionMatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 1, aliasConditionMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-condition compound candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinRepeatedMixedAliasUsesDeltaOppositeRows(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := repeatedMultiJoinMixedAliasPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{11},
		QueryID:    110,
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
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(88)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(99)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping repeated-mixed same-tx candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(77)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(77)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping repeated-mixed same-tx candidates = %v, want only %v", got, hash)
	}
}

func TestCollectCandidatesMultiJoinRepeatedCrossAliasFilterPrunesMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := repeatedMultiJoinCrossAliasFilterPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(100), types.NewUint64(20)}},
	})

	mismatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(9)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched cross-alias filter candidates = %v, want empty", got)
	}

	aliasFilterMatch := []types.ProductValue{{types.NewUint64(7), types.NewUint64(9)}}
	got := CollectCandidatesForTable(idx, 1, aliasFilterMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-filter cross-alias candidates = %v, want [%v]", got, hash)
	}

	aliasConditionMatch := []types.ProductValue{{types.NewUint64(8), types.NewUint64(20)}}
	got = CollectCandidatesForTable(idx, 1, aliasConditionMatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("alias-filter-edge candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinRepeatedCrossAliasFilterUsesDeltaOppositeRows(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := repeatedMultiJoinCrossAliasFilterPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{12},
		QueryID:    120,
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
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(88)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(99)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping cross-alias same-tx candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(8), types.NewUint64(77)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(77)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping cross-alias same-tx candidates = %v, want only %v", got, hash)
	}
}

func TestCollectCandidatesMultiJoinRepeatedSelfAliasFilterUsesDeltaRows(t *testing.T) {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	s.addTable(1, cols, 0, 1)
	s.addTable(2, cols, 1)
	mgr := NewManager(s, s)
	pred := repeatedMultiJoinSelfAliasFilterPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{13},
		QueryID:    130,
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
				{types.NewUint64(7), types.NewUint64(70)},
				{types.NewUint64(8), types.NewUint64(80)},
			}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping self-alias same-tx candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{
				{types.NewUint64(77), types.NewUint64(10)},
				{types.NewUint64(8), types.NewUint64(77)},
			}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping self-alias same-tx candidates = %v, want only %v", got, hash)
	}
}

func TestCollectCandidatesMultiJoinLocalFilterPrunesMismatch(t *testing.T) {
	idx := NewPruningIndexes()
	pred := multiJoinTestPredicate()
	hash := ComputeQueryHash(pred, nil)
	PlaceSubscription(idx, pred, hash)

	mismatch := []types.ProductValue{{types.NewUint64(99), types.NewUint64(20)}}
	if got := CollectCandidatesForTable(idx, 3, mismatch, nil, nil); len(got) != 0 {
		t.Fatalf("mismatched local MultiJoin filter candidates = %v, want empty", got)
	}

	match := []types.ProductValue{{types.NewUint64(301), types.NewUint64(20)}}
	got := CollectCandidatesForTable(idx, 3, match, nil, nil)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching local MultiJoin filter candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinConditionExistencePrunesCommittedMismatch(t *testing.T) {
	s := multiJoinTestSchema()
	idx := NewPruningIndexes()
	pred := multiJoinUnfilteredTestPredicate()
	hash := ComputeQueryHash(pred, nil)
	placeSubscriptionForResolver(idx, pred, hash, s)
	committed := multiJoinCommitted(false)

	mismatch := []types.ProductValue{{types.NewUint64(500), types.NewUint64(999)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched multi-join condition candidates = %v, want empty", got)
	}

	match := []types.ProductValue{{types.NewUint64(500), types.NewUint64(20)}}
	got := CollectCandidatesForTable(idx, 1, match, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("matching multi-join condition candidates = %v, want [%v]", got, hash)
	}
}

func TestCollectCandidatesMultiJoinConditionExistenceUsesDeltaOppositeRows(t *testing.T) {
	s := multiJoinTestSchema()
	mgr := NewManager(s, s)
	pred := multiJoinUnfilteredTestPredicate()
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{9},
		QueryID:    90,
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
			1: {Inserts: []types.ProductValue{{types.NewUint64(1), types.NewUint64(77)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(88)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping same-tx candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(1), types.NewUint64(77)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(77)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping same-tx candidates = %v, want only %v", got, hash)
	}
}

func requireSingleFanoutUpdate(t *testing.T, msg FanOutMessage, connID types.ConnectionID) SubscriptionUpdate {
	t.Helper()
	updates := msg.Fanout[connID]
	if len(updates) != 1 {
		t.Fatalf("fanout[%x] = %v, want one update", connID[:4], msg.Fanout)
	}
	return updates[0]
}

func requireMultiJoinIncrementalMatchesFresh(t *testing.T, s *fakeSchema, pred MultiJoin, before, after *mockCommitted, cs *store.Changeset) deltaBag {
	t.Helper()
	connID := types.ConnectionID{14}
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    140,
		Predicates: []Predicate{pred},
	}, before)
	if err != nil {
		t.Fatalf("RegisterSet before: %v", err)
	}
	initialRows := rowsFromUpdates(res.Update)

	mgr.EvalAndBroadcast(types.TxID(cs.TxID), cs, after, PostCommitMeta{})
	msg := <-inbox
	var delta deltaBag
	for _, update := range msg.Fanout[connID] {
		delta.inserts = append(delta.inserts, update.Inserts...)
		delta.deletes = append(delta.deletes, update.Deletes...)
	}
	incremental := applyDelta(initialRows, delta.inserts, delta.deletes)
	freshRows := multiJoinFreshRows(t, s, pred, after)
	if !bagEqual(incremental, freshRows) {
		t.Fatalf("incremental rows = %v, want fresh rows %v (delta inserts=%v deletes=%v)", incremental, freshRows, delta.inserts, delta.deletes)
	}
	return delta
}

type multiJoinAggregateCase struct {
	queryID   uint32
	aggregate *Aggregate
	column    string
}

type aggregateDelta struct {
	before uint64
	after  uint64
}

func requireMultiJoinAggregateDeltasMatchFresh(
	t *testing.T,
	s *fakeSchema,
	pred MultiJoin,
	aggregates []multiJoinAggregateCase,
	before, after *mockCommitted,
	cs *store.Changeset,
) map[uint32]SubscriptionUpdate {
	t.Helper()
	connID := types.ConnectionID{16}
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	initial := registerMultiJoinAggregateRowsByQueryID(t, mgr, connID, pred, aggregates, before)

	mgr.EvalAndBroadcast(types.TxID(cs.TxID), cs, after, PostCommitMeta{})
	msg := <-inbox
	updates := aggregateUpdatesByQueryID(msg.Fanout[connID])

	fresh := registerMultiJoinAggregateRowsByQueryID(t, NewManager(s, s), types.ConnectionID{17}, pred, aggregates, after)
	for _, agg := range aggregates {
		update := updates[agg.queryID]
		incremental := applyDelta(initial[agg.queryID], update.Inserts, update.Deletes)
		if !bagEqual(incremental, fresh[agg.queryID]) {
			t.Fatalf("query %d aggregate rows = %v, want fresh rows %v (delta inserts=%v deletes=%v)", agg.queryID, incremental, fresh[agg.queryID], update.Inserts, update.Deletes)
		}
	}
	return updates
}

func registerMultiJoinAggregateRowsByQueryID(
	t *testing.T,
	mgr *Manager,
	connID types.ConnectionID,
	pred MultiJoin,
	aggregates []multiJoinAggregateCase,
	view *mockCommitted,
) map[uint32][]types.ProductValue {
	t.Helper()
	out := make(map[uint32][]types.ProductValue, len(aggregates))
	for _, agg := range aggregates {
		res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID:     connID,
			QueryID:    agg.queryID,
			Predicates: []Predicate{pred},
			Aggregates: []*Aggregate{agg.aggregate},
		}, view)
		if err != nil {
			t.Fatalf("RegisterSet multi-join aggregate queryID=%d: %v", agg.queryID, err)
		}
		out[agg.queryID] = rowsFromUpdates(res.Update)
	}
	return out
}

func multiJoinFreshRows(t *testing.T, s *fakeSchema, pred MultiJoin, view *mockCommitted) []types.ProductValue {
	t.Helper()
	mgr := NewManager(s, s)
	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{15},
		QueryID:    150,
		Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet fresh: %v", err)
	}
	return rowsFromUpdates(res.Update)
}

func rowsFromUpdates(updates []SubscriptionUpdate) []types.ProductValue {
	var rows []types.ProductValue
	for _, update := range updates {
		rows = append(rows, update.Inserts...)
	}
	return rows
}

func productRowsHaveUint64IDs(rows []types.ProductValue, ids ...uint64) bool {
	if len(rows) != len(ids) {
		return false
	}
	want := make(map[uint64]int, len(ids))
	for _, id := range ids {
		want[id]++
	}
	for _, row := range rows {
		if len(row) == 0 {
			return false
		}
		id := row[0].AsUint64()
		if want[id] == 0 {
			return false
		}
		want[id]--
	}
	return true
}
