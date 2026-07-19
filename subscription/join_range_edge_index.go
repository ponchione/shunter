package subscription

// JoinRangeEdgeIndex is the range-filter companion to JoinEdgeIndex.
//
// Given a change on the LHS table plus an RHS range-filter value found through
// the committed join-column index, it returns query hashes that could be
// affected.
type JoinRangeEdgeIndex struct {
	*rangeEdgeIndex[JoinEdge]
}

// NewJoinRangeEdgeIndex constructs an empty JoinRangeEdgeIndex.
func NewJoinRangeEdgeIndex() *JoinRangeEdgeIndex {
	return &JoinRangeEdgeIndex{
		rangeEdgeIndex: newRangeEdgeIndex[JoinEdge](),
	}
}

// Add registers (edge, range) -> hash.
func (ji *JoinRangeEdgeIndex) Add(edge JoinEdge, lower, upper Bound, hash QueryHash) {
	ji.rangeEdgeIndex.add(edge, lower, upper, hash)
}

// Remove removes (edge, range) -> hash. Empty keys are cleaned up.
func (ji *JoinRangeEdgeIndex) Remove(edge JoinEdge, lower, upper Bound, hash QueryHash) {
	ji.rangeEdgeIndex.remove(edge, lower, upper, hash)
}

// Lookup returns query hashes for registered ranges containing filterValue.
func (ji *JoinRangeEdgeIndex) Lookup(edge JoinEdge, filterValue Value) []QueryHash {
	return ji.rangeEdgeIndex.lookup(edge, filterValue)
}

// ForEachHash calls fn for every query hash registered for a range containing
// filterValue.
func (ji *JoinRangeEdgeIndex) ForEachHash(edge JoinEdge, filterValue Value, fn func(QueryHash)) {
	ji.rangeEdgeIndex.forEachHash(edge, filterValue, fn)
}

// EdgesForTable returns all range-filter edges where LHSTable matches.
func (ji *JoinRangeEdgeIndex) EdgesForTable(table TableID) []JoinEdge {
	return ji.byTable.edgesForTable(table)
}

// ForEachEdge calls fn for every range-filter join edge whose LHSTable matches
// table.
func (ji *JoinRangeEdgeIndex) ForEachEdge(table TableID, fn func(JoinEdge)) {
	ji.rangeEdgeIndex.forEachEdge(table, fn)
}
