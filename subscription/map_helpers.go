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

func addValueHash(byVal map[valueKey]map[QueryHash]struct{}, value Value, hash QueryHash) bool {
	key := encodeValueKey(value)
	set, ok := byVal[key]
	if !ok {
		set = make(map[QueryHash]struct{})
		byVal[key] = set
	}
	if _, exists := set[hash]; exists {
		return false
	}
	set[hash] = struct{}{}
	return true
}

func removeValueHash(byVal map[valueKey]map[QueryHash]struct{}, value Value, hash QueryHash) bool {
	key := encodeValueKey(value)
	set, ok := byVal[key]
	if !ok {
		return false
	}
	if _, ok := set[hash]; !ok {
		return false
	}
	delete(set, hash)
	if len(set) == 0 {
		delete(byVal, key)
	}
	return true
}

func lookupValueHashes(byVal map[valueKey]map[QueryHash]struct{}, value Value) []QueryHash {
	set, ok := byVal[encodeValueKey(value)]
	if !ok {
		return []QueryHash{}
	}
	return mapKeys(set)
}

func forEachValueHash(byVal map[valueKey]map[QueryHash]struct{}, value Value, fn func(QueryHash)) {
	set, ok := byVal[encodeValueKey(value)]
	if !ok {
		return
	}
	for h := range set {
		fn(h)
	}
}

func addRangeHash(byRange map[rangeKey]*rangeBucket, lower, upper Bound, hash QueryHash) bool {
	key := makeRangeKey(lower, upper)
	bucket, ok := byRange[key]
	if !ok {
		bucket = &rangeBucket{
			lower:  lower,
			upper:  upper,
			hashes: make(map[QueryHash]struct{}),
		}
		byRange[key] = bucket
	}
	if _, exists := bucket.hashes[hash]; exists {
		return false
	}
	bucket.hashes[hash] = struct{}{}
	return true
}

func removeRangeHash(byRange map[rangeKey]*rangeBucket, lower, upper Bound, hash QueryHash) bool {
	key := makeRangeKey(lower, upper)
	bucket, ok := byRange[key]
	if !ok {
		return false
	}
	if _, ok := bucket.hashes[hash]; !ok {
		return false
	}
	delete(bucket.hashes, hash)
	if len(bucket.hashes) == 0 {
		delete(byRange, key)
	}
	return true
}

func lookupRangeHashes(byRange map[rangeKey]*rangeBucket, value Value) []QueryHash {
	var out []QueryHash
	var seen map[QueryHash]struct{}
	forEachRangeHash(byRange, value, func(h QueryHash) {
		if seen == nil {
			seen = make(map[QueryHash]struct{})
		}
		if _, ok := seen[h]; ok {
			return
		}
		seen[h] = struct{}{}
		out = append(out, h)
	})
	if out == nil {
		return []QueryHash{}
	}
	return out
}

func forEachRangeHash(byRange map[rangeKey]*rangeBucket, value Value, fn func(QueryHash)) {
	for _, bucket := range byRange {
		if !matchBounds(value, bucket.lower, bucket.upper) {
			continue
		}
		for h := range bucket.hashes {
			fn(h)
		}
	}
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
