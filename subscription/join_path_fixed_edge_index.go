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

// JoinPath3Edge identifies a three-hop directional join traversal.
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

// JoinPath4Edge identifies a four-hop directional join traversal.
type JoinPath4Edge struct {
	LHSTable      TableID
	Mid1Table     TableID
	Mid2Table     TableID
	Mid3Table     TableID
	RHSTable      TableID
	LHSJoinCol    ColID
	Mid1FirstCol  ColID
	Mid1SecondCol ColID
	Mid2FirstCol  ColID
	Mid2SecondCol ColID
	Mid3FirstCol  ColID
	Mid3SecondCol ColID
	RHSJoinCol    ColID
	RHSFilterCol  ColID
}

// JoinPath5Edge identifies a five-hop directional join traversal.
type JoinPath5Edge struct {
	LHSTable      TableID
	Mid1Table     TableID
	Mid2Table     TableID
	Mid3Table     TableID
	Mid4Table     TableID
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
	RHSJoinCol    ColID
	RHSFilterCol  ColID
}

// JoinPath6Edge identifies a six-hop directional join traversal.
type JoinPath6Edge struct {
	LHSTable      TableID
	Mid1Table     TableID
	Mid2Table     TableID
	Mid3Table     TableID
	Mid4Table     TableID
	Mid5Table     TableID
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
	RHSJoinCol    ColID
	RHSFilterCol  ColID
}

// JoinPath7Edge identifies a seven-hop directional join traversal.
type JoinPath7Edge struct {
	LHSTable      TableID
	Mid1Table     TableID
	Mid2Table     TableID
	Mid3Table     TableID
	Mid4Table     TableID
	Mid5Table     TableID
	Mid6Table     TableID
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
	RHSJoinCol    ColID
	RHSFilterCol  ColID
}

// JoinPath8Edge identifies an eight-hop directional join traversal.
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

type JoinPathEdgeIndex struct {
	inner *joinPathTraversalIndex
}

func NewJoinPathEdgeIndex() *JoinPathEdgeIndex {
	return &JoinPathEdgeIndex{inner: newJoinPathTraversalIndex()}
}

func (ji *JoinPathEdgeIndex) Add(edge JoinPathEdge, filterValue Value, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPathEdge(edge), filterValue, hash)
}

func (ji *JoinPathEdgeIndex) Remove(edge JoinPathEdge, filterValue Value, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPathEdge(edge), filterValue, hash)
}

func (ji *JoinPathEdgeIndex) Lookup(edge JoinPathEdge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPathEdge(edge), filterValue)
}

func (ji *JoinPathEdgeIndex) ForEachHash(edge JoinPathEdge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPathEdge(edge), filterValue, fn)
}

func (ji *JoinPathEdgeIndex) ForEachEdge(table TableID, fn func(JoinPathEdge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPathEdge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinRangePathEdgeIndex struct {
	inner *joinRangePathTraversalIndex
}

func NewJoinRangePathEdgeIndex() *JoinRangePathEdgeIndex {
	return &JoinRangePathEdgeIndex{inner: newJoinRangePathTraversalIndex()}
}

func (ji *JoinRangePathEdgeIndex) Add(edge JoinPathEdge, lower, upper Bound, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPathEdge(edge), lower, upper, hash)
}

func (ji *JoinRangePathEdgeIndex) Remove(edge JoinPathEdge, lower, upper Bound, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPathEdge(edge), lower, upper, hash)
}

func (ji *JoinRangePathEdgeIndex) Lookup(edge JoinPathEdge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPathEdge(edge), filterValue)
}

func (ji *JoinRangePathEdgeIndex) ForEachHash(edge JoinPathEdge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPathEdge(edge), filterValue, fn)
}

func (ji *JoinRangePathEdgeIndex) ForEachEdge(table TableID, fn func(JoinPathEdge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPathEdge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinPath3EdgeIndex struct {
	inner *joinPathTraversalIndex
}

func NewJoinPath3EdgeIndex() *JoinPath3EdgeIndex {
	return &JoinPath3EdgeIndex{inner: newJoinPathTraversalIndex()}
}

func (ji *JoinPath3EdgeIndex) Add(edge JoinPath3Edge, filterValue Value, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath3Edge(edge), filterValue, hash)
}

func (ji *JoinPath3EdgeIndex) Remove(edge JoinPath3Edge, filterValue Value, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath3Edge(edge), filterValue, hash)
}

func (ji *JoinPath3EdgeIndex) Lookup(edge JoinPath3Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath3Edge(edge), filterValue)
}

