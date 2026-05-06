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
	// AggregateSum sums a numeric column in a live single-table aggregate view.
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
	switch aggregate.Func {
	case AggregateCount:
		return validateCountAggregate(table, aggregate, s)
	case AggregateSum:
		return validateSumAggregate(table, aggregate, s)
	default:
		return fmt.Errorf("%w: live aggregate views support COUNT and SUM only", ErrInvalidPredicate)
	}
}

func validateCountAggregate(table TableID, aggregate *Aggregate, s SchemaLookup) error {
	if aggregate.ResultColumn.Type != types.KindUint64 {
		return fmt.Errorf("%w: COUNT aggregate result kind must be Uint64", ErrInvalidPredicate)
	}
	if aggregate.ResultColumn.Nullable {
		return fmt.Errorf("%w: COUNT aggregate result must be non-nullable", ErrInvalidPredicate)
	}
	if aggregate.Argument == nil {
		if aggregate.Distinct {
			return fmt.Errorf("%w: COUNT(DISTINCT ...) aggregate requires a column argument", ErrInvalidPredicate)
		}
		return nil
	}
	if err := validateAggregateArgument(table, "COUNT(column)", aggregate.Argument, s); err != nil {
		return err
	}
	return nil
}

func validateSumAggregate(table TableID, aggregate *Aggregate, s SchemaLookup) error {
	if aggregate.Distinct {
		return fmt.Errorf("%w: live aggregate views do not support SUM(DISTINCT ...)", ErrInvalidPredicate)
	}
	if aggregate.Argument == nil {
		return fmt.Errorf("%w: SUM aggregate requires a column argument", ErrInvalidPredicate)
	}
	if err := validateAggregateArgument(table, "SUM(column)", aggregate.Argument, s); err != nil {
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
	value, err := aggregateCommittedValue(ctx, view, table, pred, aggregate)
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

func (m *Manager) evalAggregateQuery(qs *queryState, dv *DeltaView) ([]SubscriptionUpdate, error) {
	table, ok := aggregatePredicateTable(qs.predicate)
	if !ok || qs.aggregate == nil {
		return nil, nil
	}
	after, err := aggregateCommittedValue(context.Background(), dv.CommittedView(), table, qs.predicate, qs.aggregate)
	if err != nil {
		return nil, err
	}
	var before types.Value
	switch qs.aggregate.Func {
	case AggregateCount:
		if qs.aggregate.Distinct {
			before, err = aggregateRowsValue(projectedRowsBefore(dv, table), table, qs.predicate, qs.aggregate)
			if err != nil {
				return nil, err
			}
		} else {
			before = countAggregateBeforeValue(dv, table, qs.predicate, qs.aggregate, after)
		}
	case AggregateSum:
		before, err = aggregateRowsValue(projectedRowsBefore(dv, table), table, qs.predicate, qs.aggregate)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("aggregate %q not supported", qs.aggregate.Func)
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

func aggregateCommittedValue(ctx context.Context, view store.CommittedReadView, table TableID, pred Predicate, aggregate *Aggregate) (types.Value, error) {
	if ctx == nil {
		ctx = context.Background()
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
		acc := newLiveSumAccumulator(aggregate.ResultColumn.Type, aggregate.ResultColumn.Nullable)
		for _, row := range rows {
			value, ok := aggregateArgumentValue(row, table, pred, aggregate)
			if !ok {
				continue
			}
			if err := acc.add(value); err != nil {
				return types.Value{}, err
			}
		}
		return acc.value()
	default:
		return types.Value{}, fmt.Errorf("aggregate %q not supported", aggregate.Func)
	}
}

func distinctCountAggregateRows(rows []types.ProductValue, table TableID, pred Predicate, aggregate *Aggregate) uint64 {
	seen := newAggregateDistinctValueSet()
	for _, row := range rows {
		value, ok := aggregateArgumentValue(row, table, pred, aggregate)
		if !ok || value.IsNull() {
			continue
		}
		seen.add(value)
	}
	return seen.count()
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

type aggregateDistinctValueSet struct {
	buckets map[uint64][]types.Value
	n       uint64
}

func newAggregateDistinctValueSet() *aggregateDistinctValueSet {
	return &aggregateDistinctValueSet{buckets: make(map[uint64][]types.Value)}
}

func (s *aggregateDistinctValueSet) add(value types.Value) {
	hash := value.Hash64()
	for _, existing := range s.buckets[hash] {
		if value.Equal(existing) {
			return
		}
	}
	s.buckets[hash] = append(s.buckets[hash], value)
	s.n++
}

func (s *aggregateDistinctValueSet) count() uint64 {
	return s.n
}

func emptySumAggregateValue(aggregate *Aggregate) (types.Value, error) {
	return newLiveSumAccumulator(aggregate.ResultColumn.Type, aggregate.ResultColumn.Nullable).value()
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

type liveSumAccumulator struct {
	kind     types.ValueKind
	nullable bool
	seen     bool
	i64      int64
	u64      uint64
	f64      float64
	err      error
}

func newLiveSumAccumulator(kind types.ValueKind, nullable bool) *liveSumAccumulator {
	return &liveSumAccumulator{kind: kind, nullable: nullable}
}

func (a *liveSumAccumulator) add(value types.Value) error {
	if a.err != nil {
		return a.err
	}
	if value.IsNull() {
		return nil
	}
	a.seen = true
	switch a.kind {
	case types.KindInt64:
		n, ok := aggregateValueAsInt64(value)
		if !ok {
			a.err = fmt.Errorf("SUM aggregate received non-signed value kind %s", value.Kind())
			return a.err
		}
		sum := a.i64 + n
		if (n > 0 && sum < a.i64) || (n < 0 && sum > a.i64) {
			a.err = fmt.Errorf("SUM aggregate overflowed Int64")
			return a.err
		}
		a.i64 = sum
	case types.KindUint64:
		n, ok := aggregateValueAsUint64(value)
		if !ok {
			a.err = fmt.Errorf("SUM aggregate received non-unsigned value kind %s", value.Kind())
			return a.err
		}
		if ^uint64(0)-a.u64 < n {
			a.err = fmt.Errorf("SUM aggregate overflowed Uint64")
			return a.err
		}
		a.u64 += n
	case types.KindFloat64:
		n, ok := aggregateValueAsFloat64(value)
		if !ok {
			a.err = fmt.Errorf("SUM aggregate received non-float value kind %s", value.Kind())
			return a.err
		}
		a.f64 += n
	default:
		a.err = fmt.Errorf("SUM aggregate result kind %s not supported", a.kind)
	}
	return a.err
}

func (a *liveSumAccumulator) value() (types.Value, error) {
	if a.err != nil {
		return types.Value{}, a.err
	}
	if a.nullable && !a.seen {
		return types.NewNull(a.kind), nil
	}
	switch a.kind {
	case types.KindInt64:
		return types.NewInt64(a.i64), nil
	case types.KindUint64:
		return types.NewUint64(a.u64), nil
	case types.KindFloat64:
		return types.NewFloat64(a.f64)
	default:
		return types.Value{}, fmt.Errorf("SUM aggregate result kind %s not supported", a.kind)
	}
}

func aggregateValueAsInt64(value types.Value) (int64, bool) {
	switch value.Kind() {
	case types.KindInt8:
		return int64(value.AsInt8()), true
	case types.KindInt16:
		return int64(value.AsInt16()), true
	case types.KindInt32:
		return int64(value.AsInt32()), true
	case types.KindInt64:
		return value.AsInt64(), true
	default:
		return 0, false
	}
}

func aggregateValueAsUint64(value types.Value) (uint64, bool) {
	switch value.Kind() {
	case types.KindUint8:
		return uint64(value.AsUint8()), true
	case types.KindUint16:
		return uint64(value.AsUint16()), true
	case types.KindUint32:
		return uint64(value.AsUint32()), true
	case types.KindUint64:
		return value.AsUint64(), true
	default:
		return 0, false
	}
}

func aggregateValueAsFloat64(value types.Value) (float64, bool) {
	switch value.Kind() {
	case types.KindFloat32:
		return float64(value.AsFloat32()), true
	case types.KindFloat64:
		return value.AsFloat64(), true
	default:
		return 0, false
	}
}
