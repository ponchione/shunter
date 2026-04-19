package subscription

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

func testSchema() *fakeSchema {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindInt32}, 0)
	return s
}

// buildMockCommitted creates a committed view consistent with the fakeSchema
// index IDs (via syntheticIndexID) so the Manager can do IndexSeek.
func buildMockCommitted(schema *fakeSchema, contents map[TableID][]types.ProductValue) *mockCommitted {
	c := newMockCommitted()
	for tid, rows := range contents {
		// Register the single-column indexes the schema declares.
		for col := range schema.indexes[tid] {
			c.setIndex(tid, syntheticIndexID(tid, col), int(col))
		}
		for i, row := range rows {
			c.addRow(tid, types.RowID(i+1), row)
		}
	}
	return c
}

func TestRegisterReturnsInitialRows(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
	})
	mgr := NewManager(s, s)
	req := SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    10,
		Predicates: []Predicate{AllRows{Table: 1}},
	}
	res, err := mgr.RegisterSet(req, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if len(res.Update) != 1 || len(res.Update[0].Inserts) != 2 {
		t.Fatalf("InitialRows update = %+v, want 2 inserts", res.Update)
	}
}

func TestRegisterDedupSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(42), types.NewString("x")}},
	})
	mgr := NewManager(s, s)
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 11, Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	// Only one queryState, two subscribers.
	if n := len(mgr.registry.byHash); n != 1 {
		t.Fatalf("byHash count = %d, want 1", n)
	}
	h := ComputeQueryHash(pred, nil)
	if mgr.registry.byHash[h].refCount != 2 {
		t.Fatalf("refCount = %d, want 2", mgr.registry.byHash[h].refCount)
	}
}

func TestRegisterParameterizedHashUsesClientIdentity(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	idA := &types.Identity{1}
	idB := &types.Identity{2}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:         types.ConnectionID{1},
		QueryID:        10,
		Predicates:     []Predicate{pred},
		ClientIdentity: idA,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:         types.ConnectionID{2},
		QueryID:        11,
		Predicates:     []Predicate{pred},
		ClientIdentity: idB,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 2 {
		t.Fatalf("query-state count = %d, want 2 for distinct client identities", got)
	}
	if _, ok := mgr.registry.byHash[ComputeQueryHash(pred, idA)]; !ok {
		t.Fatal("missing query state for client A hash")
	}
	if _, ok := mgr.registry.byHash[ComputeQueryHash(pred, idB)]; !ok {
		t.Fatal("missing query state for client B hash")
	}
	if ComputeQueryHash(pred, idA) == ComputeQueryHash(pred, idB) {
		t.Fatal("parameterized hashes should differ by client identity")
	}
}

func TestRegisterAllowsSameQueryIDAcrossConnections(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	pred := AllRows{Table: 1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 10, Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("second connection should be able to reuse query ID 10: %v", err)
	}
	if qs := mgr.registry.getQuery(ComputeQueryHash(pred, nil)); qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state refCount = %v, want 2", qs)
	}
}

func TestRegisterThreeTableError(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(3, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	inner := And{Left: ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
		Right: ColEq{Table: 2, Column: 0, Value: types.NewUint64(2)}}
	p := And{Left: inner, Right: ColEq{Table: 3, Column: 0, Value: types.NewUint64(3)}}
	mgr := NewManager(s, s)
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{p},
	}, nil)
	if !errors.Is(err, ErrTooManyTables) {
		t.Fatalf("want ErrTooManyTables, got %v", err)
	}
}

func TestRegisterUnindexedJoinError(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	mgr := NewManager(s, s)
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{p},
	}, nil)
	if !errors.Is(err, ErrUnindexedJoin) {
		t.Fatalf("want ErrUnindexedJoin, got %v", err)
	}
}

// TestRegisterJoinNilResolverFails: validation passes (schema declares the
// index) but the manager has no resolver. Bootstrap must hard-fail instead of
// returning silent empty rows (PHASE-5-DEFERRED §D).
func TestRegisterJoinNilResolverFails(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1)}},
		2: {{types.NewUint64(1)}},
	})
	mgr := NewManager(s, nil) // no resolver
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{p},
	}, view)
	if !errors.Is(err, ErrJoinIndexUnresolved) {
		t.Fatalf("want ErrJoinIndexUnresolved, got %v", err)
	}
}

// stubNoopResolver reports no index for any (table, col). Combined with a
// schema that declares the index, it simulates a resolver/schema contract
// disagreement.
type stubNoopResolver struct{}

func (stubNoopResolver) IndexIDForColumn(TableID, ColID) (IndexID, bool) { return 0, false }

// TestRegisterJoinResolverMissingIndexFails: schema says the RHS column is
// indexed (validation passes) but the resolver cannot produce an IndexID for
// it. Bootstrap must hard-fail instead of returning silent empty rows.
func TestRegisterJoinResolverMissingIndexFails(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1)}},
		2: {{types.NewUint64(1)}},
	})
	mgr := NewManager(s, stubNoopResolver{})
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{p},
	}, view)
	if !errors.Is(err, ErrJoinIndexUnresolved) {
		t.Fatalf("want ErrJoinIndexUnresolved, got %v", err)
	}
}

