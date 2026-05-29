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

func BenchmarkRegisterSetOrderedInitialRows(b *testing.B) {
	cases := []struct {
		totalRows  int
		limitRows  uint64
		offsetRows uint64
		inputOrder string
		keyColumns int
	}{
		{totalRows: 128, limitRows: 10, inputOrder: "ascending", keyColumns: 1},
		{totalRows: 1024, limitRows: 100, inputOrder: "descending", keyColumns: 1},
		{totalRows: 1024, limitRows: 100, offsetRows: 25, inputOrder: "shuffled", keyColumns: 2},
		{totalRows: 4096, limitRows: 100, inputOrder: "descending", keyColumns: 2},
		{totalRows: 4096, limitRows: 1000, inputOrder: "shuffled", keyColumns: 1},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("rows_%d/limit_%d/offset_%d/%s/%dcol", tc.totalRows, tc.limitRows, tc.offsetRows, tc.inputOrder, tc.keyColumns)
		b.Run(name, func(b *testing.B) {
			s := benchmarkOrderedInitialRowSchema()
			rows := benchmarkOrderedInitialRows(tc.totalRows, tc.inputOrder, tc.keyColumns)
			committed := buildMockCommitted(s, map[TableID][]types.ProductValue{1: rows})
			orderBy := benchmarkInitialRowOrderBy(tc.keyColumns)
			pred := AllRows{Table: 1}
			limitRows := tc.limitRows

			b.ResetTimer()
			b.ReportAllocs()
			returnedRows := 0
			for i := 0; i < b.N; i++ {
				mgr := NewManager(s, s)
				req := SubscriptionSetRegisterRequest{
					ConnID:         types.ConnectionID{1},
					QueryID:        uint32(i),
					Predicates:     []Predicate{pred},
					OrderByColumns: [][]OrderByColumn{orderBy},
					Limits:         []*uint64{&limitRows},
				}
				if tc.offsetRows > 0 {
					offsetRows := tc.offsetRows
					req.Offsets = []*uint64{&offsetRows}
				}
				res, err := mgr.RegisterSet(req, committed)
				if err != nil {
					b.Fatal(err)
				}
				if len(res.Update) != 0 {
					returnedRows += len(res.Update[0].Inserts)
				}
			}
			if returnedRows == 0 {
				b.Fatal("ordered RegisterSet returned no rows")
			}
		})
	}
}

func BenchmarkInitialRowsForTableOrderedWindow(b *testing.B) {
	cases := []struct {
		path       string
		totalRows  int
		limitRows  uint64
		offsetRows uint64
		inputOrder string
		keyColumns int
	}{
		{path: "table_scan", totalRows: 128, limitRows: 10, inputOrder: "ascending", keyColumns: 1},
		{path: "table_scan", totalRows: 1024, limitRows: 100, inputOrder: "shuffled", keyColumns: 1},
		{path: "table_scan", totalRows: 4096, limitRows: 1000, offsetRows: 100, inputOrder: "shuffled", keyColumns: 2},
		{path: "index_range", totalRows: 1024, limitRows: 100, inputOrder: "descending", keyColumns: 1},
		{path: "index_range", totalRows: 4096, limitRows: 100, inputOrder: "shuffled", keyColumns: 2},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("%s/rows_%d/limit_%d/offset_%d/%s/%dcol", tc.path, tc.totalRows, tc.limitRows, tc.offsetRows, tc.inputOrder, tc.keyColumns)
		b.Run(name, func(b *testing.B) {
			s := benchmarkOrderedInitialRowSchema()
			rows := benchmarkOrderedInitialRows(tc.totalRows, tc.inputOrder, tc.keyColumns)
			view := buildMockCommitted(s, map[TableID][]types.ProductValue{1: rows})
			pred := benchmarkInitialRowsForTablePredicate(tc.path, tc.totalRows)
			orderBy := benchmarkInitialRowOrderBy(tc.keyColumns)
			limitRows := tc.limitRows
			var offset *uint64
			if tc.offsetRows > 0 {
				offsetRows := tc.offsetRows
				offset = &offsetRows
			}
			mgr := NewManager(s, benchmarkInitialRowsForTableResolver(s, tc.path))
			window := initialRowWindow{orderBy: orderBy, limit: &limitRows, offset: offset}

			b.ResetTimer()
			b.ReportAllocs()
			returnedRows := 0
			for i := 0; i < b.N; i++ {
				collector := newInitialRowCollector(context.Background(), 0)
				got, err := mgr.initialRowsForTable(collector, pred, view, 1, window)
				if err != nil {
					b.Fatal(err)
				}
				returnedRows += len(got)
			}
			if returnedRows == 0 {
				b.Fatal("initialRowsForTable returned no rows")
			}
		})
	}
}

