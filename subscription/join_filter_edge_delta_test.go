package subscription

import (
	"testing"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func joinFilterEdgeDeltaTestSchema() *fakeSchema {
	s := newFakeSchema()
	s.addTable(1, map[ColID]types.ValueKind{0: types.KindUint64}, 0)
	s.addTable(2, map[ColID]types.ValueKind{
		0: types.KindUint64,
		1: types.KindUint64,
		2: types.KindString,
		3: types.KindUint64,
	}, 1)
	return s
}

func TestCollectCandidatesJoinValueFilterEdgeUsesDeltaOppositeRows(t *testing.T) {
	s := joinFilterEdgeDeltaTestSchema()
	mgr := NewManager(s, s)
	hash := hashN(1)
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 2}
	mgr.indexes.JoinEdge.Add(edge, types.NewString("red"), hash)
	committed := buildMockCommitted(s, nil)
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	noOverlap := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(7)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(20), types.NewUint64(8), types.NewString("red"), types.NewUint64(0)}}},
		},
	}
	if got := mgr.collectCandidatesInto(noOverlap, committed, scratch); len(got) != 0 {
		t.Fatalf("non-overlapping value filter-edge delta candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(7)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(20), types.NewUint64(7), types.NewString("red"), types.NewUint64(0)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping value filter-edge delta candidates = %v, want only %v", got, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Deletes: []types.ProductValue{{types.NewUint64(7)}}},
			2: {Deletes: []types.ProductValue{{types.NewUint64(20), types.NewUint64(7), types.NewString("red"), types.NewUint64(0)}}},
		},
	}
	got = mgr.collectCandidatesInto(deleteOverlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping value filter-edge delete candidates = %v, want only %v", got, hash)
	}
}

func TestCollectCandidatesJoinRangeFilterEdgeUsesDeltaOppositeRows(t *testing.T) {
	s := joinFilterEdgeDeltaTestSchema()
	mgr := NewManager(s, s)
	hash := hashN(2)
	edge := JoinEdge{LHSTable: 1, RHSTable: 2, LHSJoinCol: 0, RHSJoinCol: 1, RHSFilterCol: 3}
	mgr.indexes.JoinRangeEdge.Add(edge,
		Bound{Value: types.NewUint64(50), Inclusive: false},
		Bound{Unbounded: true},
		hash,
	)
	committed := buildMockCommitted(s, nil)
	scratch := acquireCandidateScratch()
	defer releaseCandidateScratch(scratch)

	rejected := &store.Changeset{
		TxID: 1,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(7)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(20), types.NewUint64(7), types.NewString("blue"), types.NewUint64(40)}}},
		},
	}
	if got := mgr.collectCandidatesInto(rejected, committed, scratch); len(got) != 0 {
		t.Fatalf("rejected range filter-edge delta candidates = %v, want empty", got)
	}

	overlap := &store.Changeset{
		TxID: 2,
		Tables: map[TableID]*store.TableChangeset{
			1: {Inserts: []types.ProductValue{{types.NewUint64(7)}}},
			2: {Inserts: []types.ProductValue{{types.NewUint64(20), types.NewUint64(7), types.NewString("blue"), types.NewUint64(60)}}},
		},
	}
	got := mgr.collectCandidatesInto(overlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping range filter-edge delta candidates = %v, want only %v", got, hash)
	}

	deleteOverlap := &store.Changeset{
		TxID: 3,
		Tables: map[TableID]*store.TableChangeset{
			1: {Deletes: []types.ProductValue{{types.NewUint64(7)}}},
			2: {Deletes: []types.ProductValue{{types.NewUint64(20), types.NewUint64(7), types.NewString("blue"), types.NewUint64(60)}}},
		},
	}
	got = mgr.collectCandidatesInto(deleteOverlap, committed, scratch)
	if _, ok := got[hash]; !ok || len(got) != 1 {
		t.Fatalf("overlapping range filter-edge delete candidates = %v, want only %v", got, hash)
	}
}
