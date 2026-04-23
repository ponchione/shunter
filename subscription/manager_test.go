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

func TestRegisterSet_SameTableAndChildOrderSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("alice")},
			{types.NewUint64(2), types.NewString("bob")},
		},
	})
	mgr := NewManager(s, s)
	leftFirst := And{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
		Right: ColEq{Table: 1, Column: 1, Value: types.NewString("alice")},
	}
	rightFirst := And{
		Left:  ColEq{Table: 1, Column: 1, Value: types.NewString("alice")},
		Right: ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
	}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 12, Predicates: []Predicate{leftFirst},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 13, Predicates: []Predicate{rightFirst},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(leftFirst, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SameTableOrChildOrderSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("alice")},
			{types.NewUint64(2), types.NewString("bob")},
			{types.NewUint64(3), types.NewString("carol")},
		},
	})
	mgr := NewManager(s, s)
	leftFirst := Or{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
		Right: ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)},
	}
	rightFirst := Or{
		Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)},
		Right: ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
	}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 14, Predicates: []Predicate{leftFirst},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 15, Predicates: []Predicate{rightFirst},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(leftFirst, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SameTableAndAssociativeGroupingSharesQueryState(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString, 2: types.KindUint64}, 0)
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("alice"), types.NewUint64(30)},
			{types.NewUint64(1), types.NewString("alice"), types.NewUint64(31)},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 1, Value: types.NewString("alice")}
	c := ColEq{Table: 1, Column: 2, Value: types.NewUint64(30)}
	leftGrouped := And{Left: And{Left: a, Right: b}, Right: c}
	rightGrouped := And{Left: a, Right: And{Left: b, Right: c}}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 16, Predicates: []Predicate{leftGrouped},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 17, Predicates: []Predicate{rightGrouped},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(leftGrouped, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SameTableOrAssociativeGroupingSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("alice")},
			{types.NewUint64(2), types.NewString("bob")},
			{types.NewUint64(3), types.NewString("carol")},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 0, Value: types.NewUint64(2)}
	c := ColEq{Table: 1, Column: 0, Value: types.NewUint64(3)}
	leftGrouped := Or{Left: Or{Left: a, Right: b}, Right: c}
	rightGrouped := Or{Left: a, Right: Or{Left: b, Right: c}}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 18, Predicates: []Predicate{leftGrouped},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 19, Predicates: []Predicate{rightGrouped},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(leftGrouped, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SameTableDuplicateAndSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("alice")},
			{types.NewUint64(2), types.NewString("bob")},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	duplicated := And{Left: a, Right: a}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 20, Predicates: []Predicate{a},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 21, Predicates: []Predicate{duplicated},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(a, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SameTableDuplicateOrSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("alice")},
			{types.NewUint64(2), types.NewString("bob")},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	duplicated := Or{Left: a, Right: a}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 22, Predicates: []Predicate{a},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 23, Predicates: []Predicate{duplicated},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(a, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SameTableOrAbsorptionSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("alice")},
			{types.NewUint64(1), types.NewString("bob")},
			{types.NewUint64(2), types.NewString("alice")},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 1, Value: types.NewString("alice")}
	absorbed := Or{Left: a, Right: And{Left: a, Right: b}}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 24, Predicates: []Predicate{a},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 25, Predicates: []Predicate{absorbed},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(a, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SameTableAndAbsorptionSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("alice")},
			{types.NewUint64(1), types.NewString("bob")},
			{types.NewUint64(2), types.NewString("alice")},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)}
	b := ColEq{Table: 1, Column: 1, Value: types.NewString("alice")}
	absorbed := And{Left: a, Right: Or{Left: a, Right: b}}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 26, Predicates: []Predicate{a},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 27, Predicates: []Predicate{absorbed},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(a, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
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

func TestRegisterSet_MixedHashIdentitiesOnlyParameterizeMarkedPredicates(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindBytes}, 0)
	mgr := NewManager(s, s)
	literal := ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)}
	parameterized := ColEq{Table: 1, Column: 1, Value: types.NewBytes([]byte{0x01, 0x02})}
	idA := &types.Identity{1}
	idB := &types.Identity{2}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:                  types.ConnectionID{1},
		QueryID:                 10,
		Predicates:              []Predicate{literal, parameterized},
		PredicateHashIdentities: []*types.Identity{nil, idA},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:                  types.ConnectionID{2},
		QueryID:                 11,
		Predicates:              []Predicate{literal, parameterized},
		PredicateHashIdentities: []*types.Identity{nil, idB},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 3 {
		t.Fatalf("query-state count = %d, want 3 (one shared literal + two parameterized)", got)
	}
	literalHash := ComputeQueryHash(literal, nil)
	if qs := mgr.registry.byHash[literalHash]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared literal query state = %+v, want refCount 2", qs)
	}
	if _, ok := mgr.registry.byHash[ComputeQueryHash(parameterized, idA)]; !ok {
		t.Fatal("missing query state for parameterized predicate with identity A")
	}
	if _, ok := mgr.registry.byHash[ComputeQueryHash(parameterized, idB)]; !ok {
		t.Fatal("missing query state for parameterized predicate with identity B")
	}
}

