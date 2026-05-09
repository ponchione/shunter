package subscription

import (
	"context"
	"fmt"
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func benchSchema() *fakeSchema { return testSchema() }

func drainBenchmarkInbox(b *testing.B, inbox chan FanOutMessage) {
	b.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range inbox {
		}
	}()
	b.Cleanup(func() {
		close(inbox)
		<-done
	})
}

func BenchmarkEvalEqualitySubs1K(b *testing.B) {
	s := benchSchema()
	inbox := make(chan FanOutMessage, 1024)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	for i := 0; i < 1000; i++ {
		_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID:     types.ConnectionID{byte(i % 256)},
			QueryID:    uint32(i),
			Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(uint64(i))}},
		}, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(500), types.NewString("x")}}, nil)
	drainBenchmarkInbox(b, inbox)
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
		_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID:     types.ConnectionID{byte(i % 256)},
			QueryID:    uint32(i),
			Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(uint64(i))}},
		}, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
	cs := simpleChangeset(1,
		[]types.ProductValue{{types.NewUint64(5000), types.NewString("x")}}, nil)
	drainBenchmarkInbox(b, inbox)
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
		_, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID: types.ConnectionID{1}, QueryID: uint32(i), Predicates: []Predicate{pred},
		}, nil)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := mgr.UnregisterSet(types.ConnectionID{1}, uint32(i), nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRegisterSetInitialQueryAllRows(b *testing.B) {
	s := benchSchema()
	rows := make([]types.ProductValue, 1024)
	for i := range rows {
		rows[i] = types.ProductValue{types.NewUint64(uint64(i)), types.NewString("row")}
	}
	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{1: rows})
	pred := AllRows{Table: 1}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr := NewManager(s, s)
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID:     types.ConnectionID{1},
			QueryID:    uint32(i),
			Predicates: []Predicate{pred},
		}, committed); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProjectedRowsBeforeLargeBags(b *testing.B) {
	const totalRows = 4096
	const distinctRows = 64

	s := benchSchema()
	current := make([]types.ProductValue, 0, totalRows)
	inserted := make([]types.ProductValue, 0, totalRows/2)
	for i := 0; i < totalRows; i++ {
		row := types.ProductValue{types.NewUint64(uint64(i % distinctRows)), types.NewString("row")}
		current = append(current, row)
		if i%2 == 0 {
			inserted = append(inserted, row)
		}
	}
	view := buildMockCommitted(s, map[TableID][]types.ProductValue{1: current})
	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			1: {TableID: 1, Inserts: inserted},
		},
	}
	dv := NewDeltaView(view, cs, nil)
	defer dv.Release()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = projectedRowsBefore(context.Background(), dv, 1)
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
		_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID: c, QueryID: uint32(i), Predicates: []Predicate{pred},
		}, nil)
	}
	cs := simpleChangeset(1, []types.ProductValue{{types.NewUint64(42), types.NewString("x")}}, nil)
	drainBenchmarkInbox(b, inbox)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(1), cs, nil, PostCommitMeta{})
	}
}

func BenchmarkFanOut1KClientsVariedQueries(b *testing.B) {
	const (
		clientCount = 1000
		changedRows = 256
	)

	s := benchSchema()
	inbox := make(chan FanOutMessage, 1024)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	for i := 0; i < clientCount; i++ {
		c := types.ConnectionID{}
		c[0] = byte(i)
		c[1] = byte(i >> 8)
		if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID:     c,
			QueryID:    uint32(i),
			Predicates: []Predicate{benchmarkVariedFanoutPredicate(i, changedRows)},
		}, nil); err != nil {
			b.Fatal(err)
		}
	}

	rows := make([]types.ProductValue, changedRows)
	for i := range rows {
		v := uint64(i)
		rows[i] = types.ProductValue{types.NewUint64(v), benchmarkVariedFanoutBucket(v)}
	}
	cs := simpleChangeset(1, rows, nil)
	drainBenchmarkInbox(b, inbox)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(uint64(i+1)), cs, nil, PostCommitMeta{})
	}
}

