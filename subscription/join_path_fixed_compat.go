package subscription

type joinPathFixedValueCompatibility func(*PruningIndexes, joinPathTraversalEdge, Value, QueryHash, bool)
type joinPathFixedRangeCompatibility func(*PruningIndexes, joinPathTraversalEdge, Bound, Bound, QueryHash, bool)

var joinPathFixedValueCompatibilityByHop = [...]joinPathFixedValueCompatibility{
	2: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPathEdge, mutateJoinPathEdgePlacement),
	3: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath3Edge, mutateJoinPath3EdgePlacement),
	4: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath4Edge, mutateJoinPath4EdgePlacement),
	5: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath5Edge, mutateJoinPath5EdgePlacement),
	6: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath6Edge, mutateJoinPath6EdgePlacement),
	7: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath7Edge, mutateJoinPath7EdgePlacement),
	8: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath8Edge, mutateJoinPath8EdgePlacement),
}

var joinPathFixedRangeCompatibilityByHop = [...]joinPathFixedRangeCompatibility{
	2: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPathEdge, mutateJoinRangePathEdgePlacement),
	3: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath3Edge, mutateJoinRangePath3EdgePlacement),
	4: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath4Edge, mutateJoinRangePath4EdgePlacement),
	5: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath5Edge, mutateJoinRangePath5EdgePlacement),
	6: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath6Edge, mutateJoinRangePath6EdgePlacement),
	7: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath7Edge, mutateJoinRangePath7EdgePlacement),
	8: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath8Edge, mutateJoinRangePath8EdgePlacement),
}

func mutateJoinPathTraversalFixedCompatibility(idx *PruningIndexes, edge joinPathTraversalEdge, value Value, hash QueryHash, add bool) {
	hopCount := edge.hopCount()
	if hopCount >= len(joinPathFixedValueCompatibilityByHop) {
		return
	}
	if compatibility := joinPathFixedValueCompatibilityByHop[hopCount]; compatibility != nil {
		compatibility(idx, edge, value, hash, add)
	}
}

func mutateJoinRangePathTraversalFixedCompatibility(idx *PruningIndexes, edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash, add bool) {
	hopCount := edge.hopCount()
	if hopCount >= len(joinPathFixedRangeCompatibilityByHop) {
		return
	}
	if compatibility := joinPathFixedRangeCompatibilityByHop[hopCount]; compatibility != nil {
		compatibility(idx, edge, lower, upper, hash, add)
	}
}

func newJoinPathFixedValueCompatibility[E any](
	fromTraversal func(joinPathTraversalEdge) (E, bool),
	mutate func(*PruningIndexes, E, Value, QueryHash, bool),
) joinPathFixedValueCompatibility {
	return func(idx *PruningIndexes, edge joinPathTraversalEdge, value Value, hash QueryHash, add bool) {
		if fixed, ok := fromTraversal(edge); ok {
			mutate(idx, fixed, value, hash, add)
		}
	}
}

func newJoinPathFixedRangeCompatibility[E any](
	fromTraversal func(joinPathTraversalEdge) (E, bool),
	mutate func(*PruningIndexes, E, Bound, Bound, QueryHash, bool),
) joinPathFixedRangeCompatibility {
	return func(idx *PruningIndexes, edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash, add bool) {
		if fixed, ok := fromTraversal(edge); ok {
			mutate(idx, fixed, lower, upper, hash, add)
		}
	}
}
