package subscription

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestEvalNoActiveSubsReturnsImmediately(t *testing.T) {
	s := testSchema()
	mgr := NewManager(s, s)
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}, nil)
	// Should not panic and inbox (nil) should not be touched.
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
}

func TestEvalAndBroadcastUnblocksWhenFanOutClosed(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{9}
	done := make(chan struct{})

	go func() {
		mgr.EvalAndBroadcast(types.TxID(1), nil, nil, PostCommitMeta{
			CallerConnID:  &connID,
			CallerOutcome: &CallerOutcome{Kind: CallerOutcomeCommitted},
		})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("EvalAndBroadcast returned before fan-out was closed despite no receiver")
	case <-time.After(25 * time.Millisecond):
	}

	mgr.CloseFanOut()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("EvalAndBroadcast stayed blocked after CloseFanOut")
	}
}

func TestEvalAndBroadcastSkipsFanOutAfterClosedEvenWhenInboxReady(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{9}

	mgr.CloseFanOut()
	mgr.EvalAndBroadcast(types.TxID(1), nil, nil, PostCommitMeta{
		CallerConnID:  &connID,
		CallerOutcome: &CallerOutcome{Kind: CallerOutcomeCommitted},
	})

	select {
	case msg := <-inbox:
		t.Fatalf("EvalAndBroadcast sent after CloseFanOut: %+v", msg)
	default:
	}
}

func TestEvalCapturesCallerUpdatesFromFanoutEntry(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	connID := types.ConnectionID{1}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    10,
		Predicates: []Predicate{AllRows{Table: 1}},
	}, nil)
	var captured []SubscriptionUpdate
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{
		CallerConnID: &connID,
		CaptureCallerUpdates: func(updates []SubscriptionUpdate) {
			captured = updates
		},
	})
	msg := <-inbox
	want := msg.Fanout[connID]
	if len(captured) != len(want) {
		t.Fatalf("captured len=%d want %d", len(captured), len(want))
	}
	if len(captured) == 0 || captured[0].SubscriptionID != want[0].SubscriptionID {
		t.Fatalf("captured=%v want %v", captured, want)
	}
}

func TestEvalSingleTableColEqMatches(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}},
	}, nil)
	wantSubID := mgr.querySets[types.ConnectionID{1}][10][0]

	cs := simpleChangeset(1,
		[]types.ProductValue{
			{types.NewUint64(42), types.NewString("match")},
			{types.NewUint64(7), types.NewString("nope")},
		}, nil,
	)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	select {
	case msg := <-inbox:
		if len(msg.Fanout[types.ConnectionID{1}]) != 1 {
			t.Fatalf("fanout for conn1 = %v, want 1 update", msg.Fanout)
		}
		u := msg.Fanout[types.ConnectionID{1}][0]
		if len(u.Inserts) != 1 {
			t.Fatalf("Inserts = %v, want 1", u.Inserts)
		}
		if u.SubscriptionID != wantSubID {
			t.Fatalf("SubscriptionID = %d, want %d", u.SubscriptionID, wantSubID)
		}
	default:
		t.Fatal("no fanout message received")
	}
}

func TestEvalSkipsUnaffectedTables(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{AllRows{Table: 2}},
	}, nil)
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	select {
	case msg := <-inbox:
		if len(msg.Fanout) != 0 {
			t.Fatalf("expected empty fanout, got %v", msg.Fanout)
		}
	case <-make(chan struct{}):
	default:
		// ok: no message sent because fanout would be empty — but our
		// implementation still sends even empty fanouts. Accept both.
	}
}

func TestEvalTwoSubscribersSharedQuery(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{pred}}, nil)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: types.ConnectionID{2}, QueryID: 11, Predicates: []Predicate{pred}}, nil)
	want1 := mgr.querySets[types.ConnectionID{1}][10][0]
	want2 := mgr.querySets[types.ConnectionID{2}][11][0]

	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(42), types.NewString("x")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	msg := <-inbox
	if len(msg.Fanout) != 2 {
		t.Fatalf("fanout should reach both conns, got %v", msg.Fanout)
	}
	got1 := msg.Fanout[types.ConnectionID{1}][0].SubscriptionID
	got2 := msg.Fanout[types.ConnectionID{2}][0].SubscriptionID
	if got1 != want1 || got2 != want2 {
		t.Fatalf("subIDs wrong: got (%d,%d) want (%d,%d)", got1, got2, want1, want2)
	}
}

func TestEvalSameConnectionSameQueryProducesIndependentUpdates(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	c := types.ConnectionID{1}
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 10, Predicates: []Predicate{pred}}, nil)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 11, Predicates: []Predicate{pred}}, nil)
	wantA := mgr.querySets[c][10][0]
	wantB := mgr.querySets[c][11][0]

	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(42), types.NewString("x")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[c]
	if len(updates) != 2 {
		t.Fatalf("updates for shared connection = %v, want 2", updates)
	}
	seen := map[types.SubscriptionID]bool{}
	for _, u := range updates {
		seen[u.SubscriptionID] = true
	}
	if !seen[wantA] || !seen[wantB] {
		t.Fatalf("expected updates for subIDs %d and %d, got %v", wantA, wantB, updates)
	}
}

func TestEvalFanoutCarriesClientQueryIDForEachSubscription(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	c := types.ConnectionID{1}
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	queryIDs := []uint32{410, 920}
	for _, queryID := range queryIDs {
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: queryID, Predicates: []Predicate{pred}}, nil); err != nil {
			t.Fatalf("RegisterSet queryID=%d: %v", queryID, err)
		}
	}

	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(42), types.NewString("x")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[c]
	if len(updates) != len(queryIDs) {
		t.Fatalf("updates for shared connection = %v, want %d", updates, len(queryIDs))
	}
	seen := make(map[uint32]bool, len(updates))
	for _, update := range updates {
		queryID := queryIDForUpdate(t, update)
		if queryID == uint32(update.SubscriptionID) {
			t.Fatalf("QueryID should be the client-chosen ID, not internal SubscriptionID: update=%+v", update)
		}
		seen[queryID] = true
	}
	for _, queryID := range queryIDs {
		if !seen[queryID] {
			t.Fatalf("missing QueryID %d in fanout updates %+v", queryID, updates)
		}
	}
}

