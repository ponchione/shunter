package subscription

import (
	"context"
	"fmt"

	"github.com/ponchione/shunter/internal/valueagg"
	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// AggregateFunc names a live aggregate function supported by the subscription
// manager.
type AggregateFunc string

const (
	// AggregateCount counts rows in a live aggregate view.
	AggregateCount AggregateFunc = "COUNT"
	// AggregateSum sums a numeric column in a live aggregate view.
	AggregateSum AggregateFunc = "SUM"
)

// AggregateColumn identifies the optional aggregate column argument.
type AggregateColumn struct {
	Schema schema.ColumnSchema
	Table  TableID
	Column ColID
	Alias  uint8
}

// Aggregate describes the one-row result shape for a live aggregate view.
type Aggregate struct {
	Func         AggregateFunc
	Argument     *AggregateColumn
	Distinct     bool
	ResultColumn schema.ColumnSchema
}

func copyAggregate(in *Aggregate) *Aggregate {
	if in == nil {
		return nil
	}
	out := *in
	if in.Argument != nil {
		arg := *in.Argument
		out.Argument = &arg
	}
	return &out
}

// ValidateAggregate checks the narrow executable live aggregate surface.
func ValidateAggregate(pred Predicate, aggregate *Aggregate, s SchemaLookup) error {
	if aggregate == nil {
		return nil
	}
	if s == nil {
		return fmt.Errorf("%w: aggregate schema lookup is nil", ErrInvalidPredicate)
	}
	if err := validateAggregateResultColumn(aggregate); err != nil {
		return err
	}
	if join, ok := pred.(Join); ok {
		return validateJoinAggregate(join, aggregate, s)
	}
	if cross, ok := pred.(CrossJoin); ok {
		return validateCrossJoinAggregate(cross, aggregate, s)
	}
	if multi, ok := pred.(MultiJoin); ok {
		return validateMultiJoinAggregate(multi, aggregate, s)
	}
	table, ok := aggregatePredicateTable(pred)
	if !ok {
		return fmt.Errorf("%w: live aggregate views require one referenced table", ErrInvalidPredicate)
	}
	switch aggregate.Func {
	case AggregateCount:
		return validateCountAggregate(table, aggregate, s)
	case AggregateSum:
		return validateSumAggregate(table, aggregate, s)
	default:
		return fmt.Errorf("%w: live aggregate views support COUNT and SUM only", ErrInvalidPredicate)
	}
}

func validateAggregateResultColumn(aggregate *Aggregate) error {
	if aggregate.ResultColumn.Index != 0 {
		return fmt.Errorf("%w: aggregate result schema index must be 0", ErrInvalidPredicate)
	}
	if aggregate.ResultColumn.Name == "" {
		return fmt.Errorf("%w: aggregate result column name must not be empty", ErrInvalidPredicate)
	}
	return nil
}

func validateJoinAggregate(join Join, aggregate *Aggregate, s SchemaLookup) error {
	if err := validateJoin(join, s, validateOptions{requireJoinIndex: true}); err != nil {
		return err
	}
	switch aggregate.Func {
	case AggregateCount:
		return validateJoinCountAggregate(join, aggregate, s)
	case AggregateSum:
		return validateJoinSumAggregate(join, aggregate, s)
	default:
		return fmt.Errorf("%w: live aggregate views support COUNT and SUM only", ErrInvalidPredicate)
	}
}

func validateCrossJoinAggregate(cross CrossJoin, aggregate *Aggregate, s SchemaLookup) error {
	if err := validateCrossJoin(cross, s, validateOptions{requireJoinIndex: true}); err != nil {
		return err
	}
	switch aggregate.Func {
	case AggregateCount:
		return validateCrossJoinCountAggregate(cross, aggregate, s)
	case AggregateSum:
		return validateCrossJoinSumAggregate(cross, aggregate, s)
	default:
		return fmt.Errorf("%w: live aggregate views support COUNT and SUM only", ErrInvalidPredicate)
	}
}

func validateMultiJoinAggregate(multi MultiJoin, aggregate *Aggregate, s SchemaLookup) error {
	if err := validateMultiJoin(multi, s, validateOptions{requireJoinIndex: true}); err != nil {
		return err
	}
	switch aggregate.Func {
	case AggregateCount:
		return validateMultiJoinCountAggregate(multi, aggregate, s)
	case AggregateSum:
		return validateMultiJoinSumAggregate(multi, aggregate, s)
	default:
		return fmt.Errorf("%w: live aggregate views support COUNT and SUM only", ErrInvalidPredicate)
	}
}

func validateJoinCountAggregate(join Join, aggregate *Aggregate, s SchemaLookup) error {
	return validateCountAggregateWithArgument(aggregate, func(arg *AggregateColumn) error {
		return validateJoinAggregateArgument(join, "COUNT(column)", arg, s)
	})
}

func validateCrossJoinCountAggregate(cross CrossJoin, aggregate *Aggregate, s SchemaLookup) error {
	return validateCountAggregateWithArgument(aggregate, func(arg *AggregateColumn) error {
		return validateCrossJoinAggregateArgument(cross, "COUNT(column)", arg, s)
	})
}

func validateMultiJoinCountAggregate(multi MultiJoin, aggregate *Aggregate, s SchemaLookup) error {
	return validateCountAggregateWithArgument(aggregate, func(arg *AggregateColumn) error {
		return validateMultiJoinAggregateArgument(multi, "COUNT(column)", arg, s)
	})
}

func validateCountAggregate(table TableID, aggregate *Aggregate, s SchemaLookup) error {
	return validateCountAggregateWithArgument(aggregate, func(arg *AggregateColumn) error {
		return validateAggregateArgument(table, "COUNT(column)", arg, s)
	})
}

func validateCountAggregateWithArgument(aggregate *Aggregate, validateArg func(*AggregateColumn) error) error {
	if err := validateCountResult(aggregate); err != nil {
		return err
	}
	if aggregate.Argument == nil {
		if aggregate.Distinct {
			return fmt.Errorf("%w: COUNT(DISTINCT ...) aggregate requires a column argument", ErrInvalidPredicate)
		}
		return nil
	}
	return validateArg(aggregate.Argument)
}

func validateCountResult(aggregate *Aggregate) error {
	if aggregate.ResultColumn.Type != types.KindUint64 {
		return fmt.Errorf("%w: COUNT aggregate result kind must be Uint64", ErrInvalidPredicate)
	}
	if aggregate.ResultColumn.Nullable {
		return fmt.Errorf("%w: COUNT aggregate result must be non-nullable", ErrInvalidPredicate)
	}
	return nil
}

func validateSumAggregate(table TableID, aggregate *Aggregate, s SchemaLookup) error {
	return validateSumAggregateWithArgument(aggregate, func(arg *AggregateColumn) error {
		return validateAggregateArgument(table, "SUM(column)", arg, s)
	})
}

func validateJoinSumAggregate(join Join, aggregate *Aggregate, s SchemaLookup) error {
	return validateSumAggregateWithArgument(aggregate, func(arg *AggregateColumn) error {
		return validateJoinAggregateArgument(join, "SUM(column)", arg, s)
	})
}

func validateCrossJoinSumAggregate(cross CrossJoin, aggregate *Aggregate, s SchemaLookup) error {
	return validateSumAggregateWithArgument(aggregate, func(arg *AggregateColumn) error {
		return validateCrossJoinAggregateArgument(cross, "SUM(column)", arg, s)
	})
}

func validateMultiJoinSumAggregate(multi MultiJoin, aggregate *Aggregate, s SchemaLookup) error {
	return validateSumAggregateWithArgument(aggregate, func(arg *AggregateColumn) error {
		return validateMultiJoinAggregateArgument(multi, "SUM(column)", arg, s)
	})
}

func validateSumAggregateWithArgument(aggregate *Aggregate, validateArg func(*AggregateColumn) error) error {
	if aggregate.Distinct {
		return fmt.Errorf("%w: live aggregate views do not support SUM(DISTINCT ...)", ErrInvalidPredicate)
	}
	if aggregate.Argument == nil {
		return fmt.Errorf("%w: SUM aggregate requires a column argument", ErrInvalidPredicate)
	}
	if err := validateArg(aggregate.Argument); err != nil {
		return err
	}
	wantKind, ok := sumAggregateResultKind(aggregate.Argument.Schema.Type)
	if !ok {
		return fmt.Errorf("%w: SUM aggregate only supports integer and float columns", ErrInvalidPredicate)
	}
	if aggregate.ResultColumn.Type != wantKind {
		return fmt.Errorf("%w: SUM aggregate result kind must be %s", ErrInvalidPredicate, wantKind)
	}
	if aggregate.ResultColumn.Nullable != aggregate.Argument.Schema.Nullable {
		return fmt.Errorf("%w: SUM aggregate result nullability must match source column", ErrInvalidPredicate)
	}
	return nil
}

func validateAggregateArgument(table TableID, label string, arg *AggregateColumn, s SchemaLookup) error {
	if arg == nil {
		return fmt.Errorf("%w: %s argument must not be nil", ErrInvalidPredicate, label)
	}
	if arg.Table != table || arg.Alias != 0 {
		return fmt.Errorf("%w: %s argument must come from the aggregate table", ErrInvalidPredicate, label)
	}
	return validateAggregateArgumentSchema(arg, s)
}

func validateJoinAggregateArgument(join Join, label string, arg *AggregateColumn, s SchemaLookup) error {
	if arg == nil {
		return fmt.Errorf("%w: %s argument must not be nil", ErrInvalidPredicate, label)
	}
	if !aggregateArgumentMatchesJoin(join, arg) {
		return fmt.Errorf("%w: %s argument must come from a joined relation", ErrInvalidPredicate, label)
	}
	return validateAggregateArgumentSchema(arg, s)
}

func validateCrossJoinAggregateArgument(cross CrossJoin, label string, arg *AggregateColumn, s SchemaLookup) error {
	if arg == nil {
		return fmt.Errorf("%w: %s argument must not be nil", ErrInvalidPredicate, label)
	}
	if !aggregateArgumentMatchesCrossJoin(cross, arg) {
		return fmt.Errorf("%w: %s argument must come from a joined relation", ErrInvalidPredicate, label)
	}
	return validateAggregateArgumentSchema(arg, s)
}

func validateMultiJoinAggregateArgument(multi MultiJoin, label string, arg *AggregateColumn, s SchemaLookup) error {
	if arg == nil {
		return fmt.Errorf("%w: %s argument must not be nil", ErrInvalidPredicate, label)
	}
	if !aggregateArgumentMatchesMultiJoin(multi, arg) {
		return fmt.Errorf("%w: %s argument must come from a joined relation", ErrInvalidPredicate, label)
	}
	return validateAggregateArgumentSchema(arg, s)
}

func validateAggregateArgumentSchema(arg *AggregateColumn, s SchemaLookup) error {
	return validateDeclaredColumnSchema("aggregate argument", arg.Table, arg.Column, arg.Schema, s)
}

func aggregateArgumentMatchesJoin(join Join, arg *AggregateColumn) bool {
	return aggregateArgumentMatchesRelationPair(join.Left, join.LeftAlias, join.Right, join.RightAlias, arg)
}

func aggregateArgumentMatchesCrossJoin(cross CrossJoin, arg *AggregateColumn) bool {
	return aggregateArgumentMatchesRelationPair(cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias, arg)
}

func aggregateArgumentMatchesMultiJoin(multi MultiJoin, arg *AggregateColumn) bool {
	if arg == nil {
		return false
	}
	for _, rel := range multi.Relations {
		if rel.Table == arg.Table && rel.Alias == arg.Alias {
			return true
		}
	}
	return false
}

func aggregateArgumentMatchesRelationPair(left TableID, leftAlias uint8, right TableID, rightAlias uint8, arg *AggregateColumn) bool {
	if arg == nil {
		return false
	}
	if left == right {
		return (arg.Table == left && arg.Alias == leftAlias) ||
			(arg.Table == right && arg.Alias == rightAlias)
	}
	return arg.Table == left || arg.Table == right
}

func aggregatePredicateTable(pred Predicate) (TableID, bool) {
	if pred == nil {
		return 0, false
	}
	tables := pred.Tables()
	if len(tables) != 1 {
		return 0, false
	}
	return tables[0], true
}

func aggregateUpdateColumns(aggregate *Aggregate) []schema.ColumnSchema {
	if aggregate == nil {
		return nil
	}
	return []schema.ColumnSchema{aggregate.ResultColumn}
}

func (m *Manager) initialAggregateUpdates(ctx context.Context, pred Predicate, aggregate *Aggregate, view store.CommittedReadView, subID types.SubscriptionID, queryID uint32) ([]SubscriptionUpdate, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	table, ok := aggregateEmittedTable(pred)
	if !ok {
		return nil, nil
	}
	value, err := aggregateCommittedValue(ctx, view, table, pred, aggregate, m.resolver)
	if err != nil {
		return nil, err
	}
	return []SubscriptionUpdate{{
		SubscriptionID: subID,
		QueryID:        queryID,
		TableID:        table,
		TableName:      m.schema.TableName(table),
		Columns:        aggregateUpdateColumns(aggregate),
		Inserts:        []types.ProductValue{aggregateValueRow(value)},
	}}, nil
}

func (m *Manager) evalAggregateQuery(ctx context.Context, qs *queryState, dv *DeltaView) ([]SubscriptionUpdate, error) {
	table, ok := aggregateEmittedTable(qs.predicate)
	if !ok || qs.aggregate == nil {
		return nil, nil
	}
	after, err := aggregateCommittedValue(ctx, dv.CommittedView(), table, qs.predicate, qs.aggregate, m.resolver)
	if err != nil {
		return nil, err
	}
	var before types.Value
	switch pred := qs.predicate.(type) {
	case Join:
		join := pred
		leftBefore, err := projectedRowsBefore(ctx, dv, join.Left)
		if err != nil {
			return nil, err
		}
		rightBefore, err := projectedRowsBefore(ctx, dv, join.Right)
		if err != nil {
			return nil, err
		}
		before, err = aggregateJoinRowsValue(leftBefore, rightBefore, join, qs.aggregate)
		if err != nil {
			return nil, err
		}
	case CrossJoin:
		cross := pred
		leftBefore, err := projectedRowsBefore(ctx, dv, cross.Left)
		if err != nil {
			return nil, err
		}
		rightBefore, err := projectedRowsBefore(ctx, dv, cross.Right)
		if err != nil {
			return nil, err
		}
		before, err = aggregateCrossJoinRowsValue(leftBefore, rightBefore, cross, qs.aggregate)
		if err != nil {
			return nil, err
		}
	case MultiJoin:
		multi := pred
		rowsByRelation, err := multiJoinRowsByRelationBefore(ctx, dv, multi)
		if err != nil {
			return nil, err
		}
		before, err = aggregateMultiJoinRowsValue(ctx, multi, rowsByRelation, qs.aggregate)
		if err != nil {
			return nil, err
		}
	default:
		switch qs.aggregate.Func {
		case AggregateCount:
			if qs.aggregate.Distinct {
				rows, err := projectedRowsBefore(ctx, dv, table)
				if err != nil {
					return nil, err
				}
				before, err = aggregateRowsValue(rows, table, qs.predicate, qs.aggregate)
				if err != nil {
					return nil, err
				}
			} else {
				before = countAggregateBeforeValue(dv, table, qs.predicate, qs.aggregate, after)
			}
		case AggregateSum:
			rows, err := projectedRowsBefore(ctx, dv, table)
			if err != nil {
				return nil, err
			}
			before, err = aggregateRowsValue(rows, table, qs.predicate, qs.aggregate)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("aggregate %q not supported", qs.aggregate.Func)
		}
	}
	if before.Equal(after) {
		return nil, nil
	}
	return []SubscriptionUpdate{{
		TableID:   table,
		TableName: m.schema.TableName(table),
		Columns:   aggregateUpdateColumns(qs.aggregate),
		Inserts:   []types.ProductValue{aggregateValueRow(after)},
		Deletes:   []types.ProductValue{aggregateValueRow(before)},
	}}, nil
}

func countAggregateBeforeValue(dv *DeltaView, table TableID, pred Predicate, aggregate *Aggregate, after types.Value) types.Value {
	afterCount := after.AsUint64()
	inserted := countAggregateDeltaRows(dv.InsertedRows(table), table, pred, aggregate)
	deleted := countAggregateDeltaRows(dv.DeletedRows(table), table, pred, aggregate)
	before := afterCount + deleted
	if inserted > before {
		return types.NewUint64(0)
	} else {
		before -= inserted
	}
	return types.NewUint64(before)
}

func aggregateCommittedValue(ctx context.Context, view store.CommittedReadView, table TableID, pred Predicate, aggregate *Aggregate, resolver IndexResolver) (types.Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if join, ok := pred.(Join); ok {
		return aggregateJoinCommittedValue(ctx, view, join, aggregate, resolver)
	}
	if cross, ok := pred.(CrossJoin); ok {
		return aggregateCrossJoinCommittedValue(ctx, view, cross, aggregate)
	}
	if multi, ok := pred.(MultiJoin); ok {
		return aggregateMultiJoinCommittedValue(ctx, view, multi, aggregate)
	}
	switch aggregate.Func {
	case AggregateCount:
		if !aggregate.Distinct {
			count, err := countAggregateCommittedRows(ctx, view, table, pred, aggregate)
			if err != nil {
				return types.Value{}, err
			}
			return types.NewUint64(count), nil
		}
		rows, err := aggregateCommittedRows(ctx, view, table)
		if err != nil {
			return types.Value{}, err
		}
		return aggregateRowsValue(rows, table, pred, aggregate)
	case AggregateSum:
		if view == nil {
			return emptySumAggregateValue(aggregate)
		}
		rows, err := aggregateCommittedRows(ctx, view, table)
		if err != nil {
			return types.Value{}, err
		}
		return aggregateRowsValue(rows, table, pred, aggregate)
	default:
		return types.Value{}, fmt.Errorf("aggregate %q not supported", aggregate.Func)
	}
}

func aggregateJoinCommittedValue(ctx context.Context, view store.CommittedReadView, join Join, aggregate *Aggregate, resolver IndexResolver) (types.Value, error) {
	acc, err := newJoinAggregateAccumulator(aggregate)
	if err != nil {
		return types.Value{}, err
	}
	if view == nil {
		return acc.value()
	}
	var addErr error
	err = visitJoinCommittedPairs(ctx, view, join, resolver, func(leftRow, rightRow types.ProductValue) bool {
		if addErr = acc.add(leftRow, rightRow, join); addErr != nil {
			return false
		}
		return true
	})
	if err != nil {
		return types.Value{}, err
	}
	if addErr != nil {
		return types.Value{}, addErr
	}
	return acc.value()
}

func aggregateCrossJoinCommittedValue(ctx context.Context, view store.CommittedReadView, cross CrossJoin, aggregate *Aggregate) (types.Value, error) {
	acc, err := newJoinAggregateAccumulator(aggregate)
	if err != nil {
		return types.Value{}, err
	}
	if view == nil {
		return acc.value()
	}
	var addErr error
	err = visitCrossJoinCommittedPairs(ctx, view, cross, func(leftRow, rightRow types.ProductValue) bool {
		if addErr = acc.addCross(leftRow, rightRow, cross); addErr != nil {
			return false
		}
		return true
	})
	if err != nil {
		return types.Value{}, err
	}
	if addErr != nil {
		return types.Value{}, addErr
	}
	return acc.value()
}

func aggregateMultiJoinCommittedValue(ctx context.Context, view store.CommittedReadView, multi MultiJoin, aggregate *Aggregate) (types.Value, error) {
	acc, err := newJoinAggregateAccumulator(aggregate)
	if err != nil {
		return types.Value{}, err
	}
	if view == nil {
		return acc.value()
	}
	rowsByRelation, err := multiJoinRowsByRelationFromView(ctx, view, multi)
	if err != nil {
		return types.Value{}, err
	}
	return aggregateMultiJoinRowsValue(ctx, multi, rowsByRelation, aggregate)
}

func aggregateCommittedRows(ctx context.Context, view store.CommittedReadView, table TableID) ([]types.ProductValue, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if view == nil {
		return nil, nil
	}
	var rows []types.ProductValue
	for _, row := range view.TableScan(table) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func countAggregateCommittedRows(ctx context.Context, view store.CommittedReadView, table TableID, pred Predicate, aggregate *Aggregate) (uint64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if view == nil {
		return 0, nil
	}
	var count uint64
	for _, row := range view.TableScan(table) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if aggregateRowContributes(row, table, pred, aggregate) {
			count++
		}
	}
	return count, nil
}

func visitJoinCommittedPairs(ctx context.Context, view store.CommittedReadView, join Join, resolver IndexResolver, visit func(leftRow, rightRow types.ProductValue) bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if view == nil {
		return nil
	}
	if resolver == nil {
		return fmt.Errorf("%w: manager has no IndexResolver (join=%d.%d=%d.%d)", ErrJoinIndexUnresolved, join.Left, join.LeftCol, join.Right, join.RightCol)
	}
	if idx, ok := resolver.IndexIDForColumn(join.Right, join.RightCol); ok {
		return visitJoinCommittedPairsWithProbeIndex(ctx, view, join, join.Left, join.LeftCol, join.Right, join.RightCol, idx, true, visit)
	}
	if idx, ok := resolver.IndexIDForColumn(join.Left, join.LeftCol); ok {
		return visitJoinCommittedPairsWithProbeIndex(ctx, view, join, join.Right, join.RightCol, join.Left, join.LeftCol, idx, false, visit)
	}
	return fmt.Errorf("%w: join=%d.%d=%d.%d", ErrJoinIndexUnresolved, join.Left, join.LeftCol, join.Right, join.RightCol)
}

func visitJoinCommittedPairsWithProbeIndex(
	ctx context.Context,
	view store.CommittedReadView,
	join Join,
	driveTable TableID,
	driveCol ColID,
	probeTable TableID,
	probeCol ColID,
	probeIdx IndexID,
	driveIsLeft bool,
	visit func(leftRow, rightRow types.ProductValue) bool,
) error {
	for _, driveRow := range view.TableScan(driveTable) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if int(driveCol) >= len(driveRow) {
			continue
		}
		key := store.NewIndexKey(driveRow[driveCol])
		for _, rid := range view.IndexSeek(probeTable, probeIdx, key) {
			if err := ctx.Err(); err != nil {
				return err
			}
			probeRow, ok := view.GetRow(probeTable, rid)
			if !ok || int(probeCol) >= len(probeRow) || !driveRow[driveCol].Equal(probeRow[probeCol]) {
				continue
			}
			if driveIsLeft {
				if joinPairMatches(driveRow, join.Left, probeRow, join.Right, &join) {
					if !visit(driveRow, probeRow) {
						return nil
					}
				}
				continue
			}
			if joinPairMatches(probeRow, join.Left, driveRow, join.Right, &join) {
				if !visit(probeRow, driveRow) {
					return nil
				}
			}
		}
	}
	return nil
}

