package subscription

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func countStarAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateCount,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64},
	}
}

func countBodyAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateCount,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 1, Name: "body", Type: types.KindString, Nullable: true},
			Table:  1,
			Column: 1,
		},
	}
}

func countDistinctBodyAggregate() *Aggregate {
	agg := countBodyAggregate()
	agg.Distinct = true
	return agg
}

func sumIDAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateSum,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "total", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  1,
			Column: 0,
		},
	}
}

func countJoinRHSIDAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateCount,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  joinRHS,
			Column: 0,
		},
	}
}

func countDistinctJoinLHSIDAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateCount,
		Distinct:     true,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  joinLHS,
			Column: 0,
		},
	}
}

func sumJoinRHSIDAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateSum,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "total", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  joinRHS,
			Column: 0,
		},
	}
}

func joinCountAggregateSchema() *fakeSchema {
	s := newFakeSchema()
	s.addTable(joinLHS, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
	}, joinLHSCol)
	s.addTable(joinRHS, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
	}, joinRHSCol)
	return s
}

func TestRegisterSetAggregateReturnsOneInitialRow(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewNull(types.KindString)},
			{types.NewUint64(3), types.NewString("b")},
		},
	})
	mgr := NewManager(s, s)

	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    10,
		Predicates: []Predicate{AllRows{Table: 1}},
		Aggregates: []*Aggregate{countBodyAggregate()},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet aggregate = %v", err)
	}
	if len(res.Update) != 1 {
		t.Fatalf("initial update count = %d, want 1", len(res.Update))
	}
	update := res.Update[0]
	if len(update.Columns) != 1 || update.Columns[0].Name != "n" || update.Columns[0].Type != types.KindUint64 {
		t.Fatalf("aggregate columns = %#v, want n Uint64", update.Columns)
	}
	if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != 2 {
		t.Fatalf("aggregate initial rows = %#v, want count 2", update.Inserts)
	}
}

func TestRegisterSetSumAggregateReturnsOneInitialRow(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewNull(types.KindString)},
			{types.NewUint64(3), types.NewString("b")},
		},
	})
	mgr := NewManager(s, s)

	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    20,
		Predicates: []Predicate{AllRows{Table: 1}},
		Aggregates: []*Aggregate{sumIDAggregate()},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet SUM aggregate = %v", err)
	}
	if len(res.Update) != 1 {
		t.Fatalf("initial update count = %d, want 1", len(res.Update))
	}
	update := res.Update[0]
	if len(update.Columns) != 1 || update.Columns[0].Name != "total" || update.Columns[0].Type != types.KindUint64 {
		t.Fatalf("aggregate columns = %#v, want total Uint64", update.Columns)
	}
	if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != 6 {
		t.Fatalf("SUM aggregate initial rows = %#v, want total 6", update.Inserts)
	}
}

func TestRegisterSetCountDistinctAggregateReturnsOneInitialRow(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewNull(types.KindString)},
			{types.NewUint64(3), types.NewString("b")},
			{types.NewUint64(4), types.NewString("a")},
		},
	})
	mgr := NewManager(s, s)

	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    22,
		Predicates: []Predicate{AllRows{Table: 1}},
		Aggregates: []*Aggregate{countDistinctBodyAggregate()},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet COUNT(DISTINCT) aggregate = %v", err)
	}
	if len(res.Update) != 1 {
		t.Fatalf("initial update count = %d, want 1", len(res.Update))
	}
	update := res.Update[0]
	if len(update.Columns) != 1 || update.Columns[0].Name != "n" || update.Columns[0].Type != types.KindUint64 {
		t.Fatalf("aggregate columns = %#v, want n Uint64", update.Columns)
	}
	if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != 2 {
		t.Fatalf("COUNT(DISTINCT) aggregate initial rows = %#v, want distinct count 2", update.Inserts)
	}
}