func queryIDForUpdate(t *testing.T, update SubscriptionUpdate) uint32 {
	t.Helper()
	field := reflect.ValueOf(update).FieldByName("QueryID")
	if !field.IsValid() {
		t.Fatalf("SubscriptionUpdate is missing client QueryID; update=%+v", update)
	}
	return uint32(field.Uint())
}

func TestEvalFanoutOrdersUpdatesByRegistrationWithinConnection(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	c := types.ConnectionID{1}
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}

	const subscriptionCount = 32
	for i := 0; i < subscriptionCount; i++ {
		queryID := uint32(100 + i)
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: queryID, Predicates: []Predicate{pred}}, nil); err != nil {
			t.Fatalf("RegisterSet queryID=%d: %v", queryID, err)
		}
	}
	want := mgr.registry.subscriptionsForConn(c)
	if len(want) != subscriptionCount {
		t.Fatalf("registered subscriptions = %v, want %d", want, subscriptionCount)
	}

	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(42), types.NewString("x")}}, nil)
	for attempt := 0; attempt < 64; attempt++ {
		mgr.EvalAndBroadcast(types.TxID(attempt+1), cs, nil, PostCommitMeta{})
		msg := <-inbox
		updates := msg.Fanout[c]
		if len(updates) != len(want) {
			t.Fatalf("attempt %d: updates for shared connection = %v, want %d", attempt, updates, len(want))
		}
		got := make([]types.SubscriptionID, len(updates))
		for i, u := range updates {
			got[i] = u.SubscriptionID
		}
		if !slices.Equal(got, want) {
			t.Fatalf("attempt %d: update order = %v, want registration order %v", attempt, got, want)
		}
	}
}

func TestEvalErrorQueuesSubscriptionErrorAndDropsConnection(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 2)
	observer := &recordingSubscriptionObserver{}
	mgr := NewManager(s, s, WithFanOutInbox(inbox), WithObserver(observer))
	c := types.ConnectionID{1}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 10, RequestID: 77, Predicates: []Predicate{AllRows{Table: 1}}}, nil)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 11, Predicates: []Predicate{AllRows{Table: 2}}}, nil)

	mgr.schema = nil
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(1), types.NewString("x")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	msg := <-inbox
	if len(msg.Errors[c]) != 1 {
		t.Fatalf("subscription errors for conn = %v, want 1", msg.Errors)
	}
	errMsg := msg.Errors[c][0]
	if errMsg.RequestID != 77 {
		t.Fatalf("RequestID = %d, want 77", errMsg.RequestID)
	}
	if errMsg.QueryHash != ComputeQueryHash(AllRows{Table: 1}, nil) {
		t.Fatalf("error hash = %s", errMsg.QueryHash)
	}
	if !strings.Contains(errMsg.Predicate, "AllRows") {
		t.Fatalf("predicate repr = %q, want AllRows", errMsg.Predicate)
	}
	if !strings.Contains(errMsg.Message, ErrSubscriptionEval.Error()) {
		t.Fatalf("error message = %q, want wrapped subscription eval error", errMsg.Message)
	}
	if errMsg.TotalHostExecutionDurationMicros == 0 {
		t.Fatal("TotalHostExecutionDurationMicros = 0, want non-zero (eval-path receipt seam)")
	}
	dropped := mgr.DrainDroppedClients()
	if len(dropped) != 1 || dropped[0] != c {
		t.Fatalf("dropped connections = %v, want [%v]", dropped, c)
	}
	if len(observer.evalErrors) != 1 {
		t.Fatalf("eval errors = %+v, want one", observer.evalErrors)
	}
	if observer.evalErrors[0].txID != 1 || !errors.Is(observer.evalErrors[0].err, ErrSubscriptionEval) {
		t.Fatalf("eval error = %+v, want tx 1 wrapped ErrSubscriptionEval", observer.evalErrors[0])
	}

	mgr.schema = s
	if err := mgr.DisconnectClient(c); err != nil {
		t.Fatalf("DisconnectClient: %v", err)
	}
	cs2 := &store.Changeset{
		TxID: 2,
		Tables: map[schema.TableID]*store.TableChangeset{
			2: {
				TableID:   2,
				TableName: "t2",
				Inserts:   []types.ProductValue{{types.NewUint64(2)}},
			},
		},
	}
	mgr.EvalAndBroadcast(types.TxID(2), cs2, nil, PostCommitMeta{})
	select {
	case msg2 := <-inbox:
		if len(msg2.Fanout[c]) != 0 {
			t.Fatalf("dropped connection should not receive further updates, got %v", msg2.Fanout[c])
		}
	default:
	}
}

type subscriptionEvalErrorRecord struct {
	txID types.TxID
	err  error
}

type recordingSubscriptionObserver struct {
	evalErrors []subscriptionEvalErrorRecord
}

func (o *recordingSubscriptionObserver) LogSubscriptionEvalError(txID types.TxID, err error) {
	o.evalErrors = append(o.evalErrors, subscriptionEvalErrorRecord{txID: txID, err: err})
}
func (o *recordingSubscriptionObserver) LogSubscriptionFanoutError(string, *types.ConnectionID, error) {
}
func (o *recordingSubscriptionObserver) LogSubscriptionClientDropped(string, *types.ConnectionID) {}
func (o *recordingSubscriptionObserver) LogProtocolBackpressure(string, string)                   {}
func (o *recordingSubscriptionObserver) RecordSubscriptionActive(int)                             {}
func (o *recordingSubscriptionObserver) RecordSubscriptionEvalDuration(string, time.Duration)     {}

func TestEvalBatchedTier1SingleLookup(t *testing.T) {
	// Many inserts with the same value should be candidate-resolved via
	// batching — we sanity-check that the result still reports the correct
	// inserts for the match. Finer-grained benchmarks live in Task 5.4.
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}},
	}, nil)
	ins := make([]types.ProductValue, 100)
	for i := range ins {
		ins[i] = types.ProductValue{types.NewUint64(42), types.NewString("x")}
	}
	cs := simpleChangeset(1, ins, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	msg := <-inbox
	u := msg.Fanout[types.ConnectionID{1}][0]
	if len(u.Inserts) != 100 {
		t.Fatalf("Inserts = %d, want 100", len(u.Inserts))
	}
}

