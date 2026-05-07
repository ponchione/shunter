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
	key := encodeValueKey(filterValue)
	set, ok := byVal[key]
	if !ok {
		set = make(map[QueryHash]struct{})
		byVal[key] = set
	}
	if _, exists := set[hash]; exists {
		return
	}
	set[hash] = struct{}{}
	ji.byTable.add(edge)
}

func (ji *joinPathTraversalIndex) Remove(edge joinPathTraversalEdge, filterValue Value, hash QueryHash) {
	byVal, ok := ji.edges[edge]
	if !ok {
		return
	}
	key := encodeValueKey(filterValue)
	set, ok := byVal[key]
	if !ok {
		return
	}
	if _, ok := set[hash]; !ok {
		return
	}
	delete(set, hash)
	if len(set) == 0 {
		delete(byVal, key)
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
	set, ok := byVal[encodeValueKey(filterValue)]
	if !ok {
		return []QueryHash{}
	}
	return mapKeys(set)
}

func (ji *joinPathTraversalIndex) ForEachHash(edge joinPathTraversalEdge, filterValue Value, fn func(QueryHash)) {
	byVal, ok := ji.edges[edge]
	if !ok {
		return
	}
	set, ok := byVal[encodeValueKey(filterValue)]
	if !ok {
		return
	}
	for h := range set {
		fn(h)
	}
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
	ji.byTable.add(edge)
}

func (ji *joinRangePathTraversalIndex) Remove(edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash) {
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
	ji.byTable.remove(edge)
}

func (ji *joinRangePathTraversalIndex) Lookup(edge joinPathTraversalEdge, filterValue Value) []QueryHash {
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

func (ji *joinRangePathTraversalIndex) ForEachHash(edge joinPathTraversalEdge, filterValue Value, fn func(QueryHash)) {
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
