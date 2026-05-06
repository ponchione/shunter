package subscription

// JoinPath3Edge identifies a three-hop directional join traversal used when a
// changed relation reaches a filtered relation through two intermediate
// relations.
type JoinPath3Edge struct {
	LHSTable      TableID
	Mid1Table     TableID
	Mid2Table     TableID
	RHSTable      TableID
	LHSJoinCol    ColID
	Mid1FirstCol  ColID
	Mid1SecondCol ColID
	Mid2FirstCol  ColID
	Mid2SecondCol ColID
	RHSJoinCol    ColID
	RHSFilterCol  ColID
}

// JoinPath3EdgeIndex is the value-filter pruning index for three-hop path
// edges.
type JoinPath3EdgeIndex struct {
	edges   map[JoinPath3Edge]map[valueKey]map[QueryHash]struct{}
	byTable map[TableID]map[JoinPath3Edge]int
}

// NewJoinPath3EdgeIndex constructs an empty JoinPath3EdgeIndex.
func NewJoinPath3EdgeIndex() *JoinPath3EdgeIndex {
	return &JoinPath3EdgeIndex{
		edges:   make(map[JoinPath3Edge]map[valueKey]map[QueryHash]struct{}),
		byTable: make(map[TableID]map[JoinPath3Edge]int),
	}
}

// Add registers (edge, filterValue) -> hash.
func (ji *JoinPath3EdgeIndex) Add(edge JoinPath3Edge, filterValue Value, hash QueryHash) {
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

func (ji *JoinPath3EdgeIndex) addEdgeRef(edge JoinPath3Edge) {
	perEdge, ok := ji.byTable[edge.LHSTable]
	if !ok {
		perEdge = make(map[JoinPath3Edge]int)
		ji.byTable[edge.LHSTable] = perEdge
	}
	perEdge[edge]++
}

// Remove removes (edge, filterValue) -> hash. Empty keys are cleaned up.
func (ji *JoinPath3EdgeIndex) Remove(edge JoinPath3Edge, filterValue Value, hash QueryHash) {
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

func (ji *JoinPath3EdgeIndex) removeEdgeRef(edge JoinPath3Edge) {
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
func (ji *JoinPath3EdgeIndex) Lookup(edge JoinPath3Edge, filterValue Value) []QueryHash {
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
func (ji *JoinPath3EdgeIndex) ForEachHash(edge JoinPath3Edge, filterValue Value, fn func(QueryHash)) {
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
func (ji *JoinPath3EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath3Edge)) {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return
	}
	for edge := range perEdge {
		fn(edge)
	}
}

// JoinRangePath3EdgeIndex is the range-filter companion to
// JoinPath3EdgeIndex.
type JoinRangePath3EdgeIndex struct {
	edges   map[JoinPath3Edge]map[rangeKey]*rangeBucket
	byTable map[TableID]map[JoinPath3Edge]int
}

// NewJoinRangePath3EdgeIndex constructs an empty JoinRangePath3EdgeIndex.
func NewJoinRangePath3EdgeIndex() *JoinRangePath3EdgeIndex {
	return &JoinRangePath3EdgeIndex{
		edges:   make(map[JoinPath3Edge]map[rangeKey]*rangeBucket),
		byTable: make(map[TableID]map[JoinPath3Edge]int),
	}
}

// Add registers (edge, range) -> hash.
func (ji *JoinRangePath3EdgeIndex) Add(edge JoinPath3Edge, lower, upper Bound, hash QueryHash) {
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
		perEdge = make(map[JoinPath3Edge]int)
		ji.byTable[edge.LHSTable] = perEdge
	}
	perEdge[edge]++
}

// Remove removes (edge, range) -> hash. Empty keys are cleaned up.
func (ji *JoinRangePath3EdgeIndex) Remove(edge JoinPath3Edge, lower, upper Bound, hash QueryHash) {
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
func (ji *JoinRangePath3EdgeIndex) Lookup(edge JoinPath3Edge, filterValue Value) []QueryHash {
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
func (ji *JoinRangePath3EdgeIndex) ForEachHash(edge JoinPath3Edge, filterValue Value, fn func(QueryHash)) {
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
func (ji *JoinRangePath3EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath3Edge)) {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return
	}
	for edge := range perEdge {
		fn(edge)
	}
}