func TestEvalNoRowsSkipsCandidates(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{NoRows{Table: 1}},
	}, nil); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if !mgr.indexes.TestOnlyIsEmpty() {
		t.Fatalf("NoRows should not be placed in pruning indexes: %+v", mgr.indexes)
	}

	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(42), types.NewString("x")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	msg := <-inbox
	if got := msg.Fanout[types.ConnectionID{1}]; len(got) != 0 {
		t.Fatalf("NoRows produced fanout: %v", got)
	}
}

func TestEvalSelfEquiJoinSubscription(t *testing.T) {
	// T: (id, u32). Sub: SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32.
	// Lowered to Join{Left: T, Right: T, LeftCol: 1, RightCol: 1, aliases 0/1}.
	const selfTable TableID = 1
	s := newFakeSchema()
	s.addTable(selfTable, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		selfTable: {{types.NewUint64(1), types.NewUint64(5)}},
	})

	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{Left: selfTable, Right: selfTable, LeftCol: 1, RightCol: 1, LeftAlias: 0, RightAlias: 1}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{join},
	}, committed)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}

	// Insert a second row on the same table with matching u32.
	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			selfTable: {
				TableID: selfTable, TableName: "t",
				Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(5)}},
			},
		},
	}
	committed.addRow(selfTable, 2, types.ProductValue{types.NewUint64(2), types.NewUint64(5)})
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("want 1 update, got %v", updates)
	}
	// Self-join IVM bag algebra for inserting r2(u32=5) into t={r1(u32=5)}:
	// dv(+) = V' - V = {(r1,r2), (r2,r1), (r2,r2)} after ReconcileJoinDelta
	// cancels the double-counted (r2,r2) against D3 = dT(+) join dT(+).
	if len(updates[0].Inserts) != 3 {
		t.Fatalf("expected 3 joined insert rows from self-join IVM, got %d", len(updates[0].Inserts))
	}
}

func TestEvalSelfEquiJoinWithAliasedWhere(t *testing.T) {
	// T: (id, u32). Sub: SELECT a.* FROM t AS a JOIN t AS b ON a.u32 = b.u32 WHERE a.id = 1.
	// Filter is tagged with the a-side alias; only joined pairs whose a-side
	// row has id=1 are valid. Inserting r2(id=2, u32=5) into t={r1(id=1, u32=5)}:
	//   bag(before) filtered = {(r1,r1)}
	//   bag(after)  filtered = {(r1,r1), (r1,r2)}  (b's id is unconstrained)
	//   expected delta: inserts = {(r1,r2)}, deletes = {}
	const selfTable TableID = 1
	s := newFakeSchema()
	s.addTable(selfTable, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		selfTable: {{types.NewUint64(1), types.NewUint64(5)}},
	})

	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{
		Left: selfTable, Right: selfTable,
		LeftCol: 1, RightCol: 1,
		LeftAlias: 0, RightAlias: 1,
		Filter: ColEq{Table: selfTable, Column: 0, Alias: 0, Value: types.NewUint64(1)},
	}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{join},
	}, committed)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}

	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			selfTable: {
				TableID: selfTable, TableName: "t",
				Inserts: []types.ProductValue{{types.NewUint64(2), types.NewUint64(5)}},
			},
		},
	}
	committed.addRow(selfTable, 2, types.ProductValue{types.NewUint64(2), types.NewUint64(5)})
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("want 1 update, got %v", updates)
	}
	if len(updates[0].Inserts) != 1 {
		t.Fatalf("expected 1 insert (only a.id=1 pair), got %d", len(updates[0].Inserts))
	}
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("expected 0 deletes, got %d", len(updates[0].Deletes))
	}
	// Joined row format is LHS++RHS; a-side id must be 1 (filter hit).
	if !updates[0].Inserts[0][0].Equal(types.NewUint64(1)) {
		t.Fatalf("inserted joined row a-id = %v, want 1", updates[0].Inserts[0][0])
	}
}

func TestEvalJoinSubscription(t *testing.T) {
	// T1: (id, name). T2: (id, t1_id). Sub: Join on T1.id = T2.t1_id.
	s := newFakeSchema()
	s.addTable(joinLHS, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(joinRHS, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {{types.NewUint64(1), types.NewString("a")}},
		joinRHS: {{types.NewUint64(100), types.NewUint64(1)}},
	})

	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{Left: joinLHS, Right: joinRHS, LeftCol: 0, RightCol: 1}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{join},
	}, committed)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}

	// Insert a second row in T2 pointing at the existing T1 row.
	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			joinRHS: {
				TableID: joinRHS, TableName: "t2",
				Inserts: []types.ProductValue{{types.NewUint64(101), types.NewUint64(1)}},
			},
		},
	}
	// Keep committed view consistent by pre-inserting into mock.
	committed.addRow(joinRHS, 2, types.ProductValue{types.NewUint64(101), types.NewUint64(1)})
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("want 1 update, got %v", updates)
	}
	if len(updates[0].Inserts) != 1 {
		t.Fatalf("expected 1 joined insert row, got %d", len(updates[0].Inserts))
	}
	// self-join projection contract: row is projected onto LHS (default ProjectRight=false).
	// T1 has 2 columns, so the emitted row must be 2-wide, not the 4-wide
	// LHS++RHS concat the IVM fragments carry internally.
	if updates[0].TableID != joinLHS {
		t.Fatalf("emitted TableID = %d, want %d (LHS)", updates[0].TableID, joinLHS)
	}
	if len(updates[0].Inserts[0]) != 2 {
		t.Fatalf("projected row width = %d, want 2 (LHS shape)", len(updates[0].Inserts[0]))
	}
	// LHS join key survives at column 0; name survives at column 1.
	if !updates[0].Inserts[0][0].Equal(types.NewUint64(1)) {
		t.Fatalf("projected row[0] = %v, want LHS id=1", updates[0].Inserts[0][0])
	}
}