func benchmarkVariedFanoutPredicate(i, changedRows int) Predicate {
	value := uint64(i % changedRows)
	switch i % 4 {
	case 0:
		return ColEq{Table: 1, Column: 0, Value: types.NewUint64(value)}
	case 1:
		return ColRange{
			Table:  1,
			Column: 0,
			Lower:  Bound{Value: types.NewUint64(value), Inclusive: true},
			Upper:  Bound{Value: types.NewUint64(value), Inclusive: true},
		}
	case 2:
		return And{
			Left: ColRange{
				Table:  1,
				Column: 0,
				Lower:  Bound{Value: types.NewUint64(value), Inclusive: true},
				Upper:  Bound{Value: types.NewUint64(value + 3), Inclusive: true},
			},
			Right: ColEq{Table: 1, Column: 1, Value: benchmarkVariedFanoutBucket(value)},
		}
	default:
		return Or{
			Left:  ColEq{Table: 1, Column: 0, Value: types.NewUint64(value)},
			Right: ColEq{Table: 1, Column: 0, Value: types.NewUint64((value + uint64(changedRows/2)) % uint64(changedRows))},
		}
	}
}

func benchmarkVariedFanoutBucket(value uint64) types.Value {
	return types.NewString(fmt.Sprintf("bucket-%02d", value%4))
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
	if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
		ConnID: types.ConnectionID{1}, QueryID: 10, Predicates: []Predicate{join},
	}, committed); err != nil {
		b.Fatalf("RegisterSet = %v", err)
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

	drainBenchmarkInbox(b, inbox)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(uint64(i+2)), cs, committed, PostCommitMeta{})
	}
}

func BenchmarkMultiWayLiveJoinEvalSizes(b *testing.B) {
	for _, size := range []int{32, 128, 512} {
		b.Run(fmt.Sprintf("rows_%d/table_shape", size), func(b *testing.B) {
			benchmarkMultiWayLiveJoinEval(b, size, nil)
		})
		b.Run(fmt.Sprintf("rows_%d/count", size), func(b *testing.B) {
			benchmarkMultiWayLiveJoinEval(b, size, countStarAggregate())
		})
	}
}

func benchmarkMultiWayLiveJoinEval(b *testing.B, size int, aggregate *Aggregate) {
	s := multiJoinTestSchema()
	inbox := make(chan FanOutMessage, 1024)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	pred := multiJoinTestPredicate()
	before := benchmarkMultiJoinCommitted(size, false)
	connID := types.ConnectionID{9}
	req := SubscriptionSetRegisterRequest{
		ConnID:     connID,
		QueryID:    90,
		Predicates: []Predicate{pred},
	}
	if aggregate != nil {
		req.Aggregates = []*Aggregate{aggregate}
	}
	if _, err := mgr.RegisterSet(req, before); err != nil {
		b.Fatalf("RegisterSet: %v", err)
	}
	drainBenchmarkInbox(b, inbox)

	changed := types.ProductValue{types.NewUint64(uint64(size + 1000)), types.NewUint64(uint64(size/2 + 1))}
	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			3: {TableID: 3, TableName: "t3", Inserts: []types.ProductValue{changed}},
		},
	}
	after := benchmarkMultiJoinCommitted(size, true)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(uint64(i+2)), cs, after, PostCommitMeta{})
	}
}

func benchmarkMultiJoinCommitted(size int, includeChanged bool) *mockCommitted {
	s := multiJoinTestSchema()
	tRows := make([]types.ProductValue, size)
	sRows := make([]types.ProductValue, size)
	rRows := make([]types.ProductValue, size, size+1)
	for i := 0; i < size; i++ {
		key := types.NewUint64(uint64(i + 1))
		tRows[i] = types.ProductValue{types.NewUint64(uint64(i + 1)), key}
		sRows[i] = types.ProductValue{types.NewUint64(uint64(i + 1001)), key}
		rRows[i] = types.ProductValue{types.NewUint64(uint64(i + 2001)), key}
	}
	if includeChanged {
		rRows = append(rRows, types.ProductValue{
			types.NewUint64(uint64(size + 1000)),
			types.NewUint64(uint64(size/2 + 1)),
		})
	}
	return buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: tRows,
		2: sRows,
		3: rRows,
	})
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
		dv := NewDeltaView(nil, cs, active)
		dv.Release()
	}
}

func BenchmarkCandidateCollection(b *testing.B) {
	// 1K ColEq subs, 10 changed rows with mixed values.
	s := benchSchema()
	mgr := NewManager(s, s)
	for i := 0; i < 1000; i++ {
		_, _ = mgr.RegisterSet(SubscriptionSetRegisterRequest{
			ConnID: types.ConnectionID{1}, QueryID: uint32(i),
			Predicates: []Predicate{ColEq{Table: 1, Column: 0, Value: types.NewUint64(uint64(i))}},
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
	st := acquireCandidateScratch()
	defer releaseCandidateScratch(st)
	for i := 0; i < b.N; i++ {
		_ = mgr.collectCandidatesInto(cs, nil, st)
	}
	_ = fmt.Sprint
}
