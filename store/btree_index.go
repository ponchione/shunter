package store

import (
	"iter"
	"slices"

	"github.com/ponchione/shunter/types"
)

const btreePageMaxEntries = 64

// BTreeIndex is a sorted index mapping IndexKey -> []RowID.
//
// The implementation stores sorted leaf pages and splits pages as they grow.
// That keeps point/range lookup semantics stable while avoiding whole-index
// slice shifts for every distinct-key insert or delete.
type BTreeIndex struct {
	pages    []*btreePage
	mappings int
}

type btreePage struct {
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

func (b *BTreeIndex) lowerBound(key IndexKey) (pageIdx, entryIdx int, found bool) {
	if len(b.pages) == 0 {
		return 0, 0, false
	}
	pageIdx, _ = slices.BinarySearchFunc(b.pages, key, func(p *btreePage, k IndexKey) int {
		return p.entries[len(p.entries)-1].key.Compare(k)
	})
	if pageIdx == len(b.pages) {
		return pageIdx, 0, false
	}
	entries := b.pages[pageIdx].entries
	entryIdx, found = slices.BinarySearchFunc(entries, key, func(e btreeEntry, k IndexKey) int {
		return e.key.Compare(k)
	})
	return pageIdx, entryIdx, found
}

func (b *BTreeIndex) splitPage(pageIdx int) {
	p := b.pages[pageIdx]
	if len(p.entries) <= btreePageMaxEntries {
		return
	}
	mid := len(p.entries) / 2
	rightEntries := slices.Clone(p.entries[mid:])
	p.entries = p.entries[:mid]
	right := &btreePage{entries: rightEntries}
	b.pages = slices.Insert(b.pages, pageIdx+1, right)
}

func (b *BTreeIndex) mergeSparsePage(pageIdx int) {
	if pageIdx < 0 || pageIdx >= len(b.pages) || len(b.pages) < 2 {
		return
	}
	p := b.pages[pageIdx]
	if len(p.entries) >= btreePageMaxEntries/4 {
		return
	}
	if pageIdx > 0 {
		prev := b.pages[pageIdx-1]
		if len(prev.entries)+len(p.entries) <= btreePageMaxEntries {
			prev.entries = append(prev.entries, p.entries...)
			b.pages = slices.Delete(b.pages, pageIdx, pageIdx+1)
			return
		}
	}
	if pageIdx+1 < len(b.pages) {
		next := b.pages[pageIdx+1]
		if len(p.entries)+len(next.entries) <= btreePageMaxEntries {
			p.entries = append(p.entries, next.entries...)
			b.pages = slices.Delete(b.pages, pageIdx+1, pageIdx+2)
		}
	}
}

// Insert adds a rowID under the given key.
func (b *BTreeIndex) Insert(key IndexKey, rowID types.RowID) {
	if len(b.pages) == 0 {
		b.pages = append(b.pages, &btreePage{entries: []btreeEntry{{
			key:    key,
			rowIDs: []types.RowID{rowID},
		}}})
		b.mappings++
		return
	}

	pageIdx, entryIdx, found := b.lowerBound(key)
	if pageIdx == len(b.pages) {
		pageIdx = len(b.pages) - 1
		entryIdx = len(b.pages[pageIdx].entries)
	}
	p := b.pages[pageIdx]
	if found {
		e := &p.entries[entryIdx]
		if len(e.rowIDs) == 0 || e.rowIDs[len(e.rowIDs)-1] < rowID {
			e.rowIDs = append(e.rowIDs, rowID)
		} else {
			pos, _ := slices.BinarySearch(e.rowIDs, rowID)
			e.rowIDs = slices.Insert(e.rowIDs, pos, rowID)
		}
		b.mappings++
		return
	}

	p.entries = slices.Insert(p.entries, entryIdx, btreeEntry{
		key:    key,
		rowIDs: []types.RowID{rowID},
	})
	b.mappings++
	b.splitPage(pageIdx)
}

// Remove removes a specific rowID from the given key.
// Deletes the key entry if no RowIDs remain.
func (b *BTreeIndex) Remove(key IndexKey, rowID types.RowID) {
	pageIdx, entryIdx, found := b.lowerBound(key)
	if !found {
		return
	}
	p := b.pages[pageIdx]
	e := &p.entries[entryIdx]
	pos, ok := slices.BinarySearch(e.rowIDs, rowID)
	if !ok {
		return
	}
	e.rowIDs = slices.Delete(e.rowIDs, pos, pos+1)
	b.mappings--
	if len(e.rowIDs) != 0 {
		return
	}
	p.entries = slices.Delete(p.entries, entryIdx, entryIdx+1)
	if len(p.entries) == 0 {
		b.pages = slices.Delete(b.pages, pageIdx, pageIdx+1)
		return
	}
	b.mergeSparsePage(pageIdx)
}

// Seek returns all RowIDs for an exact key match, or nil.
func (b *BTreeIndex) Seek(key IndexKey) []types.RowID {
	pageIdx, entryIdx, found := b.lowerBound(key)
	if !found {
		return nil
	}
	return slices.Clone(b.pages[pageIdx].entries[entryIdx].rowIDs)
}

// Len returns the total number of key->RowID mappings.
func (b *BTreeIndex) Len() int {
	return b.mappings
}

// SeekRange returns RowIDs for keys in [low, high). nil = unbounded.
func (b *BTreeIndex) SeekRange(low, high *IndexKey) iter.Seq[types.RowID] {
	return func(yield func(types.RowID) bool) {
		startPage, startEntry := 0, 0
		if low != nil {
			startPage, startEntry, _ = b.lowerBound(*low)
		}
		for pageIdx := startPage; pageIdx < len(b.pages); pageIdx++ {
			entries := b.pages[pageIdx].entries
			entryIdx := 0
			if pageIdx == startPage {
				entryIdx = startEntry
			}
			for ; entryIdx < len(entries); entryIdx++ {
				e := entries[entryIdx]
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
}

// SeekBounds returns RowIDs for keys between low and high per Bound
// semantics (SPEC-001 §4.4 / §4.6). Each endpoint is independently
// inclusive, exclusive, or unbounded. SPEC-004 predicate scans on
// string/bytes/float keys need "strictly greater than v" — expressible
// through Bound but not through *IndexKey. Yields keys in comparator
// order; within a key, RowIDs in ascending order.
func (b *BTreeIndex) SeekBounds(low, high Bound) iter.Seq[types.RowID] {
	return func(yield func(types.RowID) bool) {
		startPage, startEntry := 0, 0
		if !low.Unbounded {
			lowKey := NewIndexKey(low.Value)
			found := false
			startPage, startEntry, found = b.lowerBound(lowKey)
			if found && !low.Inclusive {
				startEntry++
			}
			if startPage < len(b.pages) && startEntry >= len(b.pages[startPage].entries) {
				startPage++
				startEntry = 0
			}
		}
		for pageIdx := startPage; pageIdx < len(b.pages); pageIdx++ {
			entries := b.pages[pageIdx].entries
			entryIdx := 0
			if pageIdx == startPage {
				entryIdx = startEntry
			}
			for ; entryIdx < len(entries); entryIdx++ {
				e := entries[entryIdx]
				if !high.Unbounded {
					cmp := e.key.Compare(NewIndexKey(high.Value))
					if high.Inclusive {
						if cmp > 0 {
							return
						}
					} else if cmp >= 0 {
						return
					}
				}
				for _, rid := range e.rowIDs {
					if !yield(rid) {
						return
					}
				}
			}
		}
	}
}

// Scan returns all RowIDs in key order.
func (b *BTreeIndex) Scan() iter.Seq[types.RowID] {
	return b.SeekRange(nil, nil)
}