// self-join projection contract: delta eval with ProjectRight=true returns RHS-shape rows.
func TestEvalJoinSubscriptionProjectsRight(t *testing.T) {
	s := newFakeSchema()
	s.addTable(joinLHS, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(joinRHS, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: {{types.NewUint64(1), types.NewString("a")}},
		joinRHS: {{types.NewUint64(100), types.NewUint64(1)}},
	})

	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{Left: joinLHS, Right: joinRHS, LeftCol: 0, RightCol: 1, ProjectRight: true}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{join},
	}, committed)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}

	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			joinRHS: {
				TableID: joinRHS, TableName: "t2",
				Inserts: []types.ProductValue{{types.NewUint64(101), types.NewUint64(1)}},
			},
		},
	}
	committed.addRow(joinRHS, 2, types.ProductValue{types.NewUint64(101), types.NewUint64(1)})
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 || len(updates[0].Inserts) != 1 {
		t.Fatalf("want 1 update + 1 insert, got %v", updates)
	}
	if updates[0].TableID != joinRHS {
		t.Fatalf("emitted TableID = %d, want %d (RHS)", updates[0].TableID, joinRHS)
	}
	if len(updates[0].Inserts[0]) != 2 {
		t.Fatalf("projected row width = %d, want 2 (RHS shape)", len(updates[0].Inserts[0]))
	}
	// RHS id survives at column 0 of the projected row.
	if !updates[0].Inserts[0][0].Equal(types.NewUint64(101)) {
		t.Fatalf("projected row[0] = %v, want RHS id=101", updates[0].Inserts[0][0])
	}
}

func TestEvalJoinSubscriptionPreservesProjectedLeftDeltaOrder(t *testing.T) {
	want := []types.ProductValue{
		{types.NewUint64(1), types.NewUint64(7)},
		{types.NewUint64(2), types.NewUint64(7)},
		{types.NewUint64(3), types.NewUint64(7)},
	}

	for attempt := 0; attempt < 64; attempt++ {
		s := newFakeSchema()
		s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
		s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
		committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
			1: want,
		})
		inbox := make(chan FanOutMessage, 1)
		mgr := NewManager(s, s, WithFanOutInbox(inbox))
		join := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1}
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID: types.ConnectionID{1}, QueryID: 20, Predicates: []Predicate{join},
		}, committed); err != nil {
			t.Fatalf("attempt %d: RegisterSet = %v", attempt, err)
		}
		cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
			2: {
				TableID:   2,
				TableName: "rhs",
				Inserts:   []types.ProductValue{{types.NewUint64(10), types.NewUint64(7)}},
			},
		}}
		committed.addRow(2, 1, types.ProductValue{types.NewUint64(10), types.NewUint64(7)})
		mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})
		msg := <-inbox
		updates := msg.Fanout[types.ConnectionID{1}]
		if len(updates) != 1 {
			t.Fatalf("attempt %d: update count = %d, want 1", attempt, len(updates))
		}
		assertRowsEqual(t, updates[0].Inserts, want)
		if len(updates[0].Deletes) != 0 {
			t.Fatalf("attempt %d: deletes = %v, want none", attempt, updates[0].Deletes)
		}
	}
}

func TestEvalJoinSubscriptionPreservesProjectedRightDeltaOrder(t *testing.T) {
	want := []types.ProductValue{
		{types.NewUint64(10), types.NewUint64(7)},
		{types.NewUint64(11), types.NewUint64(7)},
		{types.NewUint64(12), types.NewUint64(7)},
	}

	for attempt := 0; attempt < 64; attempt++ {
		s := newFakeSchema()
		s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
		s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
		committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
			2: want,
		})
		inbox := make(chan FanOutMessage, 1)
		mgr := NewManager(s, s, WithFanOutInbox(inbox))
		join := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1, ProjectRight: true}
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID: types.ConnectionID{1}, QueryID: 21, Predicates: []Predicate{join},
		}, committed); err != nil {
			t.Fatalf("attempt %d: RegisterSet = %v", attempt, err)
		}
		cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
			1: {
				TableID:   1,
				TableName: "lhs",
				Inserts:   []types.ProductValue{{types.NewUint64(1), types.NewUint64(7)}},
			},
		}}
		committed.addRow(1, 1, types.ProductValue{types.NewUint64(1), types.NewUint64(7)})
		mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})
		msg := <-inbox
		updates := msg.Fanout[types.ConnectionID{1}]
		if len(updates) != 1 {
			t.Fatalf("attempt %d: update count = %d, want 1", attempt, len(updates))
		}
		assertRowsEqual(t, updates[0].Inserts, want)
		if len(updates[0].Deletes) != 0 {
			t.Fatalf("attempt %d: deletes = %v, want none", attempt, updates[0].Deletes)
		}
	}
}

func TestEvalJoinSubscriptionLeftInsertWhenOnlyLeftJoinColumnIndexed(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {
			{types.NewUint64(10), types.NewUint64(7)},
			{types.NewUint64(11), types.NewUint64(7)},
		},
	})
	committed := newCountingCommitted(inner)
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 24, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	committed.tableScanCalls = 0

	inserted := types.ProductValue{types.NewUint64(1), types.NewUint64(7)}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Inserts:   []types.ProductValue{inserted},
		},
	}}
	inner.addRow(1, 1, inserted)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	want := []types.ProductValue{inserted, inserted}
	assertRowsEqual(t, updates[0].Inserts, want)
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("deletes = %v, want none", updates[0].Deletes)
	}
	if committed.tableScanCalls != 1 {
		t.Fatalf("TableScan calls = %d, want 1 unindexed committed probe scan", committed.tableScanCalls)
	}
}

func TestEvalJoinSubscriptionRightInsertWhenOnlyRightJoinColumnIndexed(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {
			{types.NewUint64(1), types.NewUint64(7)},
			{types.NewUint64(2), types.NewUint64(7)},
		},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1, ProjectRight: true}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 25, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}

	inserted := types.ProductValue{types.NewUint64(10), types.NewUint64(7)}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		2: {
			TableID:   2,
			TableName: "rhs",
			Inserts:   []types.ProductValue{inserted},
		},
	}}
	committed.addRow(2, 1, inserted)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	want := []types.ProductValue{inserted, inserted}
	assertRowsEqual(t, updates[0].Inserts, want)
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("deletes = %v, want none", updates[0].Deletes)
	}
}

