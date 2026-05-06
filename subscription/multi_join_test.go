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