func TestRegisterSet_ParameterizedSenderHashDiffersFromLiteralEquivalent(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindBytes}, 0)
	mgr := NewManager(s, s)
	pred := ColEq{Table: 1, Column: 1, Value: types.NewBytes([]byte{0x01, 0x02})}
	id := &types.Identity{9}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:                  types.ConnectionID{1},
		QueryID:                 20,
		Predicates:              []Predicate{pred},
		PredicateHashIdentities: []*types.Identity{nil},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:                  types.ConnectionID{2},
		QueryID:                 21,
		Predicates:              []Predicate{pred},
		PredicateHashIdentities: []*types.Identity{id},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	literalHash := ComputeQueryHash(pred, nil)
	parameterizedHash := ComputeQueryHash(pred, id)
	if literalHash == parameterizedHash {
		t.Fatal("literal hash and parameterized sender hash should differ")
	}
	if _, ok := mgr.registry.byHash[literalHash]; !ok {
		t.Fatal("missing literal query state")
	}
	if _, ok := mgr.registry.byHash[parameterizedHash]; !ok {
		t.Fatal("missing parameterized query state")
	}
}

func TestRegisterSet_TrueAndComparisonSharesQueryStateWithComparison(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewString("x")}},
	})
	mgr := NewManager(s, s)
	comparison := ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)}
	withTrue := And{Left: AllRows{Table: 1}, Right: comparison}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    40,
		Predicates: []Predicate{comparison},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{2},
		QueryID:    41,
		Predicates: []Predicate{withTrue},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(comparison, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_NoRowsSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewString("x")}},
	})
	mgr := NewManager(s, s)
	pred := NoRows{Table: 1}

	got, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    42,
		Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Update) != 0 {
		t.Fatalf("initial update = %+v, want none for NoRows", got.Update)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{2},
		QueryID:    43,
		Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(pred, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SameTableFalseOrComparisonSharesQueryState(t *testing.T) {
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(7), types.NewString("x")}},
	})
	mgr := NewManager(s, s)
	comparison := ColEq{Table: 1, Column: 0, Value: types.NewUint64(7)}
	withFalse := Or{Left: NoRows{Table: 1}, Right: comparison}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    44,
		Predicates: []Predicate{comparison},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{2},
		QueryID:    45,
		Predicates: []Predicate{withFalse},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h := ComputeQueryHash(comparison, nil)
	if qs := mgr.registry.byHash[h]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_PredicateHashIdentityCountMismatchRejected(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	id := &types.Identity{1}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:                  types.ConnectionID{1},
		QueryID:                 30,
		Predicates:              []Predicate{pred},
		PredicateHashIdentities: []*types.Identity{nil, id},
	}, nil)
	if err == nil {
		t.Fatal("RegisterSet error = nil, want predicate hash identity count mismatch")
	}
	if got := len(mgr.registry.byHash); got != 0 {
		t.Fatalf("query-state count = %d, want 0 after rejected request", got)
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

func TestRegisterSet_CanonicalizationDoesNotMaskTooManyTables(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(3, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	p := Or{
		Left: AllRows{Table: 1},
		Right: And{
			Left:  ColEq{Table: 2, Column: 0, Value: types.NewUint64(2)},
			Right: ColEq{Table: 3, Column: 0, Value: types.NewUint64(3)},
		},
	}
	mgr := NewManager(s, s)
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 11, Predicates: []Predicate{p},
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
	// Join subscriptions emit rows projected onto the SELECT side. Default
	// ProjectRight=false projects the LHS; Table(1) has 2 columns.
	if len(got.Update[0].Inserts[0]) != 2 {
		t.Fatalf("projected row width = %d, want 2 (LHS projection of Table(1))", len(got.Update[0].Inserts[0]))
	}
	if got.Update[0].TableID != 1 {
		t.Fatalf("emitted TableID = %d, want 1 (LHS-projected)", got.Update[0].TableID)
	}
	if !got.Update[0].Inserts[0][0].Equal(types.NewUint64(1)) || !got.Update[0].Inserts[0][1].Equal(types.NewString("lhs")) {
		t.Fatalf("projected row = %v, want [1, \"lhs\"]", got.Update[0].Inserts[0])
	}
}

// RHS counterpart of the bootstrap test — proves ProjectRight threads through
// initialQuery so `SELECT rhs.* FROM lhs JOIN rhs ...` emits RHS-shape rows.
func TestRegisterJoinBootstrapProjectsRight(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString})
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1), types.NewString("lhs")}},
		2: {{types.NewUint64(1), types.NewString("rhs")}},
	})
	mgr := NewManager(s, s)
	p := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, ProjectRight: true}
	got, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 11, Predicates: []Predicate{p},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if len(got.Update) != 1 || len(got.Update[0].Inserts) != 1 {
		t.Fatalf("initial rows = %v, want 1 joined row", got.Update)
	}
	if got.Update[0].TableID != 2 {
		t.Fatalf("emitted TableID = %d, want 2 (RHS-projected)", got.Update[0].TableID)
	}
	if len(got.Update[0].Inserts[0]) != 2 {
		t.Fatalf("projected row width = %d, want 2 (RHS projection of Table(2))", len(got.Update[0].Inserts[0]))
	}
	if !got.Update[0].Inserts[0][1].Equal(types.NewString("rhs")) {
		t.Fatalf("projected row = %v, want RHS-shaped", got.Update[0].Inserts[0])
	}
}

