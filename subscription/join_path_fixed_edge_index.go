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

const (
	joinPathFixedMaxHops = 8
	joinPathFixedMaxMids = joinPathFixedMaxHops - 1
)

type joinPathFixedMidDescriptor struct {
	table     TableID
	firstCol  ColID
	secondCol ColID
}

type joinPathFixedEdgeDescriptor struct {
	hops         int
	lhsTable     TableID
	mids         [joinPathFixedMaxMids]joinPathFixedMidDescriptor
	rhsTable     TableID
	lhsJoinCol   ColID
	rhsJoinCol   ColID
	rhsFilterCol ColID
}

func joinPathFixedMid(table TableID, firstCol, secondCol ColID) joinPathFixedMidDescriptor {
	return joinPathFixedMidDescriptor{
		table:     table,
		firstCol:  firstCol,
		secondCol: secondCol,
	}
}

func newJoinPathFixedEdgeDescriptor(
	lhsTable, rhsTable TableID,
	lhsJoinCol, rhsJoinCol, rhsFilterCol ColID,
	mids ...joinPathFixedMidDescriptor,
) joinPathFixedEdgeDescriptor {
	out := joinPathFixedEdgeDescriptor{
		hops:         len(mids) + 1,
		lhsTable:     lhsTable,
		rhsTable:     rhsTable,
		lhsJoinCol:   lhsJoinCol,
		rhsJoinCol:   rhsJoinCol,
		rhsFilterCol: rhsFilterCol,
	}
	copy(out.mids[:], mids)
	return out
}

func (edge joinPathFixedEdgeDescriptor) traversalEdge() joinPathTraversalEdge {
	if edge.hops < 2 || edge.hops > joinPathFixedMaxHops {
		return joinPathTraversalEdge{}
	}
	var tables [joinPathFixedMaxHops + 1]TableID
	var fromCols [joinPathFixedMaxHops]ColID
	var toCols [joinPathFixedMaxHops]ColID

	tables[0] = edge.lhsTable
	tables[edge.hops] = edge.rhsTable
	fromCols[0] = edge.lhsJoinCol
	toCols[edge.hops-1] = edge.rhsJoinCol
	for mid := 0; mid < edge.hops-1; mid++ {
		tables[mid+1] = edge.mids[mid].table
		fromCols[mid+1] = edge.mids[mid].secondCol
		toCols[mid] = edge.mids[mid].firstCol
	}

	out, _ := newJoinPathTraversalEdge(
		tables[:edge.hops+1],
		fromCols[:edge.hops],
		toCols[:edge.hops],
		edge.rhsFilterCol,
	)
	return out
}

func joinPathFixedEdgeDescriptorFromTraversal(edge joinPathTraversalEdge, hops int) (joinPathFixedEdgeDescriptor, bool) {
	if edge.hopCount() != hops || hops < 2 || hops > joinPathFixedMaxHops {
		return joinPathFixedEdgeDescriptor{}, false
	}
	out := joinPathFixedEdgeDescriptor{
		hops:         hops,
		lhsTable:     edge.tables[0],
		rhsTable:     edge.tables[hops],
		lhsJoinCol:   edge.fromCols[0],
		rhsJoinCol:   edge.toCols[hops-1],
		rhsFilterCol: edge.rhsFilterCol,
	}
	for mid := 0; mid < hops-1; mid++ {
		out.mids[mid] = joinPathFixedMid(edge.tables[mid+1], edge.toCols[mid], edge.fromCols[mid+1])
	}
	return out, true
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
	return newJoinPathFixedEdgeDescriptor(
		edge.LHSTable, edge.RHSTable,
		edge.LHSJoinCol, edge.RHSJoinCol, edge.RHSFilterCol,
		joinPathFixedMid(edge.MidTable, edge.MidFirstCol, edge.MidSecondCol),
	).traversalEdge()
}

