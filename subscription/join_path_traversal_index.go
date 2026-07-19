package subscription

const joinPathTraversalMaxHops = 16

type joinPathTraversalEdge struct {
	hops         uint8
	tables       [joinPathTraversalMaxHops + 1]TableID
	fromCols     [joinPathTraversalMaxHops]ColID
	toCols       [joinPathTraversalMaxHops]ColID
	rhsFilterCol ColID
}

func newJoinPathTraversalEdge(tables []TableID, fromCols, toCols []ColID, rhsFilterCol ColID) (joinPathTraversalEdge, bool) {
	hops := len(fromCols)
	if hops < 2 || hops > joinPathTraversalMaxHops || len(toCols) != hops || len(tables) != hops+1 {
		return joinPathTraversalEdge{}, false
	}
	var edge joinPathTraversalEdge
	edge.hops = uint8(hops)
	edge.rhsFilterCol = rhsFilterCol
	copy(edge.tables[:], tables)
	copy(edge.fromCols[:], fromCols)
	copy(edge.toCols[:], toCols)
	return edge, true
}

func (edge joinPathTraversalEdge) hopCount() int {
	return int(edge.hops)
}

func (edge joinPathTraversalEdge) lhsTable() TableID {
	return edge.tables[0]
}

func (edge joinPathTraversalEdge) rhsTable() TableID {
	return edge.tables[edge.hopCount()]
}

type joinPathTraversalIndex struct {
	*valueEdgeIndex[joinPathTraversalEdge]
}

func newJoinPathTraversalIndex() *joinPathTraversalIndex {
	return &joinPathTraversalIndex{
		valueEdgeIndex: newValueEdgeIndex[joinPathTraversalEdge](),
	}
}

func (ji *joinPathTraversalIndex) Add(edge joinPathTraversalEdge, filterValue Value, hash QueryHash) {
	ji.valueEdgeIndex.add(edge, filterValue, hash)
}

func (ji *joinPathTraversalIndex) Remove(edge joinPathTraversalEdge, filterValue Value, hash QueryHash) {
	ji.valueEdgeIndex.remove(edge, filterValue, hash)
}

func (ji *joinPathTraversalIndex) Lookup(edge joinPathTraversalEdge, filterValue Value) []QueryHash {
	return ji.valueEdgeIndex.lookup(edge, filterValue)
}

func (ji *joinPathTraversalIndex) ForEachHash(edge joinPathTraversalEdge, filterValue Value, fn func(QueryHash)) {
	ji.valueEdgeIndex.forEachHash(edge, filterValue, fn)
}

func (ji *joinPathTraversalIndex) ForEachEdge(table TableID, fn func(joinPathTraversalEdge)) {
	ji.valueEdgeIndex.forEachEdge(table, fn)
}

type joinRangePathTraversalIndex struct {
	*rangeEdgeIndex[joinPathTraversalEdge]
}

func newJoinRangePathTraversalIndex() *joinRangePathTraversalIndex {
	return &joinRangePathTraversalIndex{
		rangeEdgeIndex: newRangeEdgeIndex[joinPathTraversalEdge](),
	}
}

func (ji *joinRangePathTraversalIndex) Add(edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash) {
	ji.rangeEdgeIndex.add(edge, lower, upper, hash)
}

func (ji *joinRangePathTraversalIndex) Remove(edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash) {
	ji.rangeEdgeIndex.remove(edge, lower, upper, hash)
}

func (ji *joinRangePathTraversalIndex) Lookup(edge joinPathTraversalEdge, filterValue Value) []QueryHash {
	return ji.rangeEdgeIndex.lookup(edge, filterValue)
}

func (ji *joinRangePathTraversalIndex) ForEachHash(edge joinPathTraversalEdge, filterValue Value, fn func(QueryHash)) {
	ji.rangeEdgeIndex.forEachHash(edge, filterValue, fn)
}

func (ji *joinRangePathTraversalIndex) ForEachEdge(table TableID, fn func(joinPathTraversalEdge)) {
	ji.rangeEdgeIndex.forEachEdge(table, fn)
}