func TestRegisterJoinBootstrapFallsBackToLeftIndex(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString})
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1), types.NewString("lhs")}},
		2: {{types.NewUint64(1), types.NewString("rhs")}},
	})
	mgr := NewManager(s, s)
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0}
	got, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{p},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if len(got.Update) != 1 || len(got.Update[0].Inserts) != 1 {
		t.Fatalf("initial rows = %v, want 1 joined row", got.Update)
	}
	if len(got.Update[0].Inserts[0]) != 4 {
		t.Fatalf("joined row = %v, want concatenated lhs+rhs columns", got.Update[0].Inserts[0])
	}
}

func TestRegisterInitialRowLimit(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
			{types.NewUint64(3), types.NewString("c")},
		},
	})
	mgr := NewManager(s, s, WithInitialRowLimit(1))
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{AllRows{Table: 1}},
	}, view)
	if !errors.Is(err, ErrInitialRowLimit) {
		t.Fatalf("want ErrInitialRowLimit, got %v", err)
	}
}

func TestRegisterAppearsInPruningIndexes(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := mgr.indexes.Value.Lookup(1, 0, types.NewUint64(42)); len(got) != 1 {
		t.Fatalf("pruning index missing: %v", got)
	}
}

func TestUnregisterNotLast(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	pred := AllRows{Table: 1}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, nil)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 11, Predicates: []Predicate{pred},
	}, nil)
	if _, err := mgr.UnregisterSet(types.ConnectionID{1}, 10, nil); err != nil {
		t.Fatal(err)
	}
	// Query state should still be alive.
	if !mgr.registry.hasActive() {
		t.Fatal("queryState should still be alive with 1 subscriber")
	}
}

func TestUnregisterLastCleansIndexes(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, nil)
	if _, err := mgr.UnregisterSet(types.ConnectionID{1}, 10, nil); err != nil {
		t.Fatal(err)
	}
	if mgr.registry.hasActive() {
		t.Fatal("queryState should be gone")
	}
	if got := mgr.indexes.Value.Lookup(1, 0, types.NewUint64(42)); len(got) != 0 {
		t.Fatalf("pruning index not cleaned: %v", got)
	}
}

func TestUnregisterLastCleansJoinEdgeIndexes(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindInt32}, 0)
	mgr := NewManager(s, s)
	pred := Join{
		Left:     1,
		Right:    2,
		LeftCol:  0,
		RightCol: 0,
		Filter:   ColEq{Table: 2, Column: 1, Value: types.NewInt32(7)},
	}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mgr.indexes.JoinEdge.edges) == 0 || len(mgr.indexes.JoinEdge.byTable) == 0 {
		t.Fatalf("expected join-edge placement, got edges=%v byTable=%v", mgr.indexes.JoinEdge.edges, mgr.indexes.JoinEdge.byTable)
	}
	if _, err := mgr.UnregisterSet(types.ConnectionID{1}, 10, nil); err != nil {
		t.Fatal(err)
	}
	if len(mgr.indexes.JoinEdge.edges) != 0 || len(mgr.indexes.JoinEdge.byTable) != 0 {
		t.Fatalf("join-edge index not cleaned: edges=%v byTable=%v", mgr.indexes.JoinEdge.edges, mgr.indexes.JoinEdge.byTable)
	}
}

func TestUnregisterUnknown(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	_, err := mgr.UnregisterSet(types.ConnectionID{1}, 999, nil)
	if !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("want ErrSubscriptionNotFound, got %v", err)
	}
}

func TestDisconnectClientRemovesAll(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	c := types.ConnectionID{1}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 10, Predicates: []Predicate{AllRows{Table: 1}}}, nil)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 11, Predicates: []Predicate{AllRows{Table: 2}}}, nil)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 12, Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)}}}, nil)
	if err := mgr.DisconnectClient(c); err != nil {
		t.Fatal(err)
	}
	if mgr.registry.hasActive() {
		t.Fatal("all queries should be gone")
	}
	if subs := mgr.registry.subscriptionsForConn(c); len(subs) != 0 {
		t.Fatalf("byConn leftover: %v", subs)
	}
}

func TestDisconnectSharedQuerySurvives(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	cA := types.ConnectionID{1}
	cB := types.ConnectionID{2}
	pred := AllRows{Table: 1}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: cA, QueryID: 10, Predicates: []Predicate{pred}}, nil)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: cB, QueryID: 11, Predicates: []Predicate{pred}}, nil)
	_ = mgr.DisconnectClient(cA)
	if !mgr.registry.hasActive() {
		t.Fatal("queryState should remain for client B")
	}
}

func TestDisconnectUnknownIsNoop(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	if err := mgr.DisconnectClient(types.ConnectionID{99}); err != nil {
		t.Fatal(err)
	}
}

func TestDroppedClientsChannel(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	if mgr.DroppedClients() == nil {
		t.Fatal("DroppedClients should not be nil")
	}
	mgr.signalDropped(types.ConnectionID{7})
	select {
	case got := <-mgr.DroppedClients():
		if got != (types.ConnectionID{7}) {
			t.Fatalf("got %v, want {7}", got)
		}
	default:
		t.Fatal("expected dropped signal")
	}
}

func TestManagerErrorsAreDistinct(t *testing.T) {
	errs := []error{
		ErrTooManyTables,
		ErrUnindexedJoin,
		ErrInvalidPredicate,
		ErrTableNotFound,
		ErrColumnNotFound,
		ErrInitialRowLimit,
		ErrSubscriptionNotFound,
		ErrSubscriptionEval,
		ErrJoinIndexUnresolved,
	}
	for i, a := range errs {
		for j, b := range errs {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Fatalf("errors.Is(%v, %v) should be false", a, b)
			}
		}
	}
}