func TestRegisterSetJoinCountAggregateReturnsOneInitialRow(t *testing.T) {
	s := joinCountAggregateSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(101), types.NewUint64(1)},
			{types.NewUint64(200), types.NewUint64(2)},
		},
	})
	mgr := NewManager(s, s)

	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 24,
		Predicates: []Predicate{Join{
			Left: joinLHS, Right: joinRHS,
			LeftCol: joinLHSCol, RightCol: joinRHSCol,
		}},
		Aggregates: []*Aggregate{countStarAggregate()},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet join COUNT(*) aggregate = %v", err)
	}
	if len(res.Update) != 1 {
		t.Fatalf("initial update count = %d, want 1", len(res.Update))
	}
	update := res.Update[0]
	if update.TableID != joinLHS || len(update.Columns) != 1 || update.Columns[0].Name != "n" || update.Columns[0].Type != types.KindUint64 {
		t.Fatalf("join aggregate update shape = table %d columns %#v, want lhs/n Uint64", update.TableID, update.Columns)
	}
	if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != 3 {
		t.Fatalf("join COUNT(*) aggregate initial rows = %#v, want count 3", update.Inserts)
	}
}

func TestRegisterSetJoinColumnAggregatesReturnInitialRows(t *testing.T) {
	s := joinCountAggregateSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(101), types.NewUint64(1)},
			{types.NewUint64(200), types.NewUint64(2)},
		},
	})
	pred := Join{
		Left: joinLHS, Right: joinRHS,
		LeftCol: joinLHSCol, RightCol: joinRHSCol,
	}
	tests := []struct {
		name      string
		queryID   uint32
		aggregate *Aggregate
		want      uint64
		column    string
	}{
		{name: "count column", queryID: 26, aggregate: countJoinRHSIDAggregate(), want: 3, column: "n"},
		{name: "count distinct", queryID: 27, aggregate: countDistinctJoinLHSIDAggregate(), want: 2, column: "n"},
		{name: "sum", queryID: 28, aggregate: sumJoinRHSIDAggregate(), want: 401, column: "total"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager(s, s)
			res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
				ConnID:     types.ConnectionID{1},
				QueryID:    tt.queryID,
				Predicates: []Predicate{pred},
				Aggregates: []*Aggregate{tt.aggregate},
			}, view)
			if err != nil {
				t.Fatalf("RegisterSet join aggregate = %v", err)
			}
			if len(res.Update) != 1 {
				t.Fatalf("initial update count = %d, want 1", len(res.Update))
			}
			update := res.Update[0]
			if update.TableID != joinLHS || len(update.Columns) != 1 || update.Columns[0].Name != tt.column {
				t.Fatalf("join aggregate update shape = table %d columns %#v, want lhs/%s", update.TableID, update.Columns, tt.column)
			}
			if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != tt.want {
				t.Fatalf("join aggregate initial rows = %#v, want %d", update.Inserts, tt.want)
			}
		})
	}
}

func TestRegisterSetCrossJoinAggregatesReturnInitialRows(t *testing.T) {
	s := joinCountAggregateSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(101), types.NewUint64(1)},
			{types.NewUint64(200), types.NewUint64(2)},
		},
	})
	pred := CrossJoin{Left: joinLHS, Right: joinRHS}
	tests := []struct {
		name      string
		queryID   uint32
		aggregate *Aggregate
		want      uint64
		column    string
	}{
		{name: "count star", queryID: 29, aggregate: countStarAggregate(), want: 6, column: "n"},
		{name: "count column", queryID: 30, aggregate: countJoinRHSIDAggregate(), want: 6, column: "n"},
		{name: "count distinct", queryID: 31, aggregate: countDistinctJoinLHSIDAggregate(), want: 2, column: "n"},
		{name: "sum", queryID: 32, aggregate: sumJoinRHSIDAggregate(), want: 802, column: "total"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager(s, s)
			res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
				ConnID:     types.ConnectionID{1},
				QueryID:    tt.queryID,
				Predicates: []Predicate{pred},
				Aggregates: []*Aggregate{tt.aggregate},
			}, view)
			if err != nil {
				t.Fatalf("RegisterSet cross aggregate = %v", err)
			}
			if len(res.Update) != 1 {
				t.Fatalf("initial update count = %d, want 1", len(res.Update))
			}
			update := res.Update[0]
			if update.TableID != joinLHS || len(update.Columns) != 1 || update.Columns[0].Name != tt.column {
				t.Fatalf("cross aggregate update shape = table %d columns %#v, want lhs/%s", update.TableID, update.Columns, tt.column)
			}
			if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != tt.want {
				t.Fatalf("cross aggregate initial rows = %#v, want %d", update.Inserts, tt.want)
			}
		})
	}
}