func (ji *JoinPath3EdgeIndex) ForEachHash(edge JoinPath3Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath3Edge(edge), filterValue, fn)
}

func (ji *JoinPath3EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath3Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath3Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinRangePath3EdgeIndex struct {
	inner *joinRangePathTraversalIndex
}

func NewJoinRangePath3EdgeIndex() *JoinRangePath3EdgeIndex {
	return &JoinRangePath3EdgeIndex{inner: newJoinRangePathTraversalIndex()}
}

func (ji *JoinRangePath3EdgeIndex) Add(edge JoinPath3Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath3Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath3EdgeIndex) Remove(edge JoinPath3Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath3Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath3EdgeIndex) Lookup(edge JoinPath3Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath3Edge(edge), filterValue)
}

func (ji *JoinRangePath3EdgeIndex) ForEachHash(edge JoinPath3Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath3Edge(edge), filterValue, fn)
}

func (ji *JoinRangePath3EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath3Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath3Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinPath4EdgeIndex struct {
	inner *joinPathTraversalIndex
}

func NewJoinPath4EdgeIndex() *JoinPath4EdgeIndex {
	return &JoinPath4EdgeIndex{inner: newJoinPathTraversalIndex()}
}

func (ji *JoinPath4EdgeIndex) Add(edge JoinPath4Edge, filterValue Value, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath4Edge(edge), filterValue, hash)
}

func (ji *JoinPath4EdgeIndex) Remove(edge JoinPath4Edge, filterValue Value, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath4Edge(edge), filterValue, hash)
}

func (ji *JoinPath4EdgeIndex) Lookup(edge JoinPath4Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath4Edge(edge), filterValue)
}

func (ji *JoinPath4EdgeIndex) ForEachHash(edge JoinPath4Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath4Edge(edge), filterValue, fn)
}

func (ji *JoinPath4EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath4Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath4Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinRangePath4EdgeIndex struct {
	inner *joinRangePathTraversalIndex
}

func NewJoinRangePath4EdgeIndex() *JoinRangePath4EdgeIndex {
	return &JoinRangePath4EdgeIndex{inner: newJoinRangePathTraversalIndex()}
}

func (ji *JoinRangePath4EdgeIndex) Add(edge JoinPath4Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath4Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath4EdgeIndex) Remove(edge JoinPath4Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath4Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath4EdgeIndex) Lookup(edge JoinPath4Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath4Edge(edge), filterValue)
}

func (ji *JoinRangePath4EdgeIndex) ForEachHash(edge JoinPath4Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath4Edge(edge), filterValue, fn)
}

func (ji *JoinRangePath4EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath4Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath4Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinPath5EdgeIndex struct {
	inner *joinPathTraversalIndex
}

func NewJoinPath5EdgeIndex() *JoinPath5EdgeIndex {
	return &JoinPath5EdgeIndex{inner: newJoinPathTraversalIndex()}
}

func (ji *JoinPath5EdgeIndex) Add(edge JoinPath5Edge, filterValue Value, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath5Edge(edge), filterValue, hash)
}

func (ji *JoinPath5EdgeIndex) Remove(edge JoinPath5Edge, filterValue Value, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath5Edge(edge), filterValue, hash)
}

func (ji *JoinPath5EdgeIndex) Lookup(edge JoinPath5Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath5Edge(edge), filterValue)
}

func (ji *JoinPath5EdgeIndex) ForEachHash(edge JoinPath5Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath5Edge(edge), filterValue, fn)
}

func (ji *JoinPath5EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath5Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath5Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinRangePath5EdgeIndex struct {
	inner *joinRangePathTraversalIndex
}

func NewJoinRangePath5EdgeIndex() *JoinRangePath5EdgeIndex {
	return &JoinRangePath5EdgeIndex{inner: newJoinRangePathTraversalIndex()}
}

func (ji *JoinRangePath5EdgeIndex) Add(edge JoinPath5Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath5Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath5EdgeIndex) Remove(edge JoinPath5Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath5Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath5EdgeIndex) Lookup(edge JoinPath5Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath5Edge(edge), filterValue)
}

func (ji *JoinRangePath5EdgeIndex) ForEachHash(edge JoinPath5Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath5Edge(edge), filterValue, fn)
}

func (ji *JoinRangePath5EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath5Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath5Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinPath6EdgeIndex struct {
	inner *joinPathTraversalIndex
}

func NewJoinPath6EdgeIndex() *JoinPath6EdgeIndex {
	return &JoinPath6EdgeIndex{inner: newJoinPathTraversalIndex()}
}

func (ji *JoinPath6EdgeIndex) Add(edge JoinPath6Edge, filterValue Value, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath6Edge(edge), filterValue, hash)
}

