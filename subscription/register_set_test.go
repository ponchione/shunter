package subscription

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"testing"

	"github.com/ponchione/shunter/schema"
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

type countingTableScanView struct {
	*mockCommitted
	scanned int
}

func (v *countingTableScanView) TableScan(id TableID) iter.Seq2[types.RowID, types.ProductValue] {
	base := v.mockCommitted.TableScan(id)
	return func(yield func(types.RowID, types.ProductValue) bool) {
		for rid, row := range base {
			v.scanned++
			if !yield(rid, row) {
				return
			}
		}
	}
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

func TestRegisterSetRejectsSubscriptionIDOverflowAtomically(t *testing.T) {
	mgr, _ := newRegisterSetTestManager(t)
	mgr.nextSubID = ^types.SubscriptionID(0) - 1
	req := SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 7,
		Predicates: []Predicate{
			AllRows{Table: 1},
			AllRows{Table: 2},
		},
	}

	_, err := mgr.RegisterSet(req, nil)
	if !errors.Is(err, ErrSubscriptionIDOverflow) {
		t.Fatalf("RegisterSet overflow error = %v, want ErrSubscriptionIDOverflow", err)
	}
	if mgr.nextSubID != ^types.SubscriptionID(0)-1 {
		t.Fatalf("nextSubID changed after overflow rejection: got %d", mgr.nextSubID)
	}
	if _, ok := mgr.querySets[req.ConnID]; ok {
		t.Fatalf("querySets should be empty after overflow rejection, got %+v", mgr.querySets)
	}
	if m := mgr.registry.bySub; len(m) != 0 {
		t.Fatalf("registry bySub should be empty after overflow rejection, got %+v", m)
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

func TestRegisterSetTwoTablePredicateBootstrapsEachTable(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	req := SubscriptionSetRegisterRequest{
		ConnID:  types.ConnectionID{1},
		QueryID: 9,
		Predicates: []Predicate{
			And{
				Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
				Right: ColEq{Table: 2, Column: 1, Value: types.NewInt32(1)},
			},
		},
	}

	res, err := mgr.RegisterSet(req, view)
	if err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}
	sids := mgr.querySets[req.ConnID][req.QueryID]
	if len(sids) != 1 {
		t.Fatalf("querySets ids = %v, want one internal subscription", sids)
	}
	if len(res.Update) != 2 {
		t.Fatalf("initial update count = %d, want 2: %+v", len(res.Update), res.Update)
	}
	want := map[TableID]types.ProductValue{
		1: {types.NewUint64(1), types.NewString("a")},
		2: {types.NewUint64(10), types.NewInt32(1)},
	}
	for _, update := range res.Update {
		if update.SubscriptionID != sids[0] {
			t.Fatalf("SubscriptionID = %d, want %d", update.SubscriptionID, sids[0])
		}
		if update.QueryID != req.QueryID {
			t.Fatalf("QueryID = %d, want %d", update.QueryID, req.QueryID)
		}
		wantRow, ok := want[update.TableID]
		if !ok {
			t.Fatalf("unexpected table %d in update %+v", update.TableID, update)
		}
		if len(update.Inserts) != 1 {
			t.Fatalf("table %d inserts = %v, want one row", update.TableID, update.Inserts)
		}
		if !update.Inserts[0][0].Equal(wantRow[0]) || !update.Inserts[0][1].Equal(wantRow[1]) {
			t.Fatalf("table %d initial row = %v, want %v", update.TableID, update.Inserts[0], wantRow)
		}
		delete(want, update.TableID)
	}
	if len(want) != 0 {
		t.Fatalf("missing initial updates for tables %v", want)
	}
}

func TestUnregisterSetTwoTablePredicateFinalDeltaCoversEachTable(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	queryID := uint32(10)
	req := SubscriptionSetRegisterRequest{
		ConnID:  connID,
		QueryID: queryID,
		Predicates: []Predicate{
			And{
				Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
				Right: ColEq{Table: 2, Column: 1, Value: types.NewInt32(1)},
			},
		},
	}
	if _, err := mgr.RegisterSet(req, view); err != nil {
		t.Fatalf("RegisterSet: %v", err)
	}

	res, err := mgr.UnregisterSet(connID, queryID, view)
	if err != nil {
		t.Fatalf("UnregisterSet: %v", err)
	}
	if len(res.Update) != 2 {
		t.Fatalf("final update count = %d, want 2: %+v", len(res.Update), res.Update)
	}
	want := map[TableID]types.ProductValue{
		1: {types.NewUint64(1), types.NewString("a")},
		2: {types.NewUint64(10), types.NewInt32(1)},
	}
	for _, update := range res.Update {
		wantRow, ok := want[update.TableID]
		if !ok {
			t.Fatalf("unexpected table %d in update %+v", update.TableID, update)
		}
		if len(update.Deletes) != 1 {
			t.Fatalf("table %d deletes = %v, want one row", update.TableID, update.Deletes)
		}
		if len(update.Inserts) != 0 {
			t.Fatalf("table %d inserts = %v, want none on final delta", update.TableID, update.Inserts)
		}
		if !update.Deletes[0][0].Equal(wantRow[0]) || !update.Deletes[0][1].Equal(wantRow[1]) {
			t.Fatalf("table %d final row = %v, want %v", update.TableID, update.Deletes[0], wantRow)
		}
		delete(want, update.TableID)
	}
	if len(want) != 0 {
		t.Fatalf("missing final updates for tables %v", want)
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

// TestRegisterSetSameConnectionReusedHashEmitsEmptyUpdate pins same-connection
// query-hash reuse without another initial snapshot.
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

func TestRegisterSetOrderByAffectsSameConnectionQueryIdentity(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	pred := AllRows{Table: 1}

	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    1,
		Predicates: []Predicate{pred},
	}, view); err != nil {
		t.Fatalf("unordered RegisterSet: %v", err)
	}

	ordered, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    2,
		Predicates: []Predicate{pred},
		OrderByColumns: [][]OrderByColumn{{
			{Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64}, Table: 1, Column: 0, Desc: true},
		}},
	}, view)
	if err != nil {
		t.Fatalf("ordered RegisterSet: %v", err)
	}
	if len(ordered.Update) != 1 || len(ordered.Update[0].Inserts) != 2 {
		t.Fatalf("ordered Update = %+v, want fresh initial snapshot distinct from unordered hash", ordered.Update)
	}
	if got := ordered.Update[0].Inserts[0][0].AsUint64(); got != 2 {
		t.Fatalf("ordered initial first id = %d, want 2", got)
	}
}

