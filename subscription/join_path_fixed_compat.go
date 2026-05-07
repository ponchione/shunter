package subscription

func mutateJoinPathTraversalFixedCompatibility(idx *PruningIndexes, edge joinPathTraversalEdge, value Value, hash QueryHash, add bool) {
	switch edge.hopCount() {
	case 2:
		fixed, ok := joinPathTraversalEdgeToJoinPathEdge(edge)
		if ok {
			mutateJoinPathEdgePlacement(idx, fixed, value, hash, add)
		}
	case 3:
		fixed, ok := joinPathTraversalEdgeToJoinPath3Edge(edge)
		if ok {
			mutateJoinPath3EdgePlacement(idx, fixed, value, hash, add)
		}
	case 4:
		fixed, ok := joinPathTraversalEdgeToJoinPath4Edge(edge)
		if ok {
			mutateJoinPath4EdgePlacement(idx, fixed, value, hash, add)
		}
	case 5:
		fixed, ok := joinPathTraversalEdgeToJoinPath5Edge(edge)
		if ok {
			mutateJoinPath5EdgePlacement(idx, fixed, value, hash, add)
		}
	case 6:
		fixed, ok := joinPathTraversalEdgeToJoinPath6Edge(edge)
		if ok {
			mutateJoinPath6EdgePlacement(idx, fixed, value, hash, add)
		}
	case 7:
		fixed, ok := joinPathTraversalEdgeToJoinPath7Edge(edge)
		if ok {
			mutateJoinPath7EdgePlacement(idx, fixed, value, hash, add)
		}
	case 8:
		fixed, ok := joinPathTraversalEdgeToJoinPath8Edge(edge)
		if ok {
			mutateJoinPath8EdgePlacement(idx, fixed, value, hash, add)
		}
	}
}

func mutateJoinRangePathTraversalFixedCompatibility(idx *PruningIndexes, edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash, add bool) {
	switch edge.hopCount() {
	case 2:
		fixed, ok := joinPathTraversalEdgeToJoinPathEdge(edge)
		if ok {
			mutateJoinRangePathEdgePlacement(idx, fixed, lower, upper, hash, add)
		}
	case 3:
		fixed, ok := joinPathTraversalEdgeToJoinPath3Edge(edge)
		if ok {
			mutateJoinRangePath3EdgePlacement(idx, fixed, lower, upper, hash, add)
		}
	case 4:
		fixed, ok := joinPathTraversalEdgeToJoinPath4Edge(edge)
		if ok {
			mutateJoinRangePath4EdgePlacement(idx, fixed, lower, upper, hash, add)
		}
	case 5:
		fixed, ok := joinPathTraversalEdgeToJoinPath5Edge(edge)
		if ok {
			mutateJoinRangePath5EdgePlacement(idx, fixed, lower, upper, hash, add)
		}
	case 6:
		fixed, ok := joinPathTraversalEdgeToJoinPath6Edge(edge)
		if ok {
			mutateJoinRangePath6EdgePlacement(idx, fixed, lower, upper, hash, add)
		}
	case 7:
		fixed, ok := joinPathTraversalEdgeToJoinPath7Edge(edge)
		if ok {
			mutateJoinRangePath7EdgePlacement(idx, fixed, lower, upper, hash, add)
		}
	case 8:
		fixed, ok := joinPathTraversalEdgeToJoinPath8Edge(edge)
		if ok {
			mutateJoinRangePath8EdgePlacement(idx, fixed, lower, upper, hash, add)
		}
	}
}
