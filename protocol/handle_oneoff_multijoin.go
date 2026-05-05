package protocol

import (
	"context"
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/subscription"
	"github.com/ponchione/shunter/types"
)

func executeCompiledSQLMultiJoin(ctx context.Context, query compiledSQLQuery, stateAccess CommittedStateAccess, resolver schema.IndexResolver) (SQLQueryResult, error) {
	multi := query.MultiJoin
	if multi == nil {
		return SQLQueryResult{}, fmt.Errorf("multi-join metadata must not be nil")
	}
	view := stateAccess.Snapshot()
	defer view.Close()

	rowLimit := oneOffRowLimit(query.Limit)
	rowOffset := oneOffRowOffset(query.Offset)
	scanLimit := rowLimit
	if len(query.OrderBy) != 0 {
		scanLimit = -1
	} else {
		scanLimit = oneOffScanLimit(rowOffset, rowLimit)
	}
	var rows []types.ProductValue
	if query.Aggregate != nil {
		aggregateValue, err := evaluateOneOffMultiJoinAggregate(ctx, view, multi, resolver, query.Aggregate)
		if err != nil {
			return SQLQueryResult{}, err
		}
		rows = sliceOneOffRows([]types.ProductValue{{aggregateValue}}, rowOffset, rowLimit)
	} else if rowLimit != 0 {
		if len(query.OrderBy) != 0 {
			ordered, err := evaluateOneOffMultiJoinOrderedRows(ctx, view, multi, resolver, query.ProjectionColumns, query.OrderBy)
			if err != nil {
				return SQLQueryResult{}, err
			}
			rows = materializeOrderedOneOffRows(ordered, query.OrderBy, rowOffset, rowLimit)
		} else {
			err := visitOneOffMultiJoinTuples(ctx, view, multi, resolver, func(tuple []types.ProductValue) bool {
				rows = append(rows, projectOneOffMultiJoinTuple(tuple, multi, query.ProjectionColumns))
				return !oneOffLimitReached(len(rows), scanLimit)
			})
			if err != nil {
				return SQLQueryResult{}, err
			}
			rows = sliceOneOffRows(rows, rowOffset, rowLimit)
		}
	}
	return SQLQueryResult{TableName: query.TableName, Rows: rows}, nil
}

func evaluateOneOffMultiJoinOrderedRows(ctx context.Context, view store.CommittedReadView, multi *compiledSQLMultiJoin, resolver schema.IndexResolver, columns []compiledSQLProjectionColumn, orderBy []compiledSQLOrderBy) ([]orderedOneOffRow, error) {
	var rows []orderedOneOffRow
	var orderErr error
	err := visitOneOffMultiJoinTuples(ctx, view, multi, resolver, func(tuple []types.ProductValue) bool {
		key, err := orderKeysFromMultiJoinTuple(tuple, orderBy)
		if err != nil {
			orderErr = err
			return false
		}
		rows = append(rows, orderedOneOffRow{
			row: projectOneOffMultiJoinTuple(tuple, multi, columns),
			key: key,
		})
		return true
	})
	if err != nil {
		return nil, err
	}
	if orderErr != nil {
		return nil, orderErr
	}
	return rows, nil
}

func visitOneOffMultiJoinTuples(ctx context.Context, view store.CommittedReadView, multi *compiledSQLMultiJoin, resolver schema.IndexResolver, visit func([]types.ProductValue) bool) error {
	tuple := make([]types.ProductValue, len(multi.Relations))
	var walk func(int) (bool, error)
	walk = func(depth int) (bool, error) {
		if depth == len(multi.Relations) {
			if matchCompiledSQLMultiPredicate(multi.Filter, tuple) {
				copied := make([]types.ProductValue, len(tuple))
				copy(copied, tuple)
				return visit(copied), nil
			}
			return true, nil
		}
		rel := multi.Relations[depth]
		var walkErr error
		keepGoing, err := visitOneOffMultiJoinCandidateRows(ctx, view, multi, resolver, tuple, depth, func(row types.ProductValue) bool {
			if rel.Visibility != nil && !subscription.MatchRowSide(rel.Visibility, rel.Table, rel.Alias, row) {
				return true
			}
			tuple[depth] = row
			if !multiJoinPrefixConditionsMatch(multi.Conditions, tuple, depth) {
				return true
			}
			var next bool
			next, walkErr = walk(depth + 1)
			return walkErr == nil && next
		})
		if err != nil {
			return false, err
		}
		if walkErr != nil {
			return false, walkErr
		}
		if !keepGoing {
			return false, nil
		}
		return true, nil
	}
	_, err := walk(0)
	return err
}

type multiJoinIndexedProbe struct {
	known compiledSQLMultiColumnRef
	index schema.IndexID
}

