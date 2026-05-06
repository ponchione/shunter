package subscription

import (
	"context"
	"fmt"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/store"
	"github.com/ponchione/shunter/types"
)

// AggregateFunc names a live aggregate function supported by the subscription
// manager.
type AggregateFunc string

const (
	// AggregateCount counts rows in a live single-table aggregate view.
	AggregateCount AggregateFunc = "COUNT"
)

// AggregateColumn identifies the optional COUNT(column) argument.
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
	if aggregate.Func != AggregateCount {
		return fmt.Errorf("%w: live aggregate views support COUNT only", ErrInvalidPredicate)
	}
	if aggregate.Distinct {
		return fmt.Errorf("%w: live aggregate views do not support COUNT(DISTINCT ...)", ErrInvalidPredicate)
	}
	if _, ok := pred.(Join); ok {
		return fmt.Errorf("%w: live aggregate views require a single table", ErrInvalidPredicate)
	}
	if _, ok := pred.(CrossJoin); ok {
		return fmt.Errorf("%w: live aggregate views require a single table", ErrInvalidPredicate)
	}
	if _, ok := pred.(MultiJoin); ok {
		return fmt.Errorf("%w: live aggregate views require a single table", ErrInvalidPredicate)
	}
	table, ok := aggregatePredicateTable(pred)
	if !ok {
		return fmt.Errorf("%w: live aggregate views require one referenced table", ErrInvalidPredicate)
	}
	if aggregate.ResultColumn.Index != 0 {
		return fmt.Errorf("%w: aggregate result schema index must be 0", ErrInvalidPredicate)
	}
	if aggregate.ResultColumn.Name == "" {
		return fmt.Errorf("%w: aggregate result column name must not be empty", ErrInvalidPredicate)
	}
	if aggregate.ResultColumn.Type != types.KindUint64 {
		return fmt.Errorf("%w: COUNT aggregate result kind must be Uint64", ErrInvalidPredicate)
	}
	if aggregate.ResultColumn.Nullable {
		return fmt.Errorf("%w: COUNT aggregate result must be non-nullable", ErrInvalidPredicate)
	}
	if aggregate.Argument == nil {
		return nil
	}
	arg := aggregate.Argument
	if arg.Table != table || arg.Alias != 0 {
		return fmt.Errorf("%w: COUNT(column) argument must come from the aggregate table", ErrInvalidPredicate)
	}
	if arg.Schema.Index != int(arg.Column) {
		return fmt.Errorf("%w: aggregate argument schema index %d does not match source column %d", ErrInvalidPredicate, arg.Schema.Index, arg.Column)
	}
	if !s.TableExists(arg.Table) {
		return fmt.Errorf("%w: aggregate argument table %d", ErrTableNotFound, arg.Table)
	}
	if !s.ColumnExists(arg.Table, arg.Column) {
		return fmt.Errorf("%w: aggregate argument table %d column %d", ErrColumnNotFound, arg.Table, arg.Column)
	}
	if want := s.ColumnType(arg.Table, arg.Column); arg.Schema.Type != want {
		return fmt.Errorf("%w: aggregate argument kind %s does not match column kind %s", ErrInvalidPredicate, arg.Schema.Type, want)
	}
	return nil
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
	table, ok := aggregatePredicateTable(pred)
	if !ok {
		return nil, nil
	}
	count, err := countAggregateCommittedRows(ctx, view, table, pred, aggregate)
	if err != nil {
		return nil, err
	}
	return []SubscriptionUpdate{{
		SubscriptionID: subID,
		QueryID:        queryID,
		TableID:        table,
		TableName:      m.schema.TableName(table),
		Columns:        aggregateUpdateColumns(aggregate),
		Inserts:        []types.ProductValue{aggregateCountRow(count)},
	}}, nil
}

func (m *Manager) evalAggregateQuery(qs *queryState, dv *DeltaView) []SubscriptionUpdate {
	table, ok := aggregatePredicateTable(qs.predicate)
	if !ok || qs.aggregate == nil {
		return nil
	}
	after := countAggregateCommittedRowsNoContext(dv.CommittedView(), table, qs.predicate, qs.aggregate)
	inserted := countAggregateDeltaRows(dv.InsertedRows(table), table, qs.predicate, qs.aggregate)
	deleted := countAggregateDeltaRows(dv.DeletedRows(table), table, qs.predicate, qs.aggregate)
	before := after + deleted
	if inserted > before {
		before = 0
	} else {
		before -= inserted
	}
	if before == after {
		return nil
	}
	return []SubscriptionUpdate{{
		TableID:   table,
		TableName: m.schema.TableName(table),
		Columns:   aggregateUpdateColumns(qs.aggregate),
		Inserts:   []types.ProductValue{aggregateCountRow(after)},
		Deletes:   []types.ProductValue{aggregateCountRow(before)},
	}}
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

func countAggregateCommittedRowsNoContext(view store.CommittedReadView, table TableID, pred Predicate, aggregate *Aggregate) uint64 {
	count, _ := countAggregateCommittedRows(context.Background(), view, table, pred, aggregate)
	return count
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

func aggregateCountRow(count uint64) types.ProductValue {
	return types.ProductValue{types.NewUint64(count)}
}