func TestEvalJoinOppositeSideOrFilterCandidateThroughSecondBranch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString})
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindString,
		3: types.KindString,
	}, 1)

	rhs := types.ProductValue{
		types.NewUint64(100),
		types.NewUint64(7),
		types.NewString("blue"),
		types.NewString("large"),
	}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{2: {rhs}})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left:  ColEq{Table: 2, Column: 2, Value: types.NewString("red")},
			Right: ColEq{Table: 2, Column: 3, Value: types.NewString("large")},
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 26, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}

	lhs := types.ProductValue{types.NewUint64(7), types.NewString("lhs")}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Inserts:   []types.ProductValue{lhs},
		},
	}}
	committed.addRow(1, 1, lhs)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 1 || !updates[0].Inserts[0].Equal(lhs) {
		t.Fatalf("inserts = %v, want projected LHS row %v", updates[0].Inserts, lhs)
	}
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("deletes = %v, want none", updates[0].Deletes)
	}
}

func TestEvalJoinOppositeSideMixedOrFilterCandidateThroughRangeBranch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString})
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindString,
		3: types.KindUint64,
	}, 1)

	rhs := types.ProductValue{
		types.NewUint64(100),
		types.NewUint64(7),
		types.NewString("blue"),
		types.NewUint64(60),
	}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{2: {rhs}})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left: ColEq{Table: 2, Column: 2, Value: types.NewString("red")},
			Right: ColRange{Table: 2, Column: 3,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 31, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	rangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 3}
	if got := mgr.indexes.JoinRangeEdge.Lookup(rangeEdge, types.NewUint64(60)); len(got) != 1 {
		t.Fatalf("join range-edge candidates = %v, want one hash", got)
	}
	if got := mgr.indexes.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("table fallback candidates for changed LHS = %v, want none", got)
	}

	lhs := types.ProductValue{types.NewUint64(7), types.NewString("lhs")}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Inserts:   []types.ProductValue{lhs},
		},
	}}
	committed.addRow(1, 1, lhs)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 1 || !updates[0].Inserts[0].Equal(lhs) {
		t.Fatalf("inserts = %v, want projected LHS row %v", updates[0].Inserts, lhs)
	}
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("deletes = %v, want none", updates[0].Deletes)
	}
}

func TestEvalJoinCrossSideOrFilterPrunesExistenceOnlyMismatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
	}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindUint64,
	}, 1)

	rhs := types.ProductValue{types.NewUint64(100), types.NewUint64(7), types.NewUint64(40)}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{2: {rhs}})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left: ColEq{Table: 1, Column: 1, Value: types.NewString("active")},
			Right: ColRange{Table: 2, Column: 2,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 32, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	leftRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	if got := mgr.indexes.JoinEdge.exists[leftRangeEdge]; len(got) != 0 {
		t.Fatalf("broad existence candidates = %v, want none", got)
	}
	if got := mgr.indexes.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("table fallback candidates for changed LHS = %v, want none", got)
	}

	lhs := types.ProductValue{types.NewUint64(7), types.NewString("inactive")}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Inserts:   []types.ProductValue{lhs},
		},
	}}
	committed.addRow(1, 1, lhs)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	if updates := msg.Fanout[types.ConnectionID{1}]; len(updates) != 0 {
		t.Fatalf("cross-side OR mismatch produced fanout: %v", updates)
	}
}

func TestEvalJoinCrossSideOrFilterCandidateThroughOppositeRangeBranch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
	}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindUint64,
	}, 1)

	rhs := types.ProductValue{types.NewUint64(100), types.NewUint64(7), types.NewUint64(60)}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{2: {rhs}})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: Or{
			Left: ColEq{Table: 1, Column: 1, Value: types.NewString("active")},
			Right: ColRange{Table: 2, Column: 2,
				Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
				Upper: Bound{Unbounded: true},
			},
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 33, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	leftRangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	if got := mgr.indexes.JoinRangeEdge.Lookup(leftRangeEdge, types.NewUint64(60)); len(got) != 1 {
		t.Fatalf("join range-edge candidates = %v, want one hash", got)
	}
	if got := mgr.indexes.JoinEdge.exists[leftRangeEdge]; len(got) != 0 {
		t.Fatalf("broad existence candidates = %v, want none", got)
	}

	lhs := types.ProductValue{types.NewUint64(7), types.NewString("inactive")}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Inserts:   []types.ProductValue{lhs},
		},
	}}
	committed.addRow(1, 1, lhs)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 1 || !updates[0].Inserts[0].Equal(lhs) {
		t.Fatalf("inserts = %v, want projected LHS row %v", updates[0].Inserts, lhs)
	}
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("deletes = %v, want none", updates[0].Deletes)
	}
}

func TestEvalJoinOppositeSideRangeFilterCandidate(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindUint64,
	}, 1)

	rhs := types.ProductValue{types.NewUint64(100), types.NewUint64(7), types.NewUint64(15)}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{2: {rhs}})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColRange{Table: 2, Column: 2,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 28, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	if got := mgr.indexes.JoinRangeEdge.Lookup(edge, types.NewUint64(15)); len(got) != 1 {
		t.Fatalf("join range-edge candidates = %v, want one hash", got)
	}
	if got := mgr.indexes.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("table fallback candidates for changed LHS = %v, want none", got)
	}

	lhs := types.ProductValue{types.NewUint64(7), types.NewString("lhs")}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Inserts:   []types.ProductValue{lhs},
		},
	}}
	committed.addRow(1, 1, lhs)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 1 || !updates[0].Inserts[0].Equal(lhs) {
		t.Fatalf("inserts = %v, want projected LHS row %v", updates[0].Inserts, lhs)
	}
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("deletes = %v, want none", updates[0].Deletes)
	}
}

func TestEvalJoinOppositeSideRangeFilterPrunesMismatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindUint64,
	}, 1)

	rhs := types.ProductValue{types.NewUint64(100), types.NewUint64(7), types.NewUint64(25)}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{2: {rhs}})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColRange{Table: 2, Column: 2,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 29, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if got := mgr.indexes.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("table fallback candidates for changed LHS = %v, want none", got)
	}

	lhs := types.ProductValue{types.NewUint64(7), types.NewString("lhs")}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Inserts:   []types.ProductValue{lhs},
		},
	}}
	committed.addRow(1, 1, lhs)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	if updates := msg.Fanout[types.ConnectionID{1}]; len(updates) != 0 {
		t.Fatalf("out-of-range joined row produced fanout: %v", updates)
	}
}

