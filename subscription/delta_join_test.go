package subscription

import (
	"testing"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// joinChangeset builds a two-table changeset for join tests.
func joinChangeset(
	t1 TableID, insT1, delT1 []types.ProductValue,
	t2 TableID, insT2, delT2 []types.ProductValue,
) *store.Changeset {
	return &store.Changeset{
		TxID: 1,
		Tables: map[schema.TableID]*store.TableChangeset{
			t1: {TableID: t1, TableName: "t1", Inserts: insT1, Deletes: delT1},
			t2: {TableID: t2, TableName: "t2", Inserts: insT2, Deletes: delT2},
		},
	}
}

// Join setup: T1(id, name) join T2(id, t1_id) on t1_id=id.
const (
	joinLHS    TableID = 1
	joinRHS    TableID = 2
	joinLHSCol ColID   = 0 // T1.id
	joinRHSCol ColID   = 1 // T2.t1_id

	joinLHSIdx IndexID = 100 // index on T1.id
	joinRHSIdx IndexID = 200 // index on T2.t1_id
)

func newJoinResolver() *mockResolver {
	r := newMockResolver()
	r.register(joinLHS, joinLHSCol, joinLHSIdx)
	r.register(joinRHS, joinRHSCol, joinRHSIdx)
	return r
}

func newJoinCommitted() *mockCommitted {
	c := newMockCommitted()
	c.setIndex(joinLHS, joinLHSIdx, int(joinLHSCol))
	c.setIndex(joinRHS, joinRHSIdx, int(joinRHSCol))
	return c
}

func TestJoinI1DriveInsertT1ProbeCommittedT2(t *testing.T) {
	// T2 committed: (100, 1), (101, 1), (102, 2).
	// dT1(+) = (1, "a").
	// I1 yields (1,"a") joined with (100,1) and (101,1) — 2 rows.
	committed := newJoinCommitted()
	committed.addRow(joinRHS, 10, types.ProductValue{types.NewUint64(100), types.NewUint64(1)})
	committed.addRow(joinRHS, 11, types.ProductValue{types.NewUint64(101), types.NewUint64(1)})
	committed.addRow(joinRHS, 12, types.ProductValue{types.NewUint64(102), types.NewUint64(2)})
	cs := joinChangeset(
		joinLHS, []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}, nil,
		joinRHS, nil, nil,
	)
	dv := NewDeltaView(committed, cs, map[TableID][]ColID{joinLHS: {joinLHSCol}, joinRHS: {joinRHSCol}})
	join := &Join{Left: joinLHS, Right: joinRHS, LeftCol: joinLHSCol, RightCol: joinRHSCol}
	f := EvalJoinDeltaFragments(dv, join, newJoinResolver())
	if len(f.Inserts[0]) != 2 {
		t.Fatalf("I1 len = %d, want 2", len(f.Inserts[0]))
	}
}

func TestJoinI1NoMatches(t *testing.T) {
	committed := newJoinCommitted()
	committed.addRow(joinRHS, 10, types.ProductValue{types.NewUint64(100), types.NewUint64(7)})
	cs := joinChangeset(
		joinLHS, []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}, nil,
		joinRHS, nil, nil,
	)
	dv := NewDeltaView(committed, cs, map[TableID][]ColID{joinLHS: {joinLHSCol}, joinRHS: {joinRHSCol}})
	join := &Join{Left: joinLHS, Right: joinRHS, LeftCol: joinLHSCol, RightCol: joinRHSCol}
	f := EvalJoinDeltaFragments(dv, join, newJoinResolver())
	if len(f.Inserts[0]) != 0 {
		t.Fatalf("I1 empty expected, got %d", len(f.Inserts[0]))
	}
}

func TestJoinAllFragmentsPresent(t *testing.T) {
	committed := newJoinCommitted()
	committed.addRow(joinLHS, 1, types.ProductValue{types.NewUint64(1), types.NewString("committed-a")})
	committed.addRow(joinRHS, 10, types.ProductValue{types.NewUint64(100), types.NewUint64(1)})
	cs := joinChangeset(
		joinLHS,
		[]types.ProductValue{{types.NewUint64(2), types.NewString("new-a")}},
		[]types.ProductValue{{types.NewUint64(1), types.NewString("old-a")}},
		joinRHS,
		[]types.ProductValue{{types.NewUint64(101), types.NewUint64(2)}},
		[]types.ProductValue{{types.NewUint64(102), types.NewUint64(1)}},
	)
	dv := NewDeltaView(committed, cs, map[TableID][]ColID{joinLHS: {joinLHSCol}, joinRHS: {joinRHSCol}})
	join := &Join{Left: joinLHS, Right: joinRHS, LeftCol: joinLHSCol, RightCol: joinRHSCol}
	f := EvalJoinDeltaFragments(dv, join, newJoinResolver())
	// Sanity: fragments are produced even if some are empty.
	_ = f
	if len(f.Inserts) != 4 || len(f.Deletes) != 4 {
		t.Fatalf("expected 4+4 fragments, got %d+%d", len(f.Inserts), len(f.Deletes))
	}
}

