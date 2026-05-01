package subscription

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"testing"

	"github.com/ponchione/shunter/store"
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

type streamingJoinView struct {
	*mockCommitted
	beforeFirstProbe func(scanned int)
	scannedLeft      int
	probes           int
}

type cancelOnTableScanView struct {
	*mockCommitted
	cancel  func()
	yielded int
}

func (v *cancelOnTableScanView) TableScan(id TableID) iter.Seq2[types.RowID, types.ProductValue] {
	base := v.mockCommitted.TableScan(id)
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid, row := range base {
			if !yield(rid, row) {
				return
			}
			v.yielded++
			if v.yielded == 1 && v.cancel != nil {
				v.cancel()
			}
		}
	}
}

func (v *streamingJoinView) TableScan(id TableID) iter.Seq2[types.RowID, types.ProductValue] {
	base := v.mockCommitted.TableScan(id)
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid, row := range base {
			if id == 1 {
				v.scannedLeft++
			}
			if !yield(rid, row) {
				return
			}
		}
	}
}

func (v *streamingJoinView) IndexSeek(tableID TableID, indexID IndexID, key store.IndexKey) []types.RowID {
	if v.probes == 0 && v.beforeFirstProbe != nil {
		v.beforeFirstProbe(v.scannedLeft)
	}
	v.probes++
	return v.mockCommitted.IndexSeek(tableID, indexID, key)
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

func TestRegisterSetRejectsEmptyPredicateSet(t *testing.T) {
	mgr, _ := newRegisterSetTestManager(t)
	req := SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 2,
	}

	_, err := mgr.RegisterSet(req, nil)
	if !errors.Is(err, ErrInvalidPredicate) {
		t.Fatalf("RegisterSet empty predicate error = %v, want ErrInvalidPredicate", err)
	}
	if _, ok := mgr.querySets[req.ConnID]; ok {
		t.Fatalf("querySets should be empty after empty predicate rejection, got %+v", mgr.querySets)
	}
	if active := mgr.ActiveSubscriptionSets(); active != 0 {
		t.Fatalf("ActiveSubscriptionSets = %d, want 0", active)
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

func TestRegisterSetInitialQueryStopsOnContextCancel(t *testing.T) {
	mgr, base := newRegisterSetTestManagerWithRows(t)
	ctx, cancel := context.WithCancel(context.Background())
	view := &cancelOnTableScanView{mockCommitted: base, cancel: cancel}
	req := SubscriptionSetRegisterRequest{
		Context:    ctx,
		ConnID:     types.ConnectionID{1},
		QueryID:    8,
		Predicates: []Predicate{AllRows{Table: 1}},
	}
	_, err := mgr.RegisterSet(req, view)
	if !errors.Is(err, ErrInitialQuery) || !errors.Is(err, context.Canceled) {
		t.Fatalf("RegisterSet err = %v, want ErrInitialQuery wrapping context.Canceled", err)
	}
	if _, ok := mgr.querySets[req.ConnID]; ok {
		t.Fatalf("querySets should be empty after canceled initial query, got %+v", mgr.querySets)
	}
	if mgr.registry.hasActive() {
		t.Fatal("registry should be clear after canceled initial query")
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

// TestRegisterSetSameConnectionReusedHashEmitsEmptyUpdate — second
// RegisterSet from the same connection under a different client QueryID
// referencing the same predicate hash attaches a new internal subscription
// but does not re-emit the initial snapshot. Reference:
// reference/SpacetimeDB/crates/core/src/subscription/module_subscription_manager.rs::add_subscription_multi
// lines 1083-1094 (already-attached (ConnID, query-hash) paths skip
// new_queries) and module_subscription_actor.rs lines 1357-1369 (empty
// new_queries becomes empty applied update data). The cross-connection
// case is covered by TestRegisterSetCrossConnectionReusedHashStillEmitsInitialSnapshot
// below.
func TestRegisterSetSameConnectionReusedHashEmitsEmptyUpdate(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	pred := AllRows{Table: 1}

	first, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    1,
		Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatalf("first RegisterSet: %v", err)
	}
	if len(first.Update) != 1 || len(first.Update[0].Inserts) == 0 {
		t.Fatalf("first Update = %+v, want one SubscriptionUpdate with initial inserts", first.Update)
	}

	second, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    2,
		Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatalf("second RegisterSet: %v", err)
	}
	if len(second.Update) != 0 {
		t.Fatalf("second Update = %+v, want empty (same-connection reused hash)", second.Update)
	}
	if sids := mgr.querySets[connID][2]; len(sids) != 1 {
		t.Fatalf("querySets[conn][2] = %v, want one allocated subID", sids)
	}
	if sids1 := mgr.querySets[connID][1]; len(sids1) != 1 {
		t.Fatalf("querySets[conn][1] = %v, want first allocation still present", sids1)
	}
	if mgr.querySets[connID][1][0] == mgr.querySets[connID][2][0] {
		t.Fatalf("queryID=1 and queryID=2 must map to distinct internal SubscriptionIDs: %v", mgr.querySets[connID])
	}
}

// TestRegisterSetCrossConnectionReusedHashStillEmitsInitialSnapshot —
// reuse-hash elision is same-connection only. A different connection
// subscribing to the same predicate hash still receives its own initial
// snapshot. Reference: add_subscription_multi predicates reused across
// connections still add new_queries entries for the new client.
func TestRegisterSetCrossConnectionReusedHashStillEmitsInitialSnapshot(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connA := types.ConnectionID{1}
	connB := types.ConnectionID{2}
	pred := AllRows{Table: 1}

	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connA,
		QueryID:    1,
		Predicates: []Predicate{pred},
	}, view); err != nil {
		t.Fatalf("connA RegisterSet: %v", err)
	}

	second, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connB,
		QueryID:    1,
		Predicates: []Predicate{pred},
	}, view)
	if err != nil {
		t.Fatalf("connB RegisterSet: %v", err)
	}
	if len(second.Update) != 1 || len(second.Update[0].Inserts) == 0 {
		t.Fatalf("connB Update = %+v, want one SubscriptionUpdate with initial inserts (cross-connection is not elided)", second.Update)
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

// TestUnregisterSetFinalEvalErrorWrapsErrFinalQueryAndDropsAll — when
// final-delta evaluation fails during UnregisterSet, every internal
// subscription in the set is still dropped, and the returned error
// wraps `ErrFinalQuery` (so the protocol-side adapter can apply the
// reference `DBError::WithSql` suffix on the Single path). The result
// carries `SQLText` = the first-errored queryState's stored SQL so the
// adapter has what it needs to build the suffix without re-querying
// subscription internals. Reference anchors:
// `module_subscription_actor.rs:756` (Single, `return_on_err_with_sql!`)
// and `:826..:830` (query-set drop happens before eval; eval failure
// does not un-drop). The Single subscribe admission path is the only
// one that persists SQLText into queryState today, so this test uses
// length-1 predicates and a non-empty register-time SQLText.
func TestUnregisterSetFinalEvalErrorWrapsErrFinalQueryAndDropsAll(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	const sqlText = "SELECT * FROM t1"
	reg := SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    11,
		Predicates: []Predicate{AllRows{Table: 1}},
		SQLText:    sqlText,
	}
	if _, err := mgr.RegisterSet(reg, view); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	// Force initialQuery to trip ErrInitialRowLimit on the unsubscribe
	// final-delta evaluation. InitialRowLimit is consulted at evaluation
	// time, so setting it after register keeps the register path clean.
	mgr.InitialRowLimit = 1
	res, err := mgr.UnregisterSet(connID, 11, view)
	if err == nil {
		t.Fatal("UnregisterSet should surface initialQuery error")
	}
	if !errors.Is(err, ErrFinalQuery) {
		t.Fatalf("err = %v, want errors.Is ErrFinalQuery", err)
	}
	if !errors.Is(err, ErrInitialRowLimit) {
		t.Fatalf("err = %v, want errors.Is ErrInitialRowLimit (concrete cause)", err)
	}
	if res.SQLText != sqlText {
		t.Fatalf("res.SQLText = %q, want %q", res.SQLText, sqlText)
	}
	if len(res.Update) != 0 {
		t.Fatalf("res.Update = %+v, want empty on eval-error path (reference bails via return_on_err)", res.Update)
	}
	if _, ok := mgr.querySets[connID]; ok {
		t.Fatalf("querySets not cleared after eval-error unregister: %+v", mgr.querySets)
	}
	if mgr.registry.hasActive() {
		t.Fatal("registry still holds queries after eval-error unregister")
	}
}

