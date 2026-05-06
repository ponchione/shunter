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
		{
			name:      "join",
			pred:      Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0},
			aggregate: countStarAggregate(),
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
