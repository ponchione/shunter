package subscription

import (
	"fmt"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func benchSchema() *fakeSchema { return testSchema() }

func BenchmarkEvalEqualitySubs1K(b *testing.B) {
	s := benchSchema()
	inbox := make(chan FanOutMessage, 1024)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	for i := 0; i < 1000; i++ {
		_, err := mgr.Register(SubscriptionRegisterRequest{
			ConnID:         types.ConnectionID{byte(i % 256)},
			SubscriptionID: types.SubscriptionID(i),
			Predicate:      ColEq{Table: 1, Column: 0, Value: types.NewUint64(uint64(i))},
		}, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(500), types.NewString("x")}}, nil)
	// drain inbox in goroutine
	go func() {
		for range inbox {
		}
	}()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	}
}

func BenchmarkEvalEqualitySubs10K(b *testing.B) {
	s := benchSchema()
	inbox := make(chan FanOutMessage, 1024)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	for i := 0; i < 10000; i++ {
		_, err := mgr.Register(SubscriptionRegisterRequest{
			ConnID:         types.ConnectionID{byte(i % 256)},
			SubscriptionID: types.SubscriptionID(i),
			Predicate:      ColEq{Table: 1, Column: 0, Value: types.NewUint64(uint64(i))},
		}, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(5000), types.NewString("x")}}, nil)
	go func() {
		for range inbox {
		}
	}()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	}
}

func BenchmarkRegisterUnregister(b *testing.B) {
	s := benchSchema()
	mgr := NewManager(s, s)
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := mgr.Register(SubscriptionRegisterRequest{
			ConnID: types.ConnectionID{1}, SubscriptionID: types.SubscriptionID(i), Predicate: pred,
		}, nil)
		if err != nil {
			b.Fatal(err)
		}
		if err := mgr.Unregister(types.ConnectionID{1}, types.SubscriptionID(i)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFanOut1KClientsSameQuery(b *testing.B) {
	s := benchSchema()
	inbox := make(chan FanOutMessage, 1024)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := ColEq{Table: 1, Column: 0, Value: types.NewUint64(42)}
	for i := 0; i < 1000; i++ {
		c := types.ConnectionID{}
		c[0] = byte(i)
		c[1] = byte(i >> 8)
		_, _ = mgr.Register(SubscriptionRegisterRequest{
			ConnID: c, SubscriptionID: types.SubscriptionID(i), Predicate: pred,
		}, nil)
	}
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(42), types.NewString("x")}}, nil)
	go func() {
		for range inbox {
		}
	}()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	}
}

// BenchmarkJoinFragmentEval measures end-to-end EvalAndBroadcast cost for one
// affected join subscription (Story 5.4 §9.1: target < 10 ms per affected
// subscription). It is not a microbenchmark of EvalJoinDeltaFragments alone:
// the timing includes DeltaView construction, candidate collection, join-fragment
// evaluation/reconciliation, and fanout assembly for the fixed one-query setup.
// b.N loops EvalAndBroadcast over read-only manager/committed fixtures.
func BenchmarkJoinFragmentEval(b *testing.B) {
	s := newFakeSchema()
	s.addTable(joinLHS, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindString}, 0)
	s.addTable(joinRHS, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 1)

	// Committed fixture: 100 LHS rows with distinct ids; 100 RHS rows whose
	// fk references the LHS ids 1:1. Matches §9.3 scaling claim (per-edge
	// cost scales with delta × avg-fanout, not total committed rows).
	const committedRows = 100
	committedLHS := make([]types.ProductValue, committedRows)
	committedRHS := make([]types.ProductValue, committedRows)
	for i := 0; i < committedRows; i++ {
		committedLHS[i] = types.ProductValue{types.NewUint64(uint64(i + 1)), types.NewString("n")}
		committedRHS[i] = types.ProductValue{types.NewUint64(uint64(i + 1000)), types.NewUint64(uint64(i + 1))}
	}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		joinLHS: committedLHS,
		joinRHS: committedRHS,
	})

	inbox := make(chan FanOutMessage, 1024)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	join := Join{Left: joinLHS, Right: joinRHS, LeftCol: 0, RightCol: 1}
	if _, err := mgr.Register(SubscriptionRegisterRequest{
		ConnID: types.ConnectionID{1}, SubscriptionID: 10, Predicate: join,
	}, committed); err != nil {
		b.Fatalf("Register = %v", err)
	}

	// Changeset: 10 inserts on each side that fan out into joined rows. The
	// LHS inserts each match one committed RHS row (id + 1000 trick above
	// was 1..100 → fk 1..100; new LHS ids 2000.. don't match those, so only
	// RHS inserts produce joined fragments here). Keep both sides so we
	// exercise I1/I2 paths.
	lhsInserts := make([]types.ProductValue, 10)
	rhsInserts := make([]types.ProductValue, 10)
	for i := 0; i < 10; i++ {
		lhsInserts[i] = types.ProductValue{types.NewUint64(uint64(i + 1)), types.NewString("x")} // matches committed RHS
		rhsInserts[i] = types.ProductValue{types.NewUint64(uint64(i + 2000)), types.NewUint64(uint64(i + 1))}
	}
	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			joinLHS: {TableID: joinLHS, TableName: "t1", Inserts: lhsInserts},
			joinRHS: {TableID: joinRHS, TableName: "t2", Inserts: rhsInserts},
		},
	}

	go func() {
		for range inbox {
		}
	}()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(uint64(i+2)), cs, committed, PostCommitMeta{})
	}
}

func BenchmarkDeltaIndexConstruction(b *testing.B) {
	// 100 rows × 5 indexed columns.
	rows := make([]types.ProductValue, 100)
	for i := range rows {
		rows[i] = types.ProductValue{
			types.NewUint64(uint64(i)),
			types.NewUint64(uint64(i * 2)),
			types.NewUint64(uint64(i * 3)),
			types.NewUint64(uint64(i * 4)),
			types.NewUint64(uint64(i * 5)),
		}
	}
	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			1: {TableID: 1, TableName: "t", Inserts: rows},
		},
	}
	active := map[TableID][]ColID{1: {0, 1, 2, 3, 4}}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewDeltaView(nil, cs, active)
	}
}

func BenchmarkCandidateCollection(b *testing.B) {
	// 1K ColEq subs, 10 changed rows with mixed values.
	s := benchSchema()
	mgr := NewManager(s, s)
	for i := 0; i < 1000; i++ {
		_, _ = mgr.Register(SubscriptionRegisterRequest{
			ConnID: types.ConnectionID{1}, SubscriptionID: types.SubscriptionID(i),
			Predicate: ColEq{Table: 1, Column: 0, Value: types.NewUint64(uint64(i))},
		}, nil)
	}
	// Build a 10-row changeset with repeat values.
	rows := make([]types.ProductValue, 10)
	for i := range rows {
		rows[i] = types.ProductValue{types.NewUint64(uint64(i % 3)), types.NewString("x")}
	}
	cs := simpleChangeset(1, rows, nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = mgr.collectCandidates(cs, nil)
	}
	_ = fmt.Sprint
}