func multiJoinIndexedProbeForDepth(multi *compiledSQLMultiJoin, resolver schema.IndexResolver, depth int) (multiJoinIndexedProbe, bool) {
	if resolver == nil || depth <= 0 || depth >= len(multi.Relations) {
		return multiJoinIndexedProbe{}, false
	}
	rel := multi.Relations[depth]
	for _, condition := range multi.Conditions {
		if condition.Left.Relation == depth && condition.Right.Relation < depth {
			if idx, ok := resolver.IndexIDForColumn(rel.Table, condition.Left.Column.column); ok {
				return multiJoinIndexedProbe{known: condition.Right, index: idx}, true
			}
		}
		if condition.Right.Relation == depth && condition.Left.Relation < depth {
			if idx, ok := resolver.IndexIDForColumn(rel.Table, condition.Right.Column.column); ok {
				return multiJoinIndexedProbe{known: condition.Left, index: idx}, true
			}
		}
	}
	return multiJoinIndexedProbe{}, false
}

func visitOneOffMultiJoinCandidateRows(ctx context.Context, view store.CommittedReadView, multi *compiledSQLMultiJoin, resolver schema.IndexResolver, tuple []types.ProductValue, depth int, visit func(types.ProductValue) bool) (bool, error) {
	rel := multi.Relations[depth]
	if probe, ok := multiJoinIndexedProbeForDepth(multi, resolver, depth); ok {
		value, ok := multiJoinColumnValue(tuple, probe.known)
		if !ok {
			return true, nil
		}
		key := store.NewIndexKey(value)
		for _, rid := range view.IndexSeek(rel.Table, probe.index, key) {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			row, ok := view.GetRow(rel.Table, rid)
			if !ok {
				continue
			}
			if !visit(row) {
				return false, nil
			}
		}
		return true, nil
	}
	for _, row := range view.TableScan(rel.Table) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !visit(row) {
			return false, nil
		}
	}
	return true, nil
}

func multiJoinPrefixConditionsMatch(conditions []compiledSQLMultiJoinCondition, tuple []types.ProductValue, depth int) bool {
	for _, condition := range conditions {
		if condition.Left.Relation > depth || condition.Right.Relation > depth {
			continue
		}
		if !multiJoinColumnValuesEqual(tuple, condition.Left, condition.Right) {
			return false
		}
	}
	return true
}

func multiJoinColumnValuesEqual(tuple []types.ProductValue, left, right compiledSQLMultiColumnRef) bool {
	leftValue, ok := multiJoinColumnValue(tuple, left)
	if !ok {
		return false
	}
	rightValue, ok := multiJoinColumnValue(tuple, right)
	if !ok {
		return false
	}
	return leftValue.Equal(rightValue)
}

func projectOneOffMultiJoinTuple(tuple []types.ProductValue, multi *compiledSQLMultiJoin, columns []compiledSQLProjectionColumn) types.ProductValue {
	if len(columns) == 0 {
		return tuple[multi.ProjectedRelation]
	}
	out := make(types.ProductValue, 0, len(columns))
	for _, col := range columns {
		relation, ok := multiJoinRelationIndex(multi.Relations, col.Table, col.Alias)
		if !ok {
			continue
		}
		row := tuple[relation]
		idx := col.Schema.Index
		if idx < 0 || idx >= len(row) {
			continue
		}
		out = append(out, row[idx])
	}
	return out
}

func orderKeysFromMultiJoinTuple(tuple []types.ProductValue, orderBy []compiledSQLOrderBy) ([]types.Value, error) {
	keys := make([]types.Value, len(orderBy))
	for i, term := range orderBy {
		if term.Relation < 0 || term.Relation >= len(tuple) {
			return nil, fmt.Errorf("ORDER BY column %q is not from the projected table", term.Column.Schema.Name)
		}
		row := tuple[term.Relation]
		idx := term.Column.Schema.Index
		if idx < 0 || idx >= len(row) {
			return nil, fmt.Errorf("ORDER BY column %q is missing from row", term.Column.Schema.Name)
		}
		keys[i] = row[idx]
	}
	return keys, nil
}

func evaluateOneOffMultiJoinAggregate(ctx context.Context, view store.CommittedReadView, multi *compiledSQLMultiJoin, resolver schema.IndexResolver, aggregate *compiledSQLAggregate) (types.Value, error) {
	if aggregate == nil {
		return types.Value{}, fmt.Errorf("aggregate metadata must not be nil")
	}
	switch aggregate.Func {
	case "COUNT":
		count, err := countOneOffMultiJoinAggregate(ctx, view, multi, resolver, aggregate)
		if err != nil {
			return types.Value{}, err
		}
		return types.NewUint64(count), nil
	case "SUM":
		return sumOneOffMultiJoinAggregate(ctx, view, multi, resolver, aggregate)
	default:
		return types.Value{}, fmt.Errorf("aggregate %q not supported", aggregate.Func)
	}
}