func BenchmarkBoundedOrderedInitialRowsAdd(b *testing.B) {
	cases := []struct {
		totalRows  int
		keepRows   int
		inputOrder string
		keyColumns int
	}{
		{totalRows: 128, keepRows: 10, inputOrder: "ascending", keyColumns: 1},
		{totalRows: 128, keepRows: 100, inputOrder: "descending", keyColumns: 2},
		{totalRows: 1024, keepRows: 10, inputOrder: "descending", keyColumns: 1},
		{totalRows: 1024, keepRows: 100, inputOrder: "shuffled", keyColumns: 1},
		{totalRows: 1024, keepRows: 100, inputOrder: "shuffled", keyColumns: 2},
		{totalRows: 4096, keepRows: 10, inputOrder: "ascending", keyColumns: 1},
		{totalRows: 4096, keepRows: 100, inputOrder: "descending", keyColumns: 2},
		{totalRows: 4096, keepRows: 1000, inputOrder: "shuffled", keyColumns: 1},
		{totalRows: 4096, keepRows: 1000, inputOrder: "shuffled", keyColumns: 2},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("rows_%d/keep_%d/%s/%dcol", tc.totalRows, tc.keepRows, tc.inputOrder, tc.keyColumns)
		b.Run(name, func(b *testing.B) {
			rows := benchmarkOrderedInitialRows(tc.totalRows, tc.inputOrder, tc.keyColumns)
			orderBy := benchmarkInitialRowOrderBy(tc.keyColumns)

			b.ResetTimer()
			b.ReportAllocs()
			kept := 0
			for i := 0; i < b.N; i++ {
				bounded := newBoundedOrderedInitialRows(orderBy, tc.keepRows)
				for _, row := range rows {
					if err := bounded.add(row); err != nil {
						b.Fatal(err)
					}
				}
				kept += len(bounded.rows)
			}
			if kept == 0 {
				b.Fatal("bounded ordered rows kept no rows")
			}
		})
	}
}

func BenchmarkOrderWindowRows(b *testing.B) {
	for _, totalRows := range []int{128, 1024, 4096} {
		for _, inputOrder := range []string{"ascending", "descending", "shuffled"} {
			for _, keyColumns := range []int{1, 2} {
				name := fmt.Sprintf("rows_%d/%s/%dcol", totalRows, inputOrder, keyColumns)
				b.Run(name, func(b *testing.B) {
					rows := benchmarkOrderedInitialRows(totalRows, inputOrder, keyColumns)
					orderBy := benchmarkInitialRowOrderBy(keyColumns)

					b.ResetTimer()
					b.ReportAllocs()
					orderedRows := 0
					for i := 0; i < b.N; i++ {
						ordered, err := orderWindowRows(rows, orderBy, true)
						if err != nil {
							b.Fatal(err)
						}
						orderedRows += len(ordered)
					}
					if orderedRows == 0 {
						b.Fatal("ordered no rows")
					}
				})
			}
		}
	}
}

func BenchmarkOrderedInitialRowsComparatorShapes(b *testing.B) {
	cases := []struct {
		operation      string
		totalRows      int
		keepRows       int
		inputOrder     string
		keyColumns     int
		orderDirection string
		keyShape       string
	}{
		{operation: "bounded", totalRows: 1024, keepRows: 100, inputOrder: "shuffled", keyColumns: 1, orderDirection: "desc", keyShape: "unique"},
		{operation: "bounded", totalRows: 4096, keepRows: 1000, inputOrder: "shuffled", keyColumns: 1, orderDirection: "desc", keyShape: "ties"},
		{operation: "bounded", totalRows: 4096, keepRows: 1000, inputOrder: "shuffled", keyColumns: 2, orderDirection: "mixed", keyShape: "ties"},
		{operation: "full", totalRows: 1024, inputOrder: "shuffled", keyColumns: 1, orderDirection: "desc", keyShape: "ties"},
		{operation: "full", totalRows: 4096, inputOrder: "descending", keyColumns: 2, orderDirection: "mixed", keyShape: "ties"},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("%s/rows_%d/%s/%dcol/%s/%s", tc.operation, tc.totalRows, tc.inputOrder, tc.keyColumns, tc.orderDirection, tc.keyShape)
		b.Run(name, func(b *testing.B) {
			rows := benchmarkOrderedInitialRowsForShape(tc.totalRows, tc.inputOrder, tc.keyColumns, tc.keyShape)
			orderBy := benchmarkInitialRowOrderByDirection(tc.keyColumns, tc.orderDirection)

			b.ResetTimer()
			b.ReportAllocs()
			measuredRows := 0
			for i := 0; i < b.N; i++ {
				switch tc.operation {
				case "bounded":
					bounded := newBoundedOrderedInitialRows(orderBy, tc.keepRows)
					for _, row := range rows {
						if err := bounded.add(row); err != nil {
							b.Fatal(err)
						}
					}
					measuredRows += len(bounded.rows)
				case "full":
					ordered, err := orderWindowRows(rows, orderBy, true)
					if err != nil {
						b.Fatal(err)
					}
					measuredRows += len(ordered)
				default:
					b.Fatalf("unsupported ordered benchmark operation %q", tc.operation)
				}
			}
			if measuredRows == 0 {
				b.Fatal("ordered comparator benchmark measured no rows")
			}
		})
	}
}

func benchmarkOrderedInitialRowSchema() *fakeSchema {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}, 0)
	return s
}

func benchmarkInitialRowOrderBy(keyColumns int) []OrderByColumn {
	orderBy := make([]OrderByColumn, keyColumns)
	for i := range orderBy {
		orderBy[i] = OrderByColumn{
			Schema: schema.ColumnSchema{Index: i, Name: fmt.Sprintf("k%d", i), Type: types.KindUint64},
			Table:  1,
			Column: ColID(i),
		}
	}
	return orderBy
}