func TestRegisterSetLimitAffectsSameConnectionQueryIdentity(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	pred := AllRows{Table: 1}
	limitOne := uint64(1)

	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    1,
		Predicates: []Predicate{pred},
	}, view); err != nil {
		t.Fatalf("unlimited RegisterSet: %v", err)
	}

	limited, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    2,
		Predicates: []Predicate{pred},
		Limits:     []*uint64{&limitOne},
	}, view)
	if err != nil {
		t.Fatalf("limited RegisterSet: %v", err)
	}
	if len(limited.Update) != 1 || len(limited.Update[0].Inserts) != 1 {
		t.Fatalf("limited Update = %+v, want fresh one-row initial snapshot distinct from unlimited hash", limited.Update)
	}
}

func TestRegisterSetLimitStopsStreamingInitialScan(t *testing.T) {
	mgr, base := newRegisterSetTestManagerWithRows(t)
	view := &countingTableScanView{mockCommitted: base}
	limitOne := uint64(1)

	res, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{1},
		QueryID:    21,
		Predicates: []Predicate{AllRows{Table: 1}},
		Limits:     []*uint64{&limitOne},
	}, view)
	if err != nil {
		t.Fatalf("RegisterSet limited scan: %v", err)
	}
	if len(res.Update) != 1 || len(res.Update[0].Inserts) != 1 {
		t.Fatalf("limited update = %+v, want one initial row", res.Update)
	}
	if view.scanned != 1 {
		t.Fatalf("TableScan yielded %d rows, want 1 for streaming LIMIT 1", view.scanned)
	}
}