func TestEvalJoinRangeFilterOnChangedSideUsesRangeIndex(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindUint64,
	}, 1)

	lhs := types.ProductValue{types.NewUint64(7), types.NewString("lhs")}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{1: {lhs}})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColRange{Table: 2, Column: 2,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(20), Inclusive: true},
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 30, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if got := mgr.indexes.Range.Lookup(2, 2, types.NewUint64(15)); len(got) != 1 {
		t.Fatalf("range candidates for changed RHS = %v, want one hash", got)
	}
	if got := mgr.indexes.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("table fallback candidates for changed RHS = %v, want none", got)
	}

	rhs := types.ProductValue{types.NewUint64(100), types.NewUint64(7), types.NewUint64(15)}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		2: {
			TableID:   2,
			TableName: "rhs",
			Inserts:   []types.ProductValue{rhs},
		},
	}}
	committed.addRow(2, 1, rhs)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 1 || !updates[0].Inserts[0].Equal(lhs) {
		t.Fatalf("inserts = %v, want projected LHS row %v", updates[0].Inserts, lhs)
	}
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("deletes = %v, want none", updates[0].Deletes)
	}
}

func TestEvalFilteredJoinFallsBackWhenOppositeJoinColumnUnindexed(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindString,
	})

	rhs := types.ProductValue{
		types.NewUint64(100),
		types.NewUint64(7),
		types.NewString("red"),
	}
	inner := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {rhs},
	})
	committed := newCountingCommitted(inner)
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 1,
		Filter: ColEq{Table: 2, Column: 2, Value: types.NewString("red")},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 27, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if got := mgr.indexes.Table.Lookup(1); len(got) != 1 {
		t.Fatalf("table fallback candidates for changed LHS = %v, want one hash", got)
	}
	committed.tableScanCalls = 0

	lhs := types.ProductValue{types.NewUint64(7), types.NewString("lhs")}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Inserts:   []types.ProductValue{lhs},
		},
	}}
	inner.addRow(1, 1, lhs)
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 1 || !updates[0].Inserts[0].Equal(lhs) {
		t.Fatalf("inserts = %v, want projected LHS row %v", updates[0].Inserts, lhs)
	}
	if len(updates[0].Deletes) != 0 {
		t.Fatalf("deletes = %v, want none", updates[0].Deletes)
	}
	if committed.tableScanCalls != 1 {
		t.Fatalf("TableScan calls = %d, want 1 filtered unindexed committed probe scan", committed.tableScanCalls)
	}
}

func TestEvalUnfilteredJoinBothSidesDeletedStillEmitsDelete(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	lhs := types.ProductValue{types.NewUint64(7), types.NewString("lhs")}
	rhs := types.ProductValue{types.NewUint64(100), types.NewUint64(7)}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {lhs},
		2: {rhs},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{Left: 1, Right: 2, LeftCol: 0, RightCol: 1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 28, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if got := mgr.indexes.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("table fallback candidates for left table = %v, want empty", got)
	}
	if got := mgr.indexes.Table.Lookup(2); len(got) != 0 {
		t.Fatalf("table fallback candidates for right table = %v, want empty", got)
	}

	delete(committed.rows[1], types.RowID(1))
	delete(committed.rows[2], types.RowID(1))
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Deletes:   []types.ProductValue{lhs},
		},
		2: {
			TableID:   2,
			TableName: "rhs",
			Deletes:   []types.ProductValue{rhs},
		},
	}}
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 0 {
		t.Fatalf("inserts = %v, want none", updates[0].Inserts)
	}
	if len(updates[0].Deletes) != 1 || !updates[0].Deletes[0].Equal(lhs) {
		t.Fatalf("deletes = %v, want projected LHS row %v", updates[0].Deletes, lhs)
	}
}

func TestEvalJoinSubscriptionProjectedLeftCancelsJoinedPairChurn(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
	lhsRow := types.ProductValue{types.NewUint64(1), types.NewUint64(7)}
	rhsBefore := types.ProductValue{types.NewUint64(10), types.NewUint64(7)}
	rhsAfter := types.ProductValue{types.NewUint64(11), types.NewUint64(7)}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {lhsRow},
		2: {rhsBefore},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 22, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		2: {
			TableID:   2,
			TableName: "rhs",
			Inserts:   []types.ProductValue{rhsAfter},
			Deletes:   []types.ProductValue{rhsBefore},
		},
	}}
	committed.rows[2] = map[types.RowID]types.ProductValue{2: rhsAfter}
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 0 && (len(updates[0].Inserts) != 0 || len(updates[0].Deletes) != 0) {
		t.Fatalf("projected-left pair churn should net to no delta, got %v", updates)
	}
}

func TestEvalJoinSubscriptionProjectedRightCancelsJoinedPairChurn(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)
	lhsBefore := types.ProductValue{types.NewUint64(1), types.NewUint64(7)}
	lhsAfter := types.ProductValue{types.NewUint64(2), types.NewUint64(7)}
	rhsRow := types.ProductValue{types.NewUint64(10), types.NewUint64(7)}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {lhsBefore},
		2: {rhsRow},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{Left: 1, Right: 2, LeftCol: 1, RightCol: 1, ProjectRight: true}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 23, Predicates: []Predicate{join},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "lhs",
			Inserts:   []types.ProductValue{lhsAfter},
			Deletes:   []types.ProductValue{lhsBefore},
		},
	}}
	committed.rows[1] = map[types.RowID]types.ProductValue{2: lhsAfter}
	mgr.EvalAndBroadcast(types.TxID(1), cs, committed, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 0 && (len(updates[0].Inserts) != 0 || len(updates[0].Deletes) != 0) {
		t.Fatalf("projected-right pair churn should net to no delta, got %v", updates)
	}
}

func TestEvalCrossJoinProjectedOtherInsertPreservesMultiplicity(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1)}, {types.NewUint64(2)}},
		2: {{types.NewUint64(10)}, {types.NewUint64(11)}},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := CrossJoin{Left: 1, Right: 2}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 20, Predicates: []Predicate{pred},
	}, committed)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{2: {TableID: 2, TableName: "other", Inserts: []types.ProductValue{{types.NewUint64(12)}}}}}
	committed.addRow(2, 3, types.ProductValue{types.NewUint64(12)})
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 2 {
		t.Fatalf("insert count = %d, want 2 projected rows for the new partner", len(updates[0].Inserts))
	}
}

