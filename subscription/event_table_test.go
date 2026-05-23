package subscription

import (
	"fmt"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

type eventAwareSchema struct {
	*fakeSchema
	events map[TableID]bool
}

func newEventAwareSchema() *eventAwareSchema {
	return &eventAwareSchema{
		fakeSchema: newFakeSchema(),
		events:     make(map[TableID]bool),
	}
}

func (s *eventAwareSchema) addEventTable(t TableID, cols map[ColID]types.ValueKind, indexed ...ColID) {
	s.addTable(t, cols, indexed...)
	s.events[t] = true
}

func (s *eventAwareSchema) Table(t TableID) (*schema.TableSchema, bool) {
	cols, ok := s.tables[t]
	if !ok {
		return nil, false
	}
	out := &schema.TableSchema{
		ID:      t,
		Name:    s.TableName(t),
		IsEvent: s.events[t],
		Columns: make([]schema.ColumnSchema, len(cols)),
	}
	for i := range out.Columns {
		col := ColID(i)
		out.Columns[i] = schema.ColumnSchema{Index: i, Name: fmt.Sprintf("c%d", i), Type: cols[col]}
	}
	return out, true
}

func TestEventTableNoPrimaryKeyProjectionAndDuplicateRowsFanOut(t *testing.T) {
	s := newEventAwareSchema()
	s.addEventTable(1, map[ColID]types.ValueKind{0: types.KindString, 1: types.KindUint64})
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	limit := uint64(1)
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    10,
		Predicates: []Predicate{AllRows{Table: 1}},
		ProjectionColumns: [][]ProjectionColumn{{
			{Table: 1, Column: 0, Schema: schema.ColumnSchema{Index: 0, Name: "body", Type: types.KindString}},
		}},
		OrderByColumns: [][]OrderByColumn{{
			{Table: 1, Column: 0, Schema: schema.ColumnSchema{Index: 0, Name: "body", Type: types.KindString}},
		}},
		Limits: []*uint64{&limit},
	}, buildMockCommitted(s.fakeSchema, nil)); err != nil {
		t.Fatalf("RegisterSet event projection = %v", err)
	}

	row := types.ProductValue{types.NewString("same"), types.NewUint64(1)}
	mgr.EvalAndBroadcast(types.TxID(1), simpleChangeset(1, []types.ProductValue{row, row}, nil), buildMockCommitted(s.fakeSchema, nil), PostCommitMeta{})
	updates := (<-inbox).Fanout[connID]
	if len(updates) != 1 {
		t.Fatalf("event fanout updates = %+v, want one update", updates)
	}
	want := []types.ProductValue{{types.NewString("same")}, {types.NewString("same")}}
	requireProductRowsEqual(t, updates[0].Inserts, want)
}

func TestEventTableDuplicateRowsAcrossTransactionsFanOutEachCommit(t *testing.T) {
	s := newEventAwareSchema()
	s.addEventTable(1, map[ColID]types.ValueKind{0: types.KindString})
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    11,
		Predicates: []Predicate{AllRows{Table: 1}},
	}, buildMockCommitted(s.fakeSchema, nil)); err != nil {
		t.Fatalf("RegisterSet event = %v", err)
	}

	row := types.ProductValue{types.NewString("repeat")}
	for tx := types.TxID(1); tx <= 2; tx++ {
		mgr.EvalAndBroadcast(tx, simpleChangeset(1, []types.ProductValue{row}, nil), buildMockCommitted(s.fakeSchema, nil), PostCommitMeta{})
		updates := (<-inbox).Fanout[connID]
		if len(updates) != 1 || len(updates[0].Inserts) != 1 || !updates[0].Inserts[0].Equal(row) {
			t.Fatalf("tx %d event duplicate fanout = %+v, want repeated insert", tx, updates)
		}
	}
}

func TestEventTableJoinWithPersistentInsertSameChangesetFanOut(t *testing.T) {
	s := newEventAwareSchema()
	s.addEventTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	pred := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    12,
		Predicates: []Predicate{pred},
	}, buildMockCommitted(s.fakeSchema, nil)); err != nil {
		t.Fatalf("RegisterSet event join = %v", err)
	}

	eventRow := types.ProductValue{types.NewUint64(7), types.NewString("event")}
	persistentRow := types.ProductValue{types.NewUint64(100), types.NewUint64(7)}
	cs := &store.Changeset{TxID: 1, Tables: map[TableID]*store.TableChangeset{
		1: {TableID: 1, Inserts: []types.ProductValue{eventRow}},
		2: {TableID: 2, Inserts: []types.ProductValue{persistentRow}},
	}}
	after := buildMockCommitted(s.fakeSchema, map[TableID][]types.ProductValue{2: {persistentRow}})
	mgr.EvalAndBroadcast(types.TxID(1), cs, after, PostCommitMeta{})
	updates := (<-inbox).Fanout[connID]
	if len(updates) != 1 {
		t.Fatalf("event join fanout updates = %+v, want one update", updates)
	}
	requireProductRowsEqual(t, updates[0].Inserts, []types.ProductValue{eventRow})
}

func TestEventTableAggregateUsesChangesetInsertsAsAfterState(t *testing.T) {
	s := newEventAwareSchema()
	s.addEventTable(1, map[ColID]types.ValueKind{0: types.KindString})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    13,
		Predicates: []Predicate{AllRows{Table: 1}},
		Aggregates: []*Aggregate{countStarAggregate()},
	}, buildMockCommitted(s.fakeSchema, nil)); err != nil {
		t.Fatalf("RegisterSet event aggregate = %v", err)
	}

	mgr.EvalAndBroadcast(types.TxID(1), simpleChangeset(1, []types.ProductValue{{types.NewString("event")}}, nil), buildMockCommitted(s.fakeSchema, nil), PostCommitMeta{})
	updates := (<-inbox).Fanout[connID]
	if len(updates) != 1 {
		t.Fatalf("event aggregate updates = %+v, want one update", updates)
	}
	requireAggregateDelta(t, updates[0], 0, 1, "event COUNT(*)")
}