func benchmarkInitialRowsForTablePredicate(path string, totalRows int) Predicate {
	switch path {
	case "table_scan":
		return AllRows{Table: 1}
	case "index_range":
		return ColRange{
			Table:  1,
			Column: 0,
			Lower:  Bound{Value: types.NewUint64(0), Inclusive: true},
			Upper:  Bound{Value: types.NewUint64(uint64(totalRows)), Inclusive: true},
		}
	default:
		panic(fmt.Sprintf("unsupported initial rows path %q", path))
	}
}

func benchmarkInitialRowsForTableResolver(s *fakeSchema, path string) IndexResolver {
	switch path {
	case "table_scan":
		return nil
	case "index_range":
		return s
	default:
		panic(fmt.Sprintf("unsupported initial rows path %q", path))
	}
}

func benchmarkInitialRowOrderByDirection(keyColumns int, direction string) []OrderByColumn {
	orderBy := benchmarkInitialRowOrderBy(keyColumns)
	switch direction {
	case "asc":
		return orderBy
	case "desc":
		for i := range orderBy {
			orderBy[i].Desc = true
		}
	case "mixed":
		for i := range orderBy {
			orderBy[i].Desc = i%2 == 1
		}
	default:
		panic(fmt.Sprintf("unsupported order direction %q", direction))
	}
	return orderBy
}

func benchmarkOrderedInitialRowsForShape(totalRows int, inputOrder string, keyColumns int, keyShape string) []types.ProductValue {
	switch keyShape {
	case "unique":
		return benchmarkOrderedInitialRows(totalRows, inputOrder, keyColumns)
	case "ties":
		return benchmarkTieHeavyOrderedInitialRows(totalRows, inputOrder, keyColumns)
	default:
		panic(fmt.Sprintf("unsupported ordered key shape %q", keyShape))
	}
}

func benchmarkOrderedInitialRows(totalRows int, inputOrder string, keyColumns int) []types.ProductValue {
	rows := make([]types.ProductValue, totalRows)
	for i := range rows {
		rank := benchmarkOrderInputRank(i, totalRows, inputOrder)
		switch keyColumns {
		case 1:
			rows[i] = types.ProductValue{
				types.NewUint64(uint64(rank)),
				types.NewUint64(uint64((rank*31 + 7) % totalRows)),
			}
		case 2:
			rows[i] = types.ProductValue{
				types.NewUint64(uint64(rank / 16)),
				types.NewUint64(uint64(rank % 16)),
			}
		default:
			panic(fmt.Sprintf("unsupported key column count %d", keyColumns))
		}
	}
	return rows
}

func benchmarkTieHeavyOrderedInitialRows(totalRows int, inputOrder string, keyColumns int) []types.ProductValue {
	rows := make([]types.ProductValue, totalRows)
	for i := range rows {
		rank := benchmarkOrderInputRank(i, totalRows, inputOrder)
		switch keyColumns {
		case 1:
			rows[i] = types.ProductValue{
				types.NewUint64(uint64(rank % 8)),
				types.NewUint64(uint64(rank)),
				types.NewUint64(uint64((rank*31 + 7) % totalRows)),
			}
		case 2:
			rows[i] = types.ProductValue{
				types.NewUint64(uint64(rank % 16)),
				types.NewUint64(uint64((rank / 16) % 4)),
				types.NewUint64(uint64(rank)),
			}
		default:
			panic(fmt.Sprintf("unsupported key column count %d", keyColumns))
		}
	}
	return rows
}

func benchmarkOrderInputRank(i, totalRows int, inputOrder string) int {
	switch inputOrder {
	case "ascending":
		return i
	case "descending":
		return totalRows - 1 - i
	case "shuffled":
		return (i*65 + 17) % totalRows
	default:
		panic(fmt.Sprintf("unsupported input order %q", inputOrder))
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

func BenchmarkFanOut1KClientsSkewedHotKey(b *testing.B) {
	const (
		clientCount = 1000
		hotClients  = 800
		changedRows = 64
		hotValue    = 7
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
			Predicates: []Predicate{benchmarkSkewedFanoutPredicate(i, hotClients, changedRows, hotValue)},
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

func BenchmarkFanOut1KClientsMultiTableVariedQueries(b *testing.B) {
	const (
		clientCount         = 1000
		changedRowsPerTable = 256
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
			Predicates: []Predicate{benchmarkMultiTableVariedFanoutPredicate(i, changedRowsPerTable)},
		}, nil); err != nil {
			b.Fatal(err)
		}
	}

	tableOneRows := make([]types.ProductValue, changedRowsPerTable)
	tableTwoRows := make([]types.ProductValue, changedRowsPerTable)
	for i := range tableOneRows {
		v := uint64(i)
		tableOneRows[i] = types.ProductValue{types.NewUint64(v), benchmarkVariedFanoutBucket(v)}
		tableTwoRows[i] = types.ProductValue{types.NewUint64(v), benchmarkMultiTableFanoutBucket(2, v)}
	}
	cs := &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			1: {TableID: 1, TableName: "t1", Inserts: tableOneRows},
			2: {TableID: 2, TableName: "t2", Inserts: tableTwoRows},
		},
	}
	drainBenchmarkInbox(b, inbox)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(uint64(i+1)), cs, nil, PostCommitMeta{})
	}
}

