package subscription

type edgeKey interface {
	comparable
	lhsTable() TableID
}

type edgeRefs[E edgeKey] map[TableID]map[E]int

func newEdgeRefs[E edgeKey]() edgeRefs[E] {
	return make(edgeRefs[E])
}

func (refs edgeRefs[E]) add(edge E) {
	table := edge.lhsTable()
	perEdge, ok := refs[table]
	if !ok {
		perEdge = make(map[E]int)
		refs[table] = perEdge
	}
	perEdge[edge]++
}

func (refs edgeRefs[E]) remove(edge E) {
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

func (refs edgeRefs[E]) edgesForTable(table TableID) []E {
	perEdge, ok := refs[table]
	if !ok {
		return []E{}
	}
	return mapKeys(perEdge)
}

func (refs edgeRefs[E]) forEach(table TableID, fn func(E)) {
	for edge := range refs[table] {
		fn(edge)
	}
}

type valueEdgeIndex[E edgeKey] struct {
	edges   map[E]map[valueKey]map[QueryHash]struct{}
	byTable edgeRefs[E]
}

func newValueEdgeIndex[E edgeKey]() *valueEdgeIndex[E] {
	return &valueEdgeIndex[E]{
		edges:   make(map[E]map[valueKey]map[QueryHash]struct{}),
		byTable: newEdgeRefs[E](),
	}
}

func (index *valueEdgeIndex[E]) add(edge E, filterValue Value, hash QueryHash) {
	byValue, ok := index.edges[edge]
	if !ok {
		byValue = make(map[valueKey]map[QueryHash]struct{})
		index.edges[edge] = byValue
	}
	if addValueHash(byValue, filterValue, hash) {
		index.byTable.add(edge)
	}
}

func (index *valueEdgeIndex[E]) remove(edge E, filterValue Value, hash QueryHash) {
	byValue, ok := index.edges[edge]
	if !ok || !removeValueHash(byValue, filterValue, hash) {
		return
	}
	if len(byValue) == 0 {
		delete(index.edges, edge)
	}
	index.byTable.remove(edge)
}

func (index *valueEdgeIndex[E]) lookup(edge E, filterValue Value) []QueryHash {
	byValue, ok := index.edges[edge]
	if !ok {
		return []QueryHash{}
	}
	return lookupValueHashes(byValue, filterValue)
}

func (index *valueEdgeIndex[E]) forEachHash(edge E, filterValue Value, fn func(QueryHash)) {
	if byValue, ok := index.edges[edge]; ok {
		forEachValueHash(byValue, filterValue, fn)
	}
}

func (index *valueEdgeIndex[E]) forEachEdge(table TableID, fn func(E)) {
	index.byTable.forEach(table, fn)
}

type rangeEdgeIndex[E edgeKey] struct {
	edges   map[E]map[rangeKey]*rangeBucket
	byTable edgeRefs[E]
}

func newRangeEdgeIndex[E edgeKey]() *rangeEdgeIndex[E] {
	return &rangeEdgeIndex[E]{
		edges:   make(map[E]map[rangeKey]*rangeBucket),
		byTable: newEdgeRefs[E](),
	}
}

func (index *rangeEdgeIndex[E]) add(edge E, lower, upper Bound, hash QueryHash) {
	byRange, ok := index.edges[edge]
	if !ok {
		byRange = make(map[rangeKey]*rangeBucket)
		index.edges[edge] = byRange
	}
	if addRangeHash(byRange, lower, upper, hash) {
		index.byTable.add(edge)
	}
}

func (index *rangeEdgeIndex[E]) remove(edge E, lower, upper Bound, hash QueryHash) {
	byRange, ok := index.edges[edge]
	if !ok || !removeRangeHash(byRange, lower, upper, hash) {
		return
	}
	if len(byRange) == 0 {
		delete(index.edges, edge)
	}
	index.byTable.remove(edge)
}

func (index *rangeEdgeIndex[E]) lookup(edge E, filterValue Value) []QueryHash {
	byRange, ok := index.edges[edge]
	if !ok {
		return []QueryHash{}
	}
	return lookupRangeHashes(byRange, filterValue)
}

func (index *rangeEdgeIndex[E]) forEachHash(edge E, filterValue Value, fn func(QueryHash)) {
	if byRange, ok := index.edges[edge]; ok {
		forEachRangeHash(byRange, filterValue, fn)
	}
}

func (index *rangeEdgeIndex[E]) forEachEdge(table TableID, fn func(E)) {
	index.byTable.forEach(table, fn)
}
