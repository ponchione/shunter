package subscription

import "slices"

func copySlice[T any](in []T) []T {
	if len(in) == 0 {
		return nil
	}
	return slices.Clone(in)
}

func mapKeys[K comparable, V any](m map[K]V) []K {
	if len(m) == 0 {
		return []K{}
	}
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

type columnRefCounts map[TableID]map[ColID]int

func newColumnRefCounts() columnRefCounts {
	return make(columnRefCounts)
}

func (c columnRefCounts) add(table TableID, col ColID) {
	byCol, ok := c[table]
	if !ok {
		byCol = make(map[ColID]int)
		c[table] = byCol
	}
	byCol[col]++
}

func (c columnRefCounts) remove(table TableID, col ColID) {
	byCol, ok := c[table]
	if !ok {
		return
	}
	byCol[col]--
	if byCol[col] <= 0 {
		delete(byCol, col)
	}
	if len(byCol) == 0 {
		delete(c, table)
	}
}

func (c columnRefCounts) trackedColumns(table TableID) []ColID {
	byCol, ok := c[table]
	if !ok {
		return []ColID{}
	}
	return mapKeys(byCol)
}

func (c columnRefCounts) forEachTrackedColumn(table TableID, fn func(ColID)) {
	byCol, ok := c[table]
	if !ok {
		return
	}
	for col := range byCol {
		fn(col)
	}
}
