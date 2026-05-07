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

type joinPathFixedValueIndex[E any] struct {
	inner         *joinPathTraversalIndex
	toTraversal   func(E) joinPathTraversalEdge
	fromTraversal func(joinPathTraversalEdge) (E, bool)
}

func newJoinPathFixedValueIndex[E any](
	toTraversal func(E) joinPathTraversalEdge,
	fromTraversal func(joinPathTraversalEdge) (E, bool),
) *joinPathFixedValueIndex[E] {
	return &joinPathFixedValueIndex[E]{
		inner:         newJoinPathTraversalIndex(),
		toTraversal:   toTraversal,
		fromTraversal: fromTraversal,
	}
}

func (ji *joinPathFixedValueIndex[E]) Add(edge E, filterValue Value, hash QueryHash) {
	ji.inner.Add(ji.toTraversal(edge), filterValue, hash)
}

func (ji *joinPathFixedValueIndex[E]) Remove(edge E, filterValue Value, hash QueryHash) {
	ji.inner.Remove(ji.toTraversal(edge), filterValue, hash)
}

func (ji *joinPathFixedValueIndex[E]) Lookup(edge E, filterValue Value) []QueryHash {
	return ji.inner.Lookup(ji.toTraversal(edge), filterValue)
}

func (ji *joinPathFixedValueIndex[E]) ForEachHash(edge E, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(ji.toTraversal(edge), filterValue, fn)
}

func (ji *joinPathFixedValueIndex[E]) ForEachEdge(table TableID, fn func(E)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := ji.fromTraversal(edge); ok {
			fn(fixed)
		}
	})
}

type joinPathFixedRangeIndex[E any] struct {
	inner         *joinRangePathTraversalIndex
	toTraversal   func(E) joinPathTraversalEdge
	fromTraversal func(joinPathTraversalEdge) (E, bool)
}

func newJoinPathFixedRangeIndex[E any](
	toTraversal func(E) joinPathTraversalEdge,
	fromTraversal func(joinPathTraversalEdge) (E, bool),
) *joinPathFixedRangeIndex[E] {
	return &joinPathFixedRangeIndex[E]{
		inner:         newJoinRangePathTraversalIndex(),
		toTraversal:   toTraversal,
		fromTraversal: fromTraversal,
	}
}

func (ji *joinPathFixedRangeIndex[E]) Add(edge E, lower, upper Bound, hash QueryHash) {
	ji.inner.Add(ji.toTraversal(edge), lower, upper, hash)
}

func (ji *joinPathFixedRangeIndex[E]) Remove(edge E, lower, upper Bound, hash QueryHash) {
	ji.inner.Remove(ji.toTraversal(edge), lower, upper, hash)
}

func (ji *joinPathFixedRangeIndex[E]) Lookup(edge E, filterValue Value) []QueryHash {
	return ji.inner.Lookup(ji.toTraversal(edge), filterValue)
}

func (ji *joinPathFixedRangeIndex[E]) ForEachHash(edge E, filterValue Value, fn func(QueryHash)) {
	ji.inner.ForEachHash(ji.toTraversal(edge), filterValue, fn)
}

func (ji *joinPathFixedRangeIndex[E]) ForEachEdge(table TableID, fn func(E)) {
	ji.inner.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		if fixed, ok := ji.fromTraversal(edge); ok {
			fn(fixed)
		}
	})
}

type JoinPathEdgeIndex struct {
	*joinPathFixedValueIndex[JoinPathEdge]
}

func NewJoinPathEdgeIndex() *JoinPathEdgeIndex {
	return &JoinPathEdgeIndex{newJoinPathFixedValueIndex(joinPathTraversalEdgeFromJoinPathEdge, joinPathTraversalEdgeToJoinPathEdge)}
}

type JoinRangePathEdgeIndex struct {
	*joinPathFixedRangeIndex[JoinPathEdge]
}

func NewJoinRangePathEdgeIndex() *JoinRangePathEdgeIndex {
	return &JoinRangePathEdgeIndex{newJoinPathFixedRangeIndex(joinPathTraversalEdgeFromJoinPathEdge, joinPathTraversalEdgeToJoinPathEdge)}
}

type JoinPath3EdgeIndex struct {
	*joinPathFixedValueIndex[JoinPath3Edge]
}

func NewJoinPath3EdgeIndex() *JoinPath3EdgeIndex {
	return &JoinPath3EdgeIndex{newJoinPathFixedValueIndex(joinPathTraversalEdgeFromJoinPath3Edge, joinPathTraversalEdgeToJoinPath3Edge)}
}

