package subscription

import (
	"fmt"
	"slices"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// OrderByColumn describes one source-column ordering term for an initial live
// snapshot. Post-commit delivery remains ordinary row deltas; this metadata is
// part of query identity and initial materialization only.
type OrderByColumn struct {
	Schema schema.ColumnSchema
	Table  TableID
	Column ColID
	Alias  uint8
	Desc   bool
}

func copyOrderByColumns(in []OrderByColumn) []OrderByColumn {
	if len(in) == 0 {
		return nil
	}
	out := make([]OrderByColumn, len(in))
	copy(out, in)
	return out
}

// ValidateOrderBy checks the narrow executable live ORDER BY surface.
func ValidateOrderBy(pred Predicate, orderBy []OrderByColumn, aggregate *Aggregate, s SchemaLookup) error {
	return validateOrderByColumns(pred, orderBy, aggregate, s)
}

func validateOrderByColumns(pred Predicate, orderBy []OrderByColumn, aggregate *Aggregate, s SchemaLookup) error {
	if len(orderBy) == 0 {
		return nil
	}
	if s == nil {
		return fmt.Errorf("%w: order-by schema lookup is nil", ErrInvalidPredicate)
	}
	if aggregate != nil {
		return fmt.Errorf("%w: live ORDER BY views do not support aggregate views", ErrInvalidPredicate)
	}
	if _, ok := pred.(Join); ok {
		return fmt.Errorf("%w: live ORDER BY views require a single table", ErrInvalidPredicate)
	}
	if _, ok := pred.(CrossJoin); ok {
		return fmt.Errorf("%w: live ORDER BY views require a single table", ErrInvalidPredicate)
	}
	if _, ok := pred.(MultiJoin); ok {
		return fmt.Errorf("%w: live ORDER BY views require a single table", ErrInvalidPredicate)
	}
	tables := pred.Tables()
	if len(tables) != 1 {
		return fmt.Errorf("%w: live ORDER BY views require one referenced table", ErrInvalidPredicate)
	}
	table := tables[0]
	for i, col := range orderBy {
		if col.Table != table || col.Alias != 0 {
			return fmt.Errorf("%w: ORDER BY column %d must come from the ordered table", ErrInvalidPredicate, i)
		}
		if col.Schema.Index != int(col.Column) {
			return fmt.Errorf("%w: ORDER BY column %d schema index %d does not match source column %d", ErrInvalidPredicate, i, col.Schema.Index, col.Column)
		}
		if !s.TableExists(col.Table) {
			return fmt.Errorf("%w: ORDER BY column %d table %d", ErrTableNotFound, i, col.Table)
		}
		if !s.ColumnExists(col.Table, col.Column) {
			return fmt.Errorf("%w: ORDER BY column %d table %d column %d", ErrColumnNotFound, i, col.Table, col.Column)
		}
		if want := s.ColumnType(col.Table, col.Column); col.Schema.Type != want {
			return fmt.Errorf("%w: ORDER BY column %d kind %s does not match column kind %s", ErrInvalidPredicate, i, col.Schema.Type, want)
		}
	}
	return nil
}

type orderedInitialRow struct {
	row types.ProductValue
	key []types.Value
}

func orderInitialRows(rows []types.ProductValue, orderBy []OrderByColumn) ([]types.ProductValue, error) {
	if len(rows) == 0 || len(orderBy) == 0 {
		return rows, nil
	}
	ordered := make([]orderedInitialRow, 0, len(rows))
	for _, row := range rows {
		keys := make([]types.Value, len(orderBy))
		for i, term := range orderBy {
			idx := int(term.Column)
			if idx < 0 || idx >= len(row) {
				return nil, fmt.Errorf("ORDER BY column %q is missing from row", term.Schema.Name)
			}
			keys[i] = row[idx]
		}
		ordered = append(ordered, orderedInitialRow{row: row, key: keys})
	}
	slices.SortStableFunc(ordered, func(a, b orderedInitialRow) int {
		for i, term := range orderBy {
			cmp := a.key[i].Compare(b.key[i])
			if cmp == 0 {
				continue
			}
			if term.Desc {
				return -cmp
			}
			return cmp
		}
		return 0
	})
	out := make([]types.ProductValue, 0, len(ordered))
	for _, row := range ordered {
		out = append(out, row.row)
	}
	return out, nil
}
