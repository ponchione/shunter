package subscription

// JoinPathEdge identifies a two-hop directional join traversal used when a
// changed relation reaches a filtered relation through a non-key-preserving
// intermediate relation.
type JoinPathEdge struct {
	LHSTable     TableID
	MidTable     TableID
	RHSTable     TableID
	LHSJoinCol   ColID
	MidFirstCol  ColID
	MidSecondCol ColID
	RHSJoinCol   ColID
	RHSFilterCol ColID
}

// JoinPathEdgeIndex is the value-filter pruning index for two-hop path edges.
type JoinPathEdgeIndex struct {
	edges   map[JoinPathEdge]map[valueKey]map[QueryHash]struct{}
	byTable map[TableID]map[JoinPathEdge]int
}

// NewJoinPathEdgeIndex constructs an empty JoinPathEdgeIndex.
func NewJoinPathEdgeIndex() *JoinPathEdgeIndex {
	return &JoinPathEdgeIndex{
		edges:   make(map[JoinPathEdge]map[valueKey]map[QueryHash]struct{}),
		byTable: make(map[TableID]map[JoinPathEdge]int),
	}
}

// Add registers (edge, filterValue) -> hash.
func (ji *JoinPathEdgeIndex) Add(edge JoinPathEdge, filterValue Value, hash QueryHash) {
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
	ji.addEdgeRef(edge)
}

func (ji *JoinPathEdgeIndex) addEdgeRef(edge JoinPathEdge) {
	perEdge, ok := ji.byTable[edge.LHSTable]
	if !ok {
		perEdge = make(map[JoinPathEdge]int)
		ji.byTable[edge.LHSTable] = perEdge
	}
	perEdge[edge]++
}

// Remove removes (edge, filterValue) -> hash. Empty keys are cleaned up.
func (ji *JoinPathEdgeIndex) Remove(edge JoinPathEdge, filterValue Value, hash QueryHash) {
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
	ji.removeEdgeRef(edge)
}

func (ji *JoinPathEdgeIndex) removeEdgeRef(edge JoinPathEdge) {
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

// Lookup returns query hashes for the given (edge, filter value).
func (ji *JoinPathEdgeIndex) Lookup(edge JoinPathEdge, filterValue Value) []QueryHash {
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

// ForEachHash calls fn for every query hash registered for (edge, filterValue).
func (ji *JoinPathEdgeIndex) ForEachHash(edge JoinPathEdge, filterValue Value, fn func(QueryHash)) {
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

// ForEachEdge calls fn for every path edge whose LHSTable matches table.
func (ji *JoinPathEdgeIndex) ForEachEdge(table TableID, fn func(JoinPathEdge)) {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return
	}
	for edge := range perEdge {
		fn(edge)
	}
}

// JoinRangePathEdgeIndex is the range-filter companion to JoinPathEdgeIndex.
type JoinRangePathEdgeIndex struct {
	edges   map[JoinPathEdge]map[rangeKey]*rangeBucket
	byTable map[TableID]map[JoinPathEdge]int
}

// NewJoinRangePathEdgeIndex constructs an empty JoinRangePathEdgeIndex.
func NewJoinRangePathEdgeIndex() *JoinRangePathEdgeIndex {
	return &JoinRangePathEdgeIndex{
		edges:   make(map[JoinPathEdge]map[rangeKey]*rangeBucket),
		byTable: make(map[TableID]map[JoinPathEdge]int),
	}
}

// Add registers (edge, range) -> hash.
func (ji *JoinRangePathEdgeIndex) Add(edge JoinPathEdge, lower, upper Bound, hash QueryHash) {
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
		perEdge = make(map[JoinPathEdge]int)
		ji.byTable[edge.LHSTable] = perEdge
	}
	perEdge[edge]++
}

// Remove removes (edge, range) -> hash. Empty keys are cleaned up.
func (ji *JoinRangePathEdgeIndex) Remove(edge JoinPathEdge, lower, upper Bound, hash QueryHash) {
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
func (ji *JoinRangePathEdgeIndex) Lookup(edge JoinPathEdge, filterValue Value) []QueryHash {
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
func (ji *JoinRangePathEdgeIndex) ForEachHash(edge JoinPathEdge, filterValue Value, fn func(QueryHash)) {
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

// ForEachEdge calls fn for every range path edge whose LHSTable matches table.
func (ji *JoinRangePathEdgeIndex) ForEachEdge(table TableID, fn func(JoinPathEdge)) {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return
	}
	for edge := range perEdge {
		fn(edge)
	}
}