func visitCrossJoinCommittedPairs(ctx context.Context, view store.CommittedReadView, cross CrossJoin, visit func(leftRow, rightRow types.ProductValue) bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if view == nil {
		return nil
	}
	for _, leftRow := range view.TableScan(cross.Left) {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, rightRow := range view.TableScan(cross.Right) {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !MatchJoinPair(cross.Filter, cross.Left, cross.LeftAlias, leftRow, cross.Right, cross.RightAlias, rightRow) {
				continue
			}
			if !visit(leftRow, rightRow) {
				return nil
			}
		}
	}
	return nil
}

type joinAggregateAccumulator struct {
	aggregate *Aggregate
	count     uint64
	distinct  *valueagg.DistinctSet
	sum       *valueagg.Sum
}

func newJoinAggregateAccumulator(aggregate *Aggregate) (*joinAggregateAccumulator, error) {
	acc := &joinAggregateAccumulator{aggregate: aggregate}
	switch aggregate.Func {
	case AggregateCount:
		if aggregate.Distinct {
			acc.distinct = valueagg.NewDistinctSet()
		}
	case AggregateSum:
		acc.sum = valueagg.NewSum(aggregate.ResultColumn.Type, aggregate.ResultColumn.Nullable)
	default:
		return nil, fmt.Errorf("aggregate %q not supported", aggregate.Func)
	}
	return acc, nil
}

