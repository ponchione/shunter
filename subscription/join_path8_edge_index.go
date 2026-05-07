package subscription

// JoinPath8Edge identifies an eight-hop directional join traversal used when a
// changed relation reaches a filtered relation through seven intermediate
// relations.
type JoinPath8Edge struct {
	LHSTable      TableID
	Mid1Table     TableID
	Mid2Table     TableID
	Mid3Table     TableID
	Mid4Table     TableID
	Mid5Table     TableID
	Mid6Table     TableID
	Mid7Table     TableID
	RHSTable      TableID
	LHSJoinCol    ColID
	Mid1FirstCol  ColID
	Mid1SecondCol ColID
	Mid2FirstCol  ColID
	Mid2SecondCol ColID
	Mid3FirstCol  ColID
	Mid3SecondCol ColID
	Mid4FirstCol  ColID
	Mid4SecondCol ColID
	Mid5FirstCol  ColID
	Mid5SecondCol ColID
	Mid6FirstCol  ColID
	Mid6SecondCol ColID
	Mid7FirstCol  ColID
	Mid7SecondCol ColID
	RHSJoinCol    ColID
	RHSFilterCol  ColID
}

// JoinPath8EdgeIndex is the value-filter pruning index for eight-hop path
// edges.
type JoinPath8EdgeIndex struct {
	edges   map[JoinPath8Edge]map[valueKey]map[QueryHash]struct{}
	byTable map[TableID]map[JoinPath8Edge]int
}

// NewJoinPath8EdgeIndex constructs an empty JoinPath8EdgeIndex.
func NewJoinPath8EdgeIndex() *JoinPath8EdgeIndex {
	return &JoinPath8EdgeIndex{
		edges:   make(map[JoinPath8Edge]map[valueKey]map[QueryHash]struct{}),
		byTable: make(map[TableID]map[JoinPath8Edge]int),
	}
}

// Add registers (edge, filterValue) -> hash.
func (ji *JoinPath8EdgeIndex) Add(edge JoinPath8Edge, filterValue Value, hash QueryHash) {
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

func (ji *JoinPath8EdgeIndex) addEdgeRef(edge JoinPath8Edge) {
	perEdge, ok := ji.byTable[edge.LHSTable]
	if !ok {
		perEdge = make(map[JoinPath8Edge]int)
		ji.byTable[edge.LHSTable] = perEdge
	}
	perEdge[edge]++
}

// Remove removes (edge, filterValue) -> hash. Empty keys are cleaned up.
func (ji *JoinPath8EdgeIndex) Remove(edge JoinPath8Edge, filterValue Value, hash QueryHash) {
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

func (ji *JoinPath8EdgeIndex) removeEdgeRef(edge JoinPath8Edge) {
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
func (ji *JoinPath8EdgeIndex) Lookup(edge JoinPath8Edge, filterValue Value) []QueryHash {
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
func (ji *JoinPath8EdgeIndex) ForEachHash(edge JoinPath8Edge, filterValue Value, fn func(QueryHash)) {
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
func (ji *JoinPath8EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath8Edge)) {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return
	}
	for edge := range perEdge {
		fn(edge)
	}
}

// JoinRangePath8EdgeIndex is the range-filter companion to
// JoinPath8EdgeIndex.
type JoinRangePath8EdgeIndex struct {
	edges   map[JoinPath8Edge]map[rangeKey]*rangeBucket
	byTable map[TableID]map[JoinPath8Edge]int
}

// NewJoinRangePath8EdgeIndex constructs an empty JoinRangePath8EdgeIndex.
func NewJoinRangePath8EdgeIndex() *JoinRangePath8EdgeIndex {
	return &JoinRangePath8EdgeIndex{
		edges:   make(map[JoinPath8Edge]map[rangeKey]*rangeBucket),
		byTable: make(map[TableID]map[JoinPath8Edge]int),
	}
}

// Add registers (edge, range) -> hash.
func (ji *JoinRangePath8EdgeIndex) Add(edge JoinPath8Edge, lower, upper Bound, hash QueryHash) {
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
		perEdge = make(map[JoinPath8Edge]int)
		ji.byTable[edge.LHSTable] = perEdge
	}
	perEdge[edge]++
}

// Remove removes (edge, range) -> hash. Empty keys are cleaned up.
func (ji *JoinRangePath8EdgeIndex) Remove(edge JoinPath8Edge, lower, upper Bound, hash QueryHash) {
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
func (ji *JoinRangePath8EdgeIndex) Lookup(edge JoinPath8Edge, filterValue Value) []QueryHash {
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
func (ji *JoinRangePath8EdgeIndex) ForEachHash(edge JoinPath8Edge, filterValue Value, fn func(QueryHash)) {
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
func (ji *JoinRangePath8EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath8Edge)) {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return
	}
	for edge := range perEdge {
		fn(edge)
	}
}
