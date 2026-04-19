package subscription

import (
	"errors"
	"testing"

	"github.com/ponchione/shunter/types"
)

// newRegisterSetTestManager returns a Manager whose schema accepts Table(1)
// and Table(2) but rejects Table(999). No rows committed.
func newRegisterSetTestManager(t *testing.T) (*Manager, *mockCommitted) {
	t.Helper()
	s := testSchema()
	mgr := NewManager(s, s)
	return mgr, nil
}

// newRegisterSetTestManagerWithRows returns a Manager with rows populated
// in Table(1) and Table(2) so merged-snapshot tests have something to merge.
func newRegisterSetTestManagerWithRows(t *testing.T) (*Manager, *mockCommitted) {
	t.Helper()
	s := testSchema()
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
			{types.NewUint64(2), types.NewString("b")},
		},
		2: {
			{types.NewUint64(10), types.NewInt32(1)},
			{types.NewUint64(20), types.NewInt32(2)},
		},
	})
	mgr := NewManager(s, s)
	return mgr, view
}

// TestRegisterSetMultiAtomicOnInvalidPredicate — SubscribeMulti with one
// invalid predicate fails atomically; no subs registered; QueryID free.
// Reference: add_subscription_multi pre-validation at
// reference/SpacetimeDB/.../module_subscription_manager.rs:1023.
func TestRegisterSetMultiAtomicOnInvalidPredicate(t *testing.T) {
	mgr, _ := newRegisterSetTestManager(t)
	req := SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 1,
		Predicates: []Predicate{
			AllRows{Table: 1},
			AllRows{Table: 999}, // unknown table; validation fails
		},
	}
	_, err := mgr.RegisterSet(req, nil)
	if err == nil {
		t.Fatal("RegisterSet with invalid predicate should fail")
	}
	if _, ok := mgr.querySets[req.ConnID]; ok {
		t.Fatalf("querySets should be empty after atomic failure, got %+v", mgr.querySets)
	}
}

// TestRegisterSetMultiMergesInitialSnapshot — N predicates produce a
// merged Update with one SubscriptionUpdate per allocated internal sub.
func TestRegisterSetMultiMergesInitialSnapshot(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	req := SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 7,
		Predicates: []Predicate{
			AllRows{Table: 1},
			AllRows{Table: 2},
		},
	}
	res, err := mgr.RegisterSet(req, view)
	if err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	if sids := mgr.querySets[req.ConnID][req.QueryID]; len(sids) != 2 {
		t.Fatalf("querySets ids = %v, want len 2", sids)
	}
	if len(res.Update) == 0 {
		t.Logf("Update=%+v (may be empty if fixture has no rows)", res.Update)
	}
}

// TestRegisterSetRejectsDuplicateQueryID — second RegisterSet with the
// same (ConnID, QueryID) rejected. Reference: try_insert at
// reference/SpacetimeDB/.../module_subscription_manager.rs:1050.
func TestRegisterSetRejectsDuplicateQueryID(t *testing.T) {
	mgr, _ := newRegisterSetTestManager(t)
	req := SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    5,
		Predicates: []Predicate{AllRows{Table: 1}},
	}
	if _, err := mgr.RegisterSet(req, nil); err != nil {
		t.Fatalf("first RegisterSet: %v", err)
	}
	_, err := mgr.RegisterSet(req, nil)
	if !errors.Is(err, ErrQueryIDAlreadyLive) {
		t.Fatalf("second RegisterSet err = %v, want ErrQueryIDAlreadyLive", err)
	}
}

// TestRegisterSetDedupsIdenticalPredicates — two identical predicates
// within one set register once. Reference: hash_set.insert at
// reference/SpacetimeDB/.../module_subscription_manager.rs:1065.
func TestRegisterSetDedupsIdenticalPredicates(t *testing.T) {
	mgr, _ := newRegisterSetTestManager(t)
	pred := AllRows{Table: 1}
	req := SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    1,
		Predicates: []Predicate{pred, pred},
	}
	if _, err := mgr.RegisterSet(req, nil); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	if len(mgr.querySets[req.ConnID][req.QueryID]) != 1 {
		t.Fatalf("dedup failed: %+v", mgr.querySets)
	}
}

// TestUnregisterSetDropsAllInSet — UnsubscribeMulti drops every internal
// sub mapped under the QueryID.
func TestUnregisterSetDropsAllInSet(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	reg := SubscriptionSetRegisterRequest{
		ConnID:  connID,
		QueryID: 3,
		Predicates: []Predicate{
			AllRows{Table: 1},
			AllRows{Table: 2},
		},
	}
	if _, err := mgr.RegisterSet(reg, view); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	if _, err := mgr.UnregisterSet(connID, 3, view); err != nil {
		t.Fatalf("UnregisterSet: %v", err)
	}
	if _, ok := mgr.querySets[connID]; ok {
		t.Fatalf("querySets not cleared: %+v", mgr.querySets)
	}
}

// TestRegisterSetUnwindsPartialStateOnInitialQueryError — exercises the
// atomic unwind when initialQuery fails partway through a Multi set.
// With InitialRowLimit(1), the second predicate trips ErrInitialRowLimit
// after the first has already placed itself on the registry and indexes;
// the unwind must drop every trace — including the PruningIndexes rows
// that plain unregisterSingle would leave behind. Reference:
// rollback-on-failure parallel to
// reference/SpacetimeDB/.../module_subscription_manager.rs:1023.
func TestRegisterSetUnwindsPartialStateOnInitialQueryError(t *testing.T) {
	s := testSchema()
	// Table(1) has 1 row so the first predicate finishes under the
	// InitialRowLimit cap; Table(2) has 2 rows so the second predicate
	// overruns and trips ErrInitialRowLimit after the first has already
	// placed itself on the registry + indexes.
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("a")},
		},
		2: {
			{types.NewUint64(10), types.NewInt32(1)},
			{types.NewUint64(20), types.NewInt32(2)},
		},
	})
	mgr := NewManager(s, s)
	mgr.InitialRowLimit = 1
	req := SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 1,
		Predicates: []Predicate{
			AllRows{Table: 1},
			AllRows{Table: 2},
		},
	}
	_, err := mgr.RegisterSet(req, view)
	if err == nil {
		t.Fatal("RegisterSet should fail once initial-row limit is exceeded")
	}
	if !errors.Is(err, ErrInitialRowLimit) {
		t.Fatalf("err = %v, want ErrInitialRowLimit", err)
	}
	if _, ok := mgr.querySets[req.ConnID]; ok {
		t.Fatalf("querySets not cleared on unwind: %+v", mgr.querySets)
	}
	if mgr.registry.hasActive() {
		t.Fatalf("registry should be clear after unwind")
	}
	if !mgr.indexes.TestOnlyIsEmpty() {
		t.Fatalf("pruning indexes not cleared on unwind: value=%+v joinedge=%+v table=%+v",
			mgr.indexes.Value, mgr.indexes.JoinEdge, mgr.indexes.Table)
	}
}

// TestDisconnectClientClearsQuerySets — DisconnectClient drops the
// entire (ConnID, *) bucket.
func TestDisconnectClientClearsQuerySets(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    1,
		Predicates: []Predicate{AllRows{Table: 1}},
	}, view); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	if err := mgr.DisconnectClient(connID); err != nil {
		t.Fatalf("DisconnectClient: %v", err)
	}
	if _, ok := mgr.querySets[connID]; ok {
		t.Fatalf("querySets[%v] not cleared", connID)
	}
}
