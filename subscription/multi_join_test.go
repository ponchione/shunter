package subscription

import (
	"testing"

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

func TestMultiJoinPlacementUsesTableFallbackForEveryReferencedTable(t *testing.T) {
	idx := NewPruningIndexes()
	pred := multiJoinTestPredicate()
	hash := ComputeQueryHash(pred, nil)
	PlaceSubscription(idx, pred, hash)
	for _, table := range []TableID{1, 2, 3} {
		got := idx.Table.Lookup(table)
		if len(got) != 1 || got[0] != hash {
			t.Fatalf("TableIndex[%d] = %v, want [%v]", table, got, hash)
		}
	}
	RemoveSubscription(idx, pred, hash)
	for _, table := range []TableID{1, 2, 3} {
		if got := idx.Table.Lookup(table); len(got) != 0 {
			t.Fatalf("TableIndex[%d] after remove = %v, want empty", table, got)
		}
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