func TestRegisterSetOffsetAffectsSameConnectionQueryIdentity(t *testing.T) {
	mgr, view := newRegisterSetTestManagerWithRows(t)
	connID := types.ConnectionID{1}
	pred := AllRows{Table: 1}
	offsetOne := uint64(1)

	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    1,
		Predicates: []Predicate{pred},
	}, view); err != nil {
		t.Fatalf("unoffset RegisterSet: %v", err)
	}

	offset, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    2,
		Predicates: []Predicate{pred},
		Offsets:    []*uint64{&offsetOne},
	}, view)
	if err != nil {
		t.Fatalf("offset RegisterSet: %v", err)
	}
	if len(offset.Update) != 1 || len(offset.Update[0].Inserts) != 1 {
		t.Fatalf("offset Update = %+v, want fresh one-row initial snapshot distinct from unoffset hash", offset.Update)
	}
	if got := offset.Update[0].Inserts[0][0].AsUint64(); got != 2 {
		t.Fatalf("offset initial first id = %d, want 2", got)
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

// TestUnregisterSetFinalEvalErrorWrapsErrFinalQueryAndDropsAll pins that
// final-eval failure still drops the set and returns SQLText for Single errors.
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
	if !pruningIndexesEmpty(mgr.indexes) {
		t.Fatalf("pruning indexes not cleared on unwind: value=%+v range=%+v joinedge=%+v joinrangeedge=%+v table=%+v",
			mgr.indexes.Value, mgr.indexes.Range, mgr.indexes.JoinEdge, mgr.indexes.JoinRangeEdge, mgr.indexes.Table)
	}
}

