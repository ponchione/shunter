package subscription

import (
	"fmt"
	"sort"

	"github.com/ponchione/shunter/schema"
	"github.com/ponchione/shunter/types"
)

// OrderByColumn describes one source-column ordering term for a live
// single-table window. It is part of query identity, initial materialization,
// and maintained window-membership delta evaluation.
type OrderByColumn struct {
	Schema schema.ColumnSchema
	Table  TableID
	Column ColID
	Alias  uint8
	Desc   bool
}

// ValidateOrderBy checks the narrow executable live ORDER BY surface.
func ValidateOrderBy(pred Predicate, orderBy []OrderByColumn, aggregate *Aggregate, s SchemaLookup) error {
	return validateOrderByColumns(pred, orderBy, aggregate, s)
}

func validateOrderByColumns(pred Predicate, orderBy []OrderByColumn, aggregate *Aggregate, s SchemaLookup) error {
	if len(orderBy) == 0 {
		return nil
	}
	table, err := validateWindowSingleTable("ORDER BY", "order-by", pred, aggregate, s)
	if err != nil {
		return err
	}
	for i, col := range orderBy {
		if col.Table != table || col.Alias != 0 {
			return fmt.Errorf("%w: ORDER BY column %d must come from the ordered table", ErrInvalidPredicate, i)
		}
		if err := validateDeclaredColumnSchema(fmt.Sprintf("ORDER BY column %d", i), col.Table, col.Column, col.Schema, s); err != nil {
			return err
		}
	}
	return nil
}

type orderedInitialRow struct {
	row    types.ProductValue
	rowKey string
}

type boundedOrderedInitialRows struct {
	orderBy []OrderByColumn
	keep    int
	rows    []orderedInitialRow
}

type orderedInitialRowsSorter struct {
	rows    []orderedInitialRow
	orderBy []OrderByColumn
}

func (s orderedInitialRowsSorter) Len() int { return len(s.rows) }

func (s orderedInitialRowsSorter) Less(i, j int) bool {
	return compareOrderedInitialRows(&s.rows[i], &s.rows[j], s.orderBy) < 0
}

func (s orderedInitialRowsSorter) Swap(i, j int) { s.rows[i], s.rows[j] = s.rows[j], s.rows[i] }

func orderWindowRows(rows []types.ProductValue, orderBy []OrderByColumn, deterministic bool) ([]types.ProductValue, error) {
	if len(rows) == 0 || !deterministic {
		return rows, nil
	}
	ordered := make([]orderedInitialRow, 0, len(rows))
	for _, row := range rows {
		if err := validateInitialRowOrderRow(row, orderBy); err != nil {
			return nil, err
		}
		ordered = append(ordered, orderedInitialRow{row: row})
	}
	sort.Stable(orderedInitialRowsSorter{rows: ordered, orderBy: orderBy})
	return flattenOrderedInitialRows(ordered), nil
}

func newBoundedOrderedInitialRows(orderBy []OrderByColumn, keep int) *boundedOrderedInitialRows {
	if len(orderBy) == 0 || keep <= 0 {
		return nil
	}
	return &boundedOrderedInitialRows{
		orderBy: orderBy,
		keep:    keep,
		rows:    make([]orderedInitialRow, 0, keep),
	}
}

func (b *boundedOrderedInitialRows) add(row types.ProductValue) error {
	if b == nil {
		return nil
	}
	if err := validateInitialRowOrderRow(row, b.orderBy); err != nil {
		return err
	}
	item := orderedInitialRow{row: row}
	pos := upperBoundOrderedInitialRows(b.rows, &item, b.orderBy)
	if len(b.rows) == b.keep && pos == b.keep {
		return nil
	}
	b.rows = append(b.rows, orderedInitialRow{})
	copy(b.rows[pos+1:], b.rows[pos:])
	b.rows[pos] = item
	if len(b.rows) > b.keep {
		b.rows = b.rows[:b.keep]
	}
	return nil
}

func (b *boundedOrderedInitialRows) productRows() []types.ProductValue {
	if b == nil {
		return nil
	}
	return flattenOrderedInitialRows(b.rows)
}

func validateInitialRowOrderRow(row types.ProductValue, orderBy []OrderByColumn) error {
	for _, term := range orderBy {
		idx := int(term.Column)
		if idx < 0 || idx >= len(row) {
			return fmt.Errorf("ORDER BY column %q is missing from row", term.Schema.Name)
		}
	}
	return nil
}

func (r *orderedInitialRow) canonicalRowKey() string {
	if r.rowKey == "" {
		r.rowKey = encodeRowKey(r.row)
	}
	return r.rowKey
}

func compareOrderedInitialRows(a, b *orderedInitialRow, orderBy []OrderByColumn) int {
	for _, term := range orderBy {
		idx := int(term.Column)
		cmp := a.row[idx].Compare(b.row[idx])
		if cmp == 0 {
			continue
		}
		if term.Desc {
			return -cmp
		}
		return cmp
	}
	aKey := a.canonicalRowKey()
	bKey := b.canonicalRowKey()
	if aKey < bKey {
		return -1
	}
	if aKey > bKey {
		return 1
	}
	return 0
}

func upperBoundOrderedInitialRows(rows []orderedInitialRow, item *orderedInitialRow, orderBy []OrderByColumn) int {
	return sort.Search(len(rows), func(i int) bool {
		return compareOrderedInitialRows(&rows[i], item, orderBy) > 0
	})
}

func flattenOrderedInitialRows(ordered []orderedInitialRow) []types.ProductValue {
	out := make([]types.ProductValue, 0, len(ordered))
	for _, row := range ordered {
		out = append(out, row.row)
	}
	return out
}