func (ji *JoinPath6EdgeIndex) Remove(edge JoinPath6Edge, filterValue Value, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath6Edge(edge), filterValue, hash)
}

func (ji *JoinPath6EdgeIndex) Lookup(edge JoinPath6Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath6Edge(edge), filterValue)
}

func (ji *JoinPath6EdgeIndex) ForEachHash(edge JoinPath6Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath6Edge(edge), filterValue, fn)
}

func (ji *JoinPath6EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath6Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath6Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinRangePath6EdgeIndex struct {
	inner *joinRangePathTraversalIndex
}

func NewJoinRangePath6EdgeIndex() *JoinRangePath6EdgeIndex {
	return &JoinRangePath6EdgeIndex{inner: newJoinRangePathTraversalIndex()}
}

func (ji *JoinRangePath6EdgeIndex) Add(edge JoinPath6Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath6Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath6EdgeIndex) Remove(edge JoinPath6Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath6Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath6EdgeIndex) Lookup(edge JoinPath6Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath6Edge(edge), filterValue)
}

func (ji *JoinRangePath6EdgeIndex) ForEachHash(edge JoinPath6Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath6Edge(edge), filterValue, fn)
}

func (ji *JoinRangePath6EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath6Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath6Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinPath7EdgeIndex struct {
	inner *joinPathTraversalIndex
}

func NewJoinPath7EdgeIndex() *JoinPath7EdgeIndex {
	return &JoinPath7EdgeIndex{inner: newJoinPathTraversalIndex()}
}

func (ji *JoinPath7EdgeIndex) Add(edge JoinPath7Edge, filterValue Value, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath7Edge(edge), filterValue, hash)
}

func (ji *JoinPath7EdgeIndex) Remove(edge JoinPath7Edge, filterValue Value, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath7Edge(edge), filterValue, hash)
}

func (ji *JoinPath7EdgeIndex) Lookup(edge JoinPath7Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath7Edge(edge), filterValue)
}

func (ji *JoinPath7EdgeIndex) ForEachHash(edge JoinPath7Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath7Edge(edge), filterValue, fn)
}

func (ji *JoinPath7EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath7Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath7Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinRangePath7EdgeIndex struct {
	inner *joinRangePathTraversalIndex
}

func NewJoinRangePath7EdgeIndex() *JoinRangePath7EdgeIndex {
	return &JoinRangePath7EdgeIndex{inner: newJoinRangePathTraversalIndex()}
}

func (ji *JoinRangePath7EdgeIndex) Add(edge JoinPath7Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath7Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath7EdgeIndex) Remove(edge JoinPath7Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath7Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath7EdgeIndex) Lookup(edge JoinPath7Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath7Edge(edge), filterValue)
}

func (ji *JoinRangePath7EdgeIndex) ForEachHash(edge JoinPath7Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath7Edge(edge), filterValue, fn)
}

func (ji *JoinRangePath7EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath7Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath7Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinPath8EdgeIndex struct {
	inner *joinPathTraversalIndex
}

func NewJoinPath8EdgeIndex() *JoinPath8EdgeIndex {
	return &JoinPath8EdgeIndex{inner: newJoinPathTraversalIndex()}
}

func (ji *JoinPath8EdgeIndex) Add(edge JoinPath8Edge, filterValue Value, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath8Edge(edge), filterValue, hash)
}

func (ji *JoinPath8EdgeIndex) Remove(edge JoinPath8Edge, filterValue Value, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath8Edge(edge), filterValue, hash)
}

func (ji *JoinPath8EdgeIndex) Lookup(edge JoinPath8Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath8Edge(edge), filterValue)
}

func (ji *JoinPath8EdgeIndex) ForEachHash(edge JoinPath8Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath8Edge(edge), filterValue, fn)
}

func (ji *JoinPath8EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath8Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath8Edge(edge); ok {
			fn(fixed)
		}
	})
}

type JoinRangePath8EdgeIndex struct {
	inner *joinRangePathTraversalIndex
}

func NewJoinRangePath8EdgeIndex() *JoinRangePath8EdgeIndex {
	return &JoinRangePath8EdgeIndex{inner: newJoinRangePathTraversalIndex()}
}