func TestEvalCrossJoinProjectedProjectedInsertPreservesMultiplicity(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1)}},
		2: {{types.NewUint64(10)}, {types.NewUint64(11)}, {types.NewUint64(12)}},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := CrossJoin{Left: 1, Right: 2}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 21, Predicates: []Predicate{pred},
	}, committed)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{1: {TableID: 1, TableName: "projected", Inserts: []types.ProductValue{{types.NewUint64(2)}}}}}
	committed.addRow(1, 2, types.ProductValue{types.NewUint64(2)})
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 3 {
		t.Fatalf("insert count = %d, want 3 projected-row copies for three partners", len(updates[0].Inserts))
	}
	for i, row := range updates[0].Inserts {
		if !row[0].Equal(types.NewUint64(2)) {
			t.Fatalf("insert %d = %v, want inserted projected row repeated", i, row)
		}
	}
}

func TestEvalCrossJoinProjectedOtherDeletePreservesMultiplicity(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1)}, {types.NewUint64(2)}},
		2: {{types.NewUint64(10)}, {types.NewUint64(11)}},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := CrossJoin{Left: 1, Right: 2}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 22, Predicates: []Predicate{pred},
	}, committed)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	delete(committed.rows[2], 2)
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{2: {TableID: 2, TableName: "other", Deletes: []types.ProductValue{{types.NewUint64(11)}}}}}
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Deletes) != 2 {
		t.Fatalf("delete count = %d, want 2 projected-row deletes for the removed partner", len(updates[0].Deletes))
	}
}

func TestEvalCrossJoinProjectsRightOnProjectedInsert(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: {{types.NewUint64(1)}, {types.NewUint64(2)}},
		2: {{types.NewUint64(10)}},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := CrossJoin{Left: 1, Right: 2, ProjectRight: true}
	_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 23, Predicates: []Predicate{pred},
	}, committed)
	if err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{2: {TableID: 2, TableName: "projected", Inserts: []types.ProductValue{{types.NewUint64(11)}}}}}
	committed.addRow(2, 2, types.ProductValue{types.NewUint64(11)})
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 || len(updates[0].Inserts) != 2 {
		t.Fatalf("updates = %v, want 2 RHS-projected inserts", updates)
	}
	for i, row := range updates[0].Inserts {
		if !row[0].Equal(types.NewUint64(11)) {
			t.Fatalf("insert %d = %v, want inserted RHS row repeated", i, row)
		}
	}
}

func TestEvalFilteredCrossJoinLocalFilterUsesValueIndex(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(10)}, {types.NewUint64(11)}},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := CrossJoin{
		Left:  1,
		Right: 2,
		Filter: ColEq{
			Table:  1,
			Column: 1,
			Value:  types.NewString("active"),
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 24, Predicates: []Predicate{pred},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if got := mgr.indexes.Value.Lookup(1, 1, types.NewString("active")); len(got) != 1 {
		t.Fatalf("filtered cross join value candidates = %v, want one hash", got)
	}
	if got := mgr.indexes.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("table fallback candidates for local filter side = %v, want none", got)
	}

	inserted := types.ProductValue{types.NewUint64(1), types.NewString("active")}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "left",
			Inserts:   []types.ProductValue{inserted},
		},
	}}
	committed.addRow(1, 1, inserted)
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 {
		t.Fatalf("update count = %d, want 1", len(updates))
	}
	if len(updates[0].Inserts) != 2 {
		t.Fatalf("insert count = %d, want local row repeated for two partners", len(updates[0].Inserts))
	}
	for i, row := range updates[0].Inserts {
		if !row.Equal(inserted) {
			t.Fatalf("insert %d = %v, want %v", i, row, inserted)
		}
	}
}

func TestEvalFilteredCrossJoinLocalFilterPrunesMismatch(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString})
	s.addTable(2, map[ColID]types.ValueKind{0: types.KindUint64})
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(10)}},
	})
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := CrossJoin{
		Left:  1,
		Right: 2,
		Filter: ColEq{
			Table:  1,
			Column: 1,
			Value:  types.NewString("active"),
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 25, Predicates: []Predicate{pred},
	}, committed); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if got := mgr.indexes.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("table fallback candidates for local filter side = %v, want none", got)
	}

	inserted := types.ProductValue{types.NewUint64(1), types.NewString("inactive")}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID:   1,
			TableName: "left",
			Inserts:   []types.ProductValue{inserted},
		},
	}}
	committed.addRow(1, 1, inserted)
	mgr.EvalAndBroadcast(types.TxID(2), cs, committed, PostCommitMeta{})

	msg := <-inbox
	if updates := msg.Fanout[types.ConnectionID{1}]; len(updates) != 0 {
		t.Fatalf("filtered cross join mismatch produced fanout: %v", updates)
	}
}

func TestEvalFilteredCrossJoinDeltaMatchesFullBagDiff(t *testing.T) {
	pred := CrossJoin{
		Left:  1,
		Right: 2,
		Filter: ColEq{
			Table:  2,
			Column: 0,
			Value:  types.NewUint64(10),
		},
	}
	beforeLeft := []types.ProductValue{{types.NewUint64(1)}, {types.NewUint64(2)}}
	beforeRight := []types.ProductValue{{types.NewUint64(10)}, {types.NewUint64(20)}}
	afterLeft := []types.ProductValue{{types.NewUint64(1)}, {types.NewUint64(3)}}
	afterRight := []types.ProductValue{{types.NewUint64(10)}, {types.NewUint64(10)}}

	committed := newMockCommitted()
	for i, row := range afterLeft {
		committed.addRow(1, types.RowID(i+1), row)
	}
	for i, row := range afterRight {
		committed.addRow(2, types.RowID(i+1), row)
	}
	cs := &store.Changeset{TxID: 1, Tables: map[schema.TableID]*store.TableChangeset{
		1: {
			TableID: 1,
			Inserts: []types.ProductValue{
				{types.NewUint64(3)},
			},
			Deletes: []types.ProductValue{
				{types.NewUint64(2)},
			},
		},
		2: {
			TableID: 2,
			Inserts: []types.ProductValue{
				{types.NewUint64(10)},
			},
			Deletes: []types.ProductValue{
				{types.NewUint64(20)},
			},
		},
	}}
	dv := NewDeltaView(committed, cs, nil)
	defer dv.Release()

	gotIns, gotDel := evalCrossJoinDelta(dv, pred)
	wantIns, wantDel := diffProjectedRowBags(
		crossJoinProjectedRows(pred, beforeLeft, beforeRight),
		crossJoinProjectedRows(pred, afterLeft, afterRight),
	)
	if !sameRowBag(gotIns, wantIns) || !sameRowBag(gotDel, wantDel) {
		t.Fatalf("filtered cross join delta got inserts=%v deletes=%v, want inserts=%v deletes=%v", gotIns, gotDel, wantIns, wantDel)
	}
}

