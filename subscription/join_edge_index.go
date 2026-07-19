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

func (edge JoinEdge) lhsTable() TableID {
	return edge.LHSTable
}

// JoinEdgeIndex is the Tier 2 pruning index (SPEC-004 §5.2).
//
// Given a change on the LHS table plus an RHS filter value, returns the
// query hashes that could be affected.
type JoinEdgeIndex struct {
	*valueEdgeIndex[JoinEdge]
	// exists: JoinEdge → set of query hashes for joins that only need to know
	// whether an indexed RHS join-key match exists.
	exists map[JoinEdge]map[QueryHash]struct{}
}

// NewJoinEdgeIndex constructs an empty JoinEdgeIndex.
func NewJoinEdgeIndex() *JoinEdgeIndex {
	return &JoinEdgeIndex{
		valueEdgeIndex: newValueEdgeIndex[JoinEdge](),
		exists:         make(map[JoinEdge]map[QueryHash]struct{}),
	}
}

// Add registers (edge, filterValue) → hash.
func (ji *JoinEdgeIndex) Add(edge JoinEdge, filterValue Value, hash QueryHash) {
	ji.valueEdgeIndex.add(edge, filterValue, hash)
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
	ji.byTable.add(edge)
}

// Remove removes (edge, filterValue) → hash. Empty keys are cleaned up.
func (ji *JoinEdgeIndex) Remove(edge JoinEdge, filterValue Value, hash QueryHash) {
	ji.valueEdgeIndex.remove(edge, filterValue, hash)
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
	ji.byTable.remove(edge)
}

// Lookup returns query hashes for the given (edge, filter value).
// Returns an empty slice when nothing matches.
func (ji *JoinEdgeIndex) Lookup(edge JoinEdge, filterValue Value) []QueryHash {
	return ji.valueEdgeIndex.lookup(edge, filterValue)
}

// ForEachHash calls fn for every query hash registered for (edge, filterValue).
func (ji *JoinEdgeIndex) ForEachHash(edge JoinEdge, filterValue Value, fn func(QueryHash)) {
	ji.valueEdgeIndex.forEachHash(edge, filterValue, fn)
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
	return ji.byTable.edgesForTable(table)
}

// ForEachEdge calls fn for every join edge whose LHSTable matches table.
func (ji *JoinEdgeIndex) ForEachEdge(table TableID, fn func(JoinEdge)) {
	ji.valueEdgeIndex.forEachEdge(table, fn)
}
