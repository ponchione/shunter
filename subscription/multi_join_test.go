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

func multiJoinCommitted(includeExtraR bool) *mockCommitted {
	s := multiJoinTestSchema()
	contents := map[TableID][]types.ProductValue{
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