func (ji *JoinRangePath8EdgeIndex) Add(edge JoinPath8Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Add(joinPathTraversalEdgeFromJoinPath8Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath8EdgeIndex) Remove(edge JoinPath8Edge, lower, upper Bound, hash QueryHash) {
	ji.inner.Remove(joinPathTraversalEdgeFromJoinPath8Edge(edge), lower, upper, hash)
}

func (ji *JoinRangePath8EdgeIndex) Lookup(edge JoinPath8Edge, filterValue Value) []QueryHash {
	return ji.inner.Lookup(joinPathTraversalEdgeFromJoinPath8Edge(edge), filterValue)
}

func (ji *JoinRangePath8EdgeIndex) ForEachHash(edge JoinPath8Edge, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(joinPathTraversalEdgeFromJoinPath8Edge(edge), filterValue, fn)
}

func (ji *JoinRangePath8EdgeIndex) ForEachEdge(table TableID, fn func(JoinPath8Edge)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := joinPathTraversalEdgeToJoinPath8Edge(edge); ok {
			fn(fixed)
		}
	})
}

func joinPathTraversalEdgeFromJoinPathEdge(edge JoinPathEdge) joinPathTraversalEdge {
	out, _ := newJoinPathTraversalEdge(
		[]TableID{edge.LHSTable, edge.MidTable, edge.RHSTable},
		[]ColID{edge.LHSJoinCol, edge.MidSecondCol},
		[]ColID{edge.MidFirstCol, edge.RHSJoinCol},
		edge.RHSFilterCol,
	)
	return out
}

func joinPathTraversalEdgeFromJoinPath3Edge(edge JoinPath3Edge) joinPathTraversalEdge {
	out, _ := newJoinPathTraversalEdge(
		[]TableID{edge.LHSTable, edge.Mid1Table, edge.Mid2Table, edge.RHSTable},
		[]ColID{edge.LHSJoinCol, edge.Mid1SecondCol, edge.Mid2SecondCol},
		[]ColID{edge.Mid1FirstCol, edge.Mid2FirstCol, edge.RHSJoinCol},
		edge.RHSFilterCol,
	)
	return out
}

func joinPathTraversalEdgeFromJoinPath4Edge(edge JoinPath4Edge) joinPathTraversalEdge {
	out, _ := newJoinPathTraversalEdge(
		[]TableID{edge.LHSTable, edge.Mid1Table, edge.Mid2Table, edge.Mid3Table, edge.RHSTable},
		[]ColID{edge.LHSJoinCol, edge.Mid1SecondCol, edge.Mid2SecondCol, edge.Mid3SecondCol},
		[]ColID{edge.Mid1FirstCol, edge.Mid2FirstCol, edge.Mid3FirstCol, edge.RHSJoinCol},
		edge.RHSFilterCol,
	)
	return out
}

func joinPathTraversalEdgeFromJoinPath5Edge(edge JoinPath5Edge) joinPathTraversalEdge {
	out, _ := newJoinPathTraversalEdge(
		[]TableID{edge.LHSTable, edge.Mid1Table, edge.Mid2Table, edge.Mid3Table, edge.Mid4Table, edge.RHSTable},
		[]ColID{edge.LHSJoinCol, edge.Mid1SecondCol, edge.Mid2SecondCol, edge.Mid3SecondCol, edge.Mid4SecondCol},
		[]ColID{edge.Mid1FirstCol, edge.Mid2FirstCol, edge.Mid3FirstCol, edge.Mid4FirstCol, edge.RHSJoinCol},
		edge.RHSFilterCol,
	)
	return out
}

func joinPathTraversalEdgeFromJoinPath6Edge(edge JoinPath6Edge) joinPathTraversalEdge {
	out, _ := newJoinPathTraversalEdge(
		[]TableID{edge.LHSTable, edge.Mid1Table, edge.Mid2Table, edge.Mid3Table, edge.Mid4Table, edge.Mid5Table, edge.RHSTable},
		[]ColID{edge.LHSJoinCol, edge.Mid1SecondCol, edge.Mid2SecondCol, edge.Mid3SecondCol, edge.Mid4SecondCol, edge.Mid5SecondCol},
		[]ColID{edge.Mid1FirstCol, edge.Mid2FirstCol, edge.Mid3FirstCol, edge.Mid4FirstCol, edge.Mid5FirstCol, edge.RHSJoinCol},
		edge.RHSFilterCol,
	)
	return out
}

func joinPathTraversalEdgeFromJoinPath7Edge(edge JoinPath7Edge) joinPathTraversalEdge {
	out, _ := newJoinPathTraversalEdge(
		[]TableID{edge.LHSTable, edge.Mid1Table, edge.Mid2Table, edge.Mid3Table, edge.Mid4Table, edge.Mid5Table, edge.Mid6Table, edge.RHSTable},
		[]ColID{edge.LHSJoinCol, edge.Mid1SecondCol, edge.Mid2SecondCol, edge.Mid3SecondCol, edge.Mid4SecondCol, edge.Mid5SecondCol, edge.Mid6SecondCol},
		[]ColID{edge.Mid1FirstCol, edge.Mid2FirstCol, edge.Mid3FirstCol, edge.Mid4FirstCol, edge.Mid5FirstCol, edge.Mid6FirstCol, edge.RHSJoinCol},
		edge.RHSFilterCol,
	)
	return out
}

