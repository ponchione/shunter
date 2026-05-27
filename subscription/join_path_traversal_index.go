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
	edges   map[joinPathTraversalEdge]map[valueKey]map[QueryHash]struct{}
	byTable joinPathTraversalEdgeRefs
}

func newJoinPathTraversalIndex() *joinPathTraversalIndex {
	return &joinPathTraversalIndex{
		edges:   make(map[joinPathTraversalEdge]map[valueKey]map[QueryHash]struct{}),
		byTable: make(joinPathTraversalEdgeRefs),
	}
}

func (ji *joinPathTraversalIndex) Add(edge joinPathTraversalEdge, filterValue Value, hash QueryHash) {
	byVal, ok := ji.edges[edge]
	if !ok {
		byVal = make(map[valueKey]map[QueryHash]struct{})
		ji.edges[edge] = byVal
	}
	if !addValueHash(byVal, filterValue, hash) {
		return
	}
	ji.byTable.add(edge)
}

func (ji *joinPathTraversalIndex) Remove(edge joinPathTraversalEdge, filterValue Value, hash QueryHash) {
	byVal, ok := ji.edges[edge]
	if !ok {
		return
	}
	if !removeValueHash(byVal, filterValue, hash) {
		return
	}
	if len(byVal) == 0 {
		delete(ji.edges, edge)
	}
	ji.byTable.remove(edge)
}

func (ji *joinPathTraversalIndex) Lookup(edge joinPathTraversalEdge, filterValue Value) []QueryHash {
	byVal, ok := ji.edges[edge]
	if !ok {
		return []QueryHash{}
	}
	return lookupValueHashes(byVal, filterValue)
}

func (ji *joinPathTraversalIndex) ForEachHash(edge joinPathTraversalEdge, filterValue Value, fn func(QueryHash)) {
	byVal, ok := ji.edges[edge]
	if !ok {
		return
	}
	forEachValueHash(byVal, filterValue, fn)
}

func (ji *joinPathTraversalIndex) ForEachEdge(table TableID, fn func(joinPathTraversalEdge)) {
	ji.byTable.forEach(table, fn)
}

type joinRangePathTraversalIndex struct {
	edges   map[joinPathTraversalEdge]map[rangeKey]*rangeBucket
	byTable joinPathTraversalEdgeRefs
}

func newJoinRangePathTraversalIndex() *joinRangePathTraversalIndex {
	return &joinRangePathTraversalIndex{
		edges:   make(map[joinPathTraversalEdge]map[rangeKey]*rangeBucket),
		byTable: make(joinPathTraversalEdgeRefs),
	}
}

func (ji *joinRangePathTraversalIndex) Add(edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash) {
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

func (ji *joinRangePathTraversalIndex) Remove(edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash) {
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

func (ji *joinRangePathTraversalIndex) Lookup(edge joinPathTraversalEdge, filterValue Value) []QueryHash {
	byRange, ok := ji.edges[edge]
	if !ok {
		return []QueryHash{}
	}
	return lookupRangeHashes(byRange, filterValue)
}

func (ji *joinRangePathTraversalIndex) ForEachHash(edge joinPathTraversalEdge, filterValue Value, fn func(QueryHash)) {
	byRange, ok := ji.edges[edge]
	if !ok {
		return
	}
	forEachRangeHash(byRange, filterValue, fn)
}

func (ji *joinRangePathTraversalIndex) ForEachEdge(table TableID, fn func(joinPathTraversalEdge)) {
	ji.byTable.forEach(table, fn)
}

type joinPathTraversalEdgeRefs map[TableID]map[joinPathTraversalEdge]int

func (refs joinPathTraversalEdgeRefs) add(edge joinPathTraversalEdge) {
	table := edge.lhsTable()
	perEdge, ok := refs[table]
	if !ok {
		perEdge = make(map[joinPathTraversalEdge]int)
		refs[table] = perEdge
	}
	perEdge[edge]++
}

func (refs joinPathTraversalEdgeRefs) remove(edge joinPathTraversalEdge) {
	table := edge.lhsTable()
	if perEdge, ok := refs[table]; ok {
		perEdge[edge]--
		if perEdge[edge] <= 0 {
			delete(perEdge, edge)
		}
		if len(perEdge) == 0 {
			delete(refs, table)
		}
	}
}

func (refs joinPathTraversalEdgeRefs) forEach(table TableID, fn func(joinPathTraversalEdge)) {
	perEdge, ok := refs[table]
	if !ok {
		return
	}
	for edge := range perEdge {
		fn(edge)
	}
}