func countOneOffMultiJoinAggregate(ctx context.Context, view store.CommittedReadView, multi *compiledSQLMultiJoin, resolver schema.IndexResolver, aggregate *compiledSQLAggregate) (uint64, error) {
	if aggregate == nil || aggregate.Argument == nil {
		var count uint64
		err := visitOneOffMultiJoinTuples(ctx, view, multi, resolver, func([]types.ProductValue) bool {
			count++
			return true
		})
		return count, err
	}
	argument := *aggregate.Argument
	if aggregate.Distinct {
		seen := newOneOffDistinctValueSet()
		err := visitOneOffMultiJoinTuples(ctx, view, multi, resolver, func(tuple []types.ProductValue) bool {
			value, ok := oneOffMultiJoinColumnValue(tuple, multi, argument)
			if ok {
				seen.add(value)
			}
			return true
		})
		return seen.count(), err
	}
	var count uint64
	err := visitOneOffMultiJoinTuples(ctx, view, multi, resolver, func(tuple []types.ProductValue) bool {
		if _, ok := oneOffMultiJoinColumnValue(tuple, multi, argument); ok {
			count++
		}
		return true
	})
	return count, err
}

func sumOneOffMultiJoinAggregate(ctx context.Context, view store.CommittedReadView, multi *compiledSQLMultiJoin, resolver schema.IndexResolver, aggregate *compiledSQLAggregate) (types.Value, error) {
	if aggregate == nil || aggregate.Argument == nil {
		return types.Value{}, fmt.Errorf("SUM aggregate requires a column argument")
	}
	acc := newOneOffSumAccumulator(aggregate.ResultColumn.Type)
	argument := *aggregate.Argument
	err := visitOneOffMultiJoinTuples(ctx, view, multi, resolver, func(tuple []types.ProductValue) bool {
		value, ok := oneOffMultiJoinColumnValue(tuple, multi, argument)
		if !ok {
			return true
		}
		if err := acc.add(value); err != nil {
			return false
		}
		return true
	})
	if err != nil {
		return types.Value{}, err
	}
	return acc.value()
}

func oneOffMultiJoinColumnValue(tuple []types.ProductValue, multi *compiledSQLMultiJoin, column compiledSQLProjectionColumn) (types.Value, bool) {
	relation, ok := multiJoinRelationIndex(multi.Relations, column.Table, column.Alias)
	if !ok {
		return types.Value{}, false
	}
	row := tuple[relation]
	idx := column.Schema.Index
	if idx < 0 || idx >= len(row) {
		return types.Value{}, false
	}
	return row[idx], true
}

func matchCompiledSQLMultiPredicate(pred *compiledSQLMultiPredicate, tuple []types.ProductValue) bool {
	if pred == nil {
		return true
	}
	switch pred.Kind {
	case compiledSQLMultiPredicateTrue:
		return true
	case compiledSQLMultiPredicateFalse:
		return false
	case compiledSQLMultiPredicateComparison:
		value, ok := multiJoinColumnValue(tuple, pred.Column)
		if !ok {
			return false
		}
		return compareOneOffMultiJoinValue(value, pred.Op, pred.Value)
	case compiledSQLMultiPredicateColumnComparison:
		return multiJoinColumnValuesEqual(tuple, pred.LeftColumn, pred.RightColumn)
	case compiledSQLMultiPredicateAnd:
		return matchCompiledSQLMultiPredicate(pred.Left, tuple) && matchCompiledSQLMultiPredicate(pred.Right, tuple)
	case compiledSQLMultiPredicateOr:
		return matchCompiledSQLMultiPredicate(pred.Left, tuple) || matchCompiledSQLMultiPredicate(pred.Right, tuple)
	default:
		return false
	}
}

func multiJoinColumnValue(tuple []types.ProductValue, ref compiledSQLMultiColumnRef) (types.Value, bool) {
	if ref.Relation < 0 || ref.Relation >= len(tuple) {
		return types.Value{}, false
	}
	row := tuple[ref.Relation]
	idx := int(ref.Column.column)
	if idx < 0 || idx >= len(row) {
		return types.Value{}, false
	}
	return row[idx], true
}

func compareOneOffMultiJoinValue(left types.Value, op string, right types.Value) bool {
	switch op {
	case "", "=":
		return left.Equal(right)
	case "!=", "<>":
		return !left.Equal(right)
	case ">":
		return left.Compare(right) > 0
	case ">=":
		return left.Compare(right) >= 0
	case "<":
		return left.Compare(right) < 0
	case "<=":
		return left.Compare(right) <= 0
	default:
		return false
	}
}