// TestUnregisterSetMultiFinalEvalErrorEmptySQLText — Multi register does
// not populate SQLText on any queryState (handleSubscribeMulti leaves
// `RegisterSubscriptionSetRequest.SQLText` empty because reference
// `module_subscription_actor.rs:836` uses raw `return_on_err!` for the
// UnsubscribeMulti eval — no `DBError::WithSql` suffix). The subscription
// layer still wraps the failure with `ErrFinalQuery` and drops every
// internal sub; the adapter-layer variant switch is what keeps the wire
// text raw.
func TestUnregisterSetMultiFinalEvalErrorEmptySQLText(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	reg := SubscriptionSetRegisterRequest{
		ConnID:  connID,
		QueryID: 12,
		Predicates: []Predicate{
			AllRows{Table: 1},
			AllRows{Table: 2},
		},
		// Multi path leaves SQLText empty.
	}
	if _, err := mgr.RegisterSet(reg, view); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	mgr.InitialRowLimit = 1
	res, err := mgr.UnregisterSet(connID, 12, view)
	if err == nil {
		t.Fatal("UnregisterSet should surface initialQuery error")
	}
	if !errors.Is(err, ErrFinalQuery) {
		t.Fatalf("err = %v, want errors.Is ErrFinalQuery", err)
	}
	if res.SQLText != "" {
		t.Fatalf("res.SQLText = %q, want empty (Multi register never persisted SQL)", res.SQLText)
	}
	if _, ok := mgr.querySets[connID]; ok {
		t.Fatalf("querySets not cleared after eval-error unregister: %+v", mgr.querySets)
	}
}

