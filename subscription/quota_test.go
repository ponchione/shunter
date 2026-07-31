package subscription

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/ponchione/shunter/bsatn"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

func TestRegisterSetWorkLimitCoversOrdinaryPlansAtomically(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	rows := []types.ProductValue{
		{types.NewUint64(1)},
		{types.NewUint64(2)},
		{types.NewUint64(3)},
	}
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{1: rows, 2: rows})
	tests := []struct {
		name      string
		predicate Predicate
		aggregate *Aggregate
		maxWork   int
	}{
		{
			name: "filtered cross join with empty result",
			predicate: CrossJoin{Left: 1, Right: 2, Filter: ColEq{
				Table: 1, Column: 0, Value: types.NewUint64(999),
			}},
			maxWork: 8,
		},
		{
			name:      "single table distinct aggregate",
			predicate: AllRows{Table: 1},
			aggregate: &Aggregate{
				Func:         AggregateCount,
				Distinct:     true,
				ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64},
				Argument: &AggregateColumn{
					Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
					Table:  1,
					Column: 0,
				},
			},
			maxWork: 2,
		},
		{
			name:      "cross join aggregate",
			predicate: CrossJoin{Left: 1, Right: 2},
			aggregate: &Aggregate{
				Func:         AggregateCount,
				ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64},
			},
			maxWork: 8,
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager(s, s, WithMaxMultiJoinWork(tt.maxWork))
			var aggregates []*Aggregate
			if tt.aggregate != nil {
				aggregates = []*Aggregate{tt.aggregate}
			}
			_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
				ConnID:     types.ConnectionID{1},
				QueryID:    uint32(100 + i),
				Predicates: []Predicate{tt.predicate},
				Aggregates: aggregates,
			}, view)
			if !errors.Is(err, ErrSubscriptionQuota) || !errors.Is(err, ErrSubscriptionWorkLimit) {
				t.Fatalf("RegisterSet error = %v, want work quota", err)
			}
			if mgr.registry.hasActive() || len(mgr.querySets) != 0 || len(mgr.connectionUsage) != 0 {
				t.Fatalf("work-limit failure mutated manager state: sets=%v usage=%v", mgr.querySets, mgr.connectionUsage)
			}
		})
	}
}

func TestOrderedWindowLimitRejectsLiteralBeforeSnapshotAllocation(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, nil)
	limit := uint64(1)
	offset := uint64(math.MaxInt)
	mgr := NewManager(s, s, WithOrderedWindowMaxRows(16))
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    200,
		Predicates: []Predicate{AllRows{Table: 1}},
		OrderByColumns: [][]OrderByColumn{{
			{Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64}, Table: 1, Column: 0},
		}},
		Limits:  []*uint64{&limit},
		Offsets: []*uint64{&offset},
	}, view)
	if !errors.Is(err, ErrSubscriptionQuota) || !errors.Is(err, ErrOrderedWindowLimit) {
		t.Fatalf("RegisterSet error = %v, want ordered-window quota", err)
	}
	if mgr.registry.hasActive() || len(mgr.querySets) != 0 || len(mgr.connectionUsage) != 0 {
		t.Fatalf("ordered-window failure mutated manager state: sets=%v usage=%v", mgr.querySets, mgr.connectionUsage)
	}
}

func TestRegisterSetRejectsQueryCountBeforePredicateValidation(t *testing.T) {
	mgr := NewManager(testSchema(), nil, WithMaxQueriesPerSet(2))
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    1,
		Predicates: []Predicate{nil, nil, nil},
	}, nil)
	if !errors.Is(err, ErrSubscriptionQuota) || !errors.Is(err, ErrSubscriptionQueryLimit) {
		t.Fatalf("RegisterSet error = %v, want query-count quota", err)
	}
	if mgr.registry.hasActive() || len(mgr.querySets) != 0 || len(mgr.connectionUsage) != 0 {
		t.Fatalf("over-limit request mutated manager state: sets=%v usage=%v", mgr.querySets, mgr.connectionUsage)
	}
}

