package subscription

type joinPathFixedValueCompatibility func(*PruningIndexes, joinPathTraversalEdge, Value, QueryHash, bool)
type joinPathFixedRangeCompatibility func(*PruningIndexes, joinPathTraversalEdge, Bound, Bound, QueryHash, bool)

type joinPathFixedValueTarget[E any] interface {
	Add(E, Value, QueryHash)
	Remove(E, Value, QueryHash)
}

type joinPathFixedRangeTarget[E any] interface {
	Add(E, Bound, Bound, QueryHash)
	Remove(E, Bound, Bound, QueryHash)
}

var joinPathFixedValueCompatibilityByHop = [...]joinPathFixedValueCompatibility{
	2: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPathEdge, func(idx *PruningIndexes) joinPathFixedValueTarget[JoinPathEdge] {
		return idx.JoinPathEdge
	}),
	3: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath3Edge, func(idx *PruningIndexes) joinPathFixedValueTarget[JoinPath3Edge] {
		return idx.JoinPath3Edge
	}),
	4: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath4Edge, func(idx *PruningIndexes) joinPathFixedValueTarget[JoinPath4Edge] {
		return idx.JoinPath4Edge
	}),
	5: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath5Edge, func(idx *PruningIndexes) joinPathFixedValueTarget[JoinPath5Edge] {
		return idx.JoinPath5Edge
	}),
	6: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath6Edge, func(idx *PruningIndexes) joinPathFixedValueTarget[JoinPath6Edge] {
		return idx.JoinPath6Edge
	}),
	7: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath7Edge, func(idx *PruningIndexes) joinPathFixedValueTarget[JoinPath7Edge] {
		return idx.JoinPath7Edge
	}),
	8: newJoinPathFixedValueCompatibility(joinPathTraversalEdgeToJoinPath8Edge, func(idx *PruningIndexes) joinPathFixedValueTarget[JoinPath8Edge] {
		return idx.JoinPath8Edge
	}),
}

var joinPathFixedRangeCompatibilityByHop = [...]joinPathFixedRangeCompatibility{
	2: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPathEdge, func(idx *PruningIndexes) joinPathFixedRangeTarget[JoinPathEdge] {
		return idx.JoinRangePathEdge
	}),
	3: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath3Edge, func(idx *PruningIndexes) joinPathFixedRangeTarget[JoinPath3Edge] {
		return idx.JoinRangePath3Edge
	}),
	4: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath4Edge, func(idx *PruningIndexes) joinPathFixedRangeTarget[JoinPath4Edge] {
		return idx.JoinRangePath4Edge
	}),
	5: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath5Edge, func(idx *PruningIndexes) joinPathFixedRangeTarget[JoinPath5Edge] {
		return idx.JoinRangePath5Edge
	}),
	6: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath6Edge, func(idx *PruningIndexes) joinPathFixedRangeTarget[JoinPath6Edge] {
		return idx.JoinRangePath6Edge
	}),
	7: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath7Edge, func(idx *PruningIndexes) joinPathFixedRangeTarget[JoinPath7Edge] {
		return idx.JoinRangePath7Edge
	}),
	8: newJoinPathFixedRangeCompatibility(joinPathTraversalEdgeToJoinPath8Edge, func(idx *PruningIndexes) joinPathFixedRangeTarget[JoinPath8Edge] {
		return idx.JoinRangePath8Edge
	}),
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
	target func(*PruningIndexes) joinPathFixedValueTarget[E],
) joinPathFixedValueCompatibility {
	return func(idx *PruningIndexes, edge joinPathTraversalEdge, value Value, hash QueryHash, add bool) {
		if fixed, ok := fromTraversal(edge); ok {
			index := target(idx)
			if add {
				index.Add(fixed, value, hash)
				return
			}
			index.Remove(fixed, value, hash)
		}
	}
}

func newJoinPathFixedRangeCompatibility[E any](
	fromTraversal func(joinPathTraversalEdge) (E, bool),
	target func(*PruningIndexes) joinPathFixedRangeTarget[E],
) joinPathFixedRangeCompatibility {
	return func(idx *PruningIndexes, edge joinPathTraversalEdge, lower, upper Bound, hash QueryHash, add bool) {
		if fixed, ok := fromTraversal(edge); ok {
			index := target(idx)
			if add {
				index.Add(fixed, lower, upper, hash)
				return
			}
			index.Remove(fixed, lower, upper, hash)
		}
	}
}
