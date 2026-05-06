package subscription

import (
	"testing"

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
