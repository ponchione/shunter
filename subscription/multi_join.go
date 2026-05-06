package subscription

import (
	"context"
	"fmt"

	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

func (m *Manager) appendProjectedMultiJoinRows(ctx context.Context, out []types.ProductValue, view store.CommittedReadView, p MultiJoin) ([]types.ProductValue, error) {
	rows, err := multiJoinRowsFromView(ctx, view, p, m.InitialRowLimit)
	if err != nil {
		return nil, err
	}
	return append(out, rows...), nil
}

func evalMultiJoinDelta(dv *DeltaView, p MultiJoin) (inserts, deletes []types.ProductValue) {
	before := multiJoinRowsBefore(dv, p)
	after := multiJoinRowsAfter(dv.CommittedView(), p)
	return diffProjectedRowBags(before, after)
}

func multiJoinRowsAfter(view store.CommittedReadView, p MultiJoin) []types.ProductValue {
	rows, _ := multiJoinRowsFromView(context.Background(), view, p, 0)
	return rows
}

func multiJoinRowsBefore(dv *DeltaView, p MultiJoin) []types.ProductValue {
	rows, _ := collectMultiJoinProjectedRows(context.Background(), p, multiJoinRowsByRelationBefore(dv, p), 0)
	return rows
}

func multiJoinRowsByRelationBefore(dv *DeltaView, p MultiJoin) [][]types.ProductValue {
	rowsByRelation := make([][]types.ProductValue, len(p.Relations))
	for i, rel := range p.Relations {
		rowsByRelation[i] = projectedRowsBefore(dv, rel.Table)
	}
	return rowsByRelation
}

func multiJoinRowsFromView(ctx context.Context, view store.CommittedReadView, p MultiJoin, limit int) ([]types.ProductValue, error) {
	rowsByRelation, err := multiJoinRowsByRelationFromView(ctx, view, p)
	if err != nil {
		return nil, err
	}
	return collectMultiJoinProjectedRows(ctx, p, rowsByRelation, limit)
}

func multiJoinRowsByRelationFromView(ctx context.Context, view store.CommittedReadView, p MultiJoin) ([][]types.ProductValue, error) {
	if view == nil {
		return nil, nil
	}
	rowsByRelation := make([][]types.ProductValue, len(p.Relations))
	for i, rel := range p.Relations {
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		rowsByRelation[i] = tableRowsAfter(view, rel.Table)
	}
	return rowsByRelation, nil
}

func collectMultiJoinProjectedRows(ctx context.Context, p MultiJoin, rowsByRelation [][]types.ProductValue, limit int) ([]types.ProductValue, error) {
	var out []types.ProductValue
	err := visitMultiJoinTuples(ctx, p, rowsByRelation, func(tuple []types.ProductValue) error {
		if limit > 0 && len(out) >= limit {
			return fmt.Errorf("%w: cap=%d", ErrInitialRowLimit, limit)
		}
		out = append(out, tuple[p.ProjectedRelation])
		return nil
	})
	return out, err
}

func visitMultiJoinTuples(ctx context.Context, p MultiJoin, rowsByRelation [][]types.ProductValue, visit func([]types.ProductValue) error) error {
	if len(p.Relations) == 0 || p.ProjectedRelation < 0 || p.ProjectedRelation >= len(p.Relations) {
		return nil
	}
	tuple := make([]types.ProductValue, len(p.Relations))
	var walk func(int) error
	walk = func(depth int) error {
		if err := ctxErr(ctx); err != nil {
			return err
		}
		if depth == len(p.Relations) {
			if !matchMultiJoinTuple(p.Filter, p.Relations, tuple) {
				return nil
			}
			if visit != nil {
				return visit(tuple)
			}
			return nil
		}
		if depth >= len(rowsByRelation) {
			return nil
		}
		for _, row := range rowsByRelation[depth] {
			tuple[depth] = row
			if !multiJoinConditionsMatchPrefix(p.Conditions, tuple, depth) {
				continue
			}
			if err := walk(depth + 1); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(0)
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func multiJoinConditionsMatchPrefix(conditions []MultiJoinCondition, tuple []types.ProductValue, depth int) bool {
	for _, condition := range conditions {
		if condition.Left.Relation > depth || condition.Right.Relation > depth {
			continue
		}
		if !multiJoinConditionValuesEqual(tuple, condition.Left, condition.Right) {
			return false
		}
	}
	return true
}

func multiJoinConditionValuesEqual(tuple []types.ProductValue, left, right MultiJoinColumnRef) bool {
	leftValue, ok := multiJoinConditionColumnValue(tuple, left)
	if !ok {
		return false
	}
	rightValue, ok := multiJoinConditionColumnValue(tuple, right)
	if !ok {
		return false
	}
	return leftValue.Equal(rightValue)
}

func multiJoinConditionColumnValue(tuple []types.ProductValue, ref MultiJoinColumnRef) (Value, bool) {
	if ref.Relation < 0 || ref.Relation >= len(tuple) {
		return Value{}, false
	}
	row := tuple[ref.Relation]
	if int(ref.Column) >= len(row) {
		return Value{}, false
	}
	return row[ref.Column], true
}

func matchMultiJoinTuple(pred Predicate, relations []MultiJoinRelation, tuple []types.ProductValue) bool {
	if pred == nil {
		return true
	}
	switch p := pred.(type) {
	case ColEq:
		row, ok := multiJoinPredicateRow(p.Table, p.Alias, relations, tuple)
		if !ok || int(p.Column) >= len(row) {
			return false
		}
		return row[p.Column].Equal(p.Value)
	case ColNe:
		row, ok := multiJoinPredicateRow(p.Table, p.Alias, relations, tuple)
		if !ok || int(p.Column) >= len(row) {
			return false
		}
		return !row[p.Column].Equal(p.Value)
	case ColRange:
		row, ok := multiJoinPredicateRow(p.Table, p.Alias, relations, tuple)
		if !ok || int(p.Column) >= len(row) {
			return false
		}
		return matchBounds(row[p.Column], p.Lower, p.Upper)
	case ColEqCol:
		left, ok := multiJoinPredicateRow(p.LeftTable, p.LeftAlias, relations, tuple)
		if !ok || int(p.LeftColumn) >= len(left) {
			return false
		}
		right, ok := multiJoinPredicateRow(p.RightTable, p.RightAlias, relations, tuple)
		if !ok || int(p.RightColumn) >= len(right) {
			return false
		}
		return left[p.LeftColumn].Equal(right[p.RightColumn])
	case And:
		return matchMultiJoinTuple(p.Left, relations, tuple) && matchMultiJoinTuple(p.Right, relations, tuple)
	case Or:
		return matchMultiJoinTuple(p.Left, relations, tuple) || matchMultiJoinTuple(p.Right, relations, tuple)
	case AllRows:
		return true
	case NoRows:
		return false
	case Join:
		return true
	case CrossJoin:
		return true
	case MultiJoin:
		return true
	}
	return false
}

func multiJoinPredicateRow(table TableID, alias uint8, relations []MultiJoinRelation, tuple []types.ProductValue) (types.ProductValue, bool) {
	match := -1
	count := 0
	for i, rel := range relations {
		if rel.Table != table {
			continue
		}
		count++
		match = i
	}
	if count == 1 {
		if match < 0 || match >= len(tuple) {
			return nil, false
		}
		return tuple[match], true
	}
	for i, rel := range relations {
		if rel.Table == table && rel.Alias == alias {
			if i >= len(tuple) {
				return nil, false
			}
			return tuple[i], true
		}
	}
	return nil, false
}
