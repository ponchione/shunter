package subscription

import (
	"bytes"
	"container/heap"
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

type boundedOrderedInitialRow struct {
	row types.ProductValue
	key []byte
}

type boundedOrderedInitialRows struct {
	orderBy []OrderByColumn
	keep    int
	rows    []boundedOrderedInitialRow
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
	orderedRowKeyCapHint         = 40
	boundedOrderedRowKeyCapHint  = 64
	orderedSafePreallocationRows = 1_024
	orderedSafeKeyCapHint        = 64 << 10
	// DefaultOrderedWindowMaxRows bounds the logical top-K working set used by
	// ordered subscription snapshots.
	DefaultOrderedWindowMaxRows = 100_000
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
		keys:    orderedRowKeyer{capHint: orderedKeyCapacityHint(len(ordered), orderedRowKeyCapHint)},
	})
	return flattenOrderedInitialRows(ordered), nil
}

func newBoundedOrderedInitialRows(orderBy []OrderByColumn, keep int) *boundedOrderedInitialRows {
	return newBoundedOrderedInitialRowsWithCapacity(orderBy, keep, keep)
}

func newBoundedOrderedInitialRowsWithCapacity(orderBy []OrderByColumn, keep, available int) *boundedOrderedInitialRows {
	if len(orderBy) == 0 || keep <= 0 {
		return nil
	}
	capacity := min(keep, max(available, 0), orderedSafePreallocationRows)
	return &boundedOrderedInitialRows{
		orderBy: orderBy,
		keep:    keep,
		rows:    make([]boundedOrderedInitialRow, 0, capacity),
		itemKey: orderedRowKeyer{capHint: boundedOrderedRowKeyCapHint},
	}
}

func orderedKeyCapacityHint(rows, bytesPerRow int) int {
	if rows <= 0 || bytesPerRow <= 0 {
		return 0
	}
	if rows > orderedSafeKeyCapHint/bytesPerRow {
		return orderedSafeKeyCapHint
	}
	return rows * bytesPerRow
}

func (b *boundedOrderedInitialRows) add(row types.ProductValue) error {
	if b == nil {
		return nil
	}
	if err := validateInitialRowOrderRow(row, b.orderBy); err != nil {
		return err
	}
	item := boundedOrderedInitialRow{row: row}
	if len(b.rows) < b.keep {
		heap.Push(b, item)
		return nil
	}
	cmp, itemKey := b.compareCandidate(&item, &b.rows[0])
	if cmp >= 0 {
		return nil
	}
	item.key = b.rows[0].key[:0]
	if itemKey != nil {
		item.key = append(item.key, itemKey...)
	}
	b.rows[0] = item
	heap.Fix(b, 0)
	return nil
}

func (b *boundedOrderedInitialRows) productRows() []types.ProductValue {
	if b == nil {
		return nil
	}
	sort.Slice(b.rows, func(i, j int) bool {
		return b.compareRetained(&b.rows[i], &b.rows[j]) < 0
	})
	out := make([]types.ProductValue, 0, len(b.rows))
	for _, row := range b.rows {
		out = append(out, row.row)
	}
	return out
}

// boundedOrderedInitialRows implements a max-heap whose root is the worst
// retained row. Candidates that cannot improve the top-K set are discarded
// without shifting the retained window.
func (b *boundedOrderedInitialRows) Len() int { return len(b.rows) }

func (b *boundedOrderedInitialRows) Less(i, j int) bool {
	return b.compareRetained(&b.rows[i], &b.rows[j]) > 0
}

func (b *boundedOrderedInitialRows) Swap(i, j int) { b.rows[i], b.rows[j] = b.rows[j], b.rows[i] }

func (b *boundedOrderedInitialRows) Push(value any) {
	b.rows = append(b.rows, value.(boundedOrderedInitialRow))
}

func (b *boundedOrderedInitialRows) Pop() any {
	last := len(b.rows) - 1
	row := b.rows[last]
	b.rows[last] = boundedOrderedInitialRow{}
	b.rows = b.rows[:last]
	return row
}

func (b *boundedOrderedInitialRows) compareCandidate(candidate, retained *boundedOrderedInitialRow) (int, []byte) {
	if cmp := compareOrderedProductRows(candidate.row, retained.row, b.orderBy); cmp != 0 {
		return cmp, nil
	}
	candidateKey := b.itemKey.scratchProductRowKey(candidate.row)
	return bytes.Compare(candidateKey, retainedOrderedRowKey(retained)), candidateKey
}

func (b *boundedOrderedInitialRows) compareRetained(a, c *boundedOrderedInitialRow) int {
	if cmp := compareOrderedProductRows(a.row, c.row, b.orderBy); cmp != 0 {
		return cmp
	}
	return bytes.Compare(retainedOrderedRowKey(a), retainedOrderedRowKey(c))
}

func retainedOrderedRowKey(row *boundedOrderedInitialRow) []byte {
	if len(row.key) == 0 {
		row.key = encodeOrderedRowKey(row.key[:0], row.row)
	}
	return row.key
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
	return compareOrderedProductRows(a.row, b.row, orderBy)
}

func compareOrderedProductRows(a, b types.ProductValue, orderBy []OrderByColumn) int {
	for _, term := range orderBy {
		idx := int(term.Column)
		cmp := a[idx].Compare(b[idx])
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
		k.buf = encodeOrderedRowKey(k.buf, row.row)
		row.keyStart, row.keyLen = checkedOrderedRowKeyRange(start, len(k.buf)-start)
	}
	start := int(row.keyStart)
	return k.buf[start : start+int(row.keyLen)]
}

func encodeOrderedRowKey(buf []byte, row types.ProductValue) []byte {
	enc := canonicalEncoder{buf: buf}
	enc.writeLen(len(row))
	for _, v := range row {
		encodeValue(&enc, v)
	}
	return enc.buf
}

func (k *orderedRowKeyer) scratchProductRowKey(row types.ProductValue) []byte {
	k.buf = k.buf[:0]
	k.buf = encodeOrderedRowKey(k.buf, row)
	return k.buf
}

func checkedOrderedRowKeyRange(start, length int) (uint32, uint32) {
	if uint64(start) > uint64(^uint32(0)) || uint64(length) > uint64(^uint32(0)) {
		panic("subscription: ordered row key buffer exceeds uint32")
	}
	return uint32(start), uint32(length)
}

func flattenOrderedInitialRows(ordered []orderedInitialRow) []types.ProductValue {
	out := make([]types.ProductValue, 0, len(ordered))
	for _, row := range ordered {
		out = append(out, row.row)
	}
	return out
}
