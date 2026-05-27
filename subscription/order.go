package subscription

import (
	"bytes"
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
	row      types.ProductValue
	keyStart uint32
	keyLen   uint32
}

type boundedOrderedInitialRows struct {
	orderBy []OrderByColumn
	keep    int
	rows    []orderedInitialRow
	keys    orderedRowKeyer
	itemKey orderedRowKeyer
}

type orderedInitialRowsSorter struct {
	rows    []orderedInitialRow
	orderBy []OrderByColumn
	keys    orderedRowKeyer
}

func (s *orderedInitialRowsSorter) Len() int { return len(s.rows) }

func (s *orderedInitialRowsSorter) Less(i, j int) bool {
	return compareOrderedInitialRows(&s.rows[i], &s.rows[j], s.orderBy, &s.keys) < 0
}

func (s *orderedInitialRowsSorter) Swap(i, j int) { s.rows[i], s.rows[j] = s.rows[j], s.rows[i] }

// orderedRowKeyer stores canonical row tie-break keys in one local arena so
// tied ORDER BY groups do not allocate one string per row.
type orderedRowKeyer struct {
	buf     []byte
	capHint int
}

const (
	orderedRowKeyCapHint        = 40
	boundedOrderedRowKeyCapHint = 64
)

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
	sort.Stable(&orderedInitialRowsSorter{
		rows:    ordered,
		orderBy: orderBy,
		keys:    orderedRowKeyer{capHint: len(ordered) * orderedRowKeyCapHint},
	})
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
		keys:    orderedRowKeyer{capHint: keep * boundedOrderedRowKeyCapHint},
		itemKey: orderedRowKeyer{capHint: orderedRowKeyCapHint},
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
	pos := upperBoundOrderedInitialRows(b.rows, &item, b.orderBy, &b.keys, &b.itemKey)
	if len(b.rows) == b.keep && pos == b.keep {
		return nil
	}
	if item.keyLen != 0 {
		b.keys.appendKey(&item, b.itemKey.rowKey(&item))
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

func compareOrderedInitialRows(a, b *orderedInitialRow, orderBy []OrderByColumn, keys *orderedRowKeyer) int {
	if cmp := compareOrderedColumns(a, b, orderBy); cmp != 0 {
		return cmp
	}
	return bytes.Compare(keys.rowKey(a), keys.rowKey(b))
}

func compareOrderedColumns(a, b *orderedInitialRow, orderBy []OrderByColumn) int {
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
	return 0
}

func (k *orderedRowKeyer) rowKey(row *orderedInitialRow) []byte {
	if row.keyLen == 0 {
		if k.buf == nil && k.capHint > 0 {
			k.buf = make([]byte, 0, k.capHint)
		}
		start := len(k.buf)
		enc := canonicalEncoder{buf: k.buf}
		enc.writeLen(len(row.row))
		for _, v := range row.row {
			encodeValue(&enc, v)
		}
		k.buf = enc.buf
		row.keyStart, row.keyLen = checkedOrderedRowKeyRange(start, len(k.buf)-start)
	}
	start := int(row.keyStart)
	return k.buf[start : start+int(row.keyLen)]
}

func (k *orderedRowKeyer) scratchRowKey(row *orderedInitialRow) []byte {
	if row.keyLen == 0 {
		k.buf = k.buf[:0]
	}
	return k.rowKey(row)
}

func (k *orderedRowKeyer) appendKey(row *orderedInitialRow, key []byte) {
	if k.buf == nil && k.capHint > 0 {
		k.buf = make([]byte, 0, k.capHint)
	}
	start := len(k.buf)
	k.buf = append(k.buf, key...)
	row.keyStart, row.keyLen = checkedOrderedRowKeyRange(start, len(key))
}

func checkedOrderedRowKeyRange(start, length int) (uint32, uint32) {
	if uint64(start) > uint64(^uint32(0)) || uint64(length) > uint64(^uint32(0)) {
		panic("subscription: ordered row key buffer exceeds uint32")
	}
	return uint32(start), uint32(length)
}

func upperBoundOrderedInitialRows(rows []orderedInitialRow, item *orderedInitialRow, orderBy []OrderByColumn, keys, itemKeys *orderedRowKeyer) int {
	return sort.Search(len(rows), func(i int) bool {
		for _, term := range orderBy {
			idx := int(term.Column)
			cmp := rows[i].row[idx].Compare(item.row[idx])
			if cmp == 0 {
				continue
			}
			if term.Desc {
				return -cmp > 0
			}
			return cmp > 0
		}
		return bytes.Compare(keys.rowKey(&rows[i]), itemKeys.scratchRowKey(item)) > 0
	})
}

func flattenOrderedInitialRows(ordered []orderedInitialRow) []types.ProductValue {
	out := make([]types.ProductValue, 0, len(ordered))
	for _, row := range ordered {
		out = append(out, row.row)
	}
	return out
}