func BenchmarkEvalOrderedLimitedWindowDelta(b *testing.B) {
	cases := []struct {
		totalRows  int
		limitRows  uint64
		inputOrder string
		keyColumns int
		changeKind string
	}{
		{totalRows: 128, limitRows: 10, inputOrder: "ascending", keyColumns: 1, changeKind: "insert_head"},
		{totalRows: 1024, limitRows: 100, inputOrder: "descending", keyColumns: 1, changeKind: "delete_head"},
		{totalRows: 1024, limitRows: 100, inputOrder: "shuffled", keyColumns: 2, changeKind: "insert_outside"},
		{totalRows: 4096, limitRows: 100, inputOrder: "descending", keyColumns: 2, changeKind: "insert_head"},
		{totalRows: 4096, limitRows: 1000, inputOrder: "shuffled", keyColumns: 1, changeKind: "delete_head"},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("rows_%d/limit_%d/%s/%dcol/%s", tc.totalRows, tc.limitRows, tc.inputOrder, tc.keyColumns, tc.changeKind)
		b.Run(name, func(b *testing.B) {
			s := benchmarkOrderedInitialRowSchema()
			beforeRows := benchmarkLiveOrderedWindowRows(tc.totalRows, tc.inputOrder, tc.keyColumns)
			afterRows, inserted, deleted := benchmarkLiveOrderedWindowChange(beforeRows, tc.totalRows, tc.keyColumns, tc.changeKind)
			before := buildMockCommitted(s, map[TableID][]types.ProductValue{1: beforeRows})
			after := buildMockCommitted(s, map[TableID][]types.ProductValue{1: afterRows})
			cs := simpleChangeset(1, inserted, deleted)
			orderBy := benchmarkInitialRowOrderBy(tc.keyColumns)
			limitRows := tc.limitRows
			inbox := make(chan FanOutMessage, 1024)
			mgr := NewManager(s, s, WithFanOutInbox(inbox))
			if _, err := mgr.RegisterSet(SubscriptionSetRegisterRequest{
				ConnID:         types.ConnectionID{1},
				QueryID:        10,
				Predicates:     []Predicate{AllRows{Table: 1}},
				OrderByColumns: [][]OrderByColumn{orderBy},
				Limits:         []*uint64{&limitRows},
			}, before); err != nil {
				b.Fatal(err)
			}
			drainBenchmarkInbox(b, inbox)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				mgr.EvalAndBroadcast(types.TxID(uint64(i+1)), cs, after, PostCommitMeta{})
			}
		})
	}
}