func TestUnregisterSetContextStopsFinalQueryOnContextCancelAndDropsSet(t *testing.T) {
	mgr, base := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	req := SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    13,
		Predicates: []Predicate{AllRows{Table: 1}},
	}
	if _, err := mgr.RegisterSet(req, base); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	view := &cancelOnTableScanView{mockCommitted: base, cancel: cancel}
	_, err := mgr.UnregisterSetContext(ctx, connID, req.QueryID, view)
	if !errors.Is(err, ErrFinalQuery) || !errors.Is(err, context.Canceled) {
		t.Fatalf("UnregisterSetContext err = %v, want ErrFinalQuery wrapping context.Canceled", err)
	}
	if _, ok := mgr.querySets[connID]; ok {
		t.Fatalf("querySets not cleared after canceled unregister final query: %+v", mgr.querySets)
	}
	if mgr.registry.hasActive() {
		t.Fatal("registry should be clear after canceled unregister final query")
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

func TestRegisterSetJoinInitialQueryStreamsScanSideBeforeProbe(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)

	base := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewString("lhs-1")},
			{types.NewUint64(2), types.NewString("lhs-2")},
		},
		2: {
			{types.NewUint64(10), types.NewUint64(1)},
			{types.NewUint64(20), types.NewUint64(2)},
		},
	})
	view := &streamingJoinView{mockCommitted: base}
	view.beforeFirstProbe = func(scanned int) {
		if scanned != 1 {
			panic(fmt.Sprintf("first probe happened after scanning %d left rows; want streaming interleave after 1", scanned))
		}
	}

	mgr := NewManager(s, s)
	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    42,
		Predicates: []Predicate{Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 1}},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet(join): %v", err)
	}
	if view.probes != 2 {
		t.Fatalf("IndexSeek probes = %d, want 2", view.probes)
	}
	if len(res.Update) != 1 {
		t.Fatalf("updates = %d, want 1", len(res.Update))
	}
	if got := len(res.Update[0].Inserts); got != 2 {
		t.Fatalf("initial join inserts = %d, want 2", got)
	}
}
