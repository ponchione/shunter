package subscription

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

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

func TestEvalErrorQueuesSubscriptionErrorWithoutDroppingConnection(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	c := types.ConnectionID{1}
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 10, RequestID: 77, Predicates: []Predicate{AllRows{Table: 1}}}, nil)
	_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{ConnID: c, QueryID: 11, Predicates: []Predicate{AllRows{Table: 2}}}, nil)
	wantHealthy := mgr.querySets[c][11][0]

	var logs bytes.Buffer
	oldOut := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
	}()

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
	select {
	case dropped := <-mgr.DroppedClients():
		t.Fatalf("unexpected dropped connection signal: %v", dropped)
	default:
	}
	logText := logs.String()
	if !strings.Contains(logText, ComputeQueryHash(AllRows{Table: 1}, nil).String()) {
		t.Fatalf("log output missing query hash: %q", logText)
	}
	if !strings.Contains(logText, "AllRows") {
		t.Fatalf("log output missing predicate repr: %q", logText)
	}

	mgr.schema = s
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
	msg2 := <-inbox
	updates := msg2.Fanout[c]
	if len(updates) != 1 || updates[0].SubscriptionID != wantHealthy {
		t.Fatalf("healthy subscription should remain active, got %v (want subID=%d)", updates, wantHealthy)
	}
}

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
	// TD-142 Slice 14: row is projected onto LHS (default ProjectRight=false).
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

// TD-142 Slice 14: delta eval with ProjectRight=true returns RHS-shape rows.
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

func TestEvalPruningFallbackVsBaseline(t *testing.T) {
	// Pruning safety: ensure an affected subscription is picked up via the
	// expected tier.
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	// ColRange subs land in Tier 3 (range predicates have no equality).
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
		t.Fatalf("Tier 3 range predicate missed: %v", u)
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

func TestEvalErrorDropCullsQuerySets(t *testing.T) {
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

	// Trigger eval error by nil-ing the schema (same technique as
	// TestEvalErrorQueuesSubscriptionErrorWithoutDroppingConnection).
	mgr.schema = nil
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(1), types.NewString("x")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	<-inbox // drain the FanOutMessage

	if _, still := mgr.querySets[c][qid]; still {
		t.Fatalf("querySets[%v][%d] should be deleted after eval-error drop", c, qid)
	}
	mgr.schema = s // restore so UnregisterSet can run
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