func benchmarkSkewedFanoutPredicate(i, hotClients, changedRows, hotValue int) Predicate {
	if i < hotClients {
		return ColEq{Table: 1, Column: 0, Value: types.NewUint64(uint64(hotValue))}
	}
	value := uint64((i - hotClients) % changedRows)
	if value == uint64(hotValue) {
		value = (value + 1) % uint64(changedRows)
	}
	switch (i - hotClients) % 4 {
	case 0:
		return ColEq{Table: 1, Column: 0, Value: types.NewUint64(value)}
	case 1:
		return ColRange{
			Table:  1,
			Column: 0,
			Lower:  Bound{Value: types.NewUint64(value), Inclusive: true},
			Upper:  Bound{Value: types.NewUint64(value + 1), Inclusive: true},
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

func benchmarkMultiTableVariedFanoutPredicate(i, changedRowsPerTable int) Predicate {
	table := TableID(1)
	if i%2 == 1 {
		table = 2
	}
	value := uint64((i / 2) % changedRowsPerTable)
	switch (i / 2) % 4 {
	case 0:
		return ColEq{Table: table, Column: 0, Value: types.NewUint64(value)}
	case 1:
		return ColRange{
			Table:  table,
			Column: 0,
			Lower:  Bound{Value: types.NewUint64(value), Inclusive: true},
			Upper:  Bound{Value: types.NewUint64(value), Inclusive: true},
		}
	case 2:
		return And{
			Left: ColRange{
				Table:  table,
				Column: 0,
				Lower:  Bound{Value: types.NewUint64(value), Inclusive: true},
				Upper:  Bound{Value: types.NewUint64(value + 3), Inclusive: true},
			},
			Right: ColEq{Table: table, Column: 1, Value: benchmarkMultiTableFanoutBucket(table, value)},
		}
	default:
		other := (value + uint64(changedRowsPerTable/2)) % uint64(changedRowsPerTable)
		return Or{
			Left:  ColEq{Table: table, Column: 0, Value: types.NewUint64(value)},
			Right: ColEq{Table: table, Column: 0, Value: types.NewUint64(other)},
		}
	}
}

func benchmarkVariedFanoutBucket(value uint64) types.Value {
	return types.NewString(fmt.Sprintf("bucket-%02d", value%4))
}

func benchmarkMultiTableFanoutBucket(table TableID, value uint64) types.Value {
	if table == 2 {
		return types.NewInt32(int32(value % 4))
	}
	return benchmarkVariedFanoutBucket(value)
}

func benchmarkLiveOrderedWindowRows(totalRows int, inputOrder string, keyColumns int) []types.ProductValue {
	rows := make([]types.ProductValue, totalRows)
	for i := range rows {
		rank := benchmarkOrderInputRank(i, totalRows, inputOrder)
		rows[i] = benchmarkLiveOrderedWindowRow(rank+1, totalRows, keyColumns)
	}
	return rows
}

func benchmarkLiveOrderedWindowChange(beforeRows []types.ProductValue, totalRows int, keyColumns int, changeKind string) (afterRows, inserted, deleted []types.ProductValue) {
	afterRows = append([]types.ProductValue(nil), beforeRows...)
	switch changeKind {
	case "insert_head":
		inserted = []types.ProductValue{benchmarkLiveOrderedWindowRow(0, totalRows, keyColumns)}
		afterRows = append(afterRows, inserted...)
	case "insert_outside":
		inserted = []types.ProductValue{benchmarkLiveOrderedWindowRow(totalRows+1, totalRows, keyColumns)}
		afterRows = append(afterRows, inserted...)
	case "delete_head":
		deleted = []types.ProductValue{benchmarkLiveOrderedWindowRow(1, totalRows, keyColumns)}
		afterRows = benchmarkRowsWithoutOne(afterRows, deleted[0])
	default:
		panic(fmt.Sprintf("unsupported live ordered window change %q", changeKind))
	}
	return afterRows, inserted, deleted
}

func benchmarkLiveOrderedWindowRow(rank, totalRows int, keyColumns int) types.ProductValue {
	switch keyColumns {
	case 1:
		return types.ProductValue{
			types.NewUint64(uint64(rank)),
			types.NewUint64(uint64((rank*31+7)%(totalRows+1) + 1)),
		}
	case 2:
		return types.ProductValue{
			types.NewUint64(uint64(rank/16 + 1)),
			types.NewUint64(uint64(rank%16 + 1)),
		}
	default:
		panic(fmt.Sprintf("unsupported key column count %d", keyColumns))
	}
}

func benchmarkRowsWithoutOne(rows []types.ProductValue, remove types.ProductValue) []types.ProductValue {
	for i, row := range rows {
		if row.Equal(remove) {
			return append(rows[:i], rows[i+1:]...)
		}
	}
	panic(fmt.Sprintf("benchmark row not found: %v", remove))
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

func BenchmarkMultiWayLiveJoinRelationShapes(b *testing.B) {
	const size = 128
	b.Run("chain3", func(b *testing.B) {
		changed := types.ProductValue{types.NewUint64(uint64(size + 1000)), types.NewUint64(uint64(size/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, multiJoinTestSchema(), multiJoinTestPredicate(), benchmarkMultiJoinCommitted(size, false), benchmarkMultiJoinCommitted(size, true), 3, changed)
	})
	b.Run("self_alias3", func(b *testing.B) {
		changed := types.ProductValue{types.NewUint64(uint64(size + 2000)), types.NewUint64(uint64(size/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, multiJoinTestSchema(), repeatedMultiJoinConditionPredicate(), benchmarkMultiJoinCommitted(size, false), benchmarkMultiJoinSelfAliasCommitted(size, true), 2, changed)
	})
	b.Run("chain4", func(b *testing.B) {
		changed := types.ProductValue{types.NewUint64(uint64(size + 3000)), types.NewUint64(uint64(size/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, multiJoinFourRelationTestSchema(), multiJoinFourRelationPredicate(), benchmarkMultiJoinFourRelationCommitted(size, false), benchmarkMultiJoinFourRelationCommitted(size, true), 4, changed)
	})
	b.Run("chain5_rows_128", func(b *testing.B) {
		changed := types.ProductValue{types.NewUint64(uint64(size + 4000)), types.NewUint64(uint64(size/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, benchmarkMultiJoinFiveRelationTestSchema(), benchmarkMultiJoinFiveRelationPredicate(), benchmarkMultiJoinFiveRelationCommitted(size, false), benchmarkMultiJoinFiveRelationCommitted(size, true), 5, changed)
	})
	b.Run("cross3_rows_24", func(b *testing.B) {
		const crossSize = 24
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed)
	})
	b.Run("cross3_rows_32", func(b *testing.B) {
		const crossSize = 32
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed)
	})
	b.Run("cross3_rows_40", func(b *testing.B) {
		const crossSize = 40
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed)
	})
	b.Run("cross3_rows_48", func(b *testing.B) {
		const crossSize = 48
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed)
	})
	b.Run("cross3_rows_56", func(b *testing.B) {
		const crossSize = 56
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed)
	})
	b.Run("cross3_rows_64", func(b *testing.B) {
		const crossSize = 64
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShape(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed)
	})
}

func benchmarkMultiWayLiveJoinShape(b *testing.B, s *fakeSchema, pred MultiJoin, before, after *mockCommitted, changedTable TableID, changed types.ProductValue) {
	b.Helper()
	cs := benchmarkMultiJoinInsertChangeset(changedTable, []types.ProductValue{changed})
	benchmarkMultiWayLiveJoinDelta(b, s, pred, before, after, cs, nil)
}

func BenchmarkMultiWayLiveJoinSelectivity(b *testing.B) {
	const size = 128
	cases := []struct {
		name      string
		hotFanout int
	}{
		{name: "one_match", hotFanout: 1},
		{name: "hot_key_8x8", hotFanout: 8},
		{name: "hot_key_16x16", hotFanout: 16},
		{name: "hot_key_24x24", hotFanout: 24},
		{name: "hot_key_32x32", hotFanout: 32},
		{name: "hot_key_40x40", hotFanout: 40},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("rows_%d/%s", size, tc.name)
		b.Run(name, func(b *testing.B) {
			changed := types.ProductValue{types.NewUint64(9000), types.NewUint64(1)}
			before := benchmarkMultiJoinSelectivityCommitted(size, tc.hotFanout, nil)
			after := benchmarkMultiJoinSelectivityCommitted(size, tc.hotFanout, []types.ProductValue{changed})
			cs := benchmarkMultiJoinInsertChangeset(3, []types.ProductValue{changed})
			benchmarkMultiWayLiveJoinDelta(b, multiJoinTestSchema(), multiJoinTestPredicate(), before, after, cs, nil)
		})
	}
}

func BenchmarkMultiWayLiveJoinChangedRows(b *testing.B) {
	const size = 128
	for _, changedRowCount := range []int{1, 10, 100} {
		name := fmt.Sprintf("rows_%d/changed_%d", size, changedRowCount)
		b.Run(name, func(b *testing.B) {
			changedRows := benchmarkMultiJoinChangedRows(size, changedRowCount, 10_000)
			before := benchmarkMultiJoinCommittedWithChangedRows(size, nil)
			after := benchmarkMultiJoinCommittedWithChangedRows(size, changedRows)
			cs := benchmarkMultiJoinInsertChangeset(3, changedRows)
			benchmarkMultiWayLiveJoinDelta(b, multiJoinTestSchema(), multiJoinTestPredicate(), before, after, cs, nil)
		})
	}
}

func BenchmarkMultiWayLiveJoinAggregateRelationShapes(b *testing.B) {
	const size = 128
	b.Run("chain3/count", func(b *testing.B) {
		changed := types.ProductValue{types.NewUint64(uint64(size + 1000)), types.NewUint64(uint64(size/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), multiJoinTestPredicate(), benchmarkMultiJoinCommitted(size, false), benchmarkMultiJoinCommitted(size, true), 3, changed, countStarAggregate())
	})
	b.Run("self_alias3/count", func(b *testing.B) {
		changed := types.ProductValue{types.NewUint64(uint64(size + 2000)), types.NewUint64(uint64(size/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), repeatedMultiJoinConditionPredicate(), benchmarkMultiJoinCommitted(size, false), benchmarkMultiJoinSelfAliasCommitted(size, true), 2, changed, countStarAggregate())
	})
	b.Run("chain4/count", func(b *testing.B) {
		changed := types.ProductValue{types.NewUint64(uint64(size + 3000)), types.NewUint64(uint64(size/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinFourRelationTestSchema(), multiJoinFourRelationPredicate(), benchmarkMultiJoinFourRelationCommitted(size, false), benchmarkMultiJoinFourRelationCommitted(size, true), 4, changed, countStarAggregate())
	})
	b.Run("chain5_rows_128/count", func(b *testing.B) {
		changed := types.ProductValue{types.NewUint64(uint64(size + 4000)), types.NewUint64(uint64(size/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, benchmarkMultiJoinFiveRelationTestSchema(), benchmarkMultiJoinFiveRelationPredicate(), benchmarkMultiJoinFiveRelationCommitted(size, false), benchmarkMultiJoinFiveRelationCommitted(size, true), 5, changed, countStarAggregate())
	})
	b.Run("cross3_rows_24/count", func(b *testing.B) {
		const crossSize = 24
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, countStarAggregate())
	})
	b.Run("cross3_rows_32/count", func(b *testing.B) {
		const crossSize = 32
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, countStarAggregate())
	})
	b.Run("cross3_rows_40/count", func(b *testing.B) {
		const crossSize = 40
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, countStarAggregate())
	})
	b.Run("cross3_rows_48/count", func(b *testing.B) {
		const crossSize = 48
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, countStarAggregate())
	})
	b.Run("cross3_rows_56/count", func(b *testing.B) {
		const crossSize = 56
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, countStarAggregate())
	})
	b.Run("cross3_rows_64/count", func(b *testing.B) {
		const crossSize = 64
		changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
		benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, countStarAggregate())
	})
}

func BenchmarkMultiWayLiveJoinAggregateFunctions(b *testing.B) {
	const size = 128
	cases := []struct {
		name      string
		aggregate *Aggregate
	}{
		{name: "count_star", aggregate: countStarAggregate()},
		{name: "count_column", aggregate: countMultiJoinRIDAggregate()},
		{name: "count_distinct", aggregate: countDistinctMultiJoinTIDAggregate()},
		{name: "sum", aggregate: sumMultiJoinRIDAggregate()},
	}

	for _, tc := range cases {
		b.Run("chain3/"+tc.name, func(b *testing.B) {
			changed := types.ProductValue{types.NewUint64(uint64(size + 1000)), types.NewUint64(uint64(size/2 + 1))}
			benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), multiJoinTestPredicate(), benchmarkMultiJoinCommitted(size, false), benchmarkMultiJoinCommitted(size, true), 3, changed, tc.aggregate)
		})
	}
	for _, tc := range cases {
		b.Run("chain4/"+tc.name, func(b *testing.B) {
			changed := types.ProductValue{types.NewUint64(uint64(size + 3000)), types.NewUint64(uint64(size/2 + 1))}
			benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinFourRelationTestSchema(), multiJoinFourRelationPredicate(), benchmarkMultiJoinFourRelationCommitted(size, false), benchmarkMultiJoinFourRelationCommitted(size, true), 4, changed, tc.aggregate)
		})
	}
	for _, tc := range cases {
		b.Run("cross3_rows_32/"+tc.name, func(b *testing.B) {
			const crossSize = 32
			changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
			benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, tc.aggregate)
		})
	}
	for _, tc := range cases {
		b.Run("cross3_rows_40/"+tc.name, func(b *testing.B) {
			const crossSize = 40
			changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
			benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, tc.aggregate)
		})
	}
	for _, tc := range cases {
		b.Run("cross3_rows_48/"+tc.name, func(b *testing.B) {
			const crossSize = 48
			changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
			benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, tc.aggregate)
		})
	}
	for _, tc := range cases {
		b.Run("cross3_rows_56/"+tc.name, func(b *testing.B) {
			const crossSize = 56
			changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
			benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, tc.aggregate)
		})
	}
	for _, tc := range cases {
		b.Run("cross3_rows_64/"+tc.name, func(b *testing.B) {
			const crossSize = 64
			changed := types.ProductValue{types.NewUint64(uint64(crossSize + 1000)), types.NewUint64(uint64(crossSize/2 + 1))}
			benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), benchmarkMultiJoinCross3Predicate(), benchmarkMultiJoinCommitted(crossSize, false), benchmarkMultiJoinCommitted(crossSize, true), 3, changed, tc.aggregate)
		})
	}
	for _, skew := range []struct {
		name      string
		hotFanout int
	}{
		{name: "hot_key_16x16", hotFanout: 16},
		{name: "hot_key_24x24", hotFanout: 24},
		{name: "hot_key_32x32", hotFanout: 32},
		{name: "hot_key_40x40", hotFanout: 40},
	} {
		for _, tc := range cases {
			b.Run(skew.name+"/"+tc.name, func(b *testing.B) {
				changed := types.ProductValue{types.NewUint64(9000), types.NewUint64(1)}
				before := benchmarkMultiJoinSelectivityCommitted(size, skew.hotFanout, nil)
				after := benchmarkMultiJoinSelectivityCommitted(size, skew.hotFanout, []types.ProductValue{changed})
				cs := benchmarkMultiJoinInsertChangeset(3, []types.ProductValue{changed})
				benchmarkMultiWayLiveJoinDelta(b, multiJoinTestSchema(), multiJoinTestPredicate(), before, after, cs, tc.aggregate)
			})
		}
	}

	selfAliasCases := []struct {
		name      string
		aggregate *Aggregate
	}{
		{name: "count_star", aggregate: countStarAggregate()},
		{name: "count_column", aggregate: countSelfAliasEndpointIDAggregate()},
		{name: "count_distinct", aggregate: countDistinctMultiJoinTIDAggregate()},
		{name: "sum", aggregate: sumSelfAliasEndpointIDAggregate()},
	}
	for _, tc := range selfAliasCases {
		b.Run("self_alias3/"+tc.name, func(b *testing.B) {
			changed := types.ProductValue{types.NewUint64(uint64(size + 2000)), types.NewUint64(uint64(size/2 + 1))}
			benchmarkMultiWayLiveJoinShapeAggregate(b, multiJoinTestSchema(), repeatedMultiJoinConditionPredicate(), benchmarkMultiJoinCommitted(size, false), benchmarkMultiJoinSelfAliasCommitted(size, true), 2, changed, tc.aggregate)
		})
	}
}

func countSelfAliasEndpointIDAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateCount,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "n", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  2,
			Column: 0,
			Alias:  2,
		},
	}
}

func sumSelfAliasEndpointIDAggregate() *Aggregate {
	return &Aggregate{
		Func:         AggregateSum,
		ResultColumn: schema.ColumnSchema{Index: 0, Name: "total", Type: types.KindUint64},
		Argument: &AggregateColumn{
			Schema: schema.ColumnSchema{Index: 0, Name: "id", Type: types.KindUint64},
			Table:  2,
			Column: 0,
			Alias:  2,
		},
	}
}

func benchmarkMultiWayLiveJoinShapeAggregate(b *testing.B, s *fakeSchema, pred MultiJoin, before, after *mockCommitted, changedTable TableID, changed types.ProductValue, aggregate *Aggregate) {
	b.Helper()
	cs := benchmarkMultiJoinInsertChangeset(changedTable, []types.ProductValue{changed})
	benchmarkMultiWayLiveJoinDelta(b, s, pred, before, after, cs, aggregate)
}

func benchmarkMultiWayLiveJoinDelta(b *testing.B, s *fakeSchema, pred MultiJoin, before, after *mockCommitted, cs *store.Changeset, aggregate *Aggregate) {
	b.Helper()
	inbox := make(chan FanOutMessage, 1024)
	mgr := NewManager(s, s, WithFanOutInbox(inbox))
	req := SubscriptionSetRegisterRequest{
		ConnID:     types.ConnectionID{9},
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

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.EvalAndBroadcast(types.TxID(uint64(i+2)), cs, after, PostCommitMeta{})
	}
}

