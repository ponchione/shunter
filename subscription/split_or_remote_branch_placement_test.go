package subscription

import (
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func TestPlaceJoinSplitOrRemoteBranchesAvoidBroadExistence(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
		2: types.KindUint64,
	}, 0, 2)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
		2: types.KindUint64,
	}, 0, 2)
	idx := NewPruningIndexes()
	pred := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: Or{
			Left: ColEqCol{
				LeftTable:   1,
				LeftColumn:  2,
				RightTable:  2,
				RightColumn: 2,
			},
			Right: ColEq{Table: 2, Column: 1, Value: types.NewString("red")},
		},
	}
	hash := hashN(1)
	placeSubscriptionForResolver(idx, pred, hash, s)

	eqEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 2, RHSJoinCol: 2, RHSFilterCol: 2}
	if _, ok := idx.JoinEdge.exists[eqEdge][hash]; !ok {
		t.Fatalf("remote column-equality branch edge missing: %+v", idx.JoinEdge.exists)
	}
	valueEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 1}
	if got := idx.JoinEdge.Lookup(valueEdge, types.NewString("red")); len(got) != 1 || got[0] != hash {
		t.Fatalf("remote value branch edge = %v, want [%v]", got, hash)
	}
	broadEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 0}
	if got := idx.JoinEdge.exists[broadEdge]; len(got) != 0 {
		t.Fatalf("broad join-existence candidates = %v, want none", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want empty for remote split-OR branches", got)
	}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(10), types.NewString("blue"), types.NewUint64(99)}},
	})
	mismatch := []types.ProductValue{{types.NewUint64(10), types.NewString("local"), types.NewUint64(7)}}
	if got := CollectCandidatesForTable(idx, 1, mismatch, committed, s); len(got) != 0 {
		t.Fatalf("mismatched remote split-OR candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(10), types.NewString("red"), types.NewUint64(99)}},
	})
	got := CollectCandidatesForTable(idx, 1, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("remote value split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(11), types.NewString("blue"), types.NewUint64(7)}},
	})
	got = CollectCandidatesForTable(idx, 1, mismatch, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("remote column-equality split-OR candidates = %v, want [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestPlaceJoinAllRemoteSplitOrRangeBranchesUseRangeEdges(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
	}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
	}, 0)
	idx := NewPruningIndexes()
	pred := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: And{
			Left: AllRows{Table: 1},
			Right: Or{
				Left: ColRange{
					Table:  2,
					Column: 1,
					Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
					Upper:  Bound{Unbounded: true},
				},
				Right: ColNe{Table: 2, Column: 1, Value: types.NewUint64(7)},
			},
		},
	}
	hash := hashN(2)
	placeSubscriptionForResolver(idx, pred, hash, s)

	rangeEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 1}
	for _, value := range []uint64{6, 8, 60} {
		if got := idx.JoinRangeEdge.Lookup(rangeEdge, types.NewUint64(value)); len(got) != 1 || got[0] != hash {
			t.Fatalf("remote range branch edge for %d = %v, want [%v]", value, got, hash)
		}
	}
	if got := idx.JoinRangeEdge.Lookup(rangeEdge, types.NewUint64(7)); len(got) != 0 {
		t.Fatalf("remote ColNe rejected range edge = %v, want empty", got)
	}
	if got := idx.Range.Lookup(2, 1, types.NewUint64(60)); len(got) != 1 || got[0] != hash {
		t.Fatalf("remote-side local range lookup = %v, want deduped [%v]", got, hash)
	}
	broadEdge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 0, RHSFilterCol: 0}
	if got := idx.JoinEdge.exists[broadEdge]; len(got) != 0 {
		t.Fatalf("broad join-existence candidates = %v, want none", got)
	}
	if got := idx.Table.Lookup(1); len(got) != 0 {
		t.Fatalf("TableIndex[1] = %v, want empty for all-remote range split-OR branches", got)
	}

	committed := buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(10), types.NewUint64(7)}},
	})
	changed := []types.ProductValue{{types.NewUint64(10), types.NewString("local")}}
	if got := CollectCandidatesForTable(idx, 1, changed, committed, s); len(got) != 0 {
		t.Fatalf("rejected remote range split-OR candidates = %v, want empty", got)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(10), types.NewUint64(8)}},
	})
	got := CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("remote ColNe split-OR candidates = %v, want [%v]", got, hash)
	}

	committed = buildMockCommitted(s, map[TableID][]types.ProductValue{
		2: {{types.NewUint64(10), types.NewUint64(60)}},
	})
	got = CollectCandidatesForTable(idx, 1, changed, committed, s)
	if len(got) != 1 || got[0] != hash {
		t.Fatalf("remote range split-OR candidates = %v, want [%v]", got, hash)
	}

	removeSubscriptionForResolver(idx, pred, hash, s)
	if !pruningIndexesEmpty(idx) {
		t.Fatalf("indexes after remove = %+v, want empty", idx)
	}
}

func TestCollectJoinAllRemoteSplitOrRangeBranchSameTransactionRows(t *testing.T) {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindString,
	}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
	}, 0)
	idx := NewPruningIndexes()
	pred := Join{
		Left: 1, Right: 2, LeftCol: 0, RightCol: 0,
		Filter: And{
			Left: AllRows{Table: 1},
			Right: Or{
				Left: ColRange{
					Table:  2,
					Column: 1,
					Lower:  Bound{Value: types.NewUint64(50), Inclusive: false},
					Upper:  Bound{Unbounded: true},
				},
				Right: ColNe{Table: 2, Column: 1, Value: types.NewUint64(7)},
			},
		},
	}
	hash := hashN(3)
	placeSubscriptionForResolver(idx, pred, hash, s)
	leftRows := []types.ProductValue{{types.NewUint64(10), types.NewString("local")}}

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(10), types.NewUint64(7)}}},
		},
	}
	got := make(map[QueryHash]struct{})
	collectJoinFilterDeltaCandidates(idx, 1, leftRows, rejected, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if len(got) != 0 {
		t.Fatalf("rejected same-tx remote range candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			2: {Inserts: []types.ProductValue{{types.NewUint64(10), types.NewUint64(60)}}},
		},
	}
	collectJoinFilterDeltaCandidates(idx, 1, leftRows, overlap, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping same-tx remote range candidates = %v, want only %v", got, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			2: {Deletes: []types.ProductValue{{types.NewUint64(10), types.NewUint64(8)}}},
		},
	}
	clear(got)
	collectJoinFilterDeltaCandidates(idx, 1, leftRows, deleteOverlap, func(h QueryHash) {
		got[h] = struct{}{}
	})
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping same-tx remote ColNe delete candidates = %v, want only %v", got, hash)
	}
}