func TestEvalAggregateEmitsDeleteOldInsertNewOnlyWhenValueChanges(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	before := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewNull(types.KindString)},
			{types.NewUint64(3), types.NewString("b")},
		},
	})
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    11,
		Predicates: []Predicate{AllRows{Table: 1}},
		Aggregates: []*Aggregate{countBodyAggregate()},
	}, before); err != nil {
		t.Fatalf("RegisterSet aggregate = %v", err)
	}

	afterChanged := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(3), types.NewString("b")},
			{types.NewUint64(4), types.NewString("c")},
		},
	})
	changed := &store.Changeset{TxID: 1, Tables: map[TableID]*store.TableChangeset{
		1: {
			TableID: 1,
			Inserts: []types.ProductValue{
				{types.NewUint64(4), types.NewString("c")},
			},
			Deletes: []types.ProductValue{
				{types.NewUint64(2), types.NewNull(types.KindString)},
			},
		},
	}}
	mgr.EvalAndBroadcast(types.TxID(1), changed, afterChanged, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[connID]
	if len(updates) != 1 {
		t.Fatalf("aggregate fanout updates = %+v, want one update", updates)
	}
	update := updates[0]
	if len(update.Deletes) != 1 || update.Deletes[0][0].AsUint64() != 2 {
		t.Fatalf("aggregate deletes = %#v, want old count 2", update.Deletes)
	}
	if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != 3 {
		t.Fatalf("aggregate inserts = %#v, want new count 3", update.Inserts)
	}

	afterUnchanged := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(3), types.NewString("b")},
			{types.NewUint64(4), types.NewString("c")},
			{types.NewUint64(5), types.NewNull(types.KindString)},
		},
	})
	unchanged := simpleChangeset(1, []types.ProductValue{{types.NewUint64(5), types.NewNull(types.KindString)}}, nil)
	mgr.EvalAndBroadcast(types.TxID(2), unchanged, afterUnchanged, PostCommitMeta{})
	msg = <-inbox
	if len(msg.Fanout[connID]) != 0 {
		t.Fatalf("unchanged aggregate fanout = %+v, want no updates", msg.Fanout)
	}
}

func TestEvalJoinCountAggregateEmitsDeleteOldInsertNewOnlyWhenValueChanges(t *testing.T) {
	s := joinCountAggregateSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	before := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
		},
	})
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:  connID,
		QueryID: 25,
		Predicates: []Predicate{Join{
			Left: joinLHS, Right: joinRHS,
			LeftCol: joinLHSCol, RightCol: joinRHSCol,
		}},
		Aggregates: []*Aggregate{countStarAggregate()},
	}, before); err != nil {
		t.Fatalf("RegisterSet join COUNT(*) aggregate = %v", err)
	}

	after := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(101), types.NewUint64(1)},
		},
	})
	changed := joinChangeset(
		joinLHS, nil, nil,
		joinRHS, []types.ProductValue{{types.NewUint64(101), types.NewUint64(1)}}, nil,
	)
	mgr.EvalAndBroadcast(types.TxID(1), changed, after, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[connID]
	if len(updates) != 1 {
		t.Fatalf("join COUNT(*) aggregate fanout updates = %+v, want one update", updates)
	}
	update := updates[0]
	if len(update.Deletes) != 1 || update.Deletes[0][0].AsUint64() != 1 {
		t.Fatalf("join COUNT(*) aggregate deletes = %#v, want old count 1", update.Deletes)
	}
	if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != 2 {
		t.Fatalf("join COUNT(*) aggregate inserts = %#v, want new count 2", update.Inserts)
	}
}