func (a *joinAggregateAccumulator) add(leftRow, rightRow types.ProductValue, join Join) error {
	return a.addPair(leftRow, rightRow, join.Left, join.LeftAlias, join.Right, join.RightAlias)
}

func (a *joinAggregateAccumulator) addCross(leftRow, rightRow types.ProductValue, cross CrossJoin) error {
	return a.addPair(leftRow, rightRow, cross.Left, cross.LeftAlias, cross.Right, cross.RightAlias)
}

func (a *joinAggregateAccumulator) addMulti(tuple []types.ProductValue, multi MultiJoin) error {
	switch a.aggregate.Func {
	case AggregateCount:
		if a.aggregate.Argument == nil {
			a.count++
			return nil
		}
		value, ok := multiJoinAggregateArgumentValue(tuple, multi, a.aggregate.Argument)
		if !ok || value.IsNull() {
			return nil
		}
		if a.aggregate.Distinct {
			a.distinct.Add(value)
			return nil
		}
		a.count++
		return nil
	case AggregateSum:
		value, ok := multiJoinAggregateArgumentValue(tuple, multi, a.aggregate.Argument)
		if !ok {
			return nil
		}
		return a.sum.Add(value)
	default:
		return fmt.Errorf("aggregate %q not supported", a.aggregate.Func)
	}
}