func TestRegisterCrossJoinBootstrapPreservesMultiplicity(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1)}, {types.NewUint64(2)}},
		2: {{types.NewUint64(10)}, {types.NewUint64(11)}, {types.NewUint64(12)}},
	})
	mgr := NewManager(s, s)
	p := CrossJoin{Left: 1, Right: 2}
	got, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 12, Predicates: []Predicate{p},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if len(got.Update) != 1 {
		t.Fatalf("update count = %d, want 1", len(got.Update))
	}
	if len(got.Update[0].Inserts) != 6 {
		t.Fatalf("bootstrap inserts = %d, want 6 cartesian pairs", len(got.Update[0].Inserts))
	}
	counts := map[uint64]int{}
	for _, row := range got.Update[0].Inserts {
		counts[row[0].AsUint64()]++
	}
	if counts[1] != 3 || counts[2] != 3 {
		t.Fatalf("LHS multiplicity counts = %v, want {1:3, 2:3}", counts)
	}
}

func TestRegisterCrossJoinBootstrapProjectsRight(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1)}, {types.NewUint64(2)}, {types.NewUint64(3)}},
		2: {{types.NewUint64(10)}, {types.NewUint64(11)}},
	})
	mgr := NewManager(s, s)
	p := CrossJoin{Left: 1, Right: 2, ProjectRight: true}
	got, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 13, Predicates: []Predicate{p},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if len(got.Update) != 1 || len(got.Update[0].Inserts) != 6 {
		t.Fatalf("initial rows = %v, want 6 RHS-projected cartesian rows", got.Update)
	}
	counts := map[uint64]int{}
	for _, row := range got.Update[0].Inserts {
		counts[row[0].AsUint64()]++
	}
	if counts[10] != 3 || counts[11] != 3 {
		t.Fatalf("RHS multiplicity counts = %v, want {10:3, 11:3}", counts)
	}
}

func TestRegisterJoinBootstrapPreservesProjectedLeftOrderWhenOnlyLeftJoinColumnIndexed(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewUint64(7)},
			{types.NewUint64(2), types.NewUint64(7)},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(7)},
			{types.NewUint64(11), types.NewUint64(7)},
		},
	})
	mgr := NewManager(s, s)
	p := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1, ProjectRight: false}
	got, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 14, Predicates: []Predicate{p},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if len(got.Update) != 1 {
		t.Fatalf("update count = %d, want 1", len(got.Update))
	}
	gotRows := got.Update[0].Inserts
	wantRows := []types.ProductValue{
		{types.NewUint64(1), types.NewUint64(7)},
		{types.NewUint64(1), types.NewUint64(7)},
		{types.NewUint64(2), types.NewUint64(7)},
		{types.NewUint64(2), types.NewUint64(7)},
	}
	if len(gotRows) != len(wantRows) {
		t.Fatalf("insert count = %d, want %d", len(gotRows), len(wantRows))
	}
	for i := range wantRows {
		if !gotRows[i][0].Equal(wantRows[i][0]) || !gotRows[i][1].Equal(wantRows[i][1]) {
			t.Fatalf("insert[%d] = %v, want %v", i, gotRows[i], wantRows[i])
		}
	}
}