func TestRegisterSetAggregateSnapshotRowLimit(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1), types.NewString("one")}},
		2: {{types.NewUint64(2), types.NewInt32(2)}},
	})
	mgr := NewManager(s, s, WithInitialRowLimit(1), WithMaxActiveSetsPerConnection(2), WithMaxActiveSubscriptionsPerConnection(2))
	mgr.nextSubID = 9
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    1,
		Predicates: []Predicate{AllRows{Table: 1}, AllRows{Table: 2}},
	}, view)
	if !errors.Is(err, ErrSubscriptionQuota) || !errors.Is(err, ErrInitialRowLimit) {
		t.Fatalf("RegisterSet error = %v, want aggregate row quota", err)
	}
	if mgr.nextSubID != 9 || mgr.registry.hasActive() || len(mgr.querySets) != 0 || len(mgr.connectionUsage) != 0 {
		t.Fatalf("row-limit failure left state: next=%d sets=%v usage=%v", mgr.nextSubID, mgr.querySets, mgr.connectionUsage)
	}
}

func TestRegisterSetSnapshotByteLimitExactAndOver(t *testing.T) {
	s := testSchema()
	row := types.ProductValue{types.NewUint64(1), types.NewString("payload")}
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{1: {row}})
	columns := []schema.ColumnSchema{
		{Index: 0, Name: "c0", Type: types.KindUint64},
		{Index: 1, Name: "c1", Type: types.KindString},
	}
	rowBytes, err := bsatn.EncodedProductValueSizeForColumns(row, columns)
	if err != nil {
		t.Fatal(err)
	}
	exact := 4 + 4 + rowBytes

	mgr := NewManager(s, s, WithSnapshotByteLimit(exact))
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 1, Predicates: []Predicate{AllRows{Table: 1}},
	}, view); err != nil {
		t.Fatalf("exact byte cap rejected: %v", err)
	}

	mgr = NewManager(s, s, WithSnapshotByteLimit(exact-1))
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 1, Predicates: []Predicate{AllRows{Table: 1}},
	}, view)
	if !errors.Is(err, ErrSubscriptionQuota) || !errors.Is(err, ErrSnapshotByteLimit) {
		t.Fatalf("one byte over cap error = %v, want snapshot byte quota", err)
	}
	if mgr.registry.hasActive() || len(mgr.connectionUsage) != 0 {
		t.Fatalf("byte-limit failure left state: usage=%v", mgr.connectionUsage)
	}
}

func TestRegisterSetSnapshotByteLimitAggregatesManyRows(t *testing.T) {
	s := testSchema()
	rows := []types.ProductValue{
		{types.NewUint64(1), types.NewString("a")},
		{types.NewUint64(2), types.NewString("b")},
	}
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{1: rows})
	columns := []schema.ColumnSchema{
		{Index: 0, Name: "c0", Type: types.KindUint64},
		{Index: 1, Name: "c1", Type: types.KindString},
	}
	capBytes := 4
	for _, row := range rows {
		rowBytes, err := bsatn.EncodedProductValueSizeForColumns(row, columns)
		if err != nil {
			t.Fatal(err)
		}
		capBytes += 4 + rowBytes
	}
	mgr := NewManager(s, s, WithSnapshotByteLimit(capBytes-1))
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 1, Predicates: []Predicate{AllRows{Table: 1}},
	}, view)
	if !errors.Is(err, ErrSnapshotByteLimit) {
		t.Fatalf("many-row byte error = %v, want ErrSnapshotByteLimit", err)
	}
}

func TestRegisterSetPrepareFailureIsAtomic(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1), types.NewString("one")}},
	})
	mgr := NewManager(s, s, WithMaxActiveSetsPerConnection(1), WithMaxActiveSubscriptionsPerConnection(1))
	mgr.nextSubID = 41
	prepareErr := errors.New("encode failed")
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 1, Predicates: []Predicate{AllRows{Table: 1}},
		PrepareSnapshot: func([]SubscriptionUpdate) error { return prepareErr },
	}, view)
	if !errors.Is(err, ErrInitialQuery) || !errors.Is(err, prepareErr) {
		t.Fatalf("RegisterSet error = %v, want prepared encode failure", err)
	}
	if mgr.nextSubID != 41 || mgr.registry.hasActive() || len(mgr.querySets) != 0 || len(mgr.connectionUsage) != 0 || !pruningIndexesEmpty(mgr.indexes) {
		t.Fatalf("prepare failure left state: next=%d sets=%v usage=%v", mgr.nextSubID, mgr.querySets, mgr.connectionUsage)
	}
}