func (a *joinAggregateAccumulator) addPair(leftRow, rightRow types.ProductValue, left TableID, leftAlias uint8, right TableID, rightAlias uint8) error {
	switch a.aggregate.Func {
	case AggregateCount:
		if a.aggregate.Argument == nil {
			a.count++
			return nil
		}
		value, ok := relationPairAggregateArgumentValue(leftRow, rightRow, left, leftAlias, right, rightAlias, a.aggregate.Argument)
		if !ok || value.IsNull() {
			return nil
		}
		if a.aggregate.Distinct {
			a.distinct.Add(value)
			return nil
		}
		a.count++
		return nil
	case AggregateSum:
		value, ok := relationPairAggregateArgumentValue(leftRow, rightRow, left, leftAlias, right, rightAlias, a.aggregate.Argument)
		if !ok {
			return nil
		}
		return a.sum.Add(value)
	default:
		return fmt.Errorf("aggregate %q not supported", a.aggregate.Func)
	}
}

func (a *joinAggregateAccumulator) value() (types.Value, error) {
	switch a.aggregate.Func {
	case AggregateCount:
		if a.aggregate.Distinct {
			return types.NewUint64(a.distinct.Count()), nil
		}
		return types.NewUint64(a.count), nil
	case AggregateSum:
		return a.sum.Value()
	default:
		return types.Value{}, fmt.Errorf("aggregate %q not supported", a.aggregate.Func)
	}
}