func TestRegisterSet_DistinctTableJoinFilterChildOrderSharesQueryState(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint32, 1: types.KindUint32}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint32})
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint32(100), types.NewUint32(1)},
			{types.NewUint32(101), types.NewUint32(2)},
		},
		2: {
			{types.NewUint32(100)},
			{types.NewUint32(101)},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 1, Value: types.NewUint32(1)}
	b := ColRange{Table: 1, Column: 1, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}, Upper: Bound{Unbounded: true}}
	leftFirst := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, Filter: And{Left: a, Right: b}}
	rightFirst := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 0, Filter: And{Left: b, Right: a}}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 152, Predicates: []Predicate{leftFirst},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 153, Predicates: []Predicate{rightFirst},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h1 := ComputeQueryHash(leftFirst, nil)
	h2 := ComputeQueryHash(rightFirst, nil)
	if h1 != h2 {
		t.Fatalf("query hashes differ: %v vs %v", h1, h2)
	}
	if qs := mgr.registry.byHash[h1]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SelfJoinFilterChildOrderSharesQueryState(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint32, 1: types.KindUint32}, 1)
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewUint32(7)},
			{types.NewUint32(2), types.NewUint32(7)},
			{types.NewUint32(3), types.NewUint32(9)},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}
	b := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}, Upper: Bound{Unbounded: true}}
	leftFirst := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: a, Right: b}}
	rightFirst := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: b, Right: a}}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 154, Predicates: []Predicate{leftFirst},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 155, Predicates: []Predicate{rightFirst},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h1 := ComputeQueryHash(leftFirst, nil)
	h2 := ComputeQueryHash(rightFirst, nil)
	if h1 != h2 {
		t.Fatalf("query hashes differ: %v vs %v", h1, h2)
	}
	if qs := mgr.registry.byHash[h1]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SelfJoinFilterAssociativeGroupingSharesQueryState(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint32, 1: types.KindUint32}, 1)
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewUint32(7)},
			{types.NewUint32(2), types.NewUint32(7)},
			{types.NewUint32(3), types.NewUint32(9)},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}
	b := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}, Upper: Bound{Unbounded: true}}
	c := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Unbounded: true}, Upper: Bound{Value: types.NewUint32(2), Inclusive: false}}
	leftGrouped := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: And{Left: a, Right: b}, Right: c}}
	rightGrouped := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: a, Right: And{Left: b, Right: c}}}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 156, Predicates: []Predicate{leftGrouped},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 157, Predicates: []Predicate{rightGrouped},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h1 := ComputeQueryHash(leftGrouped, nil)
	h2 := ComputeQueryHash(rightGrouped, nil)
	if h1 != h2 {
		t.Fatalf("query hashes differ: %v vs %v", h1, h2)
	}
	if qs := mgr.registry.byHash[h1]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SelfJoinFilterDuplicateLeafSharesQueryState(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint32, 1: types.KindUint32}, 1)
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewUint32(7)},
			{types.NewUint32(2), types.NewUint32(7)},
			{types.NewUint32(3), types.NewUint32(9)},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}
	single := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: a}
	duplicate := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: And{Left: a, Right: a}}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 158, Predicates: []Predicate{single},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 159, Predicates: []Predicate{duplicate},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h1 := ComputeQueryHash(single, nil)
	h2 := ComputeQueryHash(duplicate, nil)
	if h1 != h2 {
		t.Fatalf("query hashes differ: %v vs %v", h1, h2)
	}
	if qs := mgr.registry.byHash[h1]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterSet_SelfJoinFilterAbsorptionSharesQueryState(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint32, 1: types.KindUint32}, 1)
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint32(1), types.NewUint32(7)},
			{types.NewUint32(2), types.NewUint32(7)},
			{types.NewUint32(3), types.NewUint32(9)},
		},
	})
	mgr := NewManager(s, s)
	a := ColEq{Table: 1, Column: 0, Alias: 0, Value: types.NewUint32(1)}
	b := ColRange{Table: 1, Column: 0, Alias: 0, Lower: Bound{Value: types.NewUint32(0), Inclusive: false}, Upper: Bound{Unbounded: true}}
	single := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: a}
	absorbed := Join{Left: 1, Right: 1, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1, Filter: Or{Left: a, Right: And{Left: a, Right: b}}}

	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 160, Predicates: []Predicate{single},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	_, err = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{2}, QueryID: 161, Predicates: []Predicate{absorbed},
	}, view)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(mgr.registry.byHash); got != 1 {
		t.Fatalf("query-state count = %d, want 1 shared state", got)
	}
	h1 := ComputeQueryHash(single, nil)
	h2 := ComputeQueryHash(absorbed, nil)
	if h1 != h2 {
		t.Fatalf("query hashes differ: %v vs %v", h1, h2)
	}
	if qs := mgr.registry.byHash[h1]; qs == nil || qs.refCount != 2 {
		t.Fatalf("shared query state = %+v, want refCount 2", qs)
	}
}

