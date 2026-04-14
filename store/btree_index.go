package store

import (
	"iter"
	"slices"

	"github.com/ponchione/shunter/types"
)

// BTreeIndex is a sorted index mapping IndexKey → []RowID.
// Uses a sorted slice internally.
type BTreeIndex struct {
	entries []btreeEntry
}

type btreeEntry struct {
	key    IndexKey
	rowIDs []types.RowID
}

// NewBTreeIndex creates an empty index.
func NewBTreeIndex() *BTreeIndex {
	return &BTreeIndex{}
}

func (b *BTreeIndex) search(key IndexKey) (int, bool) {
	idx, found := slices.BinarySearchFunc(b.entries, key, func(e btreeEntry, k IndexKey) int {
		return e.key.Compare(k)
	})
	return idx, found
}

// Insert adds a rowID under the given key.
func (b *BTreeIndex) Insert(key IndexKey, rowID types.RowID) {
	idx, found := b.search(key)
	if found {
		e := &b.entries[idx]
		// Maintain ascending RowID order.
		pos, _ := slices.BinarySearch(e.rowIDs, rowID)
		e.rowIDs = slices.Insert(e.rowIDs, pos, rowID)
		return
	}
	b.entries = slices.Insert(b.entries, idx, btreeEntry{
		key:    key,
		rowIDs: []types.RowID{rowID},
	})
}

// Remove removes a specific rowID from the given key.
// Deletes the key entry if no RowIDs remain.
func (b *BTreeIndex) Remove(key IndexKey, rowID types.RowID) {
	idx, found := b.search(key)
	if !found {
		return
	}
	e := &b.entries[idx]
	pos, ok := slices.BinarySearch(e.rowIDs, rowID)
	if !ok {
		return
	}
	e.rowIDs = slices.Delete(e.rowIDs, pos, pos+1)
	if len(e.rowIDs) == 0 {
		b.entries = slices.Delete(b.entries, idx, idx+1)
	}
}

// Seek returns all RowIDs for an exact key match, or nil.
func (b *BTreeIndex) Seek(key IndexKey) []types.RowID {
	idx, found := b.search(key)
	if !found {
		return nil
	}
	return b.entries[idx].rowIDs
}

// Len returns the total number of key→RowID mappings.
func (b *BTreeIndex) Len() int {
	n := 0
	for _, e := range b.entries {
		n += len(e.rowIDs)
	}
	return n
}

// SeekRange returns RowIDs for keys in [low, high). nil = unbounded.
func (b *BTreeIndex) SeekRange(low, high *IndexKey) iter.Seq[types.RowID] {
	return func(yield func(types.RowID) bool) {
		startIdx := 0
		if low != nil {
			startIdx, _ = slices.BinarySearchFunc(b.entries, *low, func(e btreeEntry, k IndexKey) int {
				return e.key.Compare(k)
			})
		}
		for i := startIdx; i < len(b.entries); i++ {
			e := b.entries[i]
			if high != nil && e.key.Compare(*high) >= 0 {
				return
			}
			for _, rid := range e.rowIDs {
				if !yield(rid) {
					return
				}
			}
		}
	}
}

// Scan returns all RowIDs in key order.
func (b *BTreeIndex) Scan() iter.Seq[types.RowID] {
	return b.SeekRange(nil, nil)
}