type JoinRangePath3EdgeIndex struct {
	*joinPathFixedRangeIndex[JoinPath3Edge]
}

func NewJoinRangePath3EdgeIndex() *JoinRangePath3EdgeIndex {
	return &JoinRangePath3EdgeIndex{newJoinPathFixedRangeIndex(joinPathTraversalEdgeFromJoinPath3Edge, joinPathTraversalEdgeToJoinPath3Edge)}
}

type JoinPath4EdgeIndex struct {
	*joinPathFixedValueIndex[JoinPath4Edge]
}

func NewJoinPath4EdgeIndex() *JoinPath4EdgeIndex {
	return &JoinPath4EdgeIndex{newJoinPathFixedValueIndex(joinPathTraversalEdgeFromJoinPath4Edge, joinPathTraversalEdgeToJoinPath4Edge)}
}

type JoinRangePath4EdgeIndex struct {
	*joinPathFixedRangeIndex[JoinPath4Edge]
}

func NewJoinRangePath4EdgeIndex() *JoinRangePath4EdgeIndex {
	return &JoinRangePath4EdgeIndex{newJoinPathFixedRangeIndex(joinPathTraversalEdgeFromJoinPath4Edge, joinPathTraversalEdgeToJoinPath4Edge)}
}

type JoinPath5EdgeIndex struct {
	*joinPathFixedValueIndex[JoinPath5Edge]
}

func NewJoinPath5EdgeIndex() *JoinPath5EdgeIndex {
	return &JoinPath5EdgeIndex{newJoinPathFixedValueIndex(joinPathTraversalEdgeFromJoinPath5Edge, joinPathTraversalEdgeToJoinPath5Edge)}
}

type JoinRangePath5EdgeIndex struct {
	*joinPathFixedRangeIndex[JoinPath5Edge]
}

func NewJoinRangePath5EdgeIndex() *JoinRangePath5EdgeIndex {
	return &JoinRangePath5EdgeIndex{newJoinPathFixedRangeIndex(joinPathTraversalEdgeFromJoinPath5Edge, joinPathTraversalEdgeToJoinPath5Edge)}
}

type JoinPath6EdgeIndex struct {
	*joinPathFixedValueIndex[JoinPath6Edge]
}

func NewJoinPath6EdgeIndex() *JoinPath6EdgeIndex {
	return &JoinPath6EdgeIndex{newJoinPathFixedValueIndex(joinPathTraversalEdgeFromJoinPath6Edge, joinPathTraversalEdgeToJoinPath6Edge)}
}

type JoinRangePath6EdgeIndex struct {
	*joinPathFixedRangeIndex[JoinPath6Edge]
}

func NewJoinRangePath6EdgeIndex() *JoinRangePath6EdgeIndex {
	return &JoinRangePath6EdgeIndex{newJoinPathFixedRangeIndex(joinPathTraversalEdgeFromJoinPath6Edge, joinPathTraversalEdgeToJoinPath6Edge)}
}

type JoinPath7EdgeIndex struct {
	*joinPathFixedValueIndex[JoinPath7Edge]
}

func NewJoinPath7EdgeIndex() *JoinPath7EdgeIndex {
	return &JoinPath7EdgeIndex{newJoinPathFixedValueIndex(joinPathTraversalEdgeFromJoinPath7Edge, joinPathTraversalEdgeToJoinPath7Edge)}
}

type JoinRangePath7EdgeIndex struct {
	*joinPathFixedRangeIndex[JoinPath7Edge]
}

func NewJoinRangePath7EdgeIndex() *JoinRangePath7EdgeIndex {
	return &JoinRangePath7EdgeIndex{newJoinPathFixedRangeIndex(joinPathTraversalEdgeFromJoinPath7Edge, joinPathTraversalEdgeToJoinPath7Edge)}
}

type JoinPath8EdgeIndex struct {
	*joinPathFixedValueIndex[JoinPath8Edge]
}

func NewJoinPath8EdgeIndex() *JoinPath8EdgeIndex {
	return &JoinPath8EdgeIndex{newJoinPathFixedValueIndex(joinPathTraversalEdgeFromJoinPath8Edge, joinPathTraversalEdgeToJoinPath8Edge)}
}

type JoinRangePath8EdgeIndex struct {
	*joinPathFixedRangeIndex[JoinPath8Edge]
}

func NewJoinRangePath8EdgeIndex() *JoinRangePath8EdgeIndex {
	return &JoinRangePath8EdgeIndex{newJoinPathFixedRangeIndex(joinPathTraversalEdgeFromJoinPath8Edge, joinPathTraversalEdgeToJoinPath8Edge)}
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
