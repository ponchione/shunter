package subscription

// RangeIndex maps (table, column, range) to candidate query hashes.
// Candidate collection probes changed row values against registered ranges,
// then full predicate evaluation rechecks the row before fan-out.
type RangeIndex struct {
	cols   columnRefCounts
	ranges map[TableID]map[ColID]map[rangeKey]*rangeBucket
}

type rangeBucket struct {
	lower  Bound
	upper  Bound
	hashes map[QueryHash]struct{}
}

type rangeKey struct {
	lowerUnbounded bool
	lowerInclusive bool
	lower          valueKey
	upperUnbounded bool
	upperInclusive bool
	upper          valueKey
}

// NewRangeIndex constructs an empty RangeIndex.
func NewRangeIndex() *RangeIndex {
	return &RangeIndex{
		cols:   newColumnRefCounts(),
		ranges: make(map[TableID]map[ColID]map[rangeKey]*rangeBucket),
	}
}

func (r *RangeIndex) bucketMap(t TableID, c ColID) map[rangeKey]*rangeBucket {
	byCol, ok := r.ranges[t]
	if !ok {
		byCol = make(map[ColID]map[rangeKey]*rangeBucket)
		r.ranges[t] = byCol
	}
	byRange, ok := byCol[c]
	if !ok {
		byRange = make(map[rangeKey]*rangeBucket)
		byCol[c] = byRange
	}
	return byRange
}

// Add registers a (table, column, range) -> hash mapping.
func (r *RangeIndex) Add(table TableID, col ColID, lower, upper Bound, hash QueryHash) {
	byRange := r.bucketMap(table, col)
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
		return
	}
	bucket.hashes[hash] = struct{}{}
	r.cols.add(table, col)
}

// Remove removes a (table, column, range) -> hash mapping.
func (r *RangeIndex) Remove(table TableID, col ColID, lower, upper Bound, hash QueryHash) {
	byCol, ok := r.ranges[table]
	if !ok {
		return
	}
	byRange, ok := byCol[col]
	if !ok {
		return
	}
	key := makeRangeKey(lower, upper)
	bucket, ok := byRange[key]
	if !ok {
		return
	}
	if _, ok := bucket.hashes[hash]; !ok {
		return
	}
	delete(bucket.hashes, hash)
	if len(bucket.hashes) == 0 {
		delete(byRange, key)
	}
	if len(byRange) == 0 {
		delete(byCol, col)
	}
	if len(byCol) == 0 {
		delete(r.ranges, table)
	}

	r.cols.remove(table, col)
}

// Lookup returns query hashes whose registered range contains value.
func (r *RangeIndex) Lookup(table TableID, col ColID, value Value) []QueryHash {
	var out []QueryHash
	seen := make(map[QueryHash]struct{})
	r.ForEachHash(table, col, value, func(h QueryHash) {
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

// ForEachHash calls fn for every query hash registered for a range containing
// value.
func (r *RangeIndex) ForEachHash(table TableID, col ColID, value Value, fn func(QueryHash)) {
	byCol, ok := r.ranges[table]
	if !ok {
		return
	}
	byRange, ok := byCol[col]
	if !ok {
		return
	}
	for _, bucket := range byRange {
		if !rangeContainsValue(value, bucket.lower, bucket.upper) {
			continue
		}
		for h := range bucket.hashes {
			fn(h)
		}
	}
}

// TrackedColumns returns columns with at least one registered range.
func (r *RangeIndex) TrackedColumns(table TableID) []ColID {
	return r.cols.trackedColumns(table)
}

// ForEachTrackedColumn calls fn for every range-tracked column on table.
func (r *RangeIndex) ForEachTrackedColumn(table TableID, fn func(ColID)) {
	r.cols.forEachTrackedColumn(table, fn)
}

func makeRangeKey(lower, upper Bound) rangeKey {
	k := rangeKey{
		lowerUnbounded: lower.Unbounded,
		lowerInclusive: lower.Inclusive,
		upperUnbounded: upper.Unbounded,
		upperInclusive: upper.Inclusive,
	}
	if !lower.Unbounded {
		k.lower = encodeValueKey(lower.Value)
	}
	if !upper.Unbounded {
		k.upper = encodeValueKey(upper.Value)
	}
	return k
}

func rangeContainsValue(v Value, lower, upper Bound) bool {
	if !lower.Unbounded && lower.Value.Kind() != v.Kind() {
		return false
	}
	if !upper.Unbounded && upper.Value.Kind() != v.Kind() {
		return false
	}
	return matchBounds(v, lower, upper)
}