func benchmarkMultiJoinCross3Predicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
		},
		ProjectedRelation: 0,
	}
}

func benchmarkMultiJoinFiveRelationTestSchema() *fakeSchema {
	s := newFakeSchema()
	cols := map[ColID]types.ValueKind{0: types.KindUint64, 1: types.KindUint64}
	for table := TableID(1); table <= 5; table++ {
		s.addTable(table, cols, 1)
	}
	return s
}

func benchmarkMultiJoinFiveRelationPredicate() MultiJoin {
	return MultiJoin{
		Relations: []MultiJoinRelation{
			{Table: 1, Alias: 0},
			{Table: 2, Alias: 1},
			{Table: 3, Alias: 2},
			{Table: 4, Alias: 3},
			{Table: 5, Alias: 4},
		},
		Conditions: []MultiJoinCondition{
			{
				Left:  MultiJoinColumnRef{Relation: 0, Table: 1, Column: 1, Alias: 0},
				Right: MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 1, Table: 2, Column: 1, Alias: 1},
				Right: MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 2, Table: 3, Column: 1, Alias: 2},
				Right: MultiJoinColumnRef{Relation: 3, Table: 4, Column: 1, Alias: 3},
			},
			{
				Left:  MultiJoinColumnRef{Relation: 3, Table: 4, Column: 1, Alias: 3},
				Right: MultiJoinColumnRef{Relation: 4, Table: 5, Column: 1, Alias: 4},
			},
		},
		ProjectedRelation: 0,
	}
}

