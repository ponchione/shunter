package subscription

func mutateJoinPathTraversalFixedCompatibility(idx *PruningIndexes, edge joinPathTraversalEdge, value Value, hash QueryHash, add bool) {
	switch edge.hopCount() {
	case 2:
		fixed, _ := joinPathTraversalEdgeToJoinPathEdge(edge)
		if add {
			idx.JoinPathEdge.Add(fixed, value, hash)
		} else {
			idx.JoinPathEdge.Remove(fixed, value, hash)
		}
	case 3:
		fixed := JoinPath3Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], RHSTable: edge.tables[3],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			RHSJoinCol: edge.toCols[2], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinPath3Edge.Add(fixed, value, hash)
		} else {
			idx.JoinPath3Edge.Remove(fixed, value, hash)
		}
	case 4:
		fixed := JoinPath4Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], RHSTable: edge.tables[4],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			RHSJoinCol: edge.toCols[3], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinPath4Edge.Add(fixed, value, hash)
		} else {
			idx.JoinPath4Edge.Remove(fixed, value, hash)
		}
	case 5:
		fixed := JoinPath5Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], RHSTable: edge.tables[5],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
			RHSJoinCol: edge.toCols[4], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinPath5Edge.Add(fixed, value, hash)
		} else {
			idx.JoinPath5Edge.Remove(fixed, value, hash)
		}
	case 6:
		fixed := JoinPath6Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], Mid5Table: edge.tables[5], RHSTable: edge.tables[6],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
			Mid5FirstCol: edge.toCols[4], Mid5SecondCol: edge.fromCols[5],
			RHSJoinCol: edge.toCols[5], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinPath6Edge.Add(fixed, value, hash)
		} else {
			idx.JoinPath6Edge.Remove(fixed, value, hash)
		}
	case 7:
		fixed := JoinPath7Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], Mid5Table: edge.tables[5], Mid6Table: edge.tables[6], RHSTable: edge.tables[7],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
			Mid5FirstCol: edge.toCols[4], Mid5SecondCol: edge.fromCols[5],
			Mid6FirstCol: edge.toCols[5], Mid6SecondCol: edge.fromCols[6],
			RHSJoinCol: edge.toCols[6], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinPath7Edge.Add(fixed, value, hash)
		} else {
			idx.JoinPath7Edge.Remove(fixed, value, hash)
		}
	case 8:
		fixed := JoinPath8Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], Mid5Table: edge.tables[5], Mid6Table: edge.tables[6], Mid7Table: edge.tables[7], RHSTable: edge.tables[8],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
			Mid5FirstCol: edge.toCols[4], Mid5SecondCol: edge.fromCols[5],
			Mid6FirstCol: edge.toCols[5], Mid6SecondCol: edge.fromCols[6],
			Mid7FirstCol: edge.toCols[6], Mid7SecondCol: edge.fromCols[7],
			RHSJoinCol: edge.toCols[7], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinPath8Edge.Add(fixed, value, hash)
		} else {
			idx.JoinPath8Edge.Remove(fixed, value, hash)
		}
	}
}

func mutateJoinRangePathTraversalFixedCompatibility(idx *PruningIndexes, edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash, add bool) {
	switch edge.hopCount() {
	case 2:
		fixed, _ := joinPathTraversalEdgeToJoinPathEdge(edge)
		if add {
			idx.JoinRangePathEdge.Add(fixed, lower, upper, hash)
		} else {
			idx.JoinRangePathEdge.Remove(fixed, lower, upper, hash)
		}
	case 3:
		fixed := JoinPath3Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], RHSTable: edge.tables[3],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			RHSJoinCol: edge.toCols[2], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinRangePath3Edge.Add(fixed, lower, upper, hash)
		} else {
			idx.JoinRangePath3Edge.Remove(fixed, lower, upper, hash)
		}
	case 4:
		fixed := JoinPath4Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], RHSTable: edge.tables[4],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			RHSJoinCol: edge.toCols[3], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinRangePath4Edge.Add(fixed, lower, upper, hash)
		} else {
			idx.JoinRangePath4Edge.Remove(fixed, lower, upper, hash)
		}
	case 5:
		fixed := JoinPath5Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], RHSTable: edge.tables[5],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
			RHSJoinCol: edge.toCols[4], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinRangePath5Edge.Add(fixed, lower, upper, hash)
		} else {
			idx.JoinRangePath5Edge.Remove(fixed, lower, upper, hash)
		}
	case 6:
		fixed := JoinPath6Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], Mid5Table: edge.tables[5], RHSTable: edge.tables[6],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
			Mid5FirstCol: edge.toCols[4], Mid5SecondCol: edge.fromCols[5],
			RHSJoinCol: edge.toCols[5], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinRangePath6Edge.Add(fixed, lower, upper, hash)
		} else {
			idx.JoinRangePath6Edge.Remove(fixed, lower, upper, hash)
		}
	case 7:
		fixed := JoinPath7Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], Mid5Table: edge.tables[5], Mid6Table: edge.tables[6], RHSTable: edge.tables[7],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
			Mid5FirstCol: edge.toCols[4], Mid5SecondCol: edge.fromCols[5],
			Mid6FirstCol: edge.toCols[5], Mid6SecondCol: edge.fromCols[6],
			RHSJoinCol: edge.toCols[6], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinRangePath7Edge.Add(fixed, lower, upper, hash)
		} else {
			idx.JoinRangePath7Edge.Remove(fixed, lower, upper, hash)
		}
	case 8:
		fixed := JoinPath8Edge{
			LHSTable: edge.tables[0], Mid1Table: edge.tables[1], Mid2Table: edge.tables[2], Mid3Table: edge.tables[3], Mid4Table: edge.tables[4], Mid5Table: edge.tables[5], Mid6Table: edge.tables[6], Mid7Table: edge.tables[7], RHSTable: edge.tables[8],
			LHSJoinCol: edge.fromCols[0], Mid1FirstCol: edge.toCols[0], Mid1SecondCol: edge.fromCols[1],
			Mid2FirstCol: edge.toCols[1], Mid2SecondCol: edge.fromCols[2],
			Mid3FirstCol: edge.toCols[2], Mid3SecondCol: edge.fromCols[3],
			Mid4FirstCol: edge.toCols[3], Mid4SecondCol: edge.fromCols[4],
			Mid5FirstCol: edge.toCols[4], Mid5SecondCol: edge.fromCols[5],
			Mid6FirstCol: edge.toCols[5], Mid6SecondCol: edge.fromCols[6],
			Mid7FirstCol: edge.toCols[6], Mid7SecondCol: edge.fromCols[7],
			RHSJoinCol: edge.toCols[7], RHSFilterCol: edge.rhsFilterCol,
		}
		if add {
			idx.JoinRangePath8Edge.Add(fixed, lower, upper, hash)
		} else {
			idx.JoinRangePath8Edge.Remove(fixed, lower, upper, hash)
		}
	}
}
