package subscription

import (
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func collectJoinPathTraversalCandidates(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	add func(QueryHash),
) {
	if committed == nil || resolver == nil {
		return
	}
	idx.joinPathEdge.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		forEachJoinedPathTraversalRHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.joinPathEdge.ForEachHash(edge, v, add)
		})
	})
	idx.joinRangePathEdge.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		forEachJoinedPathTraversalRHSFilterValue(rows, committed, resolver, edge, func(v Value) {
			idx.joinRangePathEdge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedPathTraversalRHSFilterValue(
	rows []types.ProductValue,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge joinPathTraversalEdge,
	fn func(Value),
) {
	values := pathStartValues(rows, edge.fromCols[0])
	if len(values) == 0 {
		return
	}
	lastHop := edge.hopCount() - 1
	for hop := 0; hop < lastHop; hop++ {
		next := make(map[valueKey]Value)
		collectCommittedPathTraversalHopValues(values, committed, resolver, edge.tables[hop+1], edge.toCols[hop], edge.fromCols[hop+1], next)
		if len(next) == 0 {
			return
		}
		values = next
	}
	forEachCommittedPathTraversalRHSFilterValue(values, committed, resolver, edge, fn)
}

func collectJoinPathTraversalFilterDeltaCandidates(
	idx *PruningIndexes,
	table TableID,
	rows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	add func(QueryHash),
) {
	if changeset == nil || len(rows) == 0 {
		return
	}
	idx.joinPathEdge.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		forEachJoinedChangedPathTraversalRHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.joinPathEdge.ForEachHash(edge, v, add)
		})
	})
	idx.joinRangePathEdge.ForEachEdge(table, func(edge joinPathTraversalEdge) {
		forEachJoinedChangedPathTraversalRHSFilterValue(rows, changeset, committed, resolver, edge, func(v Value) {
			idx.joinRangePathEdge.ForEachHash(edge, v, add)
		})
	})
}

func forEachJoinedChangedPathTraversalRHSFilterValue(
	lhsRows []types.ProductValue,
	changeset *store.Changeset,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge joinPathTraversalEdge,
	fn func(Value),
) {
	values := pathStartValues(lhsRows, edge.fromCols[0])
	if len(values) == 0 {
		return
	}
	lastHop := edge.hopCount() - 1
	for hop := 0; hop < lastHop; hop++ {
		next := make(map[valueKey]Value)
		table := edge.tables[hop+1]
		if tc := changeset.Tables[table]; tc != nil {
			collectChangedPathTraversalHopValues(values, edge.toCols[hop], edge.fromCols[hop+1], tc.Inserts, next)
			collectChangedPathTraversalHopValues(values, edge.toCols[hop], edge.fromCols[hop+1], tc.Deletes, next)
		}
		collectCommittedPathTraversalHopValues(values, committed, resolver, table, edge.toCols[hop], edge.fromCols[hop+1], next)
		if len(next) == 0 {
			return
		}
		values = next
	}
	rhsTable := edge.rhsTable()
	if tc := changeset.Tables[rhsTable]; tc != nil {
		forEachChangedPathTraversalRHSFilterValue(values, edge, tc.Inserts, fn)
		forEachChangedPathTraversalRHSFilterValue(values, edge, tc.Deletes, fn)
	}
	forEachCommittedPathTraversalRHSFilterValue(values, committed, resolver, edge, fn)
}

func pathStartValues(rows []types.ProductValue, col ColID) map[valueKey]Value {
	values := make(map[valueKey]Value, len(rows))
	for _, row := range rows {
		if int(col) >= len(row) {
			continue
		}
		values[encodeValueKey(row[col])] = row[col]
	}
	return values
}

func collectChangedPathTraversalHopValues(
	values map[valueKey]Value,
	seekCol ColID,
	valueCol ColID,
	rows []types.ProductValue,
	out map[valueKey]Value,
) {
	for _, row := range rows {
		if int(seekCol) >= len(row) || int(valueCol) >= len(row) {
			continue
		}
		if _, ok := values[encodeValueKey(row[seekCol])]; ok {
			out[encodeValueKey(row[valueCol])] = row[valueCol]
		}
	}
}

func collectCommittedPathTraversalHopValues(
	values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	table TableID,
	seekCol ColID,
	valueCol ColID,
	out map[valueKey]Value,
) {
	if committed == nil || resolver == nil {
		return
	}
	idx, ok := resolver.IndexIDForColumn(table, seekCol)
	if !ok {
		return
	}
	for _, value := range values {
		key := store.NewIndexKey(value)
		for _, rid := range committed.IndexSeek(table, idx, key) {
			row, ok := committed.GetRow(table, rid)
			if !ok || int(valueCol) >= len(row) {
				continue
			}
			out[encodeValueKey(row[valueCol])] = row[valueCol]
		}
	}
}

func forEachChangedPathTraversalRHSFilterValue(
	values map[valueKey]Value,
	edge joinPathTraversalEdge,
	rows []types.ProductValue,
	fn func(Value),
) {
	rhsJoinCol := edge.toCols[edge.hopCount()-1]
	for _, row := range rows {
		if int(rhsJoinCol) >= len(row) || int(edge.rhsFilterCol) >= len(row) {
			continue
		}
		if _, ok := values[encodeValueKey(row[rhsJoinCol])]; ok {
			fn(row[edge.rhsFilterCol])
		}
	}
}

func forEachCommittedPathTraversalRHSFilterValue(
	values map[valueKey]Value,
	committed store.CommittedReadView,
	resolver IndexResolver,
	edge joinPathTraversalEdge,
	fn func(Value),
) {
	if committed == nil || resolver == nil {
		return
	}
	rhsJoinCol := edge.toCols[edge.hopCount()-1]
	rhsIdx, ok := resolver.IndexIDForColumn(edge.rhsTable(), rhsJoinCol)
	if !ok {
		return
	}
	for _, value := range values {
		key := store.NewIndexKey(value)
		for _, rid := range committed.IndexSeek(edge.rhsTable(), rhsIdx, key) {
			row, ok := committed.GetRow(edge.rhsTable(), rid)
			if !ok || int(edge.rhsFilterCol) >= len(row) {
				continue
			}
			fn(row[edge.rhsFilterCol])
		}
	}
}