func joinPathTraversalEdgeFromJoinPath8Edge(edge JoinPath8Edge) joinPathTraversalEdge {
	out, _ := newJoinPathTraversalEdge(
		[]TableID{edge.LHSTable, edge.Mid1Table, edge.Mid2Table, edge.Mid3Table, edge.Mid4Table, edge.Mid5Table, edge.Mid6Table, edge.Mid7Table, edge.RHSTable},
		[]ColID{edge.LHSJoinCol, edge.Mid1SecondCol, edge.Mid2SecondCol, edge.Mid3SecondCol, edge.Mid4SecondCol, edge.Mid5SecondCol, edge.Mid6SecondCol, edge.Mid7SecondCol},
		[]ColID{edge.Mid1FirstCol, edge.Mid2FirstCol, edge.Mid3FirstCol, edge.Mid4FirstCol, edge.Mid5FirstCol, edge.Mid6FirstCol, edge.Mid7FirstCol, edge.RHSJoinCol},
		edge.RHSFilterCol,
	)
	return out
}

func joinPathTraversalEdgeToJoinPath3Edge(edge joinPathTraversalEdge) (JoinPath3Edge, bool) {
	if edge.hopCount() != 3 {
		return JoinPath3Edge{}, false
	}
	return JoinPath3Edge{
		LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], RHSTable: edge.tables[3],
		LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
		Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
		RHSJoinCol: edge.toCols[2], RHSFilterCol: edge.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath4Edge(edge joinPathTraversalEdge) (JoinPath4Edge, bool) {
	if edge.hopCount() != 4 {
		return JoinPath4Edge{}, false
	}
	return JoinPath4Edge{
		LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], RHSTable: edge.tables[4],
		LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
		Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
		Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
		RHSJoinCol: edge.toCols[3], RHSFilterCol: edge.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath5Edge(edge joinPathTraversalEdge) (JoinPath5Edge, bool) {
	if edge.hopCount() != 5 {
		return JoinPath5Edge{}, false
	}
	return JoinPath5Edge{
		LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], RHSTable: edge.tables[5],
		LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
		Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
		Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
		Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
		RHSJoinCol: edge.toCols[4], RHSFilterCol: edge.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath6Edge(edge joinPathTraversalEdge) (JoinPath6Edge, bool) {
	if edge.hopCount() != 6 {
		return JoinPath6Edge{}, false
	}
	return JoinPath6Edge{
		LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], Mid5Table: edge.tables[5], RHSTable: edge.tables[6],
		LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
		Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
		Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
		Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
		Mid5FirstCol: edge.toCols[4], Mid5SecondCol: edge.fromCols[5],
		RHSJoinCol: edge.toCols[5], RHSFilterCol: edge.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath7Edge(edge joinPathTraversalEdge) (JoinPath7Edge, bool) {
	if edge.hopCount() != 7 {
		return JoinPath7Edge{}, false
	}
	return JoinPath7Edge{
		LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], Mid5Table: edge.tables[5], Mid6Table: edge.tables[6], RHSTable: edge.tables[7],
		LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
		Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
		Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
		Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
		Mid5FirstCol: edge.toCols[4], Mid5SecondCol: edge.fromCols[5],
		Mid6FirstCol: edge.toCols[5], Mid6SecondCol: edge.fromCols[6],
		RHSJoinCol: edge.toCols[6], RHSFilterCol: edge.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath8Edge(edge joinPathTraversalEdge) (JoinPath8Edge, bool) {
	if edge.hopCount() != 8 {
		return JoinPath8Edge{}, false
	}
	return JoinPath8Edge{
		LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], Mid5Table: edge.tables[5], Mid6Table: edge.tables[6], Mid7Table: edge.tables[7], RHSTable: edge.tables[8],
		LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
		Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
		Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
		Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
		Mid5FirstCol: edge.toCols[4], Mid5SecondCol: edge.fromCols[5],
		Mid6FirstCol: edge.toCols[5], Mid6SecondCol: edge.fromCols[6],
		Mid7FirstCol: edge.toCols[6], Mid7SecondCol: edge.fromCols[7],
		RHSJoinCol: edge.toCols[7], RHSFilterCol: edge.rhsFilterCol,
	}, true
}