func benchmarkMultiJoinInsertChangeset(table TableID, rows []types.ProductValue) *store.Changeset {
	return &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			table: {TableID: table, TableName: fmt.Sprintf("t%d", table), Inserts: rows},
		},
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

func benchmarkMultiJoinSelectivityCommitted(size, hotFanout int, changedRows []types.ProductValue) *mockCommitted {
	s := multiJoinTestSchema()
	tRows := make([]types.ProductValue, size)
	sRows := make([]types.ProductValue, size)
	rRows := make([]types.ProductValue, size, size+len(changedRows))
	for i := 0; i < size; i++ {
		key := uint64(i + 2)
		if i < hotFanout {
			key = 1
		}
		tRows[i] = types.ProductValue{types.NewUint64(uint64(i + 1)), types.NewUint64(key)}
		sRows[i] = types.ProductValue{types.NewUint64(uint64(i + 1001)), types.NewUint64(key)}
		rRows[i] = types.ProductValue{types.NewUint64(uint64(i + 2001)), types.NewUint64(uint64(i + 20_000))}
	}
	rRows = append(rRows, changedRows...)
	return buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: tRows,
		2: sRows,
		3: rRows,
	})
}

func benchmarkMultiJoinChangedRows(size, count int, idBase uint64) []types.ProductValue {
	rows := make([]types.ProductValue, count)
	for i := range rows {
		rows[i] = types.ProductValue{
			types.NewUint64(idBase + uint64(i)),
			types.NewUint64(uint64(i%size + 1)),
		}
	}
	return rows
}

func benchmarkMultiJoinCommittedWithChangedRows(size int, changedRows []types.ProductValue) *mockCommitted {
	s := multiJoinTestSchema()
	tRows := make([]types.ProductValue, size)
	sRows := make([]types.ProductValue, size)
	rRows := make([]types.ProductValue, size, size+len(changedRows))
	for i := 0; i < size; i++ {
		key := types.NewUint64(uint64(i + 1))
		tRows[i] = types.ProductValue{types.NewUint64(uint64(i + 1)), key}
		sRows[i] = types.ProductValue{types.NewUint64(uint64(i + 1001)), key}
		rRows[i] = types.ProductValue{types.NewUint64(uint64(i + 2001)), key}
	}
	rRows = append(rRows, changedRows...)
	return buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: tRows,
		2: sRows,
		3: rRows,
	})
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

func benchmarkMultiJoinSelfAliasCommitted(size int, includeChanged bool) *mockCommitted {
	s := multiJoinTestSchema()
	tRows := make([]types.ProductValue, size)
	sRows := make([]types.ProductValue, size, size+1)
	for i := 0; i < size; i++ {
		key := types.NewUint64(uint64(i + 1))
		tRows[i] = types.ProductValue{key, key}
		sRows[i] = types.ProductValue{types.NewUint64(uint64(i + 1001)), key}
	}
	if includeChanged {
		sRows = append(sRows, types.ProductValue{
			types.NewUint64(uint64(size + 2000)),
			types.NewUint64(uint64(size/2 + 1)),
		})
	}
	return buildMockCommitted(s, map[TableID][]types.ProductValue{
		1: tRows,
		2: sRows,
	})
}

func benchmarkMultiJoinFourRelationCommitted(size int, includeChanged bool) *mockCommitted {
	s := multiJoinFourRelationTestSchema()
	rows := make(map[TableID][]types.ProductValue, 4)
	for table := TableID(1); table <= 4; table++ {
		tableRows := make([]types.ProductValue, size)
		for i := 0; i < size; i++ {
			key := types.NewUint64(uint64(i + 1))
			tableRows[i] = types.ProductValue{types.NewUint64(uint64(i) + uint64(table)*1000), key}
		}
		rows[table] = tableRows
	}
	if includeChanged {
		rows[4] = append(rows[4], types.ProductValue{
			types.NewUint64(uint64(size + 3000)),
			types.NewUint64(uint64(size/2 + 1)),
		})
	}
	return buildMockCommitted(s, rows)
}

func benchmarkMultiJoinFiveRelationCommitted(size int, includeChanged bool) *mockCommitted {
	s := benchmarkMultiJoinFiveRelationTestSchema()
	rows := make(map[TableID][]types.ProductValue, 5)
	for table := TableID(1); table <= 5; table++ {
		tableRows := make([]types.ProductValue, size)
		for i := 0; i < size; i++ {
			key := types.NewUint64(uint64(i + 1))
			tableRows[i] = types.ProductValue{types.NewUint64(uint64(i) + uint64(table)*1000), key}
		}
		rows[table] = tableRows
	}
	if includeChanged {
		rows[5] = append(rows[5], types.ProductValue{
			types.NewUint64(uint64(size + 4000)),
			types.NewUint64(uint64(size/2 + 1)),
		})
	}
	return buildMockCommitted(s, rows)
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