func TestEvalJoinColumnAggregatesEmitChanges(t *testing.T) {
	s := joinCountAggregateSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	pred := Join{
		Left: joinLHS, Right: joinRHS,
		LeftCol: joinLHSCol, RightCol: joinRHSCol,
	}
	before := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(200), types.NewUint64(2)},
		},
	})
	for _, req := range []struct {
		queryID   uint32
		aggregate *Aggregate
	}{
		{26, countJoinRHSIDAggregate()},
		{27, countDistinctJoinLHSIDAggregate()},
		{28, sumJoinRHSIDAggregate()},
	} {
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID:     connID,
			QueryID:    req.queryID,
			Predicates: []Predicate{pred},
			Aggregates: []*Aggregate{req.aggregate},
		}, before); err != nil {
			t.Fatalf("RegisterSet join aggregate queryID=%d: %v", req.queryID, err)
		}
	}

	afterRightInsert := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(101), types.NewUint64(1)},
			{types.NewUint64(200), types.NewUint64(2)},
		},
	})
	mgr.EvalAndBroadcast(types.TxID(1), joinChangeset(
		joinLHS, nil, nil,
		joinRHS, []types.ProductValue{{types.NewUint64(101), types.NewUint64(1)}}, nil,
	), afterRightInsert, PostCommitMeta{})
	first := aggregateUpdatesByQueryID((<-inbox).Fanout[connID])
	requireAggregateDelta(t, first[26], 1, 2, "COUNT(s.id)")
	requireAggregateDelta(t, first[28], 100, 201, "SUM(s.id)")
	if _, ok := first[27]; ok {
		t.Fatalf("COUNT(DISTINCT t.id) changed on duplicate joined left id: %+v", first[27])
	}

	afterLeftInsert := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(101), types.NewUint64(1)},
			{types.NewUint64(200), types.NewUint64(2)},
		},
	})
	mgr.EvalAndBroadcast(types.TxID(2), joinChangeset(
		joinLHS, []types.ProductValue{{types.NewUint64(2), types.NewString("b")}}, nil,
		joinRHS, nil, nil,
	), afterLeftInsert, PostCommitMeta{})
	second := aggregateUpdatesByQueryID((<-inbox).Fanout[connID])
	requireAggregateDelta(t, second[26], 2, 3, "COUNT(s.id)")
	requireAggregateDelta(t, second[27], 1, 2, "COUNT(DISTINCT t.id)")
	requireAggregateDelta(t, second[28], 201, 401, "SUM(s.id)")
}

func TestEvalCrossJoinAggregatesEmitChanges(t *testing.T) {
	s := joinCountAggregateSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	pred := CrossJoin{Left: joinLHS, Right: joinRHS}
	before := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(200), types.NewUint64(2)},
		},
	})
	for _, req := range []struct {
		queryID   uint32
		aggregate *Aggregate
	}{
		{29, countStarAggregate()},
		{30, countJoinRHSIDAggregate()},
		{31, countDistinctJoinLHSIDAggregate()},
		{32, sumJoinRHSIDAggregate()},
	} {
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID:     connID,
			QueryID:    req.queryID,
			Predicates: []Predicate{pred},
			Aggregates: []*Aggregate{req.aggregate},
		}, before); err != nil {
			t.Fatalf("RegisterSet cross aggregate queryID=%d: %v", req.queryID, err)
		}
	}

	afterRightInsert := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(200), types.NewUint64(2)},
			{types.NewUint64(300), types.NewUint64(3)},
		},
	})
	mgr.EvalAndBroadcast(types.TxID(1), joinChangeset(
		joinLHS, nil, nil,
		joinRHS, []types.ProductValue{{types.NewUint64(300), types.NewUint64(3)}}, nil,
	), afterRightInsert, PostCommitMeta{})
	first := aggregateUpdatesByQueryID((<-inbox).Fanout[connID])
	requireAggregateDelta(t, first[29], 2, 3, "COUNT(*)")
	requireAggregateDelta(t, first[30], 2, 3, "COUNT(s.id)")
	requireAggregateDelta(t, first[32], 300, 600, "SUM(s.id)")
	if _, ok := first[31]; ok {
		t.Fatalf("COUNT(DISTINCT t.id) changed on right-side cross insert: %+v", first[31])
	}

	afterLeftInsert := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
		joinRHS: {
			{types.NewUint64(100), types.NewUint64(1)},
			{types.NewUint64(200), types.NewUint64(2)},
			{types.NewUint64(300), types.NewUint64(3)},
		},
	})
	mgr.EvalAndBroadcast(types.TxID(2), joinChangeset(
		joinLHS, []types.ProductValue{{types.NewUint64(2), types.NewString("b")}}, nil,
		joinRHS, nil, nil,
	), afterLeftInsert, PostCommitMeta{})
	second := aggregateUpdatesByQueryID((<-inbox).Fanout[connID])
	requireAggregateDelta(t, second[29], 3, 6, "COUNT(*)")
	requireAggregateDelta(t, second[30], 3, 6, "COUNT(s.id)")
	requireAggregateDelta(t, second[31], 1, 2, "COUNT(DISTINCT t.id)")
	requireAggregateDelta(t, second[32], 600, 1200, "SUM(s.id)")
}