func aggregateJoinRowsValue(leftRows, rightRows []types.ProductValue, join Join, aggregate *Aggregate) (types.Value, error) {
	acc, err := newJoinAggregateAccumulator(aggregate)
	if err != nil {
		return types.Value{}, err
	}
	leftCol := int(join.LeftCol)
	rightCol := int(join.RightCol)
	for _, leftRow := range leftRows {
		if leftCol < 0 || leftCol >= len(leftRow) {
			continue
		}
		for _, rightRow := range rightRows {
			if rightCol < 0 || rightCol >= len(rightRow) || !leftRow[leftCol].Equal(rightRow[rightCol]) {
				continue
			}
			if !joinPairMatches(leftRow, join.Left, rightRow, join.Right, &join) {
				continue
			}
			if err := acc.add(leftRow, rightRow, join); err != nil {
				return types.Value{}, err
			}
		}
	}
	return acc.value()
}

func aggregateCrossJoinRowsValue(leftRows, rightRows []types.ProductValue, cross CrossJoin, aggregate *Aggregate) (types.Value, error) {
	acc, err := newJoinAggregateAccumulator(aggregate)
	if err != nil {
		return types.Value{}, err
	}
	for _, leftRow := range leftRows {
		for _, rightRow := range rightRows {
			if !MatchJoinPair(cross.Filter, cross.Left, cross.LeftAlias, leftRow, cross.Right, cross.RightAlias, rightRow) {
				continue
			}
			if err := acc.addCross(leftRow, rightRow, cross); err != nil {
				return types.Value{}, err
			}
		}
	}
	return acc.value()
}

