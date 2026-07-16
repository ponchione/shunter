package store

import (
	"slices"
	"testing"

	"github.com/ponchione/shunter/types"
)

func TestBTreeSparsePageMergePreservesModelOrderingMultiplicityAndLookup(t *testing.T) {
	tests := []struct {
		name        string
		insertCount int
		removeFirst int
		removeLast  int
		wantPages   int
	}{
		{name: "merge sparse first page into right neighbor", insertCount: 65, removeFirst: 1, removeLast: 17, wantPages: 1},
		{name: "merge sparse middle page into left neighbor", insertCount: 97, removeFirst: 33, removeLast: 49, wantPages: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := NewBTreeIndex()
			model := make(map[uint64][]types.RowID, tt.insertCount)
			for key := 1; key <= tt.insertCount; key++ {
				rowIDs := []types.RowID{types.RowID(key * 10)}
				if key%10 == 0 {
					rowIDs = append(rowIDs, types.RowID(key*10+1))
				}
				model[uint64(key)] = rowIDs
				for _, rowID := range rowIDs {
					index.Insert(NewIndexKey(types.NewUint64(uint64(key))), rowID)
				}
			}
			if len(index.pages) <= tt.wantPages {
				t.Fatalf("setup pages = %d, want more than post-merge count %d", len(index.pages), tt.wantPages)
			}

			for key := tt.removeFirst; key <= tt.removeLast; key++ {
				for _, rowID := range model[uint64(key)] {
					index.Remove(NewIndexKey(types.NewUint64(uint64(key))), rowID)
				}
				delete(model, uint64(key))
			}
			if len(index.pages) != tt.wantPages {
				t.Fatalf("pages after sparse merge = %d, want %d", len(index.pages), tt.wantPages)
			}

			wantScan := make([]types.RowID, 0)
			for key := 1; key <= tt.insertCount; key++ {
				rowIDs := model[uint64(key)]
				wantScan = append(wantScan, rowIDs...)
				got := index.Seek(NewIndexKey(types.NewUint64(uint64(key))))
				if !slices.Equal(got, rowIDs) {
					t.Fatalf("Seek key %d after sparse merge = %v, want model %v", key, got, rowIDs)
				}
			}
			gotScan := make([]types.RowID, 0, len(wantScan))
			for rowID := range index.Scan() {
				gotScan = append(gotScan, rowID)
			}
			if !slices.Equal(gotScan, wantScan) {
				t.Fatalf("Scan after sparse merge = %v, want model order/multiplicity %v", gotScan, wantScan)
			}
			if index.Len() != len(wantScan) {
				t.Fatalf("Len after sparse merge = %d, want model mapping count %d", index.Len(), len(wantScan))
			}
		})
	}
}