func aggregateUpdatesByQueryID(updates []SubscriptionUpdate) map[uint32]SubscriptionUpdate {
	out := make(map[uint32]SubscriptionUpdate, len(updates))
	for _, update := range updates {
		out[update.QueryID] = update
	}
	return out
}

func requireAggregateDelta(t *testing.T, update SubscriptionUpdate, wantDelete, wantInsert uint64, label string) {
	t.Helper()
	if len(update.Deletes) != 1 || len(update.Deletes[0]) != 1 || update.Deletes[0][0].AsUint64() != wantDelete {
		t.Fatalf("%s deletes = %#v, want %d", label, update.Deletes, wantDelete)
	}
	if len(update.Inserts) != 1 || len(update.Inserts[0]) != 1 || update.Inserts[0][0].AsUint64() != wantInsert {
		t.Fatalf("%s inserts = %#v, want %d", label, update.Inserts, wantInsert)
	}
}

func TestEvalCountDistinctAggregateEmitsDeleteOldInsertNewOnlyWhenValueChanges(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	before := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewNull(types.KindString)},
			{types.NewUint64(3), types.NewString("b")},
			{types.NewUint64(4), types.NewString("a")},
		},
	})
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    23,
		Predicates: []Predicate{AllRows{Table: 1}},
		Aggregates: []*Aggregate{countDistinctBodyAggregate()},
	}, before); err != nil {
		t.Fatalf("RegisterSet COUNT(DISTINCT) aggregate = %v", err)
	}

	afterChanged := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(3), types.NewString("b")},
			{types.NewUint64(4), types.NewString("a")},
			{types.NewUint64(5), types.NewString("c")},
		},
	})
	changed := &store.Changeset{TxID: 1, Tables: map[TableID]*store.TableChangeset{
		1: {
			TableID: 1,
			Inserts: []types.ProductValue{
				{types.NewUint64(5), types.NewString("c")},
			},
			Deletes: []types.ProductValue{
				{types.NewUint64(2), types.NewNull(types.KindString)},
			},
		},
	}}
	mgr.EvalAndBroadcast(types.TxID(1), changed, afterChanged, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[connID]
	if len(updates) != 1 {
		t.Fatalf("COUNT(DISTINCT) aggregate fanout updates = %+v, want one update", updates)
	}
	update := updates[0]
	if len(update.Deletes) != 1 || update.Deletes[0][0].AsUint64() != 2 {
		t.Fatalf("COUNT(DISTINCT) aggregate deletes = %#v, want old distinct count 2", update.Deletes)
	}
	if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != 3 {
		t.Fatalf("COUNT(DISTINCT) aggregate inserts = %#v, want new distinct count 3", update.Inserts)
	}

	afterUnchanged := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(3), types.NewString("b")},
			{types.NewUint64(4), types.NewString("a")},
			{types.NewUint64(5), types.NewString("c")},
			{types.NewUint64(6), types.NewString("c")},
		},
	})
	unchanged := simpleChangeset(1, []types.ProductValue{{types.NewUint64(6), types.NewString("c")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(2), unchanged, afterUnchanged, PostCommitMeta{})
	msg = <-inbox
	if len(msg.Fanout[connID]) != 0 {
		t.Fatalf("unchanged COUNT(DISTINCT) aggregate fanout = %+v, want no updates", msg.Fanout)
	}
}

func TestEvalSumAggregateEmitsDeleteOldInsertNewOnlyWhenValueChanges(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	before := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewNull(types.KindString)},
			{types.NewUint64(3), types.NewString("b")},
		},
	})
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    21,
		Predicates: []Predicate{AllRows{Table: 1}},
		Aggregates: []*Aggregate{sumIDAggregate()},
	}, before); err != nil {
		t.Fatalf("RegisterSet SUM aggregate = %v", err)
	}

	afterChanged := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(3), types.NewString("b")},
			{types.NewUint64(4), types.NewString("c")},
		},
	})
	changed := &store.Changeset{TxID: 1, Tables: map[TableID]*store.TableChangeset{
		1: {
			TableID: 1,
			Inserts: []types.ProductValue{
				{types.NewUint64(4), types.NewString("c")},
			},
			Deletes: []types.ProductValue{
				{types.NewUint64(2), types.NewNull(types.KindString)},
			},
		},
	}}
	mgr.EvalAndBroadcast(types.TxID(1), changed, afterChanged, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[connID]
	if len(updates) != 1 {
		t.Fatalf("SUM aggregate fanout updates = %+v, want one update", updates)
	}
	update := updates[0]
	if len(update.Deletes) != 1 || update.Deletes[0][0].AsUint64() != 6 {
		t.Fatalf("SUM aggregate deletes = %#v, want old total 6", update.Deletes)
	}
	if len(update.Inserts) != 1 || update.Inserts[0][0].AsUint64() != 8 {
		t.Fatalf("SUM aggregate inserts = %#v, want new total 8", update.Inserts)
	}

	afterUnchanged := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(3), types.NewString("b")},
			{types.NewUint64(4), types.NewString("d")},
		},
	})
	unchanged := &store.Changeset{TxID: 2, Tables: map[TableID]*store.TableChangeset{
		1: {
			TableID: 1,
			Inserts: []types.ProductValue{
				{types.NewUint64(4), types.NewString("d")},
			},
			Deletes: []types.ProductValue{
				{types.NewUint64(4), types.NewString("c")},
			},
		},
	}}
	mgr.EvalAndBroadcast(types.TxID(2), unchanged, afterUnchanged, PostCommitMeta{})
	msg = <-inbox
	if len(msg.Fanout[connID]) != 0 {
		t.Fatalf("unchanged SUM aggregate fanout = %+v, want no updates", msg.Fanout)
	}
}

