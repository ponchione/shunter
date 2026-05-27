package subscription

// JoinRangeEdgeIndex is the range-filter companion to JoinEdgeIndex.
//
// Given a change on the LHS table plus an RHS range-filter value found through
// the committed join-column index, it returns query hashes that could be
// affected.
type JoinRangeEdgeIndex struct {
	edges   map[JoinEdge]map[rangeKey]*rangeBucket
	byTable joinEdgeRefs
}

// NewJoinRangeEdgeIndex constructs an empty JoinRangeEdgeIndex.
func NewJoinRangeEdgeIndex() *JoinRangeEdgeIndex {
	return &JoinRangeEdgeIndex{
		edges:   make(map[JoinEdge]map[rangeKey]*rangeBucket),
		byTable: newJoinEdgeRefs(),
	}
}

// Add registers (edge, range) -> hash.
func (ji *JoinRangeEdgeIndex) Add(edge JoinEdge, lower, upper Bound, hash QueryHash) {
	byRange, ok := ji.edges[edge]
	if !ok {
		byRange = make(map[rangeKey]*rangeBucket)
		ji.edges[edge] = byRange
	}
	if !addRangeHash(byRange, lower, upper, hash) {
		return
	}
	ji.byTable.add(edge)
}

// Remove removes (edge, range) -> hash. Empty keys are cleaned up.
func (ji *JoinRangeEdgeIndex) Remove(edge JoinEdge, lower, upper Bound, hash QueryHash) {
	byRange, ok := ji.edges[edge]
	if !ok {
		return
	}
	if !removeRangeHash(byRange, lower, upper, hash) {
		return
	}
	if len(byRange) == 0 {
		delete(ji.edges, edge)
	}
	ji.byTable.remove(edge)
}

// Lookup returns query hashes for registered ranges containing filterValue.
func (ji *JoinRangeEdgeIndex) Lookup(edge JoinEdge, filterValue Value) []QueryHash {
	byRange, ok := ji.edges[edge]
	if !ok {
		return []QueryHash{}
	}
	return lookupRangeHashes(byRange, filterValue)
}

// ForEachHash calls fn for every query hash registered for a range containing
// filterValue.
func (ji *JoinRangeEdgeIndex) ForEachHash(edge JoinEdge, filterValue Value, fn func(QueryHash)) {
	byRange, ok := ji.edges[edge]
	if !ok {
		return
	}
	forEachRangeHash(byRange, filterValue, fn)
}

// EdgesForTable returns all range-filter edges where LHSTable matches.
func (ji *JoinRangeEdgeIndex) EdgesForTable(table TableID) []JoinEdge {
	return ji.byTable.edgesForTable(table)
}

// ForEachEdge calls fn for every range-filter join edge whose LHSTable matches
// table.
func (ji *JoinRangeEdgeIndex) ForEachEdge(table TableID, fn func(JoinEdge)) {
	ji.byTable.forEach(table, fn)
}