func TestRegisterJoinBootstrapPreservesProjectedRightOrderWhenOnlyRightJoinColumnIndexed(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewUint64(7)},
			{types.NewUint64(2), types.NewUint64(7)},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(7)},
			{types.NewUint64(11), types.NewUint64(7)},
		},
	})
	mgr := NewManager(s, s)
	p := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1, ProjectRight: true}
	got, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 15, Predicates: []Predicate{p},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if len(got.Update) != 1 {
		t.Fatalf("update count = %d, want 1", len(got.Update))
	}
	gotRows := got.Update[0].Inserts
	wantRows := []types.ProductValue{
		{types.NewUint64(10), types.NewUint64(7)},
		{types.NewUint64(10), types.NewUint64(7)},
		{types.NewUint64(11), types.NewUint64(7)},
		{types.NewUint64(11), types.NewUint64(7)},
	}
	if len(gotRows) != len(wantRows) {
		t.Fatalf("insert count = %d, want %d", len(gotRows), len(wantRows))
	}
	for i := range wantRows {
		if !gotRows[i][0].Equal(wantRows[i][0]) || !gotRows[i][1].Equal(wantRows[i][1]) {
			t.Fatalf("insert[%d] = %v, want %v", i, gotRows[i], wantRows[i])
		}
	}
}

func TestRegisterJoinBootstrapPreservesProjectedLeftOrderWhenOnlyRightJoinColumnIndexed(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewUint64(7)},
			{types.NewUint64(2), types.NewUint64(7)},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(7)},
			{types.NewUint64(11), types.NewUint64(7)},
		},
	})
	mgr := NewManager(s, s)
	p := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1}
	got, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 151, Predicates: []Predicate{p},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	gotRows := got.Update[0].Inserts
	wantRows := []types.ProductValue{
		{types.NewUint64(1), types.NewUint64(7)},
		{types.NewUint64(1), types.NewUint64(7)},
		{types.NewUint64(2), types.NewUint64(7)},
		{types.NewUint64(2), types.NewUint64(7)},
	}
	if len(gotRows) != len(wantRows) {
		t.Fatalf("insert count = %d, want %d", len(gotRows), len(wantRows))
	}
	for i := range wantRows {
		if !gotRows[i][0].Equal(wantRows[i][0]) || !gotRows[i][1].Equal(wantRows[i][1]) {
			t.Fatalf("insert[%d] = %v, want %v", i, gotRows[i], wantRows[i])
		}
	}
}

func TestUnregisterJoinFinalDeltaPreservesProjectedLeftOrderWhenOnlyLeftJoinColumnIndexed(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewUint64(7)},
			{types.NewUint64(2), types.NewUint64(7)},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(7)},
			{types.NewUint64(11), types.NewUint64(7)},
		},
	})
	mgr := NewManager(s, s)
	p := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 16, Predicates: []Predicate{p},
	}, view); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	got, err := mgr.UnregisterSet(types.ConnectionID{1}, 16, view)
	if err != nil {
		t.Fatalf("UnregisterSet = %v", err)
	}
	if len(got.Update) != 1 {
		t.Fatalf("update count = %d, want 1", len(got.Update))
	}
	gotRows := got.Update[0].Deletes
	wantRows := []types.ProductValue{
		{types.NewUint64(1), types.NewUint64(7)},
		{types.NewUint64(1), types.NewUint64(7)},
		{types.NewUint64(2), types.NewUint64(7)},
		{types.NewUint64(2), types.NewUint64(7)},
	}
	if len(gotRows) != len(wantRows) {
		t.Fatalf("delete count = %d, want %d", len(gotRows), len(wantRows))
	}
	for i := range wantRows {
		if !gotRows[i][0].Equal(wantRows[i][0]) || !gotRows[i][1].Equal(wantRows[i][1]) {
			t.Fatalf("delete[%d] = %v, want %v", i, gotRows[i], wantRows[i])
		}
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