func TestJoinWithFilterExcludesNonMatching(t *testing.T) {
	committed := newJoinCommitted()
	committed.addRow(joinRHS, 10, types.ProductValue{types.NewUint64(100), types.NewUint64(1)}) // t2.id=100, t1_id=1
	cs := joinChangeset(
		joinLHS, []types.ProductValue{{types.NewUint64(1), types.NewString("a")}}, nil,
		joinRHS, nil, nil,
	)
	dv := NewDeltaView(committed, cs, map[TableID][]ColID{joinLHS: {joinLHSCol}, joinRHS: {joinRHSCol}})
	// Filter requires T2.id = 999 — never matches.
	join := &Join{
		Left: joinLHS, Right: joinRHS, LeftCol: joinLHSCol, RightCol: joinRHSCol,
		Filter: ColEq{Table: joinRHS, Column: 0, Value: types.NewUint64(999)},
	}
	f := EvalJoinDeltaFragments(dv, join, newJoinResolver())
	if len(f.Inserts[0]) != 0 {
		t.Fatalf("filter did not exclude: got %d rows", len(f.Inserts[0]))
	}
}

func TestJoinWithCrossSideOrFilterEvaluatesAgainstJoinedPair(t *testing.T) {
	committed := newJoinCommitted()
	committed.addRow(joinRHS, 10, types.ProductValue{types.NewUint64(100), types.NewUint64(2)})
	committed.addRow(joinRHS, 11, types.ProductValue{types.NewUint64(200), types.NewUint64(3)})
	cs := joinChangeset(
		joinLHS,
		[]types.ProductValue{
			{types.NewUint64(2), types.NewString("no-match")},
			{types.NewUint64(3), types.NewString("rhs-match")},
		},
		nil,
		joinRHS, nil, nil,
	)
	dv := NewDeltaView(committed, cs, map[TableID][]ColID{joinLHS: {joinLHSCol}, joinRHS: {joinRHSCol}})
	join := &Join{
		Left: joinLHS, Right: joinRHS, LeftCol: joinLHSCol, RightCol: joinRHSCol,
		Filter: Or{
			Left:  ColEq{Table: joinLHS, Column: joinLHSCol, Value: types.NewUint64(1)},
			Right: ColEq{Table: joinRHS, Column: 0, Value: types.NewUint64(200)},
		},
	}
	f := EvalJoinDeltaFragments(dv, join, newJoinResolver())
	if len(f.Inserts[0]) != 1 {
		t.Fatalf("I1 len = %d, want 1", len(f.Inserts[0]))
	}
	want := types.ProductValue{
		types.NewUint64(3), types.NewString("rhs-match"),
		types.NewUint64(200), types.NewUint64(3),
	}
	if !f.Inserts[0][0].Equal(want) {
		t.Fatalf("I1 row = %v, want %v", f.Inserts[0][0], want)
	}
}