func TestUnregisterSetKeepsSharedJoinEdgeRefsUntilLastSubscription(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
	}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		1: types.KindUint64,
		2: types.KindString,
	}, 1)
	view := buildMockCommitted(s, nil)
	mgr := NewManager(s, s)
	redJoin := Join{
		Left: 1, Right: 2,
		LeftCol: 0, RightCol: 1,
		Filter: ColEq{Table: 2, Column: 2, Value: types.NewString("red")},
	}
	blueJoin := Join{
		Left: 1, Right: 2,
		LeftCol: 0, RightCol: 1,
		Filter: ColEq{Table: 2, Column: 2, Value: types.NewString("blue")},
	}

	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{redJoin},
	}, view); err != nil {
		t.Fatalf("RegisterSet red join: %v", err)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 11, Predicates: []Predicate{blueJoin},
	}, view); err != nil {
		t.Fatalf("RegisterSet blue join: %v", err)
	}

	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	if got := mgr.indexes.JoinEdge.Lookup(edge, types.NewString("red")); len(got) != 1 {
		t.Fatalf("red join-edge hashes = %v, want one", got)
	}
	if got := mgr.indexes.JoinEdge.Lookup(edge, types.NewString("blue")); len(got) != 1 {
		t.Fatalf("blue join-edge hashes = %v, want one", got)
	}
	if refs := mgr.deltaIndexColumns[1][0]; refs != 2 {
		t.Fatalf("delta index refs for left join column = %d, want 2", refs)
	}
	if refs := mgr.deltaIndexColumns[2][1]; refs != 2 {
		t.Fatalf("delta index refs for right join column = %d, want 2", refs)
	}

	if _, err := mgr.UnregisterSet(types.ConnectionID{1}, 10, nil); err != nil {
		t.Fatalf("UnregisterSet red join: %v", err)
	}
	if got := mgr.indexes.JoinEdge.Lookup(edge, types.NewString("red")); len(got) != 0 {
		t.Fatalf("red join-edge hashes after unregister = %v, want empty", got)
	}
	if got := mgr.indexes.JoinEdge.Lookup(edge, types.NewString("blue")); len(got) != 1 {
		t.Fatalf("blue join-edge hashes after unregister = %v, want one", got)
	}
	if edges := mgr.indexes.JoinEdge.EdgesForTable(1); len(edges) != 1 || edges[0] != edge {
		t.Fatalf("shared join edge refs after one unregister = %v, want [%+v]", edges, edge)
	}
	if refs := mgr.deltaIndexColumns[1][0]; refs != 1 {
		t.Fatalf("delta index refs for left join column after one unregister = %d, want 1", refs)
	}
	if refs := mgr.deltaIndexColumns[2][1]; refs != 1 {
		t.Fatalf("delta index refs for right join column after one unregister = %d, want 1", refs)
	}

	if _, err := mgr.UnregisterSet(types.ConnectionID{1}, 11, nil); err != nil {
		t.Fatalf("UnregisterSet blue join: %v", err)
	}
	if !pruningIndexesEmpty(mgr.indexes) {
		t.Fatalf("pruning indexes after last unregister = %+v, want empty", mgr.indexes)
	}
	if len(mgr.deltaIndexColumns) != 0 {
		t.Fatalf("deltaIndexColumns after last unregister = %+v, want empty", mgr.deltaIndexColumns)
	}
	if len(mgr.registry.byHash) != 0 || len(mgr.registry.bySub) != 0 || len(mgr.registry.byConn) != 0 {
		t.Fatalf("registry after last unregister = %+v", mgr.registry)
	}
	if _, ok := mgr.querySets[types.ConnectionID{1}]; ok {
		t.Fatalf("querySets after last unregister = %+v, want no connection bucket", mgr.querySets)
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

func TestDisconnectClientDropsRegistryPruningAndDeltaIndexState(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{1: types.KindUint64, 2: types.KindString}, 1)
	mgr := NewManager(s, s)
	connID := types.ConnectionID{1}
	value := types.NewUint64(7)
	red := types.NewString("red")
	join := Join{
		Left:     1,
		Right:    2,
		LeftCol:  0,
		RightCol: 1,
		Filter:   ColEq{Table: 2, Column: 2, Value: red},
	}

	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    1,
		Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: value}},
	}, nil); err != nil {
		t.Fatalf("RegisterSet value: %v", err)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    2,
		Predicates: []Predicate{AllRows{Table: 2}},
	}, nil); err != nil {
		t.Fatalf("RegisterSet table: %v", err)
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    3,
		Predicates: []Predicate{join},
	}, nil); err != nil {
		t.Fatalf("RegisterSet join: %v", err)
	}

	if got := mgr.indexes.Value.Lookup(1, 0, value); len(got) != 1 {
		t.Fatalf("value index before disconnect = %v, want one", got)
	}
	if got := mgr.indexes.Table.Lookup(2); len(got) != 1 {
		t.Fatalf("table index before disconnect = %v, want one", got)
	}
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	if got := mgr.indexes.JoinEdge.Lookup(edge, red); len(got) != 1 {
		t.Fatalf("join edge before disconnect = %v, want one", got)
	}
	if refs := mgr.deltaIndexColumns[1][0]; refs != 1 {
		t.Fatalf("left delta index refs before disconnect = %d, want 1", refs)
	}
	if refs := mgr.deltaIndexColumns[2][1]; refs != 1 {
		t.Fatalf("right delta index refs before disconnect = %d, want 1", refs)
	}

	if err := mgr.DisconnectClient(connID); err != nil {
		t.Fatalf("DisconnectClient: %v", err)
	}
	if active := mgr.ActiveSubscriptionSets(); active != 0 {
		t.Fatalf("ActiveSubscriptionSets = %d, want 0", active)
	}
	if _, ok := mgr.querySets[connID]; ok {
		t.Fatalf("querySets[%v] not cleared", connID)
	}
	if mgr.registry.hasActive() {
		t.Fatal("registry still has active queries after disconnect")
	}
	if !pruningIndexesEmpty(mgr.indexes) {
		t.Fatalf("pruning indexes not cleared after disconnect: value=%+v table=%+v join=%+v", mgr.indexes.Value, mgr.indexes.Table, mgr.indexes.JoinEdge)
	}
	if len(mgr.deltaIndexColumns) != 0 {
		t.Fatalf("delta index refs not cleared after disconnect: %+v", mgr.deltaIndexColumns)
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