func aggregateMultiJoinRowsValue(ctx context.Context, multi MultiJoin, rowsByRelation [][]types.ProductValue, aggregate *Aggregate) (types.Value, error) {
	acc, err := newJoinAggregateAccumulator(aggregate)
	if err != nil {
		return types.Value{}, err
	}
	var addErr error
	err = visitMultiJoinTuples(ctx, multi, rowsByRelation, func(tuple []types.ProductValue) error {
		if addErr = acc.addMulti(tuple, multi); addErr != nil {
			return addErr
		}
		return nil
	})
	if err != nil {
		return types.Value{}, err
	}
	if addErr != nil {
		return types.Value{}, addErr
	}
	return acc.value()
}

func relationPairAggregateArgumentValue(leftRow, rightRow types.ProductValue, left TableID, leftAlias uint8, right TableID, rightAlias uint8, arg *AggregateColumn) (types.Value, bool) {
	if arg == nil {
		return types.Value{}, false
	}
	var row types.ProductValue
	switch {
	case left == right:
		switch {
		case arg.Table == left && arg.Alias == leftAlias:
			row = leftRow
		case arg.Table == right && arg.Alias == rightAlias:
			row = rightRow
		default:
			return types.Value{}, false
		}
	case arg.Table == left:
		row = leftRow
	case arg.Table == right:
		row = rightRow
	default:
		return types.Value{}, false
	}
	idx := int(arg.Column)
	if idx < 0 || idx >= len(row) {
		return types.Value{}, false
	}
	return row[idx], true
}