func sameRowBag(a, b []types.ProductValue) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, row := range a {
		counts[encodeRowKey(row)]++
	}
	for _, row := range b {
		key := encodeRowKey(row)
		if counts[key] == 0 {
			return false
		}
		counts[key]--
		if counts[key] == 0 {
			delete(counts, key)
		}
	}
	return len(counts) == 0
}

func TestEvalPruningFallbackVsBaseline(t *testing.T) {
	// Pruning safety: ensure an affected subscription is picked up via the
	// expected tier.
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	// ColRange subs land in the range pruning tier.
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{ColRange{Table: 1, Column: 0,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(100), Inclusive: true}}},
	}, nil)
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(50), types.NewString("in")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	msg := <-inbox
	u := msg.Fanout[types.ConnectionID{1}]
	if len(u) != 1 || len(u[0].Inserts) != 1 {
		t.Fatalf("range predicate missed: %v", u)
	}
}

func TestEvalRangePruningSkipsOutOfRangeChange(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{ColRange{Table: 1, Column: 0,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(100), Inclusive: true}}},
	}, nil)
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(5), types.NewString("out")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	msg := <-inbox
	if got := msg.Fanout[types.ConnectionID{1}]; len(got) != 0 {
		t.Fatalf("out-of-range change produced fanout: %v", got)
	}
}

func TestEvalOrWithMixedEqRangeBranchesUsesIndexes(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 0)
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := Or{
		Left: ColEq{Table: 1, Column: 0, Value: types.NewUint64(1)},
		Right: ColRange{Table: 1, Column: 1,
			Lower: Bound{Value: types.NewUint64(50), Inclusive: false},
			Upper: Bound{Unbounded: true},
		},
	}
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{pred},
	}, nil); err != nil {
		t.Fatalf("RegisterSet = %v", err)
	}
	if got := mgr.indexes.Value.Lookup(1, 0, types.NewUint64(1)); len(got) != 1 {
		t.Fatalf("mixed OR equality candidates = %v, want one hash", got)
	}
	if got := mgr.indexes.Range.Lookup(1, 1, types.NewUint64(60)); len(got) != 1 {
		t.Fatalf("mixed OR range candidates = %v, want one hash", got)
	}
	if got := mgr.indexes.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("table fallback candidates for mixed OR = %v, want none", got)
	}

	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(2), types.NewUint64(60)}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	msg := <-inbox
	updates := msg.Fanout[types.ConnectionID{1}]
	if len(updates) != 1 || len(updates[0].Inserts) != 1 {
		t.Fatalf("mixed OR predicate missed range-only insert: %v", updates)
	}
	if !updates[0].Inserts[0][0].Equal(types.NewUint64(2)) || !updates[0].Inserts[0][1].Equal(types.NewUint64(60)) {
		t.Fatalf("insert row = %v, want id=2 score=60", updates[0].Inserts[0])
	}
}

func TestEvalChangesetNotMutated(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10,
		Predicates: []Predicate{AllRows{Table: 1}},
	}, nil)

	original := []types.ProductValue{
		{types.NewUint64(1), types.NewString("a")},
		{types.NewUint64(2), types.NewString("b")},
	}
	cs := simpleChangeset(1, original, nil)
	lenBefore := len(cs.Tables[1].Inserts)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	<-inbox
	if len(cs.Tables[1].Inserts) != lenBefore {
		t.Fatalf("changeset mutated: before=%d after=%d", lenBefore, len(cs.Tables[1].Inserts))
	}
}

func TestEvalErrorDropSignalsConnectionUntilDisconnectRuns(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	c := types.ConnectionID{1}
	qid := uint32(10)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID:     c,
		QueryID:    qid,
		RequestID:  77,
		Predicates: []Predicate{AllRows{Table: 1}},
	}, nil)

	mgr.schema = nil
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(1), types.NewString("x")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	<-inbox // drain the FanOutMessage

	if _, still := mgr.querySets[c][qid]; !still {
		t.Fatalf("querySets[%v][%d] should remain until DisconnectClient runs", c, qid)
	}
	dropped := mgr.DrainDroppedClients()
	if len(dropped) != 1 || dropped[0] != c {
		t.Fatalf("dropped connections = %v, want [%v]", dropped, c)
	}
	mgr.schema = s
	if err := mgr.DisconnectClient(c); err != nil {
		t.Fatalf("DisconnectClient: %v", err)
	}
	if _, still := mgr.querySets[c][qid]; still {
		t.Fatalf("querySets[%v][%d] should be deleted after DisconnectClient", c, qid)
	}
	if _, err := mgr.UnregisterSet(c, qid, nil); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("second UnregisterSet err = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestEvalMultipleTableUpdatesGrouped(t *testing.T) {
	// Two subscriptions on same connection touching different tables.
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	c := types.ConnectionID{1}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 10, Predicates: []Predicate{AllRows{Table: 1}}}, nil)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 11, Predicates: []Predicate{AllRows{Table: 2}}}, nil)

	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			1: {TableID: 1, Inserts: []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}},
			2: {TableID: 2, Inserts: []types.ProductValue{{types.NewUint64(2), types.NewInt32(3)}}},
		},
	}
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	msg := <-inbox
	updates := msg.Fanout[c]
	if len(updates) != 2 {
		t.Fatalf("want 2 updates for conn, got %v", updates)
	}
}
