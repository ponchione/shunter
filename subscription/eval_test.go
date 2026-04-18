package subscription

import (
	"bytes"
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

func TestEvalSingleTableColEqMatches(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	_, _ = mgr.Register(SubscriptionRegisterRequest{
		ConnID: types.ConnectionID{1}, SubscriptionID: 10,
		Predicate: ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)},
	}, nil)

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
		if u.SubscriptionID != types.SubscriptionID(10) {
			t.Fatalf("SubscriptionID = %d", u.SubscriptionID)
		}
	default:
		t.Fatal("no fanout message received")
	}
}

func TestEvalSkipsUnaffectedTables(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	_, _ = mgr.Register(SubscriptionRegisterRequest{
		ConnID: types.ConnectionID{1}, SubscriptionID: 10,
		Predicate: AllRows{Table: 2},
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
	_, _ = mgr.Register(SubscriptionRegisterRequest{ConnID: types.ConnectionID{1}, SubscriptionID: 10, Predicate: pred}, nil)
	_, _ = mgr.Register(SubscriptionRegisterRequest{ConnID: types.ConnectionID{2}, SubscriptionID: 11, Predicate: pred}, nil)

	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(42), types.NewString("x")}}, nil)
	mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})

	msg := <-inbox
	if len(msg.Fanout) != 2 {
		t.Fatalf("fanout should reach both conns, got %v", msg.Fanout)
	}
	got1 := msg.Fanout[types.ConnectionID{1}][0].SubscriptionID
	got2 := msg.Fanout[types.ConnectionID{2}][0].SubscriptionID
	if got1 != types.SubscriptionID(10) || got2 != types.SubscriptionID(11) {
		t.Fatalf("subIDs wrong: %d %d", got1, got2)
	}
}

func TestEvalSameConnectionSameQueryProducesIndependentUpdates(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	c := types.ConnectionID{1}
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	_, _ = mgr.Register(SubscriptionRegisterRequest{ConnID: c, SubscriptionID: 10, Predicate: pred}, nil)
	_, _ = mgr.Register(SubscriptionRegisterRequest{ConnID: c, SubscriptionID: 11, Predicate: pred}, nil)

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
	if !seen[10] || !seen[11] {
		t.Fatalf("expected updates for subIDs 10 and 11, got %v", updates)
	}
}

func TestEvalErrorQueuesSubscriptionErrorWithoutDroppingConnection(t *testing.T) {
	s := testSchema()
	inbox := make(chan FanOutMessage, 2)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	c := types.ConnectionID{1}
	_, _ = mgr.Register(SubscriptionRegisterRequest{ConnID: c, SubscriptionID: 10, RequestID: 77, Predicate: AllRows{Table: 1}}, nil)
	_, _ = mgr.Register(SubscriptionRegisterRequest{ConnID: c, SubscriptionID: 11, Predicate: AllRows{Table: 2}}, nil)

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
	if len(updates) != 1 || updates[0].SubscriptionID != 11 {
		t.Fatalf("healthy subscription should remain active, got %v", updates)
	}
}

func TestEvalBatchedTier1SingleLookup(t *testing.T) {
	// Many inserts with the same value should be candidate-resolved via
	// batching — we sanity-check that the result still reports the correct
	// inserts for the match. Finer-grained benchmarks live in Task 5.4.
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	_, _ = mgr.Register(SubscriptionRegisterRequest{
		ConnID: types.ConnectionID{1}, SubscriptionID: 10,
		Predicate: ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)},
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
	_, err := mgr.Register(SubscriptionRegisterRequest{
		ConnID: types.ConnectionID{1}, SubscriptionID: 10, Predicate: join,
	}, committed)
	if err != nil {
		t.Fatalf("Register = %v", err)
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
}

func TestEvalPruningFallbackVsBaseline(t *testing.T) {
	// Pruning safety: ensure an affected subscription is picked up via the
	// expected tier.
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	// ColRange subs land in Tier 3 (range predicates have no equality).
	_, _ = mgr.Register(SubscriptionRegisterRequest{
		ConnID: types.ConnectionID{1}, SubscriptionID: 10,
		Predicate: ColRange{Table: 1, Column: 0,
			Lower: Bound{Value: types.NewUint64(10), Inclusive: true},
			Upper: Bound{Value: types.NewUint64(100), Inclusive: true}},
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
	_, _ = mgr.Register(SubscriptionRegisterRequest{
		ConnID: types.ConnectionID{1}, SubscriptionID: 10,
		Predicate: AllRows{Table: 1},
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

func TestEvalMultipleTableUpdatesGrouped(t *testing.T) {
	// Two subscriptions on same connection touching different tables.
	s := testSchema()
	inbox := make(chan FanOutMessage, 1)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	c := types.ConnectionID{1}
	_, _ = mgr.Register(SubscriptionRegisterRequest{ConnID: c, SubscriptionID: 10, Predicate: AllRows{Table: 1}}, nil)
	_, _ = mgr.Register(SubscriptionRegisterRequest{ConnID: c, SubscriptionID: 11, Predicate: AllRows{Table: 2}}, nil)

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