func multiJoinAggregateArgumentValue(tuple []types.ProductValue, multi MultiJoin, arg *AggregateColumn) (types.Value, bool) {
	if arg == nil {
		return types.Value{}, false
	}
	relation, ok := multiJoinAggregateRelationIndex(multi.Relations, arg.Table, arg.Alias)
	if !ok || relation < 0 || relation >= len(tuple) {
		return types.Value{}, false
	}
	row := tuple[relation]
	idx := int(arg.Column)
	if idx < 0 || idx >= len(row) {
		return types.Value{}, false
	}
	return row[idx], true
}

func multiJoinAggregateRelationIndex(relations []MultiJoinRelation, table TableID, alias uint8) (int, bool) {
	for i, rel := range relations {
		if rel.Table == table && rel.Alias == alias {
			return i, true
		}
	}
	return 0, false
}

func countAggregateDeltaRows(rows []types.ProductValue, table TableID, pred Predicate, aggregate *Aggregate) uint64 {
	var count uint64
	for _, row := range rows {
		if aggregateRowContributes(row, table, pred, aggregate) {
			count++
		}
	}
	return count
}

func aggregateEmittedTable(pred Predicate) (TableID, bool) {
	switch p := pred.(type) {
	case Join:
		return p.ProjectedTable(), true
	case CrossJoin:
		return p.ProjectedTable(), true
	case MultiJoin:
		return p.ProjectedTable(), true
	default:
		return aggregatePredicateTable(pred)
	}
}