func joinPathTraversalEdgeToJoinPathEdge(edge joinPathTraversalEdge) (JoinPathEdge, bool) {
	fixed, ok := joinPathFixedEdgeDescriptorFromTraversal(edge, 2)
	if !ok {
		return JoinPathEdge{}, false
	}
	mid := fixed.mids[0]
	return JoinPathEdge{
		LHSTable:     fixed.lhsTable,
		MidTable:     mid.table,
		RHSTable:     fixed.rhsTable,
		LHSJoinCol:   fixed.lhsJoinCol,
		MidFirstCol:  mid.firstCol,
		MidSecondCol: mid.secondCol,
		RHSJoinCol:   fixed.rhsJoinCol,
		RHSFilterCol: fixed.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeFromJoinPath3Edge(edge JoinPath3Edge) joinPathTraversalEdge {
	return newJoinPathFixedEdgeDescriptor(
		edge.LHSTable, edge.RHSTable,
		edge.LHSJoinCol, edge.RHSJoinCol, edge.RHSFilterCol,
		joinPathFixedMid(edge.Mid1Table, edge.Mid1FirstCol, edge.Mid1SecondCol),
		joinPathFixedMid(edge.Mid2Table, edge.Mid2FirstCol, edge.Mid2SecondCol),
	).traversalEdge()
}

func joinPathTraversalEdgeFromJoinPath4Edge(edge JoinPath4Edge) joinPathTraversalEdge {
	return newJoinPathFixedEdgeDescriptor(
		edge.LHSTable, edge.RHSTable,
		edge.LHSJoinCol, edge.RHSJoinCol, edge.RHSFilterCol,
		joinPathFixedMid(edge.Mid1Table, edge.Mid1FirstCol, edge.Mid1SecondCol),
		joinPathFixedMid(edge.Mid2Table, edge.Mid2FirstCol, edge.Mid2SecondCol),
		joinPathFixedMid(edge.Mid3Table, edge.Mid3FirstCol, edge.Mid3SecondCol),
	).traversalEdge()
}

func joinPathTraversalEdgeFromJoinPath5Edge(edge JoinPath5Edge) joinPathTraversalEdge {
	return newJoinPathFixedEdgeDescriptor(
		edge.LHSTable, edge.RHSTable,
		edge.LHSJoinCol, edge.RHSJoinCol, edge.RHSFilterCol,
		joinPathFixedMid(edge.Mid1Table, edge.Mid1FirstCol, edge.Mid1SecondCol),
		joinPathFixedMid(edge.Mid2Table, edge.Mid2FirstCol, edge.Mid2SecondCol),
		joinPathFixedMid(edge.Mid3Table, edge.Mid3FirstCol, edge.Mid3SecondCol),
		joinPathFixedMid(edge.Mid4Table, edge.Mid4FirstCol, edge.Mid4SecondCol),
	).traversalEdge()
}

func joinPathTraversalEdgeFromJoinPath6Edge(edge JoinPath6Edge) joinPathTraversalEdge {
	return newJoinPathFixedEdgeDescriptor(
		edge.LHSTable, edge.RHSTable,
		edge.LHSJoinCol, edge.RHSJoinCol, edge.RHSFilterCol,
		joinPathFixedMid(edge.Mid1Table, edge.Mid1FirstCol, edge.Mid1SecondCol),
		joinPathFixedMid(edge.Mid2Table, edge.Mid2FirstCol, edge.Mid2SecondCol),
		joinPathFixedMid(edge.Mid3Table, edge.Mid3FirstCol, edge.Mid3SecondCol),
		joinPathFixedMid(edge.Mid4Table, edge.Mid4FirstCol, edge.Mid4SecondCol),
		joinPathFixedMid(edge.Mid5Table, edge.Mid5FirstCol, edge.Mid5SecondCol),
	).traversalEdge()
}

func joinPathTraversalEdgeFromJoinPath7Edge(edge JoinPath7Edge) joinPathTraversalEdge {
	return newJoinPathFixedEdgeDescriptor(
		edge.LHSTable, edge.RHSTable,
		edge.LHSJoinCol, edge.RHSJoinCol, edge.RHSFilterCol,
		joinPathFixedMid(edge.Mid1Table, edge.Mid1FirstCol, edge.Mid1SecondCol),
		joinPathFixedMid(edge.Mid2Table, edge.Mid2FirstCol, edge.Mid2SecondCol),
		joinPathFixedMid(edge.Mid3Table, edge.Mid3FirstCol, edge.Mid3SecondCol),
		joinPathFixedMid(edge.Mid4Table, edge.Mid4FirstCol, edge.Mid4SecondCol),
		joinPathFixedMid(edge.Mid5Table, edge.Mid5FirstCol, edge.Mid5SecondCol),
		joinPathFixedMid(edge.Mid6Table, edge.Mid6FirstCol, edge.Mid6SecondCol),
	).traversalEdge()
}

func joinPathTraversalEdgeFromJoinPath8Edge(edge JoinPath8Edge) joinPathTraversalEdge {
	return newJoinPathFixedEdgeDescriptor(
		edge.LHSTable, edge.RHSTable,
		edge.LHSJoinCol, edge.RHSJoinCol, edge.RHSFilterCol,
		joinPathFixedMid(edge.Mid1Table, edge.Mid1FirstCol, edge.Mid1SecondCol),
		joinPathFixedMid(edge.Mid2Table, edge.Mid2FirstCol, edge.Mid2SecondCol),
		joinPathFixedMid(edge.Mid3Table, edge.Mid3FirstCol, edge.Mid3SecondCol),
		joinPathFixedMid(edge.Mid4Table, edge.Mid4FirstCol, edge.Mid4SecondCol),
		joinPathFixedMid(edge.Mid5Table, edge.Mid5FirstCol, edge.Mid5SecondCol),
		joinPathFixedMid(edge.Mid6Table, edge.Mid6FirstCol, edge.Mid6SecondCol),
		joinPathFixedMid(edge.Mid7Table, edge.Mid7FirstCol, edge.Mid7SecondCol),
	).traversalEdge()
}

func joinPathTraversalEdgeToJoinPath3Edge(edge joinPathTraversalEdge) (JoinPath3Edge, bool) {
	fixed, ok := joinPathFixedEdgeDescriptorFromTraversal(edge, 3)
	if !ok {
		return JoinPath3Edge{}, false
	}
	mids := fixed.mids
	return JoinPath3Edge{
		LHSTable: fixed.lhsTable, Mid1Table: mids[0].table, Mid2Table: mids[1].table, RHSTable: fixed.rhsTable,
		LHSJoinCol: fixed.lhsJoinCol, Mid1FirstCol: mids[0].firstCol, Mid1SecondCol: mids[0].secondCol,
		Mid2FirstCol: mids[1].firstCol, Mid2SecondCol: mids[1].secondCol,
		RHSJoinCol: fixed.rhsJoinCol, RHSFilterCol: fixed.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath4Edge(edge joinPathTraversalEdge) (JoinPath4Edge, bool) {
	fixed, ok := joinPathFixedEdgeDescriptorFromTraversal(edge, 4)
	if !ok {
		return JoinPath4Edge{}, false
	}
	mids := fixed.mids
	return JoinPath4Edge{
		LHSTable: fixed.lhsTable, Mid1Table: mids[0].table, Mid2Table: mids[1].table, Mid3Table: mids[2].table, RHSTable: fixed.rhsTable,
		LHSJoinCol: fixed.lhsJoinCol, Mid1FirstCol: mids[0].firstCol, Mid1SecondCol: mids[0].secondCol,
		Mid2FirstCol: mids[1].firstCol, Mid2SecondCol: mids[1].secondCol,
		Mid3FirstCol: mids[2].firstCol, Mid3SecondCol: mids[2].secondCol,
		RHSJoinCol: fixed.rhsJoinCol, RHSFilterCol: fixed.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath5Edge(edge joinPathTraversalEdge) (JoinPath5Edge, bool) {
	fixed, ok := joinPathFixedEdgeDescriptorFromTraversal(edge, 5)
	if !ok {
		return JoinPath5Edge{}, false
	}
	mids := fixed.mids
	return JoinPath5Edge{
		LHSTable: fixed.lhsTable, Mid1Table: mids[0].table, Mid2Table: mids[1].table, Mid3Table: mids[2].table, Mid4Table: mids[3].table, RHSTable: fixed.rhsTable,
		LHSJoinCol: fixed.lhsJoinCol, Mid1FirstCol: mids[0].firstCol, Mid1SecondCol: mids[0].secondCol,
		Mid2FirstCol: mids[1].firstCol, Mid2SecondCol: mids[1].secondCol,
		Mid3FirstCol: mids[2].firstCol, Mid3SecondCol: mids[2].secondCol,
		Mid4FirstCol: mids[3].firstCol, Mid4SecondCol: mids[3].secondCol,
		RHSJoinCol: fixed.rhsJoinCol, RHSFilterCol: fixed.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath6Edge(edge joinPathTraversalEdge) (JoinPath6Edge, bool) {
	fixed, ok := joinPathFixedEdgeDescriptorFromTraversal(edge, 6)
	if !ok {
		return JoinPath6Edge{}, false
	}
	mids := fixed.mids
	return JoinPath6Edge{
		LHSTable: fixed.lhsTable, Mid1Table: mids[0].table, Mid2Table: mids[1].table, Mid3Table: mids[2].table, Mid4Table: mids[3].table, Mid5Table: mids[4].table, RHSTable: fixed.rhsTable,
		LHSJoinCol: fixed.lhsJoinCol, Mid1FirstCol: mids[0].firstCol, Mid1SecondCol: mids[0].secondCol,
		Mid2FirstCol: mids[1].firstCol, Mid2SecondCol: mids[1].secondCol,
		Mid3FirstCol: mids[2].firstCol, Mid3SecondCol: mids[2].secondCol,
		Mid4FirstCol: mids[3].firstCol, Mid4SecondCol: mids[3].secondCol,
		Mid5FirstCol: mids[4].firstCol, Mid5SecondCol: mids[4].secondCol,
		RHSJoinCol: fixed.rhsJoinCol, RHSFilterCol: fixed.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath7Edge(edge joinPathTraversalEdge) (JoinPath7Edge, bool) {
	fixed, ok := joinPathFixedEdgeDescriptorFromTraversal(edge, 7)
	if !ok {
		return JoinPath7Edge{}, false
	}
	mids := fixed.mids
	return JoinPath7Edge{
		LHSTable: fixed.lhsTable, Mid1Table: mids[0].table, Mid2Table: mids[1].table, Mid3Table: mids[2].table, Mid4Table: mids[3].table, Mid5Table: mids[4].table, Mid6Table: mids[5].table, RHSTable: fixed.rhsTable,
		LHSJoinCol: fixed.lhsJoinCol, Mid1FirstCol: mids[0].firstCol, Mid1SecondCol: mids[0].secondCol,
		Mid2FirstCol: mids[1].firstCol, Mid2SecondCol: mids[1].secondCol,
		Mid3FirstCol: mids[2].firstCol, Mid3SecondCol: mids[2].secondCol,
		Mid4FirstCol: mids[3].firstCol, Mid4SecondCol: mids[3].secondCol,
		Mid5FirstCol: mids[4].firstCol, Mid5SecondCol: mids[4].secondCol,
		Mid6FirstCol: mids[5].firstCol, Mid6SecondCol: mids[5].secondCol,
		RHSJoinCol: fixed.rhsJoinCol, RHSFilterCol: fixed.rhsFilterCol,
	}, true
}

func joinPathTraversalEdgeToJoinPath8Edge(edge joinPathTraversalEdge) (JoinPath8Edge, bool) {
	fixed, ok := joinPathFixedEdgeDescriptorFromTraversal(edge, 8)
	if !ok {
		return JoinPath8Edge{}, false
	}
	mids := fixed.mids
	return JoinPath8Edge{
		LHSTable: fixed.lhsTable, Mid1Table: mids[0].table, Mid2Table: mids[1].table, Mid3Table: mids[2].table, Mid4Table: mids[3].table, Mid5Table: mids[4].table, Mid6Table: mids[5].table, Mid7Table: mids[6].table, RHSTable: fixed.rhsTable,
		LHSJoinCol: fixed.lhsJoinCol, Mid1FirstCol: mids[0].firstCol, Mid1SecondCol: mids[0].secondCol,
		Mid2FirstCol: mids[1].firstCol, Mid2SecondCol: mids[1].secondCol,
		Mid3FirstCol: mids[2].firstCol, Mid3SecondCol: mids[2].secondCol,
		Mid4FirstCol: mids[3].firstCol, Mid4SecondCol: mids[3].secondCol,
		Mid5FirstCol: mids[4].firstCol, Mid5SecondCol: mids[4].secondCol,
		Mid6FirstCol: mids[5].firstCol, Mid6SecondCol: mids[5].secondCol,
		Mid7FirstCol: mids[6].firstCol, Mid7SecondCol: mids[6].secondCol,
		RHSJoinCol: fixed.rhsJoinCol, RHSFilterCol: fixed.rhsFilterCol,
	}, true
}