func TestJoinFragmentEqualsFullReEvaluation(t *testing.T) {
	// Baseline: reconcile the 4+4 fragments via ReconcileJoinDelta and
	// compare the resulting inserts/deletes to a full re-evaluation of the
	// join before and after the transaction.
	committed := newJoinCommitted()
	// Committed before tx.
	committed.addRow(joinLHS, 1, types.ProductValue{types.NewUint64(1), types.NewString("a")})
	committed.addRow(joinLHS, 2, types.ProductValue{types.NewUint64(2), types.NewString("b")})
	committed.addRow(joinRHS, 10, types.ProductValue{types.NewUint64(100), types.NewUint64(1)})
	committed.addRow(joinRHS, 11, types.ProductValue{types.NewUint64(101), types.NewUint64(2)})

	// Pre-tx join baseline.
	preInserts := preJoin(committed, joinLHS, joinRHS, int(joinLHSCol), int(joinRHSCol))

	// Simulate the post-tx committed state by applying the changeset to the
	// in-memory mock.
	insT1 := []types.ProductValue{{types.NewUint64(3), types.NewString("c")}}
	insT2 := []types.ProductValue{{types.NewUint64(102), types.NewUint64(3)}}
	delT2 := []types.ProductValue{{types.NewUint64(101), types.NewUint64(2)}}
	cs := joinChangeset(joinLHS, insT1, nil, joinRHS, insT2, delT2)

	// Build DeltaView against the committed view BEFORE the transaction,
	// then manually apply changes in a copy for post-tx reference.
	// For I1/I2/D1/D2 we need the *post*-tx committed state; build a second
	// mock to represent it.
	postCommitted := newJoinCommitted()
	for rid, row := range committed.rows[joinLHS] {
		postCommitted.addRow(joinLHS, rid, row)
	}
	for rid, row := range committed.rows[joinRHS] {
		postCommitted.addRow(joinRHS, rid, row)
	}
	for i, r := range insT1 {
		postCommitted.addRow(joinLHS, types.RowID(1000+i), r)
	}
	for i, r := range insT2 {
		postCommitted.addRow(joinRHS, types.RowID(2000+i), r)
	}
	for _, r := range delT2 {
		// delete matching row by value.
		for rid, existing := range postCommitted.rows[joinRHS] {
			if existing.Equal(r) {
				delete(postCommitted.rows[joinRHS], rid)
				break
			}
		}
	}

	postInserts := preJoin(postCommitted, joinLHS, joinRHS, int(joinLHSCol), int(joinRHSCol))

	// Baseline delta = postInserts - preInserts (bag subtraction).
	baselineInsertCounts := map[string]int{}
	baselineInsertRows := map[string]types.ProductValue{}
	for _, r := range postInserts {
		k := encodeRowKey(r)
		baselineInsertCounts[k]++
		if _, ok := baselineInsertRows[k]; !ok {
			baselineInsertRows[k] = r
		}
	}
	baselineDeleteCounts := map[string]int{}
	baselineDeleteRows := map[string]types.ProductValue{}
	for _, r := range preInserts {
		k := encodeRowKey(r)
		if baselineInsertCounts[k] > 0 {
			baselineInsertCounts[k]--
		} else {
			baselineDeleteCounts[k]++
			if _, ok := baselineDeleteRows[k]; !ok {
				baselineDeleteRows[k] = r
			}
		}
	}

	// Fragment evaluation uses the post-tx committed view.
	dv := NewDeltaView(postCommitted, cs, map[TableID][]ColID{joinLHS: {joinLHSCol}, joinRHS: {joinRHSCol}})
	join := &Join{Left: joinLHS, Right: joinRHS, LeftCol: joinLHSCol, RightCol: joinRHSCol}
	f := EvalJoinDeltaFragments(dv, join, newJoinResolver())
	fragInserts, fragDeletes := ReconcileJoinDelta(f.Inserts[:], f.Deletes[:])

	// Convert fragment delta to count maps and compare.
	fragInsCounts := map[string]int{}
	for _, r := range fragInserts {
		fragInsCounts[encodeRowKey(r)]++
	}
	fragDelCounts := map[string]int{}
	for _, r := range fragDeletes {
		fragDelCounts[encodeRowKey(r)]++
	}

	// Drop zero entries from baseline.
	for k, v := range baselineInsertCounts {
		if v == 0 {
			delete(baselineInsertCounts, k)
		}
	}

	if !sameCounts(fragInsCounts, baselineInsertCounts) {
		t.Fatalf("insert counts differ: frag=%v baseline=%v", fragInsCounts, baselineInsertCounts)
	}
	if !sameCounts(fragDelCounts, baselineDeleteCounts) {
		t.Fatalf("delete counts differ: frag=%v baseline=%v", fragDelCounts, baselineDeleteCounts)
	}
}

func sameCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// preJoin returns the full join of two in-memory tables as concatenated
// (LHS, RHS) rows — a re-evaluation baseline for property checks.
func preJoin(m *mockCommitted, lt, rt TableID, lcol, rcol int) []types.ProductValue {
	var out []types.ProductValue
	for _, lrow := range m.rows[lt] {
		for _, rrow := range m.rows[rt] {
			if lcol >= len(lrow) || rcol >= len(rrow) {
				continue
			}
			if lrow[lcol].Equal(rrow[rcol]) {
				joined := make(types.ProductValue, 0, len(lrow)+len(rrow))
				joined = append(joined, lrow...)
				joined = append(joined, rrow...)
				out = append(out, joined)
			}
		}
	}
	return out
}
