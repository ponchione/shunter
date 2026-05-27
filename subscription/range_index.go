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
	if !addRangeHash(byRange, lower, upper, hash) {
		return
	}
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
	if !removeRangeHash(byRange, lower, upper, hash) {
		return
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
	byCol, ok := r.ranges[table]
	if !ok {
		return []QueryHash{}
	}
	byRange, ok := byCol[col]
	if !ok {
		return []QueryHash{}
	}
	return lookupRangeHashes(byRange, value)
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
	forEachRangeHash(byRange, value, fn)
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
