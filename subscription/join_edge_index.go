package subscription

// JoinEdge identifies a directional join traversal used by Tier 2.
// Entries are asymmetric: a join subscription can register separate edges
// for LHS changes and RHS changes of the same underlying join.
type JoinEdge struct {
	LHSTable     TableID
	RHSTable     TableID
	LHSJoinCol   ColID
	RHSJoinCol   ColID
	RHSFilterCol ColID
}

// JoinEdgeIndex is the Tier 2 pruning index (SPEC-004 §5.2).
//
// Given a change on the LHS table plus an RHS filter value, returns the
// query hashes that could be affected.
type JoinEdgeIndex struct {
	// edges: JoinEdge → encoded(filter value) → set of query hashes.
	edges map[JoinEdge]map[valueKey]map[QueryHash]struct{}
	// exists: JoinEdge → set of query hashes for joins that only need to know
	// whether an indexed RHS join-key match exists.
	exists map[JoinEdge]map[QueryHash]struct{}
	// byTable tracks edges per LHS table for EdgesForTable iteration.
	byTable map[TableID]map[JoinEdge]int
}

// NewJoinEdgeIndex constructs an empty JoinEdgeIndex.
func NewJoinEdgeIndex() *JoinEdgeIndex {
	return &JoinEdgeIndex{
		edges:   make(map[JoinEdge]map[valueKey]map[QueryHash]struct{}),
		exists:  make(map[JoinEdge]map[QueryHash]struct{}),
		byTable: make(map[TableID]map[JoinEdge]int),
	}
}

// Add registers (edge, filterValue) → hash.
func (ji *JoinEdgeIndex) Add(edge JoinEdge, filterValue Value, hash QueryHash) {
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

// AddExistence registers edge-level existence pruning for hash.
func (ji *JoinEdgeIndex) AddExistence(edge JoinEdge, hash QueryHash) {
	set, ok := ji.exists[edge]
	if !ok {
		set = make(map[QueryHash]struct{})
		ji.exists[edge] = set
	}
	if _, exists := set[hash]; exists {
		return
	}
	set[hash] = struct{}{}
	ji.addEdgeRef(edge)
}

func (ji *JoinEdgeIndex) addEdgeRef(edge JoinEdge) {
	perEdge, ok := ji.byTable[edge.LHSTable]
	if !ok {
		perEdge = make(map[JoinEdge]int)
		ji.byTable[edge.LHSTable] = perEdge
	}
	perEdge[edge]++
}

// Remove removes (edge, filterValue) → hash. Empty keys are cleaned up.
func (ji *JoinEdgeIndex) Remove(edge JoinEdge, filterValue Value, hash QueryHash) {
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

// RemoveExistence removes edge-level existence pruning for hash.
func (ji *JoinEdgeIndex) RemoveExistence(edge JoinEdge, hash QueryHash) {
	set, ok := ji.exists[edge]
	if !ok {
		return
	}
	if _, ok := set[hash]; !ok {
		return
	}
	delete(set, hash)
	if len(set) == 0 {
		delete(ji.exists, edge)
	}
	ji.removeEdgeRef(edge)
}

func (ji *JoinEdgeIndex) removeEdgeRef(edge JoinEdge) {
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
// Returns an empty slice when nothing matches.
func (ji *JoinEdgeIndex) Lookup(edge JoinEdge, filterValue Value) []QueryHash {
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
func (ji *JoinEdgeIndex) ForEachHash(edge JoinEdge, filterValue Value, fn func(QueryHash)) {
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

// ForEachExistenceHash calls fn for every query hash registered for edge-level
// existence pruning.
func (ji *JoinEdgeIndex) ForEachExistenceHash(edge JoinEdge, fn func(QueryHash)) {
	set, ok := ji.exists[edge]
	if !ok {
		return
	}
	for h := range set {
		fn(h)
	}
}

// EdgesForTable returns all edges where LHSTable matches.
func (ji *JoinEdgeIndex) EdgesForTable(table TableID) []JoinEdge {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return []JoinEdge{}
	}
	return mapKeys(perEdge)
}

// ForEachEdge calls fn for every join edge whose LHSTable matches table.
func (ji *JoinEdgeIndex) ForEachEdge(table TableID, fn func(JoinEdge)) {
	perEdge, ok := ji.byTable[table]
	if !ok {
		return
	}
	for edge := range perEdge {
		fn(edge)
	}
}