func TestUnregisterSetAggregateDeletesCurrentRow(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
	})
	mgr := NewManager(s, s)
	connID := types.ConnectionID{1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    12,
		Predicates: []Predicate{AllRows{Table: 1}},
		Aggregates: []*Aggregate{countStarAggregate()},
	}, view); err != nil {
		t.Fatalf("RegisterSet aggregate = %v", err)
	}

	res, err := mgr.UnregisterSet(connID, 12, view)
	if err != nil {
		t.Fatalf("UnregisterSet aggregate = %v", err)
	}
	if len(res.Update) != 1 {
		t.Fatalf("unregister update count = %d, want 1", len(res.Update))
	}
	if len(res.Update[0].Deletes) != 1 || res.Update[0].Deletes[0][0].AsUint64() != 2 {
		t.Fatalf("unregister aggregate deletes = %#v, want count 2", res.Update[0].Deletes)
	}
	if len(res.Update[0].Inserts) != 0 {
		t.Fatalf("unregister aggregate inserts = %#v, want none", res.Update[0].Inserts)
	}
}

func TestRegisterSetAggregateDoesNotDedupWithTableShape(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1), types.NewString("a")}},
	})
	mgr := NewManager(s, s)
	pred := AllRows{Table: 1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    13,
		Predicates: []Predicate{pred},
	}, view); err != nil {
		t.Fatalf("RegisterSet table = %v", err)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{2},
		QueryID:    14,
		Predicates: []Predicate{pred},
		Aggregates: []*Aggregate{countStarAggregate()},
	}, view); err != nil {
		t.Fatalf("RegisterSet aggregate = %v", err)
	}
	if got := len(mgr.registry.byHash); got != 2 {
		t.Fatalf("query-state count = %d, want table and aggregate states", got)
	}
}

func TestValidateAggregateRejectsUnsupportedLiveShapes(t *testing.T) {
	s := testSchema()
	tests := []struct {
		name      string
		pred      Predicate
		aggregate *Aggregate
	}{
		{
			name:      "count distinct star",
			pred:      AllRows{Table: 1},
			aggregate: &Aggregate{Func: AggregateCount, Distinct: true, ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64}},
		},
		{
			name:      "sum distinct",
			pred:      AllRows{Table: 1},
			aggregate: &Aggregate{Func: AggregateSum, Distinct: true, ResultColumn: schema.ColumnSchema{Index: 0, Name: "total", Type: types.KindUint64}, Argument: sumIDAggregate().Argument},
		},
		{
			name:      "unsupported function",
			pred:      AllRows{Table: 1},
			aggregate: &Aggregate{Func: AggregateFunc("AVG"), ResultColumn: schema.ColumnSchema{Index: 0, Name: "avg", Type: types.KindUint64}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateAggregate(tt.pred, tt.aggregate, s); err == nil {
				t.Fatal("ValidateAggregate error = nil, want rejection")
			}
		})
	}
}