func TestRegisterSetCancellationReleasesReservation(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1), types.NewString("one")}},
	})
	mgr := NewManager(s, s, WithMaxActiveSetsPerConnection(1), WithMaxActiveSubscriptionsPerConnection(1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		Context: ctx, ConnID: types.ConnectionID{1}, QueryID: 1, Predicates: []Predicate{AllRows{Table: 1}},
	}, view)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RegisterSet error = %v, want context.Canceled", err)
	}
	if len(mgr.connectionUsage) != 0 || mgr.registry.hasActive() {
		t.Fatalf("cancellation retained state: usage=%v", mgr.connectionUsage)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 2, Predicates: []Predicate{AllRows{Table: 1}},
	}, nil); err != nil {
		t.Fatalf("registration after cancellation release: %v", err)
	}
}

func TestConnectionQuotasDeduplicateAndRelease(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, nil)
	connID := types.ConnectionID{1}
	mgr := NewManager(s, s,
		WithMaxActiveSetsPerConnection(2),
		WithMaxActiveSubscriptionsPerConnection(2),
	)

	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connID, QueryID: 1,
		Predicates: []Predicate{AllRows{Table: 1}, AllRows{Table: 1}},
	}, view); err != nil {
		t.Fatalf("duplicate set: %v", err)
	}
	if got := mgr.connectionUsage[connID]; got != (connectionSubscriptionUsage{sets: 1, subscriptions: 1}) {
		t.Fatalf("usage after duplicate set = %+v, want one set/subscription", got)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connID, QueryID: 2, Predicates: []Predicate{AllRows{Table: 2}},
	}, view); err != nil {
		t.Fatalf("second set at exact quota: %v", err)
	}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connID, QueryID: 3, Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(3)}},
	}, view)
	if !errors.Is(err, ErrSubscriptionSetLimit) || !errors.Is(err, ErrSubscriptionQuota) {
		t.Fatalf("third set error = %v, want active-set quota", err)
	}

	if _, err := mgr.UnregisterSet(connID, 1, nil); err != nil {
		t.Fatalf("UnregisterSet: %v", err)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connID, QueryID: 3, Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(3)}},
	}, view); err != nil {
		t.Fatalf("registration after unregister release: %v", err)
	}

	if err := mgr.DisconnectClient(connID); err != nil {
		t.Fatalf("DisconnectClient: %v", err)
	}
	if len(mgr.connectionUsage) != 0 {
		t.Fatalf("disconnect retained usage: %v", mgr.connectionUsage)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connID, QueryID: 4, Predicates: []Predicate{AllRows{Table: 1}},
	}, view); err != nil {
		t.Fatalf("registration after disconnect release: %v", err)
	}
}

func TestConnectionActiveSubscriptionQuota(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, nil)
	connID := types.ConnectionID{1}
	mgr := NewManager(s, s,
		WithMaxActiveSetsPerConnection(3),
		WithMaxActiveSubscriptionsPerConnection(2),
	)
	for queryID, pred := range []Predicate{AllRows{Table: 1}, AllRows{Table: 2}} {
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID: connID, QueryID: uint32(queryID + 1), Predicates: []Predicate{pred},
		}, view); err != nil {
			t.Fatalf("registration %d at quota: %v", queryID+1, err)
		}
	}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connID, QueryID: 3,
		Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(3)}},
	}, view)
	if !errors.Is(err, ErrSubscriptionQuota) || !errors.Is(err, ErrSubscriptionCountLimit) {
		t.Fatalf("third subscription error = %v, want active-subscription quota", err)
	}
}

func TestUnregisterSnapshotQuotaStillReleasesState(t *testing.T) {
	s := testSchema()
	row := types.ProductValue{types.NewUint64(1), types.NewString("payload")}
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{1: {row}})
	mgr := NewManager(s, s, WithSnapshotByteLimit(1<<20), WithMaxActiveSetsPerConnection(1), WithMaxActiveSubscriptionsPerConnection(1))
	connID := types.ConnectionID{1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connID, QueryID: 1, Predicates: []Predicate{AllRows{Table: 1}},
	}, view); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	mgr.SnapshotByteLimit = 4
	_, err := mgr.UnregisterSet(connID, 1, view)
	if !errors.Is(err, ErrFinalQuery) || !errors.Is(err, ErrSnapshotByteLimit) {
		t.Fatalf("UnregisterSet error = %v, want final snapshot quota", err)
	}
	if mgr.registry.hasActive() || len(mgr.querySets) != 0 || len(mgr.connectionUsage) != 0 {
		t.Fatalf("unregister quota failure retained state: sets=%v usage=%v", mgr.querySets, mgr.connectionUsage)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: connID, QueryID: 2, Predicates: []Predicate{AllRows{Table: 1}},
	}, nil); err != nil {
		t.Fatalf("quota not released for subsequent registration: %v", err)
	}
}
