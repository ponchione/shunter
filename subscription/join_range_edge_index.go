package subscription

// JoinRangeEdgeIndex is the range-filter companion to JoinEdgeIndex.
//
// Given a change on the LHS table plus an RHS range-filter value found through
// the committed join-column index, it returns query hashes that could be
// affected.
type JoinRangeEdgeIndex struct {
	edges   map[JoinEdge]map[rangeKey]*rangeBucket
	byTable map[TableID]map[JoinEdge]int
}

// NewJoinRangeEdgeIndex constructs an empty JoinRangeEdgeIndex.
func NewJoinRangeEdgeIndex() *JoinRangeEdgeIndex {
	return &JoinRangeEdgeIndex{
		edges:   make(map[JoinEdge]map[rangeKey]*rangeBucket),
		byTable: make(map[TableID]map[JoinEdge]int),
	}
}

// Add registers (edge, range) -> hash.
func (ji *JoinRangeEdgeIndex) Add(edge JoinEdge, lower, upper Bound, hash QueryHash) {
	byRange, ok := ji.edges[edge]
	if !ok {
		byRange = make(map[rangeKey]*rangeBucket)
		ji.edges[edge] = byRange
	}
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

	perEdge, ok := ji.byTable[edge.LHSTable]
	if !ok {
		perEdge = make(map[JoinEdge]int)
		ji.byTable[edge.LHSTable] = perEdge
	}
	perEdge[edge]++
}

// Remove removes (edge, range) -> hash. Empty keys are cleaned up.
func (ji *JoinRangeEdgeIndex) Remove(edge JoinEdge, lower, upper Bound, hash QueryHash) {
	byRange, ok := ji.edges[edge]
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
		delete(ji.edges, edge)
	}

	if perEdge, ok := ji.byTable[edge.LHSTable]; ok {
		perEdge[edge]--
		if perEdge[edge] <= 0 {
			delete(perEdge, edge)
		}
		if len(perEdge) == 0 {
			delete(ji.byTable, edge.LHSTable)
		}
	}
}

// Lookup returns query hashes for registered ranges containing filterValue.
func (ji *JoinRangeEdgeIndex) Lookup(edge JoinEdge, filterValue Value) []QueryHash {
	var out []QueryHash
	seen := make(map[QueryHash]struct{})
	ji.ForEachHash(edge, filterValue, func(h QueryHash) {
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
// filterValue.
func (ji *JoinRangeEdgeIndex) ForEachHash(edge JoinEdge, filterValue Value, fn func(QueryHash)) {
	byRange, ok := ji.edges[edge]
	if !ok {
		return
	}
	for _, bucket := range byRange {
		if !rangeContainsValue(filterValue, bucket.lower, bucket.upper) {
			continue
		}
		for h := range bucket.hashes {
			fn(h)
		}
	}
}

// EdgesForTable returns all range-filter edges where LHSTable matches.
func (ji *JoinRangeEdgeIndex) EdgesForTable(table TableID) []JoinEdge {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return []JoinEdge{}
	}
	return mapKeys(perEdge)
}

// ForEachEdge calls fn for every range-filter join edge whose LHSTable matches
// table.
func (ji *JoinRangeEdgeIndex) ForEachEdge(table TableID, fn func(JoinEdge)) {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return
	}
	for edge := range perEdge {
		fn(edge)
	}
}