func aggregateRowContributes(row types.ProductValue, table TableID, pred Predicate, aggregate *Aggregate) bool {
	if aggregate == nil || !MatchRow(pred, table, row) {
		return false
	}
	if aggregate.Argument == nil {
		return true
	}
	arg := aggregate.Argument
	if arg.Table != table || arg.Alias != 0 {
		return false
	}
	idx := int(arg.Column)
	if idx < 0 || idx >= len(row) {
		return false
	}
	return !row[idx].IsNull()
}

func aggregateValueRow(value types.Value) types.ProductValue {
	return types.ProductValue{value}
}

func aggregateRowsValue(rows []types.ProductValue, table TableID, pred Predicate, aggregate *Aggregate) (types.Value, error) {
	switch aggregate.Func {
	case AggregateCount:
		if aggregate.Distinct {
			return types.NewUint64(distinctCountAggregateRows(rows, table, pred, aggregate)), nil
		}
		var count uint64
		for _, row := range rows {
			if aggregateRowContributes(row, table, pred, aggregate) {
				count++
			}
		}
		return types.NewUint64(count), nil
	case AggregateSum:
		acc := valueagg.NewSum(aggregate.ResultColumn.Type, aggregate.ResultColumn.Nullable)
		for _, row := range rows {
			value, ok := aggregateArgumentValue(row, table, pred, aggregate)
			if !ok {
				continue
			}
			if err := acc.Add(value); err != nil {
				return types.Value{}, err
			}
		}
		return acc.Value()
	default:
		return types.Value{}, fmt.Errorf("aggregate %q not supported", aggregate.Func)
	}
}

func distinctCountAggregateRows(rows []types.ProductValue, table TableID, pred Predicate, aggregate *Aggregate) uint64 {
	seen := valueagg.NewDistinctSet()
	for _, row := range rows {
		value, ok := aggregateArgumentValue(row, table, pred, aggregate)
		if !ok || value.IsNull() {
			continue
		}
		seen.Add(value)
	}
	return seen.Count()
}

func aggregateArgumentValue(row types.ProductValue, table TableID, pred Predicate, aggregate *Aggregate) (types.Value, bool) {
	if aggregate == nil || aggregate.Argument == nil || !MatchRow(pred, table, row) {
		return types.Value{}, false
	}
	arg := aggregate.Argument
	if arg.Table != table || arg.Alias != 0 {
		return types.Value{}, false
	}
	idx := int(arg.Column)
	if idx < 0 || idx >= len(row) {
		return types.Value{}, false
	}
	return row[idx], true
}

func emptySumAggregateValue(aggregate *Aggregate) (types.Value, error) {
	return valueagg.NewSum(aggregate.ResultColumn.Type, aggregate.ResultColumn.Nullable).Value()
}

func sumAggregateResultKind(kind types.ValueKind) (types.ValueKind, bool) {
	switch kind {
	case types.KindInt8, types.KindInt16, types.KindInt32, types.KindInt64:
		return types.KindInt64, true
	case types.KindUint8, types.KindUint16, types.KindUint32, types.KindUint64:
		return types.KindUint64, true
	case types.KindFloat32, types.KindFloat64:
		return types.KindFloat64, true
	default:
		return 0, false
	}
}
